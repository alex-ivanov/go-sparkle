package sparkle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestExpandFeedURL(t *testing.T) {
	got := ExpandFeedURL("https://x/appcast?token=<TOKEN>&installed=<CFBundleVersion>", "a/b+c=d", 42)
	if strings.Contains(got, "a/b+c=d") {
		t.Fatalf("token not escaped: %s", got)
	}
	if !strings.Contains(got, "installed=42") {
		t.Fatalf("version not substituted: %s", got)
	}
}

// End-to-end against a token-gated feed + download: the token must arrive via
// query param OR Authorization: Bearer.
func TestCheckAndDownloadEndToEnd(t *testing.T) {
	kp, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	artifact := []byte("PK\x03\x04 pretend app zip")
	sig, err := SignArtifact(kp.Private, artifact)
	if err != nil {
		t.Fatal(err)
	}

	const wantToken = "secret-token"
	var artifactURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.URL.Query().Get("token")
		if tok == "" {
			tok = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if tok != wantToken {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/appcast":
			if r.URL.Query().Get("installed") != "10" {
				t.Errorf("installed version not sent: %q", r.URL.RawQuery)
			}
			w.Write([]byte(RenderAppcast("App", FeedItem{
				ShortVersion: "0.2.0", Version: 11, URL: artifactURL,
				Length: int64(len(artifact)), EDSignature: sig, PubDate: "Mon, 02 Jan 2006 15:04:05 -0700",
			})))
		case "/artifact.zip":
			w.Write(artifact)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	artifactURL = srv.URL + "/artifact.zip"

	up := New(Config{
		FeedURL:          srv.URL + "/appcast?token=<TOKEN>&installed=<CFBundleVersion>",
		PublicEDKey:      kp.Public,
		InstalledVersion: 10,
		HTTPClient:       srv.Client(),
	})

	// Wrong token -> ErrUnauthorized.
	if _, err := up.Check(context.Background(), "nope"); err != ErrUnauthorized {
		t.Fatalf("bad token: want ErrUnauthorized, got %v", err)
	}

	rel, err := up.Check(context.Background(), wantToken)
	if err != nil {
		t.Fatal(err)
	}
	if rel == nil || rel.Version != 11 || rel.ShortVersion != "0.2.0" {
		t.Fatalf("bad release: %+v", rel)
	}

	path, err := up.Download(context.Background(), rel, wantToken)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	got, _ := os.ReadFile(path)
	if string(got) != string(artifact) {
		t.Fatal("downloaded bytes differ from the artifact")
	}
}

func TestCheckUpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(RenderAppcast("App", FeedItem{
			ShortVersion: "0.1.0", Version: 5, URL: "https://x/a.zip", Length: 1, EDSignature: "c2ln",
		})))
	}))
	defer srv.Close()
	up := New(Config{FeedURL: srv.URL, PublicEDKey: "cHVia2V5", InstalledVersion: 5, HTTPClient: srv.Client()})
	rel, err := up.Check(context.Background(), "t")
	if err != nil {
		t.Fatal(err)
	}
	if rel != nil {
		t.Fatalf("installed==latest should be up to date, got %+v", rel)
	}
}

// A gated feed answering 200 with an informational item must surface as a
// non-nil Release (not "you're up to date"), and Download must refuse it
// without touching the network.
func TestCheckInformationalUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(informationalFeed))
	}))
	defer srv.Close()

	up := New(Config{FeedURL: srv.URL, PublicEDKey: "cHVia2V5", InstalledVersion: 42, HTTPClient: srv.Client()})
	rel, err := up.Check(context.Background(), "t")
	if err != nil {
		t.Fatal(err)
	}
	if rel == nil {
		t.Fatal("informational item reported as up to date - the misleading outcome this exists to prevent")
	}
	if !rel.Informational {
		t.Errorf("Informational not set: %+v", rel)
	}
	if rel.Version != 999000000 || rel.Title != "Reactivate your access" {
		t.Errorf("release fields: %+v", rel)
	}
	if rel.Link != "https://example.invalid/access" {
		t.Errorf("link: %q", rel.Link)
	}
	if !strings.Contains(rel.Notes, "updates are paused") {
		t.Errorf("notes: %q", rel.Notes)
	}

	// Download must not issue a request at all.
	tripwire := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("Download hit the network for an informational release: %s", r.URL)
	}))
	defer tripwire.Close()
	dl := New(Config{PublicEDKey: "cHVia2V5", HTTPClient: tripwire.Client()})
	if _, err := dl.Download(context.Background(), rel, "t"); err != ErrInformationalUpdate {
		t.Fatalf("Download: want ErrInformationalUpdate, got %v", err)
	}
	// Even with a URL glued on, the flag alone refuses the download.
	armed := *rel
	armed.URL = tripwire.URL + "/artifact.zip"
	if _, err := dl.Download(context.Background(), &armed, "t"); err != ErrInformationalUpdate {
		t.Fatalf("Download with URL: want ErrInformationalUpdate, got %v", err)
	}
}

func TestDownloadRejectsSizeMismatchAndTamper(t *testing.T) {
	kp, _ := GenerateKeys()
	good := []byte("good bytes")
	sig, _ := SignArtifact(kp.Private, good)

	// Server returns different bytes than were signed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("TAMPERED"))
	}))
	defer srv.Close()
	up := New(Config{PublicEDKey: kp.Public, HTTPClient: srv.Client()})

	// Length mismatch is caught before the signature check.
	if _, err := up.Download(context.Background(), &Release{URL: srv.URL, EDSignature: sig, Length: int64(len(good))}, "t"); err == nil {
		t.Fatal("size mismatch accepted")
	}
	// Even without a declared length, the signature rejects the tamper.
	if _, err := up.Download(context.Background(), &Release{URL: srv.URL, EDSignature: sig}, "t"); err == nil {
		t.Fatal("tampered artifact accepted")
	}
}
