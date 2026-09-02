// Package shares generates and verifies public share tokens.
//
// A token is 32 bytes of crypto/rand, URL-safe base64 (43 chars, no padding).
// Only its SHA-256 is ever persisted, so a database disclosure does not yield
// working links. Tokens are never derived from a file id, name, or timestamp.
package shares

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
)

// TokenBytes is the entropy per token.
const TokenBytes = 32

// ErrBadToken means the supplied token is not well-formed.
var ErrBadToken = errors.New("malformed share token")

// NewToken returns a fresh secret token and its storage hash.
func NewToken() (token, hash string, err error) {
	b := make([]byte, TokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

// HashToken returns the hex SHA-256 of a token, the value stored in the DB.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ValidTokenShape checks a token looks like one we issued before hashing it.
func ValidTokenShape(token string) bool {
	if len(token) < 40 || len(token) > 48 {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil
}
