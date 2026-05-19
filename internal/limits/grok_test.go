package limits

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadGrokTokenFromBytes_Valid(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "grok_auth.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	got, err := readGrokTokenFromBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "test-jwt-token-payload" {
		t.Errorf("got %q, want %q", got, "test-jwt-token-payload")
	}
}

func TestReadGrokTokenFromBytes_NoEntries(t *testing.T) {
	_, err := readGrokTokenFromBytes([]byte(`{}`))
	if err == nil {
		t.Fatal("expected error for empty auth.json, got nil")
	}
}

func TestReadGrokTokenFromBytes_EmptyKey(t *testing.T) {
	data := []byte(`{"https://auth.x.ai::abc":{"key":""}}`)
	_, err := readGrokTokenFromBytes(data)
	if err == nil {
		t.Fatal("expected error for empty key, got nil")
	}
}

func TestReadGrokTokenFromBytes_Malformed(t *testing.T) {
	_, err := readGrokTokenFromBytes([]byte(`not json`))
	if err == nil {
		t.Fatal("expected parse error for malformed JSON, got nil")
	}
}

func TestReadGrokToken_EnvOverride(t *testing.T) {
	t.Setenv("GROK_OAUTH_TOKEN", "env-token")
	got, err := readGrokToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "env-token" {
		t.Errorf("got %q, want %q", got, "env-token")
	}
}
