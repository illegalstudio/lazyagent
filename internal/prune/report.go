package prune

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// printSummaryTable groups candidates by (agent, project) and prints one row
// per group with count and oldest/newest activity.
func printSummaryTable(candidates []Candidate) {
	type key struct {
		agent   string
		project string
	}
	type bucket struct {
		count  int
		oldest time.Time
		newest time.Time
	}

	buckets := make(map[key]*bucket)
	for _, c := range candidates {
		k := key{agent: c.Session.Agent, project: displayProject(c.Session.CWD)}
		b, ok := buckets[k]
		if !ok {
			b = &bucket{}
			buckets[k] = b
		}
		b.count++
		la := c.Session.LastActivity
		if b.oldest.IsZero() || la.Before(b.oldest) {
			b.oldest = la
		}
		if la.After(b.newest) {
			b.newest = la
		}
	}

	keys := make([]key, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].agent != keys[j].agent {
			return keys[i].agent < keys[j].agent
		}
		return keys[i].project < keys[j].project
	})

	rows := [][]string{{"AGENT", "PROJECT", "COUNT", "OLDEST", "NEWEST"}}
	for _, k := range keys {
		b := buckets[k]
		rows = append(rows, []string{
			k.agent,
			k.project,
			fmt.Sprintf("%d", b.count),
			formatWhen(b.oldest),
			formatWhen(b.newest),
		})
	}
	printTable(rows)
	fmt.Printf("\nTotal: %d session(s) across %d project group(s).\n", len(candidates), len(buckets))
}

// printVerboseTable prints one row per file.
func printVerboseTable(candidates []Candidate) {
	rows := [][]string{{"AGENT", "LAST ACTIVITY", "REASON", "PROJECT", "FILE"}}
	for _, c := range candidates {
		rows = append(rows, []string{
			c.Session.Agent,
			formatWhen(c.Session.LastActivity),
			strings.Join(c.Reasons, "+"),
			displayProject(c.Session.CWD),
			c.Session.JSONLPath,
		})
	}
	printTable(rows)
	fmt.Printf("\nTotal: %d session(s).\n", len(candidates))
}

func displayProject(cwd string) string {
	if cwd == "" {
		return "(unknown)"
	}
	return cwd
}

func formatWhen(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// printTable prints rows as a simple aligned table. The first row is treated
// as a header and separated by a line of dashes. Column widths grow to the
// widest value in each column, so no column is truncated.
func printTable(rows [][]string) {
	if len(rows) == 0 {
		return
	}
	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, cell := range r {
			if i >= len(widths) {
				break
			}
			if n := len(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}

	format := func(row []string) string {
		var b strings.Builder
		for i, cell := range row {
			if i > 0 {
				b.WriteString("  ")
			}
			if i == len(row)-1 {
				b.WriteString(cell)
			} else {
				b.WriteString(padRight(cell, widths[i]))
			}
		}
		return b.String()
	}

	fmt.Println(format(rows[0]))
	// separator
	var sep strings.Builder
	for i, w := range widths {
		if i > 0 {
			sep.WriteString("  ")
		}
		sep.WriteString(strings.Repeat("-", w))
	}
	fmt.Println(sep.String())
	for _, r := range rows[1:] {
		fmt.Println(format(r))
	}
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
