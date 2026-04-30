package limits

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/illegalstudio/lazyagent/internal/codex"
)

// codexRollouts returns absolute paths to every rollout-*.jsonl under ~/.codex/sessions,
// sorted newest-first by mtime.
func codexRollouts() ([]string, error) {
	root := codex.SessionsDir()
	if root == "" {
		return nil, fmt.Errorf("could not resolve home directory")
	}
	type fileEntry struct {
		path  string
		mtime time.Time
	}
	var entries []fileEntry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Skip unreadable subtrees rather than aborting the whole walk.
			return nil
		}
		if d.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		entries = append(entries, fileEntry{path: path, mtime: info.ModTime()})
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mtime.After(entries[j].mtime) })
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.path
	}
	return out, nil
}

// codexRateLimits is the relevant subset of an event_msg/token_count payload.
type codexRateLimits struct {
	Primary   *codexLimitWindow `json:"primary"`
	Secondary *codexLimitWindow `json:"secondary"`
}

type codexLimitWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"` // unix seconds
}

// scanRolloutForLimits returns the last rate_limits block found in path, or nil if none.
// Codex streams an event_msg/token_count event after each turn, with rate_limits embedded
// in the payload. We want the most recent one in the file.
func scanRolloutForLimits(path string) (*codexRateLimits, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	type envelope struct {
		Type    string `json:"type"`
		Payload struct {
			Type       string           `json:"type"`
			RateLimits *codexRateLimits `json:"rate_limits"`
		} `json:"payload"`
	}

	var last *codexRateLimits
	for scanner.Scan() {
		var env envelope
		if err := json.Unmarshal(scanner.Bytes(), &env); err != nil {
			continue
		}
		if env.Type == "event_msg" && env.Payload.Type == "token_count" && env.Payload.RateLimits != nil {
			last = env.Payload.RateLimits
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return last, nil
}

func fetchCodexReport() (Report, error) {
	rollouts, err := codexRollouts()
	if err != nil {
		return Report{}, fmt.Errorf("scan ~/.codex/sessions: %w", err)
	}
	if len(rollouts) == 0 {
		// No rollouts at all: either Codex isn't installed or it's never been run.
		// In both cases there's nothing useful we can show.
		return Report{}, errAgentNotInstalled
	}

	// The newest rollout may not yet have any rate_limits events (very new session
	// before its first server response). Fall back to older rollouts in mtime order.
	var (
		limits     *codexRateLimits
		sourcePath string
	)
	for _, p := range rollouts {
		l, err := scanRolloutForLimits(p)
		if err != nil {
			continue
		}
		if l != nil {
			limits = l
			sourcePath = p
			break
		}
	}
	if limits == nil {
		return Report{}, fmt.Errorf("no rate_limits events found in any Codex rollout under %s", codex.SessionsDir())
	}

	r := Report{
		Provider: "Codex",
		Source:   fmt.Sprintf("Source: %s", sourcePath),
		Note:     "Note: limits are read from the latest Codex session rollout, not fetched live. They reflect the server's last response.",
	}
	if limits.Primary != nil {
		r.Windows = append(r.Windows, codexWindowToWindow("5-hour", *limits.Primary))
	}
	if limits.Secondary != nil {
		r.Windows = append(r.Windows, codexWindowToWindow("7-day", *limits.Secondary))
	}
	if len(r.Windows) == 0 {
		return Report{}, fmt.Errorf("Codex rate_limits block had no primary/secondary windows (%s)", sourcePath)
	}
	return r, nil
}

func codexWindowToWindow(label string, w codexLimitWindow) Window {
	return Window{
		Label:         label,
		WindowMinutes: w.WindowMinutes,
		UsedPercent:   w.UsedPercent,
		ResetsAt:      time.Unix(w.ResetsAt, 0),
	}
}
