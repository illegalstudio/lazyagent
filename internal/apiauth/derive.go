// Package apiauth derives bearer tokens from passphrases and provides HTTP
// middleware that enforces them. The derivation is deterministic so that any
// client (browser playground, mobile app, curl) can produce the same token
// from the same passphrase by reimplementing the algorithm.
package apiauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

// Derivation parameters. These are part of the public protocol: every client
// that wants to compute a token from a passphrase must use these KDF settings
// plus the server's public per-install salt.
//
// SaltPrefix marks salts generated for this protocol version and allows future
// migrations without making salts ambiguous.
const (
	SaltPrefix = "lazyagent-api-v1"
	Iterations = 600_000
	KeyLength  = 32
)

// NewSalt returns a public, per-install salt for API token derivation.
func NewSalt() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return SaltPrefix + "-" + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// DeriveToken returns the bearer token for the given passphrase and salt.
// Returns the empty string when passphrase is empty (callers use this to
// detect "auth disabled / not configured").
func DeriveToken(passphrase, salt string) string {
	passphrase = strings.TrimSpace(passphrase)
	if passphrase == "" {
		return ""
	}
	salt = strings.TrimSpace(salt)
	if salt == "" {
		salt = SaltPrefix
	}
	key := pbkdf2.Key([]byte(passphrase), []byte(salt), Iterations, KeyLength, sha256.New)
	return base64.RawURLEncoding.EncodeToString(key)
}
