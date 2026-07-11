# Contributing to go-sparkle

Thanks for helping improve go-sparkle. This is a small, dependency-free Go
library plus two CLIs (`./cmd/sparkle`, `./cmd/sign_update`). Contributions of
all sizes are welcome — please keep the project's core constraints (below) in
mind before you start.

## Project constraints (read first)

- **Standard library only.** go-sparkle deliberately has **zero third-party
  dependencies** (it uses only stdlib such as `crypto/ed25519`, `encoding/xml`,
  and `net/http`), and there is no `go.sum`. **Do not add a new third-party
  dependency without opening an issue to discuss it first.** Most PRs that pull
  in a module will be asked to solve the problem with the stdlib instead.
- **macOS-targeted, but keep the stubs compiling.** go-sparkle targets macOS.
  Some files are build-tagged per OS (`apply_darwin.go` / `apply_other.go`,
  `cmd/sign_update/keychain_darwin.go` / `keychain_other.go`). The `*_darwin.go`
  files carry the real implementation and are built and tested on macOS; the
  `*_other.go` files are non-darwin stubs that are **compile-checked but not
  tested** (CI cross-builds them for `linux`/`windows`). Changes to platform
  code must keep the `*_darwin.go` variants building and passing on macOS **and**
  the `*_other.go` stubs compiling.
- **Stable API.** From `v1.0.0` on, the exported API and CLI surfaces follow
  SemVer (see [RELEASING.md](RELEASING.md)). Prefer additive, backward-compatible
  changes; flag anything breaking in your PR description.

## Build, test, and lint locally

You need a Go toolchain matching the `go` directive in `go.mod` (Go 1.26+), and
macOS to run the full suite (the darwin code paths shell out to `ditto`,
`hdiutil`, and `/usr/bin/security`).

```sh
# Build everything (library + both CLIs):
go build ./...

# Vet:
go vet ./...

# Run the full test suite with the race detector:
go test -race ./...

# Confirm the non-darwin stubs still compile (what CI's cross-build job checks):
CGO_ENABLED=0 GOOS=linux   go build ./...
CGO_ENABLED=0 GOOS=windows go build ./...
```

The root package test suite takes roughly **20 seconds** of wall time (it
includes an intentionally slow test), and `-race` makes it slower — that's
expected, not a hang.

Lint with [golangci-lint](https://golangci-lint.run/) (v2) using the
repository's configuration:

```sh
golangci-lint run
```

Please run `build`, `vet`, `test -race`, and `golangci-lint run` before pushing.

## CI expectations

Every push and pull request runs several GitHub Actions workflows, all of which
must be green before a PR can merge:

- **CI** (`ci.yml`) — on **macOS**: builds, vets, and runs `go test -race ./...`
  (currently on Go 1.26), plus a **compile-only cross-build** job that builds
  for `linux` and `windows` so the `*_other.go` stubs can't silently break.
  This workflow does **not** run golangci-lint.
- **Lint** (`lint.yml`) — runs `golangci-lint` (v2) with the repository's
  `.golangci.yml`, on macOS so the darwin-tagged source is type-checked.
- **CodeQL** (`codeql.yml`) — code scanning on macOS, on push/PR and weekly.

Dependabot additionally keeps the GitHub Actions (and any future Go modules) up
to date. Test jobs are given a generous timeout because of the slow test plus
`-race`; if you add a long-running test, keep that in mind.

## Pull request process

1. **Discuss first for anything non-trivial** — especially new dependencies, new
   exported API, or behavior changes. Open an issue.
2. Fork and create a topic branch from `main`.
3. Make focused commits with clear messages. Keep the change small and
   single-purpose where possible.
4. **Add or update tests** for your change, and update docs/README where user-
   facing behavior changes.
5. Ensure `go build ./...`, `go vet ./...`, `go test -race ./...`, and
   `golangci-lint run` all pass locally.
6. Open the PR against `main`. Describe *what* changed and *why*; call out any
   API/CLI changes and whether they are backward-compatible.
7. Make sure CI is green and address review feedback. Maintainers merge once CI
   passes and the change is approved.

Releases are cut by the maintainer from `main` as SemVer tags — see
[RELEASING.md](RELEASING.md).
