package sparkle

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// KeyPair is a base64-encoded ed25519 keypair for signing update artifacts.
// Public goes into the app (Sparkle SUPublicEDKey); Private stays secret with
// the release operator. The encoding matches Sparkle's generate_keys, so a
// Sparkle-minted key drops in and vice versa.
type KeyPair struct {
	Public  string // base64 of the 32-byte public key
	Private string // base64 of the 64-byte private key (seed+public)
}

// GenerateKeys mints a fresh ed25519 keypair, base64-encoded.
func GenerateKeys() (KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{
		Public:  base64.StdEncoding.EncodeToString(pub),
		Private: base64.StdEncoding.EncodeToString(priv),
	}, nil
}

// SignArtifact signs the artifact bytes with the base64 private key and returns
// the base64 signature for the appcast's sparkle:edSignature. Same EdDSA-over-
// file-bytes scheme as Sparkle's sign_update, so VerifySignature (and Sparkle
// itself) accept the result.
func SignArtifact(privKeyB64 string, data []byte) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(privKeyB64)
	if err != nil {
		return "", fmt.Errorf("decoding private key: %w", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("private key is %d bytes, want %d", len(raw), ed25519.PrivateKeySize)
	}
	sig := ed25519.Sign(ed25519.PrivateKey(raw), data)
	return base64.StdEncoding.EncodeToString(sig), nil
}
