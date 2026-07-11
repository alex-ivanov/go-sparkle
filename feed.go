package sparkle

import (
	"fmt"
	"html"
	"strings"
)

// FeedItem describes one update to render into an appcast (the release side).
type FeedItem struct {
	Title        string // default: "<name> <ShortVersion>"
	ShortVersion string // human version (CFBundleShortVersionString)
	Version      int    // build number (CFBundleVersion / sparkle:version)
	Channel      string // optional sparkle:channel
	Notes        string // release notes (rendered as CDATA)
	PubDate      string // RFC1123Z, e.g. from `date -R`
	URL          string // enclosure download URL
	Length       int64  // enclosure length in bytes
	EDSignature  string // base64 ed25519 signature (from SignArtifact)
	MinimumOS    string // optional sparkle:minimumSystemVersion
}

// RenderAppcast renders a single-item Sparkle appcast (RSS 2.0). name titles
// the channel. The output validates against ParseAppcast and Sparkle.
func RenderAppcast(name string, it FeedItem) string {
	title := it.Title
	if title == "" {
		title = strings.TrimSpace(name + " " + it.ShortVersion)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle">
  <channel>
    <title>%s</title>
    <item>
      <title>%s</title>
`, html.EscapeString(name), html.EscapeString(title))
	if it.PubDate != "" {
		fmt.Fprintf(&b, "      <pubDate>%s</pubDate>\n", html.EscapeString(it.PubDate))
	}
	fmt.Fprintf(&b, "      <description><![CDATA[%s]]></description>\n", it.Notes)
	fmt.Fprintf(&b, "      <sparkle:version>%d</sparkle:version>\n", it.Version)
	fmt.Fprintf(&b, "      <sparkle:shortVersionString>%s</sparkle:shortVersionString>\n", html.EscapeString(it.ShortVersion))
	if it.Channel != "" {
		fmt.Fprintf(&b, "      <sparkle:channel>%s</sparkle:channel>\n", html.EscapeString(it.Channel))
	}
	if it.MinimumOS != "" {
		fmt.Fprintf(&b, "      <sparkle:minimumSystemVersion>%s</sparkle:minimumSystemVersion>\n", html.EscapeString(it.MinimumOS))
	}
	fmt.Fprintf(&b, "      <enclosure url=%q length=\"%d\" type=\"application/zip\" sparkle:edSignature=%q/>\n",
		html.EscapeString(it.URL), it.Length, it.EDSignature)
	b.WriteString("    </item>\n  </channel>\n</rss>\n")
	return b.String()
}
