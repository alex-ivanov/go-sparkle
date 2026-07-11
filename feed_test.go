package sparkle

import (
	"net/url"
	"testing"
)

// A rendered feed must parse back to the same item (generate <-> consume).
func TestRenderAppcastRoundTrip(t *testing.T) {
	kp, _ := GenerateKeys()
	artifact := []byte("zip bytes")
	sig, _ := SignArtifact(kp.Private, artifact)

	xml := RenderAppcast("Example App", FeedItem{
		ShortVersion: "1.2.3",
		Version:      42,
		Channel:      "beta",
		Notes:        "notes with <html> & ampersand",
		PubDate:      "Mon, 02 Jan 2006 15:04:05 -0700",
		URL:          "https://dl.example/App-1.2.3.zip",
		Length:       int64(len(artifact)),
		EDSignature:  sig,
		MinimumOS:    "13.0",
	})

	items, err := ParseAppcast([]byte(xml), nil)
	if err != nil {
		t.Fatalf("rendered feed does not parse: %v\n%s", err, xml)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	it := items[0]
	if it.Version != 42 || it.ShortVersion != "1.2.3" || it.Channel != "beta" ||
		it.MinimumSystemVersion != "13.0" || it.EnclosureURL != "https://dl.example/App-1.2.3.zip" ||
		it.EnclosureLength != int64(len(artifact)) || it.EDSignature != sig {
		t.Fatalf("round-trip mismatch: %+v", it)
	}

	// The signature in the rendered feed verifies against the artifact.
	if err := VerifySignature(kp.Public, it.EDSignature, artifact); err != nil {
		t.Fatalf("rendered signature does not verify: %v", err)
	}
}

// A rendered feed is offerable end to end through pickBest.
func TestRenderThenPick(t *testing.T) {
	xml := RenderAppcast("App", FeedItem{ShortVersion: "2.0", Version: 20, URL: "https://x/a.zip", Length: 9, EDSignature: "c2ln"})
	base, _ := url.Parse("https://x/appcast.xml")
	items, _ := ParseAppcast([]byte(xml), base)
	if rel := pickBest(items, Config{InstalledVersion: 10}); rel == nil || rel.Version != 20 {
		t.Fatalf("rendered item not offered: %+v", rel)
	}
}
