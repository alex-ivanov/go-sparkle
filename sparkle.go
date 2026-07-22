// Package sparkle is a small, dependency-free Go client for the Sparkle
// software-update protocol (https://sparkle-project.org): it fetches a Sparkle
// appcast, picks the newest eligible item above the installed build, verifies
// the artifact's EdDSA (ed25519) signature against the app's public key
// (Sparkle's SUPublicEDKey / sparkle:edSignature), and - on macOS - swaps the
// downloaded .app bundle in place.
//
// It is the client half only: the real Sparkle.framework is Objective-C/Swift,
// so a pure-Go program (a systray app, a CLI) cannot embed it. This speaks the
// same appcast + EdDSA wire format, so it interoperates with a Sparkle feed and
// with Sparkle's own generate_keys / sign_update tooling (this package can also
// mint keys, sign artifacts, and render a feed - see sign.go / feed.go).
//
// A token-gated feed (an access token injected into the appcast query and the
// download Authorization header) is supported for private or gated
// distribution: put <TOKEN> and <CFBundleVersion> placeholders in the feed URL
// and pass the token to Check/Download.
//
// # Informational updates
//
// A feed may answer with a <sparkle:informationalUpdate> item: news with a
// <link> and no <enclosure>, which a gated service uses to say "this install's
// access has lapsed" without offering a download. Check returns such an item as
// a Release with Informational set, so callers must distinguish it from an
// installable update:
//
//	rel, err := up.Check(ctx, token)
//	if rel != nil && rel.Informational {
//		// show rel.Title / rel.Notes; open rel.Link. Do NOT call Download.
//	}
//
// Since v1.1.0 this changes what an existing caller observes: a feed serving an
// informational item used to yield a nil Release ("up to date") and now yields a
// non-nil one. A caller that ignores Informational and calls Download gets
// ErrInformationalUpdate before any request is made, rather than silently
// telling the user nothing is available.
package sparkle

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

// ErrUnauthorized is returned when the feed or download gate rejects the
// request (HTTP 401/403): the access token is missing, revoked, or expired.
var ErrUnauthorized = errors.New("update access denied - the token is missing, revoked, or expired")

// ErrInformationalUpdate is returned by Download for a Release that carries no
// artifact (a <sparkle:informationalUpdate> item). Show it to the user and open
// its Link instead.
var ErrInformationalUpdate = errors.New("informational update: nothing to download")

// Config configures an Updater. Only FeedURL + PublicEDKey are required to
// check and verify; InstalledVersion gates what counts as "newer".
type Config struct {
	// FeedURL is the Sparkle appcast URL. It may contain the <TOKEN> and
	// <CFBundleVersion> placeholders, substituted per request.
	FeedURL string
	// PublicEDKey is the base64 ed25519 public key (Sparkle SUPublicEDKey)
	// every artifact's edSignature must verify against.
	PublicEDKey string
	// InstalledVersion is the running build number (Sparkle sparkle:version /
	// CFBundleVersion). Items with a higher version are offered.
	InstalledVersion int
	// Channel, when non-empty, additionally admits items tagged with this
	// sparkle:channel; untagged items are always eligible (the release
	// channel). Empty = release channel only.
	Channel string
	// OSVersion, when non-empty, filters items whose sparkle:minimumSystemVersion
	// exceeds it (dotted numeric compare, e.g. "14.4"). Empty = no filtering.
	OSVersion string
	// HTTPClient is used for all requests; nil selects a 30s default.
	HTTPClient *http.Client
}

// Updater checks a feed and downloads verified artifacts.
type Updater struct {
	cfg    Config
	client *http.Client
}

// New builds an Updater from cfg.
func New(cfg Config) *Updater {
	c := cfg.HTTPClient
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	return &Updater{cfg: cfg, client: c}
}

// Release is one appcast item newer than the installed build.
type Release struct {
	Version      int    // sparkle:version (build number, monotonic)
	ShortVersion string // sparkle:shortVersionString (human-readable)
	Title        string
	Notes        string // <description> (release notes)
	Channel      string // sparkle:channel ("" = release)
	URL          string // enclosure url (absolute; empty on an informational item)
	Length       int64  // enclosure length in bytes (0 = unknown)
	EDSignature  string // base64 ed25519 signature of the artifact bytes

	// Informational reports a <sparkle:informationalUpdate> item: news, not an
	// artifact. Show Title/Notes and send the user to Link; Download refuses it
	// with ErrInformationalUpdate.
	Informational bool
	// Link is the item's <link> (absolute) - where an informational item sends
	// the user.
	Link string
}

// Check fetches the token-gated appcast and returns the newest eligible
// release above the installed build, or nil when up to date. A 401/403 maps to
// ErrUnauthorized so the caller can prompt for (re)activation.
func (u *Updater) Check(ctx context.Context, token string) (*Release, error) {
	if strings.TrimSpace(u.cfg.FeedURL) == "" {
		return nil, errors.New("no feed URL configured")
	}
	feed := ExpandFeedURL(u.cfg.FeedURL, token, u.cfg.InstalledVersion)
	base, err := url.Parse(feed)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPError{Op: "appcast fetch", Status: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	items, err := ParseAppcast(body, base)
	if err != nil {
		return nil, err
	}
	return pickBest(items, u.cfg), nil
}

// Download fetches the release artifact (Bearer-authorized), checks its length,
// verifies the ed25519 signature against the configured public key, and writes
// it to a temp file. The caller removes the file when done.
func (u *Updater) Download(ctx context.Context, rel *Release, token string) (string, error) {
	// An informational item has no artifact: refuse before touching the
	// network, so it can never be mistaken for something installable.
	if rel.Informational || strings.TrimSpace(rel.URL) == "" {
		return "", ErrInformationalUpdate
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.URL, nil)
	if err != nil {
		return "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return "", ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return "", &HTTPError{Op: "download", Status: resp.StatusCode}
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if rel.Length > 0 && int64(len(data)) != rel.Length {
		return "", &SizeMismatchError{Got: int64(len(data)), Want: rel.Length}
	}
	if err := VerifySignature(u.cfg.PublicEDKey, rel.EDSignature, data); err != nil {
		return "", err
	}
	return writeTemp(data, artifactExt(rel.URL))
}

// artifactExt returns the artifact's file extension (".zip", ".dmg", ...) from
// the enclosure URL, defaulting to ".zip". Apply sniffs by content, so this is
// only for a truthful temp-file name.
func artifactExt(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil {
		if e := path.Ext(u.Path); len(e) >= 2 && len(e) <= 6 {
			return e
		}
	}
	return ".zip"
}

// writeTemp writes data to a unique temp file (with the given extension, so a
// downloaded .dmg is not misnamed .zip) and returns its path.
func writeTemp(data []byte, ext string) (string, error) {
	f, err := os.CreateTemp("", "sparkle-update-*"+ext)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// pickBest returns the highest-version eligible item above the installed build,
// or nil. Eligibility: version > installed, channel admitted, OS requirement
// met, and an absolute http(s) enclosure URL present.
func pickBest(items []Item, cfg Config) *Release {
	var best *Item
	for i := range items {
		it := &items[i]
		if it.Version <= cfg.InstalledVersion {
			continue
		}
		if !channelAdmitted(it.Channel, cfg.Channel) {
			continue
		}
		if !osMeetsMinimum(cfg.OSVersion, it.MinimumSystemVersion) {
			continue
		}
		// An informational item legitimately has no enclosure; everything
		// else must carry a usable download URL.
		if !isDownloadURL(it.EnclosureURL) && !it.Informational {
			continue
		}
		if !informationalApplies(it, cfg.InstalledVersion) {
			continue
		}
		if best == nil || it.Version > best.Version {
			best = it
		}
	}
	if best == nil {
		return nil
	}
	rel := &Release{
		Version:       best.Version,
		ShortVersion:  best.ShortVersion,
		Title:         best.Title,
		Notes:         best.Description,
		Channel:       best.Channel,
		URL:           best.EnclosureURL,
		Length:        best.EnclosureLength,
		EDSignature:   best.EDSignature,
		Informational: best.Informational,
		Link:          best.Link,
	}
	// Never hand back a URL that is not a real download target (an
	// informational item may carry none, or a bogus one).
	if !isDownloadURL(rel.URL) {
		rel.URL = ""
	}
	return rel
}

// channelAdmitted reports whether an item's channel is offered to a client on
// want: untagged items are always eligible; a tagged item needs a channel match.
func channelAdmitted(itemChannel, want string) bool {
	if itemChannel == "" {
		return true
	}
	return itemChannel == want
}

// isDownloadURL requires an absolute http(s) URL, rejecting file://, data:, and
// other schemes an appcast must never point a download at.
func isDownloadURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.IsAbs() && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// ExpandFeedURL substitutes the <TOKEN> and <CFBundleVersion> placeholders in a
// Sparkle feed template, each percent-encoded as a query value.
func ExpandFeedURL(tmpl, token string, installedVersion int) string {
	return strings.NewReplacer(
		"<TOKEN>", url.QueryEscape(token),
		"<CFBundleVersion>", url.QueryEscape(strconv.Itoa(installedVersion)),
	).Replace(tmpl)
}
