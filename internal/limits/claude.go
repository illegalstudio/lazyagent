package limits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/illegalstudio/lazyagent/internal/version"
)

// claudeUsageURL is the (undocumented) endpoint Claude Code itself calls for /status.
// See README disclaimer in fetchClaudeLimits.
const (
	claudeUsageURL  = "https://api.anthropic.com/api/oauth/usage"
	claudeBetaValue = "oauth-2025-04-20"
)

// userAgent returns the User-Agent we send to Anthropic. Honest identification
// (we don't impersonate Claude Code) so any rate-limit / abuse review can attribute
// traffic to lazyagent and find the project.
func userAgent() string {
	return "lazyagent/" + version.Version + " (+https://github.com/illegalstudio/lazyagent)"
}

// claudeUsageResponse mirrors the shape returned by /api/oauth/usage.
// Unknown / null fields (seven_day_opus etc.) are omitted; we only need 5h + 7d.
type claudeUsageResponse struct {
	FiveHour *claudeWindow `json:"five_hour"`
	SevenDay *claudeWindow `json:"seven_day"`
}

type claudeWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

func fetchClaudeReport(ctx context.Context) (Report, error) {
	token, err := readClaudeToken()
	if err != nil {
		if errors.Is(err, errAgentNotInstalled) {
			return Report{}, err
		}
		return Report{}, fmt.Errorf("read Claude OAuth token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, claudeUsageURL, nil)
	if err != nil {
		return Report{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", claudeBetaValue)
	req.Header.Set("User-Agent", userAgent())

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Report{}, fmt.Errorf("call Claude usage endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return Report{}, fmt.Errorf("Claude OAuth token rejected (401). Run `claude` to refresh, or set CLAUDE_CODE_OAUTH_TOKEN")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return Report{}, fmt.Errorf("Claude usage endpoint rate-limited (429). Try again in a minute")
	}
	if resp.StatusCode != http.StatusOK {
		return Report{}, fmt.Errorf("Claude usage endpoint: %s — %s", resp.Status, snippet(body, 200))
	}

	var usage claudeUsageResponse
	if err := json.Unmarshal(body, &usage); err != nil {
		return Report{}, fmt.Errorf("parse Claude usage response: %w", err)
	}

	r := Report{
		Provider: "Claude Code",
		Note:     "Note: reads /api/oauth/usage, an undocumented Claude endpoint. May break or be revoked by Anthropic without notice.",
	}
	if usage.FiveHour != nil {
		r.Windows = append(r.Windows, claudeWindowToWindow("5-hour", 300, *usage.FiveHour))
	}
	if usage.SevenDay != nil {
		r.Windows = append(r.Windows, claudeWindowToWindow("7-day", 7*24*60, *usage.SevenDay))
	}
	if len(r.Windows) == 0 {
		return Report{}, fmt.Errorf("Claude usage endpoint returned no five_hour / seven_day windows (response: %s)", snippet(body, 200))
	}
	return r, nil
}

func claudeWindowToWindow(label string, windowMinutes int, w claudeWindow) Window {
	resetsAt, _ := time.Parse(time.RFC3339Nano, w.ResetsAt)
	return Window{
		Label:         label,
		WindowMinutes: windowMinutes,
		UsedPercent:   w.Utilization,
		ResetsAt:      resetsAt,
	}
}

// readClaudeToken returns the OAuth access token, in this priority order:
//  1. CLAUDE_CODE_OAUTH_TOKEN env var (lets users override / use in CI)
//  2. macOS Keychain (Claude Code-credentials)
//  3. ~/.claude/.credentials.json (Linux default; macOS fallback)
//
// Windows storage (Credential Manager) is not yet implemented; users on Windows
// can fall back to the env var or the file (Claude Code on WSL writes the file).
func readClaudeToken() (string, error) {
	if v := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); v != "" {
		return v, nil
	}
	if runtime.GOOS == "darwin" {
		if tok, err := readClaudeTokenKeychain(); err == nil && tok != "" {
			return tok, nil
		}
		// Fall through to file in case the user has a non-standard setup.
	}
	if tok, err := readClaudeTokenFile(); err == nil && tok != "" {
		return tok, nil
	}
	// No source produced a token. From the command's perspective, Claude is either
	// not installed or not logged in — both look identical from here.
	return "", errAgentNotInstalled
}

func readClaudeTokenKeychain() (string, error) {
	user := os.Getenv("USER")
	if user == "" {
		return "", fmt.Errorf("USER env var unset")
	}
	cmd := exec.Command("security", "find-generic-password",
		"-s", "Claude Code-credentials",
		"-a", user,
		"-w",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return parseCredentialsJSON(out)
}

func readClaudeTokenFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".claude", ".credentials.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return parseCredentialsJSON(data)
}

// parseCredentialsJSON extracts claudeAiOauth.accessToken from the credentials blob.
// Both the keychain payload and the file have the same JSON shape.
func parseCredentialsJSON(data []byte) (string, error) {
	var creds struct {
		ClaudeAIOauth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("parse credentials JSON: %w", err)
	}
	if creds.ClaudeAIOauth.AccessToken == "" {
		return "", fmt.Errorf("credentials missing claudeAiOauth.accessToken")
	}
	return creds.ClaudeAIOauth.AccessToken, nil
}

func snippet(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}
