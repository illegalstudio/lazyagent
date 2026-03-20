//go:build !notray

package tray

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/illegalstudio/lazyagent/internal/assets"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/version"
	"github.com/pkg/browser"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// buildProvider returns the session provider for the given agent mode.
func buildProvider(agentMode string) core.SessionProvider {
	cfg := core.LoadConfig()
	return core.BuildProvider(agentMode, cfg)
}

// Available reports whether tray support was compiled in.
func Available() bool { return true }

// Run starts the macOS menu bar app with system tray.
func Run(demoMode bool, agentMode string) error {
	if !assets.HasFrontend() {
		return fmt.Errorf("frontend assets not found — run 'make build' to include the menu bar app")
	}

	svc := &SessionService{demoMode: demoMode, provider: buildProvider(agentMode)}

	app := application.New(application.Options{
		Name:        "lazyagent",
		Description: "Claude Code session monitor",
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyAccessory,
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets.Frontend),
		},
		Services: []application.Service{
			application.NewService(svc),
		},
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})

	svc.app = app

	// System tray
	tray := app.SystemTray.New()
	tray.SetTemplateIcon(trayIcon)
	tooltip := "lazyagent"
	if version.Version != "dev" {
		tooltip += " v" + version.Version
	}
	tray.SetTooltip(tooltip)

	// Panel window attached to tray
	panelWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "main",
		Title:           "lazyagent",
		Width:           380,
		Height:          520,
		Frameless:       true,
		AlwaysOnTop:     true,
		Hidden:          true,
		DisableResize:   false,
		HideOnFocusLost: true,
		BackgroundType:  application.BackgroundTypeTranslucent,
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBar{
				AppearsTransparent: true,
				Hide:               true,
			},
			Backdrop:    application.MacBackdropTranslucent,
			WindowLevel: application.MacWindowLevelFloating,
		},
		URL: "/",
	})

	// Detached window (normal window, hidden at startup)
	detachedWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:           "detached",
		Title:          "lazyagent",
		Width:          800,
		Height:         600,
		Hidden:         true,
		DisableResize:  false,
		BackgroundType: application.BackgroundTypeTranslucent,
		Mac: application.MacWindow{
			Backdrop: application.MacBackdropTranslucent,
		},
		URL: "/",
	})

	// Intercept detached window close: hide instead and switch back to panel mode.
	detachedWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if svc.detached {
			event.Cancel()
			svc.Attach()
		}
	})

	svc.panelWindow = panelWindow
	svc.detachedWindow = detachedWindow
	svc.tray = tray

	tray.AttachWindow(panelWindow).WindowOffset(5)

	// Override tray click: if detached, focus the detached window instead.
	tray.OnClick(func() {
		if svc.detached {
			svc.detachedWindow.Show().Focus()
		} else {
			tray.ToggleWindow()
		}
	})

	// Context menu
	menu := app.NewMenu()
	menu.Add("Show Panel").OnClick(func(ctx *application.Context) {
		if svc.detached {
			svc.detachedWindow.Show().Focus()
		} else {
			panelWindow.Show()
		}
	})
	menu.Add("Refresh Now").OnClick(func(ctx *application.Context) {
		_ = svc.manager.Reload()
		svc.emitUpdate()
	})
	menu.AddSeparator()
	menu.Add("⭐ Star on GitHub").OnClick(func(ctx *application.Context) {
		_ = browser.OpenURL("https://github.com/illegalstudio/lazyagent")
	})
	menu.Add("Quit lazyagent").OnClick(func(ctx *application.Context) {
		app.Quit()
	})
	tray.SetMenu(menu)

	return app.Run()
}
