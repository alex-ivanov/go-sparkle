package sparkle

import (
	"net/url"
	"reflect"
	"strings"
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
	// A normal item is not informational and carries no link.
	if a.Informational || a.Link != "" || len(a.BelowVersions) != 0 {
		t.Fatalf("item 0 wrongly informational: %+v", a)
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

// The literal response a gated distribution service serves when an install's
// access has lapsed: HTTP 200, a very high version, a <link>, no <enclosure>,
// and an empty-element <sparkle:informationalUpdate>.
const informationalFeed = `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle">
  <channel>
    <title>BurnMeter</title>
    <item>
      <title>Reactivate your access</title>
      <description><![CDATA[<p>Your access has expired or been revoked, so updates are paused.</p>]]></description>
      <sparkle:version>999000000</sparkle:version>
      <sparkle:shortVersionString>Access renewal</sparkle:shortVersionString>
      <sparkle:informationalUpdate></sparkle:informationalUpdate>
      <link>https://example.invalid/access</link>
    </item>
  </channel>
</rss>`

func TestParseInformationalItem(t *testing.T) {
	base, _ := url.Parse("https://dl.example/feed/appcast.xml")
	items, err := ParseAppcast([]byte(informationalFeed), base)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1: %+v", len(items), items)
	}
	it := items[0]
	if !it.Informational {
		t.Error("empty-element <sparkle:informationalUpdate> not detected")
	}
	if it.Version != 999000000 || it.ShortVersion != "Access renewal" {
		t.Errorf("version fields: %+v", it)
	}
	if it.Title != "Reactivate your access" {
		t.Errorf("title: %q", it.Title)
	}
	if !strings.Contains(it.Description, "updates are paused") {
		t.Errorf("description (CDATA) not carried: %q", it.Description)
	}
	if it.Link != "https://example.invalid/access" {
		t.Errorf("link: %q", it.Link)
	}
	if it.EnclosureURL != "" || len(it.BelowVersions) != 0 {
		t.Errorf("unexpected enclosure/belowVersion: %+v", it)
	}
}

// The self-closing form must parse identically to the empty-element form, and a
// relative <link> resolves against the feed base like an enclosure URL does.
func TestParseInformationalSelfClosing(t *testing.T) {
	selfClosing := strings.Replace(informationalFeed,
		"<sparkle:informationalUpdate></sparkle:informationalUpdate>",
		"<sparkle:informationalUpdate/>", 1)
	if selfClosing == informationalFeed {
		t.Fatal("fixture rewrite did not apply")
	}
	base, _ := url.Parse("https://dl.example/feed/appcast.xml")
	empty, err := ParseAppcast([]byte(informationalFeed), base)
	if err != nil {
		t.Fatal(err)
	}
	self, err := ParseAppcast([]byte(selfClosing), base)
	if err != nil {
		t.Fatal(err)
	}
	if len(self) != 1 || !reflect.DeepEqual(self, empty) {
		t.Fatalf("self-closing parsed differently:\n self: %+v\nempty: %+v", self, empty)
	}

	rel := strings.Replace(informationalFeed,
		"<link>https://example.invalid/access</link>", "<link>access</link>", 1)
	items, err := ParseAppcast([]byte(rel), base)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Link != "https://dl.example/feed/access" {
		t.Errorf("relative link not resolved: %q", items[0].Link)
	}
}

// belowVersion narrows an informational item to installs below a given build.
func TestInformationalBelowVersion(t *testing.T) {
	feed := strings.Replace(informationalFeed,
		"<sparkle:informationalUpdate></sparkle:informationalUpdate>",
		"<sparkle:informationalUpdate><sparkle:belowVersion>50</sparkle:belowVersion>"+
			"<sparkle:belowVersion>x.y</sparkle:belowVersion></sparkle:informationalUpdate>", 1)
	items, err := ParseAppcast([]byte(feed), nil)
	if err != nil {
		t.Fatal(err)
	}
	// The non-numeric entry is dropped; eligibility rides on the build number.
	if got := items[0].BelowVersions; len(got) != 1 || got[0] != 50 {
		t.Fatalf("belowVersion: %v", got)
	}
	if rel := pickBest(items, Config{InstalledVersion: 49}); rel == nil || !rel.Informational {
		t.Fatalf("installed below belowVersion should be offered: %+v", rel)
	}
	if rel := pickBest(items, Config{InstalledVersion: 50}); rel != nil {
		t.Fatalf("installed at/above belowVersion should be skipped: %+v", rel)
	}
	// No belowVersion children: applies to every install.
	plain, _ := ParseAppcast([]byte(informationalFeed), nil)
	if rel := pickBest(plain, Config{InstalledVersion: 50}); rel == nil {
		t.Fatal("informational item without belowVersion should apply to any install")
	}
}

// pickBest's enclosure rule: informational items are exempt, everything else is
// not, and the highest version still wins over an informational item.
func TestPickBestInformational(t *testing.T) {
	base, _ := url.Parse("https://dl.example/appcast.xml")
	info, _ := ParseAppcast([]byte(informationalFeed), base)

	rel := pickBest(info, Config{InstalledVersion: 10})
	if rel == nil {
		t.Fatal("informational item was discarded")
	}
	if !rel.Informational || rel.Link != "https://example.invalid/access" || rel.URL != "" {
		t.Fatalf("release fields: %+v", rel)
	}
	// Not newer than installed: skipped like any other item.
	if rel := pickBest(info, Config{InstalledVersion: 999000000}); rel != nil {
		t.Fatalf("informational item at/below installed should be skipped: %+v", rel)
	}
	// A non-informational item with no usable enclosure is still skipped.
	noEnclosure := strings.Replace(informationalFeed,
		"<sparkle:informationalUpdate></sparkle:informationalUpdate>", "", 1)
	plain, _ := ParseAppcast([]byte(noEnclosure), base)
	if rel := pickBest(plain, Config{InstalledVersion: 10}); rel != nil {
		t.Fatalf("enclosure-less non-informational item admitted: %+v", rel)
	}

	// Both kinds in one feed: highest version wins, deliberately. The
	// informational item's 999000000 outranks the real 20, so the caller is
	// told about the lapsed access rather than offered a download it cannot
	// authorize.
	both, _ := ParseAppcast([]byte(mixedFeed), base)
	if len(both) != 2 {
		t.Fatalf("mixed feed parsed %d items", len(both))
	}
	rel = pickBest(both, Config{InstalledVersion: 10})
	if rel == nil || !rel.Informational || rel.Version != 999000000 {
		t.Fatalf("highest version should win: %+v", rel)
	}
	// Drop the informational item below the real one and the download wins.
	lower, _ := ParseAppcast([]byte(strings.Replace(mixedFeed, "999000000", "15", 1)), base)
	rel = pickBest(lower, Config{InstalledVersion: 10})
	if rel == nil || rel.Informational || rel.Version != 20 {
		t.Fatalf("real update should win when it is newer: %+v", rel)
	}
}

// An informational item alongside a genuine downloadable update.
const mixedFeed = `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle">
  <channel>
    <title>App</title>
    <item>
      <title>Reactivate your access</title>
      <sparkle:version>999000000</sparkle:version>
      <sparkle:informationalUpdate/>
      <link>https://example.invalid/access</link>
    </item>
    <item>
      <title>App 2.0</title>
      <sparkle:version>20</sparkle:version>
      <sparkle:shortVersionString>2.0</sparkle:shortVersionString>
      <enclosure url="https://dl.example/App-2.0.zip" length="222" type="application/zip" sparkle:edSignature="c2ln"/>
    </item>
  </channel>
</rss>`

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
