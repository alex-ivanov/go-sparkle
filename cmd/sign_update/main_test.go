package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	sparkle "github.com/alex-ivanov/go-sparkle"
)

// writeKey mints a keypair and writes the base64 private key to a temp file,
// as `generate_keys -x` / `sparkle keygen` do. Returns (keyFile, publicKey).
func writeKey(t *testing.T) (string, string) {
	t.Helper()
	kp, err := sparkle.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "eddsa.key")
	if err := os.WriteFile(f, []byte(kp.Private+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return f, kp.Public
}

var sigLine = regexp.MustCompile(`^sparkle:edSignature="([^"]+)" length="(\d+)"$`)

func TestSignArchiveOutputMatchesSparkle(t *testing.T) {
	keyFile, pub := writeKey(t)
	artifact := []byte("PK\x03\x04 pretend app zip contents")
	zip := filepath.Join(t.TempDir(), "App-1.0.zip")
	if err := os.WriteFile(zip, artifact, 0o644); err != nil {
		t.Fatal(err)
	}

	// Flags after the positional (Sparkle allows any order).
	opt, err := parseArgs([]string{zip, "-f", keyFile})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(opt, &out); err != nil {
		t.Fatal(err)
	}

	line := bytes.TrimSpace(out.Bytes())
	m := sigLine.FindSubmatch(line)
	if m == nil {
		t.Fatalf("output does not match Sparkle's format: %q", line)
	}
	sig, length := string(m[1]), string(m[2])
	if length != "29" && length != itoa(len(artifact)) {
		t.Fatalf("length attr = %s, want %d", length, len(artifact))
	}
	// The emitted signature must verify against the artifact, and be usable by
	// the go-sparkle client end to end.
	if err := sparkle.VerifySignature(pub, sig, artifact); err != nil {
		t.Fatalf("emitted signature does not verify: %v", err)
	}
}

func TestPrintOnlySignature(t *testing.T) {
	keyFile, pub := writeKey(t)
	data := []byte("bytes")
	f := filepath.Join(t.TempDir(), "a.zip")
	os.WriteFile(f, data, 0o644)

	opt, _ := parseArgs([]string{"-p", "-f", keyFile, f})
	var out bytes.Buffer
	if err := run(opt, &out); err != nil {
		t.Fatal(err)
	}
	sig := string(bytes.TrimSpace(out.Bytes()))
	if sigLine.MatchString(sig) {
		t.Fatal("-p should print only the bare signature, not the metadata line")
	}
	if err := sparkle.VerifySignature(pub, sig, data); err != nil {
		t.Fatalf("bare signature does not verify: %v", err)
	}
}

func TestVerifyMode(t *testing.T) {
	keyFile, _ := writeKey(t)
	data := []byte("verify me")
	f := filepath.Join(t.TempDir(), "a.zip")
	os.WriteFile(f, data, 0o644)

	// Sign, capture the signature, then verify it back through --verify.
	signOpt, _ := parseArgs([]string{"-p", "-f", keyFile, f})
	var out bytes.Buffer
	if err := run(signOpt, &out); err != nil {
		t.Fatal(err)
	}
	sig := string(bytes.TrimSpace(out.Bytes()))

	verOpt, _ := parseArgs([]string{"--verify", f, sig, "-f", keyFile})
	if err := run(verOpt, &bytes.Buffer{}); err != nil {
		t.Fatalf("valid signature failed --verify: %v", err)
	}
	// A tampered signature must fail.
	badOpt, _ := parseArgs([]string{"--verify", f, "AAAA", "-f", keyFile})
	if err := run(badOpt, &bytes.Buffer{}); err == nil {
		t.Fatal("--verify accepted a bad signature")
	}
}

func TestKeyFileFromStdinAndInterop(t *testing.T) {
	// A key file signs; the resulting feed is what the go-sparkle client would
	// consume - proving the drop-in and the client agree end to end.
	keyFile, pub := writeKey(t)
	data := []byte("interop artifact")
	f := filepath.Join(t.TempDir(), "App-2.0.zip")
	os.WriteFile(f, data, 0o644)

	opt, _ := parseArgs([]string{f, "--ed-key-file", keyFile})
	var out bytes.Buffer
	if err := run(opt, &out); err != nil {
		t.Fatal(err)
	}
	sig := string(sigLine.FindSubmatch(bytes.TrimSpace(out.Bytes()))[1])

	// Build a feed with that signature and run it through Check + verify.
	feed := sparkle.RenderAppcast("App", sparkle.FeedItem{
		ShortVersion: "2.0", Version: 20, URL: "https://x/App-2.0.zip",
		Length: int64(len(data)), EDSignature: sig, PubDate: "Mon, 02 Jan 2006 15:04:05 -0700",
	})
	items, err := sparkle.ParseAppcast([]byte(feed), nil)
	if err != nil || len(items) != 1 {
		t.Fatalf("feed parse: %v (%d items)", err, len(items))
	}
	if err := sparkle.VerifySignature(pub, items[0].EDSignature, data); err != nil {
		t.Fatalf("feed signature does not verify: %v", err)
	}
}

func TestRejectsXMLAndReleaseNotes(t *testing.T) {
	keyFile, _ := writeKey(t)
	for _, name := range []string{"appcast.xml", "notes.md", "notes.html", "notes.txt"} {
		f := filepath.Join(t.TempDir(), name)
		os.WriteFile(f, []byte("x"), 0o644)
		opt, _ := parseArgs([]string{f, "-f", keyFile})
		if err := run(opt, &bytes.Buffer{}); err == nil {
			t.Fatalf("%s should be rejected (feed/release-notes signing unsupported)", name)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// A publishing service may hand over the bare 32-byte ed25519 seed rather than
// Sparkle's 64-byte seed||public key. Signing and --verify must both work with
// it, and produce the same signature as the full key.
func TestSeedOnlyKeyFile(t *testing.T) {
	kp, err := sparkle.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	full, err := base64.StdEncoding.DecodeString(kp.Private)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	seedFile := filepath.Join(dir, "seed.key")
	seedB64 := base64.StdEncoding.EncodeToString(full[:ed25519.SeedSize])
	if err := os.WriteFile(seedFile, []byte(seedB64+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fullFile := filepath.Join(dir, "full.key")
	if err := os.WriteFile(fullFile, []byte(kp.Private+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	data := []byte("PK\x03\x04 artifact signed with a seed")
	f := filepath.Join(dir, "App-1.0.zip")
	if err := os.WriteFile(f, data, 0o644); err != nil {
		t.Fatal(err)
	}

	sign := func(keyFile string) string {
		t.Helper()
		opt, err := parseArgs([]string{"-p", "-f", keyFile, f})
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := run(opt, &out); err != nil {
			t.Fatalf("signing with %s: %v", filepath.Base(keyFile), err)
		}
		return string(bytes.TrimSpace(out.Bytes()))
	}

	seedSig, fullSig := sign(seedFile), sign(fullFile)
	if seedSig != fullSig {
		t.Fatalf("seed key signature differs from full key signature\n seed %s\n full %s", seedSig, fullSig)
	}
	if err := sparkle.VerifySignature(kp.Public, seedSig, data); err != nil {
		t.Fatalf("seed-signed artifact does not verify: %v", err)
	}

	// --verify derives the public key from the seed too.
	verOpt, _ := parseArgs([]string{"--verify", f, seedSig, "-f", seedFile})
	if err := run(verOpt, &bytes.Buffer{}); err != nil {
		t.Fatalf("--verify with a seed key failed: %v", err)
	}
}
