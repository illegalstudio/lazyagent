// Package passphrase implements the `lazyagent passphrase` subcommand:
// a small interactive utility to set or rotate the API passphrase without
// having to start the server. Useful when the user wants to change their
// passphrase, share the bearer token with a new device, or recover the
// token they forgot from a configured passphrase.
package passphrase

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/illegalstudio/lazyagent/internal/apiauth"
	"github.com/illegalstudio/lazyagent/internal/core"
)

// Run is the entry point invoked by main.go for `lazyagent passphrase ...`.
func Run(args []string) int {
	fs := flag.NewFlagSet("passphrase", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var showOnly bool
	fs.BoolVar(&showOnly, "show", false, "Print the bearer token for the current passphrase and exit (no prompt)")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `lazyagent passphrase — set or rotate the HTTP API passphrase

Usage:
  lazyagent passphrase           Prompt for a new passphrase, save it, print the bearer token
  lazyagent passphrase --show    Print the current bearer token without prompting

The passphrase lives in ~/.config/lazyagent/config.json under "api_passphrase".
The bearer token is derived from it using PBKDF2-SHA256 — every client (mobile
app, browser playground, curl) that knows the passphrase can reproduce the
same token locally. See docs/interfaces/http-api.md for the full algorithm.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg := core.LoadConfig()

	if showOnly {
		return runShow(cfg)
	}
	return runRotate(&cfg)
}

// runShow prints the bearer token for the current passphrase. The raw token
// goes to stdout (single line, no prefix) so it can be captured in a pipe:
//
//	TOKEN=$(lazyagent passphrase --show)
//
// All diagnostic context (source of the passphrase, hints on missing config)
// goes to stderr and never pollutes stdout.
func runShow(cfg core.Config) int {
	pp := strings.TrimSpace(os.Getenv(apiauth.EnvVar))
	source := apiauth.EnvVar
	if pp == "" {
		pp = strings.TrimSpace(cfg.APIPassphrase)
		source = core.ConfigPath()
	}
	if pp == "" {
		fmt.Fprintln(os.Stderr, "No passphrase configured.")
		fmt.Fprintln(os.Stderr, "Run `lazyagent passphrase` to set one.")
		return 1
	}
	fmt.Println(apiauth.DeriveToken(pp))
	fmt.Fprintf(os.Stderr, "(passphrase source: %s)\n", source)
	return 0
}

// runRotate prompts for a new passphrase and saves it. All output goes to
// stderr — same convention as the auth setup banner in main.go — so a user
// who pipes the command somewhere doesn't end up with a token in their pipe.
func runRotate(cfg *core.Config) int {
	pp, err := apiauth.PromptForNew()
	if err != nil {
		if err == apiauth.ErrNoTTY {
			fmt.Fprintln(os.Stderr, "Error: cannot prompt — stdin is not a terminal.")
			fmt.Fprintln(os.Stderr, "Edit ~/.config/lazyagent/config.json directly to change the passphrase.")
			return 1
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	cfg.APIPassphrase = pp
	if err := core.SaveConfig(*cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "Bearer token: %s\n", apiauth.DeriveToken(pp))
	fmt.Fprintf(os.Stderr, "Passphrase saved to %s\n", core.ConfigPath())
	fmt.Fprintln(os.Stderr, "Restart `lazyagent --api` for the new token to take effect.")
	return 0
}
