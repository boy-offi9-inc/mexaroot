# Release checklist

How to cut a mexaroot release. The version lives in one place - the git tag -
and is injected at build time; a plain `go build` still reports `dev`.

## One-time setup

- [ ] Install goreleaser (or let CI do the work - you only need it for
      dry-runs): `go install github.com/goreleaser/goreleaser/v2@latest`
- [ ] Confirm `GITHUB_TOKEN` is available when running goreleaser locally
      (CI provides its own).

## Every release

1. **Sanity**: on `main`, up to date with origin, CI green on the latest
   commit (`go test -race ./...`, `go vet ./...`, `gofmt` clean locally too).

2. **CHANGELOG.md**: move everything that shipped under `Unreleased` into a
   new `## <version> - <YYYY-MM-DD>` section, in the Keep a Changelog shape
   (`Added` / `Changed` / `Fixed` / `Security`). Leave an empty
   `## Unreleased` above it. This file is the human-readable record; the
   GitHub Release body comes from GoReleaser's generated notes plus the
   header/footer in `.goreleaser.yaml`.

3. **Docs check**: does `docs/ROADMAP.md`'s intro paragraph ("What exists
   today...") still match? Does the README claim anything the release
   doesn't ship? Fix both in the same commit if so.

4. **Dry run** (no publish):
   ```bash
   goreleaser release --snapshot --clean
   ```
   Inspect `dist/`: both binaries per platform, `checksums.txt`, and confirm
   `mexaroot -version` on one built binary prints the snapshot version, not
   `dev`.

5. **Tag and push**:
   ```bash
   git tag -a vX.Y.Z -m "vX.Y.Z"
   git push origin vX.Y.Z
   ```
   The release workflow (`.github/workflows/release.yml`) runs on the tag,
   executes `goreleaser release --clean`, and publishes the GitHub Release
   with the archives and `checksums.txt`. Nothing publishes without a tag.

6. **Verify after CI finishes**:
   - [ ] GitHub Release exists with assets for linux/amd64, linux/arm64,
         darwin/arm64, windows/amd64 (both `mexaroot` and `mexarootd`), plus
         `checksums.txt`
   - [ ] Download one archive, run the binary with `-version`: prints
         `X.Y.Z`
   - [ ] `sha256sum -c checksums.txt` passes in the unpacked downloads dir
   - [ ] `go install github.com/boy-offi9-inc/mexaroot/cmd/mexaroot@vX.Y.Z`
         works and `mexaroot -version` prints `X.Y.Z` (this is a roadmap
         item - the first release is where it gets verified for real)

7. **Follow-ups**: update `docs/ROADMAP.md` (v0.1.0 items move to Done),
   and if this was a big release, bump the version mention in the daemon
   banner docs if any exist.

## Notes and gotchas

- Version is injected via
  `-X github.com/boy-offi9-inc/mexaroot/internal/version.Version={{.Version}}`
  in `.goreleaser.yaml`; both `cmd/mexaroot` and `cmd/mexarootd` are built
  with it. A binary built without the ldflags reports `dev` - that's
  expected for hand builds.
- The Windows service-install question (ADR 0013, console program under the
  service manager) is still open - do not promise Windows daemon service
  support in release notes until it's verified on Windows.
- Termux users: the arm64 Linux tarball runs under Termux, but the
  roadmap's "static arm64 build on Android" verification (DNS, certs) must
  be checked before claiming it in notes.
