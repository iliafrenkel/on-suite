# Releasing

> This is for maintainers cutting a new version of ON Suite itself. If you
> just want to deploy an existing release, see [`docs/DEPLOYING.md`](DEPLOYING.md).

Releases are just a git tag. Pushing a tag matching `v*` triggers
[`.github/workflows/release.yml`](../.github/workflows/release.yml), which
runs [goreleaser](https://goreleaser.com) to build, package, and publish a
GitHub Release — there's no separate release script or manual build step.

## Cutting a release

1. Make sure `main` is green and up to date:

   ```bash
   git checkout main && git pull
   go build ./... && go vet ./... && go test ./... -race
   ```

2. Pick the next version ([semver](https://semver.org)) and tag it. Cutting a
   release is ad hoc — whenever a batch of merged work feels release-worthy,
   not on a schedule or a fixed set of triggers.

   ON Suite is pre-1.0 (`0.MINOR.PATCH`) until all four apps
   (README.md's "Of the four apps...") exist and feel solid; major stays `0`
   until then. The bump itself is derived from the [Conventional
   Commits](CONTRIBUTING.md#commit-messages) merged since the last tag, the
   same classification `.goreleaser.yaml`'s `changelog.groups` already uses:

   | Commits since the last tag                                | Bump                |
   | ----------------------------------------------------------- | ------------------- |
   | any `feat:`, or a `BREAKING CHANGE:` footer / `type!:`       | minor (`0.(N+1).0`) |
   | only `fix:`/`refactor:`/`perf:`/`chore:` (or unlabeled)      | patch (`0.N.(P+1)`) |
   | only `docs:`/`test:`                                         | no release needed   |

   A feature and a breaking change bump the same digit pre-1.0 because
   major is pinned at `0` — there is nowhere else for "breaking" to signal.
   Once ON Suite crosses 1.0, this becomes ordinary semver: breaking →
   major, `feat` → minor, `fix` → patch.

   [`scripts/next-version.sh`](../scripts/next-version.sh) applies this rule
   for you — it only prints a suggestion, review it before tagging:

   ```bash
   ./scripts/next-version.sh
   # v0.4.0
   git tag -a v0.4.0 -m "v0.4.0"
   git push origin v0.4.0
   ```

   The tag must start with `v` — that's what the workflow's `on.push.tags:
   ['v*']` filter matches.

3. Watch the run under the repo's Actions tab. Goreleaser will:
   - re-run `go mod tidy` and `go test ./... -race` as a final gate
     ([`.goreleaser.yaml`](../.goreleaser.yaml) `before.hooks`),
   - cross-compile `onsuite` for `linux/amd64`, `linux/arm64`, `darwin/arm64`,
     `windows/amd64` with `CGO_ENABLED=0`, stamping the tag into
     `main.version`,
   - package each binary into a `.tar.gz` (`.zip` for Windows) alongside
     `README.md`, `LICENSE`, `docs/DEPLOYING.md`, and `docs/onsuite.service`,
     plus a `checksums.txt`,
   - sign `checksums.txt` with [cosign](https://docs.sigstore.dev/cosign/overview/)
     using keyless (Sigstore) signing, producing `checksums.txt.sig` and
     `checksums.txt.pem`,
   - generate release notes from `git log`, grouped into New Features, Bug
     Fixes, Improvements, and Everything Else (for the curious) based on each
     commit's Conventional Commits type — see
     [`CONTRIBUTING.md`](../CONTRIBUTING.md#commit-messages) for the type-to-section
     mapping. Commits starting with `docs:` or `test:`, and merge commits,
     are dropped entirely,
   - publish everything as a GitHub Release named after the tag.

`actions/checkout` and `goreleaser-action` use `permissions: contents:
write` as before. Signing needs one more: `permissions: id-token: write`,
which lets the workflow request a short-lived GitHub OIDC token — that
token *is* the signing identity, so there's no private key for this project
to generate, store, or rotate.

## Verifying a release

Anyone downloading a release can confirm it was actually built by this
repo's release workflow, not just that the file wasn't corrupted in
transit:

```bash
cosign verify-blob --certificate checksums.txt.pem --signature checksums.txt.sig \
  --certificate-identity-regexp 'https://github.com/iliafrenkel/on-suite/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum --check --ignore-missing checksums.txt
```

The first command verifies the signature over `checksums.txt` itself; the
second then verifies every archive's checksum against it. Verifying only
the archive's checksum without the signature just confirms the download
wasn't corrupted — not who built it.

## If something goes wrong

Goreleaser refuses to reuse a tag. To redo a release, delete the tag both
locally and on the remote, fix the problem, and tag again:

```bash
git tag -d v0.4.0
git push origin :refs/tags/v0.4.0
```

Then also delete the (likely partial) GitHub Release it created before
re-pushing the tag, or goreleaser will fail on the name collision.

## What this doesn't cover

- **Docker.** The [`Dockerfile`](../Dockerfile) isn't built or published by
  this workflow — build and push it yourself if you want a versioned image:

  ```bash
  docker build --build-arg VERSION=v0.4.0 -t onsuite:v0.4.0 .
  ```

- **Deploying a release to a server** — see [`docs/DEPLOYING.md`](DEPLOYING.md).
