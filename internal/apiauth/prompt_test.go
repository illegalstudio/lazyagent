package apiauth

import (
	"errors"
	"testing"
)

// The TTY branch of ResolvePassphrase is hard to test in an automated way
// (it requires a real terminal for term.ReadPassword). These tests cover
// the two non-TTY branches: env-var precedence and the configured-value
// fallback. The TTY branch is left to manual testing.

func TestResolvePassphraseEnvVarBeatsConfigured(t *testing.T) {
	t.Setenv(EnvVar, "from-env-passphrase")
	pp, fromPrompt, err := ResolvePassphrase("from-config-passphrase")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pp != "from-env-passphrase" {
		t.Errorf("passphrase = %q, want %q (env should win)", pp, "from-env-passphrase")
	}
	if fromPrompt {
		t.Error("fromPrompt = true, want false (env-var path)")
	}
}

func TestResolvePassphraseEnvVarTrimsWhitespace(t *testing.T) {
	t.Setenv(EnvVar, "  spaced-passphrase  ")
	pp, _, err := ResolvePassphrase("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pp != "spaced-passphrase" {
		t.Errorf("passphrase = %q, want %q (env trimmed)", pp, "spaced-passphrase")
	}
}

func TestResolvePassphraseEnvVarBlankFallsThrough(t *testing.T) {
	// Whitespace-only env var must be treated as unset, otherwise the user
	// could accidentally export an empty value and get past the "no
	// passphrase configured" guard.
	t.Setenv(EnvVar, "   ")
	pp, _, err := ResolvePassphrase("from-config-passphrase")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pp != "from-config-passphrase" {
		t.Errorf("passphrase = %q, want %q (whitespace env should fall through)", pp, "from-config-passphrase")
	}
}

func TestResolvePassphraseConfiguredWhenEnvUnset(t *testing.T) {
	t.Setenv(EnvVar, "")
	pp, fromPrompt, err := ResolvePassphrase("from-config-passphrase")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pp != "from-config-passphrase" {
		t.Errorf("passphrase = %q, want %q", pp, "from-config-passphrase")
	}
	if fromPrompt {
		t.Error("fromPrompt = true, want false (configured path)")
	}
}

func TestResolvePassphraseConfiguredTrimsWhitespace(t *testing.T) {
	t.Setenv(EnvVar, "")
	pp, _, err := ResolvePassphrase("  spaced-passphrase  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pp != "spaced-passphrase" {
		t.Errorf("passphrase = %q, want %q (configured trimmed)", pp, "spaced-passphrase")
	}
}

// When nothing is configured and there's no TTY (the standard test runner
// state), ResolvePassphrase must surface ErrNoTTY rather than block on
// stdin or return a misleading empty passphrase.
func TestResolvePassphraseNoSourcesReturnsErrNoTTY(t *testing.T) {
	t.Setenv(EnvVar, "")
	_, _, err := ResolvePassphrase("")
	if err != ErrNoTTY {
		t.Fatalf("err = %v, want ErrNoTTY", err)
	}
}

func TestResolvePassphraseRejectsShortEnvVar(t *testing.T) {
	t.Setenv(EnvVar, "short")
	_, _, err := ResolvePassphrase("from-config-passphrase")
	if !errors.Is(err, ErrWeakPassphrase) {
		t.Fatalf("err = %v, want ErrWeakPassphrase", err)
	}
}

func TestResolvePassphraseRejectsShortConfigured(t *testing.T) {
	t.Setenv(EnvVar, "")
	_, _, err := ResolvePassphrase("short")
	if !errors.Is(err, ErrWeakPassphrase) {
		t.Fatalf("err = %v, want ErrWeakPassphrase", err)
	}
}
