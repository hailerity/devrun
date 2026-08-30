---
name: release
description: Cut a new devrun release — pick the semver bump, run pre-flight checks, push the vX.Y.Z tag, and verify the GoReleaser GitHub Actions run. Use when asked to "release", "cut a release", "tag a version", "ship vX.Y.Z", or publish a new build.
---

# Releasing devrun

Releases are **tag-driven**. Pushing a `v*` tag to GitHub triggers `.github/workflows/release.yml`,
which runs GoReleaser (`.goreleaser.yaml`) to build the binaries, generate the changelog, and
publish a GitHub Release. There is no Homebrew tap and no `CHANGELOG.md` to edit. The version is
injected at build time via ldflags — **never edit `internal/cli/version.go`**.

Your job is everything up to and including the tag push, then confirming the automation succeeded.

## 1. Sync and confirm a clean base

```sh
git checkout main
git pull --ff-only origin main
git status --porcelain          # MUST be empty
gh run list --branch main --limit 3 --workflow CI   # latest run on HEAD must be "success"
```

Do not release if the tree is dirty, `main` isn't fast-forwarded, or CI on the release commit
is not green.

## 2. Pick the version

```sh
git describe --tags --abbrev=0                       # last released tag
git log --oneline "$(git describe --tags --abbrev=0)"..HEAD \
  | grep -vE 'Merge (pull request|branch|remote)'    # what's unreleased
```

Choose the next semver from the unreleased commits (project is **pre-1.0**):

| Unreleased commits contain… | Bump |
|---|---|
| any `feat:` | minor — `0.X.0` |
| only `fix:` / `perf:` / `docs:` / `chore:` | patch — `0.x.Y` |
| a breaking change (`feat!:`, `fix!:`, `BREAKING CHANGE:`) | minor while 0.x — **call it out explicitly to the user** |

`docs:`, `test:`, `chore:` and merge commits are filtered out of the generated changelog, but
still count toward "there is something to release". **State the proposed version and the reason,
and get the user's confirmation before tagging.**

## 3. Pre-flight checks

Run locally (these mirror CI, but catch problems before the tag is public):

```sh
go build ./...
go test ./... -count=1
make test-integration
go mod tidy && git diff --exit-code -- go.mod go.sum   # must be clean
golangci-lint run        # if installed locally; otherwise note it's covered by CI
govulncheck ./...         # optional; covered by CI
```

If `goreleaser` is installed, dry-run the actual release build:

```sh
goreleaser check
goreleaser release --snapshot --clean --skip=publish
```

If it isn't installed, skip — the CI job is the source of truth. Do not block the release on a
missing local `goreleaser`.

## 4. Tag and push

```sh
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z
```

Use an **annotated** tag. The `v` prefix is required (`.goreleaser.yaml` and the install script
both assume it).

## 5. Watch the Release workflow

```sh
gh run watch "$(gh run list --workflow Release --limit 1 --json databaseId -q '.[0].databaseId')" --exit-status
```

If it fails **before publishing**, fix the cause, then delete and recreate the tag:

```sh
git tag -d vX.Y.Z
git push origin :refs/tags/vX.Y.Z
# ...fix, commit, merge to main, then re-tag from the new HEAD
```

## 6. Verify the published release

```sh
gh release view vX.Y.Z
gh release view vX.Y.Z --json assets -q '.assets[].name'
```

Expect these assets:

- `devrun_vX.Y.Z_linux_amd64.tar.gz`
- `devrun_vX.Y.Z_linux_arm64.tar.gz`
- `devrun_vX.Y.Z_darwin_amd64.tar.gz`
- `devrun_vX.Y.Z_darwin_arm64.tar.gz`
- `checksums.txt`
- source archives (auto)

Confirm the `prerelease` flag matches intent (see below), and that the generated release notes
list the expected `feat`/`fix` commits.

## 7. Post-release checks

```sh
curl -fsSL https://api.github.com/repos/hailerity/devrun/releases/latest | grep '"tag_name"'
```

Smoke-test the install path in a scratch dir:

```sh
DEVRUN_INSTALL="$(mktemp -d)" DEVRUN_VERSION=vX.Y.Z \
  sh scripts/install.sh && "$DEVRUN_INSTALL/devrun" --version
```

`devrun --version` should print `devrun X.Y.Z (commit: …, built: …)`.

## Prereleases

Tag with a pre-release identifier: `vX.Y.Z-rc.1`. GoReleaser's `prerelease: auto` detects the
`-` and marks the GitHub Release as a prerelease. GitHub's "latest release" (and therefore the
default `install.sh` path) skips prereleases; test one with `DEVRUN_VERSION=vX.Y.Z-rc.1`.

## Fixing a bad published release

Prefer rolling forward with a new patch release. Only delete a published release if you're
certain nothing has consumed it:

```sh
gh release delete vX.Y.Z --yes
git push origin :refs/tags/vX.Y.Z
git tag -d vX.Y.Z
```
