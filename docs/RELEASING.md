# Releasing

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
     and `docs/deploy/`, plus a `checksums.txt`,
   - generate release notes from `git log` (commits starting with `docs:`,
     `test:`, or that are merge commits are filtered out — keep that in mind
     when writing commit messages for anything you want in the changelog),
   - publish everything as a GitHub Release named after the tag.

No step needs `GITHUB_TOKEN` beyond what `actions/checkout` and
`goreleaser-action` already have via the workflow's `permissions: contents:
write`.

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

- **Deploying a release to a server** — see
  [`docs/deploy/README.md`](deploy/README.md).
