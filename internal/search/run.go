package search

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/mattn/go-isatty"
)

type options struct {
	agent    string
	limit    int
	snippets int
	reindex  bool
	dbPath   string
}

func Run(args []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var opts options
	fs.StringVar(&opts.agent, "agent", "all", "Agent to search: claude,codex,pi,amp,all")
	fs.IntVar(&opts.limit, "limit", 20, "Maximum chat sessions to show")
	fs.IntVar(&opts.snippets, "snippets", 2, "Maximum snippets per chat session")
	fs.BoolVar(&opts.reindex, "reindex", false, "Rebuild the local search index before searching")
	fs.StringVar(&opts.dbPath, "db", "", "Override search index path (for testing)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `lazyagent search — search chat transcripts

Usage:
  lazyagent search [query] [flags]
  lazyagent search --agent codex "parser bug"

If query is omitted, lazyagent prompts for it.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(normalizeArgs(args)); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if opts.limit <= 0 {
		fmt.Fprintln(os.Stderr, "Error: --limit must be > 0")
		return 2
	}
	if opts.snippets <= 0 {
		fmt.Fprintln(os.Stderr, "Error: --snippets must be > 0")
		return 2
	}

	query, err := resolveQuery(fs.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}
	query = strings.TrimSpace(query)
	if query == "" {
		fmt.Fprintln(os.Stderr, "Error: empty search query")
		return 2
	}

	agents, err := resolveAgents(opts.agent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}

	idx, err := openIndex(opts.dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: open search index: %v\n", err)
		return 1
	}
	defer idx.close()

	stats := indexAgents(idx, agents, opts.reindex)
	for _, warning := range stats.Warnings {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", warning)
	}

	hits, err := idx.search(query, agents, opts.limit*opts.snippets*4)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: search index: %v\n", err)
		return 1
	}
	results := groupHits(hits, opts.snippets)
	if len(results) > opts.limit {
		results = results[:opts.limit]
	}
	renderResults(results, query)
	return 0
}

func resolveQuery(args []string) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		data, err := io.ReadAll(os.Stdin)
		return string(data), err
	}
	fmt.Print("Search: ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return line, nil
}

func resolveAgents(arg string) ([]string, error) {
	arg = strings.TrimSpace(strings.ToLower(arg))
	if arg == "" || arg == "all" {
		return append([]string(nil), supportedAgents...), nil
	}
	parts := strings.Split(arg, ",")
	var out []string
	seen := make(map[string]bool)
	for _, part := range parts {
		agent := strings.TrimSpace(part)
		if agent == "" {
			continue
		}
		if !slices.Contains(supportedAgents, agent) {
			return nil, fmt.Errorf("unsupported agent %q (supported: %s)", agent, strings.Join(supportedAgents, ", "))
		}
		if !seen[agent] {
			seen[agent] = true
			out = append(out, agent)
		}
	}
	return out, nil
}

func normalizeArgs(args []string) []string {
	valueFlags := map[string]bool{
		"-agent":     true,
		"--agent":    true,
		"-limit":     true,
		"--limit":    true,
		"-snippets":  true,
		"--snippets": true,
		"-db":        true,
		"--db":       true,
	}
	boolFlags := map[string]bool{
		"-reindex":  true,
		"--reindex": true,
		"-h":        true,
		"--help":    true,
	}
	var flags []string
	var query []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name := arg
		if before, _, ok := strings.Cut(arg, "="); ok {
			name = before
		}
		switch {
		case valueFlags[name] && strings.Contains(arg, "="):
			flags = append(flags, arg)
		case valueFlags[arg] && i+1 < len(args):
			flags = append(flags, arg, args[i+1])
			i++
		case boolFlags[arg]:
			flags = append(flags, arg)
		default:
			query = append(query, arg)
		}
	}
	return append(flags, query...)
}
