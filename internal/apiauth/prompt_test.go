package apiauth

import "testing"

// The TTY branch of ResolvePassphrase is hard to test in an automated way
// (it requires a real terminal for term.ReadPassword). These tests cover
// the two non-TTY branches: env-var precedence and the configured-value
// fallback. The TTY branch is left to manual testing.

func TestResolvePassphraseEnvVarBeatsConfigured(t *testing.T) {
	t.Setenv(EnvVar, "from-env")
	pp, fromPrompt, err := ResolvePassphrase("from-config")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pp != "from-env" {
		t.Errorf("passphrase = %q, want %q (env should win)", pp, "from-env")
	}
	if fromPrompt {
		t.Error("fromPrompt = true, want false (env-var path)")
	}
}

func TestResolvePassphraseEnvVarTrimsWhitespace(t *testing.T) {
	t.Setenv(EnvVar, "  spaced  ")
	pp, _, err := ResolvePassphrase("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pp != "spaced" {
		t.Errorf("passphrase = %q, want %q (env trimmed)", pp, "spaced")
	}
}

func TestResolvePassphraseEnvVarBlankFallsThrough(t *testing.T) {
	// Whitespace-only env var must be treated as unset, otherwise the user
	// could accidentally export an empty value and get past the "no
	// passphrase configured" guard.
	t.Setenv(EnvVar, "   ")
	pp, _, err := ResolvePassphrase("from-config")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pp != "from-config" {
		t.Errorf("passphrase = %q, want %q (whitespace env should fall through)", pp, "from-config")
	}
}

func TestResolvePassphraseConfiguredWhenEnvUnset(t *testing.T) {
	t.Setenv(EnvVar, "")
	pp, fromPrompt, err := ResolvePassphrase("from-config")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pp != "from-config" {
		t.Errorf("passphrase = %q, want %q", pp, "from-config")
	}
	if fromPrompt {
		t.Error("fromPrompt = true, want false (configured path)")
	}
}

func TestResolvePassphraseConfiguredTrimsWhitespace(t *testing.T) {
	t.Setenv(EnvVar, "")
	pp, _, err := ResolvePassphrase("  spaced  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pp != "spaced" {
		t.Errorf("passphrase = %q, want %q (configured trimmed)", pp, "spaced")
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
