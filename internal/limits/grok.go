package limits

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// grokAuthFile is the JSON layout of ~/.grok/auth.json: a map keyed by the
// OIDC scope ("https://auth.x.ai::<client-id>") whose value carries the bearer
// JWT under "key". Extra fields are ignored.
type grokAuthEntry struct {
	Key string `json:"key"`
}

// readGrokToken returns the OAuth bearer in this priority order:
//  1. GROK_OAUTH_TOKEN env var (override for CI / debugging)
//  2. ~/.grok/auth.json (the location the Grok CLI itself writes to)
//
// Both states "not installed" and "not logged in" surface as errAgentNotInstalled
// so the dispatcher can silently skip Grok in --agent all mode.
func readGrokToken() (string, error) {
	if v := os.Getenv("GROK_OAUTH_TOKEN"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".grok", "auth.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errAgentNotInstalled
		}
		return "", err
	}
	return readGrokTokenFromBytes(data)
}

func readGrokTokenFromBytes(data []byte) (string, error) {
	var entries map[string]grokAuthEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return "", fmt.Errorf("parse grok auth.json: %w", err)
	}
	for _, e := range entries {
		if e.Key != "" {
			return e.Key, nil
		}
	}
	return "", errAgentNotInstalled
}
