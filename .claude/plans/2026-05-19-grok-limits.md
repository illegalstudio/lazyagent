# Grok Limits Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Grok (xAI) as a third provider to the `lazyagent limits` command, so users see their monthly credit usage alongside Claude Code and Codex.

**Architecture:** Mirror the `claude.go` pattern — read the user's OAuth bearer from `~/.grok/auth.json`, GET the undocumented endpoint `https://cli-chat-proxy.grok.com/v1/billing`, map the response to the existing `Window` model. Grok exposes a single **monthly** window (not 5h / 7d like Claude / Codex), so the report has one `Window` instead of two. The existing format / pace pipeline works unchanged because `Window` only cares about `WindowMinutes`, `UsedPercent`, and `ResetsAt`.

**Tech Stack:** Go 1.x (standard library only — `net/http`, `encoding/json`, `os`, `time`). No new dependencies.

---

## Background — what was discovered

The Grok CLI binary (`~/.grok/bin/grok`, Rust) ships an extension `xai_grok_shell::extensions::billing` whose `/usage show` slash command fetches `BillingConfigResponse` from the chat proxy. The slash command's UI message ("Credits used: … / Resets: …") and the error strings ("Authentication required to fetch billing data", "Billing data requires auth with grok.com. Run `grok login` to authenticate.") are baked into the binary.

By probing candidate paths with the user's own OAuth bearer (from `~/.grok/auth.json`), we confirmed:

- **Endpoint:** `GET https://cli-chat-proxy.grok.com/v1/billing`
- **Auth:** `Authorization: Bearer <JWT>` where the JWT is the `key` field of `~/.grok/auth.json`'s only entry. The map is keyed by `https://auth.x.ai::<oidc-client-id>`.
- **Response shape (live, verified):**
  ```json
  {
    "config": {
      "monthlyLimit":       { "val": 60000 },
      "used":               { "val": 8325 },
      "onDemandCap":        { "val": 0 },
      "billingPeriodStart": "2026-05-01T00:00:00+00:00",
      "billingPeriodEnd":   "2026-06-01T00:00:00+00:00",
      "history": [
        { "billingCycle": {"year":2026,"month":4},
          "includedUsed":  {"val":0},
          "onDemandUsed":  {"val":0},
          "totalUsed":     {"val":0} }
      ]
    }
  }
  ```
  `val` is **cents**. `monthlyLimit` is the included credit budget, `used` is total spend in the current period, `onDemandCap` is the over-budget spending cap (0 = on-demand disabled). The billing period is a calendar month.
- **Stability:** undocumented. Treat exactly like Claude's `/api/oauth/usage` — read-only, on user invocation, fail gracefully on shape changes. Same disclaimer copy.

---

## File structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/limits/grok.go` | Create | Token reader, billing JSON types, HTTP fetcher, Report builder. |
| `internal/limits/grok_test.go` | Create | Tests for token reader and JSON → Window conversion. |
| `internal/limits/testdata/grok_auth.json` | Create | Fixture for token reader test. |
| `internal/limits/testdata/grok_billing.json` | Create | Fixture for billing parser test. |
| `internal/limits/run.go` | Modify | Add "grok" to dispatcher (resolveAgents, fetchReport, notInstalledMessage, --agent help, doc comment). |
| `main.go` | Modify | Top-level help mentions Grok. |
| `README.md` | Modify | "News" bullet mentions Grok in the supported providers for `limits`. |
| `docs/maintenance/limits.md` | Modify | Synopsis, flag table, How-it-gets-the-data section, disclaimer, env vars, install-state matrix all updated for Grok. |

---

## Task 1: Read the Grok OAuth token (TDD)

Read the JWT from `~/.grok/auth.json`, with a `GROK_OAUTH_TOKEN` env-var override for CI / debugging. Same priority pattern as `readClaudeToken` (env var first, then on-disk).

The auth file's shape is `{ "<scope-key>": { "key": "<jwt>", ... } }` where the scope key looks like `https://auth.x.ai::<client-id>`. There's exactly one entry in practice; we iterate to be defensive and accept the first one with a non-empty `key`.

**Files:**
- Create: `internal/limits/grok.go`
- Create: `internal/limits/grok_test.go`
- Create: `internal/limits/testdata/grok_auth.json`

- [ ] **Step 1: Create the auth fixture**

Write `internal/limits/testdata/grok_auth.json`:
```json
{
  "https://auth.x.ai::b1a00492-073a-47ea-816f-4c329264a828": {
    "key": "test-jwt-token-payload",
    "extra": "ignored"
  }
}
```

- [ ] **Step 2: Write the failing tests**

Write `internal/limits/grok_test.go`:
```go
package limits

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadGrokTokenFromBytes_Valid(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "grok_auth.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	got, err := readGrokTokenFromBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "test-jwt-token-payload" {
		t.Errorf("got %q, want %q", got, "test-jwt-token-payload")
	}
}

func TestReadGrokTokenFromBytes_NoEntries(t *testing.T) {
	_, err := readGrokTokenFromBytes([]byte(`{}`))
	if err == nil {
		t.Fatal("expected error for empty auth.json, got nil")
	}
}

func TestReadGrokTokenFromBytes_EmptyKey(t *testing.T) {
	data := []byte(`{"https://auth.x.ai::abc":{"key":""}}`)
	_, err := readGrokTokenFromBytes(data)
	if err == nil {
		t.Fatal("expected error for empty key, got nil")
	}
}

func TestReadGrokTokenFromBytes_Malformed(t *testing.T) {
	_, err := readGrokTokenFromBytes([]byte(`not json`))
	if err == nil {
		t.Fatal("expected parse error for malformed JSON, got nil")
	}
}

func TestReadGrokToken_EnvOverride(t *testing.T) {
	t.Setenv("GROK_OAUTH_TOKEN", "env-token")
	got, err := readGrokToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "env-token" {
		t.Errorf("got %q, want %q", got, "env-token")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/limits/ -run TestReadGrokToken -v`
Expected: build fails / tests fail because `readGrokToken` and `readGrokTokenFromBytes` don't exist yet.

- [ ] **Step 4: Create `grok.go` with the token reader**

Write `internal/limits/grok.go`:
```go
package limits

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// grokAuthFile is the JSON layout of ~/.grok/auth.json: a map keyed by the
// OIDC scope ("https://auth.x.ai::<client-id>") whose value carries the bearer
// JWT under "key". Extra fields are ignored.
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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/limits/ -run TestReadGrokToken -v`
Expected: PASS for all five cases.

- [ ] **Step 6: Commit**

```bash
git add internal/limits/grok.go internal/limits/grok_test.go internal/limits/testdata/grok_auth.json
git commit -m "limits: read Grok OAuth token from ~/.grok/auth.json"
```

---

## Task 2: Parse the billing response into a `Window` (TDD)

Pure logic, no network. Parse the JSON shape we verified live, map to one `Window` covering the calendar billing month. `monthlyLimit` and `used` come in **cents**; `UsedPercent = used / monthlyLimit * 100`. `WindowMinutes = (billingPeriodEnd - billingPeriodStart) / 1 minute`. `ResetsAt = billingPeriodEnd`.

A helper `formatCentsUSD(8325) → "$83.25"` is used for the `Source` line so absolute dollar amounts show up next to the percentage.

**Files:**
- Modify: `internal/limits/grok.go`
- Modify: `internal/limits/grok_test.go`
- Create: `internal/limits/testdata/grok_billing.json`

- [ ] **Step 1: Create the billing fixture**

Write `internal/limits/testdata/grok_billing.json` (real shape, anonymized numbers):
```json
{
  "config": {
    "monthlyLimit":       { "val": 60000 },
    "used":               { "val": 8325 },
    "onDemandCap":        { "val": 0 },
    "billingPeriodStart": "2026-05-01T00:00:00+00:00",
    "billingPeriodEnd":   "2026-06-01T00:00:00+00:00",
    "history": [
      { "billingCycle": {"year":2026,"month":4},
        "includedUsed":  {"val":0},
        "onDemandUsed":  {"val":0},
        "totalUsed":     {"val":0} }
    ]
  }
}
```

- [ ] **Step 2: Write the failing tests**

Append to `internal/limits/grok_test.go`:
```go
import "time" // add to existing import block

func TestParseGrokBillingFromBytes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "grok_billing.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	resp, err := parseGrokBilling(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Config == nil {
		t.Fatal("config is nil")
	}
	if resp.Config.MonthlyLimit.Val != 60000 {
		t.Errorf("monthlyLimit: got %d, want 60000", resp.Config.MonthlyLimit.Val)
	}
	if resp.Config.Used.Val != 8325 {
		t.Errorf("used: got %d, want 8325", resp.Config.Used.Val)
	}
	if resp.Config.BillingPeriodEnd.IsZero() {
		t.Error("billingPeriodEnd parsed as zero time")
	}
}

func TestGrokConfigToWindow(t *testing.T) {
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	cfg := &grokBillingConfig{
		MonthlyLimit:       grokCents{Val: 60000},
		Used:               grokCents{Val: 8325},
		OnDemandCap:        grokCents{Val: 0},
		BillingPeriodStart: start,
		BillingPeriodEnd:   end,
	}
	w := grokConfigToWindow(cfg)
	if w.Label != "monthly" {
		t.Errorf("label: got %q, want %q", w.Label, "monthly")
	}
	wantMinutes := int(end.Sub(start).Minutes()) // 31 * 24 * 60 = 44640
	if w.WindowMinutes != wantMinutes {
		t.Errorf("windowMinutes: got %d, want %d", w.WindowMinutes, wantMinutes)
	}
	wantPct := 100 * float64(8325) / float64(60000)
	if w.UsedPercent < wantPct-0.001 || w.UsedPercent > wantPct+0.001 {
		t.Errorf("usedPercent: got %.4f, want %.4f", w.UsedPercent, wantPct)
	}
	if !w.ResetsAt.Equal(end) {
		t.Errorf("resetsAt: got %v, want %v", w.ResetsAt, end)
	}
}

func TestGrokConfigToWindow_ZeroLimitYieldsZeroPercent(t *testing.T) {
	cfg := &grokBillingConfig{
		MonthlyLimit:       grokCents{Val: 0},
		Used:               grokCents{Val: 1234},
		BillingPeriodStart: time.Now(),
		BillingPeriodEnd:   time.Now().Add(24 * time.Hour),
	}
	w := grokConfigToWindow(cfg)
	if w.UsedPercent != 0 {
		t.Errorf("usedPercent with zero monthlyLimit: got %.4f, want 0", w.UsedPercent)
	}
}

func TestFormatCentsUSD(t *testing.T) {
	cases := []struct {
		cents int64
		want  string
	}{
		{0, "$0.00"},
		{1, "$0.01"},
		{99, "$0.99"},
		{100, "$1.00"},
		{8325, "$83.25"},
		{60000, "$600.00"},
		{123456, "$1234.56"},
	}
	for _, c := range cases {
		if got := formatCentsUSD(c.cents); got != c.want {
			t.Errorf("formatCentsUSD(%d): got %q, want %q", c.cents, got, c.want)
		}
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/limits/ -run 'TestParseGrokBilling|TestGrokConfigToWindow|TestFormatCentsUSD' -v`
Expected: build fails because `parseGrokBilling`, `grokBillingConfig`, `grokCents`, `grokConfigToWindow`, `formatCentsUSD` don't exist.

- [ ] **Step 4: Add the types and helpers**

Append to `internal/limits/grok.go`:
```go
// Add to the existing import block at the top of grok.go:
//   "time"

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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/limits/ -run 'TestParseGrokBilling|TestGrokConfigToWindow|TestFormatCentsUSD' -v`
Expected: PASS for all four cases.

- [ ] **Step 6: Commit**

```bash
git add internal/limits/grok.go internal/limits/grok_test.go internal/limits/testdata/grok_billing.json
git commit -m "limits: parse Grok billing response into a Window"
```

---

## Task 3: Fetch the live endpoint and assemble the Report

Mirror `fetchClaudeReport`: one HTTPS GET, same 15s timeout, same 401 / 429 / non-200 handling, same on-failure messages adapted to Grok's auth model. The `Report.Source` line surfaces the absolute dollar figures (because the bar % isn't enough — users care about "$83 of $600"), and `Report.Note` carries the same "undocumented endpoint" disclaimer used for Claude.

We reuse `userAgent()` from `claude.go` since both are in package `limits`.

**Files:**
- Modify: `internal/limits/grok.go`

- [ ] **Step 1: Add the HTTP fetcher**

Append to `internal/limits/grok.go`:
```go
// Add to the existing import block at the top of grok.go:
//   "context"
//   "errors"
//   "io"
//   "net/http"

// grokBillingURL is the (undocumented) endpoint the Grok CLI itself calls when
// the user runs the /usage show slash command. See package doc in run.go for
// the stability caveat.
const grokBillingURL = "https://cli-chat-proxy.grok.com/v1/billing"

func fetchGrokReport(ctx context.Context) (Report, error) {
	token, err := readGrokToken()
	if err != nil {
		if errors.Is(err, errAgentNotInstalled) {
			return Report{}, err
		}
		return Report{}, fmt.Errorf("read Grok OAuth token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, grokBillingURL, nil)
	if err != nil {
		return Report{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent())

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Report{}, fmt.Errorf("call Grok billing endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return Report{}, fmt.Errorf("Grok OAuth token rejected (401). Run `grok login` to refresh, or set GROK_OAUTH_TOKEN")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return Report{}, fmt.Errorf("Grok billing endpoint rate-limited (429). Try again in a minute")
	}
	if resp.StatusCode != http.StatusOK {
		return Report{}, fmt.Errorf("Grok billing endpoint: %s — %s", resp.Status, snippet(body, 200))
	}

	billing, err := parseGrokBilling(body)
	if err != nil {
		return Report{}, err
	}
	if billing.Config == nil {
		return Report{}, fmt.Errorf("Grok billing response missing config block (response: %s)", snippet(body, 200))
	}
	if billing.Config.MonthlyLimit.Val <= 0 {
		return Report{}, fmt.Errorf("Grok billing response has no monthly limit (no active subscription?)")
	}

	w := grokConfigToWindow(billing.Config)
	source := fmt.Sprintf("Source: %s of %s used",
		formatCentsUSD(billing.Config.Used.Val),
		formatCentsUSD(billing.Config.MonthlyLimit.Val),
	)
	if billing.Config.OnDemandCap.Val > 0 {
		source += fmt.Sprintf(" (on-demand cap: %s)", formatCentsUSD(billing.Config.OnDemandCap.Val))
	}

	return Report{
		Provider: "Grok",
		Source:   source,
		Windows:  []Window{w},
		Note:     "Note: reads /v1/billing on cli-chat-proxy.grok.com, an undocumented xAI endpoint used by the Grok CLI's /usage command. May break or be revoked by xAI without notice.",
	}, nil
}
```

- [ ] **Step 2: Build the package**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 3: Run all limits tests**

Run: `go test ./internal/limits/ -v`
Expected: PASS for the existing Claude / format tests plus the new Grok tests.

- [ ] **Step 4: Commit**

```bash
git add internal/limits/grok.go
git commit -m "limits: fetch Grok monthly billing from cli-chat-proxy"
```

---

## Task 4: Wire Grok into the `limits` dispatcher

Add `"grok"` to `resolveAgents`, `fetchReport`, and `notInstalledMessage`. Update the `--agent` flag description, the `Usage:` block printed by `--help`, and the package-level doc comment that explains why the data source disclaimer exists.

**Files:**
- Modify: `internal/limits/run.go`

- [ ] **Step 1: Update the package doc comment**

In `internal/limits/run.go`, replace the top doc-comment block (lines 1-15) with:
```go
// Package limits implements the `lazyagent limits` subcommand: a one-shot
// snapshot of the user's Claude Code, Codex, and Grok rate-limit / billing
// windows, plus a "pace" indicator that compares actual consumption to
// a perfectly linear consumption rate.
//
// IMPORTANT (Claude): the source for Claude is /api/oauth/usage on
// api.anthropic.com — the same endpoint Claude Code's own `/status` calls.
// As of this writing Anthropic does not document it publicly. lazyagent
// queries it on explicit user invocation only (no polling). Behavior may
// change without notice; failures degrade gracefully.
//
// Codex limits are read from the latest session rollout under
// ~/.codex/sessions, where Codex itself persists the server's rate_limits
// response after each turn. No network call is made for Codex.
//
// IMPORTANT (Grok): the source for Grok is /v1/billing on
// cli-chat-proxy.grok.com — the same endpoint the Grok CLI's `/usage show`
// slash command calls. As of this writing xAI does not document it publicly.
// Same caveats as Claude: on-demand only, fail gracefully.
package limits
```

- [ ] **Step 2: Update the `--agent` flag default description**

In `internal/limits/run.go`, change the `fs.StringVar` line:
```go
// Old:
fs.StringVar(&opts.agent, "agent", "all", "Which agent to query: claude, codex, all")
// New:
fs.StringVar(&opts.agent, "agent", "all", "Which agent to query: claude, codex, grok, all")
```

- [ ] **Step 3: Update the `fs.Usage` printout**

In `internal/limits/run.go`, replace the multi-line `fs.Usage` string with:
```go
fs.Usage = func() {
    fmt.Fprint(os.Stderr, `lazyagent limits — show rate-limit usage

Usage:
  lazyagent limits                  Show limits for Claude Code, Codex, and Grok
  lazyagent limits --agent claude   Show only Claude Code limits
  lazyagent limits --agent codex    Show only Codex limits
  lazyagent limits --agent grok     Show only Grok limits

Output explains:
  - Used %:    how much of the window has been consumed
  - Elapsed %: how far we are into the window's time
  - Pace:      consumption vs. a perfectly linear pace
                 underutilizing  (used < 0.85 × elapsed)
                 on track        (0.85 ≤ used/elapsed ≤ 1.15)
                 overutilizing   (used > 1.15 × elapsed)

Authentication:
  Claude  reads its OAuth token from, in order:
            1. CLAUDE_CODE_OAUTH_TOKEN env var
            2. macOS Keychain (service "Claude Code-credentials")
            3. ~/.claude/.credentials.json
          If none is found, run `+"`claude`"+` to log in.
  Codex   reads ~/.codex/sessions/<date>/rollout-*.jsonl (no network call).
  Grok    reads its OAuth token from, in order:
            1. GROK_OAUTH_TOKEN env var
            2. ~/.grok/auth.json
          If none is found, run `+"`grok login`"+`.

Disclaimer (Claude, Grok):
  Both providers expose their usage through undocumented endpoints used by
  their respective official CLIs. lazyagent calls them only on explicit user
  invocation. They may break or be revoked by their vendors without notice.

Flags:
`)
    fs.PrintDefaults()
}
```

- [ ] **Step 4: Update `resolveAgents`**

Replace the function body with:
```go
func resolveAgents(arg string) ([]string, error) {
	arg = strings.TrimSpace(strings.ToLower(arg))
	switch arg {
	case "", "all":
		return []string{"claude", "codex", "grok"}, nil
	case "claude", "codex", "grok":
		return []string{arg}, nil
	default:
		return nil, fmt.Errorf("unsupported agent %q (use claude, codex, grok, or all)", arg)
	}
}
```

- [ ] **Step 5: Update `fetchReport`**

Replace the function body with:
```go
func fetchReport(ctx context.Context, agent string) (Report, error) {
	switch agent {
	case "claude":
		return fetchClaudeReport(ctx)
	case "codex":
		return fetchCodexReport()
	case "grok":
		return fetchGrokReport(ctx)
	default:
		return Report{}, fmt.Errorf("unsupported agent %q", agent)
	}
}
```

- [ ] **Step 6: Update `notInstalledMessage`**

Replace the function body with:
```go
func notInstalledMessage(agent string) string {
	switch agent {
	case "claude":
		return "Claude Code is not installed or not logged in. Run `claude` to log in, or set CLAUDE_CODE_OAUTH_TOKEN."
	case "codex":
		return "Codex is not installed (no sessions under ~/.codex/sessions). Run a Codex CLI session first."
	case "grok":
		return "Grok CLI is not installed or not logged in (no ~/.grok/auth.json). Run `grok login`, or set GROK_OAUTH_TOKEN."
	default:
		return fmt.Sprintf("%s is not installed.", agent)
	}
}
```

- [ ] **Step 7: Update the "no agents installed" fallback message**

Find this block near the end of `Run`:
```go
if printed == 0 && !explicit && missing == len(agents) {
    fmt.Fprintln(os.Stderr, "No supported agents are installed (neither Claude Code nor Codex was detected).")
    fmt.Fprintln(os.Stderr, "Run `claude` to log in, or run a Codex CLI session first.")
    exitCode = 1
}
```
Replace with:
```go
if printed == 0 && !explicit && missing == len(agents) {
    fmt.Fprintln(os.Stderr, "No supported agents are installed (none of Claude Code, Codex, or Grok was detected).")
    fmt.Fprintln(os.Stderr, "Run `claude` / `grok login` to authenticate, or run a Codex CLI session first.")
    exitCode = 1
}
```

- [ ] **Step 8: Build and run the test suite**

Run: `go build ./... && go test ./internal/limits/ -v`
Expected: build succeeds; all tests pass.

- [ ] **Step 9: Verify --help renders cleanly**

Run: `go run . limits --help`
Expected: prints the new Usage block (mentions Grok in 3 places, mentions `GROK_OAUTH_TOKEN`).

- [ ] **Step 10: Commit**

```bash
git add internal/limits/run.go
git commit -m "limits: wire Grok into the dispatcher and --help"
```

---

## Task 5: Update top-level help, README, and docs

The `limits` subcommand is advertised in two top-level user-facing places (the binary's `--help` and the project README) and one deep-link doc page (`docs/maintenance/limits.md`).

**Files:**
- Modify: `main.go`
- Modify: `README.md`
- Modify: `docs/maintenance/limits.md`

- [ ] **Step 1: Update `main.go` top-level help lines**

The help text in `main.go` lives inside a single backtick raw-string literal that spans roughly lines 75-91. Two lines (currently lines 85-86) need to change:

Before:
```
  lazyagent limits              Show 5h / weekly rate-limit usage and pace
  lazyagent limits --help       Show limits options (--agent claude|codex|all)
```

After:
```
  lazyagent limits              Show rate-limit / billing usage and pace
  lazyagent limits --help       Show limits options (--agent claude|codex|grok|all)
```

Preserve the leading two-space indentation and the column alignment for the description.

- [ ] **Step 2: Update the README "News" bullet**

In `README.md`, line 29:
```markdown
// Old:
- **[`lazyagent limits`](docs/maintenance/limits.md)** — on-demand 5-hour and weekly rate-limit snapshot for Claude Code and Codex, with a pace indicator that flags whether you're under-, on-, or over-utilizing the window.
// New:
- **[`lazyagent limits`](docs/maintenance/limits.md)** — on-demand rate-limit / billing snapshot for Claude Code (5h + 7d), Codex (5h + 7d), and Grok (monthly), with a pace indicator that flags whether you're under-, on-, or over-utilizing the window.
```

- [ ] **Step 3: Update `docs/maintenance/limits.md` — frontmatter description**

```markdown
// Old:
description: "On-demand snapshot of Claude Code and Codex 5-hour and weekly rate-limit windows, with a pace indicator vs. linear consumption."
// New:
description: "On-demand snapshot of Claude Code (5h + 7d), Codex (5h + 7d), and Grok (monthly billing) rate-limit windows, with a pace indicator vs. linear consumption."
```

- [ ] **Step 4: Update the opening paragraph and "Synopsis" of `limits.md`**

Replace the first two paragraphs (lines 8-10) with:
```markdown
`lazyagent limits` prints a one-shot snapshot of the rate-limit / billing windows exposed by Claude Code, Codex, and Grok, with a *pace indicator* that compares actual consumption to a perfectly linear pace through the window. It's read-only, on demand, and does not poll. Claude and Codex each expose a **5-hour** and a **7-day** window; Grok exposes a single **monthly** credit window.

Use it to answer questions like *"am I burning the weekly limit faster than I should?"* before you commit to a long agent run, *"how much of my 5-hour budget is left until the next reset?"* when you suspect you're close to the wall, or *"how much of my Grok monthly credit have I burned this month?"* before kicking off a long Grok run.
```

Replace the `Synopsis` line (line 15) with:
```
lazyagent limits [--agent claude|codex|grok|all]
```

Replace the `--agent NAME` row in the Flags table to read:
```markdown
| `--agent NAME` | string | `all` | Which agent to query: `claude`, `codex`, `grok`, or `all` |
```

Replace the sentence immediately under that table (line 25) with:
```markdown
Only `claude`, `codex`, and `grok` are supported — they're the agents in lazyagent's set that expose rate-limit or billing windows in a stable-enough, observable form.
```

Replace the `Quick reference` code block to read:
```bash
lazyagent limits                   # all three agents (default)
lazyagent limits --agent claude    # only Claude Code
lazyagent limits --agent codex     # only Codex
lazyagent limits --agent grok      # only Grok
lazyagent limits --help            # full usage + disclaimers
```

- [ ] **Step 5: Add a `Grok` subsection under "How it gets the data"**

After the existing `### Codex` subsection (ends at line 120), add:
```markdown
### Grok

A single HTTPS GET to `https://cli-chat-proxy.grok.com/v1/billing` with the user's OAuth bearer token. This is the **same** endpoint Grok CLI's interactive `/usage show` slash command queries.

The OAuth token is read in this priority order:

1. **`GROK_OAUTH_TOKEN`** environment variable — useful for CI or for overriding the on-disk credential file
2. **`~/.grok/auth.json`** — the file `grok login` writes to. lazyagent picks the first entry whose `key` field is non-empty (in practice there is exactly one)

If neither is present, the command tells you to run `grok login`.

The response carries one monthly window's worth of state — the included credit limit, the amount used in the current billing period (both in cents), the on-demand spending cap, and the period start / end timestamps. lazyagent maps this onto a single `monthly` `Window`, computes `Used %` as `used / monthlyLimit × 100`, and uses the period end as the reset time. Absolute dollar amounts appear on the `Source:` line so you can see both "$83.25 of $600.00" and the percentage in the same report.

When the response advertises an `onDemandCap` greater than zero, the cap is shown after the source line. The `Used %` is intentionally not re-scaled against the cap — what matters for the pace indicator is how fast you're consuming the *included* monthly budget.
```

- [ ] **Step 6: Update the "When an agent isn't installed" matrix**

Replace the matrix table (lines 126-131) with one that has a Grok column:
```markdown
| State | Default (`--agent all`) | `--agent claude` | `--agent codex` | `--agent grok` |
|-------|-------------------------|------------------|-----------------|----------------|
| All three installed | Three reports printed | Claude printed | Codex printed | Grok printed |
| Subset installed | Installed providers printed, others silently skipped | Claude printed or error | Codex printed or error | Grok printed or error |
| None installed | Single guidance message on stderr, exit 1 | Friendly error, exit 1 | Friendly error, exit 1 | Friendly error, exit 1 |
```

- [ ] **Step 7: Extend the Disclaimer section**

Change the heading on line 135 from `## Disclaimer (Claude Code)` to `## Disclaimers (Claude Code, Grok)`, and append after the existing Claude bullet block (after line 143) a parallel Grok block:
```markdown
For **Grok**, `/v1/billing` is similarly **not** part of xAI's documented public API. As of this writing it is used internally by the Grok CLI's `/usage show` slash command and is subject to:

- **No stability guarantee** — endpoint path, response shape, and field names may change without notice. lazyagent fails gracefully (clear error, exit code 1) when this happens.
- **Subscription scope** — the response reflects the billing plan associated with the OAuth token (SuperGrok and similar). Users without a billing plan, or pure API-key users on `api.x.ai`, won't see meaningful data here.
- **Bearer reuse** — lazyagent sends the same JWT the Grok CLI uses. Treat it as a credential; the same caveats about token-rotation and revocation apply.

The "don't poll" guidance applies equally to Grok: run `lazyagent limits` interactively, not in a `watch` loop.
```

- [ ] **Step 8: Extend the Environment section**

In the table (lines 157-160), add a row:
```markdown
| `GROK_OAUTH_TOKEN` | Override the OAuth token for the Grok call. Used in priority before `~/.grok/auth.json` |
```

- [ ] **Step 9: Build + test once more to make sure nothing else broke**

Run: `go build ./... && go test ./...`
Expected: build succeeds; full test suite passes.

- [ ] **Step 10: Commit**

```bash
git add main.go README.md docs/maintenance/limits.md
git commit -m "docs: document Grok support in lazyagent limits"
```

---

## Task 6: Live smoke test and final verification

Verify against the user's actual Grok account that the report renders correctly, with realistic numbers and reset times. This is the final sanity check before the branch is finished — no commits expected unless something visibly wrong shows up.

**Files:** none (read-only verification).

- [ ] **Step 1: `--agent grok` runs cleanly**

Run: `go run . limits --agent grok`
Expected output shape:
```
Grok

  monthly window
    Used:      NN.N%  █████░░░░░░░░░░░░░░░
    Elapsed:   NN.N%  ███████░░░░░░░░░░░░░
    Resets:   in Xd Yh (… …)
    Pace:     <label> (X.XX× of expected NN.N%)

  Source: $NN.NN of $NN.NN used
  Note: reads /v1/billing on cli-chat-proxy.grok.com, an undocumented xAI endpoint used by the Grok CLI's /usage command. May break or be revoked by xAI without notice.
```
Verify: the bars render, the pace label colors correctly in a real terminal (run it interactively, not piped), the absolute USD numbers on the Source line match what `grok` `/usage show` reports.

- [ ] **Step 2: `--agent all` includes Grok**

Run: `go run . limits`
Expected: three sections in order — Claude Code, Codex, Grok — separated by blank lines.

- [ ] **Step 3: Negative test — bad token surfaces a clear error**

Run: `GROK_OAUTH_TOKEN=definitely-not-a-real-token go run . limits --agent grok`
Expected: exit code 1, stderr contains `Grok OAuth token rejected (401). Run `grok login` to refresh, or set GROK_OAUTH_TOKEN`.

- [ ] **Step 4: Negative test — env override path works**

Run: `GROK_OAUTH_TOKEN=$(python3 -c "import json; d=json.load(open('$HOME/.grok/auth.json')); print(next(iter(d.values()))['key'])") go run . limits --agent grok`
Expected: same successful output as Step 1 (proves the env-var path reads the same token the file path does).

- [ ] **Step 5: Pipe-safety test**

Run: `go run . limits --agent grok | cat`
Expected: no ANSI escape sequences in the piped output. Bars render as plain `█`/`░`.

- [ ] **Step 6: Final tidy commit (only if anything was tweaked)**

If any of Steps 1-5 surfaced a real issue and you patched it, commit the fix with a message describing what broke. Otherwise no commit needed.

---

## Open questions for the implementer

None blocking — the endpoint and response shape are verified live in the research that produced this plan. Two things to *watch* during implementation:

1. **`onDemandCap` semantics.** We assume it's 0 when on-demand is disabled. If a user has it enabled but at value 0 (toggle without setting a cap), the report won't surface the on-demand row. That's acceptable — we'd rather under-display than misrepresent.
2. **Period boundary off-by-one.** `billingPeriodEnd` is the *exclusive* end of the period (next period's start). Our `WindowMinutes = end - start` is exact for that interpretation, and `ResetsAt = end` aligns with how Codex and Claude report "resets at" (the moment the new window begins). No special-casing needed.
