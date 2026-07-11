# go-sparkle

A small, dependency-free Go client and toolchain for the
[Sparkle](https://sparkle-project.org) software-update protocol.

Sparkle is the de-facto auto-update framework for macOS apps, but
`Sparkle.framework` is Objective-C/Swift — a pure-Go program (a menu-bar/systray
app, a CLI) can't embed it. **go-sparkle re-implements the client and signing
in Go** against the same wire format, so it interoperates with a Sparkle feed
and with Sparkle's own `generate_keys` / `sign_update` tooling — without
bundling any Sparkle binary or framework.

- **Stdlib only.** No third-party dependencies (`crypto/ed25519`,
  `encoding/xml`, `net/http`).
- **Same wire format as Sparkle.** Appcast RSS, base64 `SUPublicEDKey`,
  `sparkle:edSignature` (EdDSA / ed25519 over the artifact bytes).
- **Both halves.** A client (check + verify + apply) *and* the release side
  (mint keys, sign, render a feed) — the `sparkle` CLI replaces Sparkle's
  `generate_keys` + `sign_update`.
- **Token-gated feeds.** Optional access token for private or gated
  distribution, injected into the appcast query and the download `Bearer`.

## Install

Library:

```sh
go get github.com/alex-ivanov/go-sparkle
```

CLI (release-side signer):

```sh
go install github.com/alex-ivanov/go-sparkle/cmd/sparkle@latest
```

## Client

```go
up := sparkle.New(sparkle.Config{
    FeedURL:          "https://dl.example/appcast.xml", // or a token template (below)
    PublicEDKey:      "<base64 ed25519 public key>",    // Sparkle SUPublicEDKey
    InstalledVersion: 42,                               // CFBundleVersion
    Channel:          "",                               // "" = release; else e.g. "beta"
    OSVersion:        "14.4",                            // filters sparkle:minimumSystemVersion
})

rel, err := up.Check(ctx, token) // nil when up to date; ErrUnauthorized on 401/403
if rel != nil {
    zip, err := up.Download(ctx, rel, token) // Bearer + length + ed25519 verified
    app, err := sparkle.Apply(zip)           // macOS: swap the .app in place + relaunch
    _ = app
}
```

`Check` returns the newest item above `InstalledVersion` whose channel is
admitted, whose `minimumSystemVersion` (if any) is met, and whose enclosure is
an absolute `http(s)` URL. `Download` verifies the declared length and the
ed25519 signature before returning the file. On macOS, `Apply` `ditto`-extracts
the `.app` from the zip, rename-swaps it over the running bundle (safe while
running), and relaunches; `CleanupBackups` clears the `.bak` on next launch.

### Token-gated feeds

For private or gated releases, put `<TOKEN>` and `<CFBundleVersion>` in the feed URL:

```
https://dl.example/appcast?token=<TOKEN>&installed=<CFBundleVersion>
```

The token is percent-encoded into the query and also sent as
`Authorization: Bearer <token>` on both the appcast and the download, so a gate
that accepts either works. An empty token yields an empty placeholder (the gate
rejects it).

## Release side

Mint a keypair once, keep the private key secret, embed the public key in the
app (`SUPublicEDKey`, or your own config):

```sh
sparkle keygen --out sparkle-update.key
# prints the base64 public key to embed
```

Package a new version — sign the artifact and render the appcast:

```sh
sparkle appcast \
  --key sparkle-update.key \
  --url https://dl.example/MyApp-1.2.3.zip \
  --build 43 --short 1.2.3 \
  --name "My App" \
  --date "$(date -R)" \
  MyApp-1.2.3.zip > appcast.xml
```

`sparkle sign --key sparkle-update.key <artifact>` prints just the signature.
In Go, the same functions are `GenerateKeys`, `SignArtifact`, and
`RenderAppcast`.

### Drop-in `sign_update`

`cmd/sign_update` is a drop-in replacement for Sparkle's `sign_update` binary —
same flags, same key format, same output — so tooling/CI that expects Sparkle's
tool can call this one (pure Go, cross-platform, no Sparkle install):

```sh
go install github.com/alex-ivanov/go-sparkle/cmd/sign_update@latest

sign_update App-1.2.3.zip -f eddsa_private.key
# -> sparkle:edSignature="<base64>" length="12345"
```

The key comes from `-f`/`--ed-key-file` (a base64 ed25519 private key file, or
`-` for stdin), the deprecated `-s` inline key, or — on macOS with no key given
— the login Keychain (`--account`, default `ed25519`), exactly where Sparkle's
`generate_keys` stores it. `-p` prints only the signature; `--verify <archive>
<sig>` verifies. Its keys are interchangeable with Sparkle's (both are base64
64-byte ed25519 keys). Signing XML feeds in place and release-notes warnings are
not implemented — sign archives/pkgs/deltas here, and render feeds with the
appcast writer above.

## Interop with Sparkle

Sparkle 2.x signs updates with plain ed25519 (RFC 8032) over the artifact
bytes. go-sparkle uses `crypto/ed25519`, so signatures and public keys are
byte-identical: a feed signed by Sparkle's `sign_update` verifies here, and a
feed signed by `sparkle` verifies in Sparkle. This is checked against the
RFC 8032 standard test vectors (`rfc8032_test.go`).

The artifact is expected to be a **`.zip`** of the `.app` (Sparkle also supports
DMG; `Apply` handles zips).

## Scope

Implemented: appcast parse (namespaced `sparkle:` elements, item- and
enclosure-attribute version forms, relative enclosure URLs resolved against the
feed, `sparkle:channel`, `minimumSystemVersion`, non-`http(s)` enclosure
rejection); ed25519 verify; token-gated check/download; macOS `.app` swap +
relaunch; key/sign/feed generation.

Not implemented (open a PR if you need them): delta updates, phased rollout,
critical updates, localized release-notes selection, DSA (legacy Sparkle 1.x)
signatures.

## License

[MIT](./LICENSE).
