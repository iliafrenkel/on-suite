# What's new: changelog and badge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an in-app "What's new" changelog page with a per-user badge that appears after an upgrade, and link the footer version to it.

**Architecture:** `docs/user/changelog.md` is one more embedded help guide, served at `/help/changelog`. A `users.seen_version` column, loaded with the session user on every request, drives `render.Shell.WhatsNew()`. Opening the changelog page while signed in writes the running version into `seen_version`.

**Tech Stack:** Go, SQLite migrations (`internal/platform/auth/migrations`), html/template, goldmark help pages, POSIX sh.

**Spec:** `docs/superpowers/specs/2026-11-26-whats-new-changelog-design.md`

## Global Constraints

- The changelog URL is `/help/changelog`. Its help-sidebar label is "What's new", and it comes after Administration.
- Each changelog entry heading is `## N.N.N: Title`, newest first. A date line `*D Month YYYY*` and the summary follow it.
- The badge shows only when the user is logged in, the version isn't `""` or `"dev"`, and `SeenVersion != Version`. It's a plain inequality, not a semver comparison.
- Existing users get `seen_version = ''`. New accounts get the running version.
- The footer version links to `/help/changelog`. A `dev` build's version stays plain text.
- Use only existing CSS tokens for the dot (`--c-accent-attention`). It must work in both palettes and both themes.
- Never push to `main`. The work goes in one PR from `feat/whats-new`.

## Review Focus

- **A signed-out visitor opens `/help/changelog`.** The page renders, nothing is written and nothing panics. Test in Task 3.
- **A `dev` build opens the changelog.** `seen_version` isn't overwritten with `"dev"`, so a later real version still shows the badge. Test in Task 3.
- **An HTMX or error page renders for a signed-in user.** The error page's shell gets the badge too, so the menu doesn't flicker. Test in Task 2.
- **`onsuite user add` creates an account.** It starts as seen, because the CLI path calls `SetVersion`. Test in Task 1 at the store level; the wiring is checked by reading the code.
- **A malformed changelog entry is merged**, such as a missing version or an entry out of order. The test fails in CI rather than the page silently rendering wrong. Test in Task 3.

---

### Task 1: Store `seen_version`

**Files:**
- Create: `internal/platform/auth/migrations/0005_seen_version.sql`
- Modify: `internal/platform/auth/store.go` (User, Store, CreateUser, UserByUsername, UserByID, scanUser)
- Modify: `cmd/onsuite/serve.go:52`, `cmd/onsuite/user.go:73` (call `SetVersion(version)`)
- Test: `internal/platform/auth/store_test.go`

**Interfaces:**
- Produces: `auth.User.SeenVersion string`; `func (s *Store) SetVersion(v string)`; `func (s *Store) MarkSeen(ctx context.Context, userID int64, version string) error`

- [ ] **Step 1: Write the failing tests**
  - `TestNewUserStartsSeenAtTheRunningVersion`: call `s.SetVersion("6.5.0")`, then `CreateUser`, then `UserByID`. Assert `SeenVersion == "6.5.0"`.
  - `TestNewUserWithoutAVersionStartsUnseen`: with no `SetVersion`, `SeenVersion == ""`.
  - `TestMarkSeen`: run `MarkSeen(ctx, id, "6.5.0")`, then `UserByUsername`. Assert `SeenVersion == "6.5.0"`. `MarkSeen` on a missing id returns `ErrNotFound`.
- [ ] **Step 2: Run** `go test ./internal/platform/auth/ -run 'Seen'`. Expect a compile failure, since there's no `SeenVersion` yet.
- [ ] **Step 3: Implement.** The migration is `ALTER TABLE users ADD COLUMN seen_version TEXT NOT NULL DEFAULT '';`. Add a header comment in the existing style: existing users get `''`, so they see the badge once. Add `version string` to `Store`. `CreateUser` inserts `seen_version = s.version`. Add `seen_version` to both SELECTs and to `scanUser`. `MarkSeen` is an `UPDATE`, and it returns `ErrNotFound` when no rows are affected. In `serve.go` and `user.go`, call `SetVersion(version)` on the store right after `NewStore`.
- [ ] **Step 4: Run** `go test ./internal/platform/auth/ ./cmd/...`. Expect PASS.
- [ ] **Step 5: Commit** with `feat(platform): remember the last version each user saw`.

### Task 2: Badge, menu item and footer link

**Files:**
- Modify: `internal/platform/render/render.go` (Shell)
- Modify: `internal/platform/app/app.go` (`NewPage`), `internal/platform/web/errors.go` (shell user block)
- Modify: `internal/ui/templates/base.html` (user menu summary, menu panel, footer)
- Modify: the shell stylesheet that holds `.shell-user-toggle` and `.app-footer` (find it with `grep -rn shell-user-toggle internal/ui/static`)
- Test: `internal/platform/render/render_test.go` (or the existing render test file), `internal/platform/help/handlers_test.go`

**Interfaces:**
- Consumes: `auth.User.SeenVersion`
- Produces: `render.Shell.SeenVersion string`; `func (s Shell) WhatsNew() bool`

- [ ] **Step 1: Write the failing tests**
  - `TestShellWhatsNew` is a table test over `{LoggedIn, Version, SeenVersion} → want`:
    - `{false,"6.5.0",""}` → false
    - `{true,"dev",""}` → false
    - `{true,"",""}` → false
    - `{true,"6.5.0","6.5.0"}` → false
    - `{true,"6.5.0","6.4.0"}` → true
    - `{true,"6.5.0",""}` → true
  - In the help handler tests, set the fixture server's version to `"6.5.0"` and make `ilia` start at `""`:
    - `TestWhatsNewBadgeShowsForAnUnseenVersion`: `GET /help/notes` signed in. It must have `.shell-user-toggle .whats-new-dot` and `a.shell-menu-item[href="/help/changelog"] .whats-new-dot`.
    - `TestFooterVersionLinksToTheChangelog`: the page must have `.app-footer a[href="/help/changelog"]` with text `6.5.0`.
    - `TestErrorPageKeepsTheBadge`: `GET /help/nope` signed in. The 404 page has `.whats-new-dot`.
- [ ] **Step 2: Run** `go test ./internal/platform/render/ ./internal/platform/help/`. Expect FAIL.
- [ ] **Step 3: Implement.**
  - Add `SeenVersion` to `Shell`, with the `WhatsNew()` method per the Global Constraints.
  - `NewPage` and `errors.go` set `SeenVersion = u.SeenVersion` in their logged-in blocks.
  - In the template, add `{{if .Shell.WhatsNew}}<span class="whats-new-dot"><span class="visually-hidden">What's new</span></span>{{end}}` inside the summary. Use the project's existing visually-hidden class; grep for it.
  - Add a menu item after Help: `<a class="shell-menu-item" href="/help/changelog">{{ticon "sparkle-or-existing-icon"}}<span>What's new</span>{{dot}}</a>`. Pick an existing `ticon` name and don't draw a new icon. `help` is acceptable if nothing fits.
  - The footer is `ON Suite{{with .Shell.Version}} · {{if eq . "dev"}}{{.}}{{else}}<a href="/help/changelog">{{.}}</a>{{end}}{{end}}`.
  - The dot is an 8px circle in `var(--c-accent-attention)`, with the footer link in the footer's own colour.
- [ ] **Step 4: Run** `go test ./internal/platform/...`. Expect PASS.
- [ ] **Step 5: Commit** with `feat(ui): What's new badge and footer changelog link`.

### Task 3: Changelog page and marking it seen

**Files:**
- Create: `docs/user/changelog.md`
- Create: `docs/changelog_test.go`
- Modify: `internal/platform/help/pages.go` (`order`), `internal/platform/help/handlers.go` (Deps, page)
- Modify: `cmd/onsuite/stack.go`, where `help.Deps` is built, to pass `Users`
- Modify: `internal/platform/help/fixture_test.go` (pass Users and Version), `handlers_test.go` (sidebar count 10 → 11)
- Modify: `docs/user/index.md`, adding a line pointing to What's new

**Interfaces:**
- Consumes: `(*auth.Store).MarkSeen`, `render.Shell.WhatsNew()`
- Produces: `help.Deps.Users *auth.Store`

- [ ] **Step 1: Write the failing tests**
  - In `docs/changelog_test.go`, `TestChangelogEntries` reads `user/changelog.md`. Assert:
    - the first line is `# What's new`
    - every `## ` line matches `^## (\d+)\.(\d+)\.(\d+): \S.*$`
    - each heading is followed by a date line matching `^\*\d{1,2} [A-Z][a-z]+ \d{4}\*$`
    - versions are strictly descending
    - there's at least one entry
  - In the help tests:
    - `TestOpeningTheChangelogClearsTheBadge`: `GET /help/changelog` signed in. That response has no `.whats-new-dot`, and the next `GET /help/notes` has none either.
    - `TestSignedOutChangelogRenders`: returns 200 with no panic.
    - `TestDevBuildDoesNotMarkSeen`: with a fixture at version `"dev"`, `GET /help/changelog`, then reload the user from the store. `SeenVersion` is still `""`.
- [ ] **Step 2: Run** `go test ./docs/ ./internal/platform/help/`. Expect FAIL.
- [ ] **Step 3: Write `docs/user/changelog.md`.** Backfill every `v*` tag, newest first, from `git for-each-ref refs/tags --sort=-v:refname --format='%(refname:short)|%(creatordate:format:%-d %B %Y)|%(contents)'`.
  - The heading is the version without its `v`, plus the tag subject.
  - For a tag whose subject is just the version, write a short title and summary from that release's merged PR titles (`git log --oneline vPREV..vX --first-parent`).
  - Add a new top entry for this release, using the version `scripts/next-version.sh` suggests. It's expected to be `6.5.0`, a minor. Leave Ilia to rename it before tagging.
- [ ] **Step 4: Implement.**
  - Add `{"changelog", "What's new"}` to `order` after admin.
  - Add `Users *auth.Store` to `Deps`.
  - In `page`, when `pg.Slug == "changelog"`, the user is signed in, `h.d.Version` isn't `""` or `"dev"`, and `u.SeenVersion != h.d.Version`:
    - call `MarkSeen`
    - on error, log it with the request's logger if there is one; otherwise ignore it, since a failed write only means the badge stays
    - set `page.Shell.SeenVersion = h.d.Version` so this response renders without the dot
  - The `title()` for this page should read "What's new", not "What's new help". Special-case it beside `index`.
- [ ] **Step 5: Run** `go test ./docs/ ./internal/platform/help/ ./cmd/...`. Expect PASS.
- [ ] **Step 6: Commit** with `feat(help): What's new changelog page`.

### Task 4: Release routine

**Files:**
- Create: `scripts/tag-message.sh`
- Modify: `docs/developers/releasing.md` (Cutting a release, step 2), `docs/developers/repository-layout.md` (scripts list)

- [ ] **Step 1: Write `scripts/tag-message.sh VERSION`.**
  - `VERSION` is with or without the leading `v`.
  - The script prints the top changelog entry as a tag message: the title, a blank line, then the summary paragraphs, without the date line.
  - It exits non-zero with a clear message if the top entry's version differs from `VERSION`, or if there's no argument.
  - Use `set -eu` and plain awk, so it works with BSD and GNU tools.
- [ ] **Step 2: Verify:**
  - `scripts/tag-message.sh 6.5.0` prints the new entry.
  - `scripts/tag-message.sh 9.9.9` fails with an error naming both versions.
- [ ] **Step 3: Update `releasing.md`.** Before tagging, add the entry to `docs/user/changelog.md` in a PR and merge it. Then tag with `git tag -a vX.Y.Z -F <(scripts/tag-message.sh X.Y.Z)`. Replace the heredoc example, and keep the "check with `git show`" advice.
- [ ] **Step 4: Run the full check** `go build ./... && go vet ./... && go test ./... -race` (plus whatever AGENTS.md lists as the full gate). Expect PASS.
- [ ] **Step 5: Commit** with `docs(release): cut tags from the in-app changelog`.

### After the tasks

- Check it in the browser:
  - the badge in Light and Dark, and in Warm and Ink
  - the phone width
  - the badge clearing
  - the footer link
- Open the PR with `closes` per issue, if any.
- File the follow-up issue: "Admin: tell me when a newer ON Suite release exists". Server checks GitHub releases, opt-out, admin-only; references this PR.
