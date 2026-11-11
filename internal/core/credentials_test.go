package core

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestProviderCredentialsMissingDoesNotCreateFiles(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LAZYAGENT_CURSOR_API_KEY", "")
	key, source, err := ResolveProviderAPIKey("cursor")
	if key != "" || source != "" || err != nil {
		t.Fatalf("missing credential: key present=%t, source=%q, error=%v", key != "", source, err)
	}
	if _, err := os.Stat(ConfigDir()); !os.IsNotExist(err) {
		t.Fatalf("lookup created the config directory: %v", err)
	}
}

func TestProviderCredentialsPersistenceAndPermissions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LAZYAGENT_CURSOR_API_KEY", "")
	if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	config := []byte(`{"window_minutes":42}`)
	if err := os.WriteFile(ConfigPath(), config, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetProviderAPIKey(" Cursor ", "  saved-cursor-key\n"); err != nil {
		t.Fatal(err)
	}
	key, source, err := ResolveProviderAPIKey("cursor")
	if err != nil || key != "saved-cursor-key" || source != CredentialsPath() {
		t.Fatalf("saved credential did not resolve: source=%q, error=%v", source, err)
	}
	if CredentialsPath() != filepath.Join(ConfigDir(), "credentials.json") {
		t.Errorf("credential path = %q", CredentialsPath())
	}
	if runtime.GOOS != "windows" {
		for path, want := range map[string]os.FileMode{ConfigDir(): 0o700, CredentialsPath(): 0o600} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != want {
				t.Fatalf("permissions for %s: info=%v, error=%v; want %o", path, info, err, want)
			}
		}
	}
	if got, err := os.ReadFile(ConfigPath()); err != nil || !bytes.Equal(got, config) {
		t.Fatalf("saving credentials changed config.json: %v", err)
	}
	if err := os.Chmod(CredentialsPath(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetProviderAPIKey("another", "another-key"); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(CredentialsPath())
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("existing credential file was not tightened: %o", info.Mode().Perm())
		}
	}
}

func TestProviderCredentialsEnvironmentPrecedence(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LAZYAGENT_CURSOR_API_KEY", "  environment-key  ")
	if err := SetProviderAPIKey("cursor", "saved-key"); err != nil {
		t.Fatal(err)
	}
	key, source, err := ResolveProviderAPIKey("cursor")
	if err != nil || key != "environment-key" || source != "LAZYAGENT_CURSOR_API_KEY" {
		t.Fatalf("environment override failed: source=%q, error=%v", source, err)
	}
	t.Setenv("LAZYAGENT_CURSOR_API_KEY", " \t")
	key, source, err = ResolveProviderAPIKey("cursor")
	if err != nil || key != "saved-key" || source != CredentialsPath() {
		t.Fatalf("empty environment did not fall back: source=%q, error=%v", source, err)
	}
	if err := RemoveProviderAPIKey("cursor"); err != nil {
		t.Fatal(err)
	}
	if key, _, err := ResolveProviderAPIKey("cursor"); err != nil || key != "" {
		t.Fatalf("saved key was not removed: present=%t, error=%v", key != "", err)
	}
	t.Setenv("LAZYAGENT_CURSOR_API_KEY", "environment-key")
	if key, _, err := ResolveProviderAPIKey("cursor"); err != nil || key != "environment-key" {
		t.Fatalf("removing saved credentials changed the environment override: %v", err)
	}
}

func TestProviderCredentialsMalformedFileIsPreserved(t *testing.T) {
	for _, content := range []string{`{`, `null`, `{}`, `{"providers":null}`, `{"providers":{"cursor":{"api_key":123}}}`} {
		t.Run(content, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("LAZYAGENT_CURSOR_API_KEY", "")
			if err := os.MkdirAll(ConfigDir(), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(CredentialsPath(), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := ResolveProviderAPIKey("cursor"); err == nil {
				t.Fatal("malformed credentials were silently ignored")
			}
			if err := SetProviderAPIKey("cursor", "replacement"); err == nil {
				t.Fatal("malformed credentials were overwritten")
			}
			if data, err := os.ReadFile(CredentialsPath()); err != nil || string(data) != content {
				t.Fatalf("malformed file was modified: %v", err)
			}
			t.Setenv("LAZYAGENT_CURSOR_API_KEY", "environment-key")
			if key, _, err := ResolveProviderAPIKey("cursor"); err != nil || key != "environment-key" {
				t.Fatalf("environment override depended on the credential file: %v", err)
			}
		})
	}
}

func TestProviderCredentialsConcurrentWritersPreserveOtherProviders(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- SetProviderAPIKey(fmt.Sprintf("provider%d", i), fmt.Sprintf("key-%d", i))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	credentials, err := readProviderCredentials()
	if err != nil || len(credentials.Providers) != n {
		t.Fatalf("concurrent updates lost providers: count=%d, error=%v", len(credentials.Providers), err)
	}
	for i := range n {
		if credentials.Providers[fmt.Sprintf("provider%d", i)].APIKey != fmt.Sprintf("key-%d", i) {
			t.Fatalf("credential update lost for provider %d", i)
		}
	}
	if err := RemoveProviderAPIKey("provider0"); err != nil {
		t.Fatal(err)
	}
	credentials, err = readProviderCredentials()
	if err != nil || len(credentials.Providers) != n-1 {
		t.Fatalf("removal affected other providers: count=%d, error=%v", len(credentials.Providers), err)
	}
}

func TestProviderCredentialsRejectInvalidProviderAndKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, provider := range []string{"", "../cursor", "cursor/api", "1cursor", "cursor_api"} {
		if err := SetProviderAPIKey(provider, "key"); err == nil {
			t.Errorf("accepted invalid provider %q", provider)
		}
	}
	for _, key := range []string{"", " \n", "first\nsecond", "first\x00second", strings.Repeat("x", 65537)} {
		if err := SetProviderAPIKey("cursor", key); err == nil {
			t.Error("accepted an invalid API key")
		}
	}
	if _, err := os.Stat(CredentialsPath()); !os.IsNotExist(err) {
		t.Fatalf("invalid input wrote credentials: %v", err)
	}
}

func TestProviderCredentialsCrossProcessWrites(t *testing.T) {
	if prefix := os.Getenv("LAZYAGENT_TEST_CREDENTIAL_PROVIDER"); prefix != "" {
		for i := range 8 {
			if err := SetProviderAPIKey(fmt.Sprintf("%s-%d", prefix, i), "child-test-key"); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("advisory file locking is not supported on this platform")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(executable, "-test.run=^TestProviderCredentialsCrossProcessWrites$")
			cmd.Env = append(os.Environ(), fmt.Sprintf("LAZYAGENT_TEST_CREDENTIAL_PROVIDER=process%d", i))
			if output, err := cmd.CombinedOutput(); err != nil {
				errs <- fmt.Errorf("credential writer failed: %w: %s", err, output)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	credentials, err := readProviderCredentials()
	if err != nil || len(credentials.Providers) != 32 {
		t.Fatalf("cross-process updates lost providers: count=%d, error=%v", len(credentials.Providers), err)
	}
}
