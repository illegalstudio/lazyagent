package main

import (
	"log"
	"log/slog"
	"os"

	"github.com/nahime0/lazyagent/internal/assets"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/icons"
)

func main() {
	svc := &SessionService{}

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
	tray.SetTemplateIcon(icons.SystrayMacTemplate)
	tray.SetTooltip("lazyagent")

	// Panel window attached to tray
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
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

	tray.AttachWindow(window).WindowOffset(5)

	// Context menu
	menu := app.NewMenu()
	menu.Add("Show Panel").OnClick(func(ctx *application.Context) {
		window.Show()
	})
	menu.Add("Refresh Now").OnClick(func(ctx *application.Context) {
		_ = svc.manager.Reload()
		svc.emitUpdate()
	})
	menu.AddSeparator()
	menu.Add("Quit lazyagent").OnClick(func(ctx *application.Context) {
		app.Quit()
	})
	tray.SetMenu(menu)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
