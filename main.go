package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/illegalstudio/lazyagent/internal/api"
	"github.com/illegalstudio/lazyagent/internal/apiauth"
	"github.com/illegalstudio/lazyagent/internal/assets"
	"github.com/illegalstudio/lazyagent/internal/compact"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/demo"
	"github.com/illegalstudio/lazyagent/internal/limits"
	"github.com/illegalstudio/lazyagent/internal/passphrase"
	"github.com/illegalstudio/lazyagent/internal/prune"
	"github.com/illegalstudio/lazyagent/internal/search"
	"github.com/illegalstudio/lazyagent/internal/sessions"
	"github.com/illegalstudio/lazyagent/internal/tray"
	"github.com/illegalstudio/lazyagent/internal/ui"
	"github.com/illegalstudio/lazyagent/internal/version"
	"github.com/illegalstudio/lazyagent/internal/webhook"
	"golang.org/x/term"
)

var trayPidFile = os.TempDir() + "/lazyagent-tray.pid"

type launchModes struct {
	gui  bool
	tray bool
	tui  bool
	api  bool
}

func shouldRunDirectGUI(inBundle, trayAvailable, stdinTTY, appImage bool, modes launchModes) bool {
	bundleLaunch := inBundle && trayAvailable &&
		!modes.gui && !modes.tray && !modes.tui && !modes.api && !stdinTTY
	appImageLaunch := appImage && (modes.gui || modes.tray) && !modes.tui && !modes.api
	return bundleLaunch || appImageLaunch
}

func main() {
	// Subcommands are parsed before the global flag set so they get their own
	// FlagSet and don't collide with lazyagent's mode flags.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "prune":
			os.Exit(prune.Run(os.Args[2:]))
		case "compact":
			os.Exit(compact.Run(os.Args[2:]))
		case "search":
			os.Exit(search.Run(os.Args[2:]))
		case "limits":
			os.Exit(limits.Run(os.Args[2:]))
		case "passphrase":
			os.Exit(passphrase.Run(os.Args[2:]))
		case "sessions", "history":
			os.Exit(sessions.Run(os.Args[2:]))
		case "latest":
			os.Exit(sessions.RunLatest(os.Args[2:]))
		}
	}

	showVersion := flag.Bool("version", false, "Print version and exit")
	guiMode := flag.Bool("gui", false, "Launch the desktop tray app")
	trayMode := flag.Bool("tray", false, "Launch the desktop tray app (deprecated: use --gui)")
	tuiMode := flag.Bool("tui", false, "Launch the terminal UI (default when no flags given)")
	apiMode := flag.Bool("api", false, "Start the API server")
	apiHost := flag.String("host", "", "API listen address (e.g. :7421 or 0.0.0.0:7421). Default: 127.0.0.1:7421")
	demoMode := flag.Bool("demo", false, "Use generated fake data instead of real Claude sessions")
	agentMode := flag.String("agent", "all", "Which agent sessions to show: claude, pi, opencode, kilo, cursor, codex, amp, grok, kimi, all (default: all)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `%s — monitor all running coding agent sessions

Usage:
  lazyagent                     Launch the terminal UI (default, monitors all agents)
  lazyagent --agent claude      Monitor only Claude Code sessions
  lazyagent --agent pi          Monitor only pi coding agent sessions
  lazyagent --agent opencode    Monitor only OpenCode sessions
  lazyagent --agent kilo        Monitor only Kilo sessions
  lazyagent --agent cursor      Monitor only Cursor sessions
  lazyagent --agent codex       Monitor only Codex CLI sessions
  lazyagent --agent amp         Monitor only Amp CLI sessions
  lazyagent --agent grok        Monitor only Grok CLI sessions
  lazyagent --agent kimi        Monitor only Kimi Code CLI sessions
  lazyagent --agent all         Monitor all agents (default)
  lazyagent --api               Start the API server (http://127.0.0.1:7421)
  lazyagent --api --host :7421  Start the API server on custom address
  lazyagent --tui --api         Launch TUI + API server
  lazyagent --gui               Launch the desktop tray app
  lazyagent --gui --api         Launch GUI + API server (foreground)
  lazyagent --tui --gui --api   Launch everything
  lazyagent --demo              Launch with fake data (for screenshots)

Subcommands:
  lazyagent prune --days N      Delete chat sessions older than N days
  lazyagent prune --help        Show prune options (--orphaned, --dry-run, ...)
  lazyagent compact             Shrink sessions by truncating bulky tool outputs
  lazyagent compact --help      Show compact options (--threshold-kb, --dry-run, ...)
  lazyagent search "query"      Search chat transcripts with highlighted snippets
  lazyagent sessions            List sessions for the current directory and reopen one
  lazyagent sessions --help     Show sessions options (--agent, --json, --dir, --yolo)
  lazyagent history             Alias for lazyagent sessions
  lazyagent latest              Resume the most recent session for the current directory
  lazyagent latest --help       Show latest options (--agent, --dir, --yolo)
  lazyagent limits              Show rate-limit / billing usage summary
  lazyagent limits --help       Show limits options (--agent claude|codex|grok|kimi|all, --detailed)
  lazyagent passphrase          Set or rotate the HTTP API passphrase
  lazyagent passphrase --show   Print the current bearer token without prompting

Flags:
`, version.String())
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
TUI keybindings:
  ↑/k, ↓/j    Navigate sessions       tab       Switch panel focus
  +/-          Adjust time window       f         Cycle activity filter
  /            Search by project path   o         Open in editor ($VISUAL)
  c/C          Copy normal/YOLO resume command
  l            Open limits              r         Rename / refresh limits
  q/ctrl+c     Quit

If you find lazyagent useful, leave a ⭐ → https://github.com/illegalstudio/lazyagent
`)
	}

	flag.Parse()

	if *showVersion {
		fmt.Println(version.String())
		if v := version.CheckLatest(); v != "" {
			fmt.Printf("Update available: %s → https://github.com/illegalstudio/lazyagent/releases\n", v)
		}
		return
	}

	// Build the session provider based on flags and config.
	cfg := core.LoadConfig()
	var provider core.SessionProvider
	if *demoMode {
		provider = demo.Provider{}
	} else {
		switch *agentMode {
		case "claude", "pi", "opencode", "kilo", "cursor", "codex", "amp", "grok", "kimi", "all":
			provider = core.BuildProvider(*agentMode, cfg)
		default:
			fmt.Fprintf(os.Stderr, "Error: unknown --agent value %q (use claude, pi, opencode, kilo, cursor, codex, amp, grok, kimi, or all)\n", *agentMode)
			os.Exit(1)
		}
	}

	runAPI := *apiMode
	if *trayMode {
		fmt.Fprintln(os.Stderr, "Warning: --tray is deprecated, use --gui instead")
	}
	// A LaunchServices launch (double-click, login item) executes the
	// bundle binary with no mode flags: treat it as a GUI launch that
	// runs in-process — the process already carries the bundle identity,
	// so forking would throw it away.
	exePath, _ := os.Executable()
	if rp, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = rp
	}
	inBundle := core.InBundlePath(exePath)
	// A LaunchServices launch (Finder, login item) has no controlling
	// terminal on stdin; a user typing the self-linked lazyagent in a
	// shell does. Bundle + no mode flags + no TTY = GUI launch; with a
	// TTY the default stays the TUI, as documented.
	// Keep AppImage-only GUI launches in-process. Its runtime owns the
	// temporary mount; exiting a forking parent can unmount WebKit resources
	// while the detached GUI child still needs them.
	runDirectGUI := shouldRunDirectGUI(
		inBundle,
		tray.Available(),
		term.IsTerminal(int(os.Stdin.Fd())),
		runtime.GOOS == "linux" && os.Getenv("APPIMAGE") != "",
		launchModes{gui: *guiMode, tray: *trayMode, tui: *tuiMode, api: *apiMode},
	)

	runGUI := *guiMode || *trayMode || runDirectGUI
	// Default: TUI if no other mode explicitly requested.
	runTUI := *tuiMode || (!runGUI && !runAPI)

	if runGUI {
		if !tray.Available() {
			fmt.Fprintln(os.Stderr, "Error: --gui is not available in this build")
			os.Exit(1)
		}
		// Pre-flight: the parent forks the tray with stderr suppressed, so a
		// missing frontend embed would exit silently. Catch it here for a
		// terminal-visible diagnostic.
		if !assets.HasFrontend() {
			fmt.Fprintln(os.Stderr, "Error: --gui requires embedded frontend assets that are missing from this binary.")
			fmt.Fprintln(os.Stderr, "Rebuild with 'make build' from source, or reinstall the official release.")
			os.Exit(1)
		}

		if os.Getenv("LAZYAGENT_DETACHED") == "" && !runDirectGUI {
			if inBundle {
				// Relaunch through LaunchServices so the GUI process
				// keeps the bundle identity (Cmd-Tab icon and name).
				// --demo/--agent are forwarded via `open`'s --args
				// passthrough; --gui is never forwarded, so the
				// relaunched bundle process enters with no mode flags
				// (plus these passthrough flags) and becomes
				// runDirectGUI on its own — no recursion possible.
				openArgs := []string{"-b", "com.illegalstudio.lazyagent"}
				var passthrough []string
				if *demoMode {
					passthrough = append(passthrough, "--demo")
				}
				if *agentMode != "all" {
					passthrough = append(passthrough, "--agent", *agentMode)
				}
				if len(passthrough) > 0 {
					openArgs = append(openArgs, "--args")
					openArgs = append(openArgs, passthrough...)
				}
				if err := exec.Command("open", openArgs...).Run(); err != nil {
					// Dev copies moved outside a registered bundle can
					// fail `open`; the bare fork still works, minus the
					// LaunchServices identity.
					forkTray(*demoMode, *agentMode)
				}
			} else {
				// Bare binary (make dev): fork with its own main thread.
				forkTray(*demoMode, *agentMode)
			}
			if !runTUI && !runAPI {
				return
			}
		} else {
			// Detached tray process, or a direct LaunchServices launch.
			_ = os.WriteFile(trayPidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
			defer os.Remove(trayPidFile)

			if err := tray.Run(*demoMode, *agentMode); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	// Set up signal handling for graceful shutdown.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// EventBus + webhook dispatcher for the main process.
	// When --gui is set the tray fork owns webhook delivery (avoids
	// cross-process duplicate POSTs); otherwise the main process owns it.
	var eventBus *core.EventBus
	var apiAddrAtomic atomic.Value
	apiAddrAtomic.Store("")
	if len(cfg.ValidWebhooks()) > 0 && os.Getenv("LAZYAGENT_DETACHED") == "" && !runGUI {
		eventBus = core.NewEventBus()
		httpClient := &http.Client{Timeout: 10 * time.Second}
		apiAddrFunc := func() string {
			v, _ := apiAddrAtomic.Load().(string)
			return v
		}
		disp := webhook.New(eventBus, &webhook.ConfigAdapter{Cfg: cfg}, httpClient, apiAddrFunc)
		go func() { _ = disp.Start(ctx) }()
	}

	var apiDone chan struct{}

	if runAPI {
		bearerToken, err := setupAPIAuth(&cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		srv, err := api.New(*apiHost, provider, bearerToken, cfg.APISalt, eventBus)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Store the resolved listen address so the webhook dispatcher can
		// include a self-link in outbound payloads.
		apiAddrAtomic.Store("http://" + normalizeAPIAddr(srv.Addr().String()))

		if runTUI {
			// API in background, TUI in foreground.
			apiDone = make(chan struct{})
			go func() {
				defer close(apiDone)
				if err := srv.Run(ctx); err != nil {
					fmt.Fprintf(os.Stderr, "API error: %v\n", err)
				}
			}()
		} else {
			// API only (no tray, no TUI): block until signal.
			if err := srv.Run(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	if runTUI {
		// ui.NewModel must stay here, evaluated as an argument to
		// tea.NewProgram, and not be hoisted above this call: it resolves the
		// theme via LoadTheme, whose "auto" case blocks on a terminal query
		// that has to run before p.Run() puts the terminal into raw mode /
		// the alt screen (see internal/ui/theme.go LoadTheme).
		p := tea.NewProgram(
			ui.NewModel(provider, eventBus),
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
		)

		finalModel, err := p.Run()
		// Close stops the file watcher and, if the discovery cache was
		// persisted (EnableCachePersistence in ui.NewModel), flushes it one
		// last time if dirty — the TUI's only shutdown hook, run regardless
		// of how p.Run() returned.
		if tm, ok := finalModel.(ui.Model); ok {
			tm.Manager().Close()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Thanks for using lazyagent! If you find it useful, leave a ⭐ → https://github.com/illegalstudio/lazyagent")
		// TUI exited: cancel ctx to stop API server, then wait for graceful shutdown.
		cancel()
		if apiDone != nil {
			<-apiDone
		}
	}
}

// setupAPIAuth resolves the API passphrase (env var, configured value, or
// interactive prompt), persists it if the user just chose one, and returns
// the bearer token derived from it.
//
// Returns an error when no passphrase is configured and stdin is not a
// terminal — in that case the API server cannot start.
func setupAPIAuth(cfg *core.Config) (string, error) {
	passphrase, fromPrompt, err := apiauth.ResolvePassphrase(cfg.APIPassphrase)
	if err != nil {
		if err == apiauth.ErrNoTTY {
			fmt.Fprintln(os.Stderr, "Error: API passphrase not configured.")
			fmt.Fprintln(os.Stderr, "Run `lazyagent --api` from a terminal to set one,")
			fmt.Fprintf(os.Stderr, "or set the %s environment variable.\n", apiauth.EnvVar)
			return "", fmt.Errorf("no passphrase available")
		}
		return "", err
	}

	saltChanged := core.EnsureAPISalt(cfg)
	if fromPrompt {
		cfg.APIPassphrase = passphrase
	}
	if fromPrompt || saltChanged {
		// PersistAPIAuth recomputes passphrase/salt on the freshest config
		// under the lock — writing this function's earlier snapshot would
		// silently revert a concurrent rotation.
		newPass := ""
		if fromPrompt {
			newPass = passphrase
		}
		persistedPass, salt, err := core.PersistAPIAuth(newPass)
		if err != nil {
			return "", fmt.Errorf("save config: %w", err)
		}
		cfg.APISalt = salt
		// Derive the token from credentials someone can actually derive
		// too: prompt → the value we just persisted; env → the env value
		// wins at runtime (documented); config snapshot → the freshest
		// persisted passphrase, which a concurrent rotation may have
		// changed between our load and this save.
		envSet := strings.TrimSpace(os.Getenv(apiauth.EnvVar)) != ""
		if !fromPrompt && !envSet && persistedPass != "" {
			passphrase = persistedPass
			cfg.APIPassphrase = persistedPass
		}
	}
	if fromPrompt {
		fmt.Fprintf(os.Stderr, "Passphrase saved to %s\n\n", core.ConfigPath())
	}

	token := apiauth.DeriveToken(passphrase, cfg.APISalt)
	fmt.Fprintln(os.Stderr, "API authentication enabled.")
	if fromPrompt {
		fmt.Fprintf(os.Stderr, "Bearer token: %s\n", token)
		fmt.Fprintln(os.Stderr, "Use header:   Authorization: Bearer <token>")
	} else {
		fmt.Fprintln(os.Stderr, "Use `lazyagent passphrase --show` to print the bearer token.")
	}
	fmt.Fprintln(os.Stderr)
	return token, nil
}

// forkTray launches the tray as a detached background process with its own main thread.
func forkTray(demoMode bool, agentMode string) {
	killPreviousTray()

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	trayArgs := []string{"--gui"}
	if demoMode {
		trayArgs = append(trayArgs, "--demo")
	}
	if agentMode != "all" {
		trayArgs = append(trayArgs, "--agent", agentMode)
	}
	cmd := exec.Command(exe, trayArgs...)
	cmd.Env = append(os.Environ(), "LAZYAGENT_DETACHED=1")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// normalizeAPIAddr substitutes wildcard bind hosts with a client-usable
// loopback so payloads carry navigable URLs instead of 0.0.0.0:NNNN.
func normalizeAPIAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// killPreviousTray reads the PID file, kills the old process if still alive, and cleans up.
func killPreviousTray() {
	data, err := os.ReadFile(trayPidFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		os.Remove(trayPidFile)
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(trayPidFile)
		return
	}
	// Check if process is alive (signal 0 doesn't kill, just checks).
	if proc.Signal(syscall.Signal(0)) == nil {
		_ = proc.Signal(syscall.SIGTERM)
	}
	os.Remove(trayPidFile)
}
