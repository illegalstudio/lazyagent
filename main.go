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

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `lazyagent — monitor all running Claude Code sessions

Usage:
  lazyagent                     Launch the terminal UI (default)
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
  r            Force refresh            q/ctrl+c  Quit

More info: https://github.com/illegalstudio/lazyagent
`)
	}

	flag.Parse()

	runAPI := *apiMode
	runTray := *trayMode
	// Default: TUI if no other mode explicitly requested.
	runTUI := *tuiMode || (!runTray && !runAPI)

	// When --api is active, the process stays in foreground (no detach).
	foreground := runAPI || runTUI

	if runTray {
		if !tray.Available() {
			fmt.Fprintln(os.Stderr, "Error: --tray is not available in this build")
			os.Exit(1)
		}

		if !foreground {
			// Tray-only: detach to background (existing behavior).
			if os.Getenv("LAZYAGENT_DETACHED") == "" {
				killPreviousTray()

				exe, err := os.Executable()
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				trayArgs := []string{"--tray"}
				if *demoMode {
					trayArgs = append(trayArgs, "--demo")
				}
				cmd := exec.Command(exe, trayArgs...)
				cmd.Env = append(os.Environ(), "LAZYAGENT_DETACHED=1")
				cmd.Stdin = nil
				cmd.Stdout = nil
				cmd.Stderr = nil
				if err := cmd.Start(); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				return
			}

			// Detached tray process.
			_ = os.WriteFile(trayPidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
			defer os.Remove(trayPidFile)

			if err := tray.Run(*demoMode); err != nil {
				os.Exit(1)
			}
			return
		}

		// Foreground tray (with --api or --tui): launch tray in background goroutine.
		// Kill previous detached tray first.
		killPreviousTray()
		go func() {
			if err := tray.Run(*demoMode); err != nil {
				fmt.Fprintf(os.Stderr, "Tray error: %v\n", err)
			}
		}()
	}

	// Set up signal handling for graceful shutdown.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if runAPI {
		srv, err := api.New(*apiHost, *demoMode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if runTUI {
			// API in background, TUI in foreground.
			go func() {
				if err := srv.Run(ctx); err != nil {
					fmt.Fprintf(os.Stderr, "API error: %v\n", err)
				}
			}()
		} else {
			// API only: block until signal.
			if err := srv.Run(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	if runTUI {
		p := tea.NewProgram(
			ui.NewModel(*demoMode),
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
		)

		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		// TUI exited: cancel ctx to stop API server.
		cancel()
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
