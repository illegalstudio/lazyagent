package limits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/illegalstudio/lazyagent/internal/kimi"
)

const defaultKimiCodeBaseURL = "https://api.kimi.com/coding/v1"

type kimiCredentials struct {
	AccessToken string `json:"access_token"`
}

func readKimiToken() (string, error) {
	if v := os.Getenv("KIMI_CODE_OAUTH_TOKEN"); v != "" {
		return v, nil
	}
	path := kimi.CredentialsPath()
	if path == "" {
		return "", errAgentNotInstalled
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errAgentNotInstalled
		}
		return "", err
	}
	return readKimiTokenFromBytes(data)
}

func readKimiTokenFromBytes(data []byte) (string, error) {
	var creds kimiCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("parse Kimi credentials: %w", err)
	}
	if creds.AccessToken == "" {
		return "", errAgentNotInstalled
	}
	return creds.AccessToken, nil
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
	token, err := readKimiToken()
	if err != nil {
		if errors.Is(err, errAgentNotInstalled) {
			return Report{}, err
		}
		return Report{}, fmt.Errorf("read Kimi OAuth token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, kimiUsageURL(), nil)
	if err != nil {
		return Report{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
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
	if len(report.Windows) == 0 {
		return Report{}, fmt.Errorf("Kimi usage endpoint returned no usable windows (response: %s)", snippet(body, 200))
	}
	return report, nil
}

func kimiUsageURL() string {
	base := os.Getenv("KIMI_CODE_BASE_URL")
	if base == "" {
		base = defaultKimiCodeBaseURL
	}
	return strings.TrimRight(base, "/") + "/usages"
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
