package webhook

import "testing"

func TestSign_KnownVector(t *testing.T) {
	// HMAC-SHA256("it's a secret", `{"foo":"bar"}`) formatted as sha256=<hex>.
	// Vector verified with: echo -n '{"foo":"bar"}' | openssl dgst -sha256 -hmac "it's a secret"
	const want = "sha256=98e87fdc5126c604e0faff20d289f1cefbcc6816ee9ebb60451278d96751ce80"
	got := Sign("it's a secret", []byte(`{"foo":"bar"}`))
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSign_EmptySecret(t *testing.T) {
	if Sign("", []byte("x")) == "" {
		t.Fatal("Sign with empty secret should still return a valid signature string")
	}
}
