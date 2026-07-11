package sparkle

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Item is one parsed appcast entry (Sparkle RSS <item>). Only the fields a
// minimal update client needs are surfaced.
type Item struct {
	Title                string
	Description          string // release notes (may be CDATA)
	Version              int    // sparkle:version (build number)
	ShortVersion         string // sparkle:shortVersionString
	Channel              string // sparkle:channel ("" = release)
	MinimumSystemVersion string // sparkle:minimumSystemVersion ("" = none)
	EnclosureURL         string // enclosure url (resolved absolute)
	EnclosureLength      int64  // enclosure length attr (0 = absent)
	EDSignature          string // enclosure sparkle:edSignature (base64)
}

// rawAppcast mirrors the on-wire Sparkle RSS. encoding/xml matches by local
// name, so the sparkle:-prefixed elements/attributes bind without namespace
// tags. Both the item-level <sparkle:version> form and the enclosure attribute
// form (sparkle:version="…" on <enclosure>) appear in the wild; we read both.
type rawAppcast struct {
	Items []struct {
		Title        string `xml:"title"`
		Description  string `xml:"description"`
		Version      string `xml:"version"`            // <sparkle:version>
		ShortVersion string `xml:"shortVersionString"` // <sparkle:shortVersionString>
		Channel      string `xml:"channel"`            // <sparkle:channel>
		MinSystem    string `xml:"minimumSystemVersion"`
		Enclosure    struct {
			URL          string `xml:"url,attr"`
			Length       string `xml:"length,attr"`
			Version      string `xml:"version,attr"` // sparkle:version on <enclosure>
			ShortVersion string `xml:"shortVersionString,attr"`
			EDSignature  string `xml:"edSignature,attr"` // sparkle:edSignature
		} `xml:"enclosure"`
	} `xml:"channel>item"`
}

// ParseAppcast parses a Sparkle appcast. Relative enclosure URLs are resolved
// against base (the feed URL); items whose version cannot be read are skipped.
// A nil base leaves relative URLs untouched.
func ParseAppcast(data []byte, base *url.URL) ([]Item, error) {
	var raw rawAppcast
	if err := xml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing appcast: %w", err)
	}
	items := make([]Item, 0, len(raw.Items))
	for _, r := range raw.Items {
		// version may live on the item or on the enclosure attribute.
		vs := strings.TrimSpace(r.Version)
		if vs == "" {
			vs = strings.TrimSpace(r.Enclosure.Version)
		}
		v, err := strconv.Atoi(vs)
		if err != nil {
			continue // no usable version: not an updateable item
		}
		short := strings.TrimSpace(r.ShortVersion)
		if short == "" {
			short = strings.TrimSpace(r.Enclosure.ShortVersion)
		}
		it := Item{
			Title:                strings.TrimSpace(r.Title),
			Description:          strings.TrimSpace(r.Description),
			Version:              v,
			ShortVersion:         short,
			Channel:              strings.TrimSpace(r.Channel),
			MinimumSystemVersion: strings.TrimSpace(r.MinSystem),
			EnclosureURL:         resolveURL(base, strings.TrimSpace(r.Enclosure.URL)),
			EnclosureLength:      parseLen(r.Enclosure.Length),
			EDSignature:          strings.TrimSpace(r.Enclosure.EDSignature),
		}
		items = append(items, it)
	}
	return items, nil
}

func parseLen(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if n < 0 {
		return 0
	}
	return n
}

// resolveURL resolves a possibly-relative enclosure URL against the feed base.
func resolveURL(base *url.URL, raw string) string {
	if raw == "" || base == nil {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if ref.IsAbs() {
		return raw
	}
	return base.ResolveReference(ref).String()
}

// osMeetsMinimum reports whether osVersion satisfies an item's
// minimumSystemVersion. Empty osVersion (caller opted out) or empty minimum
// (item has none) both pass.
func osMeetsMinimum(osVersion, minimum string) bool {
	if osVersion == "" || minimum == "" {
		return true
	}
	return compareDotted(osVersion, minimum) >= 0
}

// compareDotted compares dotted-numeric versions ("14.4" vs "14.4.1"). Missing
// trailing components count as zero. Non-numeric components compare as zero.
func compareDotted(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(as) {
			av, _ = strconv.Atoi(strings.TrimSpace(as[i]))
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(strings.TrimSpace(bs[i]))
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}
