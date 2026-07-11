// Command sparkle is the release-side tool for go-sparkle: mint the signing
// keypair, sign an update artifact, and render a Sparkle appcast - the same
// jobs Sparkle's generate_keys + sign_update binaries do, in pure Go (no
// Sparkle install required). The output (base64 ed25519 keys/signatures,
// Sparkle RSS) is wire-compatible with Sparkle and with the go-sparkle client.
//
//	sparkle keygen  [--out sparkle-update.key]
//	sparkle sign    --key KEY <artifact>
//	sparkle appcast --key KEY --url URL --build N --short X.Y --date RFC1123Z [flags] <artifact>
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	sparkle "github.com/alex-ivanov/go-sparkle"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Args[2:])
	case "sign":
		err = sign(os.Args[2:])
	case "appcast":
		err = appcast(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`sparkle - sign updates + render appcasts (Sparkle-compatible ed25519)

  sparkle keygen  [--out sparkle-update.key]
  sparkle sign    --key KEY <artifact>
  sparkle appcast --key KEY --url URL --build N --short X.Y --date RFC1123Z \
                  [--name NAME] [--title T] [--notes FILE] [--channel C] [--min-os V] <artifact>

keygen writes the base64 PRIVATE key to --out (0600) and prints the PUBLIC key
to embed in the app (Sparkle SUPublicEDKey). URL is the artifact's download
URL. build is the monotonic CFBundleVersion; short is the human version. date
is RFC1123Z (e.g. from ` + "`date -R`" + `).`)
}

func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("out", "sparkle-update.key", "file to write the base64 private key")
	_ = fs.Parse(args)
	kp, err := sparkle.GenerateKeys()
	if err != nil {
		return err
	}
	if _, err := os.Stat(*out); err == nil {
		return fmt.Errorf("%s already exists - refusing to overwrite a signing key", *out)
	}
	if err := os.WriteFile(*out, []byte(kp.Private+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Printf("Private key written to %s (keep it secret).\n", *out)
	fmt.Printf("Public key (embed in the app as SUPublicEDKey):\n\n  %s\n\n", kp.Public)
	return nil
}

func sign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	key := fs.String("key", "", "base64 private key file")
	_ = fs.Parse(args)
	if *key == "" || fs.NArg() != 1 {
		return fmt.Errorf("usage: sparkle sign --key KEY <artifact>")
	}
	sig, err := signFile(*key, fs.Arg(0))
	if err != nil {
		return err
	}
	fmt.Println(sig)
	return nil
}

func appcast(args []string) error {
	fs := flag.NewFlagSet("appcast", flag.ExitOnError)
	key := fs.String("key", "", "base64 private key file")
	dlURL := fs.String("url", "", "download URL of the artifact")
	build := fs.Int("build", 0, "monotonic build number (CFBundleVersion)")
	short := fs.String("short", "", "human version (CFBundleShortVersionString)")
	name := fs.String("name", "App", "product name (appcast channel title)")
	title := fs.String("title", "", "item title (default: <name> <short>)")
	notesFile := fs.String("notes", "", "release-notes file (plain text)")
	channel := fs.String("channel", "", "sparkle:channel (optional, e.g. beta)")
	minOS := fs.String("min-os", "", "sparkle:minimumSystemVersion (optional)")
	pubDate := fs.String("date", "", "RFC1123Z pubDate (required)")
	_ = fs.Parse(args)
	if *key == "" || *dlURL == "" || *build <= 0 || *short == "" || fs.NArg() != 1 {
		return fmt.Errorf("usage: sparkle appcast --key KEY --url URL --build N --short X.Y --date RFC1123Z <artifact>")
	}
	if *pubDate == "" {
		return fmt.Errorf("--date is required (RFC1123Z, e.g. from `date -R`) - the tool avoids the wall clock for reproducibility")
	}
	if _, err := time.Parse(time.RFC1123Z, *pubDate); err != nil {
		return fmt.Errorf("--date must be RFC1123Z: %w", err)
	}
	artifact := fs.Arg(0)
	info, err := os.Stat(artifact)
	if err != nil {
		return err
	}
	sig, err := signFile(*key, artifact)
	if err != nil {
		return err
	}
	notes := ""
	if *notesFile != "" {
		b, err := os.ReadFile(*notesFile)
		if err != nil {
			return err
		}
		notes = strings.TrimSpace(string(b))
	}
	fmt.Print(sparkle.RenderAppcast(*name, sparkle.FeedItem{
		Title:        *title,
		ShortVersion: *short,
		Version:      *build,
		Channel:      *channel,
		Notes:        notes,
		PubDate:      *pubDate,
		URL:          *dlURL,
		Length:       info.Size(),
		EDSignature:  sig,
		MinimumOS:    *minOS,
	}))
	return nil
}

func signFile(keyFile, artifact string) (string, error) {
	privRaw, err := os.ReadFile(keyFile)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(artifact)
	if err != nil {
		return "", err
	}
	return sparkle.SignArtifact(strings.TrimSpace(string(privRaw)), data)
}
