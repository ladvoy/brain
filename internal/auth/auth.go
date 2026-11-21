// Package auth issues and verifies project plane tokens (sha256 hashed at rest).
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// Hash returns sha256(token) as raw bytes for storage.
func Hash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// NewToken returns a random 32-byte hex token.
func NewToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Bearer extracts the token from Authorization: Bearer ….
func Bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const p = "Bearer "
	if !strings.HasPrefix(h, p) && !strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return ""
	}
	// case-insensitive "Bearer "
	idx := strings.Index(h, " ")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(h[idx+1:])
}

// EqualHash reports whether plaintext matches a stored hash.
func EqualHash(plaintext string, hash []byte) bool {
	if plaintext == "" || len(hash) == 0 {
		return false
	}
	got := Hash(plaintext)
	if len(got) != len(hash) {
		return false
	}
	var v byte
	for i := range got {
		v |= got[i] ^ hash[i]
	}
	return v == 0
}
