package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nahime0/lazyagent/internal/api"
	"github.com/nahime0/lazyagent/internal/core"
	"github.com/nahime0/lazyagent/internal/demo"
	"github.com/nahime0/lazyagent/internal/tray"
	"github.com/nahime0/lazyagent/internal/ui"
)

var trayPidFile = os.TempDir() + "/lazyagent-tray.pid"

func main() {
	trayMode := flag.Bool("tray", false, "Launch as macOS menu bar app")
	tuiMode := flag.Bool("tui", false, "Launch the terminal UI (default when no flags given)")
	apiMode := flag.Bool("api", false, "Start the API server")
	apiHost := flag.String("host", "", "API listen address (e.g. :7421 or 0.0.0.0:7421). Default: 127.0.0.1:7421")
	demoMode := flag.Bool("demo", false, "Use generated fake data instead of real Claude sessions")
	agentMode := flag.String("agent", "all", "Which agent sessions to show: claude, pi, opencode, cursor, all (default: all)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `lazyagent — monitor all running coding agent sessions

Usage:
  lazyagent                     Launch the terminal UI (default, monitors all agents)
  lazyagent --agent claude      Monitor only Claude Code sessions
  lazyagent --agent pi          Monitor only pi coding agent sessions
  lazyagent --agent opencode    Monitor only OpenCode sessions
  lazyagent --agent cursor      Monitor only Cursor sessions
  lazyagent --agent all         Monitor all agents (default)
  lazyagent --api               Start the API server (http://127.0.0.1:7421)
  lazyagent --api --host :7421  Start the API server on custom address
  lazyagent --tui --api         Launch TUI + API server
  lazyagent --tray              Launch as macOS menu bar app (detaches)
  lazyagent --tray --api        Launch tray + API server (foreground)
  lazyagent --tui --tray --api  Launch everything
  lazyagent --demo              Launch with fake data (for screenshots)

Flags:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
TUI keybindings:
  ↑/k, ↓/j    Navigate sessions       tab       Switch panel focus
  +/-          Adjust time window       f         Cycle activity filter
  /            Search by project path   o         Open in editor ($VISUAL)
  r            Rename session           q/ctrl+c  Quit

More info: https://github.com/illegalstudio/lazyagent
`)
	}

	flag.Parse()

	// Build the session provider based on flags and config.
	cfg := core.LoadConfig()
	var provider core.SessionProvider
	if *demoMode {
		provider = demo.Provider{}
	} else {
		switch *agentMode {
		case "claude", "pi", "opencode", "cursor", "all":
			provider = core.BuildProvider(*agentMode, cfg)
		default:
			fmt.Fprintf(os.Stderr, "Error: unknown --agent value %q (use claude, pi, opencode, cursor, or all)\n", *agentMode)
			os.Exit(1)
		}
	}

	runAPI := *apiMode
	runTray := *trayMode
	// Default: TUI if no other mode explicitly requested.
	runTUI := *tuiMode || (!runTray && !runAPI)

	if runTray {
		if !tray.Available() {
			fmt.Fprintln(os.Stderr, "Error: --tray is not available in this build")
			os.Exit(1)
		}

		if os.Getenv("LAZYAGENT_DETACHED") == "" {
			// Always fork the tray as a separate process (macOS Cocoa needs its own main thread).
			forkTray(*demoMode, *agentMode)
			if !runTUI && !runAPI {
				return
			}
		} else {
			// Detached tray process.
			_ = os.WriteFile(trayPidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
			defer os.Remove(trayPidFile)

			if err := tray.Run(*demoMode, *agentMode); err != nil {
				os.Exit(1)
			}
			return
		}
	}

	// Set up signal handling for graceful shutdown.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var apiDone chan struct{}

	if runAPI {
		srv, err := api.New(*apiHost, provider)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

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
		p := tea.NewProgram(
			ui.NewModel(provider),
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
		)

		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		// TUI exited: cancel ctx to stop API server, then wait for graceful shutdown.
		cancel()
		if apiDone != nil {
			<-apiDone
		}
	}
}

// forkTray launches the tray as a detached background process with its own main thread.
func forkTray(demoMode bool, agentMode string) {
	killPreviousTray()

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	trayArgs := []string{"--tray"}
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
