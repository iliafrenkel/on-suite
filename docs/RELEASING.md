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

2. Pick the next version ([semver](https://semver.org)) and tag it:

   ```bash
   git tag -a v0.4.0 -m "v0.4.0"
   git push origin v0.4.0
   ```

   The tag must start with `v` — that's what the workflow's `on.push.tags:
   ['v*']` filter matches.

3. Watch the run under the repo's Actions tab. Goreleaser will:
   - re-run `go mod tidy` and `go test ./... -race` as a final gate
     ([`.goreleaser.yaml`](../.goreleaser.yaml) `before.hooks`),
   - cross-compile `onsuite` for `linux/amd64`, `linux/arm64`, `darwin/amd64`,
     `darwin/arm64` with `CGO_ENABLED=0`, stamping the tag into
     `main.version`,
   - package each binary into a `.tar.gz` alongside `README.md`, `LICENSE`,
     `docs/DEPLOYING.md`, and `docs/onsuite.service`, plus a `checksums.txt`,
   - sign `checksums.txt` with [cosign](https://docs.sigstore.dev/cosign/overview/)
     using keyless (Sigstore) signing, producing `checksums.txt.sig` and
     `checksums.txt.pem`,
   - generate release notes from `git log` (commits starting with `docs:`,
     `test:`, or that are merge commits are filtered out — keep that in mind
     when writing commit messages for anything you want in the changelog),
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
