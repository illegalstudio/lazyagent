package search

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/illegalstudio/lazyagent/internal/chatops"
	"github.com/illegalstudio/lazyagent/internal/core"
)

var (
	styleSearchHeader = lipgloss.NewStyle().
				Bold(true).
				Foreground(chatops.ColTextBright).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(chatops.ColPrimary).
				Padding(0, 1).
				Align(lipgloss.Center)
	styleSection = lipgloss.NewStyle().Bold(true).Foreground(chatops.ColPrimary)
	styleCard    = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(chatops.ColBorderDim).
			Padding(1, 2).
			MarginBottom(1)
	styleCardTitle = lipgloss.NewStyle().Bold(true).Foreground(chatops.ColTextBright)
	styleSnippet   = lipgloss.NewStyle().Foreground(chatops.ColTextSubtle)
	styleHighlight = lipgloss.NewStyle().Bold(true).Foreground(chatops.ColDarkText).Background(chatops.ColZen)
	styleIndex     = lipgloss.NewStyle().Bold(true).Foreground(chatops.ColDarkText).Background(chatops.ColPrimary).Padding(0, 1)
	styleRole      = lipgloss.NewStyle().Bold(true).Foreground(chatops.ColZen)
	styleMeta      = lipgloss.NewStyle().Foreground(chatops.ColTextDim)
	styleQuote     = lipgloss.NewStyle().Foreground(chatops.ColBorderDim)
)

func groupHits(hits []hit, snippetsPerSession int) []sessionResult {
	bySession := make(map[string]*sessionResult)
	var order []string
	for _, h := range hits {
		key := h.Agent + "\x00" + h.SessionID
		r, ok := bySession[key]
		if !ok {
			r = &sessionResult{
				Agent:     h.Agent,
				SessionID: h.SessionID,
				CWD:       h.CWD,
				Name:      h.Name,
				LastHit:   h.Timestamp,
				BestRank:  h.Rank,
			}
			bySession[key] = r
			order = append(order, key)
		}
		r.Matches++
		if h.Timestamp.After(r.LastHit) {
			r.LastHit = h.Timestamp
		}
		if h.Rank < r.BestRank {
			r.BestRank = h.Rank
		}
		if len(r.Snippets) < snippetsPerSession {
			r.Snippets = append(r.Snippets, h)
		}
	}

	results := make([]sessionResult, 0, len(order))
	for _, key := range order {
		results = append(results, *bySession[key])
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].BestRank != results[j].BestRank {
			return results[i].BestRank < results[j].BestRank
		}
		return results[i].LastHit.After(results[j].LastHit)
	})
	return results
}

func renderResults(results []sessionResult, query string) {
	terms := searchTerms(query)
	if len(results) == 0 {
		fmt.Println(chatops.StyleMuted.Render("No chat sessions matched your search."))
		return
	}

	width := renderWidth()
	fmt.Println(styleSearchHeader.Width(width).Render(fmt.Sprintf("%d matching chats for %q", len(results), query)))
	fmt.Println()
	renderSummaryTable(results, width)
	fmt.Println()
	fmt.Println(styleSection.Render("Matching chats"))
	for idx, result := range results {
		fmt.Println(renderResultCard(idx+1, result, terms, width))
	}
}

func renderSummaryTable(results []sessionResult, width int) {
	t := chatops.NewTable().Headers("#", "AGENT", "CHAT", "HITS", "LAST", "PROJECT")
	chatW := 34
	projectW := width - 76
	if projectW < 18 {
		projectW = 18
	}
	for i, result := range results {
		t.Row(
			fmt.Sprintf("%d", i+1),
			strings.ToUpper(result.Agent),
			core.ShortName(resultName(result), chatW),
			fmt.Sprintf("%d", result.Matches),
			formatWhen(result.LastHit),
			core.ShortName(shortProject(result.CWD), projectW),
		)
	}
	fmt.Println(t)
}

func renderResultCard(index int, result sessionResult, terms []string, width int) string {
	contentWidth := width - 8
	if contentWidth < 50 {
		contentWidth = 50
	}

	headerLeft := strings.Join([]string{
		styleIndex.Render(fmt.Sprintf("%02d", index)),
		agentBadge(result.Agent),
		styleCardTitle.Render(core.ShortName(resultName(result), contentWidth-28)),
	}, " ")
	headerRight := styleMeta.Render(formatWhen(result.LastHit))
	header := lipgloss.JoinHorizontal(
		lipgloss.Top,
		headerLeft,
		flexibleGap(headerLeft, headerRight, contentWidth),
		headerRight,
	)
	if result.CWD != "" {
		header += "\n" + styleMeta.Render(core.ShortName(result.CWD, contentWidth))
	}

	var lines []string
	for _, snip := range result.Snippets {
		role := snip.Role
		if role == "" {
			role = "message"
		}
		meta := strings.Join([]string{
			styleRole.Render(role),
			styleMeta.Render(formatWhen(snip.Timestamp)),
		}, " ")
		body := renderSnippet(snip.Text, terms, contentWidth-4)
		lines = append(lines, meta+"\n"+quoteBlock(body))
	}
	if len(lines) == 0 {
		lines = append(lines, chatops.StyleMuted.Render("No snippet text available."))
	}

	return styleCard.Width(width).Render(header + "\n\n" + strings.Join(lines, "\n\n"))
}

func agentBadge(agent string) string {
	return chatops.StyleAgent.Render(strings.ToUpper(agent))
}

func resultName(r sessionResult) string {
	name := strings.TrimSpace(r.Name)
	if name == "" {
		name = filepath.Base(r.CWD)
	}
	if name == "." || name == "/" || name == "" {
		name = r.SessionID
	}
	return name
}

func shortProject(cwd string) string {
	if cwd == "" {
		return "unknown"
	}
	parent := filepath.Base(filepath.Dir(cwd))
	base := filepath.Base(cwd)
	if parent == "." || parent == "/" || parent == "" {
		return base
	}
	return filepath.Join(parent, base)
}

func renderWidth() int {
	width := 100
	if cols, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && cols > 0 {
		width = cols - 4
	}
	if width < 72 {
		return 72
	}
	if width > 140 {
		return 140
	}
	return width
}

func flexibleGap(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return strings.Repeat(" ", gap)
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func quoteBlock(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = styleQuote.Render("│") + " " + lines[i]
	}
	return strings.Join(lines, "\n")
}

func formatWhen(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	now := time.Now()
	if t.After(now.Add(-24 * time.Hour)) {
		return t.Format("15:04")
	}
	if t.Year() == now.Year() {
		return t.Format("Jan 02")
	}
	return t.Format("2006-01-02")
}

func renderSnippet(text string, terms []string, width int) string {
	if width < 32 {
		width = 32
	}
	plain := makeSnippet(text, terms, width*3)
	lines := wrapText(plain, width)
	for i := range lines {
		lines[i] = styleSnippet.Render(highlight(lines[i], terms))
	}
	return strings.Join(lines, "\n")
}

func makeSnippet(text string, terms []string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return ""
	}
	lower := strings.ToLower(string(runes))
	pos := -1
	for _, term := range terms {
		if idx := strings.Index(lower, strings.ToLower(term)); idx >= 0 && (pos < 0 || idx < pos) {
			pos = idx
		}
	}
	start := 0
	if pos > 0 {
		prefixRunes := len([]rune(string([]byte(lower[:pos]))))
		start = prefixRunes - maxRunes/3
		if start < 0 {
			start = 0
		}
	}
	end := start + maxRunes
	if end > len(runes) {
		end = len(runes)
	}
	snippet := string(runes[start:end])
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(runes) {
		snippet += "..."
	}
	return snippet
}

func wrapText(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	var current strings.Builder
	for _, word := range words {
		wordLen := len([]rune(word))
		curLen := len([]rune(current.String()))
		if curLen > 0 && curLen+1+wordLen > width {
			lines = append(lines, current.String())
			current.Reset()
		}
		if wordLen > width {
			if current.Len() > 0 {
				lines = append(lines, current.String())
				current.Reset()
			}
			chunks := chunkWord(word, width)
			lines = append(lines, chunks[:len(chunks)-1]...)
			current.WriteString(chunks[len(chunks)-1])
			continue
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(word)
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

func chunkWord(word string, width int) []string {
	runes := []rune(word)
	var chunks []string
	for len(runes) > width {
		chunks = append(chunks, string(runes[:width]))
		runes = runes[width:]
	}
	chunks = append(chunks, string(runes))
	return chunks
}

func highlight(text string, terms []string) string {
	if len(terms) == 0 {
		return text
	}
	var out strings.Builder
	i := 0
	lower := strings.ToLower(text)
	for i < len(text) {
		matchStart, matchEnd := -1, -1
		for _, term := range terms {
			if term == "" {
				continue
			}
			if strings.HasPrefix(lower[i:], strings.ToLower(term)) {
				matchStart, matchEnd = i, i+len(term)
				break
			}
		}
		if matchStart >= 0 {
			out.WriteString(styleHighlight.Render(text[matchStart:matchEnd]))
			i = matchEnd
			continue
		}
		out.WriteByte(text[i])
		i++
	}
	return out.String()
}
