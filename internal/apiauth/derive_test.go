package apiauth

import "testing"

func TestDeriveTokenDeterministic(t *testing.T) {
	a := DeriveToken("pippo", SaltPrefix)
	b := DeriveToken("pippo", SaltPrefix)
	if a != b {
		t.Fatalf("non-deterministic: %q vs %q", a, b)
	}
}

func TestDeriveTokenDifferentPassphrases(t *testing.T) {
	if DeriveToken("pippo", SaltPrefix) == DeriveToken("pluto", SaltPrefix) {
		t.Fatal("different passphrases produced same token")
	}
}

func TestDeriveTokenLength(t *testing.T) {
	tok := DeriveToken("anything", SaltPrefix)
	// 32 bytes -> 43 chars in base64url without padding.
	if len(tok) != 43 {
		t.Fatalf("token length = %d, want 43", len(tok))
	}
}

func TestDeriveTokenEmpty(t *testing.T) {
	if DeriveToken("", SaltPrefix) != "" {
		t.Fatal("empty passphrase should derive empty token")
	}
	if DeriveToken("   ", SaltPrefix) != "" {
		t.Fatal("whitespace-only passphrase should derive empty token")
	}
}

func TestDeriveTokenTrimsWhitespace(t *testing.T) {
	if DeriveToken("hello", SaltPrefix) != DeriveToken("  hello  ", SaltPrefix) {
		t.Fatal("token differs based on surrounding whitespace")
	}
}

func TestDeriveTokenDifferentSalts(t *testing.T) {
	if DeriveToken("same-passphrase", "salt-a") == DeriveToken("same-passphrase", "salt-b") {
		t.Fatal("different salts produced same token")
	}
}

func TestNewSalt(t *testing.T) {
	a, err := NewSalt()
	if err != nil {
		t.Fatalf("NewSalt: %v", err)
	}
	b, err := NewSalt()
	if err != nil {
		t.Fatalf("NewSalt: %v", err)
	}
	if a == b {
		t.Fatal("NewSalt returned duplicate salts")
	}
	if len(a) <= len(SaltPrefix)+1 || a[:len(SaltPrefix)+1] != SaltPrefix+"-" {
		t.Fatalf("salt = %q, want %q prefix", a, SaltPrefix+"-")
	}
}

// TestDeriveTokenKnownVector pins the algorithm so accidental parameter changes
// are caught. Clients (mobile app, browser) can use this vector to verify their
// implementation matches the server.
//
// Vector:
//
//	passphrase = "pippo"
//	salt       = "lazyagent-api-v1"
//	iterations = 600_000
//	hash       = SHA-256
//	length     = 32 bytes -> base64url (no padding)
func TestDeriveTokenKnownVector(t *testing.T) {
	const want = "zqh9_r0QeYpLiLSQGZMYriIWqNZgZOu3Qc_l7wtraV4"
	got := DeriveToken("pippo", SaltPrefix)
	if got != want {
		t.Fatalf("token for %q = %q, want %q (algorithm changed?)", "pippo", got, want)
	}
}
