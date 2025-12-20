package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// GeneratePKCE generates a PKCE code verifier and its S256 challenge.
// The verifier is a random 32-byte string (base64url-encoded to ~43 chars).
// The challenge is the SHA256 hash of the verifier, also base64url-encoded.
func GeneratePKCE() (verifier, challenge string, err error) {
	// Generate 32 random bytes for the verifier
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}

	verifier = base64.RawURLEncoding.EncodeToString(b)

	// Create S256 challenge: BASE64URL(SHA256(verifier))
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])

	return verifier, challenge, nil
}
