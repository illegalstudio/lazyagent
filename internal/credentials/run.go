// Package credentials manages provider API keys shared by lazyagent interfaces.
package credentials

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/illegalstudio/lazyagent/internal/core"
	"golang.org/x/term"
)

const usage = `lazyagent credentials - manage shared provider API keys

Usage:
  lazyagent credentials set PROVIDER     Read a key from a hidden prompt or stdin
  lazyagent credentials status PROVIDER  Show the active key source, never its value
  lazyagent credentials remove PROVIDER  Remove the saved key

Cursor cloud setup:
  lazyagent credentials set cursor

Keys are stored in credentials.json next to lazyagent's config.json.
LAZYAGENT_<PROVIDER>_API_KEY overrides the saved key without changing it.
Restart running lazyagent interfaces after changing credentials.
Currently only the Cursor cloud integration consumes these saved keys.
`

// Run is the entry point for lazyagent credentials.
func Run(args []string) int {
	return run(args, os.Stdin, os.Stdout, os.Stderr)
}

func run(args []string, in *os.File, out, errOut io.Writer) int {
	if len(args) == 0 || len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprint(out, usage)
		return 0
	}
	if len(args) != 2 || (args[0] != "set" && args[0] != "status" && args[0] != "remove") {
		fmt.Fprint(errOut, usage)
		return 2
	}
	provider := strings.ToLower(strings.TrimSpace(args[1]))
	env, err := core.ProviderAPIKeyEnv(provider)
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 2
	}
	switch args[0] {
	case "set":
		var data []byte
		if term.IsTerminal(int(in.Fd())) {
			fmt.Fprintf(errOut, "API key for %s: ", provider)
			data, err = term.ReadPassword(int(in.Fd()))
			fmt.Fprintln(errOut)
		} else {
			data, err = io.ReadAll(io.LimitReader(in, 65537))
		}
		if err == nil && len(data) > 65536 {
			err = fmt.Errorf("API key input exceeds 65536 bytes")
		}
		if err == nil {
			err = core.SetProviderAPIKey(provider, string(data))
		}
	case "remove":
		err = core.RemoveProviderAPIKey(provider)
	case "status":
		var key, source string
		key, source, err = core.ResolveProviderAPIKey(provider)
		if err == nil {
			if key == "" {
				fmt.Fprintf(out, "%s: not configured\n", provider)
			} else {
				fmt.Fprintf(out, "%s: configured (source: %s)\n", provider, source)
			}
		}
	}
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}
	if args[0] != "status" {
		fmt.Fprintf(errOut, "%s: saved credentials updated in %s\n", provider, core.CredentialsPath())
		if strings.TrimSpace(os.Getenv(env)) != "" {
			fmt.Fprintf(errOut, "%s is set and still overrides the saved credential.\n", env)
		}
		fmt.Fprintln(errOut, "Restart running lazyagent interfaces to apply the change.")
	}
	return 0
}
