package limits

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/illegalstudio/lazyagent/internal/cursor"
)

// cursorUsageSummaryURL is the (undocumented) endpoint Cursor's own dashboard
// calls to render its usage headline. See the package doc in run.go for the
// stability caveat.
const cursorUsageSummaryURL = "https://cursor.com/api/usage-summary"

// fetchCursorReports returns standalone Cursor's two metered pools as separate
// reports. Unified subscriptions use the shared xAI credits reported by Grok.
func fetchCursorReports(ctx context.Context) ([]Report, error) {
	token, ok, err := cursor.ReadAuth()
	if err != nil {
		return nil, fmt.Errorf("read Cursor credentials: %w", err)
	}
	if !ok {
		return nil, errAgentNotInstalled
	}

	userID, err := cursorUserIDFromToken(token)
	if err != nil {
		return nil, fmt.Errorf("decode Cursor access token: %w", err)
	}
	cookie := userID + "%3A%3A" + token

	var summary cursorUsageSummary
	if err := cursorGET(ctx, cursorUsageSummaryURL, cookie, &summary); err != nil {
		return nil, err
	}
	return cursorSummaryToReports(&summary)
}

// cursorUserIDFromToken extracts the user id from the JWT access token. Cursor
// builds its session cookie as "<userId>%3A%3A<token>", where userId is the part
// of the JWT `sub` claim after the "auth0|" / "workos|" prefix.
func cursorUserIDFromToken(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("not a JWT (expected 3 dot-separated segments)")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode JWT payload: %w", err)
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("parse JWT payload: %w", err)
	}
	if claims.Sub == "" {
		return "", fmt.Errorf("JWT has no sub claim")
	}
	if i := strings.Index(claims.Sub, "|"); i >= 0 {
		return claims.Sub[i+1:], nil
	}
	return claims.Sub, nil
}

func cursorPlanName(membership string) string {
	m := strings.TrimSpace(membership)
	if m == "" {
		return "Cursor"
	}
	switch strings.ToLower(m) {
	case "pro_plus":
		return "Pro+"
	default:
		return strings.ToUpper(m[:1]) + m[1:]
	}
}

func cursorGET(ctx context.Context, url, cookie string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return cursorDo(req, cookie, out)
}

func cursorDo(req *http.Request, cookie string, out any) error {
	req.Header.Set("Cookie", "WorkosCursorSessionToken="+cookie)
	req.Header.Set("Origin", "https://cursor.com")
	req.Header.Set("Referer", "https://cursor.com/dashboard")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent())

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call Cursor %s: %w", req.URL.Path, err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("Cursor session token rejected (%d). Open Cursor and sign in again", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Cursor %s: %s — %s", req.URL.Path, resp.Status, snippet(data, 200))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parse Cursor %s response: %w (body: %s)", req.URL.Path, err, snippet(data, 200))
	}
	return nil
}

// cursorUsageSummary is the (subset of the) shape returned by
// GET /api/usage-summary on cursor.com — the endpoint Cursor's dashboard uses
// to render its usage headline. Fields we don't consume are omitted.
//
// Cursor meters two disjoint pools against two separate allowances and reports
// each as its own percentage: autoPercentUsed for the Auto/Composer pool and
// apiPercentUsed for the usage-based (API) pool. Note that plan.used/limit are
// NOT the denominators of those percentages, which is why they are not parsed.
type cursorUsageSummary struct {
	BillingCycleStart string           `json:"billingCycleStart"`
	BillingCycleEnd   string           `json:"billingCycleEnd"`
	MembershipType    string           `json:"membershipType"`
	IsUnlimited       bool             `json:"isUnlimited"`
	IndividualUsage   cursorUsageBlock `json:"individualUsage"`
	TeamUsage         cursorUsageBlock `json:"teamUsage"`
}

// cursorUsageBlock is one of the two parallel usage scopes in the response.
// Individual accounts populate individualUsage and leave teamUsage as {};
// team-billed accounts do the reverse.
type cursorUsageBlock struct {
	Plan *cursorPlanUsage `json:"plan"`
}

// AutoPercentUsed and APIPercentUsed are pointers so a renamed or missing
// upstream field is distinguishable from a genuine 0.0%: cursorSummaryToReports
// treats a nil pointer as errAgentUnavailable instead of silently reporting a
// green "0.0% used".
type cursorPlanUsage struct {
	Enabled         bool     `json:"enabled"`
	AutoPercentUsed *float64 `json:"autoPercentUsed"`
	APIPercentUsed  *float64 `json:"apiPercentUsed"`
}

// cursorPlanBlock picks the usage scope that applies to this account: the
// individual plan when enabled, otherwise the team plan. Returns nil when
// neither is usable, which the caller turns into errAgentUnavailable.
func cursorPlanBlock(s *cursorUsageSummary) *cursorPlanUsage {
	if p := s.IndividualUsage.Plan; p != nil && p.Enabled {
		return p
	}
	if p := s.TeamUsage.Plan; p != nil && p.Enabled {
		return p
	}
	return nil
}

// cursorSummaryToReports maps one usage-summary response to the two rows
// lazyagent shows for Cursor. Kept pure so the mapping is unit-testable without
// a server; all network work lives in fetchCursorReports.
func cursorSummaryToReports(s *cursorUsageSummary) ([]Report, error) {
	// Check this account's membership independently of the Grok login. The
	// user may have a standalone Cursor plan on a different account or team.
	if strings.HasPrefix(s.MembershipType, "unified_") {
		return nil, fmt.Errorf("%w: Cursor usage is included in the unified xAI subscription. Run `lazyagent limits --agent grok` to view shared limits", errAgentUnavailable)
	}
	if s.IsUnlimited {
		return nil, fmt.Errorf("%w: Cursor reports this account as unlimited, so there is no budget to pace against", errAgentUnavailable)
	}
	plan := cursorPlanBlock(s)
	if plan == nil {
		return nil, fmt.Errorf("%w: Cursor is signed in but reports no enabled usage plan for this account", errAgentUnavailable)
	}
	if plan.AutoPercentUsed == nil {
		return nil, fmt.Errorf("%w: Cursor's usage-summary response is missing autoPercentUsed", errAgentUnavailable)
	}
	if plan.APIPercentUsed == nil {
		return nil, fmt.Errorf("%w: Cursor's usage-summary response is missing apiPercentUsed", errAgentUnavailable)
	}
	autoPercentUsed := *plan.AutoPercentUsed
	apiPercentUsed := *plan.APIPercentUsed

	start, err := time.Parse(time.RFC3339, s.BillingCycleStart)
	if err != nil {
		return nil, fmt.Errorf("parse Cursor billingCycleStart %q: %w", s.BillingCycleStart, err)
	}
	end, err := time.Parse(time.RFC3339, s.BillingCycleEnd)
	if err != nil {
		return nil, fmt.Errorf("parse Cursor billingCycleEnd %q: %w", s.BillingCycleEnd, err)
	}

	windowMinutes := int(end.Sub(start).Minutes())
	planName := cursorPlanName(s.MembershipType)
	cycle := fmt.Sprintf("billing cycle %s – %s", start.Local().Format("2 Jan"), end.Local().Format("2 Jan"))
	window := func(pct float64) []Window {
		return []Window{{
			Label:         "monthly",
			WindowMinutes: windowMinutes,
			UsedPercent:   pct,
			ResetsAt:      end,
		}}
	}

	return []Report{
		{
			Provider: "Cursor Models",
			Source: fmt.Sprintf("Source: %.1f%% of the plan's included Auto/Composer allowance (%s); %s",
				autoPercentUsed, planName, cycle),
			Windows: window(autoPercentUsed),
			Note:    "Note: reads /api/usage-summary on cursor.com with the Cursor session token from state.vscdb — the Auto/Composer pool's own allowance (autoPercentUsed). Cursor's dashboard shows the combined total instead when Auto is the selected model, so this figure reads lower than the one in its UI. Undocumented; may break or be revoked by Cursor without notice.",
		},
		{
			Provider: "Cursor API",
			Source: fmt.Sprintf("Source: %.1f%% of the plan's included API allowance (%s); %s",
				apiPercentUsed, planName, cycle),
			Windows: window(apiPercentUsed),
			Note:    "Note: reads /api/usage-summary on cursor.com with the Cursor session token from state.vscdb — the usage-based (API) pool only. Undocumented; may break or be revoked by Cursor without notice.",
		},
	}, nil
}
