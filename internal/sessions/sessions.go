package sessions

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/illegalstudio/lazyagent/internal/chatops"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/model"
	"github.com/mattn/go-isatty"
)

var validAgents = map[string]bool{
	"claude": true, "pi": true, "opencode": true, "kilo": true, "cursor": true,
	"codex": true, "amp": true, "grok": true, "kimi": true, "all": true,
}

// Run implements `lazyagent sessions`. It lists the sessions recorded for
// the current (or --dir) directory across agents and reopens the chosen one.
func Run(args []string) int {
	fs := flag.NewFlagSet("sessions", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	agent := fs.String("agent", "all", "Agent to list: claude, pi, opencode, kilo, cursor, codex, amp, grok, kimi, all")
	jsonOut := fs.Bool("json", false, "Print the session list as JSON and exit")
	dirFlag := fs.String("dir", "", "List sessions for this directory instead of the current one")
	yolo := fs.Bool("yolo", false, "Use the agent-specific YOLO mode when reopening a session with Enter")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `lazyagent sessions — list sessions for a directory and reopen one

Lists every recorded session whose working directory is the current
directory (or --dir) or a subdirectory of it, across all agents.
Selecting a session resumes it with the originating agent's CLI.

Usage:
  lazyagent sessions
  lazyagent sessions --agent claude
  lazyagent sessions --json
  lazyagent sessions --dir ~/projects/foo
  lazyagent sessions --yolo

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if !validAgents[*agent] {
		fmt.Fprintf(os.Stderr, "Error: unknown --agent value %q (use claude, pi, opencode, kilo, cursor, codex, amp, grok, kimi, or all)\n", *agent)
		return 2
	}

	dir, err := resolveTargetDir(*dirFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}

	cfg := core.LoadConfig()
	provider := core.BuildProvider(*agent, cfg)

	// Persistence is advisory and best-effort: when the user cache
	// directory can't be resolved, discovery just runs cold, exactly as
	// before this existed. This subcommand wires Load/Save explicitly
	// (below and around the picker) rather than through
	// core.SessionManager.EnableCachePersistence -- the mechanism the
	// long-lived surfaces (TUI/GUI/API) use -- because it needs save points
	// tied to this command's own control flow (e.g. skipping the save when
	// the interactive picker exits before its background discovery stream
	// finishes; see runInteractive).
	cacheDir, hasCacheDir := ResolveCacheDir()
	if hasCacheDir {
		core.LoadProviderCaches(provider, cacheDir)
	}

	variants, err := targetVariants(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	match := func(cwd string) bool { return matchesDir(cwd, variants) }
	names := core.NewSessionNames()

	// Interactive picker path: open immediately and stream discovery
	// results in as agents finish (see runPicker), instead of blocking on
	// a full discovery first. This only applies when we're actually about
	// to show a picker on a real terminal -- --json always wants the
	// complete, byte-identical list, and without a TTY there is no
	// progressive rendering to benefit from, so both of those keep using
	// the plain blocking flow below unchanged.
	//
	// Accepted divergence from that blocking flow: core.DiscoverMatchingStream
	// has no error channel (best-effort by design, the same swallow
	// semantics MultiProvider's fan-out already has in the default "all"
	// mode), so here a single misconfigured provider's hard error is
	// indistinguishable from "found nothing" -- it surfaces as an empty
	// listing (exit 0) rather than the sharp "Error: ..." + exit 1 that
	// --json and the no-TTY fallback below still give. Signed off as
	// acceptable specifically for the interactive path.
	if !*jsonOut && isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stderr.Fd()) {
		mode := core.ResumeNormal
		if *yolo {
			mode = core.ResumeYolo
		}
		return runInteractive(provider, match, dir, names, hasCacheDir, cacheDir, mode)
	}

	all, err := core.DiscoverMatching(provider, match)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	// Save right after a successful discovery, reached by --json and by
	// the non-interactive (no-TTY) fallback below, and even when zero
	// sessions were found. Never reached on a discovery error (the return
	// above). The interactive picker path above has its own, later save
	// point tied to stream completion -- see runInteractive.
	if hasCacheDir {
		core.SaveProviderCaches(provider, cacheDir)
	}
	filtered, err := FilterByDir(all, dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	if *jsonOut {
		nameFor := func(s *model.Session) string {
			if alias := names.Get(s.SessionID); alias != "" {
				return alias
			}
			return s.Name
		}
		if err := writeJSON(os.Stdout, filtered, nameFor); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		return 0
	}

	if len(filtered) == 0 {
		fmt.Fprintf(os.Stderr, "No sessions found in %s.\n", abbreviateHome(dir))
		return 0
	}
	fmt.Fprintln(os.Stderr, "Error: the interactive picker needs a terminal (use --json for scripted output)")
	return 2
}

// runInteractive drives the streaming picker path: open the picker
// immediately, let discovery results stream in, then act on whatever the
// user chose. The resume/copy action always happens before any cache save
// (which itself only happens when the stream had already completed by the
// time the picker exited -- see runPicker's streamComplete and the
// maybeSave comment below), so a user's chosen action is never delayed by
// unfinished background discovery.
func runInteractive(provider core.SessionProvider, match func(string) bool, dir string, names *core.SessionNames, hasCacheDir bool, cacheDir string, defaultMode core.ResumeMode) int {
	dirLabel := abbreviateHome(dir)
	chosen, action, mode, streamComplete, err := runPicker(provider, match, dir, dirLabel, names, defaultMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Only persist provider caches when the discovery stream had already
	// fully finished by the time the picker exited -- if the user quit or
	// acted early, the providers' in-memory caches may still be getting
	// mutated by discovery goroutines running in the background (see
	// runPicker), so saving then would race with them; skipping the save
	// in that case is acceptable, since the next run just redoes the work.
	maybeSave := func() {
		if hasCacheDir && streamComplete {
			core.SaveProviderCaches(provider, cacheDir)
		}
	}

	switch action {
	case actionEmpty:
		fmt.Fprintf(os.Stderr, "No sessions found in %s.\n", dirLabel)
		maybeSave()
		return 0
	case actionOpen:
		code := openSession(chosen, mode)
		maybeSave()
		return code
	case actionCopy:
		cmdStr := core.ResumeCommandWithMode(chosen.Agent, chosen.SessionID, mode)
		code := 0
		if err := core.CopyToClipboard(cmdStr); err != nil {
			fmt.Fprintf(os.Stderr, "Copy failed: %v\nCommand: %s\n", err, cmdStr)
			code = 1
		} else {
			fmt.Fprintf(os.Stderr, "Copied to clipboard: %s\n", cmdStr)
		}
		maybeSave()
		return code
	default: // actionQuit
		maybeSave()
		return 0
	}
}

// openSession execs the agent's resume command in the current terminal,
// running from the session's own CWD when it still exists (claude --resume
// locates sessions by project directory).
func openSession(s *model.Session, mode core.ResumeMode) int {
	argv := core.ResumeArgvWithMode(s.Agent, s.SessionID, mode)
	if argv == nil {
		if mode == core.ResumeYolo && core.ResumeArgv(s.Agent, s.SessionID) != nil {
			fmt.Fprintf(os.Stderr, "No YOLO resume mode available for %s sessions.\n", s.Agent)
		} else {
			fmt.Fprintf(os.Stderr, "No resume command available for %s sessions.\n", s.Agent)
		}
		return 1
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	if s.CWD != "" {
		if info, err := os.Stat(s.CWD); err == nil && info.IsDir() {
			cmd.Dir = s.CWD
		}
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Fprintf(os.Stderr, "%s %s\n", chatops.StyleMuted.Render("Opening:"), core.ResumeCommandWithMode(s.Agent, s.SessionID, mode))
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

// discoverDirSessions runs a full blocking discovery for agentMode and
// returns dir's sessions, recency-sorted (FilterByDir), with the advisory
// provider caches loaded before and saved after. RunLatest has no picker,
// so it has no reason to stream. On failure it prints the error to stderr
// and returns a non-zero exit code for the caller to return.
func discoverDirSessions(agentMode, dir string) ([]*model.Session, int) {
	cfg := core.LoadConfig()
	provider := core.BuildProvider(agentMode, cfg)

	cacheDir, hasCacheDir := ResolveCacheDir()
	if hasCacheDir {
		core.LoadProviderCaches(provider, cacheDir)
	}

	variants, err := targetVariants(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return nil, 1
	}
	match := func(cwd string) bool { return matchesDir(cwd, variants) }

	all, err := core.DiscoverMatching(provider, match)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return nil, 1
	}
	if hasCacheDir {
		core.SaveProviderCaches(provider, cacheDir)
	}
	filtered, err := FilterByDir(all, dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return nil, 1
	}
	return filtered, 0
}

// resolveTargetDir returns the directory a subcommand should operate on:
// dirFlag when given, otherwise the current working directory. The returned
// error is user-facing (callers prefix it with "Error: " and exit 2).
func resolveTargetDir(dirFlag string) (string, error) {
	dir := dirFlag
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("cannot determine working directory: %v", err)
		}
		dir = wd
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", dir)
	}
	return dir, nil
}

// abbreviateHome shortens a path with the user's home directory to ~/...
func abbreviateHome(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" && (path == home || strings.HasPrefix(path, home+"/")) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// stripControl removes control runes so user-controlled directory names
// cannot inject terminal escape sequences into command output.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}
