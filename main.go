package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nahime0/lazyagent/internal/tray"
	"github.com/nahime0/lazyagent/internal/ui"
)

var trayPidFile = os.TempDir() + "/lazyagent-tray.pid"

func main() {
	trayMode := flag.Bool("tray", false, "Launch as macOS menu bar app (detaches automatically)")
	tuiMode := flag.Bool("tui", false, "Launch the terminal UI (default when no flags given)")
	demoMode := flag.Bool("demo", false, "Use generated fake data instead of real Claude sessions")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `lazyagent — monitor all running Claude Code sessions

Usage:
  lazyagent                Launch the terminal UI
  lazyagent --tui          Launch the terminal UI (explicit)
  lazyagent --tray         Launch as macOS menu bar app (detaches automatically)
  lazyagent --tui --tray   Launch both TUI and tray app
  lazyagent --demo         Launch with fake data (for screenshots)

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

	// Default: TUI if no flags given.
	runTUI := *tuiMode || !*trayMode
	runTray := *trayMode

	if runTray {
		if !tray.Available() {
			fmt.Fprintln(os.Stderr, "Error: --tray is not available in this build")
			os.Exit(1)
		}

		// If not already detached, kill previous instance and re-launch tray in background.
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
			// If also running TUI, fall through; otherwise exit.
			if !runTUI {
				return
			}
		} else {
			// Detached tray process: write PID file and run.
			_ = os.WriteFile(trayPidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
			defer os.Remove(trayPidFile)

			if err := tray.Run(*demoMode); err != nil {
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
