// Command sign_update is a drop-in replacement for Sparkle's sign_update tool
// (github.com/sparkle-project/Sparkle), in pure Go. Tools and CI that expect
// Sparkle's binary can call this one: the flags, key format, and output match.
//
//	sign_update [--account NAME] [-f|--ed-key-file FILE] [-s KEY] [-p] <archive>
//	sign_update --verify <archive> <base64-signature> [-f FILE]
//
// The EdDSA (ed25519) private key comes from --ed-key-file (a base64 key file
// holding either the 64-byte seed||public key that Sparkle's generate_keys
// emits or a bare 32-byte seed, or "-" to read from stdin), the deprecated -s
// inline key, or - on
// macOS with no key given - the login Keychain (service
// https://sparkle-project.org, the --account name, default "ed25519"), exactly
// where Sparkle's generate_keys stores it. Signing an archive prints:
//
//	sparkle:edSignature="<base64>" length="<bytes>"
//
// Not implemented (Sparkle uses these mainly inside generate_appcast): signing
// XML appcast feeds in place and the release-notes (.md/.html/.txt) signing
// warnings. Sign archives/pkgs/deltas here; use the appcast writer for feeds.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	sparkle "github.com/alex-ivanov/go-sparkle"
)

const keychainService = "https://sparkle-project.org"

type options struct {
	account            string
	edKeyFile          string
	inlineKey          string // -s (deprecated)
	printOnlySignature bool   // -p
	verify             bool
	positionals        []string
}

func main() {
	opt, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if err := run(opt, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// parseArgs hand-parses flags so they may appear in any position (like
// Sparkle's swift-argument-parser), supporting -x val and -x=val forms.
func parseArgs(args []string) (options, error) {
	opt := options{account: "ed25519"}
	i := 0
	value := func(flag, inline string) (string, error) {
		if inline != "" {
			return inline, nil
		}
		i++
		if i >= len(args) {
			return "", fmt.Errorf("%s needs a value", flag)
		}
		return args[i], nil
	}
	for ; i < len(args); i++ {
		a := args[i]
		name, inline := a, ""
		if eq := strings.IndexByte(a, '='); strings.HasPrefix(a, "-") && eq >= 0 {
			name, inline = a[:eq], a[eq+1:]
		}
		var err error
		switch name {
		case "-h", "--help":
			usage(os.Stdout)
			os.Exit(0)
		case "-f", "--ed-key-file":
			opt.edKeyFile, err = value(name, inline)
		case "-s":
			opt.inlineKey, err = value(name, inline)
		case "--account":
			opt.account, err = value(name, inline)
		case "-p":
			opt.printOnlySignature = true
		case "--verify":
			opt.verify = true
		case "--disable-signing-warning":
			// accepted for compatibility; only affects xml/release-notes signing
		default:
			if strings.HasPrefix(a, "-") && a != "-" {
				return opt, fmt.Errorf("unknown option %q", a)
			}
			opt.positionals = append(opt.positionals, a)
		}
		if err != nil {
			return opt, err
		}
	}
	return opt, nil
}

func run(opt options, out io.Writer) error {
	if len(opt.positionals) == 0 {
		usage(os.Stderr)
		return fmt.Errorf("missing <archive> argument")
	}
	filePath := opt.positionals[0]
	if isFeedOrReleaseNotes(filePath) {
		return fmt.Errorf("signing xml feeds and release-note files is not supported by this tool; sign archives/pkgs/deltas, and render feeds with the go-sparkle appcast writer")
	}

	privB64, err := resolveKey(opt)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	if opt.verify {
		if len(opt.positionals) < 2 {
			return fmt.Errorf("--verify needs the base64 signature as a second argument")
		}
		pubB64, err := publicFromPrivate(privB64)
		if err != nil {
			return err
		}
		if err := sparkle.VerifySignature(pubB64, opt.positionals[1], data); err != nil {
			return fmt.Errorf("failed to pass signing verification: %w", err)
		}
		return nil
	}

	sig, err := sparkle.SignArtifact(privB64, data)
	if err != nil {
		return err
	}
	if opt.printOnlySignature {
		fmt.Fprintln(out, sig)
	} else {
		fmt.Fprintf(out, "sparkle:edSignature=%q length=\"%d\"\n", sig, len(data))
	}
	return nil
}

// resolveKey mirrors Sparkle's precedence: -s (deprecated) then --ed-key-file
// (file or "-" stdin) then the login Keychain (macOS).
func resolveKey(opt options) (string, error) {
	switch {
	case opt.inlineKey != "":
		fmt.Fprintln(os.Stderr, "Warning: the -s option for passing the private EdDSA key is insecure and deprecated; use --ed-key-file.")
		return strings.TrimSpace(opt.inlineKey), nil
	case opt.edKeyFile == "-":
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading EdDSA private key from stdin: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	case opt.edKeyFile != "":
		b, err := os.ReadFile(opt.edKeyFile)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	default:
		key, err := keychainKey(opt.account)
		if err != nil {
			return "", fmt.Errorf("no signing key: %w\nProvide one with --ed-key-file <file>, or run generate_keys", err)
		}
		return key, nil
	}
}

// publicFromPrivate extracts the base64 public key from a base64 ed25519
// private key, for --verify. Accepts both the 64-byte seed||public encoding
// and the bare 32-byte seed, matching sparkle.SignArtifact.
func publicFromPrivate(privB64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(privB64))
	if err != nil {
		return "", fmt.Errorf("decoding private key: %w", err)
	}
	var priv ed25519.PrivateKey
	switch len(raw) {
	case ed25519.SeedSize:
		priv = ed25519.NewKeyFromSeed(raw)
	case ed25519.PrivateKeySize:
		priv = ed25519.PrivateKey(raw)
	default:
		return "", fmt.Errorf("private key is %d bytes, want %d or %d", len(raw), ed25519.SeedSize, ed25519.PrivateKeySize)
	}
	pub := priv.Public().(ed25519.PublicKey)
	return base64.StdEncoding.EncodeToString(pub), nil
}

func isFeedOrReleaseNotes(path string) bool {
	p := strings.ToLower(path)
	for _, ext := range []string{".xml", ".txt", ".md", ".markdown", ".html", ".htm"} {
		if strings.HasSuffix(p, ext) {
			return true
		}
	}
	return false
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `sign_update - sign an update archive with an EdDSA (ed25519) key (Sparkle-compatible)

  sign_update [--account NAME] [-f|--ed-key-file FILE] [-p] <archive>
  sign_update --verify <archive> <base64-signature> [-f FILE]

  -f, --ed-key-file FILE   base64 ed25519 private key file ("-" = read from stdin)
  -s KEY                   (deprecated) inline base64 private key
  --account NAME           Keychain account for the key (macOS; default "ed25519")
  -p                       print only the signature (no metadata)
  --verify                 verify <archive> against a base64 signature

Signing an archive prints:  sparkle:edSignature="<sig>" length="<bytes>"`)
}
