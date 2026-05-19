package limits

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// grokAuthEntry is one value in the map persisted at ~/.grok/auth.json.
// The map is keyed by the OIDC scope ("https://auth.x.ai::<client-id>") and
// each value carries the bearer JWT under "key". Extra fields are ignored.
type grokAuthEntry struct {
	Key string `json:"key"`
}

// readGrokToken returns the OAuth bearer in this priority order:
//  1. GROK_OAUTH_TOKEN env var (override for CI / debugging)
//  2. ~/.grok/auth.json (the location the Grok CLI itself writes to)
//
// Both states "not installed" and "not logged in" surface as errAgentNotInstalled
// so the dispatcher can silently skip Grok in --agent all mode.
func readGrokToken() (string, error) {
	if v := os.Getenv("GROK_OAUTH_TOKEN"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".grok", "auth.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errAgentNotInstalled
		}
		return "", err
	}
	return readGrokTokenFromBytes(data)
}

func readGrokTokenFromBytes(data []byte) (string, error) {
	var entries map[string]grokAuthEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return "", fmt.Errorf("parse grok auth.json: %w", err)
	}
	for _, e := range entries {
		if e.Key != "" {
			return e.Key, nil
		}
	}
	return "", errAgentNotInstalled
}

// grokBillingResponse is the (subset of the) shape returned by
// GET /v1/billing on cli-chat-proxy.grok.com. Fields we don't use are omitted.
type grokBillingResponse struct {
	Config *grokBillingConfig `json:"config"`
}

type grokBillingConfig struct {
	MonthlyLimit       grokCents `json:"monthlyLimit"`
	Used               grokCents `json:"used"`
	OnDemandCap        grokCents `json:"onDemandCap"`
	BillingPeriodStart time.Time `json:"billingPeriodStart"`
	BillingPeriodEnd   time.Time `json:"billingPeriodEnd"`
}

// grokCents wraps a monetary amount. The Grok billing API expresses every dollar
// figure as { "val": <cents> } — including limits, usage, and caps.
type grokCents struct {
	Val int64 `json:"val"`
}

func parseGrokBilling(data []byte) (*grokBillingResponse, error) {
	var resp grokBillingResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse Grok billing response: %w", err)
	}
	return &resp, nil
}

func grokConfigToWindow(cfg *grokBillingConfig) Window {
	var usedPct float64
	if cfg.MonthlyLimit.Val > 0 {
		usedPct = 100 * float64(cfg.Used.Val) / float64(cfg.MonthlyLimit.Val)
	}
	windowMin := int(cfg.BillingPeriodEnd.Sub(cfg.BillingPeriodStart).Minutes())
	return Window{
		Label:         "monthly",
		WindowMinutes: windowMin,
		UsedPercent:   usedPct,
		ResetsAt:      cfg.BillingPeriodEnd,
	}
}

// formatCentsUSD renders cents as "$NN.NN". No locale awareness; the Grok billing
// API is USD-only as of writing. We use fixed 2-decimal precision so the value
// aligns vertically when stacked in a report.
func formatCentsUSD(cents int64) string {
	return fmt.Sprintf("$%d.%02d", cents/100, ((cents%100)+100)%100)
}
