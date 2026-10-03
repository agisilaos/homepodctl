# Releasing

Release publication uses a reviewed changelog and a clean macOS checkout of
`main`. The CLI and both macOS archives keep their existing CGO-free build.

## Local verification

Run `make verify` while developing. It accepts local changes, verifies the pinned
helper bundle, checks module metadata before Go commands, then runs formatting,
vet, tests, docs/help, a development-stamped binary proof and release fixtures.
`scripts/release-check.sh` with no arguments remains the version-independent
entry point for the ordinary checks and development build. Module validation
uses temporary alternate files and leaves the checkout unchanged on errors.

Fish completion checks remain part of the Go suite. Fish is optional locally;
both macOS CI jobs install it. Ordinary CI runs `make verify` with Go 1.27.1
from `go.mod`; release CI uses the same version and
`make release-check-ci`, which allows a historical top section and tag.

Release commands select `go1.27.1` through `GOTOOLCHAIN`, as pinned in
`scripts/release-config.sh`. Go downloads and verifies that toolchain if it is
not installed. The module and all CI jobs require Go 1.27.1.

## Prepare the changelog

Start with the repository evidence:

```bash
make changelog-context VERSION=vX.Y.Z
```

Prepare only the new top section of `CHANGELOG.md`, describing user-visible
outcomes and linking every list item to its verified merged GitHub pull request
or direct commit. Group related changes, state breaking changes, and preserve
existing release sections. Review and commit the concrete section before the
human release gates.

## Validate and publish

Run the commands in order:

```bash
make release-check VERSION=vX.Y.Z
make release-dry-run VERSION=vX.Y.Z
make release VERSION=vX.Y.Z
```

Human preflight requires clean tracked, staged and untracked state, an unused
tag, and the requested first changelog section with traceable list items. It
runs all verification and checks the candidate's exact version metadata.

The offline dry run builds darwin/amd64 and darwin/arm64 archives, verifies their
contents, metadata and checksums, extracts the approved release notes into
`dist/NOTES.md`, and renders and syntax-checks the Homebrew formula. It performs
no publication writes. A dry run may warn about another checkout branch;
actual publication requires `main`.

Before its first publication write, the publisher clones the selected existing
tap branch and prepares its validated formula commit. It then creates and pushes
the version tag, creates the GitHub release with the approved notes and assets,
and pushes the formula to that branch. `HOMEBREW_TAP_URL` overrides the default
HTTPS transport; `HOMEBREW_TAP_BRANCH` selects the existing branch. `GITHUB_REPO`
controls the formula URLs and explicit GitHub CLI publishing target, while tag
pushes use `origin`. Verify that those targets identify the intended repository.

If publication stops, preserve the original archives, notes, checksums and
prepared formula, and follow [manual recovery](docs/release-recovery.md).

## Changelog policy

- Use concrete headings: `## [vX.Y.Z] - YYYY-MM-DD`.
- Do not add an `Unreleased` section.
- Reviewed changelog wording is the source of truth for GitHub release notes.
