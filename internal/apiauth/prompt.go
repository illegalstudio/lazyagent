package apiauth

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// EnvVar is the environment variable name that overrides the configured
// passphrase. Useful for CI / non-interactive deployments where editing the
// config file isn't convenient. The env var value is never written to disk.
const EnvVar = "LAZYAGENT_API_PASSPHRASE"

// MinPassphraseLength is the soft minimum below which we warn the user.
// Not enforced — short passphrases produce valid (just weaker) tokens.
const MinPassphraseLength = 8

// ErrNoTTY is returned when no passphrase is configured and stdin is not a
// terminal, so we can't prompt for one. The caller should print actionable
// guidance and exit.
var ErrNoTTY = errors.New("no passphrase configured and stdin is not a terminal")

// ResolvePassphrase returns the passphrase to use, in priority order:
//  1. the LAZYAGENT_API_PASSPHRASE env var, if set
//  2. the configured value, if non-empty
//  3. an interactive prompt, if stdin is a TTY
//
// If none of those apply, ErrNoTTY is returned. The boolean second return is
// true when the passphrase came from the interactive prompt and should be
// persisted to the config file by the caller.
func ResolvePassphrase(configured string) (passphrase string, fromPrompt bool, err error) {
	if env := strings.TrimSpace(os.Getenv(EnvVar)); env != "" {
		return env, false, nil
	}
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured, false, nil
	}
	pp, err := PromptForNew()
	if err != nil {
		return "", false, err
	}
	return pp, true, nil
}

// PromptForNew always prompts the user for a new passphrase (double-entry
// confirmation), regardless of what's in the env or config. Used by the
// `lazyagent passphrase` subcommand to rotate the passphrase. Returns
// ErrNoTTY when stdin isn't a terminal.
func PromptForNew() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", ErrNoTTY
	}
	return promptPassphraseTwice(os.Stdin, os.Stderr)
}

// promptPassphraseTwice asks for the passphrase, then asks again for
// confirmation. It loops until both entries match and the result is non-empty.
// Reads from in (which must be a terminal); writes prompts to out.
func promptPassphraseTwice(in *os.File, out io.Writer) (string, error) {
	fmt.Fprintln(out, "No API passphrase is configured. Set one now.")
	fmt.Fprintln(out, "It will be saved to the config file and used to derive your bearer token.")
	fmt.Fprintln(out)

	for {
		fmt.Fprint(out, "Enter API passphrase: ")
		first, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(out)
		if err != nil {
			return "", fmt.Errorf("read passphrase: %w", err)
		}
		first = []byte(strings.TrimSpace(string(first)))
		if len(first) == 0 {
			fmt.Fprintln(out, "Passphrase cannot be empty. Try again.")
			continue
		}

		fmt.Fprint(out, "Confirm passphrase:   ")
		second, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(out)
		if err != nil {
			return "", fmt.Errorf("read passphrase: %w", err)
		}
		second = []byte(strings.TrimSpace(string(second)))

		if string(first) != string(second) {
			fmt.Fprintln(out, "Passphrases do not match. Try again.")
			continue
		}
		if len(first) < MinPassphraseLength {
			fmt.Fprintf(out, "Warning: passphrase is shorter than %d characters. A longer passphrase is harder to brute-force.\n\n", MinPassphraseLength)
		}
		return string(first), nil
	}
}
