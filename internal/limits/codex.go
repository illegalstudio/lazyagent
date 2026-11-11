package limits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// defaultCodexUsageURL is the endpoint the Codex CLI's TUI polls (~every 60s via
// ChatWidget::prefetch_rate_limits) to show live rate-limit usage. It's the same
// data the official client displays, so it's always current — unlike the session
// rollouts, which only update when a turn completes.
const defaultCodexUsageURL = "https://chatgpt.com/backend-api/wham/usage"

// codexAuth is the subset of ~/.codex/auth.json we need: the ChatGPT OAuth
// access token and the account id the usage endpoint expects as a header.
type codexAuth struct {
	Tokens struct {
		AccessToken string `json:"access_token"`
		AccountID   string `json:"account_id"`
	} `json:"tokens"`
}

// readCodexAuth returns the bearer token and account id in this priority order:
//  1. CODEX_OAUTH_TOKEN env var (override for CI / debugging; CODEX_ACCOUNT_ID optional)
//  2. ~/.codex/auth.json (where the Codex CLI persists its ChatGPT login)
//
// Both "not installed" and "not logged in" surface as errAgentNotInstalled so the
// dispatcher can silently skip Codex in --agent all mode.
func readCodexAuth() (token, accountID string, err error) {
	if v := os.Getenv("CODEX_OAUTH_TOKEN"); v != "" {
		return v, os.Getenv("CODEX_ACCOUNT_ID"), nil
	}
	path := codexAuthPath()
	if path == "" {
		return "", "", errAgentNotInstalled
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", errAgentNotInstalled
		}
		return "", "", err
	}
	return readCodexAuthFromBytes(data)
}

func readCodexAuthFromBytes(data []byte) (token, accountID string, err error) {
	var a codexAuth
	if err := json.Unmarshal(data, &a); err != nil {
		return "", "", fmt.Errorf("parse Codex auth.json: %w", err)
	}
	if a.Tokens.AccessToken == "" {
		return "", "", errAgentNotInstalled
	}
	return a.Tokens.AccessToken, a.Tokens.AccountID, nil
}

func codexAuthPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "auth.json")
}

// codexUsageResponse is the subset of GET /backend-api/wham/usage we render.
type codexUsageResponse struct {
	PlanType             string                 `json:"plan_type"`
	RateLimit            *codexUsageLimits      `json:"rate_limit"`
	CodeReviewRateLimit  *codexUsageLimits      `json:"code_review_rate_limit"`
	AdditionalRateLimits []codexAdditionalLimit `json:"additional_rate_limits"`
	Credits              *codexCredits          `json:"credits"`
	SpendControl         *codexSpendControl     `json:"spend_control"`
	ResetCredits         *codexResetCredits     `json:"rate_limit_reset_credits"`
}

type codexUsageLimits struct {
	Primary   *codexUsageWindow `json:"primary_window"`
	Secondary *codexUsageWindow `json:"secondary_window"`
}

type codexUsageWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	ResetAt            int64   `json:"reset_at"` // unix seconds
}

// codexAdditionalLimit is a separately metered feature or model, with its own
// windows (for example a model with its own allowance).
type codexAdditionalLimit struct {
	LimitName      string            `json:"limit_name"`
	MeteredFeature string            `json:"metered_feature"`
	RateLimit      *codexUsageLimits `json:"rate_limit"`
}

// codexCredits is the pay-as-you-go credit balance. balance is a decimal
// string; the approx_* pairs estimate how many messages it buys.
type codexCredits struct {
	HasCredits          bool      `json:"has_credits"`
	Unlimited           bool      `json:"unlimited"`
	OverageLimitReached bool      `json:"overage_limit_reached"`
	Balance             *string   `json:"balance"`
	ApproxLocalMessages []float64 `json:"approx_local_messages"`
	ApproxCloudMessages []float64 `json:"approx_cloud_messages"`
}

type codexSpendControl struct {
	Reached         bool            `json:"reached"`
	IndividualLimit json.RawMessage `json:"individual_limit"`
}

type codexResetCredits struct {
	AvailableCount int `json:"available_count"`
}

// codexResetCreditDetails is GET /backend-api/wham/rate-limit-reset-credits,
// which the Codex CLI reads to list each banked reset. Its sibling
// .../consume redeems one; lazyagent never calls it.
type codexResetCreditDetails struct {
	Credits []codexResetCredit `json:"credits"`
}

type codexResetCredit struct {
	Status            string `json:"status"`
	Title             string `json:"title"`
	ExpiresAt         string `json:"expires_at"`
	IsSupportedByPlan *bool  `json:"is_supported_by_plan"`
}

func fetchCodexReport(ctx context.Context) (Report, error) {
	token, accountID, err := readCodexAuth()
	if err != nil {
		if errors.Is(err, errAgentNotInstalled) {
			return Report{}, err
		}
		return Report{}, fmt.Errorf("read Codex auth: %w", err)
	}

	status, body, err := codexGET(ctx, codexUsageURL(), token, accountID)
	if err != nil {
		return Report{}, fmt.Errorf("call Codex usage endpoint: %w", err)
	}
	switch status {
	case http.StatusOK:
		// fall through to parse
	case http.StatusUnauthorized, http.StatusForbidden:
		return Report{}, fmt.Errorf("Codex OAuth token rejected (%d). It may have expired — open the Codex CLI again to refresh your login", status)
	case http.StatusTooManyRequests:
		return Report{}, fmt.Errorf("Codex usage endpoint rate-limited (429). Try again in a minute")
	default:
		return Report{}, fmt.Errorf("Codex usage endpoint: %d %s — %s", status, http.StatusText(status), snippet(body, 200))
	}

	usage, err := parseCodexUsage(body)
	if err != nil {
		return Report{}, err
	}
	report := codexUsageToReport(usage)
	if len(report.Windows) == 0 {
		return Report{}, fmt.Errorf("Codex usage endpoint returned no usable windows (response: %s)", snippet(body, 200))
	}
	if usage.ResetCredits != nil && usage.ResetCredits.AvailableCount > 0 {
		report.Resets = codexResets(ctx, usage.ResetCredits.AvailableCount, token, accountID)
	}
	return report, nil
}

// codexResets lists the banked resets with their expiry. The usage response
// only carries a count, so the detail endpoint is read when the count is
// positive; if that read fails the count alone is reported.
func codexResets(ctx context.Context, available int, token, accountID string) []Reset {
	fallback := []Reset{{Label: "Rate-limit reset", Left: available}}
	status, body, err := codexGET(ctx, codexResetCreditsURL(), token, accountID)
	if err != nil || status != http.StatusOK {
		return fallback
	}
	var details codexResetCreditDetails
	if json.Unmarshal(body, &details) != nil {
		return fallback
	}
	resets := codexResetDetailsToResets(&details)
	if len(resets) == 0 {
		return fallback
	}
	return resets
}

// codexResetDetailsToResets keeps the available credits, one Reset each,
// soonest expiry first. Pure, for tests.
func codexResetDetailsToResets(d *codexResetCreditDetails) []Reset {
	var out []Reset
	for _, c := range d.Credits {
		if c.Status != "available" {
			continue
		}
		r := Reset{Label: c.Title, Left: 1, Usable: true}
		if r.Label == "" {
			r.Label = "Rate-limit reset"
		}
		if c.IsSupportedByPlan != nil && !*c.IsSupportedByPlan {
			r.Usable = false
			r.Status = "not supported by the current plan"
		}
		r.ExpiresAt, _ = time.Parse(time.RFC3339Nano, c.ExpiresAt)
		out = append(out, r)
	}
	slices.SortStableFunc(out, func(a, b Reset) int {
		switch {
		case a.ExpiresAt.IsZero() && b.ExpiresAt.IsZero():
			return 0
		case a.ExpiresAt.IsZero():
			return 1
		case b.ExpiresAt.IsZero():
			return -1
		}
		return a.ExpiresAt.Compare(b.ExpiresAt)
	})
	return out
}

func codexGET(ctx context.Context, url, token, accountID string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if accountID != "" {
		req.Header.Set("chatgpt-account-id", accountID)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent())
	req.Header.Set("originator", "codex_cli_rs")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body, nil
}

// codexResetCreditsURL sits next to the usage URL, so a CODEX_USAGE_URL
// override (tests, proxies) moves both and the token never leaves the
// configured host.
// Only the path changes: a query string or fragment on the override is kept.
func codexResetCreditsURL() string {
	raw := codexUsageURL()
	u, err := url.Parse(raw)
	if err != nil {
		return strings.TrimSuffix(strings.TrimRight(raw, "/"), "/usage") + "/rate-limit-reset-credits"
	}
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/usage") + "/rate-limit-reset-credits"
	u.RawPath = ""
	return u.String()
}

func codexUsageURL() string {
	if v := os.Getenv("CODEX_USAGE_URL"); v != "" {
		return v
	}
	return defaultCodexUsageURL
}

func parseCodexUsage(data []byte) (*codexUsageResponse, error) {
	var resp codexUsageResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse Codex usage response: %w", err)
	}
	return &resp, nil
}

func codexUsageToReport(resp *codexUsageResponse) Report {
	report := Report{
		Provider: "Codex",
		Source:   codexSource(resp),
		Note:     "Note: reads /backend-api/wham/usage, the endpoint Codex CLI polls for its rate-limit display. May break or be revoked by OpenAI without notice.",
	}
	if resp == nil {
		return report
	}
	report.Windows = append(report.Windows, codexLimitsToWindows(resp.RateLimit, "")...)
	for _, extra := range resp.AdditionalRateLimits {
		scope := extra.LimitName
		if scope == "" {
			scope = humanReason(extra.MeteredFeature)
		}
		report.Windows = append(report.Windows, codexLimitsToWindows(extra.RateLimit, scope)...)
	}
	report.Windows = append(report.Windows, codexLimitsToWindows(resp.CodeReviewRateLimit, "Code review")...)
	if c, ok := codexCreditsToCredit(resp.Credits, resp.SpendControl); ok {
		report.Credits = append(report.Credits, c)
	}
	return report
}

func codexLimitsToWindows(l *codexUsageLimits, scope string) []Window {
	if l == nil {
		return nil
	}
	var out []Window
	for _, cw := range []*codexUsageWindow{l.Primary, l.Secondary} {
		if w, ok := codexUsageWindowToWindow(cw); ok {
			w.Scope = scope
			out = append(out, w)
		}
	}
	return out
}

// codexCreditsToCredit maps the credit balance. Accounts without credits
// (has_credits false and not unlimited) produce no line.
func codexCreditsToCredit(c *codexCredits, spend *codexSpendControl) (Credit, bool) {
	if c == nil || (!c.HasCredits && !c.Unlimited) {
		return Credit{}, false
	}
	credit := Credit{Label: "Credits", Unit: "credits", Enabled: true}
	var notes []string
	if c.Unlimited {
		notes = append(notes, "unlimited")
	} else if c.Balance != nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(*c.Balance), 64); err == nil {
			credit.Balance = floatPtr(v)
		}
	}
	if spend != nil {
		if limit, ok := jsonNumber(spend.IndividualLimit); ok {
			credit.Limit = floatPtr(limit)
		}
	}
	if s := codexApproxText(c.ApproxLocalMessages, "local"); s != "" {
		notes = append(notes, s)
	}
	if s := codexApproxText(c.ApproxCloudMessages, "cloud"); s != "" {
		notes = append(notes, s)
	}
	if c.OverageLimitReached || (spend != nil && spend.Reached) {
		notes = append(notes, "spend limit reached")
	}
	credit.Status = strings.Join(notes, " · ")
	return credit, true
}

// codexApproxText renders the [low, high] message estimate the Codex CLI
// shows next to the balance: "~7,079-36,810 local messages".
func codexApproxText(r []float64, kind string) string {
	switch len(r) {
	case 1:
		return fmt.Sprintf("~%s %s messages", groupInt(r[0]), kind)
	case 2:
		return fmt.Sprintf("~%s-%s %s messages", groupInt(r[0]), groupInt(r[1]), kind)
	}
	return ""
}

func groupInt(v float64) string {
	s := groupThousands(math.Round(v))
	return strings.TrimSuffix(s, ".00")
}

// jsonNumber reads a number that may be encoded as a JSON number or string.
func jsonNumber(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		return n, true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return v, true
		}
	}
	return 0, false
}

func codexUsageWindowToWindow(w *codexUsageWindow) (Window, bool) {
	if w == nil || w.LimitWindowSeconds <= 0 {
		return Window{}, false
	}
	minutes := int(w.LimitWindowSeconds / 60)
	var reset time.Time
	switch {
	case w.ResetAt > 0:
		reset = time.Unix(w.ResetAt, 0)
	case w.ResetAfterSeconds > 0:
		reset = time.Now().Add(time.Duration(w.ResetAfterSeconds) * time.Second)
	}
	return Window{
		Label:         codexWindowLabel(minutes),
		WindowMinutes: minutes,
		UsedPercent:   w.UsedPercent,
		ResetsAt:      reset,
	}, true
}

// codexWindowLabel names a window from its length so the renderer's window
// matchers (5-hour, 7-day/weekly) line up, while staying readable if OpenAI ever
// changes the window sizes.
func codexWindowLabel(minutes int) string {
	switch minutes {
	case 5 * 60:
		return "5-hour"
	case 7 * 24 * 60:
		return "7-day"
	}
	switch {
	case minutes > 0 && minutes%(24*60) == 0:
		return fmt.Sprintf("%d-day", minutes/(24*60))
	case minutes > 0 && minutes%60 == 0:
		return fmt.Sprintf("%d-hour", minutes/60)
	case minutes > 0:
		return fmt.Sprintf("%d-minute", minutes)
	default:
		return "window"
	}
}

func codexSource(resp *codexUsageResponse) string {
	if resp != nil && resp.PlanType != "" {
		return fmt.Sprintf("Source: Codex (ChatGPT %s) /backend-api/wham/usage", resp.PlanType)
	}
	return "Source: Codex /backend-api/wham/usage"
}
