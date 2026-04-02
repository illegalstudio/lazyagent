package amp

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// syncState tracks the last CLI sync to throttle calls.
var syncState struct {
	mu        sync.Mutex
	lastSync  time.Time
	// message counts from the last `amp threads list` call, keyed by thread ID.
	knownCounts map[string]int
}

const syncInterval = 30 * time.Second

// SyncRemoteThreads checks for new or updated threads via `amp threads list`
// and exports any that have changed to the local threads directory.
// This is called from DiscoverSessions and throttled to run at most every 30s.
func SyncRemoteThreads() {
	syncState.mu.Lock()
	if time.Since(syncState.lastSync) < syncInterval {
		syncState.mu.Unlock()
		return
	}
	syncState.lastSync = time.Now()
	syncState.mu.Unlock()

	ampPath, err := exec.LookPath("amp")
	if err != nil {
		return
	}

	threadsDir := ThreadsDir()
	if threadsDir == "" {
		return
	}

	entries := listRemoteThreads(ampPath)
	if len(entries) == 0 {
		return
	}

	syncState.mu.Lock()
	if syncState.knownCounts == nil {
		syncState.knownCounts = make(map[string]int)
	}
	syncState.mu.Unlock()

	for _, e := range entries {
		localPath := filepath.Join(threadsDir, e.id+".json")
		localMsgs := localMessageCount(localPath)

		syncState.mu.Lock()
		prevRemote := syncState.knownCounts[e.id]
		syncState.knownCounts[e.id] = e.messages
		syncState.mu.Unlock()

		needsExport := false
		if localMsgs < 0 {
			// No local file — new thread.
			needsExport = true
		} else if e.messages > localMsgs {
			// Remote has more messages.
			needsExport = true
		} else if e.messages > prevRemote && prevRemote > 0 {
			// Message count grew since last check.
			needsExport = true
		}

		if needsExport {
			exportThread(ampPath, e.id, localPath)
		}
	}
}

type remoteThread struct {
	id       string
	messages int
}

// listRemoteThreads runs `amp threads list --no-color` and parses the output.
func listRemoteThreads(ampPath string) []remoteThread {
	cmd := exec.Command(ampPath, "threads", "list", "--no-color")
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var result []remoteThread
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if lineNo <= 2 {
			continue // header + separator
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		id, msgs := parseListLine(line)
		if id != "" {
			result = append(result, remoteThread{id: id, messages: msgs})
		}
	}
	return result
}

// parseListLine extracts thread ID and message count from a table row.
// Parses from right to avoid issues with variable-width title column.
func parseListLine(line string) (string, int) {
	// Thread ID is the last double-space-delimited token.
	idx := strings.LastIndex(line, "  ")
	if idx < 0 {
		return "", 0
	}
	threadID := strings.TrimSpace(line[idx:])
	if !strings.HasPrefix(threadID, "T-") {
		return "", 0
	}
	rest := strings.TrimRight(line[:idx], " ")

	// Messages is the next token from the right.
	idx = strings.LastIndex(rest, "  ")
	if idx < 0 {
		return threadID, 0
	}
	msgs, _ := strconv.Atoi(strings.TrimSpace(rest[idx:]))
	return threadID, msgs
}

// localMessageCount returns the total messages in a local thread file,
// or -1 if the file doesn't exist.
func localMessageCount(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	// Quick count: count "messageId" occurrences as a proxy.
	// This is faster than full JSON parsing and good enough for comparison.
	return strings.Count(string(data), `"messageId"`)
}

// exportThread runs `amp threads export <id>` and writes stdout directly to path.
// Writing directly to a file avoids pipe buffer limits that truncate cmd.Output().
func exportThread(ampPath, threadID, path string) {
	// Ensure directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}

	cmd := exec.Command(ampPath, "threads", "export", threadID, "--no-color")
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	cmd.Stdout = f
	err = cmd.Run()
	f.Close()

	if err != nil {
		os.Remove(tmp)
		return
	}

	// Verify we got something non-empty.
	info, err := os.Stat(tmp)
	if err != nil || info.Size() == 0 {
		os.Remove(tmp)
		return
	}

	os.Rename(tmp, path)
}
