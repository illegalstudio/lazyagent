package core

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ShortName truncates a file path intelligently, showing "…/parent/child" when needed.
func ShortName(path string, maxLen int) string {
	if maxLen <= 2 {
		return ""
	}
	if len(path) <= maxLen {
		return path
	}
	base := filepath.Base(path)
	parent := filepath.Base(filepath.Dir(path))
	short := parent + "/" + base
	if len(short)+2 <= maxLen {
		return "…/" + short
	}
	if len(base)+2 <= maxLen {
		return "…/" + base
	}
	if maxLen > 3 {
		return "…" + base[len(base)-(maxLen-1):]
	}
	return base[:maxLen]
}

// FormatDuration converts a duration to a human-readable "Xs ago" string.
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// FormatTokens formats a token count for display (e.g., 1200 → "1.2k").
func FormatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
}

// FormatCost formats a USD cost for display.
func FormatCost(usd float64) string {
	if usd < 0.01 {
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", usd)
}

// EstimateCost estimates API cost based on model and token counts.
func EstimateCost(model string, inputTokens, outputTokens, cacheCreation, cacheRead int) float64 {
	var inputRate, outputRate float64
	lowerModel := strings.ToLower(model)
	switch {
	case strings.Contains(lowerModel, "opus"):
		inputRate, outputRate = 15.0/1_000_000, 75.0/1_000_000
	case strings.Contains(lowerModel, "haiku"):
		inputRate, outputRate = 1.0/1_000_000, 5.0/1_000_000
	default: // sonnet and others
		inputRate, outputRate = 3.0/1_000_000, 15.0/1_000_000
	}
	cacheWriteRate := inputRate * 1.25
	cacheReadRate := inputRate * 0.1
	return float64(inputTokens)*inputRate +
		float64(cacheCreation)*cacheWriteRate +
		float64(cacheRead)*cacheReadRate +
		float64(outputTokens)*outputRate
}

// PadRight pads a string with spaces to reach width n, or truncates if longer.
func PadRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}

// Clamp bounds an integer to [lo, hi].
func Clamp(lo, hi, v int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// BucketTimestamps groups timestamps into fixed-width buckets for sparkline display.
// Returns a slice of counts per bucket.
func BucketTimestamps(timestamps []time.Time, window time.Duration, width int) []int {
	buckets := make([]int, width)
	if width <= 0 || len(timestamps) == 0 {
		return buckets
	}

	now := time.Now()
	cutoff := now.Add(-window)
	bucketDur := window / time.Duration(width)
	if bucketDur <= 0 {
		return buckets
	}

	for _, ts := range timestamps {
		if ts.Before(cutoff) || ts.After(now) {
			continue
		}
		idx := int(ts.Sub(cutoff) / bucketDur)
		if idx >= width {
			idx = width - 1
		}
		buckets[idx]++
	}
	return buckets
}

// RenderSparkline renders bucketed data as a Unicode sparkline string.
func RenderSparkline(timestamps []time.Time, window time.Duration, width int) string {
	if width <= 0 {
		return ""
	}
	if len(timestamps) == 0 {
		return strings.Repeat(" ", width)
	}

	buckets := BucketTimestamps(timestamps, window, width)

	maxVal := 0
	for _, v := range buckets {
		if v > maxVal {
			maxVal = v
		}
	}

	if maxVal == 0 {
		return strings.Repeat(" ", width)
	}

	var sb strings.Builder
	sb.Grow(width * 3)
	for _, v := range buckets {
		idx := v * 8 / maxVal
		if idx > 8 {
			idx = 8
		}
		sb.WriteRune(sparkBlocks[idx])
	}
	return sb.String()
}

var sparkBlocks = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// SpinnerFrames contains the Braille spinner animation frames.
var SpinnerFrames = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
