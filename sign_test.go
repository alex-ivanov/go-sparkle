package sparkle

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
)

// A bare 32-byte seed is a valid ed25519 private-key encoding and is what most
// non-Sparkle tooling emits; it must sign identically to the 64-byte
// seed||public form that Sparkle's generate_keys produces.
func TestSignArtifactAcceptsSeedAndFullKey(t *testing.T) {
	kp, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	full, err := base64.StdEncoding.DecodeString(kp.Private)
	if err != nil {
		t.Fatal(err)
	}
	seedB64 := base64.StdEncoding.EncodeToString(full[:ed25519.SeedSize])
	data := []byte("the update artifact bytes")

	fullSig, err := SignArtifact(kp.Private, data)
	if err != nil {
		t.Fatalf("signing with 64-byte key: %v", err)
	}
	seedSig, err := SignArtifact(seedB64, data)
	if err != nil {
		t.Fatalf("signing with 32-byte seed: %v", err)
	}
	if seedSig != fullSig {
		t.Fatalf("seed signature differs from full-key signature\n seed %s\n full %s", seedSig, fullSig)
	}
	if err := VerifySignature(kp.Public, seedSig, data); err != nil {
		t.Fatalf("seed-produced signature rejected: %v", err)
	}
}

func TestSignArtifactRejectsBadKeys(t *testing.T) {
	data := []byte("artifact")

	if _, err := SignArtifact("!!!not-base64!!!", data); err == nil {
		t.Fatal("malformed base64 key accepted")
	}
	// Correctly-encoded but neither 32 nor 64 bytes.
	for _, n := range []int{0, 16, 31, 33, 63, 65} {
		b64 := base64.StdEncoding.EncodeToString(make([]byte, n))
		_, err := SignArtifact(b64, data)
		if err == nil || !strings.Contains(err.Error(), "private key is") {
			t.Fatalf("%d-byte key not rejected: %v", n, err)
		}
	}
}
