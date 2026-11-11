package ui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/illegalstudio/lazyagent/internal/limits"
)

func limitsTestModel(tab int, loading bool, view limits.View) Model {
	theme := DarkTheme()
	return Model{
		theme:         theme,
		width:         100,
		height:        40,
		limitsOpen:    true,
		limitsTab:     tab,
		limitsLoading: loading,
		limitsView:    view,
	}
}

func sampleView() limits.View {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	return limits.BuildView([]limits.Report{
		{
			Provider: "Claude Code",
			Windows: []limits.Window{
				{Label: "5-hour", WindowMinutes: 300, UsedPercent: 50, ResetsAt: now.Add(150 * time.Minute)},
			},
		},
	}, now)
}

// fullView builds a multi-provider view whose reports carry long Source/Note
// disclaimers — the lines most likely to wrap and overflow the modal.
func fullView() limits.View {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	longNote := "This source is undocumented and may break without notice; lazyagent only queries it on explicit user invocation."
	var reports []limits.Report
	for _, p := range []string{"Claude Code", "Codex", "Grok", "Kimi Code", "Cursor Models", "Cursor API"} {
		reports = append(reports, limits.Report{
			Provider: p,
			Source:   longNote,
			Note:     longNote,
			Windows: []limits.Window{
				{Label: "5-hour", WindowMinutes: 300, UsedPercent: 80, ResetsAt: now.Add(90 * time.Minute)},
				{Label: "7-day", WindowMinutes: 7 * 24 * 60, UsedPercent: 40, ResetsAt: now.Add(72 * time.Hour)},
			},
		})
	}
	return limits.BuildView(reports, now)
}

// TestRenderLimitsModal_FitsScreen guards against the modal rendering larger
// than the terminal — long disclaimer lines must be truncated, not wrapped.
func TestRenderLimitsModal_FitsScreen(t *testing.T) {
	view := fullView()
	sizes := []struct{ w, h int }{
		{100, 40}, {80, 24}, {60, 20}, {40, 15}, {30, 12},
	}
	for _, tab := range []int{limitsTabSummary, limitsTabDetailed} {
		for _, s := range sizes {
			m := limitsTestModel(tab, false, view)
			m.width, m.height = s.w, s.h
			out := m.renderLimitsModal()
			if h := lipgloss.Height(out); h > s.h {
				t.Errorf("tab=%d size=%dx%d: rendered height %d > screen %d", tab, s.w, s.h, h, s.h)
			}
			if w := lipgloss.Width(out); w > s.w {
				t.Errorf("tab=%d size=%dx%d: rendered width %d > screen %d", tab, s.w, s.h, w, s.w)
			}
		}
	}
}

// columnGutterRe matches the first run of 2+ spaces in a summary line — the
// gutter between the provider column and the first data column. Cell text
// itself (e.g. "80.0% used / 70.0% exp") only ever contains single spaces, so
// this reliably locates where the data columns begin, provided the caller has
// already stripped the fixed 2-space left margin every line starts with
// (otherwise that margin itself is the first match).
var columnGutterRe = regexp.MustCompile(`\s{2,}`)

// TestRenderLimitsSummaryLines_ColumnsAlign guards against provider names
// wider than their column padding pushing the data columns out of alignment.
// %-8s is a minimum width, not a truncation: "Cursor Models" (13 chars) and
// "Cursor API" (10 chars) both overflow an 8-wide column, so each row's data
// columns start at a different position instead of lining up under the
// header. This must fail against the old %-8s verb and pass against %-14s.
func TestRenderLimitsSummaryLines_ColumnsAlign(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	reports := []limits.Report{
		{Provider: "Claude Code", Windows: []limits.Window{
			{Label: "5-hour", WindowMinutes: 300, UsedPercent: 80, ResetsAt: now.Add(time.Hour)},
		}},
		{Provider: "Cursor Models", Windows: []limits.Window{
			{Label: "monthly", WindowMinutes: 31 * 24 * 60, UsedPercent: 5.2, ResetsAt: now.Add(24 * time.Hour)},
		}},
		{Provider: "Cursor API", Windows: []limits.Window{
			{Label: "monthly", WindowMinutes: 31 * 24 * 60, UsedPercent: 5.2, ResetsAt: now.Add(24 * time.Hour)},
		}},
	}
	// Scoped rows: one wider than the 14-cell minimum, one past the cap.
	reports = append(reports,
		limits.Report{Provider: "Codex", Windows: []limits.Window{
			{Label: "7-day", Scope: "Code review", WindowMinutes: 7 * 24 * 60, UsedPercent: 3, ResetsAt: now.Add(48 * time.Hour)},
			{Label: "7-day", Scope: "GPT-5.3-Codex-Spark", WindowMinutes: 7 * 24 * 60, UsedPercent: 1, ResetsAt: now.Add(48 * time.Hour)},
		}},
	)
	view := limits.BuildView(reports, now)
	m := limitsTestModel(limitsTabSummary, false, view)

	lines := m.renderLimitsSummaryLines()
	if len(lines) != 6 {
		t.Fatalf("got %d lines, want 6 (header + 5 rows): %q", len(lines), lines)
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w > 76 {
			t.Errorf("line %d is %d cells wide, past the modal's 76-cell content: %q", i, w, ln)
		}
	}
	if !strings.Contains(lines[4], "Codex Code review") {
		t.Errorf("row under the cap should keep its full name: %q", lines[4])
	}
	if !strings.Contains(lines[5], "…") {
		t.Errorf("row past the cap should be ellipsized: %q", lines[5])
	}

	// Columns are compared in display cells, not bytes: an ellipsized name
	// ends in "…", which is one cell but three bytes.
	header := strings.TrimPrefix(lines[0], "  ")
	headerLoc := columnGutterRe.FindStringIndex(header)
	if headerLoc == nil {
		t.Fatalf("header line has no column gutter: %q", lines[0])
	}
	wantCol := lipgloss.Width(header[:headerLoc[1]])

	for i, ln := range lines[1:] {
		row := strings.TrimPrefix(ln, "  ")
		loc := columnGutterRe.FindStringIndex(row)
		if loc == nil {
			t.Fatalf("row %d: no column gutter found: %q", i, ln)
		}
		if got := lipgloss.Width(row[:loc[1]]); got != wantCol {
			t.Errorf("row %d: data column starts at %d, want %d (header's column) — %q", i, got, wantCol, ln)
		}
	}
}

func TestRenderLimitsModal_SummaryScrolls(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	var windows []limits.Window
	for _, scope := range []string{"", "Fable", "Code review", "Spark", "Mini", "Nano"} {
		windows = append(windows, limits.Window{Label: "7-day", Scope: scope, WindowMinutes: 7 * 24 * 60, UsedPercent: 5, ResetsAt: now.Add(time.Hour)})
	}
	view := limits.BuildView([]limits.Report{{Provider: "Codex", Windows: windows}}, now)
	m := limitsTestModel(limitsTabSummary, false, view)
	m.height = 14 // body budget of 4 lines: header + 3 of the 6 rows

	if out := m.renderLimitsModal(); strings.Contains(out, "Codex Nano") || !strings.Contains(out, "↓ more") {
		t.Fatalf("short terminal should hide the last row and offer more:\n%s", out)
	}
	m.limitsScroll = 10 // clamped to the last page
	if out := m.renderLimitsModal(); !strings.Contains(out, "Codex Nano") {
		t.Fatalf("scrolled Summary should reach the last row:\n%s", out)
	}
}

func TestRenderLimitsModal_Loading(t *testing.T) {
	m := limitsTestModel(0, true, limits.View{})
	out := m.renderLimitsModal()
	if !strings.Contains(out, "Loading limits") {
		t.Fatalf("loading modal missing spinner text: %q", out)
	}
}

func TestRenderLimitsModal_Empty(t *testing.T) {
	m := limitsTestModel(0, false, limits.View{Available: false})
	out := m.renderLimitsModal()
	if !strings.Contains(out, "No supported agents") {
		t.Fatalf("empty modal missing empty text: %q", out)
	}
}

func TestRenderLimitsModal_SummaryAndDetailed(t *testing.T) {
	v := sampleView()
	summary := limitsTestModel(0, false, v).renderLimitsModal()
	if !strings.Contains(summary, "Summary") || !strings.Contains(summary, "Claude") {
		t.Fatalf("summary modal missing content: %q", summary)
	}
	detailed := limitsTestModel(1, false, v).renderLimitsModal()
	if !strings.Contains(detailed, "5-hour window") {
		t.Fatalf("detailed modal missing window label: %q", detailed)
	}
}

func TestLimitsOpenClose(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 40

	opened, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	om := opened.(Model)
	if !om.limitsOpen || !om.limitsLoading {
		t.Fatalf("after 'l': open=%v loading=%v, want true/true", om.limitsOpen, om.limitsLoading)
	}
	if cmd == nil {
		t.Fatal("expected a load command after opening limits")
	}

	closed, _ := om.Update(tea.KeyMsg{Type: tea.KeyEsc})
	cm := closed.(Model)
	if cm.limitsOpen {
		t.Fatal("after esc: limitsOpen = true, want false")
	}
	if cm.limitsView.Available {
		t.Fatal("after close: view not cleared")
	}
}

func TestLimitsRefresh(t *testing.T) {
	m := limitsTestModel(limitsTabDetailed, false, sampleView())
	m.limitsScroll = 3
	m.limitsRequest = 7

	refreshed, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	rm := refreshed.(Model)
	if !rm.limitsOpen || !rm.limitsLoading {
		t.Fatalf("after 'r': open=%v loading=%v, want true/true", rm.limitsOpen, rm.limitsLoading)
	}
	if cmd == nil {
		t.Fatal("expected a load command after refreshing limits")
	}
	if rm.limitsRequest != 8 {
		t.Fatalf("limitsRequest = %d, want 8", rm.limitsRequest)
	}
	if rm.limitsTab != limitsTabDetailed || rm.limitsScroll != 3 {
		t.Fatalf("refresh changed position: tab=%d scroll=%d", rm.limitsTab, rm.limitsScroll)
	}

	stale, _ := rm.Update(limitsLoadedMsg{view: limits.View{}, requestID: 7})
	sm := stale.(Model)
	if !sm.limitsLoading || !sm.limitsView.Available {
		t.Fatal("stale response replaced the active limits refresh")
	}

	updatedView := limits.View{Available: false}
	loaded, _ := sm.Update(limitsLoadedMsg{view: updatedView, requestID: 8})
	lm := loaded.(Model)
	if lm.limitsLoading {
		t.Fatal("current refresh response left loading enabled")
	}
	if lm.limitsView.Available {
		t.Fatal("current refresh response was not applied")
	}
}

func TestRenderLimitsModal_RefreshHint(t *testing.T) {
	out := limitsTestModel(limitsTabSummary, false, sampleView()).renderLimitsModal()
	if !strings.Contains(out, "r refresh") {
		t.Fatalf("limits modal missing refresh hint: %q", out)
	}
}

func TestWindowLines(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e"}
	cases := []struct {
		name     string
		offset   int
		height   int
		wantLen  int
		wantMore bool
	}{
		{"top partial", 0, 3, 3, true},
		{"top full", 0, 5, 5, false},
		{"top overshoot height", 0, 10, 5, false},
		{"middle", 2, 2, 2, true},
		{"offset past end", 99, 3, 0, false},
		{"negative offset", -5, 2, 2, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vis, more := windowLines(lines, tc.offset, tc.height)
			if len(vis) != tc.wantLen {
				t.Errorf("len(visible) = %d, want %d", len(vis), tc.wantLen)
			}
			if more != tc.wantMore {
				t.Errorf("moreBelow = %v, want %v", more, tc.wantMore)
			}
		})
	}
}
