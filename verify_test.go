package sparkle

import (
	"strings"
	"testing"
)

// These cases mirror Sparkle's SUFeedSignatureVerifierTest (valid, missing,
// tampered, re-signed) plus defensive cases Sparkle's Swift/C layer handles
// structurally (wrong key, malformed base64, wrong length, empty key).
func TestVerifySignature(t *testing.T) {
	kp, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("the update artifact bytes")
	sig, err := SignArtifact(kp.Private, data)
	if err != nil {
		t.Fatal(err)
	}

	// Valid signature verifies.
	if err := VerifySignature(kp.Public, sig, data); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	// Tampered data (a byte inserted mid-stream, signature unchanged) fails.
	tampered := append(append([]byte{}, data[:len(data)/2]...), append([]byte{'X'}, data[len(data)/2:]...)...)
	if err := VerifySignature(kp.Public, sig, tampered); err == nil {
		t.Fatal("tampered data verified")
	}

	// Re-signing the tampered data makes it verify again.
	resig, err := SignArtifact(kp.Private, tampered)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifySignature(kp.Public, resig, tampered); err != nil {
		t.Fatalf("re-signed data rejected: %v", err)
	}

	// Missing signature fails.
	if err := VerifySignature(kp.Public, "", data); err == nil {
		t.Fatal("missing signature accepted")
	}

	// Wrong key fails.
	other, _ := GenerateKeys()
	if err := VerifySignature(other.Public, sig, data); err == nil {
		t.Fatal("wrong key verified")
	}

	// Malformed base64 in the key and in the signature both fail.
	if err := VerifySignature("!!!not-base64!!!", sig, data); err == nil {
		t.Fatal("malformed base64 key accepted")
	}
	if err := VerifySignature(kp.Public, "!!!not-base64!!!", data); err == nil {
		t.Fatal("malformed base64 signature accepted")
	}

	// A correctly-encoded but wrong-length key/signature fails cleanly.
	if err := VerifySignature("AAAA", sig, data); err == nil || !strings.Contains(err.Error(), "public key is") {
		t.Fatalf("short key not rejected: %v", err)
	}
	if err := VerifySignature(kp.Public, "AAAA", data); err == nil || !strings.Contains(err.Error(), "signature is") {
		t.Fatalf("short signature not rejected: %v", err)
	}
}
