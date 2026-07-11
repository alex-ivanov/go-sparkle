package sparkle

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

// RFC 8032 section 7.1 Ed25519 test vectors. Sparkle 2.x signs updates with
// this same plain Ed25519 (a signature over the raw file bytes, with a base64
// SUPublicEDKey and sparkle:edSignature). Proving go-sparkle both VERIFIES
// these standard vectors and REPRODUCES their signatures shows it is
// wire-compatible with Sparkle's sign_update - no Sparkle binary involved, the
// interop is via the shared standard.
var rfc8032Vectors = []struct {
	name, seed, pub, msg, sig string
}{
	{
		// Verify-only known-answer (the empty-message vector); the client
		// side is what verifies a Sparkle-signed feed.
		name: "rfc8032-1-empty-message",
		pub:  "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a",
		msg:  "",
		sig:  "e5564300c360ac729086e2cc806e828a84877f1eb8e5d974d873e065224901555fb8821590a33bacc61e39701cf9b46bd25bf5f0595bbe24655141438e7a100b",
	},
	{
		name: "rfc8032-2-one-byte-message",
		seed: "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb",
		pub:  "3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c",
		msg:  "72",
		sig:  "92a009a9f0d4cab8720e820b5f642540a2b27b5416503f8fb3762223ebdb69da085ac1e43e15996e458f3613d0f11d8c387b2eaeb4302aeeb00d291612bb0c00",
	},
}

func TestRFC8032InteropVectors(t *testing.T) {
	for _, v := range rfc8032Vectors {
		t.Run(v.name, func(t *testing.T) {
			seed := mustHex(t, v.seed)
			pub := mustHex(t, v.pub)
			msg := mustHex(t, v.msg)
			wantSig := mustHex(t, v.sig)

			pubB64 := base64.StdEncoding.EncodeToString(pub)
			sigB64 := base64.StdEncoding.EncodeToString(wantSig)

			// VERIFY: go-sparkle accepts the standard vector's signature.
			if err := VerifySignature(pubB64, sigB64, msg); err != nil {
				t.Fatalf("standard vector rejected: %v", err)
			}
			// A tampered message must fail against the same signature.
			if err := VerifySignature(pubB64, sigB64, append(append([]byte{}, msg...), 0x00)); err == nil {
				t.Fatal("tampered message verified")
			}

			// SIGN (when the vector carries a seed): the seed derives the
			// vector's public key, and SignArtifact reproduces the vector's
			// signature byte-for-byte - i.e. identical output to sign_update.
			if v.seed != "" {
				priv := ed25519.NewKeyFromSeed(seed)
				if !bytes.Equal(priv.Public().(ed25519.PublicKey), pub) {
					t.Fatal("seed does not derive the vector's public key")
				}
				gotSig, err := SignArtifact(base64.StdEncoding.EncodeToString(priv), msg)
				if err != nil {
					t.Fatal(err)
				}
				if gotSig != sigB64 {
					t.Fatalf("SignArtifact != standard signature\n got  %s\n want %s", gotSig, sigB64)
				}
			}
		})
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}
