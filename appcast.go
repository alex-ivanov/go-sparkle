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

	// Informational is true when <sparkle:informationalUpdate> is present on
	// the item (presence alone is the signal; the element is usually empty).
	// Such an item carries news, not an artifact: it has no <enclosure>, and
	// the client should show Title/Description and send the user to Link.
	Informational bool
	// Link is the item's <link>, resolved against the feed base. For an
	// informational item this is where the user is sent (e.g. a reactivation
	// page).
	Link string
	// BelowVersions holds the <sparkle:belowVersion> children of
	// <sparkle:informationalUpdate>: the item is informational only for
	// installs below one of these builds. Empty means it applies to every
	// install. Non-numeric entries (Sparkle permits dotted short versions
	// here) are dropped, since eligibility is decided on the integer build.
	BelowVersions []int
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
		Link         string `xml:"link"`
		// A pointer so presence is detectable: both <sparkle:informationalUpdate/>
		// and <sparkle:informationalUpdate></sparkle:informationalUpdate> yield a
		// non-nil value, while an absent element leaves it nil.
		Informational *struct {
			BelowVersion []string `xml:"belowVersion"`
		} `xml:"informationalUpdate"`
		Enclosure struct {
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
			Link:                 resolveURL(base, strings.TrimSpace(r.Link)),
		}
		if r.Informational != nil {
			it.Informational = true
			for _, bv := range r.Informational.BelowVersion {
				if n, err := strconv.Atoi(strings.TrimSpace(bv)); err == nil {
					it.BelowVersions = append(it.BelowVersions, n)
				}
			}
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

// informationalApplies reports whether an informational item is meant for this
// install. With no <sparkle:belowVersion> children it applies to every install;
// otherwise it applies only when the installed build is below at least one of
// them. Non-informational items are unaffected.
func informationalApplies(it *Item, installed int) bool {
	if !it.Informational || len(it.BelowVersions) == 0 {
		return true
	}
	for _, bv := range it.BelowVersions {
		if installed < bv {
			return true
		}
	}
	return false
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
