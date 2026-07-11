# Releasing go-sparkle

This is the maintainer's runbook for cutting a release. go-sparkle is
distributed **as Git tags only** — there are no prebuilt binaries, no
GoReleaser, and no attached archives. Consumers install straight from source
through the Go module proxy (`go get` / `go install @version`). A "release" is
therefore just an annotated, semver-named tag on a green `main`.

## 1. Versioning policy (SemVer 2.0.0)

go-sparkle follows [Semantic Versioning](https://semver.org/). Given a version
`vMAJOR.MINOR.PATCH`:

- **MAJOR** — incompatible, breaking API changes (removed/renamed exported
  identifiers, changed function signatures or behavior consumers rely on).
- **MINOR** — new, backward-compatible functionality (new exported APIs, new
  flags on the `sparkle` / `sign_update` CLIs).
- **PATCH** — backward-compatible bug fixes only.

Tags are always prefixed with `v` (Go's module system requires this):
`v1.0.0`, `v1.1.0`, `v1.1.1`, …

**The first release is `v1.0.0`.** Tagging `v1.0.0` is a deliberate commitment
to a stable public API: the exported surface of the root `sparkle` package and
the command-line interfaces of `./cmd/sparkle` and `./cmd/sign_update` are now
under the SemVer contract above. Breaking any of them requires a **major**
bump — read section 6 before you consider that, because a major bump has a
mechanical cost in Go.

Pre-release testing, if ever needed, uses SemVer pre-release suffixes
(`v1.2.0-rc.1`). The module proxy and `go get` treat these as pre-releases:
they are **not** selected by `@latest` and are only used when requested
explicitly.

## 2. Before you tag: make sure `main` is green

You can only release a commit that is already on `main` and passing CI. Tags
are immutable in practice (deleting/moving a published tag breaks the module
proxy's cache and every consumer that fetched it), so verify *before* tagging.

```sh
# Be on the exact commit you intend to release.
git checkout main
git pull --ff-only origin main

# Confirm every required check is green for this commit on GitHub:
#   CI      (ci.yml)     -> build + vet + go test -race on macOS, plus a
#                           compile-only cross-build smoke (linux/windows)
#   Lint    (lint.yml)   -> golangci-lint on macOS (its own workflow, NOT ci.yml)
#   CodeQL  (codeql.yml) -> code scanning on macOS
gh run list --branch main --limit 10

# Sanity-check locally as well (see CONTRIBUTING.md):
go build ./...
go vet ./...
go test -race ./...     # root suite is ~20s wall (slower with -race)
golangci-lint run
```

Do **not** proceed until CI on `main` is passing.

## 3. Cut the release: tag and push

Create an **annotated** tag (`-a`) — the Go toolchain and `gh` expect a real
tag object with a message, not a lightweight tag. Use the version as the tag
name and a short summary as the message.

```sh
# First release:
git tag -a v1.0.0 -m "go-sparkle v1.0.0"

# Subsequent releases, e.g.:
git tag -a v1.1.0 -m "go-sparkle v1.1.0: add <feature>"

# Push the tag (and only the tag) to origin:
git push origin v1.0.0
```

`git push origin vX.Y.Z` pushes exactly that ref. (`git push --tags` also
works but pushes every local tag — pushing the single ref is cleaner.)

### What the release workflow does

Pushing a `v*` tag triggers `.github/workflows/release.yml` (guarded to run only
for `refs/tags/v*`). On the tagged commit it:

1. Runs `go test -race ./...` once on macOS (macos-latest) as a final gate. It
   is a single test run on the target platform, **not** the full CI — the CI,
   Lint, and CodeQL workflows already ran on `main` before you tagged, so the
   release job only needs a fast last check on the exact tagged commit.
2. On success, publishes a **GitHub Release** for the tag using the preinstalled
   `gh` CLI (`gh release create "$GITHUB_REF_NAME" --generate-notes --verify-tag`),
   with notes auto-generated from the merged PRs / commits since the previous tag.

It does **not** build, upload, or attach any binaries or archives — by design,
distribution is tags-only and consumers build from source. The workflow file in
`.github/workflows/release.yml` is the source of truth; if its behavior and this
document ever disagree, fix whichever is wrong.

If you prefer, you can also create the GitHub Release by hand after pushing the
tag:

```sh
gh release create v1.0.0 --generate-notes --title "go-sparkle v1.0.0"
```

## 4. How pkg.go.dev and the Go module proxy pick up the tag

Consumers fetch go-sparkle through the public module mirror
(`proxy.golang.org`), and its docs are rendered on
[pkg.go.dev](https://pkg.go.dev/github.com/alex-ivanov/go-sparkle). Neither is
tied to your GitHub Release — both key off the **Git tag**. Discovery is lazy:
the proxy caches a version the first time *anyone* asks for it, and pkg.go.dev
indexes it shortly after. This usually happens within minutes of the first
request, but you can force it immediately.

**Force the module proxy to fetch and cache the new version:**

```sh
GOPROXY=proxy.golang.org go list -m github.com/alex-ivanov/go-sparkle@v1.0.0
```

That request makes the proxy clone the tag, compute its hashes, and cache it.
The command prints the module path and version once it's live, e.g.:

```
github.com/alex-ivanov/go-sparkle v1.0.0
```

**Force pkg.go.dev to index and render the docs:** simply visit the version URL
in a browser — the first visit triggers a fetch-and-render:

```
https://pkg.go.dev/github.com/alex-ivanov/go-sparkle@v1.0.0
```

If either still shows a stale/"not found" state, wait a minute and retry the
`go list` command; propagation to the checksum database and the docs indexer
can lag slightly. The `Go Reference` badge in the README points at the latest
version and updates automatically once the proxy has the tag.

## 5. How consumers pin and install a version

Library consumers:

```sh
# Add / upgrade to a specific tagged version:
go get github.com/alex-ivanov/go-sparkle@v1.0.0

# Track the latest release:
go get github.com/alex-ivanov/go-sparkle@latest
```

CLI users install the commands directly from a pinned version (build from
source into `$GOBIN`):

```sh
go install github.com/alex-ivanov/go-sparkle/cmd/sparkle@v1.0.0
go install github.com/alex-ivanov/go-sparkle/cmd/sign_update@v1.0.0

# Or the latest release:
go install github.com/alex-ivanov/go-sparkle/cmd/sparkle@latest
```

`@vX.Y.Z` pins exactly; `@latest` resolves to the highest non-pre-release
SemVer tag. Pinning to an exact tag is the reproducible choice for downstream
projects.

## 6. The Go major-version rule (the cost of the v1.0.0 commitment)

This is the most important long-term consequence of tagging `v1.0.0`. Go's
[import compatibility rule](https://go.dev/ref/mod#major-version-suffixes) says:

> Starting at major version 2, the module path **must** end in a matching
> major-version suffix (`/v2`, `/v3`, …), and that suffix becomes part of every
> import path.

Concretely:

- **v0 and v1 use no suffix.** The current module path
  `github.com/alex-ivanov/go-sparkle` is correct for all of `v0.x.y` and
  `v1.x.y`. You can ship `v1.0.0`, `v1.1.0`, `v1.9.3`, … indefinitely with no
  path changes. Stay within v1 as long as the API only grows compatibly.

- **v2+ is a module-path change, not just a tag.** To ever release `v2.0.0` you
  must:
  1. Change the `module` line in `go.mod` to
     `github.com/alex-ivanov/go-sparkle/v2`.
  2. Update **every internal import** of the root package and subpackages to the
     `/v2` path (including imports inside `./cmd/sparkle` and
     `./cmd/sign_update`).
  3. Tag `v2.0.0`. Consumers then import
     `github.com/alex-ivanov/go-sparkle/v2` and install
     `.../go-sparkle/v2/cmd/sparkle@v2.0.0`.

  v1 and v2 are, to the toolchain, *different modules* that can coexist in the
  same build.

**Practical takeaway:** because v2+ is disruptive for you and every consumer,
treat the `v1.0.0` API as something to preserve. Prefer additive (MINOR) changes;
deprecate rather than remove; reserve a MAJOR bump (and the `/v2` migration) for
genuinely unavoidable breaking changes.

## 7. Release checklist

- [ ] Intended commit is on `main` and pulled locally (`git pull --ff-only`).
- [ ] CI, Lint, and CodeQL are all green for that commit on GitHub.
- [ ] Local `go build ./...`, `go vet ./...`, `go test -race ./...`,
      `golangci-lint run` all pass.
- [ ] Chosen the correct SemVer bump (MAJOR/MINOR/PATCH) — remembering v2+ needs
      the `/v2` module-path migration.
- [ ] `git tag -a vX.Y.Z -m "go-sparkle vX.Y.Z: ..."`
- [ ] `git push origin vX.Y.Z`
- [ ] `release.yml` succeeded and the GitHub Release exists with generated notes.
- [ ] `GOPROXY=proxy.golang.org go list -m github.com/alex-ivanov/go-sparkle@vX.Y.Z`
      returns the new version.
- [ ] `https://pkg.go.dev/github.com/alex-ivanov/go-sparkle@vX.Y.Z` renders.
