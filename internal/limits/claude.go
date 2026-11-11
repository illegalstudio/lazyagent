package limits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/illegalstudio/lazyagent/internal/version"
)

// claudeUsageURL is the (undocumented) endpoint Claude Code itself calls for
// /status and /usage. at_wall=1 is the variant Claude Code reads at a usage
// limit: besides the meters and extra usage it carries the reset programs
// (cedar_ember grants and the weekly juniper_tide 5-hour reset). It is a plain
// read; redeeming a reset is a separate POST lazyagent never sends.
const (
	claudeUsageURL  = "https://api.anthropic.com/api/oauth/usage?at_wall=1"
	claudeBetaValue = "oauth-2025-04-20"
)

// claudeMinCLIVersion is the oldest Claude Code version known to receive the
// reset programs. The server answers older versions with
// ineligible_reason "cli_version", so the User-Agent never claims less.
const claudeMinCLIVersion = "2.1.322"

// userAgent returns the User-Agent we send to every provider except Claude.
// Honest identification (we don't impersonate the vendor CLIs) so any
// rate-limit / abuse review can attribute traffic to lazyagent.
func userAgent() string {
	return "lazyagent/" + version.Version + " (+https://github.com/illegalstudio/lazyagent)"
}

// claudeUserAgent identifies the request as Claude Code. Anthropic only
// reports reset grants to the claude_code_cli surface, which it derives from a
// User-Agent that starts with "claude-cli/" and names a recent version; with
// the lazyagent User-Agent the reset programs come back as ineligible.
func claudeUserAgent() string {
	return "claude-cli/" + claudeCLIVersion() + " (external, cli)"
}

// claudeCLIVersion returns the installed Claude Code version as recorded in
// ~/.claude.json (lastReleaseNotesSeen), or claudeMinCLIVersion when that is
// missing or older.
func claudeCLIVersion() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return claudeMinCLIVersion
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return claudeMinCLIVersion
	}
	var cfg struct {
		LastReleaseNotesSeen string `json:"lastReleaseNotesSeen"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return claudeMinCLIVersion
	}
	if compareVersions(cfg.LastReleaseNotesSeen, claudeMinCLIVersion) > 0 {
		return cfg.LastReleaseNotesSeen
	}
	return claudeMinCLIVersion
}

// compareVersions compares dotted numeric versions ("2.1.322"). A version that
// does not parse compares lower than any version that does.
func compareVersions(a, b string) int {
	pa, okA := parseVersion(a)
	pb, okB := parseVersion(b)
	switch {
	case !okA && !okB:
		return 0
	case !okA:
		return -1
	case !okB:
		return 1
	}
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func parseVersion(v string) ([]int, bool) {
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// claudeUsageResponse mirrors the parts of /api/oauth/usage lazyagent renders.
// limits[] is the server's list of meters (Claude Code renders it verbatim and
// classifies rows on kind); five_hour / seven_day are the legacy fields, used
// only when the server sends no rows.
type claudeUsageResponse struct {
	FiveHour           *claudeWindow       `json:"five_hour"`
	SevenDay           *claudeWindow       `json:"seven_day"`
	Limits             []claudeLimit       `json:"limits"`
	WeeklyScopedShares []claudeScopedShare `json:"weekly_scoped_shares"`
	ExtraUsage         *claudeExtraUsage   `json:"extra_usage"`
	CedarEmber         *claudeCedarEmber   `json:"cedar_ember"`
	JuniperTide        *claudeJuniperTide  `json:"juniper_tide"`
}

type claudeWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

// claudeLimit is one meter row: kind is "session", "weekly_all" or
// "weekly_scoped"; group is "session" or "weekly"; percent is 0-100.
type claudeLimit struct {
	Kind     string            `json:"kind"`
	Group    string            `json:"group"`
	Percent  float64           `json:"percent"`
	ResetsAt string            `json:"resets_at"`
	Scope    *claudeLimitScope `json:"scope"`
}

type claudeLimitScope struct {
	Model   *claudeDisplayName `json:"model"`
	Surface json.RawMessage    `json:"surface"`
}

type claudeDisplayName struct {
	DisplayName string `json:"display_name"`
}

// claudeScopedShare says how much of the plan-wide weekly limit a scoped meter
// (limits[limit_index]) has consumed, and how much it may consume at most.
type claudeScopedShare struct {
	LimitIndex               int      `json:"limit_index"`
	UsedPercentOfWeekly      *float64 `json:"used_percent_of_weekly"`
	AllowancePercentOfWeekly *float64 `json:"allowance_percent_of_weekly"`
}

// claudeExtraUsage is the overage spend for the billing period. Amounts are
// in minor units of currency (decimal_places digits).
type claudeExtraUsage struct {
	IsEnabled         bool     `json:"is_enabled"`
	MonthlyLimit      *float64 `json:"monthly_limit"`
	UsedCredits       *float64 `json:"used_credits"`
	Currency          string   `json:"currency"`
	DecimalPlaces     *int     `json:"decimal_places"`
	DisabledReason    string   `json:"disabled_reason"`
	SpendLimitReached bool     `json:"spend_limit_reached"`
}

// claudeCedarEmber is the reset-grant program (for example the Fable resets
// granted at a model launch).
type claudeCedarEmber struct {
	Eligible bool               `json:"eligible"`
	Grants   []claudeResetGrant `json:"grants"`
}

type claudeResetGrant struct {
	Label             string `json:"label"`
	ResetsTotal       int    `json:"resets_total"`
	ResetsLeft        int    `json:"resets_left"`
	EndsAt            string `json:"ends_at"`
	Paused            bool   `json:"paused"`
	UsableNow         bool   `json:"usable_now"`
	UseRequiresLimit  bool   `json:"use_requires_limit"`
	UnavailableReason string `json:"unavailable_reason"`
}

// claudeJuniperTide is the weekly "reset your 5-hour limit now" program.
type claudeJuniperTide struct {
	Eligible         bool   `json:"eligible"`
	IneligibleReason string `json:"ineligible_reason"`
	InExperiment     bool   `json:"in_experiment"`
	Available        bool   `json:"available"`
	NextAvailableAt  string `json:"next_available_at"`
	ResetsPerWeek    int    `json:"resets_per_week"`
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
	req.Header.Set("User-Agent", claudeUserAgent())

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
	r := claudeUsageToReport(&usage, time.Now())
	if len(r.Windows) == 0 {
		return Report{}, fmt.Errorf("Claude usage endpoint returned no usage meters (response: %s)", snippet(body, 200))
	}
	return r, nil
}

// claudeUsageToReport maps one usage response to a report. Pure, for tests.
func claudeUsageToReport(usage *claudeUsageResponse, now time.Time) Report {
	r := Report{
		Provider: "Claude Code",
		Note:     "Note: reads /api/oauth/usage, an undocumented Claude endpoint, identifying as Claude Code so Anthropic reports reset grants. May break or be revoked by Anthropic without notice.",
	}
	r.Windows = claudeLimitsToWindows(usage)
	if len(r.Windows) == 0 {
		if usage.FiveHour != nil {
			r.Windows = append(r.Windows, claudeWindowToWindow("5-hour", 300, *usage.FiveHour))
		}
		if usage.SevenDay != nil {
			r.Windows = append(r.Windows, claudeWindowToWindow("7-day", 7*24*60, *usage.SevenDay))
		}
	}
	r.Resets = claudeResets(usage, now)
	if c, ok := claudeExtraUsageCredit(usage.ExtraUsage); ok {
		r.Credits = append(r.Credits, c)
	}
	return r
}

// claudeLimitsToWindows maps the server's meter rows. The kind decides the
// window; a scoped row keeps its model (or surface) name as the window scope.
func claudeLimitsToWindows(usage *claudeUsageResponse) []Window {
	var out []Window
	for i, l := range usage.Limits {
		label, minutes := claudeLimitWindow(l)
		w := Window{
			Label:         label,
			WindowMinutes: minutes,
			UsedPercent:   l.Percent,
			Scope:         claudeScopeName(l.Scope),
		}
		w.ResetsAt, _ = time.Parse(time.RFC3339Nano, l.ResetsAt)
		for _, share := range usage.WeeklyScopedShares {
			if share.LimitIndex == i && w.Scope != "" {
				w.Detail = claudeShareDetail(share)
			}
		}
		out = append(out, w)
	}
	return out
}

func claudeLimitWindow(l claudeLimit) (string, int) {
	switch l.Kind {
	case "session":
		return "5-hour", 5 * 60
	case "weekly_all", "weekly_scoped":
		return "7-day", 7 * 24 * 60
	}
	switch l.Group {
	case "session":
		return "5-hour", 5 * 60
	case "weekly":
		return "7-day", 7 * 24 * 60
	}
	if l.Kind != "" {
		return humanReason(l.Kind), 0
	}
	return "window", 0
}

func claudeScopeName(s *claudeLimitScope) string {
	if s == nil {
		return ""
	}
	if s.Model != nil && s.Model.DisplayName != "" {
		return s.Model.DisplayName
	}
	if len(s.Surface) > 0 && string(s.Surface) != "null" {
		var named claudeDisplayName
		if json.Unmarshal(s.Surface, &named) == nil && named.DisplayName != "" {
			return named.DisplayName
		}
		var plain string
		if json.Unmarshal(s.Surface, &plain) == nil && plain != "" {
			return humanReason(plain)
		}
	}
	return ""
}

func claudeShareDetail(s claudeScopedShare) string {
	switch {
	case s.UsedPercentOfWeekly != nil && s.AllowancePercentOfWeekly != nil:
		return fmt.Sprintf("%.0f%% of the weekly limit used, capped at %.0f%%", *s.UsedPercentOfWeekly, *s.AllowancePercentOfWeekly)
	case s.UsedPercentOfWeekly != nil:
		return fmt.Sprintf("%.0f%% of the weekly limit used", *s.UsedPercentOfWeekly)
	case s.AllowancePercentOfWeekly != nil:
		return fmt.Sprintf("capped at %.0f%% of the weekly limit", *s.AllowancePercentOfWeekly)
	}
	return ""
}

// claudeResets lists the reset grants that still have resets left, then the
// weekly 5-hour reset when the account takes part in that program.
func claudeResets(usage *claudeUsageResponse, now time.Time) []Reset {
	var out []Reset
	if ce := usage.CedarEmber; ce != nil {
		for _, g := range ce.Grants {
			if g.ResetsLeft <= 0 {
				continue
			}
			r := Reset{
				Label:  g.Label,
				Left:   g.ResetsLeft,
				Total:  g.ResetsTotal,
				Usable: g.UsableNow && !g.Paused,
			}
			if g.Label == "" {
				r.Label = "Limit reset"
			}
			r.ExpiresAt, _ = time.Parse(time.RFC3339Nano, g.EndsAt)
			switch {
			case g.Paused:
				r.Status = "paused"
			case g.UnavailableReason != "":
				r.Status = humanReason(g.UnavailableReason)
			case !g.UsableNow && g.UseRequiresLimit:
				r.Status = "only at a usage limit"
			}
			out = append(out, r)
		}
	}
	if jt := usage.JuniperTide; jt != nil && (jt.Eligible || jt.InExperiment) {
		r := Reset{
			Label:  "5-hour limit reset",
			Total:  jt.ResetsPerWeek,
			Usable: jt.Available,
		}
		held := max(jt.ResetsPerWeek, 1)
		next, nextErr := time.Parse(time.RFC3339Nano, jt.NextAvailableAt)
		switch {
		case jt.Available:
			r.Left = held
		case nextErr == nil:
			// Spent this week: it comes back at next_available_at.
			r.NextAt = next
		case jt.IneligibleReason == "not_at_wall":
			// Held, but redeemable only once a limit is hit.
			r.Left = held
			r.Status = "only at a usage limit"
		case jt.IneligibleReason != "":
			r.Status = humanReason(jt.IneligibleReason)
		}
		out = append(out, r)
	}
	return out
}

// claudeExtraUsageCredit maps extra usage (overage) to a credit line. Amounts
// arrive in minor units of the currency.
func claudeExtraUsageCredit(x *claudeExtraUsage) (Credit, bool) {
	if x == nil {
		return Credit{}, false
	}
	places := 2
	if x.DecimalPlaces != nil {
		places = *x.DecimalPlaces
	}
	scale := math.Pow10(places)
	c := Credit{Label: "Extra usage", Unit: x.Currency, Enabled: x.IsEnabled}
	if x.UsedCredits != nil {
		c.Used = floatPtr(*x.UsedCredits / scale)
	}
	if x.MonthlyLimit != nil && *x.MonthlyLimit > 0 {
		c.Limit = floatPtr(*x.MonthlyLimit / scale)
	}
	switch {
	case x.SpendLimitReached:
		c.Status = "spend limit reached"
	case !x.IsEnabled && x.DisabledReason != "":
		c.Status = claudeDisabledReason(x.DisabledReason)
	}
	return c, true
}

// claudeDisabledReason turns extra_usage.disabled_reason into a short phrase.
// "org_level_disabled_until" carries no date anywhere in the response, so the
// dangling "until" is dropped rather than shown.
func claudeDisabledReason(reason string) string {
	if strings.HasPrefix(reason, "org_level_disabled") {
		return "set by your organization"
	}
	return humanReason(reason)
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
