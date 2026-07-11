package sparkle

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// VerifySignature checks an EdDSA (ed25519) signature the way Sparkle does:
// pubKeyB64 is base64 of the 32-byte public key (SUPublicEDKey); sigB64 is
// base64 of the 64-byte signature over the raw artifact bytes (an item's
// sparkle:edSignature). It returns a descriptive error on any failure so a
// caller never treats an unverified artifact as good.
func VerifySignature(pubKeyB64, sigB64 string, data []byte) error {
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pubKeyB64))
	if err != nil {
		return fmt.Errorf("decoding public key: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("public key is %d bytes, want %d", len(pub), ed25519.PublicKeySize)
	}
	if strings.TrimSpace(sigB64) == "" {
		return errors.New("artifact has no signature")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil {
		return fmt.Errorf("decoding signature: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("signature is %d bytes, want %d", len(sig), ed25519.SignatureSize)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), data, sig) {
		return errors.New("signature does not verify against the trusted key")
	}
	return nil
}
