package kimi

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const globalConfig = `default_model = "kimi-code/k3"

[providers."managed:kimi-code"]
type = "kimi"
api_key = ""
base_url = "https://api.kimi.ai/coding/v1"

[providers."managed:kimi-code".oauth]
storage = "file"
key = "oauth/kimi-code-env-0e4f99c69cc27850"
oauth_host = "https://auth.kimi.ai"

[services.moonshot_search]
base_url = "https://api.kimi.ai/coding/v1/search"
`

func TestParseManagedProvider(t *testing.T) {
	got := parseManagedProvider([]byte(globalConfig))
	want := ManagedProvider{
		BaseURL:   "https://api.kimi.ai/coding/v1",
		OAuthKey:  "oauth/kimi-code-env-0e4f99c69cc27850",
		OAuthHost: "https://auth.kimi.ai",
	}
	if got != want {
		t.Fatalf("parseManagedProvider() = %+v, want %+v", got, want)
	}
}

func TestParseManagedProvider_IgnoresOtherTables(t *testing.T) {
	config := `# comment
[providers."managed:other"]
base_url = "https://example.invalid/v1"

[services.moonshot_fetch.oauth]
key = "oauth/not-ours"
`
	if got := parseManagedProvider([]byte(config)); got != (ManagedProvider{}) {
		t.Fatalf("parseManagedProvider() = %+v, want zero value", got)
	}
}

func TestCredentialsPath_ScopedSlotFromConfig(t *testing.T) {
	root := kimiHome(t)
	writeFile(t, filepath.Join(root, "config.toml"), globalConfig)
	writeFile(t, filepath.Join(root, "credentials", "kimi-code.json"), `{"access_token":"legacy","expires_at":10}`)
	scoped := filepath.Join(root, "credentials", "kimi-code-env-0e4f99c69cc27850.json")
	writeFile(t, scoped, `{"access_token":"scoped","expires_at":5}`)

	if got := CredentialsPath(); got != scoped {
		t.Fatalf("CredentialsPath() = %q, want %q", got, scoped)
	}
}

func TestCredentialsPath_LegacyWhenConfigSlotMissing(t *testing.T) {
	root := kimiHome(t)
	writeFile(t, filepath.Join(root, "config.toml"), globalConfig)
	legacy := filepath.Join(root, "credentials", "kimi-code.json")
	writeFile(t, legacy, `{"access_token":"legacy"}`)

	if got := CredentialsPath(); got != legacy {
		t.Fatalf("CredentialsPath() = %q, want %q", got, legacy)
	}
}

// With no config to name a slot, the freshest login on disk wins.
func TestCredentialsPath_FreshestSlot(t *testing.T) {
	root := kimiHome(t)
	writeFile(t, filepath.Join(root, "credentials", "kimi-code-env-aaaa.json"), `{"access_token":"old","expires_at":100}`)
	fresh := filepath.Join(root, "credentials", "kimi-code-env-bbbb.json")
	writeFile(t, fresh, `{"access_token":"new","expires_at":200}`)

	if got := CredentialsPath(); got != fresh {
		t.Fatalf("CredentialsPath() = %q, want %q", got, fresh)
	}
}

func TestCredentialsPath_FallsBackToLegacyName(t *testing.T) {
	root := kimiHome(t)
	want := filepath.Join(root, "credentials", "kimi-code.json")
	if got := CredentialsPath(); got != want {
		t.Fatalf("CredentialsPath() = %q, want %q", got, want)
	}
}

func TestCredentialSlotFile(t *testing.T) {
	cases := map[string]string{
		"oauth/kimi-code":              "kimi-code.json",
		"oauth/kimi-code-env-abc123":   "kimi-code-env-abc123.json",
		"kimi-code":                    "kimi-code.json",
		"":                             "",
		"oauth/..":                     "",
		"oauth/.hidden":                "",
		"oauth/nested/kimi-code-env-x": "kimi-code-env-x.json",
	}
	for key, want := range cases {
		if got := credentialSlotFile(key); got != want {
			t.Errorf("credentialSlotFile(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestCodingBaseURL(t *testing.T) {
	t.Run("env override wins", func(t *testing.T) {
		kimiHome(t)
		t.Setenv("KIMI_CODE_BASE_URL", "http://127.0.0.1:1234/coding/v1/")
		if got := CodingBaseURL(); got != "http://127.0.0.1:1234/coding/v1" {
			t.Fatalf("CodingBaseURL() = %q", got)
		}
	})
	t.Run("config base url", func(t *testing.T) {
		root := kimiHome(t)
		writeFile(t, filepath.Join(root, "config.toml"), globalConfig)
		if got := CodingBaseURL(); got != GlobalCodingBaseURL {
			t.Fatalf("CodingBaseURL() = %q, want %q", got, GlobalCodingBaseURL)
		}
	})
	t.Run("region marker", func(t *testing.T) {
		root := kimiHome(t)
		writeFile(t, filepath.Join(root, "region"), "global\n")
		if got := CodingBaseURL(); got != GlobalCodingBaseURL {
			t.Fatalf("CodingBaseURL() = %q, want %q", got, GlobalCodingBaseURL)
		}
	})
	t.Run("mainland default", func(t *testing.T) {
		kimiHome(t)
		if got := CodingBaseURL(); got != MainlandCodingBaseURL {
			t.Fatalf("CodingBaseURL() = %q, want %q", got, MainlandCodingBaseURL)
		}
	})
}

func TestOAuthHost(t *testing.T) {
	t.Run("config host", func(t *testing.T) {
		root := kimiHome(t)
		writeFile(t, filepath.Join(root, "config.toml"), globalConfig)
		if got := OAuthHost(); got != GlobalOAuthHost {
			t.Fatalf("OAuthHost() = %q, want %q", got, GlobalOAuthHost)
		}
	})
	t.Run("env override", func(t *testing.T) {
		root := kimiHome(t)
		writeFile(t, filepath.Join(root, "config.toml"), globalConfig)
		t.Setenv("KIMI_CODE_OAUTH_HOST", "http://127.0.0.1:9999/")
		if got := OAuthHost(); got != "http://127.0.0.1:9999" {
			t.Fatalf("OAuthHost() = %q", got)
		}
	})
	t.Run("mainland default", func(t *testing.T) {
		kimiHome(t)
		if got := OAuthHost(); got != MainlandOAuthHost {
			t.Fatalf("OAuthHost() = %q, want %q", got, MainlandOAuthHost)
		}
	})
}

// kimiHome points Kimi discovery at an empty temporary share dir and clears the
// environment overrides that would otherwise leak in from the host machine.
func kimiHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("KIMI_SHARE_DIR", root)
	t.Setenv("KIMI_CODE_BASE_URL", "")
	t.Setenv("KIMI_CODE_OAUTH_HOST", "")
	t.Setenv("KIMI_OAUTH_HOST", "")
	return root
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(fmt.Errorf("write %s: %w", path, err))
	}
}
