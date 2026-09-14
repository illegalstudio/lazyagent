package kimi

// Kimi Code CLI scopes its OAuth credentials per (oauthHost, baseUrl)
// environment. Only a mainland-CN login on the default endpoints lands in the
// legacy `credentials/kimi-code.json` slot; every other login — a global (.ai)
// login included — lands in `credentials/kimi-code-env-<sha256[:16]>.json`,
// with the slot name recorded in config.toml as the provider's oauth key:
//
//	[providers."managed:kimi-code"]
//	base_url = "https://api.kimi.ai/coding/v1"
//
//	[providers."managed:kimi-code".oauth]
//	key = "oauth/kimi-code-env-0e4f99c69cc27850"
//	oauth_host = "https://auth.kimi.ai"
//
// Credential and endpoint discovery therefore reads config.toml first, then
// falls back to the legacy slot, then to the freshest slot on disk.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Kimi Code's two regional deployments: mainland China (.com) and global (.ai).
const (
	MainlandCodingBaseURL = "https://api.kimi.com/coding/v1"
	GlobalCodingBaseURL   = "https://api.kimi.ai/coding/v1"
	MainlandOAuthHost     = "https://auth.kimi.com"
	GlobalOAuthHost       = "https://auth.kimi.ai"
)

// managedProviderTable is the config.toml table Kimi Code CLI writes for its
// managed (OAuth) provider; its `.oauth` sub-table names the credential slot.
const managedProviderTable = `providers."managed:kimi-code"`

// legacyCredentialsFile is the slot a mainland-CN login on the default
// endpoints writes to.
const legacyCredentialsFile = "kimi-code.json"

// ManagedProvider is the subset of ~/.kimi-code/config.toml lazyagent needs to
// locate credentials and endpoints. Zero values mean "not configured".
type ManagedProvider struct {
	BaseURL   string
	OAuthKey  string
	OAuthHost string
}

// ConfigPath returns the Kimi Code CLI config file path.
func ConfigPath() string {
	root := ShareDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "config.toml")
}

// RegionMarkerPath returns the install-channel region marker file, written by
// Kimi's install scripts and consulted when no login has been persisted yet.
func RegionMarkerPath() string {
	root := ShareDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "region")
}

// CredentialsDir returns the directory holding Kimi Code OAuth credential slots.
func CredentialsDir() string {
	root := ShareDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "credentials")
}

// ReadManagedProvider reads the managed Kimi Code provider settings from
// config.toml. A missing or unreadable config yields the zero value.
func ReadManagedProvider() ManagedProvider {
	path := ConfigPath()
	if path == "" {
		return ManagedProvider{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ManagedProvider{}
	}
	return parseManagedProvider(data)
}

// parseManagedProvider scans config.toml for the handful of keys lazyagent
// needs. It is deliberately a scanner and not a TOML implementation: Kimi
// writes these tables itself, one flat `key = "value"` per line.
func parseManagedProvider(data []byte) ManagedProvider {
	var provider ManagedProvider
	section := ""
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			end := strings.Index(line, "]")
			if end < 0 {
				continue
			}
			section = normalizeTableName(line[1:end])
			continue
		}
		key, value, ok := parseTOMLPair(line)
		if !ok {
			continue
		}
		switch section {
		case managedProviderTable:
			if key == "base_url" {
				provider.BaseURL = value
			}
		case managedProviderTable + ".oauth":
			switch key {
			case "key":
				provider.OAuthKey = value
			case "oauth_host":
				provider.OAuthHost = value
			}
		}
	}
	return provider
}

// normalizeTableName strips whitespace from a table header's dotted parts so
// `[ providers."managed:kimi-code" . oauth ]` compares equal to the canonical
// spelling. Quoted parts keep their quotes — that is how Kimi writes them.
func normalizeTableName(header string) string {
	parts := splitOutsideQuotes(header, '.')
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return strings.Join(parts, ".")
}

// parseTOMLPair splits a `key = value` line, unquoting the value and dropping a
// trailing comment. It reports false for lines that are not a scalar pair.
func parseTOMLPair(line string) (key, value string, ok bool) {
	eq := strings.Index(line, "=")
	if eq < 0 {
		return "", "", false
	}
	key = strings.Trim(strings.TrimSpace(line[:eq]), `"'`)
	value = strings.TrimSpace(line[eq+1:])
	if key == "" || value == "" {
		return "", "", false
	}
	if quote := value[0]; quote == '"' || quote == '\'' {
		if end := strings.IndexByte(value[1:], quote); end >= 0 {
			return key, value[1 : 1+end], true
		}
		return "", "", false
	}
	if hash := strings.IndexByte(value, '#'); hash >= 0 {
		value = strings.TrimSpace(value[:hash])
	}
	return key, value, value != ""
}

// splitOutsideQuotes splits on sep, ignoring separators inside double quotes.
func splitOutsideQuotes(s string, sep byte) []string {
	var parts []string
	start, quoted := 0, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			quoted = !quoted
		case sep:
			if !quoted {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}

// Environment is a credential slot together with the endpoints that slot was
// issued for. Kimi scopes credentials per (oauth host, base URL), so the two
// must be resolved as a pair: refreshing a global token against the mainland
// OAuth host, or spending it on the mainland usage endpoint, fails.
type Environment struct {
	CredentialsPath string
	BaseURL         string
	OAuthHost       string
}

// ResolveEnvironment picks the credential slot lazyagent should read and the
// endpoints that go with it:
//
//  1. the slot named by config.toml's oauth key, with config.toml's endpoints
//  2. the legacy `kimi-code.json` slot, which by Kimi's own rule exists only
//     for a mainland-CN login on the default endpoints
//  3. the freshest `kimi-code-env-*.json` slot, with the endpoints whose hash
//     reproduces that slot's name
//
// When no slot is found it names the legacy path, so callers reporting "not
// logged in" still name a concrete file. KIMI_CODE_BASE_URL and
// KIMI_CODE_OAUTH_HOST / KIMI_OAUTH_HOST override the resolved endpoints last,
// the way Kimi Code CLI honours them.
func ResolveEnvironment() Environment {
	env := resolveSlot(ReadManagedProvider())
	if v := envEndpoint("KIMI_CODE_BASE_URL"); v != "" {
		env.BaseURL = v
	}
	if v := envEndpoint("KIMI_CODE_OAUTH_HOST", "KIMI_OAUTH_HOST"); v != "" {
		env.OAuthHost = v
	}
	return env
}

func resolveSlot(provider ManagedProvider) Environment {
	dir := CredentialsDir()
	if dir == "" {
		return Environment{BaseURL: regionBaseURL(provider), OAuthHost: regionOAuthHost(provider)}
	}
	configured := Environment{
		BaseURL:   regionBaseURL(provider),
		OAuthHost: regionOAuthHost(provider),
	}

	// The configured slot and the configured endpoints belong together.
	if name := credentialSlotFile(provider.OAuthKey); name != "" {
		if path := filepath.Join(dir, name); fileExists(path) {
			configured.CredentialsPath = path
			return configured
		}
	}

	// Kimi writes the legacy slot only for a mainland-CN login on the default
	// endpoints, so that file identifies its own environment.
	legacy := filepath.Join(dir, legacyCredentialsFile)
	if fileExists(legacy) {
		return Environment{
			CredentialsPath: legacy,
			BaseURL:         MainlandCodingBaseURL,
			OAuthHost:       MainlandOAuthHost,
		}
	}

	// A scoped slot is named after the hash of the endpoints it was issued for,
	// so the endpoints can be recovered by recomputing the hash of each
	// environment this install could plausibly be logged in to.
	if path := freshestCredentialSlot(dir); path != "" {
		env := Environment{CredentialsPath: path}
		if base, host, ok := endpointsForSlot(filepath.Base(path), provider); ok {
			env.BaseURL, env.OAuthHost = base, host
			return env
		}
		// An unrecognized environment (a private deployment, say): the
		// configured endpoints are the only information left.
		env.BaseURL, env.OAuthHost = configured.BaseURL, configured.OAuthHost
		return env
	}

	configured.CredentialsPath = legacy
	return configured
}

// endpointsForSlot identifies which (base URL, OAuth host) pair a scoped slot
// belongs to by reproducing the name Kimi derives from that pair.
func endpointsForSlot(name string, provider ManagedProvider) (baseURL, oauthHost string, ok bool) {
	candidates := [][2]string{
		{MainlandCodingBaseURL, MainlandOAuthHost},
		{GlobalCodingBaseURL, GlobalOAuthHost},
		{GlobalCodingBaseURL, MainlandOAuthHost},
		{MainlandCodingBaseURL, GlobalOAuthHost},
	}
	if base, host := trimEndpoint(provider.BaseURL), trimEndpoint(provider.OAuthHost); base != "" && host != "" {
		candidates = append([][2]string{{base, host}}, candidates...)
	}
	for _, candidate := range candidates {
		if scopedSlotFile(candidate[1], candidate[0]) == name {
			return candidate[0], candidate[1], true
		}
	}
	return "", "", false
}

// scopedSlotFile reproduces Kimi's credential slot name for an environment:
// the first 16 hex digits of sha256 over `{"oauthHost":…,"baseUrl":…}`, exactly
// as resolveKimiCodeOAuthKey builds it in Kimi Code CLI.
func scopedSlotFile(oauthHost, baseURL string) string {
	oauthHost, baseURL = trimEndpoint(oauthHost), trimEndpoint(baseURL)
	if oauthHost == MainlandOAuthHost && baseURL == MainlandCodingBaseURL {
		return legacyCredentialsFile
	}
	payload := fmt.Sprintf(`{"oauthHost":%s,"baseUrl":%s}`, jsonString(oauthHost), jsonString(baseURL))
	sum := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("kimi-code-env-%s.json", hex.EncodeToString(sum[:])[:16])
}

func jsonString(s string) string {
	encoded, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(encoded)
}

// credentialSlotFile maps a config.toml oauth key ("oauth/kimi-code-env-…") to
// the file name Kimi's file storage writes under the credentials directory. It
// returns "" for keys that do not resolve to a plain file name.
func credentialSlotFile(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	base := key
	if idx := strings.LastIndexAny(base, `/\`); idx >= 0 {
		base = base[idx+1:]
	}
	if base == "" || base == "." || base == ".." || strings.HasPrefix(base, ".") {
		return ""
	}
	return base + ".json"
}

// freshestCredentialSlot returns the scoped slot with the latest expiry — the
// one the CLI most recently refreshed — or "" when there is none.
func freshestCredentialSlot(dir string) string {
	matches, err := filepath.Glob(filepath.Join(dir, "kimi-code-env-*.json"))
	if err != nil {
		return ""
	}
	best, bestExpiry := "", int64(-1)
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var slot struct {
			AccessToken string `json:"access_token"`
			ExpiresAt   int64  `json:"expires_at"`
		}
		if err := json.Unmarshal(data, &slot); err != nil || slot.AccessToken == "" {
			continue
		}
		if slot.ExpiresAt > bestExpiry {
			best, bestExpiry = path, slot.ExpiresAt
		}
	}
	return best
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// CredentialsPath returns the credential slot lazyagent should read.
func CredentialsPath() string {
	return ResolveEnvironment().CredentialsPath
}

// OAuthHost returns the Kimi OAuth host the resolved credentials belong to.
func OAuthHost() string {
	return ResolveEnvironment().OAuthHost
}

// CodingBaseURL returns the managed Kimi Code API base URL the resolved
// credentials belong to.
func CodingBaseURL() string {
	return ResolveEnvironment().BaseURL
}

// regionBaseURL derives a base URL from config.toml, then from the region
// implied by the persisted OAuth host or the install marker.
func regionBaseURL(provider ManagedProvider) string {
	if base := trimEndpoint(provider.BaseURL); base != "" {
		return base
	}
	if trimEndpoint(provider.OAuthHost) == GlobalOAuthHost || isGlobalRegion() {
		return GlobalCodingBaseURL
	}
	return MainlandCodingBaseURL
}

// regionOAuthHost derives an OAuth host from config.toml, then from the region
// implied by the configured base URL or the install marker.
func regionOAuthHost(provider ManagedProvider) string {
	if host := trimEndpoint(provider.OAuthHost); host != "" {
		return host
	}
	if trimEndpoint(provider.BaseURL) == GlobalCodingBaseURL || isGlobalRegion() {
		return GlobalOAuthHost
	}
	return MainlandOAuthHost
}

func envEndpoint(keys ...string) string {
	for _, key := range keys {
		if v := trimEndpoint(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

func trimEndpoint(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

// isGlobalRegion reports whether the install-channel marker says this machine
// was installed from the global (.ai) channel.
func isGlobalRegion() bool {
	path := RegionMarkerPath()
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(string(data)), "global")
}
