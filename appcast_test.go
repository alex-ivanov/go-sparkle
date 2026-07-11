package sparkle

import (
	"net/url"
	"testing"
)

// A feed exercising the parsing behaviors a minimal Sparkle client needs
// (cf. SUAppcastTest): multiple items, namespaced sparkle: elements, an
// enclosure-attribute version form, a relative enclosure URL, a channel tag,
// a minimumSystemVersion, and an item missing optional fields.
const sampleFeed = `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle">
  <channel>
    <title>App</title>
    <item>
      <title>App 1.0</title>
      <description>first</description>
      <sparkle:version>10</sparkle:version>
      <sparkle:shortVersionString>1.0</sparkle:shortVersionString>
      <enclosure url="https://dl.example/App-1.0.zip" length="111" type="application/zip" sparkle:edSignature="c2ln"/>
    </item>
    <item>
      <title>App 2.0 beta</title>
      <sparkle:channel>beta</sparkle:channel>
      <sparkle:minimumSystemVersion>14.0</sparkle:minimumSystemVersion>
      <enclosure url="rel/App-2.0.zip" length="222" type="application/zip"
                 sparkle:version="20" sparkle:shortVersionString="2.0" sparkle:edSignature="c2ln2"/>
    </item>
    <item>
      <title>no enclosure version - skipped</title>
      <enclosure url="https://dl.example/broken.zip" length="1"/>
    </item>
  </channel>
</rss>`

func TestParseAppcast(t *testing.T) {
	base, _ := url.Parse("https://dl.example/feed/appcast.xml")
	items, err := ParseAppcast([]byte(sampleFeed), base)
	if err != nil {
		t.Fatal(err)
	}
	// The version-less item is dropped.
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(items), items)
	}

	a := items[0]
	if a.Version != 10 || a.ShortVersion != "1.0" || a.Description != "first" {
		t.Fatalf("item 0 fields: %+v", a)
	}
	if a.EnclosureURL != "https://dl.example/App-1.0.zip" || a.EnclosureLength != 111 || a.EDSignature != "c2ln" {
		t.Fatalf("item 0 enclosure: %+v", a)
	}

	b := items[1]
	// version + short live on the enclosure attribute here.
	if b.Version != 20 || b.ShortVersion != "2.0" {
		t.Fatalf("item 1 version from enclosure attr: %+v", b)
	}
	if b.Channel != "beta" || b.MinimumSystemVersion != "14.0" {
		t.Fatalf("item 1 channel/minsys: %+v", b)
	}
	// Relative enclosure URL resolves against the feed base dir.
	if b.EnclosureURL != "https://dl.example/feed/rel/App-2.0.zip" {
		t.Fatalf("relative URL not resolved: %q", b.EnclosureURL)
	}
}

func TestChannelAdmitted(t *testing.T) {
	if !channelAdmitted("", "beta") {
		t.Error("untagged item should always be admitted")
	}
	if !channelAdmitted("beta", "beta") {
		t.Error("matching channel should be admitted")
	}
	if channelAdmitted("beta", "") {
		t.Error("tagged item should be excluded on the release channel")
	}
	if channelAdmitted("beta", "nightly") {
		t.Error("wrong channel should be excluded")
	}
}

func TestOSMeetsMinimum(t *testing.T) {
	cases := []struct {
		os, min string
		want    bool
	}{
		{"", "14.0", true},       // caller opted out of OS filtering
		{"14.4", "", true},       // item has no minimum
		{"14.4", "14.0", true},   // newer than minimum
		{"14.0", "14.0", true},   // exactly the minimum
		{"13.6", "14.0", false},  // too old
		{"14.4.1", "14.4", true}, // extra component
		{"14.4", "14.4.1", false},
	}
	for _, c := range cases {
		if got := osMeetsMinimum(c.os, c.min); got != c.want {
			t.Errorf("osMeetsMinimum(%q,%q)=%v want %v", c.os, c.min, got, c.want)
		}
	}
}

func TestIsDownloadURL(t *testing.T) {
	ok := []string{"https://x/a.zip", "http://127.0.0.1:9/a.zip"}
	bad := []string{"file:///etc/passwd", "javascript:alert(1)", "data:text/plain,x", "relative.zip", ""}
	for _, u := range ok {
		if !isDownloadURL(u) {
			t.Errorf("%q should be a valid download URL", u)
		}
	}
	for _, u := range bad {
		if isDownloadURL(u) {
			t.Errorf("%q should be rejected", u)
		}
	}
}

// pickBest should honor version, channel, OS, and URL eligibility.
func TestPickBest(t *testing.T) {
	base, _ := url.Parse("https://dl.example/appcast.xml")
	items, _ := ParseAppcast([]byte(sampleFeed), base)

	// Installed 10, release channel, no OS filter: the beta item (20) is
	// excluded by channel, so nothing newer is offered.
	if rel := pickBest(items, Config{InstalledVersion: 10}); rel != nil {
		t.Fatalf("beta item leaked onto release channel: %+v", rel)
	}
	// Opt into beta + a new enough OS: 2.0 is offered.
	rel := pickBest(items, Config{InstalledVersion: 10, Channel: "beta", OSVersion: "14.4"})
	if rel == nil || rel.Version != 20 {
		t.Fatalf("beta 2.0 not offered: %+v", rel)
	}
	// Beta but an OS too old: filtered out.
	if rel := pickBest(items, Config{InstalledVersion: 10, Channel: "beta", OSVersion: "13.0"}); rel != nil {
		t.Fatalf("minimumSystemVersion not enforced: %+v", rel)
	}
	// Already at 20: up to date.
	if rel := pickBest(items, Config{InstalledVersion: 20, Channel: "beta", OSVersion: "14.4"}); rel != nil {
		t.Fatalf("should be up to date: %+v", rel)
	}
}
