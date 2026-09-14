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
	"encoding/json"
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

// CredentialsPath returns the Kimi Code OAuth credential file lazyagent should
// read, resolved in order:
//
//  1. the slot named by config.toml's oauth key, when that file exists
//  2. the legacy `kimi-code.json` slot, when it exists
//  3. the freshest `kimi-code*.json` slot on disk
//
// When nothing is found it returns the legacy path, so callers reporting "not
// logged in" still name a concrete file.
func CredentialsPath() string {
	dir := CredentialsDir()
	if dir == "" {
		return ""
	}
	legacy := filepath.Join(dir, legacyCredentialsFile)
	if name := credentialSlotFile(ReadManagedProvider().OAuthKey); name != "" {
		if path := filepath.Join(dir, name); fileExists(path) {
			return path
		}
	}
	if fileExists(legacy) {
		return legacy
	}
	if path := freshestCredentialSlot(dir); path != "" {
		return path
	}
	return legacy
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

// freshestCredentialSlot returns the kimi-code*.json slot with the latest
// expiry — the one the CLI most recently refreshed — or "" when there is none.
func freshestCredentialSlot(dir string) string {
	matches, err := filepath.Glob(filepath.Join(dir, "kimi-code*.json"))
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

// OAuthHost returns the Kimi OAuth host for this install, resolved in Kimi's
// own order: env override, persisted login, region marker, mainland default.
func OAuthHost() string {
	for _, key := range []string{"KIMI_CODE_OAUTH_HOST", "KIMI_OAUTH_HOST"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return strings.TrimRight(v, "/")
		}
	}
	if host := strings.TrimSpace(ReadManagedProvider().OAuthHost); host != "" {
		return strings.TrimRight(host, "/")
	}
	if isGlobalRegion() {
		return GlobalOAuthHost
	}
	return MainlandOAuthHost
}

// CodingBaseURL returns the managed Kimi Code API base URL for this install:
// the KIMI_CODE_BASE_URL override, then config.toml, then the region implied by
// the persisted OAuth host or the install marker, then the mainland default.
func CodingBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("KIMI_CODE_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	provider := ReadManagedProvider()
	if base := strings.TrimSpace(provider.BaseURL); base != "" {
		return strings.TrimRight(base, "/")
	}
	if strings.TrimRight(strings.TrimSpace(provider.OAuthHost), "/") == GlobalOAuthHost || isGlobalRegion() {
		return GlobalCodingBaseURL
	}
	return MainlandCodingBaseURL
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
