package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/illegalstudio/lazyagent/internal/diskcache"
)

// Provider credentials are separate from Config so they cannot be returned by
// configuration endpoints or GUI bindings. Interfaces can share this resolver.
type providerCredential struct {
	APIKey string `json:"api_key"`
}

type providerCredentials struct {
	Providers map[string]providerCredential `json:"providers"`
}

var credentialsMu sync.Mutex

// CredentialsPath is shared by CLI, TUI, GUI and HTTP API consumers.
func CredentialsPath() string {
	if dir := ConfigDir(); dir != "" {
		return filepath.Join(dir, "credentials.json")
	}
	return ""
}

func normalizeCredentialProvider(provider string) (string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	for i, ch := range provider {
		if ch >= 'a' && ch <= 'z' || i > 0 && (ch >= '0' && ch <= '9' || ch == '-') {
			continue
		}
		return "", errors.New("provider must start with a letter and contain only letters, digits or hyphens")
	}
	if provider == "" {
		return "", errors.New("provider is required")
	}
	return provider, nil
}

// ProviderAPIKeyEnv returns the optional environment override for a provider.
func ProviderAPIKeyEnv(provider string) (string, error) {
	provider, err := normalizeCredentialProvider(provider)
	if err != nil {
		return "", err
	}
	return "LAZYAGENT_" + strings.ToUpper(strings.ReplaceAll(provider, "-", "_")) + "_API_KEY", nil
}

func readProviderCredentials() (providerCredentials, error) {
	credentials := providerCredentials{Providers: make(map[string]providerCredential)}
	path := CredentialsPath()
	if path == "" {
		return credentials, errors.New("credentials directory unavailable")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return credentials, nil
	}
	if err != nil {
		return credentials, fmt.Errorf("read provider credentials: %w", err)
	}
	var decoded providerCredentials
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Providers == nil {
		// Do not include parser errors: malformed input can contain a secret.
		return credentials, errors.New("invalid credentials.json; expected a providers object with api_key values")
	}
	return decoded, nil
}

// ResolveProviderAPIKey prefers a nonempty environment override over the saved
// credential. Source is the environment variable or file path, never the key.
// Reading a missing credentials file does not create it.
func ResolveProviderAPIKey(provider string) (key, source string, err error) {
	provider, err = normalizeCredentialProvider(provider)
	if err != nil {
		return "", "", err
	}
	env, _ := ProviderAPIKeyEnv(provider)
	if key = strings.TrimSpace(os.Getenv(env)); key != "" {
		return key, env, nil
	}
	credentials, err := readProviderCredentials()
	if err != nil {
		return "", "", err
	}
	if key = strings.TrimSpace(credentials.Providers[provider].APIKey); key != "" {
		return key, CredentialsPath(), nil
	}
	return "", "", nil
}

// updateProviderCredentials preserves other providers and serializes writers
// with an in-process mutex and the same advisory locking used for config.json.
func updateProviderCredentials(mutate func(*providerCredentials)) error {
	credentialsMu.Lock()
	defer credentialsMu.Unlock()
	dir := ConfigDir()
	if dir == "" {
		return errors.New("credentials directory unavailable")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "credentials.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := flockExclusive(lock); err != nil {
		return err
	}
	defer flockUnlock(lock)
	credentials, err := readProviderCredentials()
	if err != nil {
		return err
	}
	mutate(&credentials)
	return diskcache.AtomicWriteJSON(CredentialsPath(), credentials)
}

// SetProviderAPIKey saves a provider key without copying environment overrides.
func SetProviderAPIKey(provider, key string) error {
	provider, err := normalizeCredentialProvider(provider)
	if err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, "\r\n\x00") || len(key) > 65536 {
		return errors.New("API key must be a nonempty single line of at most 65536 bytes")
	}
	return updateProviderCredentials(func(c *providerCredentials) {
		c.Providers[provider] = providerCredential{APIKey: key}
	})
}

// RemoveProviderAPIKey removes only the saved key, not an environment override.
func RemoveProviderAPIKey(provider string) error {
	provider, err := normalizeCredentialProvider(provider)
	if err != nil {
		return err
	}
	return updateProviderCredentials(func(c *providerCredentials) {
		delete(c.Providers, provider)
	})
}
