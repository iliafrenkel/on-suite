# Documentation overhaul and in-app help — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite the README for users, write a complete user guide per app with
screenshots, move developer material into `docs/developers/`, and serve the user
guides inside ON Suite at `/help` (issues #308 and #309).

**Architecture:** User guides are Markdown under `docs/user/`, readable on GitHub
as-is. A leaf `docs` package embeds them; `internal/platform/help` renders them
once at startup with goldmark, sanitises with bluemonday, rewrites relative links
to `/help/...`, and serves them publicly inside the normal shell. Screenshots are
reproducible: a seed program fills a demo data dir and a pure-Go capture tool
drives headless Chrome over the DevTools protocol.

**Tech Stack:** Go 1.26, `html/template`, `github.com/yuin/goldmark` (new),
`github.com/microcosm-cc/bluemonday` (existing), `golang.org/x/net/websocket`
(existing module), headless Google Chrome (local tooling only).

**Spec:** [`docs/superpowers/specs/2026-09-27-documentation-design.md`](../specs/2026-09-27-documentation-design.md)

## Global Constraints

- No CGO; every dependency pure Go. The only new module dependency is `github.com/yuin/goldmark`.
- No Node, no npm, no JavaScript build step. The capture tool is Go; Chrome is a local tool, never a build dependency.
- No inline `<script>` or `style=` attributes anywhere (CSP). Rendered help HTML must not carry `style` attributes either.
- Apps never import apps; the platform never imports an app. `docs` (the embed package) imports only `embed`/`io/fs`. goldmark is imported only by `internal/platform/help`.
- One page per app guide. Help pages are public (work signed out) and admin help is visible to everyone.
- App guides use light-theme screenshots only; README hero/thumbnails get light + dark variants.
- App guides in `docs/user/` use only relative links to other `docs/user/*.md` pages, `images/*.png`, in-page `#anchors`, or absolute `https://` URLs. No `../` links, no raw HTML.
- Keep export/download names generic (e.g. `notes-export.md`); don't describe title-derived filenames.
- The full check `go build ./... && go vet ./... && go test ./... -race` passes at the end of every task.
- Commits: Conventional Commits, `docs:`/`feat(platform):`/`test:` etc., referencing `#308` or `#309`.
- Never push to `main`; each PR from its own branch.

## File Structure

**PR 1 — #308 (branch `docs/308-user-docs`)**

| Path | Responsibility |
|---|---|
| `docs/doc.go` | `package docs` — package comment only (PR 2 adds the embed) |
| `docs/links_test.go` | link/image/anchor integrity for README, CONTRIBUTING and `docs/{user,developers,self-hosting}` |
| `docs/self-hosting/deploying.md`, `docs/self-hosting/onsuite.service` | moved from `docs/DEPLOYING.md`, `docs/onsuite.service` |
| `docs/developers/{index,getting-started,architecture,repository-layout,adding-an-app,testing,releasing}.md` | developer docs; `releasing.md` moved from `docs/RELEASING.md` |
| `docs/screenshots/seed/main.go`, `seed.go`, `seed_test.go`, `fixtures/*` | demo data seed |
| `docs/screenshots/capture/main.go`, `cdp.go`, `shots.go` | headless Chrome capture tool |
| `docs/screenshots/README.md` | how to regenerate screenshots |
| `docs/user/{index,paste,notes,reader,flash,admin}.md`, `docs/user/images/*.png` | user guides |
| `docs/images/*.png` | README hero + thumbnails (light and dark) |
| `README.md`, `CONTRIBUTING.md`, `AGENTS.md`, `.goreleaser.yaml` | rewritten / reference updates |

**PR 2 — #309 (branch `feat/309-in-app-help`, from `main` after PR 1 merges)**

| Path | Responsibility |
|---|---|
| `docs/embed.go` | `//go:embed user` → `docs.User() fs.FS` |
| `internal/platform/help/pages.go` | load, render, sanitise, cache pages; link rewriting |
| `internal/platform/help/handlers.go` | `/help`, `/help/{slug}`, `/help/images/{file}` |
| `internal/platform/help/*_test.go` | unit + HTTP tests |
| `internal/ui/templates/help.html`, `internal/ui/static/app.css` | help page layout + styles |
| `internal/ui/templates/base.html`, `internal/ui/toolbar_icons.go` | Help menu item + `help` icon |
| `internal/arch/arch_test.go` | scan `docs`, `docs` leaf rule, goldmark containment |
| `cmd/onsuite/stack.go` | wire `help.Routes` |
| `.dockerignore`, spec/CONTRIBUTING/developer docs | build + dependency list updates |

---

# PR 1 — Documentation (#308)

### Task 1: Branch, `docs` package and link-integrity test

**Files:**
- Create: `docs/doc.go`
- Create: `docs/links_test.go`

**Interfaces:**
- Produces: `package docs` at `github.com/iliafrenkel/on-suite/docs`; `go test ./docs/` runs link checks every later task relies on. Exported helper for PR 2: none (test-only). The slug function `githubSlug(string) string` lives in this test file.

- [ ] **Step 1: Create the branch**

```bash
git checkout main && git pull && git checkout -b docs/308-user-docs
```

(If the spec/plan PR hasn't merged yet, branch from `docs/308-309-documentation-spec` instead and rebase later.)

- [ ] **Step 2: Create `docs/doc.go`**

```go
// Package docs holds ON Suite's documentation.
//
// It is a Go package only so the user guides under user/ can be embedded
// into the binary and served at /help (#309), and so links_test.go can
// check every document's links on every test run. It imports nothing.
package docs
```

- [ ] **Step 3: Write the link-integrity test**

`docs/links_test.go`:

```go
package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// linkRe matches Markdown links and images: [text](target) / ![alt](target).
// An optional quoted title after the target is ignored.
var linkRe = regexp.MustCompile(`!?\[[^\]]*\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)`)

// htmlRefRe matches src="..." and href="..." / srcset="..." in raw HTML,
// which README.md uses for <picture> and <img>.
var htmlRefRe = regexp.MustCompile(`(?:src|href|srcset)="([^"]+)"`)

// headingRe matches ATX headings.
var headingRe = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*#*\s*$`)

// fenceRe strips fenced code blocks so links inside examples aren't checked.
var fenceRe = regexp.MustCompile("(?ms)^```.*?^```")

// githubSlug reproduces GitHub's heading anchor algorithm closely enough
// for our headings: lowercase, drop everything but letters, digits,
// spaces, hyphens and underscores, spaces to hyphens.
func githubSlug(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}

func anchors(t *testing.T, path string) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	seen := map[string]int{}
	for _, m := range headingRe.FindAllStringSubmatch(fenceRe.ReplaceAllString(string(body), ""), -1) {
		// Strip inline code/emphasis markers and link syntax before slugging.
		text := regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`).ReplaceAllString(m[1], "$1")
		text = strings.NewReplacer("`", "", "*", "").Replace(text)
		s := githubSlug(text)
		if n := seen[s]; n > 0 {
			out[s+"-"+strconv.Itoa(n)] = true
		} else {
			out[s] = true
		}
		seen[s]++
	}
	return out
}

// documents lists every Markdown file whose links we promise are good.
func documents(t *testing.T) []string {
	t.Helper()
	files := []string{"../README.md", "../CONTRIBUTING.md"}
	for _, dir := range []string{"user", "developers", "self-hosting", "screenshots"} {
		err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(p, ".md") {
				files = append(files, p)
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	return files
}

func TestEveryRelativeLinkResolves(t *testing.T) {
	for _, doc := range documents(t) {
		body, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		text := fenceRe.ReplaceAllString(string(body), "")
		var targets []string
		for _, m := range linkRe.FindAllStringSubmatch(text, -1) {
			targets = append(targets, m[1])
		}
		for _, m := range htmlRefRe.FindAllStringSubmatch(text, -1) {
			// srcset may hold "a.png 1x, b.png 2x"; take each URL.
			for _, part := range strings.Split(m[1], ",") {
				targets = append(targets, strings.Fields(part)[0])
			}
		}
		for _, target := range targets {
			checkTarget(t, doc, target)
		}
	}
}

func checkTarget(t *testing.T, doc, target string) {
	t.Helper()
	if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
		return
	}
	if strings.HasPrefix(target, "/") {
		t.Errorf("%s: absolute path link %q; use a relative link", doc, target)
		return
	}
	path, frag, _ := strings.Cut(target, "#")
	inUser := strings.HasPrefix(filepath.ToSlash(doc), "user/")
	if inUser && strings.HasPrefix(path, "../") {
		t.Errorf("%s: %q leaves docs/user; in-app help can't follow it — use an https:// GitHub URL", doc, target)
		return
	}
	resolved := doc
	if path != "" {
		resolved = filepath.Join(filepath.Dir(doc), path)
		if _, err := os.Stat(resolved); err != nil {
			t.Errorf("%s: link %q: %v", doc, target, err)
			return
		}
	}
	if frag != "" && strings.HasSuffix(resolved, ".md") {
		if !anchors(t, resolved)[frag] {
			t.Errorf("%s: link %q: no heading with anchor #%s in %s", doc, target, frag, resolved)
		}
	}
}

func TestUserDocsHaveNoRawHTML(t *testing.T) {
	// goldmark drops raw HTML by default, so it would silently vanish in
	// /help. Guides stay plain Markdown.
	tag := regexp.MustCompile(`<[a-zA-Z/][^>]*>`)
	for _, doc := range documents(t) {
		if !strings.HasPrefix(filepath.ToSlash(doc), "user/") {
			continue
		}
		body, _ := os.ReadFile(doc)
		text := fenceRe.ReplaceAllString(string(body), "")
		text = regexp.MustCompile("`[^`]*`").ReplaceAllString(text, "")
		if m := tag.FindString(text); m != "" {
			t.Errorf("%s contains raw HTML %q", doc, m)
		}
	}
}

func TestGithubSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Sharing a note publicly":    "sharing-a-note-publicly",
		"Keyboard shortcuts":         "keyboard-shortcuts",
		"Import & export (OPML)":     "import--export-opml",
		"What's `#tag` syntax?":      "whats-tag-syntax",
	} {
		if got := githubSlug(strings.ReplaceAll(in, "`", "")); got != want {
			t.Errorf("githubSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 4: Run it, and prove it fires**

Run: `go test ./docs/ -v`
Expected: PASS or a short list of genuinely broken links in today's README/CONTRIBUTING. Fix any broken ones minimally in place (the full rewrite is Task 12) — every task ends green.

Then prove the check fires: temporarily append `[x](nope.md)` and `[y](../README.md#no-such-heading)` to `CONTRIBUTING.md`, run again, expect two FAIL lines naming them, and revert.

- [ ] **Step 5: Full check and commit**

```bash
go build ./... && go vet ./... && go test ./... -race
git add docs/doc.go docs/links_test.go README.md
git commit -m "test(docs): check every documentation link, image and anchor (#308)"
```

---

### Task 2: Move self-hosting and releasing docs; update references

**Files:**
- Move: `docs/DEPLOYING.md` → `docs/self-hosting/deploying.md`
- Move: `docs/onsuite.service` → `docs/self-hosting/onsuite.service`
- Move: `docs/RELEASING.md` → `docs/developers/releasing.md`
- Modify: `.goreleaser.yaml` (archive `files`, `# See docs/RELEASING.md` comment)
- Modify: `AGENTS.md` (docs list near line 198), `CONTRIBUTING.md` (RELEASING link), `README.md` (links only — rewrite comes in Task 12)

**Interfaces:**
- Produces: the new paths every later task links to.

- [ ] **Step 1: Move with git**

```bash
mkdir -p docs/self-hosting docs/developers
git mv docs/DEPLOYING.md docs/self-hosting/deploying.md
git mv docs/onsuite.service docs/self-hosting/onsuite.service
git mv docs/RELEASING.md docs/developers/releasing.md
```

- [ ] **Step 2: Find every reference**

```bash
grep -rn "DEPLOYING\|RELEASING\|onsuite.service" --exclude-dir=.git --exclude-dir=superpowers .
```

Update each hit: relative links inside the moved files (`../README.md` → `../../README.md`, `DEPLOYING.md` → `../self-hosting/deploying.md`, `CONTRIBUTING.md#commit-messages` → `../../CONTRIBUTING.md#commit-messages`), `scp docs/onsuite.service` → `scp docs/self-hosting/onsuite.service` in the deploy guide, `.goreleaser.yaml`:

```yaml
    files:
      - README.md
      - LICENSE
      - docs/self-hosting/deploying.md
      - docs/self-hosting/onsuite.service
```

and `# See docs/developers/releasing.md.` Also check `.github/workflows/*.yml` and `Dockerfile*` hits. Leave `docs/superpowers/**` historical plans alone.

In `docs/developers/releasing.md`, the sentence referencing "README.md's 'Of the four apps...'" is stale (v1.0.0+ shipped): replace the pre-1.0 paragraph with the post-1.0 rule — breaking → major, `feat` → minor, `fix` → patch — and update the bump table accordingly. Fold in the README's "Versioning" section content (post-1.0 wording) as a `## Versioning` section at the top.

- [ ] **Step 3: Verify goreleaser config still parses (if goreleaser installed)**

Run: `goreleaser check 2>/dev/null || echo "goreleaser not installed — skipped"`
Expected: `config is valid` or the skip message.

- [ ] **Step 4: Run link test and full check**

Run: `go test ./docs/ -v && go build ./... && go vet ./... && go test ./... -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A docs .goreleaser.yaml AGENTS.md CONTRIBUTING.md README.md
git commit -m "docs: move deploying and releasing guides under self-hosting/ and developers/ (#308)"
```

---

### Task 3: Developer docs and trimmed CONTRIBUTING

**Files:**
- Create: `docs/developers/index.md`, `getting-started.md`, `architecture.md`, `repository-layout.md`, `adding-an-app.md`, `testing.md`
- Modify: `CONTRIBUTING.md`, `AGENTS.md` (docs list)

**Interfaces:**
- Consumes: moved files from Task 2.
- Produces: `docs/developers/testing.md` has a `## Refreshing screenshots` heading (Task 5 links to it as `../developers/testing.md#refreshing-screenshots`).

Source material: current `README.md` sections "Getting started", "Why", "Design at a glance", "Repository layout", "Testing"; `CONTRIBUTING.md`; `AGENTS.md`; the platform design spec. **Verify every claim against the code** — the README's repository layout is stale (missing `reader`, `flash`, `admin`, `jobs`, `jobsadmin`, `usermgmt`, `apptest`). Regenerate the tree from `ls`/`find`, don't copy it.

- [ ] **Step 1: Inventory the facts to document**

```bash
find cmd internal -maxdepth 3 -type d | grep -v testdata | sort
./onsuite help 2>/dev/null || go run ./cmd/onsuite help
go run ./cmd/onsuite serve -h
grep -n "func registeredApps" -A10 cmd/onsuite/main.go
sed -n 1,80p internal/platform/app/app.go
```

- [ ] **Step 2: Write the pages**

Each page starts with a one-line "who this is for" and ends with a "Next:" link. Content:

- `index.md` — "Start here". What ON Suite is in two sentences; the non-negotiable constraints (no CGO, no Node/npm/JS build, capped dependencies list — copy the current list from `go.mod`'s direct requires, forward-only migrations, platform/app import rules, CSP: no inline script/style); a table mapping each page to what it answers; link to CONTRIBUTING for the PR ground rules and commit style; link to the platform design spec and `docs/superpowers/` for historical specs/plans (one sentence: "every feature has a spec and plan there").
- `getting-started.md` — requirements (Go version from `go.mod`), clone, build, `onsuite user add <name> --admin --data-dir ./data` (password prompt, never a flag), `serve`, `curl localhost:8080/healthz`, env vars (`ONSUITE_*`), `onsuite help`, cross-compiling, the full check command, a "your first change" walkthrough (edit a template, rebuild, see it).
- `architecture.md` — the README's "Design at a glance" bullets (verified and updated: jobs registry, admin/usermgmt/jobsadmin packages, `onsuite export`, `DisabledApps`), a request-flow walk-through (`web.Stack` middleware → `LoadUser` → CSRF → router default-deny `Handle` vs `Public` → app handler → `render.Page` into `base.html`), a text diagram of package layering matching `TestLayering` in `internal/arch/arch_test.go`, HTMX usage and the CSP, SQLite pragmas and single connection, sessions, Argon2id.
- `repository-layout.md` — the regenerated tree with one comment per directory, plus "where do I look for…" table (a route, a template, a migration, a CSS rule, an icon).
- `adding-an-app.md` — the `App` interface (`Meta`, `Migrations`, `Mount`), `Meta.Validate` rules, default-deny router and `Public`, migrations namespace (`<id>:0001`), templates/static embedding (look at `internal/apps/paste` as the smallest real example and cite its files), registering in `registeredApps()`, jobs via `RegisterJobs` if applicable, admin stats via `Stats`, export participation, arch tests that will hold you to it. Include a minimal skeleton `app.go` modelled on paste's — copy real signatures from `internal/platform/app/app.go`.
- `testing.md` — the full check; why store tests use a real SQLite file; `internal/apptest` and `internal/htmlassert` helpers with a short example from an existing test; the arch test; `docs/links_test.go`; fixture password hashing (`apptest.PasswordHash`) and why it's fast; `## Refreshing screenshots` (filled in Task 5 — write the heading and a one-line pointer to `docs/screenshots/README.md` now).

- [ ] **Step 3: Trim CONTRIBUTING.md**

Keep: the opening paragraph (open an issue first for big changes), "Before you dive in" constraints (short — link to `docs/developers/index.md` for detail), "Commit messages", PR expectations. Replace "Building and testing" with a link to `docs/developers/getting-started.md` and `testing.md`. Update the design-spec link text. Update AGENTS.md's docs list to the new structure (user guides, self-hosting, developers).

- [ ] **Step 4: Link test + full check**

Run: `go test ./docs/ -v && go build ./... && go vet ./... && go test ./... -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add docs/developers CONTRIBUTING.md AGENTS.md
git commit -m "docs: developer guide under docs/developers (#308)"
```

---

### Task 4: Demo data seed

**Files:**
- Create: `docs/screenshots/seed/main.go`, `docs/screenshots/seed/seed.go`, `docs/screenshots/seed/seed_test.go`
- Create: `docs/screenshots/seed/fixtures/notes.md`, `fixtures/feeds/*.xml` (4 feeds), `fixtures/pastes/*` (4 snippets)

**Interfaces:**
- Produces: `go run ./docs/screenshots/seed --data-dir DIR` also writes `DIR/share-paste` and `DIR/share-notes` (the URL paths of the seeded public share pages, e.g. `/p/abc123` — take the real path format from the paste/notes share handlers) so capture can find them; and creates `DIR/onsuite.db` (use `config.Config{DataDir: DIR}.DBPath()` — check the real field name in `internal/platform/config/config.go`) and writes `DIR/demo-session` containing a live session id for user `demo`. Function `Seed(ctx context.Context, dataDir string, now time.Time) (sessionID string, err error)` in `seed.go`.
- Demo accounts: `demo` (admin) and `sam` (regular). Their password is `demo-password-not-secret`, written in `docs/screenshots/README.md` (Task 5) — it's a local demo value, never a real credential.

- [ ] **Step 1: Write the failing test**

`docs/screenshots/seed/seed_test.go`:

```go
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/apps/paste"
	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

func TestSeedFillsEveryApp(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	now := time.Date(2026, 9, 28, 9, 30, 0, 0, time.UTC)

	sid, err := Seed(ctx, dir, now)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if sid == "" {
		t.Fatal("no session id")
	}
	if b, err := os.ReadFile(filepath.Join(dir, "demo-session")); err != nil || string(b) != sid {
		t.Fatalf("demo-session = %q, %v; want %q", b, err, sid)
	}
	for _, f := range []string{"share-paste", "share-notes"} {
		if b, err := os.ReadFile(filepath.Join(dir, f)); err != nil || !strings.HasPrefix(string(b), "/") {
			t.Errorf("%s = %q, %v; want a URL path", f, b, err)
		}
	}

	handle, err := db.Open(dbPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()

	users := auth.NewStore(handle)
	demo, err := users.UserByUsername(ctx, "demo")
	if err != nil || !demo.IsAdmin {
		t.Fatalf("demo user = %+v, %v; want an admin", demo, err)
	}
	if _, err := users.UseSession(ctx, sid); err != nil {
		t.Fatalf("session not live: %v", err)
	}

	if s, _ := paste.NewStore(handle).List(ctx, demo.ID, 100); len(s) < 4 {
		t.Errorf("pastes = %d, want >= 4", len(s))
	}
	if top, _ := notes.NewStore(handle).Children(ctx, demo.ID, notes.RootID); len(top) < 3 {
		t.Errorf("top-level notes = %d, want >= 3", len(top))
	}
	tree, err := reader.NewStore(handle).Tree(ctx, demo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Folders) < 2 {
		t.Errorf("reader folders = %d, want >= 2", len(tree.Folders))
	}
	if decks, _ := flash.NewStore(handle).ListDecks(ctx, demo.ID); len(decks) < 3 {
		t.Errorf("decks = %d, want >= 3", len(decks))
	}
}

func TestSeedRefusesANonEmptyDataDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := Seed(context.Background(), dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := Seed(context.Background(), dir, time.Now()); err == nil {
		t.Fatal("second Seed into the same dir succeeded; want an error")
	}
}
```

Check the real names before running: `auth.Store`'s lookup-by-username method (`grep -n "^func (s \*Store)" internal/platform/auth/store.go`) and `reader.Tree`'s folder field (`grep -n "type Tree" -A8 internal/apps/reader/store.go`). Adjust the test to the real names — don't add methods to production code for the seed.

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./docs/screenshots/seed/`
Expected: FAIL — `undefined: Seed`, `undefined: dbPath`.

- [ ] **Step 3: Write fixtures**

- `fixtures/notes.md` — ParseMarkdown format (`- ` bullets, two-space indent, ` [x]` done suffix, trailing ` @YYYY-MM-DD` due; `#tags` inline). About 40 bullets under 4 top-level nodes: "Home renovation" (with due dates and done items), "Reading list #books" (sci-fi titles), "Trip to Japan" (nested itinerary, a note paragraph), "Work" (#meeting tags, one archived-worthy item). Due dates should be relative to 2026-09-28 (some overdue, some this week) — but since `Seed` takes `now`, write them as placeholders `@DUE+3` and have `Seed` substitute `now.AddDate(0,0,3).Format("2006-01-02")` before `ParseMarkdown`. Regex: `@DUE([+-]\d+)`.
- `fixtures/feeds/` — four small RSS 2.0/Atom files (6–10 items each, plain HTML summaries, no remote images): `tech.xml` "The Byte Log", `space.xml` "Orbital Notes", `racing.xml` "Pit Wall Weekly", `books.xml` "Paperback Galaxy". Item `pubDate`s as `DAY-N` placeholders substituted relative to `now` (so articles look fresh). Use `<link>` values under `https://example.com/...`.
- `fixtures/pastes/` — `retry.go` (a Go retry helper), `deploy.sh`, `schema.sql`, `notes.txt`; titles come from a small table in `seed.go`.

- [ ] **Step 4: Implement `seed.go` and `main.go`**

`docs/screenshots/seed/seed.go`:

```go
// Command seed fills a fresh ON Suite data directory with demo content, so
// the documentation screenshots can be regenerated identically. It is local
// tooling, never part of the binary. See docs/screenshots/README.md.
package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/apps/paste"
	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

//go:embed fixtures
var fixtures embed.FS

// Password is the demo accounts' password. It is a published local demo
// value, never a real credential.
const Password = "demo-password-not-secret"

func dbPath(dataDir string) string { return filepath.Join(dataDir, "onsuite.db") }

var dueRe = regexp.MustCompile(`@DUE([+-]\d+)`)
var dayRe = regexp.MustCompile(`DAY-(\d+)`)

// Seed creates the demo accounts and content and returns a live session id
// for "demo". It refuses a data dir that already holds a database.
func Seed(ctx context.Context, dataDir string, now time.Time) (string, error) {
	if _, err := os.Stat(dbPath(dataDir)); err == nil {
		return "", errors.New("seed: data dir already has a database; use an empty one")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", err
	}
	handle, err := db.Open(dbPath(dataDir))
	if err != nil {
		return "", err
	}
	defer handle.Close()

	reg, err := app.NewRegistry(flash.New(), notes.New(), paste.New(), reader.New())
	if err != nil {
		return "", err
	}
	ms, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		return "", err
	}
	appMs, err := reg.Migrations()
	if err != nil {
		return "", err
	}
	if _, err := db.Apply(ctx, handle, append(ms, appMs...)); err != nil {
		return "", err
	}

	users := auth.NewStore(handle)
	hash, err := auth.HashPassword(Password)
	if err != nil {
		return "", err
	}
	demo, err := users.CreateUser(ctx, "demo", hash, true)
	if err != nil {
		return "", err
	}
	if _, err := users.CreateUser(ctx, "sam", hash, false); err != nil {
		return "", err
	}

	steps := []func() error{
		func() error { return seedPastes(ctx, paste.NewStore(handle), demo.ID, dataDir) },
		func() error { return seedNotes(ctx, notes.NewStore(handle), demo.ID, now, dataDir) },
		func() error { return seedReader(ctx, reader.NewStore(handle), demo.ID, now) },
		func() error { return seedFlash(ctx, flash.NewStore(handle), demo.ID, now) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return "", err
		}
	}

	sess, err := users.CreateSession(ctx, demo.ID)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dataDir, "demo-session"), []byte(sess.ID), 0o600); err != nil {
		return "", err
	}
	return sess.ID, nil
}

func seedPastes(ctx context.Context, st *paste.Store, userID int64, dataDir string) error {
	for _, p := range []struct{ file, title, lang string }{
		{"schema.sql", "Reading-list schema", "sql"},
		{"deploy.sh", "Deploy to the Pi", "bash"},
		{"notes.txt", "Wi-Fi and router notes", "text"},
		{"retry.go", "Retry with backoff", "go"},
	} {
		body, err := fixtures.ReadFile("fixtures/pastes/" + p.file)
		if err != nil {
			return err
		}
		s, err := st.Create(ctx, userID, p.title, p.lang, string(body))
		if err != nil {
			return fmt.Errorf("paste %s: %w", p.file, err)
		}
		if p.file == "retry.go" {
			slug, err := st.Share(ctx, userID, s.ID)
			if err != nil {
				return err
			}
			// Path format: check paste's public share route
			// (grep -n "Public" internal/apps/paste/*.go).
			if err := os.WriteFile(filepath.Join(dataDir, "share-paste"), []byte("/paste/s/"+slug), 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func seedNotes(ctx context.Context, st *notes.Store, userID int64, now time.Time, dataDir string) error {
	raw, err := fixtures.ReadFile("fixtures/notes.md")
	if err != nil {
		return err
	}
	text := dueRe.ReplaceAllStringFunc(string(raw), func(m string) string {
		n, _ := strconv.Atoi(dueRe.FindStringSubmatch(m)[1])
		return "@" + now.AddDate(0, 0, n).Format("2006-01-02")
	})
	parsed, err := notes.ParseMarkdown(text)
	if err != nil {
		return fmt.Errorf("notes fixture: %w", err)
	}
	if _, err := st.ImportUnder(ctx, userID, notes.RootID, parsed); err != nil {
		return err
	}
	// Share the "Trip to Japan" subtree for the public-share screenshot.
	top, err := st.Children(ctx, userID, notes.RootID)
	if err != nil {
		return err
	}
	for _, n := range top {
		if n.Title == "Trip to Japan" {
			slug, err := st.Share(ctx, userID, n.ID)
			if err != nil {
				return err
			}
			// Path format: check notes' public share route.
			return os.WriteFile(filepath.Join(dataDir, "share-notes"), []byte("/notes/s/"+slug), 0o600)
		}
	}
	return errors.New("seed: notes fixture has no top-level \"Trip to Japan\"")
}

func seedReader(ctx context.Context, st *reader.Store, userID int64, now time.Time) error {
	folders := map[string]string{
		"tech.xml": "Tech", "space.xml": "Science",
		"racing.xml": "Hobbies", "books.xml": "Hobbies",
	}
	ids := map[string]int64{}
	for _, name := range []string{"Tech", "Science", "Hobbies"} {
		f, err := st.CreateFolder(ctx, userID, name)
		if err != nil {
			return err
		}
		ids[name] = f.ID
	}
	for file, folder := range folders {
		raw, err := fixtures.ReadFile("fixtures/feeds/" + file)
		if err != nil {
			return err
		}
		body := dayRe.ReplaceAllStringFunc(string(raw), func(m string) string {
			n, _ := strconv.Atoi(dayRe.FindStringSubmatch(m)[1])
			return now.Add(-time.Duration(n) * 6 * time.Hour).Format(time.RFC1123Z)
		})
		feedURL := "https://example.com/feeds/" + file
		parsed, err := reader.ParseFeed([]byte(body), feedURL)
		if err != nil {
			return fmt.Errorf("feed %s: %w", file, err)
		}
		folderID := ids[folder]
		sub, err := st.Subscribe(ctx, userID, feedURL, &folderID)
		if err != nil {
			return err
		}
		// A year until the next fetch: the demo server must never poll
		// example.com and replace the fixtures.
		if err := st.SaveFetchResult(ctx, reader.FetchResult{
			FeedID: sub.FeedID, ResolvedURL: feedURL, Title: parsed.Title,
			SiteURL: parsed.SiteURL, Status: 200, FetchedAt: now,
			NextFetchAt: now.AddDate(1, 0, 0),
		}); err != nil {
			return err
		}
		if _, err := st.SaveItems(ctx, sub.FeedID, parsed.Items, now); err != nil {
			return err
		}
	}
	// Some history: read a few, star one, so counts and stats look lived-in.
	items, err := st.ItemsForScope(ctx, userID, reader.ScopeAll, 0, reader.ParseFilter("all"), "", 100)
	if err != nil {
		return err
	}
	for i, it := range items {
		if i%3 == 0 {
			if err := st.SetRead(ctx, userID, it.ID, true, now); err != nil {
				return err
			}
		}
		if i == 1 {
			if err := st.SetStarred(ctx, userID, it.ID, true, now); err != nil {
				return err
			}
		}
	}
	_, err = st.BackfillDailyStats(ctx)
	return err
}

func seedFlash(ctx context.Context, st *flash.Store, userID int64, now time.Time) error {
	decks := []struct {
		name, desc, color string
		cards             [][3]string // type, front, back
		tags              []string
	}{
		{"Japanese travel phrases", "Enough to order ramen and find the station", "coral", [][3]string{
			{"basic", "Excuse me", "すみません (sumimasen)"},
			{"basic", "Where is the station?", "駅はどこですか (eki wa doko desu ka)"},
			{"basic", "Thank you", "ありがとう (arigatou)"},
			{"basic", "The bill, please", "お会計お願いします (okaikei onegaishimasu)"},
			{"cloze", "{{c1::Konnichiwa}} means hello", ""},
		}, []string{"japanese", "travel"}},
		{"F1 circuits", "Which track is where", "teal", [][3]string{
			{"basic", "Albert Park", "Melbourne, Australia"},
			{"basic", "Suzuka", "Suzuka, Japan"},
			{"basic", "Interlagos", "São Paulo, Brazil"},
			{"basic", "Spa-Francorchamps", "Stavelot, Belgium"},
		}, []string{"f1"}},
		{"Go standard library", "Which package does what", "blue", [][3]string{
			{"basic", "Read a whole file", "os.ReadFile"},
			{"basic", "Cancellation and deadlines", "context"},
			{"cloze", "{{c1::errors.Is}} checks a wrapped error chain", ""},
		}, []string{"go", "programming"}},
	}
	for _, d := range decks {
		deck, err := st.CreateDeck(ctx, userID, d.name, d.desc, d.color)
		if err != nil {
			return err
		}
		for i, c := range d.cards {
			card, err := st.SaveCardForm(ctx, userID, deck.ID, 0, flash.CardForm{
				CardType: c[0], Front: c[1], Back: c[2], Tags: d.tags,
			})
			if err != nil {
				return fmt.Errorf("card %q: %w", c[1], err)
			}
			// Review history over the past week for all but the last card,
			// so stats, streaks and "due" counts have something to show.
			if i < len(d.cards)-1 {
				for day := 7; day >= 1; day -= 3 {
					if _, err := st.GradeCard(ctx, userID, card.ID, 3, now.AddDate(0, 0, -day)); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
```

Before running, verify every name used above against the code: `reader.ScopeAll` (`grep -n "Scope[A-Z][a-z]* *Scope\|ScopeAll" internal/apps/reader/*.go`), the rating scale `GradeCard` expects (`grep -n "rating" internal/apps/flash/review.go | head`), cloze front syntax (`internal/apps/flash/cloze.go`), and that `SaveFetchResult` doesn't need extra fields. Fix the seed, not production code.

`docs/screenshots/seed/main.go`:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	dataDir := flag.String("data-dir", "", "empty directory to create the demo database in")
	flag.Parse()
	if *dataDir == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./docs/screenshots/seed --data-dir DIR")
		os.Exit(2)
	}
	if _, err := Seed(context.Background(), *dataDir, time.Now().UTC()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("seeded %s — sign in as demo / %s\n", *dataDir, Password)
}
```

- [ ] **Step 5: Run the test**

Run: `go test ./docs/screenshots/seed/ -v`
Expected: PASS (both tests).

- [ ] **Step 6: Smoke-run against the real server**

```bash
SEED=$TMPDIR/onsuite-demo && rm -rf $SEED && go run ./docs/screenshots/seed --data-dir $SEED
go build -o $SEED/onsuite ./cmd/onsuite
```

Add a `.claude/launch.json` entry `onsuite-docs` (`runtimeExecutable` `$SEED/onsuite` as an absolute path, `serve --addr :8308`, `ONSUITE_DATA_DIR=$SEED`), start it with `preview_start`, sign in as `demo`, and eyeball each app. Adjust fixtures until every app looks realistic and lived-in (no empty states except where a guide shows one on purpose).

- [ ] **Step 7: Full check and commit**

```bash
go build ./... && go vet ./... && go test ./... -race
git add docs/screenshots/seed
git commit -m "docs: demo data seed for reproducible screenshots (#308)"
```

---

### Task 5: Headless Chrome capture tool

**Files:**
- Create: `docs/screenshots/capture/main.go`, `cdp.go`, `cdp_test.go`, `shots.go`
- Create: `docs/screenshots/README.md`
- Modify: `docs/developers/testing.md` (`## Refreshing screenshots`)

**Interfaces:**
- Consumes: `DIR/demo-session` from Task 4.
- Produces: `go run ./docs/screenshots/capture --base http://localhost:8308 --session-file DIR/demo-session [--only name1,name2]` writes PNGs into `docs/user/images/` and `docs/images/`. `shots.go` holds `var shots = []shot{...}`; Tasks 6–12 append to it. Type:

```go
type shot struct {
	Name   string // output path relative to the repo root, e.g. "docs/user/images/notes-outline.png"
	URL    string // path on the demo server, e.g. "/notes/"
	Setup  string // optional JS run after load, before the capture (open a menu, flip a card)
	Theme  string // "light" (default) or "dark"
	Width  int    // viewport, default 1280
	Height int    // viewport, default 800
	Anon   bool   // capture signed out
}
```

- [ ] **Step 1: Write the failing test for the CDP message layer**

The DevTools protocol is JSON-RPC over a websocket. Test the pure part — request encoding and response matching — without Chrome:

`docs/screenshots/capture/cdp_test.go`:

```go
package main

import (
	"encoding/json"
	"testing"
)

func TestResponseMatchesRequestIDAndSkipsEvents(t *testing.T) {
	msgs := []string{
		`{"method":"Page.loadEventFired","params":{}}`,
		`{"id":7,"result":{"data":"aGVsbG8="}}`,
	}
	got, err := awaitResult(7, func() ([]byte, error) {
		m := msgs[0]
		msgs = msgs[1:]
		return []byte(m), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var r struct{ Data string }
	if err := json.Unmarshal(got, &r); err != nil || r.Data != "aGVsbG8=" {
		t.Fatalf("result = %s, %v", got, err)
	}
}

func TestExpandURLSubstitutesSeedFiles(t *testing.T) {
	vars := map[string]string{"share-paste": "/paste/s/abc"}
	if got := expandURL("{{share-paste}}", vars); got != "/paste/s/abc" {
		t.Errorf("expandURL = %q", got)
	}
	if got := expandURL("/notes/", vars); got != "/notes/" {
		t.Errorf("plain URL changed: %q", got)
	}
}

func TestProtocolErrorIsReturned(t *testing.T) {
	_, err := awaitResult(3, func() ([]byte, error) {
		return []byte(`{"id":3,"error":{"code":-32000,"message":"boom"}}`), nil
	})
	if err == nil || err.Error() != "cdp: boom" {
		t.Fatalf("err = %v, want cdp: boom", err)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./docs/screenshots/capture/`
Expected: FAIL — `undefined: awaitResult`, `undefined: expandURL`.

- [ ] **Step 3: Implement `cdp.go`**

```go
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

// browser is one headless Chrome with one page target.
type browser struct {
	cmd    *exec.Cmd
	conn   *websocket.Conn
	nextID int
	tmp    string
}

// chromePath finds Chrome: $CHROME, then the usual macOS and Linux paths.
func chromePath() (string, error) {
	cands := []string{os.Getenv("CHROME"),
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser"}
	for _, c := range cands {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", errors.New("capture: Chrome not found; set $CHROME")
}

func launch() (*browser, error) {
	path, err := chromePath()
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "onsuite-capture-")
	if err != nil {
		return nil, err
	}
	const port = "9333"
	cmd := exec.Command(path, "--headless=new", "--remote-debugging-port="+port,
		"--remote-allow-origins=*", "--user-data-dir="+filepath.Join(tmp, "profile"),
		"--hide-scrollbars", "--force-device-scale-factor=1", "--no-first-run",
		"--no-default-browser-check", "about:blank")
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	b := &browser{cmd: cmd, tmp: tmp}

	// Wait for the DevTools endpoint and find the page target.
	var targets []struct {
		Type, WebSocketDebuggerURL string
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get("http://127.0.0.1:" + port + "/json/list")
		if err == nil {
			err = json.NewDecoder(resp.Body).Decode(&targets)
			resp.Body.Close()
			if err == nil && len(targets) > 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			b.close()
			return nil, fmt.Errorf("capture: Chrome DevTools not reachable: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	for _, t := range targets {
		if t.Type == "page" {
			b.conn, err = websocket.Dial(t.WebSocketDebuggerURL, "", "http://127.0.0.1/")
			if err != nil {
				b.close()
				return nil, err
			}
			return b, nil
		}
	}
	b.close()
	return nil, errors.New("capture: no page target")
}

func (b *browser) close() {
	if b.conn != nil {
		b.conn.Close()
	}
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
		_ = b.cmd.Wait()
	}
	os.RemoveAll(b.tmp)
}

// call sends one CDP command and returns its result, skipping events.
func (b *browser) call(method string, params any) (json.RawMessage, error) {
	b.nextID++
	id := b.nextID
	if err := websocket.JSON.Send(b.conn, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	return awaitResult(id, func() ([]byte, error) {
		var raw []byte
		err := websocket.Message.Receive(b.conn, &raw)
		return raw, err
	})
}

// expandURL replaces {{name}} with vars[name] — the share-* files seed
// writes next to demo-session, whose slugs are random per seed.
func expandURL(u string, vars map[string]string) string {
	for k, v := range vars {
		u = strings.ReplaceAll(u, "{{"+k+"}}", v)
	}
	return u
}

// awaitResult reads messages until the response for id arrives. Events
// (messages with a method and no id) are discarded.
func awaitResult(id int, next func() ([]byte, error)) (json.RawMessage, error) {
	for {
		raw, err := next()
		if err != nil {
			return nil, err
		}
		var m struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		if m.ID != id {
			continue
		}
		if m.Error != nil {
			return nil, errors.New("cdp: " + m.Error.Message)
		}
		return m.Result, nil
	}
}
```

- [ ] **Step 4: Run the test**

Run: `go test ./docs/screenshots/capture/ -v`
Expected: PASS.

- [ ] **Step 5: Implement `main.go` and a starter `shots.go`**

`shots.go`:

```go
package main

// shots is every screenshot the documentation uses. Each guide task adds its
// own; keep them grouped by guide and in page order.
var shots = []shot{
	// docs/user/index.md
	{Name: "docs/user/images/dashboard.png", URL: "/"},
}
```

`main.go`:

```go
// Command capture regenerates the documentation screenshots from a server
// running on seeded demo data. See docs/screenshots/README.md.
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type shot struct {
	Name   string
	URL    string
	Setup  string
	Theme  string
	Width  int
	Height int
	Anon   bool
}

func main() {
	base := flag.String("base", "http://localhost:8308", "demo server base URL")
	sessionFile := flag.String("session-file", "", "file holding the demo session id (written by seed)")
	only := flag.String("only", "", "comma-separated shot names (base names) to capture; empty = all")
	flag.Parse()

	sid, err := os.ReadFile(*sessionFile)
	if err != nil {
		fail(err)
	}
	vars := map[string]string{}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(*sessionFile), "share-*")); matches != nil {
		for _, m := range matches {
			b, err := os.ReadFile(m)
			if err != nil {
				fail(err)
			}
			vars[filepath.Base(m)] = strings.TrimSpace(string(b))
		}
	}
	b, err := launch()
	if err != nil {
		fail(err)
	}
	defer b.close()
	for _, m := range []string{"Page.enable", "Network.enable", "Runtime.enable"} {
		if _, err := b.call(m, map[string]any{}); err != nil {
			fail(err)
		}
	}

	want := map[string]bool{}
	for _, n := range strings.Split(*only, ",") {
		if n != "" {
			want[n] = true
		}
	}
	for _, s := range shots {
		if len(want) > 0 && !want[filepath.Base(s.Name)] {
			continue
		}
		s.URL = expandURL(s.URL, vars)
		if err := capture(b, *base, strings.TrimSpace(string(sid)), s); err != nil {
			fail(fmt.Errorf("%s: %w", s.Name, err))
		}
		fmt.Println("wrote", s.Name)
	}
}

func capture(b *browser, base, sid string, s shot) error {
	if s.Width == 0 {
		s.Width, s.Height = 1280, 800
	}
	if s.Theme == "" {
		s.Theme = "light"
	}
	if _, err := b.call("Network.clearBrowserCookies", map[string]any{}); err != nil {
		return err
	}
	cookies := []map[string]any{{"name": "onsuite_theme", "value": s.Theme, "url": base}}
	if !s.Anon {
		cookies = append(cookies, map[string]any{"name": "onsuite_session", "value": sid, "url": base})
	}
	if _, err := b.call("Network.setCookies", map[string]any{"cookies": cookies}); err != nil {
		return err
	}
	if _, err := b.call("Emulation.setDeviceMetricsOverride", map[string]any{
		"width": s.Width, "height": s.Height, "deviceScaleFactor": 1, "mobile": false,
	}); err != nil {
		return err
	}
	if _, err := b.call("Page.navigate", map[string]any{"url": base + s.URL}); err != nil {
		return err
	}
	if err := waitReady(b); err != nil {
		return err
	}
	if s.Setup != "" {
		if _, err := b.call("Runtime.evaluate", map[string]any{
			"expression": "(async () => {" + s.Setup + "})()", "awaitPromise": true,
		}); err != nil {
			return err
		}
		time.Sleep(400 * time.Millisecond) // let htmx swaps and transitions settle
	}
	res, err := b.call("Page.captureScreenshot", map[string]any{"format": "png"})
	if err != nil {
		return err
	}
	var out struct{ Data string }
	if err := json.Unmarshal(res, &out); err != nil {
		return err
	}
	png, err := base64.StdEncoding.DecodeString(out.Data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Name), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.Name, png, 0o644)
}

// waitReady polls document.readyState, then gives fonts a moment.
func waitReady(b *browser) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		res, err := b.call("Runtime.evaluate", map[string]any{
			"expression": "document.readyState === 'complete' && document.fonts.status === 'loaded'", "returnByValue": true,
		})
		if err != nil {
			return err
		}
		var r struct{ Result struct{ Value bool } }
		if json.Unmarshal(res, &r) == nil && r.Result.Value {
			time.Sleep(300 * time.Millisecond)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("page not ready after 10s")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
```

Run from the repo root so `Name` paths resolve. Seed-dependent URLs use `{{share-paste}}` / `{{share-notes}}`.

- [ ] **Step 6: Try it**

Start the `onsuite-docs` preview (Task 4 Step 6), then:

```bash
go run ./docs/screenshots/capture --session-file $TMPDIR/onsuite-demo/demo-session
```

Expected: `wrote docs/user/images/dashboard.png`. Open it with the Read tool and confirm it shows the signed-in dashboard in the light theme at 1280×800. If it shows the login page, the session cookie didn't stick — check the cookie `url` matches `--base` exactly.

- [ ] **Step 7: Write `docs/screenshots/README.md` and the testing section**

`docs/screenshots/README.md`: what the seed and capture tools are, prerequisites (Go, Google Chrome; `$CHROME` override), the demo accounts (`demo` admin, `sam`) with password `demo-password-not-secret` (a local demo value), and the exact commands:

```bash
SEED=$(mktemp -d)/demo
go run ./docs/screenshots/seed --data-dir $SEED
go build -o $SEED/onsuite ./cmd/onsuite && $SEED/onsuite serve --addr :8308 --data-dir $SEED &
go run ./docs/screenshots/capture --session-file $SEED/demo-session
```

plus how to add a shot (append to `shots.go`, reference `images/<name>.png` from the guide) and `--only`. Fill `docs/developers/testing.md`'s `## Refreshing screenshots` with a two-sentence summary linking to `../screenshots/README.md`.

- [ ] **Step 8: Full check and commit**

Don't commit `dashboard.png` yet — Task 6 commits it with the guide that references it.

```bash
go build ./... && go vet ./... && go test ./... -race
git add docs/screenshots/capture docs/screenshots/README.md docs/developers/testing.md
git commit -m "docs: headless Chrome capture tool for documentation screenshots (#308)"
```

---

### Tasks 6–11: User guides

Each guide task follows the **same procedure**. It's repeated in full per task below with the app-specific inventory, sections and shots.

**Procedure (every guide task):**

1. **Inventory from the code, not memory.** List every route, template and keyboard handler for the app. Every user-visible feature must appear in the guide. Write the inventory as a checklist in your scratchpad and tick items off as you document them.
2. **Try each feature** in the `onsuite-docs` preview as `demo` so the wording matches actual behaviour (button labels verbatim, menu names verbatim).
3. **Write the guide** with this outline:
   - `# <App name>` then a one-paragraph "what it's for".
   - `## Quick tour` — one screenshot of the main screen, then a short list naming its parts.
   - One `##` section per task-shaped feature group (headings are tasks: "Sharing a snippet", not "Share button"). Screenshots where a picture beats a sentence — not every section.
   - `## Keyboard shortcuts` table (key | what it does) where the app has any.
   - A last line: `Back to [all guides](index.md).`
4. **Add shots** for every image you reference to `docs/screenshots/capture/shots.go`, capture them (`--only`), and look at each PNG with the Read tool. Re-shoot if anything is cut off, empty or mid-transition.
5. **Size check:** `du -ch docs/user/images/*.png | tail -1` — the whole `docs/user/images` should stay ≈ 2 MB by the end of Task 11. If a shot is huge, crop it with a smaller `Width`/`Height` or a `Setup` that scrolls to the relevant part.
6. **Verify:** `go test ./docs/ -v` (links, anchors, no raw HTML) and the full check.
7. **Commit** the guide, its images and `shots.go` together.

Style rules for all guides: second person, present tense, short sentences, UI labels in **bold** exactly as shown on screen, keys as `Ctrl`+`Enter`-style inline code, no developer jargon (no "HTMX", "endpoint", "slug"). Mention signing in only in `index.md`. Where a feature is admin-only, say so.

### Task 6: `docs/user/index.md` — Welcome

**Files:** Create `docs/user/index.md`; images `docs/user/images/{login,dashboard,user-menu,account}.png`; modify `docs/screenshots/capture/shots.go`.

- [ ] **Step 1: Inventory**

```bash
cat internal/ui/templates/{login,home,account,base}.html
grep -n "Handle\|mux" internal/platform/web/login.go internal/platform/usermgmt/account.go
grep -n "data-theme-value\|data-font-value\|sidebar-toggle" internal/ui/templates/base.html
```

- [ ] **Step 2: Write** sections: "What ON Suite is" (two sentences + the four apps, each linking to its guide), "Signing in" (accounts are created by an admin — there's no sign-up; link to [admin.md](admin.md)), "The dashboard", "Moving between apps" (sidebar, collapse toggle, breadcrumb), "The user menu" (Account, Display → theme and font, Log out; Help will be added in PR 2), "Your account" (change password; other sessions are signed out), "Getting help" (these guides).

- [ ] **Step 3: Shots** (append to `shots.go`):

```go
	// docs/user/index.md
	{Name: "docs/user/images/login.png", URL: "/login", Anon: true, Width: 1280, Height: 640},
	{Name: "docs/user/images/dashboard.png", URL: "/"},
	{Name: "docs/user/images/user-menu.png", URL: "/", Setup: `document.querySelector('[data-shell-user-menu]').open = true;`},
	{Name: "docs/user/images/account.png", URL: "/account"},
```

(Replace the starter `dashboard.png` entry from Task 5.)

- [ ] **Step 4: Capture, verify, commit** per the procedure.

```bash
git add docs/user/index.md docs/user/images docs/screenshots/capture/shots.go
git commit -m "docs: welcome guide for ON Suite users (#308)"
```

### Task 7: `docs/user/paste.md` — ON Paste

**Files:** Create `docs/user/paste.md`; images `docs/user/images/paste-*.png`; modify `shots.go`.

- [ ] **Step 1: Inventory**

```bash
grep -n "Handle\|Public" internal/apps/paste/*.go | grep -v _test
ls internal/apps/paste/templates && cat internal/apps/paste/templates/*.html | grep -n "button\|<a \|hx-\|label"
grep -n "Languages\|func languages" -A20 internal/apps/paste/highlight.go | head -40
```

- [ ] **Step 2: Write** sections (adjust to the inventory): "Creating a snippet" (title, language, body), "Finding your snippets" (the list, its two-line rows and pills), "Editing and deleting", "Syntax highlighting and languages", "Sharing a snippet" (share link, what anonymous viewers see, unsharing revokes), "Copying and downloading" (if present — generic filenames), "Exporting everything" (if paste participates in export UI; otherwise mention `onsuite export` is an admin task and link to admin.md).

- [ ] **Step 3: Shots** — at least `paste-list.png` (`/paste/` with a snippet selected — use its detail URL), `paste-editor.png` (the new-snippet form), `paste-shared.png` (`URL: "{{share-paste}}", Anon: true`).

- [ ] **Step 4: Capture, verify, commit.**

```bash
git add docs/user/paste.md docs/user/images docs/screenshots
git commit -m "docs: ON Paste user guide (#308)"
```

### Task 8: `docs/user/notes.md` — ON Notes

**Files:** Create `docs/user/notes.md`; images `docs/user/images/notes-*.png`; modify `shots.go`.

- [ ] **Step 1: Inventory**

```bash
grep -n "Handle\|Public" internal/apps/notes/*.go | grep -v _test
ls internal/apps/notes/templates internal/apps/notes/static
grep -n "key ===\|e.key\|case '" internal/apps/notes/static/*.js | head -80
grep -n "func render\|#\|\*\*" internal/apps/notes/markdown.go | head -30
```

Features to cover at minimum (from the README and memory of N1–N10, verify each): one infinite tree; add/edit bullets; notes under a bullet; indent/outdent/move up/down; drag-to-move with the mouse; collapse/expand; zoom into a bullet and breadcrumbs; inline Markdown (bold, italic, links, `#tags` — clicking a tag filters); done/undone and show/hide completed; due dates and the cross-tree due view; search-as-filter with ancestor breadcrumbs; archiving and the archive view; copy/export a single note (#392); Markdown/JSON export and import (paste an outline too); public share links and unsharing; toolbar and settings.

- [ ] **Step 2: Write** — task-shaped sections covering all of the above, and a full `## Keyboard shortcuts` table generated from the JS key handlers (every binding, verbatim).

- [ ] **Step 3: Shots** — `notes-outline.png`, `notes-zoomed.png` (a zoomed bullet URL), `notes-due.png`, `notes-search.png` (Setup: type into the search box and dispatch `input`), `notes-share.png` (`URL: "{{share-notes}}", Anon: true`), plus any that genuinely help (e.g. the export menu open).

- [ ] **Step 4: Capture, verify, commit.**

```bash
git add docs/user/notes.md docs/user/images docs/screenshots
git commit -m "docs: ON Notes user guide (#308)"
```

### Task 9: `docs/user/reader.md` — ON Reader

**Files:** Create `docs/user/reader.md`; images `docs/user/images/reader-*.png`; modify `shots.go`.

- [ ] **Step 1: Inventory**

```bash
grep -n "Handle\|Public" internal/apps/reader/*.go | grep -v _test
ls internal/apps/reader/templates internal/apps/reader/static
grep -n "key ===\|e.key\|case '" internal/apps/reader/static/*.js | head -80
```

Cover at minimum (verify each): subscribing (paste a site or feed URL; discovery), fetch on add, folders and moving feeds, renaming a feed, per-feed refresh, unsubscribing, the three panes and resizing them, unread/starred/all filters, reading an article and "full article" extraction, images through the proxy (one sentence, user-facing: "images load through ON Suite"), starring, mark read/unread, mark all read and the "older than a day/week" split button, search, keyboard navigation, OPML import/export, reading stats page, how often feeds refresh and what happens to old articles (purge window, starred articles kept — check `poll.go`/`PurgeItems` caller for the real retention).

- [ ] **Step 2: Write** with a `## Keyboard shortcuts` table from the JS handlers.

- [ ] **Step 3: Shots** — `reader-three-pane.png` (an article open), `reader-add-feed.png`, `reader-mark-read.png` (split button menu open), `reader-stats.png`. Stats needs history; if the chart is empty, extend `seedReader` to record past-day stats (call `RecordDailyStats` for several past days after marking items read with past timestamps).

- [ ] **Step 4: Capture, verify, commit.**

```bash
git add docs/user/reader.md docs/user/images docs/screenshots
git commit -m "docs: ON Reader user guide (#308)"
```

### Task 10: `docs/user/flash.md` — ON Flash

**Files:** Create `docs/user/flash.md`; images `docs/user/images/flash-*.png`; modify `shots.go`.

- [ ] **Step 1: Inventory**

```bash
grep -n "Handle\|Public" internal/apps/flash/*.go | grep -v _test
ls internal/apps/flash/templates internal/apps/flash/static
grep -n "key ===\|e.key\|case '" internal/apps/flash/static/*.js | head -80
sed -n 1,60p internal/apps/flash/import_prompt.txt
```

Cover at minimum (verify each): decks (create, colours, description, daily new/review limits), cards (basic and cloze, notes, tags, images and audio), the cards grid and flipping, reviewing (the four grades and what they mean in plain words, undo last grade), how scheduling works in one friendly paragraph (spaced repetition; no algorithm names needed beyond "FSRS" once), "Take a break" (snooze a deck), tags and the cross-deck tag filter, gifting/sharing decks with another user (offer, preview, accept/decline, revoke), importing cards (Markdown format and the AI-prompt helper — show the prompt's purpose, not its full text), stats (streak, retention, mastered, due today, daily chart).

- [ ] **Step 2: Write** with a `## Keyboard shortcuts` table (review screen keys especially).

- [ ] **Step 3: Shots** — `flash-home.png`, `flash-cards.png` (cards grid, one flipped via `Setup`), `flash-review.png` (answer revealed), `flash-share.png` (Share popover open), `flash-import.png`, `flash-stats.png`. For the gift flow, seed a pending share from `sam` to `demo` if the guide shows the receiving side (`ShareDeck` from sam's deck — add a small deck for sam in `seedFlash`).

- [ ] **Step 4: Capture, verify, commit.**

```bash
git add docs/user/flash.md docs/user/images docs/screenshots
git commit -m "docs: ON Flash user guide (#308)"
```

### Task 11: `docs/user/admin.md` — Administration

**Files:** Create `docs/user/admin.md`; images `docs/user/images/admin-*.png`; modify `shots.go`.

- [ ] **Step 1: Inventory**

```bash
cat internal/ui/templates/admin*.html | grep -n "<h\|button\|label"
grep -n "Handle" internal/platform/usermgmt/*.go internal/platform/jobsadmin/*.go cmd/onsuite/stack.go | grep -v _test
go run ./cmd/onsuite help
```

Cover: who is an admin; the admin page (facts, per-app cards, what the numbers mean); managing users (add with an initial password, make/remove admin, reset password, delete — and what deleting removes); the jobs page (each job in plain words, Run now, why a job might be disabled); command-line tasks an admin may need (`onsuite user add`, password reset command, `onsuite backup`, `onsuite export`) — each in one or two lines with a link to the self-hosting guide as an absolute GitHub URL (`https://github.com/iliafrenkel/on-suite/blob/main/docs/self-hosting/deploying.md`), since user docs can't use `../` links.

- [ ] **Step 2: Write.**
- [ ] **Step 3: Shots** — `admin-overview.png`, `admin-users.png`, `admin-jobs.png`.
- [ ] **Step 4: Capture, verify (including total image size), commit.**

```bash
du -ch docs/user/images/*.png | tail -1
git add docs/user/admin.md docs/user/images docs/screenshots
git commit -m "docs: administration guide (#308)"
```

---

### Task 12: README rewrite

**Files:**
- Modify: `README.md`
- Create: `docs/images/hero-light.png`, `hero-dark.png`, `app-{paste,notes,reader,flash}-{light,dark}.png`
- Delete: `docs/images/dashboard.png`, `docs/images/paste-snippet.png` (after confirming nothing else references them: `grep -rn "docs/images/" --exclude-dir=.git .`)
- Modify: `docs/screenshots/capture/shots.go`

- [ ] **Step 1: Shots** — hero: the dashboard or Notes outline (pick whichever reads best at a glance) at 1280×800 light and dark; per-app thumbnails at 1280×800 light and dark (they're displayed at ~49% width). Add them all to `shots.go` under `// README.md`.

- [ ] **Step 2: Write the README** following the spec's "README shape":

```markdown
# ON Suite

<one-line pitch: a small, self-hosted set of everyday apps for you and the people you trust — one binary, one SQLite file.>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/images/hero-dark.png">
    <img src="docs/images/hero-light.png" alt="ON Suite ..." width="100%">
  </picture>
</p>

## Why ON Suite
<draft from today's "Why" + "household, not a company" paragraphs: what it is (a household suite, invite-only, one binary + one data dir, runs on a Pi), what it isn't (not SaaS, not multi-tenant, not a pile of containers, no public sign-up). 2–3 short paragraphs.>

## The apps
### ON Paste
<1–2 sentences> → [Guide](docs/user/paste.md)
<picture> thumbnail
... (Notes, Reader, Flash)

## Run it
<download a release binary or the Docker image, `onsuite user add`, `onsuite serve` — 3 commands max> → [Self-hosting guide](docs/self-hosting/deploying.md)

## Documentation
| | |
|---|---|
| [User guides](docs/user/index.md) | Using each app — also built into ON Suite under **Help** |
| [Self-hosting](docs/self-hosting/deploying.md) | Installing, TLS, backups, upgrades, Docker |
| [Developers](docs/developers/index.md) | Building, architecture, adding an app, testing, releasing |
| [Contributing](CONTRIBUTING.md) | Ground rules for issues and pull requests |

## License
MIT — see [LICENSE](LICENSE).
```

Two per-app thumbnails side by side (`width="49%"`) is fine if it reads better than one per section. Mark the "Help" mention in the Documentation table as true only after PR 2; for PR 1 write "User guides | Using each app" and PR 2's Task 17 adds "— also built into ON Suite under **Help**".

Check the release asset names for the "Run it" commands against `.goreleaser.yaml` (`name_template`) and the docker image name there.

- [ ] **Step 3: Verify** — `go test ./docs/ -v` (README `<picture>` srcset paths are checked by `htmlRefRe`), full check. Preview the README rendering: `gh markdown-preview` isn't standard — instead open `README.md` via the Read tool and re-read it top to bottom once for tone and flow.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/images docs/screenshots/capture/shots.go
git commit -m "docs: rewrite README for users with per-app summaries (#308)"
```

### Task 13: PR 1

- [ ] **Step 1: Final checks**

```bash
go build ./... && go vet ./... && go test ./... -race
grep -rn "TODO\|TBD\|FIXME" docs/user docs/developers docs/self-hosting README.md CONTRIBUTING.md
du -ch docs/user/images/*.png docs/images/*.png | tail -1
```

Expected: green; no placeholders; image total reported in the PR description.

- [ ] **Step 2: Push and open the PR** (confirm with Ilia before pushing, per session norms)

```bash
git push -u origin docs/308-user-docs
gh pr create --title "docs: user guides, developer docs and a user-facing README (#308)" --body "Closes #308. ..."
```

The body lists: new structure, moved files, seed + capture tools, image total size, and "In-app help (/help) follows in the #309 PR."

---

# PR 2 — In-app help (#309)

Start after PR 1 merges: `git checkout main && git pull && git checkout -b feat/309-in-app-help`.

### Task 14: Embed package and help page loader

**Files:**
- Create: `docs/embed.go`
- Create: `internal/platform/help/pages.go`, `internal/platform/help/pages_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces:
  - `docs.User() fs.FS` — rooted at `docs/user` (contains `index.md`, `images/`).
  - `help.Load(fsys fs.FS) (*Pages, error)`
  - `type Page struct { Slug, Label, Title string; HTML template.HTML }`
  - `func (p *Pages) Get(slug string) (Page, bool)` — `""` and `"index"` both return the index page.
  - `func (p *Pages) List() []Page` — sidebar order.
  - `func (p *Pages) Images() fs.FS` — rooted at `images/`.
  - `func rewriteLink(dest string) string` (unexported, tested).

- [ ] **Step 1: Add goldmark**

```bash
go get github.com/yuin/goldmark@latest
```

- [ ] **Step 2: Create `docs/embed.go`**

```go
package docs

import (
	"embed"
	"io/fs"
)

//go:embed user
var user embed.FS

// User returns the user guides, rooted at docs/user: index.md, one page per
// app, admin.md and images/. internal/platform/help serves them at /help.
func User() fs.FS {
	sub, err := fs.Sub(user, "user")
	if err != nil {
		panic(err) // "user" is a literal embedded directory; this cannot fail
	}
	return sub
}
```

Update `docs/doc.go`'s comment if needed so there's one package comment.

- [ ] **Step 3: Write the failing tests**

`internal/platform/help/pages_test.go`:

```go
package help

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/iliafrenkel/on-suite/docs"
)

func TestRewriteLink(t *testing.T) {
	for in, want := range map[string]string{
		"notes.md":                 "/help/notes",
		"notes.md#keyboard-shortcuts": "/help/notes#keyboard-shortcuts",
		"index.md":                 "/help",
		"index.md#signing-in":      "/help#signing-in",
		"images/notes-outline.png": "/help/images/notes-outline.png",
		"#quick-tour":              "#quick-tour",
		"https://example.com/a.md": "https://example.com/a.md",
		"/already/absolute":        "/already/absolute",
		"mailto:x@example.com":     "mailto:x@example.com",
		"":                         "",
	} {
		if got := rewriteLink(in); got != want {
			t.Errorf("rewriteLink(%q) = %q, want %q", in, got, want)
		}
	}
}

func minimalFS() fstest.MapFS {
	fsys := fstest.MapFS{"images/a.png": {Data: []byte("\x89PNG")}}
	for _, e := range order {
		fsys[e.Slug+".md"] = &fstest.MapFile{Data: []byte("# " + e.Label + "\n\nSee [notes](notes.md) and ![a](images/a.png).\n")}
	}
	return fsys
}

func TestLoadRendersEveryPageInOrder(t *testing.T) {
	p, err := Load(minimalFS())
	if err != nil {
		t.Fatal(err)
	}
	list := p.List()
	if len(list) != len(order) {
		t.Fatalf("List() has %d pages, want %d", len(list), len(order))
	}
	for i, pg := range list {
		if pg.Slug != order[i].Slug || pg.Label != order[i].Label {
			t.Errorf("page %d = %s/%s, want %s/%s", i, pg.Slug, pg.Label, order[i].Slug, order[i].Label)
		}
		html := string(pg.HTML)
		if !strings.Contains(html, `href="/help/notes"`) || !strings.Contains(html, `src="/help/images/a.png"`) {
			t.Errorf("%s: links not rewritten: %s", pg.Slug, html)
		}
	}
	if idx, ok := p.Get(""); !ok || idx.Slug != "index" {
		t.Errorf(`Get("") = %v, %v; want index`, idx.Slug, ok)
	}
	if _, ok := p.Get("nope"); ok {
		t.Error(`Get("nope") found a page`)
	}
}

func TestLoadFailsOnAMissingOrUnlistedPage(t *testing.T) {
	missing := minimalFS()
	delete(missing, "flash.md")
	if _, err := Load(missing); err == nil {
		t.Error("Load succeeded with flash.md missing")
	}
	extra := minimalFS()
	extra["secret.md"] = &fstest.MapFile{Data: []byte("# Secret\n")}
	if _, err := Load(extra); err == nil {
		t.Error("Load succeeded with an unlisted page; add it to order or delete it")
	}
}

func TestSanitisingStripsScriptsAndStyles(t *testing.T) {
	fsys := minimalFS()
	fsys["index.md"] = &fstest.MapFile{Data: []byte("# Welcome\n\n[x](javascript:alert(1))\n\n| a | b |\n|:--|--:|\n| 1 | 2 |\n")}
	p, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	html := string(p.List()[0].HTML)
	for _, bad := range []string{"javascript:", "style=", "<script"} {
		if strings.Contains(html, bad) {
			t.Errorf("rendered HTML contains %q: %s", bad, html)
		}
	}
	if !strings.Contains(html, "<table>") || !strings.Contains(html, `align="right"`) {
		t.Errorf("GFM table alignment lost: %s", html)
	}
}

func TestHeadingsGetTheSameAnchorsGitHubUses(t *testing.T) {
	fsys := minimalFS()
	fsys["index.md"] = &fstest.MapFile{Data: []byte("# Welcome\n\n## Sharing a note publicly\n\n## Import & export (OPML)\n")}
	p, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	html := string(p.List()[0].HTML)
	for _, id := range []string{`id="sharing-a-note-publicly"`, `id="import--export-opml"`} {
		if !strings.Contains(html, id) {
			t.Errorf("missing %s in %s", id, html)
		}
	}
}

func TestTheRealGuidesLoad(t *testing.T) {
	p, err := Load(docs.User())
	if err != nil {
		t.Fatalf("Load(docs.User()): %v", err)
	}
	for _, pg := range p.List() {
		if pg.Title == "" || len(pg.HTML) == 0 {
			t.Errorf("%s: empty title or body", pg.Slug)
		}
		if strings.Contains(string(pg.HTML), "raw HTML omitted") {
			t.Errorf("%s: contains raw HTML goldmark dropped", pg.Slug)
		}
	}
}
```

- [ ] **Step 4: Run to confirm failure**

Run: `go test ./internal/platform/help/`
Expected: FAIL — `undefined: rewriteLink`, `Load`, `order`.

- [ ] **Step 5: Implement `pages.go`**

```go
// Package help serves the user guides (docs/user) at /help, rendered from
// Markdown once at startup (#309, spec
// docs/superpowers/specs/2026-09-27-documentation-design.md).
//
// The same Markdown reads correctly on GitHub: relative page links and
// images are rewritten here to their /help URLs. It is the only importer of
// goldmark (see TestGoldmarkIsContained in internal/arch).
package help

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/url"
	"path"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// order is the sidebar, and the complete list of pages: Load fails on a
// page missing from the FS or an .md file missing from here.
var order = []struct{ Slug, Label string }{
	{"index", "Welcome"},
	{"paste", "ON Paste"},
	{"notes", "ON Notes"},
	{"reader", "ON Reader"},
	{"flash", "ON Flash"},
	{"admin", "Administration"},
}

type Page struct {
	Slug, Label, Title string
	HTML               template.HTML
}

type Pages struct {
	list   []Page
	bySlug map[string]Page
	images fs.FS
}

func Load(fsys fs.FS) (*Pages, error) {
	listed := map[string]bool{}
	for _, e := range order {
		listed[e.Slug+".md"] = true
	}
	mds, err := fs.Glob(fsys, "*.md")
	if err != nil {
		return nil, err
	}
	for _, m := range mds {
		if !listed[m] {
			return nil, fmt.Errorf("help: %s is not in the page order", m)
		}
	}

	md := goldmark.New(
		goldmark.WithExtensions(extension.NewTable(
			extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute),
		), extension.Strikethrough, extension.Linkify),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(util.Prioritized(linkRewriter{}, 100)),
		),
	)
	policy := sanitizer()

	p := &Pages{bySlug: map[string]Page{}}
	for _, e := range order {
		src, err := fs.ReadFile(fsys, e.Slug+".md")
		if err != nil {
			return nil, fmt.Errorf("help: %w", err)
		}
		var buf bytes.Buffer
		if err := md.Convert(src, &buf); err != nil {
			return nil, fmt.Errorf("help: render %s: %w", e.Slug, err)
		}
		pg := Page{
			Slug: e.Slug, Label: e.Label, Title: firstHeading(src),
			HTML: template.HTML(policy.SanitizeBytes(buf.Bytes())), // #nosec G203 -- sanitised
		}
		p.list = append(p.list, pg)
		p.bySlug[e.Slug] = pg
	}
	if p.images, err = fs.Sub(fsys, "images"); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Pages) Get(slug string) (Page, bool) {
	if slug == "" {
		slug = "index"
	}
	pg, ok := p.bySlug[slug]
	return pg, ok
}

func (p *Pages) List() []Page  { return p.list }
func (p *Pages) Images() fs.FS { return p.images }

func sanitizer() *bluemonday.Policy {
	pol := bluemonday.UGCPolicy()
	pol.AllowAttrs("id").OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	pol.AllowAttrs("align").Matching(bluemonday.SpaceSeparatedTokens).OnElements("th", "td")
	return pol
}

func firstHeading(src []byte) string {
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

type linkRewriter struct{}

func (linkRewriter) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch l := n.(type) {
		case *ast.Link:
			l.Destination = []byte(rewriteLink(string(l.Destination)))
		case *ast.Image:
			l.Destination = []byte(rewriteLink(string(l.Destination)))
		}
		return ast.WalkContinue, nil
	})
}

// rewriteLink maps a GitHub-relative link to its /help URL. Anything that
// isn't a relative .md page or images/ file is returned unchanged.
func rewriteLink(dest string) string {
	if dest == "" || strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "/") {
		return dest
	}
	u, err := url.Parse(dest)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return dest
	}
	var out string
	switch {
	case strings.HasPrefix(u.Path, "images/"):
		out = "/help/" + path.Clean(u.Path)
	case strings.HasSuffix(u.Path, ".md") && !strings.Contains(u.Path, "/"):
		name := strings.TrimSuffix(u.Path, ".md")
		out = "/help/" + name
		if name == "index" {
			out = "/help"
		}
	default:
		return dest
	}
	if u.Fragment != "" {
		out += "#" + u.Fragment
	}
	return out
}
```

goldmark's auto heading IDs may not match GitHub's for `&` (GitHub: `import--export-opml`). If `TestHeadingsGetTheSameAnchorsGitHubUses` fails, add a custom `parser.IDs` implementation (`parser.WithIDs` via `parser.NewContext(parser.WithIDs(...))` passed to `md.Convert(src, &buf, parser.WithContext(ctx))`) using the same algorithm as `githubSlug` in `docs/links_test.go` — lowercase; keep letters, digits, `-`, `_`; space → `-`; duplicate → `-1`, `-2`. Keep both implementations in sync by testing both with the same cases.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/platform/help/ -v`
Expected: PASS.

- [ ] **Step 7: Full check and commit**

```bash
go mod tidy
go build ./... && go vet ./... && go test ./... -race
git add go.mod go.sum docs/embed.go docs/doc.go internal/platform/help
git commit -m "feat(platform): render the user guides for in-app help (#309)"
```

### Task 15: Architecture rules for `docs` and goldmark

**Files:**
- Modify: `internal/arch/arch_test.go`

- [ ] **Step 1: Stop skipping `docs` in `scan`**

In `scan`, change the skip list from `".git", "docs", "dist"` to `".git", ".claude", "dist", "testdata"` (`.claude/` can hold whole git worktrees of this repo from agent sessions, which the walks would otherwise count as a second copy of every package — skip it in `importersOf` and every other tree walk in this file too) — `docs/superpowers` has no Go files, and `docs` now holds real packages. Run `go test ./internal/arch/` and fix fallout: `docs/screenshots/seed` imports apps (it's a `main` tool — add it to whatever exemption `cmd/onsuite` has, if any rule flags it; check `TestAppsDoNotImportEachOther`/`TestPlatformDoesNotImportApps` only look at `internal/apps` and `internal/platform` prefixes, in which case nothing breaks).

- [ ] **Step 2: Add the failing rules**

```go
// TestDocsIsALeaf: docs only embeds the user guides. If it imports
// anything from the module, documentation has started to depend on code.
func TestDocsIsALeaf(t *testing.T) {
	imports := scan(t)
	if _, ok := imports.prod["docs"]; !ok {
		t.Fatal("docs was not scanned")
	}
	if deps := imports.prod["docs"]; len(deps) != 0 {
		t.Errorf("docs imports %v; it must be a leaf", deps)
	}
}

// TestGoldmarkIsContained: goldmark was accepted for in-app help only
// (#309); a second importer is a new decision, like go-readability's.
func TestGoldmarkIsContained(t *testing.T) {
	importers := importersOf(t, "github.com/yuin/goldmark")
	want := []string{"internal/platform/help/pages.go"}
	if !slices.Equal(importers, want) {
		t.Errorf("goldmark is imported by %v, want only %v", importers, want)
	}
}
```

`importersOf` doesn't exist yet: extract the walk from `TestReadabilityIsContained` into `func importersOf(t *testing.T, libPrefix string) []string` (match `imported == libPrefix || strings.HasPrefix(imported, libPrefix+"/")`, skipping `_test.go` files) and make the readability and FSRS containment tests use it too — the three stay identical in behaviour. Also add `"docs"` and `"internal/platform/help"` to `TestScanSeesTheRealTree`'s expected list.

- [ ] **Step 3: Run**

Run: `go test ./internal/arch/ -v`
Expected: PASS (the rules hold already); temporarily add a goldmark import to a scratch file in another package to watch `TestGoldmarkIsContained` fail, then delete it.

- [ ] **Step 4: Full check and commit**

```bash
go build ./... && go vet ./... && go test ./... -race
git add internal/arch/arch_test.go
git commit -m "test(arch): docs is a leaf and goldmark stays inside help (#309)"
```

### Task 16: `/help` routes and template

**Files:**
- Create: `internal/platform/help/handlers.go`, `internal/platform/help/handlers_test.go`
- Create: `internal/ui/templates/help.html`
- Modify: `internal/ui/static/app.css`, `cmd/onsuite/stack.go`

**Interfaces:**
- Consumes: `help.Load`, `docs.User()`.
- Produces: `help.Deps{Pages *Pages; Render *render.Renderer; Errors *web.Errors; Nav []render.NavItem; Version string}`, `func Routes(mux *http.ServeMux, rec *web.Recorder, d Deps)`. Template data `helpPage{Nav []navEntry; Body template.HTML}`, `navEntry{Href, Label string; Current bool}`.

- [ ] **Step 1: Write the failing tests**

`internal/platform/help/handlers_test.go` — build the real stack like `internal/platform/jobsadmin/fixture_test.go` does (copy its `newServer` shape: real DB, `web.Stack`, one user `ilia` logged in), mounting `help.Routes(mux, nil, help.Deps{Pages: pages, ...})` with `pages, _ := help.Load(docs.User())`. Package `help_test`. Tests:

```go
func TestHelpIsPublic(t *testing.T) {
	s := newServer(t)
	for _, path := range []string{"/help", "/help/notes", "/help/admin"} {
		rec := s.get(t, nil, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s signed out = %d, want 200", path, rec.Code)
		}
	}
}

func TestHelpPageRendersInTheShellWithASidebar(t *testing.T) {
	s := newServer(t)
	d := doc(t, s.get(t, s.user, "/help/notes"))
	d.MustHave(".shell-bar")
	d.MustHave(".help-nav")
	cur := d.MustHave(`.help-nav a[aria-current="page"]`)
	if href, _ := htmlassert.Attr(cur, "href"); href != "/help/notes" {
		t.Errorf("current sidebar link = %q, want /help/notes", href)
	}
	if got := len(d.QueryAll(".help-nav a")); got != 6 {
		t.Errorf("sidebar has %d links, want 6", got)
	}
	d.MustHave(".help-body h1")
}

func TestUnknownHelpPageIsTheNormal404(t *testing.T) {
	s := newServer(t)
	missing := s.get(t, nil, "/no-such-page")
	got := s.get(t, nil, "/help/no-such-page")
	if got.Code != http.StatusNotFound || got.Body.String() != missing.Body.String() {
		t.Errorf("GET /help/no-such-page = %d, want the standard 404", got.Code)
	}
}

func TestHelpImagesServeWithAnImageType(t *testing.T) {
	s := newServer(t)
	rec := s.get(t, nil, "/help/images/dashboard.png")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("image = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, bad := range []string{"/help/images/../index.md", "/help/images/nope.png"} {
		if rec := s.get(t, nil, bad); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", bad, rec.Code)
		}
	}
}
```

Adjust the traversal case to whatever ServeMux does with `..` (it cleans and redirects with 301 — accept a non-200 that isn't serving `index.md`'s bytes: assert `rec.Code != 200 || !strings.Contains(rec.Body.String(), "# Welcome")`).

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/platform/help/`
Expected: FAIL — `undefined: help.Routes`.

- [ ] **Step 3: Implement `handlers.go`**

```go
package help

import (
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

type Deps struct {
	Pages   *Pages
	Render  *render.Renderer
	Errors  *web.Errors
	Nav     []render.NavItem
	Version string
}

// Routes registers /help. Every route is public: the guides hold nothing
// private, and a signed-out visitor is exactly who may need them.
func Routes(mux *http.ServeMux, rec *web.Recorder, d Deps) {
	h := &handlers{d: d}
	rec.Handle(mux, "GET /help", true, http.HandlerFunc(h.page))
	rec.Handle(mux, "GET /help/{slug}", true, http.HandlerFunc(h.page))
	rec.Handle(mux, "GET /help/images/{file}", true, http.HandlerFunc(h.image))
}

type handlers struct{ d Deps }

type navEntry struct {
	Href, Label string
	Current     bool
}

type helpPage struct {
	Nav  []navEntry
	Body template.HTML
}

func href(slug string) string {
	if slug == "index" {
		return "/help"
	}
	return "/help/" + slug
}

func (h *handlers) page(w http.ResponseWriter, r *http.Request) {
	pg, ok := h.d.Pages.Get(r.PathValue("slug"))
	if !ok {
		h.d.Errors.NotFound(w, r)
		return
	}
	data := helpPage{Body: pg.HTML}
	for _, p := range h.d.Pages.List() {
		data.Nav = append(data.Nav, navEntry{Href: href(p.Slug), Label: p.Label, Current: p.Slug == pg.Slug})
	}
	page := app.NewPage(r, "Help", h.d.Nav)
	page.Shell.Version = h.d.Version
	page.Data = data
	if err := h.d.Render.Page(w, http.StatusOK, "help", page); err != nil {
		h.d.Errors.Internal(w, r, err)
	}
}

func (h *handlers) image(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	if !fs.ValidPath(file) || strings.Contains(file, "/") {
		h.d.Errors.NotFound(w, r)
		return
	}
	if _, err := fs.Stat(h.d.Pages.Images(), file); err != nil {
		h.d.Errors.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFileFS(w, r, h.d.Pages.Images(), file)
}
```

Check `rec.Handle` accepts a nil `*web.Recorder` (the jobsadmin tests pass nil — confirm in `internal/platform/web/routes.go`).

- [ ] **Step 4: Template `internal/ui/templates/help.html`**

```html
{{define "content"}}
<div class="help">
	<nav class="help-nav" aria-label="Help pages">
		<ul>
			{{range .Data.Nav}}
			<li><a href="{{.Href}}"{{if .Current}} aria-current="page"{{end}}>{{.Label}}</a></li>
			{{end}}
		</ul>
	</nav>
	<article class="help-body">{{.Data.Body}}</article>
</div>
{{end}}
```

- [ ] **Step 5: CSS in `internal/ui/static/app.css`** (append a `/* in-app help (#309) */` section; use existing tokens only):

```css
/* in-app help (#309) */
.help {
	display: grid;
	grid-template-columns: 12rem minmax(0, var(--measure));
	gap: 2.5rem;
	padding: 2rem 1.5rem 4rem;
	justify-content: center;
}
.help-nav ul { list-style: none; margin: 0; padding: 0; position: sticky; top: 1rem; }
.help-nav a {
	display: block; padding: .35rem .6rem; border-radius: 6px;
	color: var(--c-text-dim); text-decoration: none;
}
.help-nav a:hover { background: var(--c-bg-subtle); color: var(--c-text); }
.help-nav a[aria-current="page"] { background: var(--c-accent-bg); color: var(--c-accent); font-weight: 600; }
.help-body { font-family: var(--font-body); line-height: 1.6; color: var(--c-text); }
.help-body h1, .help-body h2, .help-body h3 { font-family: var(--font-ui); line-height: 1.25; }
.help-body h2 { margin-top: 2.25rem; padding-top: 1rem; border-top: 1px solid var(--c-border); }
.help-body img { max-width: 100%; height: auto; border: 1px solid var(--c-border); border-radius: 8px; }
.help-body code { background: var(--c-bg-inset); padding: .1em .35em; border-radius: 4px; font-size: .9em; }
.help-body pre { background: var(--c-bg-inset); padding: 1rem; border-radius: 8px; overflow-x: auto; }
.help-body pre code { background: none; padding: 0; }
.help-body table { border-collapse: collapse; width: 100%; }
.help-body th, .help-body td { border-bottom: 1px solid var(--c-border); padding: .45rem .6rem; text-align: left; }
.help-body td[align="right"], .help-body th[align="right"] { text-align: right; }
.help-body td[align="center"], .help-body th[align="center"] { text-align: center; }
@media (max-width: 800px) {
	.help { grid-template-columns: minmax(0, 1fr); gap: 1rem; padding: 1rem; }
	.help-nav ul { position: static; display: flex; flex-wrap: wrap; gap: .25rem; }
}
```

Confirm `--font-body`, `--font-ui`, `--measure` exist in `app.css` (they do at the top) and match the look of existing prose (compare with the reader article body styles and reuse them if they're a better fit).

- [ ] **Step 6: Wire into `cmd/onsuite/stack.go`**

Before the `routes.Handle(mux, "GET /{$}", ...)` line:

```go
	// The user guides, public so they work signed out (#309).
	helpPages, err := help.Load(docs.User())
	if err != nil {
		return nil, err
	}
	help.Routes(mux, routes, help.Deps{
		Pages:   helpPages,
		Render:  rend,
		Errors:  errs,
		Nav:     deps.Registry.NavItems(),
		Version: deps.Version,
	})
```

with imports `github.com/iliafrenkel/on-suite/docs` and `github.com/iliafrenkel/on-suite/internal/platform/help`. If `cmd/onsuite` has a routes test listing every public route (`grep -n "Public\|public" cmd/onsuite/*_test.go`), add the three help routes to it.

- [ ] **Step 7: Run tests, then look at it**

Run: `go test ./internal/platform/help/ ./cmd/onsuite/ -v`
Expected: PASS.

Build and start the `onsuite-docs` preview, open `/help/notes` signed out and signed in, check light + dark (`resize_window` colorScheme), and at mobile width. Check `read_console_messages` for CSP violations — there must be none. Screenshot for the PR.

- [ ] **Step 8: Full check and commit**

```bash
go build ./... && go vet ./... && go test ./... -race
git add internal/platform/help internal/ui/templates/help.html internal/ui/static/app.css cmd/onsuite
git commit -m "feat(platform): serve the user guides at /help (#309)"
```

### Task 17: Help item in the user menu

**Files:**
- Modify: `internal/ui/templates/base.html`, `internal/ui/toolbar_icons.go`
- Modify: `internal/platform/render/render_test.go`
- Modify: `docs/user/index.md` (the user menu section), `README.md` (Documentation table: "— also built into ON Suite under **Help**")
- Modify: `docs/screenshots/capture/shots.go` if `user-menu.png` should now show Help (re-capture it)

**Interfaces:**
- Consumes: `render.Shell.ActiveApp` (`"paste"|"notes"|"reader"|"flash"|"admin"|""`).

- [ ] **Step 1: Write the failing test** in `internal/platform/render/render_test.go`:

```go
func TestUserMenuHelpLinkFollowsTheActiveApp(t *testing.T) {
	r := testRenderer(t)
	for active, want := range map[string]string{
		"":      "/help",
		"notes": "/help/notes",
		"flash": "/help/flash",
		"admin": "/help/admin",
	} {
		rec := httptest.NewRecorder()
		err := r.Page(rec, http.StatusOK, "error", render.Page{
			Shell: render.Shell{LoggedIn: true, Username: "ilia", CSRFToken: "tok", ActiveApp: active},
			Data:  map[string]any{"Status": 404, "Title": "Not found", "Message": "x"},
		})
		if err != nil {
			t.Fatal(err)
		}
		doc := htmlassert.Parse(t, rec.Body.String())
		link := doc.MustHave(`.shell-user-menu-panel a[href="` + want + `"]`)
		if got := htmlassert.Text(link); got != "Help" {
			t.Errorf("active %q: Help link text = %q", active, got)
		}
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/platform/render/ -run TestUserMenuHelpLink -v`
Expected: FAIL — no `a[href="/help"]`.

- [ ] **Step 3: Add the icon** to `internal/ui/toolbar_icons.go`'s map (same 24×24 stroke style as its neighbours):

```go
	"help": `<svg class="toolbar-icon" viewBox="0 0 24 24" aria-hidden="true">
		<circle cx="12" cy="12" r="9"/>
		<path d="M9.5 9.5a2.5 2.5 0 1 1 3.5 2.3c-.6.3-1 .9-1 1.6v.6"/>
		<circle cx="12" cy="17" r=".6" fill="currentColor" stroke="none"/>
	</svg>`,
```

Match the attribute conventions of the existing entries (look at `"user"` and `"settings"` — if they set `fill="none" stroke="currentColor"` on paths or rely on CSS, do the same). If `TestToolbarIconForKnownNames` lists names, add `"help"`.

- [ ] **Step 4: Add the menu item** in `base.html`, directly after the Account link:

```html
				<a class="shell-menu-item" href="/help{{with .Shell.ActiveApp}}/{{.}}{{end}}">{{ticon "help"}}<span>Help</span></a>
```

Every value `ActiveApp` takes (`paste`, `notes`, `reader`, `flash`, `admin`) is a help slug, so no mapping is needed. Add a test case guarding that: in `internal/platform/help/pages_test.go`, assert every registered app ID has a page — but `help` can't import apps; instead put the guard in `cmd/onsuite` (`stack_test.go`): for each `registeredApps()` ID plus `"admin"`, `GET /help/<id>` returns 200.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/platform/render/ ./internal/ui/ ./cmd/onsuite/ -v`
Expected: PASS. Then the existing `TestShellPutsUserActionsBehindOneMenu` must still pass unchanged.

- [ ] **Step 6: Update docs and re-shoot the menu**

`docs/user/index.md`: add **Help** to the user-menu section ("opens the guide for the app you're in"). README Documentation table: add "— also built into ON Suite under **Help**". Re-capture `user-menu.png` (`--only user-menu.png`).

- [ ] **Step 7: Verify in the browser** — in the preview, open the menu inside ON Reader, click **Help**, land on `/help/reader`. Screenshot.

- [ ] **Step 8: Full check and commit**

```bash
go build ./... && go vet ./... && go test ./... -race
git add internal/ui internal/platform/render cmd/onsuite docs/user README.md docs/screenshots
git commit -m "feat(platform): Help in the user menu opens the current app's guide (#309)"
```

### Task 18: Docker, dependency lists and PR 2

**Files:**
- Modify: `.dockerignore`
- Modify: `docs/superpowers/specs/2026-08-18-on-suite-platform-design.md` (dependency section), `CONTRIBUTING.md` and `docs/developers/index.md` (dependency list), `docs/developers/architecture.md` (help package, request flow), `docs/developers/repository-layout.md` (`docs/embed.go`, `internal/platform/help`)

- [ ] **Step 1: Fix `.dockerignore`**

Replace the `docs` line with:

```
docs/*
!docs/embed.go
!docs/doc.go
!docs/user
```

- [ ] **Step 2: Verify the Docker build** (if Docker is available locally)

```bash
docker build -t onsuite-help-check . && docker run --rm onsuite-help-check version 2>/dev/null || echo "docker unavailable — rely on CI"
```

Expected: build succeeds. Also check `Dockerfile.release` — goreleaser packages a prebuilt binary, so it needs no change; confirm by reading it.

- [ ] **Step 3: Update dependency lists** — add `github.com/yuin/goldmark` (in-app help only, contained by `TestGoldmarkIsContained`) to the platform spec's dependency section with a dated note referencing #309, and to the lists in CONTRIBUTING / `docs/developers/index.md`. Update architecture and layout pages for `internal/platform/help` and `docs/embed.go`.

- [ ] **Step 4: Final checks**

```bash
go build ./... && go vet ./... && go test ./... -race
go test ./docs/ -v
```

- [ ] **Step 5: Commit, push, PR** (confirm with Ilia before pushing)

```bash
git add .dockerignore docs CONTRIBUTING.md
git commit -m "build: ship docs/user in the Docker context; record goldmark (#309)"
git push -u origin feat/309-in-app-help
gh pr create --title "feat(platform): in-app help at /help (#309)" --body "Closes #309. ..."
```

Body includes screenshots of `/help/notes` light/dark/mobile and the menu item.
