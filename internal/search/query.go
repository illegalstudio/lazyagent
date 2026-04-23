package search

import (
	"strings"
	"unicode"
)

func ftsQuery(q string) string {
	terms := searchTerms(q)
	if len(terms) == 0 {
		return ""
	}
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		parts = append(parts, term+"*")
	}
	return strings.Join(parts, " AND ")
}

func searchTerms(q string) []string {
	seen := make(map[string]bool)
	var terms []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		term := strings.ToLower(b.String())
		b.Reset()
		if !seen[term] {
			seen[term] = true
			terms = append(terms, term)
		}
	}
	for _, r := range q {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		flush()
	}
	flush()
	return terms
}
