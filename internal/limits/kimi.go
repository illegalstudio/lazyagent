package limits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/illegalstudio/lazyagent/internal/kimi"
)

// kimiOAuthClientID is the public client id Kimi Code CLI uses for its device
// OAuth flow; the refresh grant below is the same one the CLI performs.
const kimiOAuthClientID = "17e5f671-d194-4dfb-9706-5516cb48c098"

// kimiRefreshSkew refreshes slightly before expiry so a token cannot lapse
// between the check and the usage call.
const kimiRefreshSkew = 60 * time.Second

type kimiCredentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
}

// kimiToken is a bearer token for the usage call plus anything the user should
// know about how it was obtained. Warn is empty on the ordinary paths.
type kimiToken struct {
	Value string
	Warn  string
}

// kimiRefreshLocks serializes the read-refresh-write sequence per credential
// slot. Kimi rotates the refresh token on every grant, so two concurrent
// refreshes of one slot would race, and the loser would persist credentials the
// server has already superseded. This covers refreshes inside this process; it
// cannot coordinate with Kimi Code CLI itself, which takes no lock either — a
// refresh that loses that cross-process race fails, and the stale token falls
// through to the usage call's own 401.
var kimiRefreshLocks sync.Map // credential slot path -> *sync.Mutex

func kimiRefreshLock(path string) *sync.Mutex {
	lock, _ := kimiRefreshLocks.LoadOrStore(path, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// readKimiToken resolves a bearer token for the usage call. Kimi's access
// tokens live 15 minutes, so a credentials file the CLI has not touched
// recently is usually stale: when it is, lazyagent runs the same refresh grant
// the CLI does and writes the rotated tokens back to the slot it read.
func readKimiToken(ctx context.Context, env kimi.Environment) (kimiToken, error) {
	if v := os.Getenv("KIMI_CODE_OAUTH_TOKEN"); v != "" {
		return kimiToken{Value: v}, nil
	}
	path := env.CredentialsPath
	if path == "" {
		return kimiToken{}, errAgentNotInstalled
	}
	creds, _, err := readKimiCredentialsFile(path)
	if err != nil {
		return kimiToken{}, err
	}
	if !kimiTokenExpired(creds, time.Now()) {
		return kimiToken{Value: creds.AccessToken}, nil
	}

	lock := kimiRefreshLock(path)
	lock.Lock()
	defer lock.Unlock()

	// Re-read under the lock: another caller — or Kimi Code CLI — may have
	// refreshed this slot while we waited.
	creds, data, err := readKimiCredentialsFile(path)
	if err != nil {
		return kimiToken{}, err
	}
	if !kimiTokenExpired(creds, time.Now()) {
		return kimiToken{Value: creds.AccessToken}, nil
	}

	refreshed, warn, err := refreshKimiToken(ctx, env, creds, path, data)
	if err != nil {
		// Fall back to the stale token: the usage call's 401 message is more
		// actionable than a refresh-transport error.
		return kimiToken{Value: creds.AccessToken}, nil
	}
	return kimiToken{Value: refreshed, Warn: warn}, nil
}

func readKimiCredentialsFile(path string) (kimiCredentials, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return kimiCredentials{}, nil, errAgentNotInstalled
		}
		return kimiCredentials{}, nil, err
	}
	creds, err := parseKimiCredentials(data)
	if err != nil {
		return kimiCredentials{}, nil, err
	}
	return creds, data, nil
}

func parseKimiCredentials(data []byte) (kimiCredentials, error) {
	var creds kimiCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return kimiCredentials{}, fmt.Errorf("parse Kimi credentials: %w", err)
	}
	if creds.AccessToken == "" {
		return kimiCredentials{}, errAgentNotInstalled
	}
	return creds, nil
}

func readKimiTokenFromBytes(data []byte) (string, error) {
	creds, err := parseKimiCredentials(data)
	if err != nil {
		return "", err
	}
	return creds.AccessToken, nil
}

// kimiTokenExpired reports whether the stored access token is past (or within
// kimiRefreshSkew of) its expiry. Credentials with no expiry are taken as live.
func kimiTokenExpired(creds kimiCredentials, now time.Time) bool {
	if creds.ExpiresAt <= 0 {
		return false
	}
	return !now.Add(kimiRefreshSkew).Before(time.Unix(creds.ExpiresAt, 0))
}

// refreshKimiToken runs Kimi's refresh_token grant and persists the rotated
// credentials back into path, preserving every field already in the file. The
// refresh token rotates on every grant, so not writing it back would leave the
// CLI holding an invalidated one.
// It returns the new access token and, when the rotated credentials could not
// be persisted, a warning for the report: the usage call still succeeds, but the
// slot now holds a refresh token the server has superseded, so the next refresh
// — lazyagent's or the CLI's — may force a fresh `kimi login`.
func refreshKimiToken(ctx context.Context, env kimi.Environment, creds kimiCredentials, path string, original []byte) (token, warn string, err error) {
	if creds.RefreshToken == "" {
		return "", "", errors.New("no refresh token in Kimi credentials")
	}
	form := url.Values{
		"client_id":     {kimiOAuthClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds.RefreshToken},
	}
	endpoint := env.OAuthHost + "/api/oauth/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent())

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("refresh Kimi OAuth token: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("refresh Kimi OAuth token: %s — %s", resp.Status, snippet(body, 200))
	}

	var grant struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &grant); err != nil {
		return "", "", fmt.Errorf("parse Kimi refresh response: %w", err)
	}
	if grant.AccessToken == "" {
		return "", "", errors.New("Kimi refresh response carried no access token")
	}
	if err := writeKimiCredentials(path, original, grant.AccessToken, grant.RefreshToken, grant.ExpiresIn, grant.Scope, grant.TokenType); err != nil {
		return grant.AccessToken, fmt.Sprintf("Warning: refreshed Kimi credentials could not be written to %s (%v). %s now holds a superseded refresh token; run `kimi login` if Kimi Code stops working.", path, err, filepath.Base(path)), nil
	}
	return grant.AccessToken, "", nil
}

// writeKimiCredentials rewrites the credential slot the way Kimi's own file
// storage does: merge into the existing document, temp file, rename, mode 0600.
func writeKimiCredentials(path string, original []byte, accessToken, refreshToken string, expiresIn int64, scope, tokenType string) error {
	doc := map[string]any{}
	if err := json.Unmarshal(original, &doc); err != nil {
		doc = map[string]any{}
	}
	doc["access_token"] = accessToken
	if refreshToken != "" {
		doc["refresh_token"] = refreshToken
	}
	if expiresIn > 0 {
		doc["expires_in"] = expiresIn
		doc["expires_at"] = time.Now().Add(time.Duration(expiresIn) * time.Second).Unix()
	}
	if scope != "" {
		doc["scope"] = scope
	}
	if tokenType != "" {
		doc["token_type"] = tokenType
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

type kimiUsageResponse struct {
	Usage      *kimiUsageDetail `json:"usage"`
	Limits     []kimiLimit      `json:"limits"`
	TotalQuota *kimiUsageDetail `json:"totalQuota"`
	Parallel   *kimiParallel    `json:"parallel"`
}

type kimiLimit struct {
	Name   string           `json:"name"`
	Title  string           `json:"title"`
	Scope  string           `json:"scope"`
	Window kimiLimitWindow  `json:"window"`
	Detail *kimiUsageDetail `json:"detail"`
	kimiUsageDetail
}

type kimiLimitWindow struct {
	Duration int    `json:"duration"`
	TimeUnit string `json:"timeUnit"`
}

type kimiParallel struct {
	Limit json.RawMessage `json:"limit"`
}

type kimiUsageDetail struct {
	Name           string          `json:"name"`
	Title          string          `json:"title"`
	Limit          json.RawMessage `json:"limit"`
	Used           json.RawMessage `json:"used"`
	Remaining      json.RawMessage `json:"remaining"`
	ResetTimeCamel string          `json:"resetTime"`
	ResetTimeSnake string          `json:"reset_time"`
	ResetAtCamel   string          `json:"resetAt"`
	ResetAtSnake   string          `json:"reset_at"`
	ResetInCamel   json.RawMessage `json:"resetIn"`
	ResetInSnake   json.RawMessage `json:"reset_in"`
	TTL            json.RawMessage `json:"ttl"`
}

func fetchKimiReport(ctx context.Context) (Report, error) {
	env := kimi.ResolveEnvironment()
	token, err := readKimiToken(ctx, env)
	if err != nil {
		if errors.Is(err, errAgentNotInstalled) {
			return Report{}, err
		}
		return Report{}, fmt.Errorf("read Kimi OAuth token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, kimiUsageURL(env), nil)
	if err != nil {
		return Report{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token.Value)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent())

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Report{}, fmt.Errorf("call Kimi usage endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return Report{}, fmt.Errorf("Kimi OAuth token rejected (401). Run `kimi login` or open Kimi Code CLI again, or set KIMI_CODE_OAUTH_TOKEN")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return Report{}, fmt.Errorf("Kimi usage endpoint rate-limited (429). Try again in a minute")
	}
	if resp.StatusCode == http.StatusNotFound {
		return Report{}, fmt.Errorf("Kimi usage endpoint not available (404). Try Kimi for Coding")
	}
	if resp.StatusCode != http.StatusOK {
		return Report{}, fmt.Errorf("Kimi usage endpoint: %s — %s", resp.Status, snippet(body, 200))
	}

	usage, err := parseKimiUsage(body)
	if err != nil {
		return Report{}, err
	}
	report := kimiUsageToReport(usage)
	if token.Warn != "" {
		report.Note = token.Warn + "\n" + report.Note
	}
	if len(report.Windows) == 0 {
		return Report{}, fmt.Errorf("Kimi usage endpoint returned no usable windows (response: %s)", snippet(body, 200))
	}
	return report, nil
}

// kimiUsageURL targets the deployment the resolved credentials belong to:
// api.kimi.ai for a global login, api.kimi.com for mainland China.
func kimiUsageURL(env kimi.Environment) string {
	return strings.TrimRight(env.BaseURL, "/") + "/usages"
}

func parseKimiUsage(data []byte) (*kimiUsageResponse, error) {
	var resp kimiUsageResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse Kimi usage response: %w", err)
	}
	return &resp, nil
}

func kimiUsageToReport(resp *kimiUsageResponse) Report {
	report := Report{
		Provider: "Kimi Code",
		Source:   kimiSource(resp),
		Note:     "Note: reads /coding/v1/usages, the endpoint used by Kimi Code CLI's /status command. May break or be revoked by Moonshot AI without notice.",
	}
	if resp == nil {
		return report
	}
	if resp.Usage != nil {
		if w, ok := kimiDetailToWindow("weekly", 7*24*60, *resp.Usage); ok {
			report.Windows = append(report.Windows, w)
		}
	}
	for i, limit := range resp.Limits {
		detail := limit.kimiUsageDetail
		if limit.Detail != nil {
			detail = *limit.Detail
		}
		minutes := kimiWindowMinutes(limit.Window)
		label := kimiLimitLabel(limit, detail, minutes, i)
		if w, ok := kimiDetailToWindow(label, minutes, detail); ok {
			report.Windows = append(report.Windows, w)
		}
	}
	return report
}

func kimiDetailToWindow(label string, windowMinutes int, detail kimiUsageDetail) (Window, bool) {
	used, limit, ok := kimiUsedAndLimit(detail)
	if !ok || limit <= 0 {
		return Window{}, false
	}
	reset := kimiResetTime(detail)
	usedPercent := 100 * float64(used) / float64(limit)
	return Window{
		Label:         label,
		WindowMinutes: windowMinutes,
		UsedPercent:   usedPercent,
		ResetsAt:      reset,
	}, true
}

func kimiUsedAndLimit(detail kimiUsageDetail) (used, limit int, ok bool) {
	limit, ok = kimiInt(detail.Limit)
	if !ok {
		return 0, 0, false
	}
	if used, ok = kimiInt(detail.Used); ok {
		return used, limit, true
	}
	if remaining, ok := kimiInt(detail.Remaining); ok {
		return limit - remaining, limit, true
	}
	return 0, limit, true
}

func kimiResetTime(detail kimiUsageDetail) time.Time {
	for _, s := range []string{detail.ResetTimeCamel, detail.ResetTimeSnake, detail.ResetAtCamel, detail.ResetAtSnake} {
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
	}
	for _, raw := range []json.RawMessage{detail.ResetInCamel, detail.ResetInSnake, detail.TTL} {
		if seconds, ok := kimiInt(raw); ok && seconds > 0 {
			return time.Now().Add(time.Duration(seconds) * time.Second)
		}
	}
	return time.Time{}
}

func kimiWindowMinutes(w kimiLimitWindow) int {
	if w.Duration <= 0 {
		return 0
	}
	unit := strings.ToUpper(w.TimeUnit)
	switch {
	case strings.Contains(unit, "MINUTE"):
		return w.Duration
	case strings.Contains(unit, "HOUR"):
		return w.Duration * 60
	case strings.Contains(unit, "DAY"):
		return w.Duration * 24 * 60
	case strings.Contains(unit, "SECOND"):
		mins := w.Duration / 60
		if mins == 0 {
			return 1
		}
		return mins
	default:
		return w.Duration
	}
}

func kimiLimitLabel(limit kimiLimit, detail kimiUsageDetail, minutes int, idx int) string {
	for _, val := range []string{limit.Name, limit.Title, limit.Scope, detail.Name, detail.Title} {
		if val != "" {
			return val
		}
	}
	switch minutes {
	case 300:
		return "5-hour"
	case 7 * 24 * 60:
		return "weekly"
	}
	if minutes > 0 && minutes%1440 == 0 {
		return fmt.Sprintf("%d-day", minutes/1440)
	}
	if minutes > 0 && minutes%60 == 0 {
		return fmt.Sprintf("%d-hour", minutes/60)
	}
	if minutes > 0 {
		return fmt.Sprintf("%d-minute", minutes)
	}
	return fmt.Sprintf("limit #%d", idx+1)
}

func kimiSource(resp *kimiUsageResponse) string {
	if resp == nil {
		return ""
	}
	var parts []string
	if resp.Usage != nil {
		if used, limit, ok := kimiUsedAndLimit(*resp.Usage); ok {
			parts = append(parts, fmt.Sprintf("%d of %d weekly quota used", used, limit))
		}
	}
	if resp.TotalQuota != nil {
		if used, limit, ok := kimiUsedAndLimit(*resp.TotalQuota); ok {
			parts = append(parts, fmt.Sprintf("%d of %d total quota used", used, limit))
		}
	}
	if resp.Parallel != nil {
		if limit, ok := kimiInt(resp.Parallel.Limit); ok {
			parts = append(parts, fmt.Sprintf("parallel limit: %d", limit))
		}
	}
	if len(parts) == 0 {
		return "Source: Kimi Code /usages"
	}
	return "Source: " + strings.Join(parts, "; ")
}

func kimiInt(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, true
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int(f), true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		s = strings.TrimSpace(s)
		if s == "" {
			return 0, false
		}
		n, err := strconv.Atoi(s)
		return n, err == nil
	}
	return 0, false
}
