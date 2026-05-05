// Package apiauth derives bearer tokens from passphrases and provides HTTP
// middleware that enforces them. The derivation is deterministic so that any
// client (browser playground, mobile app, curl) can produce the same token
// from the same passphrase by reimplementing the algorithm.
package apiauth

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

// Derivation parameters. These are part of the public protocol — every client
// that wants to compute a token from a passphrase MUST use exactly these
// values, otherwise the derived token will not match what the server expects.
//
// The "-v1" suffix in the salt allows future migrations: a "-v2" can be
// introduced without breaking existing v1 clients.
const (
	Salt       = "lazyagent-api-v1"
	Iterations = 600_000
	KeyLength  = 32
)

// DeriveToken returns the bearer token for the given passphrase.
// Returns the empty string when passphrase is empty (callers use this to
// detect "auth disabled / not configured").
func DeriveToken(passphrase string) string {
	passphrase = strings.TrimSpace(passphrase)
	if passphrase == "" {
		return ""
	}
	key := pbkdf2.Key([]byte(passphrase), []byte(Salt), Iterations, KeyLength, sha256.New)
	return base64.RawURLEncoding.EncodeToString(key)
}
