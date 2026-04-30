package limits

import (
	"testing"
	"time"
)

func TestElapsedPercent(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name          string
		windowMinutes int
		resetsAt      time.Time
		want          float64
	}{
		{"start of window", 300, now.Add(300 * time.Minute), 0},
		{"middle of window", 300, now.Add(150 * time.Minute), 50},
		{"end of window", 300, now, 100},
		{"already past reset", 300, now.Add(-10 * time.Minute), 100},
		{"reset further than window (clamp)", 300, now.Add(400 * time.Minute), 0},
		{"7-day, half consumed", 10080, now.Add(5040 * time.Minute), 50},
		{"zero window minutes", 0, now.Add(time.Hour), 0},
		{"zero reset time", 300, time.Time{}, 0},
	}
	for _, c := range cases {
		got := elapsedPercent(c.windowMinutes, c.resetsAt, now)
		if abs(got-c.want) > 0.01 {
			t.Errorf("%s: got %.3f, want %.3f", c.name, got, c.want)
		}
	}
}

func TestClassifyPace(t *testing.T) {
	cases := []struct {
		name    string
		used    float64
		elapsed float64
		wantP   pace
	}{
		{"window just opened (unknown)", 0.5, 0.5, paceUnknown},
		{"linear", 50, 50, paceOnTrack},
		{"slightly under (still on track)", 48, 50, paceOnTrack},
		{"slightly over (still on track)", 56, 50, paceOnTrack},
		{"clearly under", 20, 50, paceUnder},
		{"clearly over", 70, 50, paceOver},
		{"empty consumption far into window", 0, 80, paceUnder},
		{"full consumption early", 90, 10, paceOver},
	}
	for _, c := range cases {
		_, gotP := classifyPace(c.used, c.elapsed)
		if gotP != c.wantP {
			t.Errorf("%s: got pace=%d, want %d", c.name, gotP, c.wantP)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Minute, "30m"},
		{90 * time.Minute, "1h 30m"},
		{4*time.Hour + 23*time.Minute, "4h 23m"},
		{25 * time.Hour, "1d 1h"},
		{5*24*time.Hour + 3*time.Hour, "5d 3h"},
		{0, "0m"},
		{-time.Hour, "0m"},
	}
	for _, c := range cases {
		got := humanDuration(c.d)
		if got != c.want {
			t.Errorf("humanDuration(%v): got %q, want %q", c.d, got, c.want)
		}
	}
}

func TestBar(t *testing.T) {
	if got := bar(0, 10); got != "░░░░░░░░░░" {
		t.Errorf("bar(0): %q", got)
	}
	if got := bar(100, 10); got != "██████████" {
		t.Errorf("bar(100): %q", got)
	}
	if got := bar(50, 10); got != "█████░░░░░" {
		t.Errorf("bar(50): %q", got)
	}
	if got := bar(150, 10); got != "██████████" {
		t.Errorf("bar(over 100): %q", got)
	}
	if got := bar(-5, 10); got != "░░░░░░░░░░" {
		t.Errorf("bar(negative): %q", got)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
