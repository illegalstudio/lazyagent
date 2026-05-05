package apiauth

import "testing"

func TestDeriveTokenDeterministic(t *testing.T) {
	a := DeriveToken("pippo")
	b := DeriveToken("pippo")
	if a != b {
		t.Fatalf("non-deterministic: %q vs %q", a, b)
	}
}

func TestDeriveTokenDifferentPassphrases(t *testing.T) {
	if DeriveToken("pippo") == DeriveToken("pluto") {
		t.Fatal("different passphrases produced same token")
	}
}

func TestDeriveTokenLength(t *testing.T) {
	tok := DeriveToken("anything")
	// 32 bytes -> 43 chars in base64url without padding.
	if len(tok) != 43 {
		t.Fatalf("token length = %d, want 43", len(tok))
	}
}

func TestDeriveTokenEmpty(t *testing.T) {
	if DeriveToken("") != "" {
		t.Fatal("empty passphrase should derive empty token")
	}
	if DeriveToken("   ") != "" {
		t.Fatal("whitespace-only passphrase should derive empty token")
	}
}

func TestDeriveTokenTrimsWhitespace(t *testing.T) {
	if DeriveToken("hello") != DeriveToken("  hello  ") {
		t.Fatal("token differs based on surrounding whitespace")
	}
}

// TestDeriveTokenKnownVector pins the algorithm so accidental parameter changes
// are caught. Clients (mobile app, browser) can use this vector to verify their
// implementation matches the server.
//
// Vector:
//   passphrase = "pippo"
//   salt       = "lazyagent-api-v1"
//   iterations = 600_000
//   hash       = SHA-256
//   length     = 32 bytes -> base64url (no padding)
func TestDeriveTokenKnownVector(t *testing.T) {
	const want = "zqh9_r0QeYpLiLSQGZMYriIWqNZgZOu3Qc_l7wtraV4"
	got := DeriveToken("pippo")
	if got != want {
		t.Fatalf("token for %q = %q, want %q (algorithm changed?)", "pippo", got, want)
	}
}
