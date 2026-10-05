# ON Later L3 — tags and search: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close #484: tag articles (when saving, from a row's ⋯ menu and
from the reading view's ⋯ menu), filter the list by one tag, and search
every article — title, text, highlights, comments and the note — from a
live search box, with a snippet saying where each result matched.

**Architecture:** Builds on the merged L2 app (`internal/apps/later`). Two
migrations: `0005_tags.sql` (`later_tags`, `later_article_tags`) and
`0006_search.sql` (a regular FTS5 table `later_search` kept in step by
triggers on `later_articles` and `later_highlights`, so every write lands in
the index in the same transaction). The list page is driven by a
`listQuery` (tab, tag, search text) that also builds every link on the
page. The search box is the suite's live filter (Notes, Reader): HTMX
`hx-get` with `hx-replace-url`, swapping a `#later-list` region; with a
query the tabs give way to results from every state, best match first.

**Tech Stack:** Go, SQLite FTS5 (modernc, SQLite 3.53), `html/template`,
HTMX. No new JavaScript.

**Spec:** [2026-10-05-on-later-design.md](../specs/2026-10-05-on-later-design.md)
(sections "Data model" — `later_tags`, `later_article_tags`,
`later_search`; "Saving"; "Reading view" — ⋯ menu; "The list" — tag chips,
search, delete). **Issue:** #484 (closes it). **Previous plan:**
[L2](2026-10-05-on-later-l2-highlights.md).

**Decisions already made (Ilia):**
- Search is a live filter next to the tabs. While the box has a word in
  it, the tabs are hidden and results come from every state, best match
  first, each row with a state pill and the "In a highlight: …" snippet.
  Clearing the box brings back the tab you were on.
- The selected tag narrows everything: the tab's rows, the tab counts, and
  search results.
- Tags can be edited in three places: the save box (and the bookmarklet
  popup), each row's ⋯ menu, and the reading view's ⋯ menu. The reading
  view also shows the article's tags under its title.
- No favourites. #484's scope line mentions them, but the spec dropped
  them on purpose. Say so in the PR and edit the issue's scope line.

**Decisions made in this plan (flag them in the PR):**
- Tag names are cleaned, never refused: trimmed, lowercased, inner
  whitespace collapsed, control characters dropped, a leading `#` dropped,
  cut to 40 characters, de-duplicated, at most 20 per article. A form error
  inside a ⋯ menu would be worse than a shortened private label.
- Saving a URL that is already saved ignores the tags typed with it (the
  existing article is shown, unchanged). This happens rarely.
- `later_search` is a regular FTS5 table, not external-content like
  Reader's: the highlights column is gathered from another table, which
  external content can't express, and `snippet()` needs the text. The
  extra copy of the text costs little for one household.
- Snippet priority: highlight/comment, then note, then article text. A
  title-only match shows no snippet, because the row already shows the
  title.
- Ranking: `bm25` with weights title 10, text 1, highlights 4, note 4.

## Global Constraints

- Everything from L1a/L1b/L2's Global Constraints still binds: `later_`
  prefixes, STRICT tables, `… ON DELETE CASCADE`, `db.FormatTime`/`ParseTime`,
  time only via `Store.now()`, no import of another app, owner scoping (404
  for someone else's), no inline `<script>`/`style=` attributes, CSS in
  app.css's ON Later section with `later-` classes, no new dependencies,
  full check green after every task:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- Tag names are stored only through `ParseTags`/`cleanTags`. They are
  lowercase and unique per user. Unused tags are deleted in the same
  transaction that unlinked them (spec: "Unused tags are removed when their
  last article loses them"), including on article delete.
- `later_search` is changed **only by triggers**. No Go code writes to it.
- FTS input goes through `ftsQuery` (a copy of Reader's — PATTERNS.md
  "Cross-app mirroring"). An empty `ftsQuery` result never reaches `MATCH`.
- Reference `later_search` by its real name in queries, never an alias:
  this driver resolves `MATCH`, `snippet()` and `bm25()` against the real
  name (the same note as Notes' and Reader's search).
- List links are built only by `listQuery.url`, in the fixed parameter
  order `tab`, `tag`, `q`, `offset`. Existing tests pin
  `/later/?tab=unread&offset=50` and `/later/?tab=archived`.
- No `template.HTML` added. Snippet highlighting is rendered from
  `[]snippetPart` with `{{if .Hit}}<mark>`.
- htmlassert selectors take **one** qualifier per compound
  (`a.later-tag-chip` or `a[href=…]`, never `a.x[href=…]`). Use
  `QueryAll` and check attributes when you need both.
- Commits: Conventional Commits, scope `later`.

## File structure (new or substantially changed)

```
internal/apps/later/
  migrations/0005_tags.sql        later_tags, later_article_tags
  migrations/0006_search.sql      later_search (FTS5) + triggers + backfill
  tag.go                          ParseTags, cleanTags, linkTags, gcTags, SetTags, ArticleTags, TagNames
  tag_test.go                     store tests for tags
  search.go                       ftsQuery, MatchIn, SearchHit, Store.Search
  search_test.go                  store tests for search
  snippet.go                      snippetPart, snippetParts (handler-side)
  snippet_internal_test.go        table test for snippetParts (package later)
  list.go                         listQuery, tagChips, newRow, renderListPage (moved out of handlers.go)
  list_test.go                    handler tests for tags on the list and search
  store.go                        NewArticle.Tags, ListItem.State/Tags, listSelect, List/Counts take a tag, Save links tags, Delete GCs tags
  handlers.go                     setTags; save reads tags; articleView tags
  later.go                        POST /a/{id}/tags
  templates/index.html            save-box tags, search box, #later-list, chips, pills, row tag form, snippets
  templates/article.html          tag links under the title, ⋯ tag form
  templates/popup.html            tags field
internal/ui/static/app.css        ON Later section
docs/user/later.md                "Tags" and "Searching"
```

---

### Task 1: The tag store

**Files:**
- Create: `internal/apps/later/migrations/0005_tags.sql`, `internal/apps/later/tag.go`, `internal/apps/later/tag_test.go`
- Modify: `internal/apps/later/store.go` (`NewArticle.Tags`, `Save`, `Delete`)

**Interfaces — Produces:**
```go
const MaxTagRunes = 40
const MaxTags = 20
func ParseTags(raw string) []string            // comma-separated field -> clean names, typed order
func cleanTags(names []string) []string        // ParseTags(strings.Join(names, ","))
func linkTags(ctx context.Context, tx *sql.Tx, userID, articleID int64, names []string) error // names already clean
func gcTags(ctx context.Context, tx *sql.Tx, userID int64) error
func (st *Store) SetTags(ctx context.Context, userID, id int64, names []string) error // ErrNotFound for missing/foreign
func (st *Store) ArticleTags(ctx context.Context, userID, id int64) ([]string, error) // alphabetical
func (st *Store) TagNames(ctx context.Context, userID int64) ([]string, error)        // alphabetical
// NewArticle gains: Tags []string // raw names; Save cleans them
```

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/later/tag_test.go`:

```go
package later_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
)

func TestParseTags(t *testing.T) {
	long := strings.Repeat("x", 50)
	var many []string
	for i := 0; i < 25; i++ {
		many = append(many, string(rune('a'+i)))
	}
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"Essays, AI", []string{"essays", "ai"}},
		{" #go ,  go,GO ", []string{"go"}},
		{"to   cook", []string{"to cook"}},
		{"", nil},
		{" , ,", nil},
		{"##", nil},
		{"a\x1fb", []string{"a b"}},
		{long, []string{strings.Repeat("x", later.MaxTagRunes)}},
		{"Ünïcode, РУССКИЙ, עברית", []string{"ünïcode", "русский", "עברית"}},
		{strings.Join(many, ","), many[:later.MaxTags]},
	} {
		if got := later.ParseTags(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("ParseTags(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// saveTagged stores an article for userID with tags.
func (f *fixture) saveTagged(t *testing.T, userID int64, url string, tags ...string) later.Article {
	t.Helper()
	a, _, err := f.store.Save(context.Background(), userID, later.NewArticle{
		URL: url, Title: url, ContentHTML: "<p>body</p>", Tags: tags,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (f *fixture) articleTags(t *testing.T, userID, id int64) []string {
	t.Helper()
	got, err := f.store.ArticleTags(context.Background(), userID, id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (f *fixture) tagNames(t *testing.T, userID int64) []string {
	t.Helper()
	got, err := f.store.TagNames(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSaveLinksTags(t *testing.T) {
	f := newFixture(t)
	a := f.saveTagged(t, f.alice.ID, "https://a.example/1", "Essays", "ai", "essays")
	if got := f.articleTags(t, f.alice.ID, a.ID); !slices.Equal(got, []string{"ai", "essays"}) {
		t.Errorf("ArticleTags = %q, want [ai essays]", got)
	}
	if got := f.tagNames(t, f.alice.ID); !slices.Equal(got, []string{"ai", "essays"}) {
		t.Errorf("TagNames = %q, want [ai essays]", got)
	}
}

func TestSetTagsReplacesAndRemovesUnusedTags(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a1 := f.saveTagged(t, f.alice.ID, "https://a.example/1", "x", "y")
	f.saveTagged(t, f.alice.ID, "https://a.example/2", "y")

	if err := f.store.SetTags(ctx, f.alice.ID, a1.ID, []string{"Z"}); err != nil {
		t.Fatal(err)
	}
	if got := f.articleTags(t, f.alice.ID, a1.ID); !slices.Equal(got, []string{"z"}) {
		t.Errorf("ArticleTags = %q, want [z]", got)
	}
	// x lost its last article; y is still used by the second one.
	if got := f.tagNames(t, f.alice.ID); !slices.Equal(got, []string{"y", "z"}) {
		t.Errorf("TagNames = %q, want [y z]", got)
	}

	if err := f.store.SetTags(ctx, f.alice.ID, a1.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.articleTags(t, f.alice.ID, a1.ID); len(got) != 0 {
		t.Errorf("ArticleTags after clearing = %q, want none", got)
	}
	if got := f.tagNames(t, f.alice.ID); !slices.Equal(got, []string{"y"}) {
		t.Errorf("TagNames = %q, want [y]", got)
	}
}

func TestTagsArePerUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.saveTagged(t, f.alice.ID, "https://a.example/1", "shared", "alice-only")
	b := f.saveTagged(t, f.bob.ID, "https://a.example/1", "shared")

	if err := f.store.SetTags(ctx, f.bob.ID, a.ID, []string{"hijack"}); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("SetTags on someone else's article = %v, want ErrNotFound", err)
	}
	if got := f.articleTags(t, f.bob.ID, a.ID); len(got) != 0 {
		t.Errorf("ArticleTags for someone else's article = %q, want none", got)
	}
	if err := f.store.SetTags(ctx, f.bob.ID, b.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.tagNames(t, f.alice.ID); !slices.Equal(got, []string{"alice-only", "shared"}) {
		t.Errorf("alice's TagNames = %q, want her two tags untouched", got)
	}
	if got := f.tagNames(t, f.bob.ID); len(got) != 0 {
		t.Errorf("bob's TagNames = %q, want none", got)
	}
}

func TestSetTagsOnAMissingArticleIsNotFound(t *testing.T) {
	f := newFixture(t)
	if err := f.store.SetTags(context.Background(), f.alice.ID, 999, []string{"x"}); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("SetTags = %v, want ErrNotFound", err)
	}
	if got := f.tagNames(t, f.alice.ID); len(got) != 0 {
		t.Errorf("TagNames = %q, want none created", got)
	}
}

func TestDeleteRemovesUnusedTags(t *testing.T) {
	f := newFixture(t)
	a1 := f.saveTagged(t, f.alice.ID, "https://a.example/1", "solo", "shared")
	f.saveTagged(t, f.alice.ID, "https://a.example/2", "shared")
	if err := f.store.Delete(context.Background(), f.alice.ID, a1.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.tagNames(t, f.alice.ID); !slices.Equal(got, []string{"shared"}) {
		t.Errorf("TagNames = %q, want [shared]", got)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/later/ -run 'Tag|Tags' -count=1`
Expected: build failure — `later.ParseTags`, `NewArticle.Tags`, `SetTags`, `ArticleTags`, `TagNames`, `MaxTagRunes`, `MaxTags` undefined.

- [ ] **Step 3: Add the migration**

Create `internal/apps/later/migrations/0005_tags.sql`:

```sql
-- L3: tags. Names are clean (tag.go: ParseTags) and unique per user. A tag
-- with no articles left is deleted by the store in the same transaction
-- that unlinked it (tag.go: gcTags).
CREATE TABLE later_tags (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name    TEXT    NOT NULL,
    UNIQUE (user_id, name)
) STRICT;

CREATE TABLE later_article_tags (
    article_id INTEGER NOT NULL REFERENCES later_articles (id) ON DELETE CASCADE,
    tag_id     INTEGER NOT NULL REFERENCES later_tags (id) ON DELETE CASCADE,
    PRIMARY KEY (article_id, tag_id)
) STRICT, WITHOUT ROWID;

-- The tag filter and gcTags look links up by tag.
CREATE INDEX later_article_tags_tag_idx ON later_article_tags (tag_id);
```

- [ ] **Step 4: Write `tag.go`**

Create `internal/apps/later/tag.go`:

```go
package later

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// MaxTagRunes bounds one tag name. Longer names are cut, not refused: a tag
// is a private label, and a form error inside a ⋯ menu would be worse than
// a shortened one.
const MaxTagRunes = 40

// MaxTags bounds how many tags one article keeps; the rest are dropped.
const MaxTags = 20

// ParseTags turns a comma-separated tags field into clean names, in the
// order typed: control characters become spaces, whitespace collapses,
// leading '#' go, names are lowercased and cut to MaxTagRunes, blanks and
// repeats are skipped, and at most MaxTags are kept.
func ParseTags(raw string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		part = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, part)
		name := strings.ToLower(strings.Join(strings.Fields(part), " "))
		name = strings.TrimSpace(strings.TrimLeft(name, "#"))
		if r := []rune(name); len(r) > MaxTagRunes {
			name = strings.TrimSpace(string(r[:MaxTagRunes]))
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
		if len(out) == MaxTags {
			break
		}
	}
	return out
}

// cleanTags applies ParseTags to names that may not have come through it.
func cleanTags(names []string) []string { return ParseTags(strings.Join(names, ",")) }

// linkTags finds or creates each of userID's tags and links articleID to
// them, inside the caller's transaction. names must already be clean.
func linkTags(ctx context.Context, tx *sql.Tx, userID, articleID int64, names []string) error {
	for _, name := range names {
		var tagID int64
		// DO UPDATE (a no-op) rather than DO NOTHING, so RETURNING gives the
		// id of an existing tag too.
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO later_tags (user_id, name) VALUES (?, ?)
			ON CONFLICT (user_id, name) DO UPDATE SET name = excluded.name
			RETURNING id`, userID, name).Scan(&tagID); err != nil {
			return fmt.Errorf("later: tag %q: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO later_article_tags (article_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			articleID, tagID); err != nil {
			return fmt.Errorf("later: link tag %q: %w", name, err)
		}
	}
	return nil
}

// gcTags deletes userID's tags that no article uses any more (spec: "Unused
// tags are removed when their last article loses them").
func gcTags(ctx context.Context, tx *sql.Tx, userID int64) error {
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM later_tags
		 WHERE user_id = ?
		   AND NOT EXISTS (SELECT 1 FROM later_article_tags WHERE tag_id = later_tags.id)`, userID); err != nil {
		return fmt.Errorf("later: remove unused tags: %w", err)
	}
	return nil
}

// SetTags replaces an article's tags with names (cleaned here).
func (st *Store) SetTags(ctx context.Context, userID, id int64, names []string) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("later: begin set tags: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The owner check runs inside the transaction, as Flash learned (#294):
	// with one connection, a check before it could race a delete.
	res, err := tx.ExecContext(ctx,
		`UPDATE later_articles SET updated_at = ? WHERE id = ? AND user_id = ?`,
		db.FormatTime(st.now()), id, userID)
	if err != nil {
		return fmt.Errorf("later: set tags: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM later_article_tags WHERE article_id = ?`, id); err != nil {
		return fmt.Errorf("later: clear tags: %w", err)
	}
	if err := linkTags(ctx, tx, userID, id, cleanTags(names)); err != nil {
		return err
	}
	if err := gcTags(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// ArticleTags returns the names of an article's tags, alphabetically; none
// for a missing or someone else's article.
func (st *Store) ArticleTags(ctx context.Context, userID, id int64) ([]string, error) {
	return st.names(ctx, `
		SELECT t.name FROM later_article_tags x
		  JOIN later_tags t ON t.id = x.tag_id
		  JOIN later_articles a ON a.id = x.article_id
		 WHERE a.id = ? AND a.user_id = ?
		 ORDER BY t.name`, id, userID)
}

// TagNames returns every tag userID has, alphabetically.
func (st *Store) TagNames(ctx context.Context, userID int64) ([]string, error) {
	return st.names(ctx, `SELECT name FROM later_tags WHERE user_id = ? ORDER BY name`, userID)
}

func (st *Store) names(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := st.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("later: tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("later: scan tag: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("later: tags: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 5: Link tags in `Save`, remove unused ones in `Delete`**

In `internal/apps/later/store.go`, add a field to `NewArticle` (after `FaviconURL`):

```go
	Tags                         []string          // raw names; Save cleans them
```

In `Save`, just before `if err := tx.Commit(); err != nil {` (after the favicon block), add:

```go
	if err := linkTags(ctx, tx, userID, id, cleanTags(n.Tags)); err != nil {
		return Article{}, false, err
	}
```

In `Delete`, just before the final `return tx.Commit()`, add:

```go
	// The article's tag links went with it (ON DELETE CASCADE); its tags go
	// too if nothing else uses them (spec: "Delete").
	if err := gcTags(ctx, tx, userID); err != nil {
		return err
	}
```

Update `Delete`'s doc comment to: `// Delete removes an article, its highlights, its tags that nothing else
// uses, and every image no other article still uses.`

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/apps/later/ -count=1`
Expected: PASS (new and existing).

- [ ] **Step 7: Full check and commit**

Run the full check from Global Constraints. Then:

```bash
git add internal/apps/later/migrations/0005_tags.sql internal/apps/later/tag.go internal/apps/later/tag_test.go internal/apps/later/store.go
git commit -m "feat(later): tag store — tags on save, replace, and clean-up of unused tags"
```

---

### Task 2: The list filtered by tag

**Files:**
- Create: `internal/apps/later/list.go`, `internal/apps/later/list_test.go`
- Modify: `internal/apps/later/store.go` (`ListItem`, `List`, `Counts`), `internal/apps/later/handlers.go` (move list code out), `internal/apps/later/templates/index.html`, `internal/ui/static/app.css`, every `*_test.go` calling `List`/`Counts`

**Interfaces:**
- Consumes (Task 1): `ParseTags`, `TagNames`, `NewArticle.Tags`.
- Produces:
```go
// store.go
type ListItem struct { /* existing fields */ State State; Tags []string /* alphabetical */ }
const listSelect, listJoins, tagFilter string // SQL fragments, reused by Search (Task 4)
func scanListItem(row rowScanner, now time.Time, extra ...any) (ListItem, error)
func (st *Store) List(ctx context.Context, userID int64, state State, tag string, offset, limit int) ([]ListItem, error)
func (st *Store) Counts(ctx context.Context, userID int64, tag string) (map[State]int, error)
// list.go
type listQuery struct { Tab State; Tag string }
func (q listQuery) url(offset int) string
func parseListQuery(v url.Values) listQuery
func tagParam(v string) string
type chipView struct { Name, URL string; Current bool }
func tagChips(q listQuery, names []string) []chipView
func newRow(it ListItem) rowView
// rowView gains State State, Archived bool, Tags []string
// indexView gains Tag string, Chips []chipView, Back string; NextOffset int becomes NextURL string
// tabView gains URL string
```

- [ ] **Step 1: Write the failing store tests**

Append to `internal/apps/later/tag_test.go`:

```go
func TestListFiltersByTag(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.saveTagged(t, f.alice.ID, "https://a.example/1", "x")
	b := f.saveTagged(t, f.alice.ID, "https://a.example/2", "x", "y")
	c := f.saveTagged(t, f.alice.ID, "https://a.example/3")
	f.saveTagged(t, f.bob.ID, "https://a.example/4", "x")

	for _, tc := range []struct {
		tag  string
		want []int64
	}{
		{"x", []int64{b.ID, a.ID}},
		{"y", []int64{b.ID}},
		{"", []int64{c.ID, b.ID, a.ID}},
		{"nope", nil},
	} {
		got, err := f.store.List(ctx, f.alice.ID, later.StateUnread, tc.tag, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		if !sameIDs(got, tc.want...) {
			t.Errorf("List(tag %q) = %v, want %v", tc.tag, ids(got), tc.want)
		}
	}
}

func TestListItemCarriesTagsAndState(t *testing.T) {
	f := newFixture(t)
	f.saveTagged(t, f.alice.ID, "https://a.example/1", "zeta", "Alpha")
	got, err := f.store.List(context.Background(), f.alice.ID, later.StateUnread, "", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d items", len(got))
	}
	if !slices.Equal(got[0].Tags, []string{"alpha", "zeta"}) {
		t.Errorf("Tags = %q, want [alpha zeta]", got[0].Tags)
	}
	if got[0].State != later.StateUnread {
		t.Errorf("State = %q, want unread", got[0].State)
	}
}

func TestCountsByTag(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.saveTagged(t, f.alice.ID, "https://a.example/1", "x")
	b := f.saveTagged(t, f.alice.ID, "https://a.example/2", "x")
	f.saveTagged(t, f.alice.ID, "https://a.example/3")
	f.saveTagged(t, f.bob.ID, "https://a.example/4", "x")
	if err := f.store.SetState(ctx, f.alice.ID, b.ID, later.StateArchived); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.Counts(ctx, f.alice.ID, "x")
	if err != nil {
		t.Fatal(err)
	}
	if got[later.StateUnread] != 1 || got[later.StateReading] != 0 || got[later.StateArchived] != 1 {
		t.Errorf("Counts(x) = %v, want unread 1, reading 0, archived 1", got)
	}
	all, err := f.store.Counts(ctx, f.alice.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if all[later.StateUnread] != 2 || all[later.StateArchived] != 1 {
		t.Errorf("Counts() = %v, want unread 2, archived 1", all)
	}
}
```

- [ ] **Step 2: Update existing call sites to the new signatures**

`List` and `Counts` gain a `tag string` argument. Update every test call
with:

```bash
cd internal/apps/later
perl -pi -e 's/(\.List\([^,]+, [^,]+, [^,]+), (\d+, \d+\))/$1, "", $2/' *_test.go
perl -pi -e 's/\.Counts\(([^,]+), ([^)]+)\)/.Counts($1, $2, "")/' *_test.go
cd -
```

Then `grep -n '\.List(\|\.Counts(' internal/apps/later/*_test.go` and check
each line now has the extra `""`.

- [ ] **Step 3: Run them to see them fail**

Run: `go test ./internal/apps/later/ -count=1`
Expected: build failure — too many arguments to `List`/`Counts`;
`ListItem.Tags`, `ListItem.State` undefined.

- [ ] **Step 4: Rewrite `List` and `Counts` in `store.go`**

Add to `ListItem` (after `Highlights int`):

```go
	State      State
	Tags       []string // alphabetical
```

Replace the whole `List` function with:

```go
// listSelect is every column scanListItem reads: an article a, the favicon
// joins in listJoins, its highlight count and its tags joined by tagSep.
const listSelect = `a.id, a.title, a.site_host, a.content, a.word_count, a.progress, a.state,
	COALESCE(sf.hash, ''), f.bytes IS NOT NULL, COALESCE(f.error_count, 0), f.fetched_at,
	(SELECT count(*) FROM later_highlights h WHERE h.article_id = a.id),
	(SELECT COALESCE(group_concat(t.name, char(31) ORDER BY t.name), '')
	   FROM later_article_tags x JOIN later_tags t ON t.id = x.tag_id
	  WHERE x.article_id = a.id)`

// tagSep is char(31) in listSelect. ParseTags turns control characters into
// spaces, so no name contains it.
const tagSep = "\x1f"

const listJoins = `
	  LEFT JOIN later_site_favicons sf ON sf.site_host = a.site_host
	  LEFT JOIN later_favicons f ON f.hash = sf.hash`

// tagFilter keeps the articles a carrying one tag. It takes the tag name
// twice; "" keeps everything.
const tagFilter = `(? = '' OR EXISTS (
	SELECT 1 FROM later_article_tags x JOIN later_tags t ON t.id = x.tag_id
	 WHERE x.article_id = a.id AND t.name = ?))`

// scanListItem reads one listSelect row, then any extra columns into extra.
func scanListItem(row rowScanner, now time.Time, extra ...any) (ListItem, error) {
	var it ListItem
	var cached bool
	var errorCount int
	var fetched sql.NullString
	var tags string
	dest := append([]any{&it.ID, &it.Title, &it.SiteHost, &it.Content, &it.WordCount, &it.Progress, &it.State,
		&it.FaviconHash, &cached, &errorCount, &fetched, &it.Highlights, &tags}, extra...)
	if err := row.Scan(dest...); err != nil {
		return ListItem{}, fmt.Errorf("later: scan list: %w", err)
	}
	var attempt time.Time
	if fetched.Valid {
		var err error
		if attempt, err = db.ParseTime(fetched.String); err != nil {
			return ListItem{}, err
		}
	}
	it.FaviconShown = it.FaviconHash != "" && (cached || !webfetch.GivenUp(errorCount, attempt, now))
	if tags != "" {
		it.Tags = strings.Split(tags, tagSep)
	}
	return it, nil
}

// List returns one page of a tab, newest activity first, narrowed to the
// articles carrying tag unless tag is "".
func (st *Store) List(ctx context.Context, userID int64, state State, tag string, offset, limit int) ([]ListItem, error) {
	order, ok := listOrder[state]
	if !ok {
		return nil, ErrInvalid
	}
	// order comes only from the listOrder map above, never from input.
	rows, err := st.db.QueryContext(ctx, `
		SELECT `+listSelect+`
		  FROM later_articles a`+listJoins+`
		 WHERE a.user_id = ? AND a.state = ? AND `+tagFilter+`
		 ORDER BY `+order+`
		 LIMIT ? OFFSET ?`, userID, state, tag, tag, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("later: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	now := st.now()
	var items []ListItem
	for rows.Next() {
		it, err := scanListItem(rows, now)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("later: list: %w", err)
	}
	return items, nil
}
```

In `Counts`, change the signature and query:

```go
// Counts is how many articles userID has in each state, narrowed to tag
// unless it is ""; every state is present, 0 when empty.
func (st *Store) Counts(ctx context.Context, userID int64, tag string) (map[State]int, error) {
	counts := map[State]int{StateUnread: 0, StateReading: 0, StateArchived: 0}
	rows, err := st.db.QueryContext(ctx, `
		SELECT a.state, count(*) FROM later_articles a
		 WHERE a.user_id = ? AND `+tagFilter+`
		 GROUP BY a.state`, userID, tag, tag)
```

(the rest of `Counts` is unchanged).

Run: `go test ./internal/apps/later/ -run 'List|Counts' -count=1` — the
store tests pass; the package may still fail to build until Step 7 updates
the handler.

- [ ] **Step 5: Write the failing handler tests**

Create `internal/apps/later/list_test.go`:

```go
package later_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// seedTagged saves an extracted article for Alice with tags.
func seedTagged(t *testing.T, s *server, url, title string, tags ...string) later.Article {
	t.Helper()
	return seed(t, s, s.Alice.User.ID, later.NewArticle{URL: url, Title: title, ContentHTML: words(10), Tags: tags})
}

// texts is each node's text with its whitespace collapsed.
func texts(nodes []*html.Node) []string {
	var out []string
	for _, n := range nodes {
		out = append(out, strings.Join(strings.Fields(htmlassert.Text(n)), " "))
	}
	return out
}

func TestIndexShowsATagChipPerTag(t *testing.T) {
	s := newServer(t)
	seedTagged(t, s, "https://a.example/1", "Essay", "essays")
	seedTagged(t, s, "https://a.example/2", "News", "news")
	doc := s.Get(t, s.Alice, "/later/")

	chips := doc.QueryAll("a.later-tag-chip")
	if got := texts(chips); !slices.Equal(got, []string{"essays", "news"}) {
		t.Fatalf("chips = %q, want [essays news]", got)
	}
	if href, _ := htmlassert.Attr(chips[0], "href"); href != "/later/?tab=unread&tag=essays" {
		t.Errorf("chip href = %q", href)
	}
	if _, ok := htmlassert.Attr(chips[0], "aria-current"); ok {
		t.Error("a chip is marked current with no tag selected")
	}
}

func TestIndexWithoutTagsHasNoChips(t *testing.T) {
	s := newServer(t)
	seedStates(t, s)
	s.Get(t, s.Alice, "/later/").MustNotHave(".later-tags")
}

func TestTagChipNarrowsRowsAndCounts(t *testing.T) {
	s := newServer(t)
	seedTagged(t, s, "https://a.example/1", "Essay", "essays")
	seedTagged(t, s, "https://a.example/2", "News", "news")
	seedTagged(t, s, "https://a.example/3", "Plain")
	doc := s.Get(t, s.Alice, "/later/?tab=unread&tag=Essays") // normalised to essays

	rows := doc.QueryAll(".later-row")
	if len(rows) != 1 || !strings.Contains(htmlassert.Text(rows[0]), "Essay") {
		t.Fatalf("rows = %q, want just Essay", texts(rows))
	}
	tabs := doc.QueryAll(".later-tab")
	if got := texts(tabs); !slices.Equal(got, []string{"Unread 1", "Reading 0", "Archived 0"}) {
		t.Errorf("tabs = %q, want counts narrowed to the tag", got)
	}
	if href, _ := htmlassert.Attr(tabs[2], "href"); href != "/later/?tab=archived&tag=essays" {
		t.Errorf("archived tab href = %q, want it to keep the tag", href)
	}
	for _, c := range doc.QueryAll("a.later-tag-chip") {
		href, _ := htmlassert.Attr(c, "href")
		_, current := htmlassert.Attr(c, "aria-current")
		switch htmlassert.Text(c) {
		case "essays":
			if !current || href != "/later/?tab=unread" {
				t.Errorf("selected chip: current=%v href=%q, want current and a link that clears it", current, href)
			}
		case "news":
			if current || href != "/later/?tab=unread&tag=news" {
				t.Errorf("other chip: current=%v href=%q", current, href)
			}
		}
	}
	doc.MustHave(`input[value="/later/?tab=unread&tag=essays"]`) // the row forms come back here
}

func TestAStaleTagStillHasAChipToClearIt(t *testing.T) {
	s := newServer(t)
	seedTagged(t, s, "https://a.example/1", "Essay", "essays")
	doc := s.Get(t, s.Alice, "/later/?tag=gone")
	var found bool
	for _, c := range doc.QueryAll("a.later-tag-chip") {
		if htmlassert.Text(c) == "gone" {
			found = true
			if href, _ := htmlassert.Attr(c, "href"); href != "/later/?tab=unread" {
				t.Errorf("stale chip href = %q", href)
			}
		}
	}
	if !found {
		t.Error("no chip for the stale tag")
	}
	if got := htmlassert.Text(doc.MustHave(".later-empty")); got != "Nothing tagged “gone” here." {
		t.Errorf("empty text = %q", got)
	}
}

func TestRowShowsTagPills(t *testing.T) {
	s := newServer(t)
	seedTagged(t, s, "https://a.example/1", "Essay", "work", "essays")
	doc := s.Get(t, s.Alice, "/later/")
	if got := texts(doc.QueryAll(".later-pill-tag")); !slices.Equal(got, []string{"essays", "work"}) {
		t.Errorf("tag pills = %q, want [essays work]", got)
	}
}

func TestLoadMoreKeepsTheTag(t *testing.T) {
	s := newServer(t)
	for i := 0; i < 51; i++ {
		seedTagged(t, s, fmt.Sprintf("https://p.example/%d", i), fmt.Sprintf("Item %d", i), "bulk")
	}
	doc := s.Get(t, s.Alice, "/later/?tab=unread&tag=bulk")
	if got, _ := htmlassert.Attr(doc.MustHave(".later-more button"), "hx-get"); got != "/later/?tab=unread&tag=bulk&offset=50" {
		t.Errorf("hx-get = %q", got)
	}
}
```

Later tasks add tests to this file that need `context`, `net/http`,
`net/http/httptest` and `net/url`; add each import when the first test
using it lands, so every commit builds.

- [ ] **Step 6: Run them to see them fail**

Run: `go test ./internal/apps/later/ -run 'Chip|Tag|LoadMore' -count=1`
Expected: build failure or FAIL — no chips, no tag filter.

- [ ] **Step 7: Move the list code into `list.go` and add the tag filter**

Move `tabView`, `rowView`, `indexView`, `tabs`, `parseTab`, `index`,
`savedNote`, `renderIndex`, `renderListPage` and `siteInitial` from
`handlers.go` into a new `internal/apps/later/list.go` (same package),
then change them as follows.

```go
package later

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// listQuery is what the list page shows: a tab, optionally narrowed to one
// tag (spec: "Tag chips filter by one tag, combined with the current tab").
type listQuery struct {
	Tab State
	Tag string // "" means every tag
}

// url is the list page for q. Parameters come in a fixed order (tab, tag,
// offset) so links are stable and tests can pin them.
func (q listQuery) url(offset int) string {
	var b strings.Builder
	b.WriteString("/later/?tab=" + string(q.Tab))
	if q.Tag != "" {
		b.WriteString("&tag=" + url.QueryEscape(q.Tag))
	}
	if offset > 0 {
		fmt.Fprintf(&b, "&offset=%d", offset)
	}
	return b.String()
}

func parseListQuery(v url.Values) listQuery {
	return listQuery{Tab: parseTab(v.Get("tab")), Tag: tagParam(v.Get("tag"))}
}

// tagParam is the one tag a ?tag= names, cleaned like a stored name.
func tagParam(v string) string {
	if ts := ParseTags(v); len(ts) > 0 {
		return ts[0]
	}
	return ""
}

type chipView struct {
	Name, URL string
	Current   bool
}

// tagChips is one chip per tag. The selected one links back to the whole
// tab, so clicking it again clears the filter. A ?tag= that names no tag
// any more still gets a chip, or nothing on the page could clear it.
func tagChips(q listQuery, names []string) []chipView {
	if q.Tag != "" && !slices.Contains(names, q.Tag) {
		names = append([]string{q.Tag}, names...)
	}
	var out []chipView
	for _, n := range names {
		next := q
		next.Tag = n
		if n == q.Tag {
			next.Tag = ""
		}
		out = append(out, chipView{Name: n, URL: next.url(0), Current: n == q.Tag})
	}
	return out
}
```

Change the view types:

```go
type tabView struct {
	State   State
	Label   string
	Count   int
	Current bool
	URL     string
}

type rowView struct {
	ID       int64
	Title    string
	Site     string
	Minutes  int
	LinkOnly bool
	Progress int // percent, 0-100
	State    State
	Archived bool
	Tags     []string

	Highlights int

	FaviconSrc string // "" when no <img> should be emitted
	Initial    string // the site's first letter, for the badge
}

type indexView struct {
	Tabs      []tabView
	Tab       State
	Tag       string
	Chips     []chipView
	Back      string // this list, for the row forms' back field
	Rows      []rowView
	NextURL   string // Load more; "" when there are no more rows
	FormError string
	FormValue string
	EmptyText string
	Saved     *savedView // the note after a save, or nil
}

// newRow is the list row for it.
func newRow(it ListItem) rowView {
	row := rowView{
		ID:       it.ID,
		Title:    it.Title,
		Site:     it.SiteHost,
		Minutes:  ReadingMinutes(it.WordCount),
		LinkOnly: it.Content == ContentLinkOnly,
		Progress: int(math.Round(it.Progress * 100)),
		State:    it.State,
		Archived: it.State == StateArchived,
		Tags:     it.Tags,
		Initial:  siteInitial(it.SiteHost),

		Highlights: it.Highlights,
	}
	if it.FaviconShown {
		row.FaviconSrc = "/later/favicon/" + it.FaviconHash
	}
	return row
}
```

`index` becomes:

```go
func (a *App) index(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	a.renderListPage(w, r, userID, parseListQuery(r.URL.Query()), offset, http.StatusOK, "", "", a.savedNote(r, userID))
}
```

`renderIndex` passes `listQuery{Tab: tab}`:

```go
func (a *App) renderIndex(w http.ResponseWriter, r *http.Request, userID int64, tab State, status int, formError, formValue string) {
	a.renderListPage(w, r, userID, listQuery{Tab: tab}, 0, status, formError, formValue, nil)
}
```

`renderListPage` becomes:

```go
// renderListPage draws the list page, or just its rows when HTMX asks for the
// next page.
func (a *App) renderListPage(w http.ResponseWriter, r *http.Request, userID int64, q listQuery, offset, status int, formError, formValue string, saved *savedView) {
	ctx := r.Context()
	items, err := a.store.List(ctx, userID, q.Tab, q.Tag, offset, pageSize+1)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	counts, err := a.store.Counts(ctx, userID, q.Tag)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	names, err := a.store.TagNames(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	view := indexView{
		Tab: q.Tab, Tag: q.Tag, Chips: tagChips(q, names), Back: q.url(0),
		FormError: formError, FormValue: formValue, Saved: saved,
	}
	if len(items) > pageSize {
		items = items[:pageSize]
		view.NextURL = q.url(offset + pageSize)
	}
	for _, it := range items {
		view.Rows = append(view.Rows, newRow(it))
	}
	for _, t := range tabs {
		tq := listQuery{Tab: t.state, Tag: q.Tag}
		view.Tabs = append(view.Tabs, tabView{State: t.state, Label: t.label, Count: counts[t.state], Current: t.state == q.Tab, URL: tq.url(0)})
		if t.state == q.Tab {
			view.EmptyText = t.empty
		}
	}
	if q.Tag != "" {
		view.EmptyText = fmt.Sprintf("Nothing tagged “%s” here.", q.Tag)
	}
	page := a.deps.Page(r, "ON Later")
	page.Data = view
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) && offset > 0 {
		if err := a.deps.Render.Fragment(w, status, "later/index", "rows", page); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	if err := a.deps.Render.Page(w, status, "later/index", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
```

Remove now-unused imports from `handlers.go` (`math`, `unicode`,
`unicode/utf8` if nothing else there uses them; `goimports`-style tidy by
hand — `go vet` will tell you).

- [ ] **Step 8: Update `index.html`**

Tabs use their URL, and the chips follow them:

```html
	<nav class="later-tabs" aria-label="Lists">
		{{range .Data.Tabs}}
		<a class="later-tab" href="{{.URL}}"{{if .Current}} aria-current="page"{{end}}>{{.Label}} <span class="later-tab-count">{{.Count}}</span></a>
		{{end}}
	</nav>
	{{if .Data.Chips}}
	<nav class="later-tags" aria-label="Tags">
		{{range .Data.Chips}}<a class="later-tag-chip" href="{{.URL}}"{{if .Current}} aria-current="true"{{end}}>{{.Name}}</a>
		{{end}}
	</nav>
	{{end}}
```

In the `rows` block:
- After the highlight pill in `.later-row-meta`, add
  `{{range .Tags}}<span class="later-pill later-pill-tag">{{.}}</span>{{end}}`.
- Replace `{{if eq $.Data.Tab "archived"}}` with `{{if .Archived}}` (a
  search in Task 5 mixes states in one list, so the row decides).
- Replace each of the three
  `<input type="hidden" name="back" value="/later/?tab={{$.Data.Tab}}">`
  with `<input type="hidden" name="back" value="{{$.Data.Back}}">`.
- The Load more block becomes:

```html
{{if .Data.NextURL}}
<li class="later-more">
	<button type="button"
	        hx-get="{{.Data.NextURL}}"
	        hx-target="closest li" hx-swap="outerHTML">Load more</button>
</li>
{{end}}
```

- [ ] **Step 9: Styles**

In `internal/ui/static/app.css`'s ON Later section, add `flex-wrap: wrap;`
to the existing `.later-row-meta` rule, and add after `.later-tab-count`:

```css
.later-tags { display: flex; flex-wrap: wrap; gap: .4rem; }
.later-tag-chip { border: var(--border); border-radius: 999px; padding: .05rem .6rem; font-size: var(--fs-sm); color: var(--c-text-dim); text-decoration: none; }
.later-tag-chip[aria-current="true"] { background: var(--c-accent-bg); color: var(--c-accent); border-color: var(--c-accent); }
.later-pill-tag { background: var(--c-accent-bg); color: var(--c-accent); text-decoration: none; }
```

- [ ] **Step 10: Run the tests**

Run: `go test ./internal/apps/later/ -count=1`
Expected: PASS, including the existing list tests
(`/later/?tab=unread&offset=50`, the `back` values).

- [ ] **Step 11: Full check and commit**

```bash
git add internal/apps/later internal/ui/static/app.css
git commit -m "feat(later): filter the list by tag, with tag chips and row pills"
```

---

### Task 3: Editing tags

**Files:**
- Modify: `internal/apps/later/handlers.go` (`setTags`, `save`, `articleView`, `buildArticleView`), `internal/apps/later/list.go` (`saveForm`), `internal/apps/later/later.go` (route), `internal/apps/later/templates/index.html`, `internal/apps/later/templates/article.html`, `internal/apps/later/templates/popup.html`, `internal/ui/static/app.css`, `docs/user/later.md`
- Test: `internal/apps/later/list_test.go`

**Interfaces:**
- Consumes: `ParseTags`, `SetTags`, `ArticleTags` (Task 1); `listQuery`, `newRow`, `rowView` (Task 2).
- Produces:
```go
func (a *App) setTags(w http.ResponseWriter, r *http.Request) // POST /later/a/{id}/tags: tags, back
type saveForm struct{ Error, URL, Tags string }
// renderListPage(w, r, userID, q listQuery, offset, status int, form saveForm, saved *savedView)
// renderIndex(w, r, userID, tab State, status int, form saveForm)
// indexView: FormError/FormValue replaced by Form saveForm
// rowView gains TagsValue string ("a, b")
type tagLink struct{ Name, URL string }
// articleView gains Tags []tagLink, TagsValue string
```

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/later/list_test.go` (add `context`, `net/http`,
`net/url` and `github.com/iliafrenkel/on-suite/internal/apptest` imports as
needed):

```go
func storedTags(t *testing.T, s *server, a later.Article) []string {
	t.Helper()
	got, err := s.Store.ArticleTags(context.Background(), s.Alice.User.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSetTagsFromTheRowMenu(t *testing.T) {
	s := newServer(t)
	a := seedTagged(t, s, "https://a.example/1", "Essay", "old")
	doc := s.Get(t, s.Alice, "/later/")
	path := articlePath(a, "/tags")
	doc.MustHave(".later-row-menu form[action=" + path + "]")
	if got := attr(t, doc, "input#later-tags-"+fmt.Sprint(a.ID), "value"); got != "old" {
		t.Errorf("row tags input = %q, want old", got)
	}

	s.Submit(t, s.Alice, path, url.Values{"tags": {"Essays, AI"}, "back": {"/later/?tab=unread"}}, "/later/?tab=unread")
	if got := storedTags(t, s, a); !slices.Equal(got, []string{"ai", "essays"}) {
		t.Errorf("tags = %q, want [ai essays]", got)
	}
	if got := attr(t, s.Get(t, s.Alice, "/later/"), "input#later-tags-"+fmt.Sprint(a.ID), "value"); got != "ai, essays" {
		t.Errorf("row tags input after saving = %q", got)
	}
}

func TestSetTagsGoesBackToTheArticleByDefault(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	s.Submit(t, s.Alice, articlePath(a, "/tags"), url.Values{"tags": {"x"}}, articlePath(a, ""))
	s.Submit(t, s.Alice, articlePath(a, "/tags"), url.Values{"tags": {"x"}, "back": {"https://evil.example/"}}, articlePath(a, ""))
}

func TestSetTagsOnSomeoneElsesArticleIs404(t *testing.T) {
	s := newServer(t)
	a := seedTagged(t, s, "https://a.example/1", "Essay", "mine")
	rec := s.Post(t, s.Bob, articlePath(a, "/tags"), url.Values{"tags": {"theirs"}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if got := storedTags(t, s, a); !slices.Equal(got, []string{"mine"}) {
		t.Errorf("tags = %q, want them untouched", got)
	}
}

func TestReadingViewShowsAndEditsTags(t *testing.T) {
	s := newServer(t)
	a := seedTagged(t, s, "https://a.example/1", "Essay", "work", "essays")
	doc := s.Get(t, s.Alice, articlePath(a, "")) // opening moves it to reading

	links := doc.QueryAll(".later-article-tags a")
	if got := texts(links); !slices.Equal(got, []string{"essays", "work"}) {
		t.Fatalf("tag links = %q", got)
	}
	if href, _ := htmlassert.Attr(links[0], "href"); href != "/later/?tab=reading&tag=essays" {
		t.Errorf("tag link href = %q", href)
	}
	path := articlePath(a, "/tags")
	doc.MustHave(".later-topbar form[action=" + path + "]")
	if got := attr(t, doc, "input#later-tags-input", "value"); got != "essays, work" {
		t.Errorf("tags input = %q", got)
	}
	doc.MustHave(`.later-tags-form input[value="` + articlePath(a, "") + `"]`) // back to the article
}

func TestReadingViewWithoutTagsHasNoTagLine(t *testing.T) {
	s := newServer(t)
	a := seedReadable(t, s)
	s.Get(t, s.Alice, articlePath(a, "")).MustNotHave(".later-article-tags")
}

func TestSaveBoxTagsTheNewArticle(t *testing.T) {
	s, app := newSaveServer(t)
	app.AllowPrivateFetchesForTest()
	origin := pageOrigin(t, "text/html", articlePage)
	rec := s.Post(t, s.Alice, "/later/save", url.Values{"url": {origin.URL + "/essay"}, "tags": {"Long reads, #ai"}})
	id := idFrom(t, rec)
	got, err := s.Store.ArticleTags(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"ai", "long reads"}) {
		t.Errorf("tags = %q, want [ai long reads]", got)
	}
}

func TestSaveBoxKeepsTagsOnABadURL(t *testing.T) {
	s := newServer(t)
	rec := s.Post(t, s.Alice, "/later/save", url.Values{"url": {"not a url"}, "tags": {"keep me"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := attr(t, doc, "input#later-save-tags", "value"); got != "keep me" {
		t.Errorf("tags input = %q, want it kept", got)
	}
}

func TestPopupHasATagsField(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/later/save?url=https://example.com/a")
	doc.MustHave(".later-popup input#later-popup-tags")
	if got := attr(t, doc, "input#later-popup-tags", "name"); got != "tags" {
		t.Errorf("name = %q", got)
	}
}
```

`idFrom`, `pageOrigin`, `articlePage` and `newSaveServer` live in
`save_test.go` (same package); reuse them, don't redeclare them.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/later/ -run 'SetTags|ReadingView|SaveBox|Popup' -count=1`
Expected: FAIL — no `/tags` route (404/405), no tags inputs.

- [ ] **Step 3: The `setTags` handler and route**

In `handlers.go`, add:

```go
// setTags replaces an article's tags from the comma-separated field in a
// row's or the reading view's ⋯ menu, then goes back where it came from.
func (a *App) setTags(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.SetTags(r.Context(), userID, id, ParseTags(r.PostFormValue("tags"))); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, safeBack(r, fmt.Sprintf("/later/a/%d", id)), http.StatusSeeOther)
}
```

In `later.go`'s `Mount`, after the `note` route:

```go
	r.HandleFunc("POST /a/{id}/tags", a.setTags)
```

- [ ] **Step 4: Tags in the save box and popup**

In `list.go`, add the form type and replace `FormError`/`FormValue`:

```go
// saveForm is what the save box shows again after a refused save.
type saveForm struct {
	Error, URL, Tags string
}
```

`indexView` loses `FormError` and `FormValue` and gains `Form saveForm`.
`index` passes `saveForm{}`; `renderIndex` becomes
`renderIndex(w, r, userID, tab State, status int, form saveForm)`;
`renderListPage` takes `form saveForm` in place of
`formError, formValue string` and sets `Form: form`.

In `handlers.go`'s `save`, the bad-URL branch becomes:

```go
		a.renderIndex(w, r, userID, StateUnread, http.StatusUnprocessableEntity, saveForm{
			Error: badURLMessage, URL: r.PostFormValue("url"), Tags: r.PostFormValue("tags"),
		})
```

and just after `n := a.fetchArticle(r.Context(), pageURL)`:

```go
	n.Tags = ParseTags(r.PostFormValue("tags"))
```

In `rowView` add `TagsValue string`, and in `newRow` set
`TagsValue: strings.Join(it.Tags, ", "),`.

In `index.html`, the save form becomes:

```html
	<form class="later-save" method="post" action="/later/save">
		<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
		<label class="visually-hidden" for="later-url">Web address</label>
		<input id="later-url" class="later-save-input" name="url" type="text" inputmode="url" placeholder="Paste a URL to save…" value="{{.Data.Form.URL}}">
		<label class="visually-hidden" for="later-save-tags">Tags</label>
		<input id="later-save-tags" class="later-save-tags" name="tags" type="text" placeholder="Tags, comma-separated" autocomplete="off" value="{{.Data.Form.Tags}}">
		<button class="primary" type="submit">Save</button>
	</form>
	{{with .Data.Form.Error}}<p class="later-form-error" role="alert">{{.}}</p>{{end}}
```

In the row menu panel, before the archive/unarchive form:

```html
			<form class="later-tags-form stack" method="post" action="/later/a/{{.ID}}/tags">
				<input type="hidden" name="{{csrfField}}" value="{{$.Shell.CSRFToken}}">
				<input type="hidden" name="back" value="{{$.Data.Back}}">
				<label for="later-tags-{{.ID}}">Tags</label>
				<input id="later-tags-{{.ID}}" name="tags" type="text" value="{{.TagsValue}}" placeholder="essays, ai" autocomplete="off">
				<button type="submit">Save tags</button>
			</form>
```

In `popup.html`, the save form becomes:

```html
	<form method="post" action="/later/save" class="stack">
		<input type="hidden" name="{{csrfField}}" value="{{.Shell.CSRFToken}}">
		<input type="hidden" name="url" value="{{.Data.URL}}">
		<input type="hidden" name="popup" value="1">
		<label for="later-popup-tags">Tags (optional)</label>
		<input id="later-popup-tags" name="tags" type="text" placeholder="essays, ai" autocomplete="off">
		<div class="dialog-actions">
			<button class="primary" type="submit" autofocus>Save</button>
			<button type="button" data-later-close>Cancel</button>
		</div>
	</form>
```

- [ ] **Step 5: Tags in the reading view**

In `handlers.go`, add:

```go
// tagLink is one of an article's tags, linking to its list filtered by it.
type tagLink struct{ Name, URL string }
```

`articleView` gains (after `Note string`):

```go
	Tags      []tagLink
	TagsValue string // the ⋯ menu's tags field
```

In `buildArticleView`, before `view.Note = art.Note`:

```go
	names, err := a.store.ArticleTags(r.Context(), userID, art.ID)
	if err != nil {
		return articleView{}, err
	}
	for _, n := range names {
		view.Tags = append(view.Tags, tagLink{Name: n, URL: listQuery{Tab: art.State, Tag: n}.url(0)})
	}
	view.TagsValue = strings.Join(names, ", ")
```

In `article.html`, after the `<p class="later-article-meta">…</p>`:

```html
			{{if $d.Tags}}<p class="later-article-tags">{{range $d.Tags}}<a class="later-pill later-pill-tag" href="{{.URL}}">{{.Name}}</a>
			{{end}}</p>{{end}}
```

and in the topbar's ⋯ menu panel, before **Open original**:

```html
				<form class="later-tags-form stack" method="post" action="/later/a/{{$d.ID}}/tags">
					<input type="hidden" name="{{csrfField}}" value="{{$csrf}}">
					<input type="hidden" name="back" value="{{$d.Back}}">
					<label for="later-tags-input">Tags</label>
					<input id="later-tags-input" name="tags" type="text" value="{{$d.TagsValue}}" placeholder="essays, ai" autocomplete="off">
					<button type="submit">Save tags</button>
				</form>
```

- [ ] **Step 6: Styles**

```css
.later-save-tags { flex: 0 1 12rem; min-width: 0; }
.later-tags-form input { width: 100%; }
.later-article-tags { display: flex; flex-wrap: wrap; gap: .4rem; margin: 0; }
```

and inside the existing `@media (max-width: 40rem)` block of the ON Later
section:

```css
  .later-save { flex-wrap: wrap; }
  .later-save-input { flex-basis: 100%; }
```

- [ ] **Step 7: User guide**

In `docs/user/later.md`, after the "Finding your articles" section, add:

```markdown
## Tags

Tags group articles however suits you — `essays`, `work`, `to cook`.

- **When saving:** type tags, separated by commas, in the **Tags** box
  next to the address, or in the bookmarklet's window.
- **Afterwards:** open an article's **⋯** menu — in the list, or while
  reading — change the **Tags** box and click **Save tags**. Empty the box
  to remove all its tags.

Tags are lowercase, and a tag disappears once no article has it. Click a
tag above the list to see only the articles with it; the counts on
**Unread**, **Reading** and **Archived** follow it. Click the tag again to
see everything. While reading, an article's tags show under its title;
click one to go to its list.
```

- [ ] **Step 8: Run the tests**

Run: `go test ./internal/apps/later/ -count=1`
Expected: PASS.

- [ ] **Step 9: Full check and commit**

```bash
git add internal/apps/later internal/ui/static/app.css docs/user/later.md
git commit -m "feat(later): edit tags when saving, from the row menu and while reading"
```

---

### Task 4: The search index and `Store.Search`

**Files:**
- Create: `internal/apps/later/migrations/0006_search.sql`, `internal/apps/later/search.go`, `internal/apps/later/search_test.go`

**Interfaces:**
- Consumes: `listSelect`, `listJoins`, `tagFilter`, `scanListItem` (Task 2).
- Produces:
```go
func ftsQuery(q string) string
type MatchIn string
const (
	MatchTitle     MatchIn = ""          // only the title matched
	MatchHighlight MatchIn = "highlight" // a highlight's quote or comment
	MatchNote      MatchIn = "note"
	MatchText      MatchIn = "text"
)
const SnippetOpen, SnippetClose = "\x02", "\x03"
type SearchHit struct {
	ListItem
	In      MatchIn
	Snippet string // the matching passage, hits wrapped in SnippetOpen/SnippetClose; "" for MatchTitle
}
func (st *Store) Search(ctx context.Context, userID int64, query, tag string, offset, limit int) ([]SearchHit, error)
```

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/later/search_test.go`:

```go
package later_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// saveBody stores an extracted article whose text is body.
func (f *fixture) saveBody(t *testing.T, userID int64, url, title, body string, tags ...string) later.Article {
	t.Helper()
	a, _, err := f.store.Save(context.Background(), userID, later.NewArticle{
		URL: url, Title: title, ContentHTML: "<p>" + body + "</p>", Tags: tags,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (f *fixture) search(t *testing.T, query, tag string) []later.SearchHit {
	t.Helper()
	hits, err := f.store.Search(context.Background(), f.alice.ID, query, tag, 0, 50)
	if err != nil {
		t.Fatalf("Search(%q): %v", query, err)
	}
	return hits
}

func hitIDs(hits []later.SearchHit) []int64 {
	var out []int64
	for _, h := range hits {
		out = append(out, h.ID)
	}
	return out
}

func TestSearchSaysWhereItMatched(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.saveBody(t, f.alice.ID, "https://a.example/1", "Walrus", "zebra crossing at night")
	if _, err := f.store.AddHighlight(ctx, a.ID, a.ContentText, 0, 5, "zebra", "giraffe thoughts"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetNote(ctx, f.alice.ID, a.ID, "remember the okapi"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query, hit string
		in         later.MatchIn
	}{
		{"walrus", "", later.MatchTitle},
		{"night", "night", later.MatchText},
		{"giraffe", "giraffe", later.MatchHighlight},
		{"zebra", "zebra", later.MatchHighlight}, // the quote beats the text
		{"okapi", "okapi", later.MatchNote},
	} {
		hits := f.search(t, tc.query, "")
		if len(hits) != 1 || hits[0].ID != a.ID {
			t.Fatalf("Search(%q) = %v, want the article", tc.query, hitIDs(hits))
		}
		if hits[0].In != tc.in {
			t.Errorf("Search(%q).In = %q, want %q", tc.query, hits[0].In, tc.in)
		}
		want := later.SnippetOpen + tc.hit + later.SnippetClose
		if tc.hit != "" && !strings.Contains(hits[0].Snippet, want) {
			t.Errorf("Search(%q).Snippet = %q, want it to mark %q", tc.query, hits[0].Snippet, tc.hit)
		}
		if tc.hit == "" && hits[0].Snippet != "" {
			t.Errorf("title-only Snippet = %q, want none", hits[0].Snippet)
		}
	}
}

func TestSearchFindsEveryStateButOnlyYours(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.saveBody(t, f.alice.ID, "https://a.example/1", "One", "kumquat")
	b := f.saveBody(t, f.alice.ID, "https://a.example/2", "Two", "kumquat")
	f.saveBody(t, f.bob.ID, "https://a.example/3", "Bob's", "kumquat")
	if err := f.store.SetState(ctx, f.alice.ID, b.ID, later.StateArchived); err != nil {
		t.Fatal(err)
	}
	hits := f.search(t, "kumquat", "")
	if len(hits) != 2 {
		t.Fatalf("hits = %v, want alice's two", hitIDs(hits))
	}
	states := map[int64]later.State{}
	for _, h := range hits {
		states[h.ID] = h.State
	}
	if states[a.ID] != later.StateUnread || states[b.ID] != later.StateArchived {
		t.Errorf("states = %v", states)
	}
}

func TestSearchRanksTitleMatchesFirst(t *testing.T) {
	f := newFixture(t)
	body := f.saveBody(t, f.alice.ID, "https://a.example/1", "Other things", "some gardening tips")
	title := f.saveBody(t, f.alice.ID, "https://a.example/2", "Gardening", "nothing here")
	got := hitIDs(f.search(t, "gardening", ""))
	if len(got) != 2 || got[0] != title.ID || got[1] != body.ID {
		t.Errorf("order = %v, want title match %d then body match %d", got, title.ID, body.ID)
	}
}

func TestSearchMatchesPrefixesAndFoldsCase(t *testing.T) {
	f := newFixture(t)
	a := f.saveBody(t, f.alice.ID, "https://a.example/1", "Words", "Categorically Привет мир")
	for _, q := range []string{"categ", "CATEGORICALLY", "привет", "ПРИВ"} {
		if got := hitIDs(f.search(t, q, "")); len(got) != 1 || got[0] != a.ID {
			t.Errorf("Search(%q) = %v, want the article", q, got)
		}
	}
}

func TestSearchQueriesAreInert(t *testing.T) {
	f := newFixture(t)
	f.saveBody(t, f.alice.ID, "https://a.example/1", "Words", "plain text")
	for _, q := range []string{`AND`, `"`, `(`, `title:plain`, `NEAR(a b)`, `*`, `-plain`} {
		if _, err := f.store.Search(context.Background(), f.alice.ID, q, "", 0, 10); err != nil {
			t.Errorf("Search(%q) = %v, want no error", q, err)
		}
	}
	for _, q := range []string{"", "   "} {
		hits, err := f.store.Search(context.Background(), f.alice.ID, q, "", 0, 10)
		if err != nil || len(hits) != 0 {
			t.Errorf("Search(%q) = %v, %v; want nothing", q, hitIDs(hits), err)
		}
	}
}

func TestSearchNarrowsByTag(t *testing.T) {
	f := newFixture(t)
	a := f.saveBody(t, f.alice.ID, "https://a.example/1", "One", "pomelo", "fruit")
	f.saveBody(t, f.alice.ID, "https://a.example/2", "Two", "pomelo")
	if got := hitIDs(f.search(t, "pomelo", "fruit")); len(got) != 1 || got[0] != a.ID {
		t.Errorf("Search(tag fruit) = %v, want [%d]", got, a.ID)
	}
}

func TestSearchStaysInStepWithEdits(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.saveBody(t, f.alice.ID, "https://a.example/1", "One", "alpha beta")

	if err := f.store.SetNote(ctx, f.alice.ID, a.ID, "quince"); err != nil {
		t.Fatal(err)
	}
	if len(f.search(t, "quince", "")) != 1 {
		t.Error("note not indexed")
	}
	if err := f.store.SetNote(ctx, f.alice.ID, a.ID, ""); err != nil {
		t.Fatal(err)
	}
	if len(f.search(t, "quince", "")) != 0 {
		t.Error("old note still indexed")
	}

	h, err := f.store.AddHighlight(ctx, a.ID, a.ContentText, 0, 5, "alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetHighlightComment(ctx, a.ID, h.ID, "loquat"); err != nil {
		t.Fatal(err)
	}
	if hits := f.search(t, "loquat", ""); len(hits) != 1 || hits[0].In != later.MatchHighlight {
		t.Error("comment not indexed")
	}
	if err := f.store.DeleteHighlight(ctx, a.ID, h.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.search(t, "loquat", "")) != 0 {
		t.Error("deleted highlight's comment still indexed")
	}

	link, _, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{URL: "https://a.example/2", Title: "Walled"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetPastedText(ctx, f.alice.ID, link.ID, "durian season"); err != nil {
		t.Fatal(err)
	}
	if got := hitIDs(f.search(t, "durian", "")); len(got) != 1 || got[0] != link.ID {
		t.Errorf("pasted text not indexed: %v", got)
	}

	if err := f.store.Delete(ctx, f.alice.ID, link.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.db.QueryRowContext(ctx, `SELECT count(*) FROM later_search WHERE rowid = ?`, link.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("deleted article still has %d search rows", n)
	}
}

func TestSearchPages(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 3; i++ {
		f.saveBody(t, f.alice.ID, "https://a.example/"+string(rune('a'+i)), "T", "lychee")
	}
	first, err := f.store.Search(context.Background(), f.alice.ID, "lychee", "", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	rest, err := f.store.Search(context.Background(), f.alice.ID, "lychee", "", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(rest) != 1 {
		t.Errorf("pages = %d + %d, want 2 + 1", len(first), len(rest))
	}
}

// TestSearchMigrationIndexesExistingArticles applies the migrations before
// 0006, saves an article with a highlight and a note, then applies 0006 and
// checks the backfill indexed all of it.
func TestSearchMigrationIndexesExistingArticles(t *testing.T) {
	ctx := context.Background()
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	authMs, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	laterMs, err := db.Collect(later.ID, later.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if len(laterMs) != 6 {
		t.Fatalf("got %d later migrations; this test assumes 0006 is the last", len(laterMs))
	}
	if _, err := db.Apply(ctx, handle, append(append([]db.Migration{}, authMs...), laterMs[:5]...)); err != nil {
		t.Fatal(err)
	}
	u, err := auth.NewStore(handle).CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}
	st := later.NewStore(handle)
	a, _, err := st.Save(ctx, u.ID, later.NewArticle{URL: "https://a.example/1", Title: "Tapir", ContentHTML: "<p>zebra crossing</p>"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddHighlight(ctx, a.ID, a.ContentText, 0, 5, "zebra", "giraffe"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetNote(ctx, u.ID, a.ID, "okapi"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Apply(ctx, handle, append(authMs, laterMs...)); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"tapir", "crossing", "giraffe", "okapi"} {
		hits, err := st.Search(ctx, u.ID, q, "", 0, 10)
		if err != nil || len(hits) != 1 {
			t.Errorf("Search(%q) after migrating = %d hits, %v; want 1", q, len(hits), err)
		}
	}
}
```

Check `internal/platform/db/migrate.go` for the exact type `Collect`
returns (`[]db.Migration` here) and adjust the slice type if it differs.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/later/ -run Search -count=1`
Expected: build failure — `Search`, `SearchHit`, `MatchIn`, `SnippetOpen` undefined.

- [ ] **Step 3: The migration**

Create `internal/apps/later/migrations/0006_search.sql`:

```sql
-- L3: full-text search over an article's title, text, highlights (quotes
-- and comments) and note (spec: "later_search").
--
-- A regular FTS5 table, keeping its own copy of the text, rather than an
-- external-content one like Reader's: the highlights column is gathered
-- from another table, which external content can't express, and snippet()
-- needs the text. For one household the extra copy is cheap.
--
-- rowid is the article id. Only the triggers below write to it, so every
-- change lands in the same transaction as the write that caused it (spec:
-- "kept in step in the same transactions"), including the cascade from
-- deleting an article. prefix='2 3 4' is what makes the live filter match a
-- word still being typed, as in Reader's and Notes' indexes.
CREATE VIRTUAL TABLE later_search USING fts5(
    title, body, highlights, note,
    tokenize='unicode61', prefix='2 3 4'
);

INSERT INTO later_search (rowid, title, body, highlights, note)
SELECT a.id, a.title, a.content_text,
       COALESCE((SELECT group_concat(h.quote || ' ' || h.comment, char(10) ORDER BY h.start_offset)
                   FROM later_highlights h WHERE h.article_id = a.id), ''),
       a.note
  FROM later_articles a;

CREATE TRIGGER later_search_ai AFTER INSERT ON later_articles BEGIN
    INSERT INTO later_search (rowid, title, body, highlights, note)
    VALUES (new.id, new.title, new.content_text, '', new.note);
END;

-- Only the indexed columns: progress and state writes leave the index alone.
CREATE TRIGGER later_search_au AFTER UPDATE OF title, content_text, note ON later_articles BEGIN
    UPDATE later_search SET title = new.title, body = new.content_text, note = new.note
     WHERE rowid = new.id;
END;

CREATE TRIGGER later_search_ad AFTER DELETE ON later_articles BEGIN
    DELETE FROM later_search WHERE rowid = old.id;
END;

-- A highlight change rebuilds its article's whole highlights column.
CREATE TRIGGER later_search_hl_ai AFTER INSERT ON later_highlights BEGIN
    UPDATE later_search SET highlights = COALESCE((
        SELECT group_concat(h.quote || ' ' || h.comment, char(10) ORDER BY h.start_offset)
          FROM later_highlights h WHERE h.article_id = new.article_id), '')
     WHERE rowid = new.article_id;
END;

CREATE TRIGGER later_search_hl_au AFTER UPDATE OF quote, comment ON later_highlights BEGIN
    UPDATE later_search SET highlights = COALESCE((
        SELECT group_concat(h.quote || ' ' || h.comment, char(10) ORDER BY h.start_offset)
          FROM later_highlights h WHERE h.article_id = new.article_id), '')
     WHERE rowid = new.article_id;
END;

CREATE TRIGGER later_search_hl_ad AFTER DELETE ON later_highlights BEGIN
    UPDATE later_search SET highlights = COALESCE((
        SELECT group_concat(h.quote || ' ' || h.comment, char(10) ORDER BY h.start_offset)
          FROM later_highlights h WHERE h.article_id = old.article_id), '')
     WHERE rowid = old.article_id;
END;
```

- [ ] **Step 4: `search.go`**

Create `internal/apps/later/search.go`:

```go
package later

import (
	"context"
	"fmt"
	"strings"
)

// ftsQuery turns free text into an FTS5 MATCH expression that can never be a
// syntax error.
//
// Mirrors ON Reader's ftsQuery (internal/apps/reader/search.go), itself a
// copy of ON Notes' — copied rather than shared because apps never import
// each other; see "Cross-app mirroring" in PATTERNS.md.
//
// Each word becomes its own quoted phrase, doubling any embedded quote, so
// anything a person types — an operator like AND, a bare quote, a
// parenthesis — lands inside the quotes as inert phrase text instead of
// breaking the query. The trailing * is FTS5's prefix operator, which is
// what makes a live filter match while a word is still being typed.
func ftsQuery(q string) string {
	words := strings.Fields(q)
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"*`
	}
	return strings.Join(quoted, " ")
}

// MatchIn says where a search result matched, for its snippet's label
// (spec: "a snippet saying where it matched").
type MatchIn string

// Where a result can match. A title-only match has no snippet: the row
// already shows the title.
const (
	MatchTitle     MatchIn = ""
	MatchHighlight MatchIn = "highlight" // a highlight's quote or comment
	MatchNote      MatchIn = "note"
	MatchText      MatchIn = "text"
)

// SnippetOpen and SnippetClose wrap each matched word in a snippet. Control
// characters never occur in content_text, quotes, comments or notes the way
// the user sees them, so they can't be confused with real text.
const (
	SnippetOpen  = "\x02"
	SnippetClose = "\x03"
)

// SearchHit is one search result: its list row plus where it matched.
type SearchHit struct {
	ListItem
	In      MatchIn
	Snippet string // "" for MatchTitle
}

// Search finds userID's articles in any state matching query, best match
// first, narrowed to tag unless it is "". A query with no words finds
// nothing.
func (st *Store) Search(ctx context.Context, userID int64, query, tag string, offset, limit int) ([]SearchHit, error) {
	match := ftsQuery(query)
	if match == "" {
		return nil, nil
	}
	// later_search is named, never aliased: the driver resolves MATCH,
	// snippet() and bm25() against the real name. Column numbers: 0 title,
	// 1 body, 2 highlights, 3 note. bm25 weights favour the title, then
	// what the reader wrote, then the text.
	rows, err := st.db.QueryContext(ctx, `
		SELECT `+listSelect+`,
		       snippet(later_search, 2, char(2), char(3), '…', 16),
		       snippet(later_search, 3, char(2), char(3), '…', 16),
		       snippet(later_search, 1, char(2), char(3), '…', 16)
		  FROM later_search
		  JOIN later_articles a ON a.id = later_search.rowid`+listJoins+`
		 WHERE later_search MATCH ? AND a.user_id = ? AND `+tagFilter+`
		 ORDER BY bm25(later_search, 10.0, 1.0, 4.0, 4.0), a.id DESC
		 LIMIT ? OFFSET ?`, match, userID, tag, tag, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("later: search: %w", err)
	}
	defer func() { _ = rows.Close() }()
	now := st.now()
	var hits []SearchHit
	for rows.Next() {
		var inHighlights, inNote, inText string
		it, err := scanListItem(rows, now, &inHighlights, &inNote, &inText)
		if err != nil {
			return nil, err
		}
		hit := SearchHit{ListItem: it}
		// snippet() returns a column's opening words even when it didn't
		// match, so a column matched only if its snippet has a marker.
		switch {
		case strings.Contains(inHighlights, SnippetOpen):
			hit.In, hit.Snippet = MatchHighlight, inHighlights
		case strings.Contains(inNote, SnippetOpen):
			hit.In, hit.Snippet = MatchNote, inNote
		case strings.Contains(inText, SnippetOpen):
			hit.In, hit.Snippet = MatchText, inText
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("later: search: %w", err)
	}
	return hits, nil
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/apps/later/ -count=1`
Expected: PASS. If `TestSearchRanksTitleMatchesFirst` fails, check the
`bm25` weights' column order matches the table's (title, body,
highlights, note) before changing anything else.

- [ ] **Step 6: Full check and commit**

```bash
git add internal/apps/later/migrations/0006_search.sql internal/apps/later/search.go internal/apps/later/search_test.go
git commit -m "feat(later): full-text search index over titles, text, highlights and notes"
```

---

### Task 5: The search box

**Files:**
- Create: `internal/apps/later/snippet.go`, `internal/apps/later/snippet_internal_test.go`
- Modify: `internal/apps/later/list.go`, `internal/apps/later/templates/index.html`, `internal/ui/static/app.css`, `docs/user/later.md`
- Test: `internal/apps/later/list_test.go`

**Interfaces:**
- Consumes: `Store.Search`, `SearchHit`, `MatchIn`, `SnippetOpen`/`SnippetClose` (Task 4); `listQuery`, `tagChips`, `newRow`, `saveForm` (Tasks 2–3).
- Produces:
```go
type snippetPart struct{ Text string; Hit bool }
func snippetParts(s string) []snippetPart
type snippetView struct{ In string; Parts []snippetPart }
// listQuery gains Q string; url adds &q= between tag and offset
func (q listQuery) searching() bool
// rowView gains StateLabel string, Snippet *snippetView
// indexView gains Q string, Searching bool, ClearURL string
// template block "later-list": the #later-list region, swapped by the search box
```

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/later/snippet_internal_test.go`:

```go
package later

import (
	"slices"
	"testing"
)

func TestSnippetParts(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []snippetPart
	}{
		{"", nil},
		{"plain", []snippetPart{{Text: "plain"}}},
		{"a \x02b\x03 c", []snippetPart{{Text: "a "}, {Text: "b", Hit: true}, {Text: " c"}}},
		{"\x02x\x03\x02y\x03", []snippetPart{{Text: "x", Hit: true}, {Text: "y", Hit: true}}},
		{"one\n\n  two \x02three\x03", []snippetPart{{Text: "one two "}, {Text: "three", Hit: true}}},
		{"cut \x02off", []snippetPart{{Text: "cut "}, {Text: "off", Hit: true}}},
	} {
		if got := snippetParts(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("snippetParts(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}
```

Append to `internal/apps/later/list_test.go`:

```go
func TestSearchShowsMatchesFromEveryState(t *testing.T) {
	s := newServer(t)
	seedStates(t, s) // Unread One, Unread Two, Reading One, Archived One
	doc := s.Get(t, s.Alice, "/later/?tab=unread&q=one")

	doc.MustNotHave(".later-tabs")
	doc.MustHave(".later-search-head")
	rows := doc.QueryAll(".later-row")
	if len(rows) != 3 {
		t.Fatalf("rows = %q, want the three titled One", texts(rows))
	}
	got := texts(doc.QueryAll(".later-pill-state"))
	slices.Sort(got)
	if !slices.Equal(got, []string{"Archived", "Reading", "Unread"}) {
		t.Errorf("state pills = %q", got)
	}
	if href, _ := htmlassert.Attr(doc.MustHave(".later-search-head a"), "href"); href != "/later/?tab=unread" {
		t.Errorf("clear link = %q, want back to the tab", href)
	}
}

func TestSearchRowsActOnTheirOwnState(t *testing.T) {
	s := newServer(t)
	seedStates(t, s)
	// Searching from Unread finds the archived article; its menu must offer
	// Move to unread, not Archive.
	doc := s.Get(t, s.Alice, "/later/?tab=unread&q=archived")
	var unarchive, archive int
	for _, f := range doc.QueryAll(".later-row-menu form") {
		action, _ := htmlassert.Attr(f, "action")
		switch {
		case strings.HasSuffix(action, "/unarchive"):
			unarchive++
		case strings.HasSuffix(action, "/archive"):
			archive++
		}
	}
	if unarchive != 1 || archive != 0 {
		t.Errorf("menu forms: %d unarchive, %d archive; want 1 and 0", unarchive, archive)
	}
	doc.MustHave(`input[value="/later/?tab=unread&q=archived"]`)
}

func TestSearchShowsWhereItMatched(t *testing.T) {
	s := newServer(t)
	a := seedHello(t, s)
	quote := string([]rune(a.ContentText)[0:5])
	if _, err := s.Store.AddHighlight(context.Background(), a.ID, a.ContentText, 0, 5, quote, "giraffe thoughts"); err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, "/later/?q=giraffe")
	if got := htmlassert.Text(doc.MustHave(".later-snippet-in")); got != "In a highlight:" {
		t.Errorf("label = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".later-row-snippet mark")); got != "giraffe" {
		t.Errorf("marked = %q, want giraffe", got)
	}
}

func TestSearchHTMXAnswersTheListOnly(t *testing.T) {
	s := newServer(t)
	seedStates(t, s)
	req := httptest.NewRequest("GET", "/later/?tab=unread&q=one", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "later-list")
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("fragment is a whole page")
	}
	frag := htmlassert.Parse(t, rec.Body.String())
	frag.MustHave("div#later-list")
	frag.MustNotHave(".later-save")
	if n := len(frag.QueryAll(".later-row")); n != 3 {
		t.Errorf("fragment has %d rows, want 3", n)
	}
}

func TestSearchBoxKeepsTheQueryTabAndTag(t *testing.T) {
	s := newServer(t)
	seedTagged(t, s, "https://a.example/1", "Essay", "essays")
	doc := s.Get(t, s.Alice, "/later/?tab=archived&tag=essays&q=ess")
	if got := attr(t, doc, "input#later-q", "value"); got != "ess" {
		t.Errorf("search box = %q", got)
	}
	if got := attr(t, doc, "input#later-q", "hx-get"); got != "/later/" {
		t.Errorf("hx-get = %q", got)
	}
	if got := attr(t, doc, "input#later-q", "hx-target"); got != "#later-list" {
		t.Errorf("hx-target = %q", got)
	}
	doc.MustHave(`.later-search input[value="archived"]`)
	doc.MustHave(`.later-search input[value="essays"]`)
}

func TestSearchNarrowsByTag(t *testing.T) {
	s := newServer(t)
	seedTagged(t, s, "https://a.example/1", "Pomelo One", "fruit")
	seedTagged(t, s, "https://a.example/2", "Pomelo Two")
	doc := s.Get(t, s.Alice, "/later/?tab=unread&tag=fruit&q=pomelo")
	if rows := doc.QueryAll(".later-row"); len(rows) != 1 {
		t.Errorf("rows = %q, want just the tagged one", texts(rows))
	}
}

func TestBlankSearchShowsTheTab(t *testing.T) {
	s := newServer(t)
	seedStates(t, s)
	doc := s.Get(t, s.Alice, "/later/?tab=unread&q=++")
	doc.MustHave(".later-tabs")
	doc.MustNotHave(".later-search-head")
}

func TestSearchWithNoMatches(t *testing.T) {
	s := newServer(t)
	seedStates(t, s)
	doc := s.Get(t, s.Alice, "/later/?q=zzz")
	if got := htmlassert.Text(doc.MustHave(".later-empty")); got != "Nothing matches “zzz”." {
		t.Errorf("empty text = %q", got)
	}
}

func TestSearchLoadMoreKeepsTheQuery(t *testing.T) {
	s := newServer(t)
	for i := 0; i < 51; i++ {
		seedTagged(t, s, fmt.Sprintf("https://p.example/%d", i), fmt.Sprintf("Mango %d", i))
	}
	doc := s.Get(t, s.Alice, "/later/?q=mango")
	if got, _ := htmlassert.Attr(doc.MustHave(".later-more button"), "hx-get"); got != "/later/?tab=unread&q=mango&offset=50" {
		t.Errorf("hx-get = %q", got)
	}
}
```

Add the `net/http/httptest` import for `TestSearchHTMXAnswersTheListOnly`.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/later/ -run 'Search|Snippet' -count=1`
Expected: FAIL / build failure — no `snippetParts`, no search box.

- [ ] **Step 3: `snippet.go`**

Create `internal/apps/later/snippet.go`:

```go
package later

import "strings"

// snippetPart is a run of a search snippet; Hit runs are drawn as <mark>.
// Rendering parts, rather than converting the snippet to template.HTML,
// keeps every character escaped by html/template.
type snippetPart struct {
	Text string
	Hit  bool
}

// snippetParts splits a Search snippet at its SnippetOpen/SnippetClose
// markers, collapsing whitespace (the highlights column separates entries
// with newlines). An unclosed marker runs to the end.
func snippetParts(s string) []snippetPart {
	s = strings.Join(strings.Fields(s), " ")
	var parts []snippetPart
	for s != "" {
		i := strings.Index(s, SnippetOpen)
		if i < 0 {
			parts = append(parts, snippetPart{Text: s})
			break
		}
		if i > 0 {
			parts = append(parts, snippetPart{Text: s[:i]})
		}
		s = s[i+len(SnippetOpen):]
		j := strings.Index(s, SnippetClose)
		if j < 0 {
			j = len(s)
		}
		if j > 0 {
			parts = append(parts, snippetPart{Text: s[:j], Hit: true})
		}
		s = s[min(len(s), j+len(SnippetClose)):]
	}
	return parts
}

// snippetView is the line under a search result saying where it matched.
type snippetView struct {
	In    string
	Parts []snippetPart
}

var matchLabels = map[MatchIn]string{
	MatchHighlight: "In a highlight:",
	MatchNote:      "In your note:",
	MatchText:      "In the text:",
}

// newSnippet is h's snippet line, or nil for a title-only match.
func newSnippet(h SearchHit) *snippetView {
	if h.In == MatchTitle {
		return nil
	}
	return &snippetView{In: matchLabels[h.In], Parts: snippetParts(h.Snippet)}
}
```

- [ ] **Step 4: Searching in `list.go`**

`listQuery` gains the search text, and `url` adds it between tag and
offset:

```go
type listQuery struct {
	Tab State
	Tag string // "" means every tag
	Q   string // the search box as typed (spec: "Search: FTS5 across all states")
}

// searching is whether the page shows search results instead of a tab.
func (q listQuery) searching() bool { return strings.TrimSpace(q.Q) != "" }

// url is the list page for q. Parameters come in a fixed order (tab, tag,
// q, offset) so links are stable and tests can pin them.
func (q listQuery) url(offset int) string {
	var b strings.Builder
	b.WriteString("/later/?tab=" + string(q.Tab))
	if q.Tag != "" {
		b.WriteString("&tag=" + url.QueryEscape(q.Tag))
	}
	if q.searching() {
		b.WriteString("&q=" + url.QueryEscape(strings.TrimSpace(q.Q)))
	}
	if offset > 0 {
		fmt.Fprintf(&b, "&offset=%d", offset)
	}
	return b.String()
}

func parseListQuery(v url.Values) listQuery {
	return listQuery{Tab: parseTab(v.Get("tab")), Tag: tagParam(v.Get("tag")), Q: v.Get("q")}
}
```

`rowView` gains:

```go
	StateLabel string       // shown on search results, which mix states
	Snippet    *snippetView // search results only; nil for a title match
```

`indexView` gains:

```go
	Q         string // the search box's value
	Searching bool
	ClearURL  string // the tab the search was started from
```

Add a lookup for the state pill:

```go
// stateLabel is the tab name of s, for a search result's state pill.
func stateLabel(s State) string {
	for _, t := range tabs {
		if t.state == s {
			return t.label
		}
	}
	return string(s)
}
```

Replace `renderListPage` with:

```go
// renderListPage draws the list page: a tab, or search results when the
// search box has a word in it. HTMX gets just the rows for Load more, or
// just #later-list for the search box.
func (a *App) renderListPage(w http.ResponseWriter, r *http.Request, userID int64, q listQuery, offset, status int, form saveForm, saved *savedView) {
	ctx := r.Context()
	names, err := a.store.TagNames(ctx, userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	view := indexView{
		Tab: q.Tab, Tag: q.Tag, Q: q.Q, Searching: q.searching(),
		Chips: tagChips(q, names), Back: q.url(0), Form: form, Saved: saved,
	}
	var rows []rowView
	if view.Searching {
		hits, err := a.store.Search(ctx, userID, q.Q, q.Tag, offset, pageSize+1)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		for _, h := range hits {
			row := newRow(h.ListItem)
			row.StateLabel = stateLabel(h.State)
			row.Snippet = newSnippet(h)
			rows = append(rows, row)
		}
		view.ClearURL = listQuery{Tab: q.Tab, Tag: q.Tag}.url(0)
		view.EmptyText = fmt.Sprintf("Nothing matches “%s”.", strings.TrimSpace(q.Q))
	} else {
		items, err := a.store.List(ctx, userID, q.Tab, q.Tag, offset, pageSize+1)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		counts, err := a.store.Counts(ctx, userID, q.Tag)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		for _, it := range items {
			rows = append(rows, newRow(it))
		}
		for _, t := range tabs {
			tq := listQuery{Tab: t.state, Tag: q.Tag}
			view.Tabs = append(view.Tabs, tabView{State: t.state, Label: t.label, Count: counts[t.state], Current: t.state == q.Tab, URL: tq.url(0)})
			if t.state == q.Tab {
				view.EmptyText = t.empty
			}
		}
		if q.Tag != "" {
			view.EmptyText = fmt.Sprintf("Nothing tagged “%s” here.", q.Tag)
		}
	}
	if len(rows) > pageSize {
		rows = rows[:pageSize]
		view.NextURL = q.url(offset + pageSize)
	}
	view.Rows = rows

	page := a.deps.Page(r, "ON Later")
	page.Data = view
	block := ""
	if web.IsHTMX(r) && !web.IsHTMXHistoryRestore(r) {
		switch {
		case offset > 0:
			block = "rows"
		case web.HTMXTarget(r) == "later-list":
			block = "later-list"
		}
	}
	if block != "" {
		if err := a.deps.Render.Fragment(w, status, "later/index", block, page); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	if err := a.deps.Render.Page(w, status, "later/index", page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
```

- [ ] **Step 5: `index.html`**

After the bookmarklet hint, replace the tabs, chips, rows and empty text
with the search box and the `later-list` block (the confirm dialog stays
where it is):

```html
	{{/* The live filter, as in ON Notes and ON Reader: hx-replace-url so
	     typing doesn't push one history entry per keystroke, and "search"
	     fires at once when the native clear (x) button is used. */}}
	<form class="later-search" method="get" action="/later/" role="search">
		<input type="hidden" name="tab" value="{{.Data.Tab}}">
		{{with .Data.Tag}}<input type="hidden" name="tag" value="{{.}}">{{end}}
		<label class="visually-hidden" for="later-q">Search articles</label>
		<input type="search" id="later-q" name="q" value="{{.Data.Q}}" placeholder="Search all articles…"
		       hx-get="/later/" hx-target="#later-list" hx-swap="outerHTML"
		       hx-include="closest form" hx-trigger="input changed delay:300ms, search"
		       hx-replace-url="true">
	</form>

	{{template "later-list" .}}
```

Add the block (after the `content` block):

```html
{{define "later-list"}}
<div id="later-list" class="later-list stack">
	{{if .Data.Searching}}
	<p class="later-search-head">Results from all lists{{with .Data.Tag}} tagged “{{.}}”{{end}} <a href="{{.Data.ClearURL}}">Clear search</a></p>
	{{else}}
	<nav class="later-tabs" aria-label="Lists">
		{{range .Data.Tabs}}
		<a class="later-tab" href="{{.URL}}"{{if .Current}} aria-current="page"{{end}}>{{.Label}} <span class="later-tab-count">{{.Count}}</span></a>
		{{end}}
	</nav>
	{{end}}
	{{if .Data.Chips}}
	<nav class="later-tags" aria-label="Tags">
		{{range .Data.Chips}}<a class="later-tag-chip" href="{{.URL}}"{{if .Current}} aria-current="true"{{end}}>{{.Name}}</a>
		{{end}}
	</nav>
	{{end}}
	{{if .Data.Rows}}
	<ul class="later-rows">{{template "rows" .}}</ul>
	{{else}}
	<p class="later-empty">{{.Data.EmptyText}}</p>
	{{end}}
</div>
{{end}}
```

In the `rows` block's `.later-row-meta`, before the site span:

```html
				{{if $.Data.Searching}}<span class="later-pill later-pill-state">{{.StateLabel}}</span>{{end}}
```

and after the closing `</span>` of `.later-row-meta` (still inside
`.later-row-text`):

```html
			{{with .Snippet}}<span class="later-row-snippet"><span class="later-snippet-in">{{.In}}</span> {{range .Parts}}{{if .Hit}}<mark>{{.Text}}</mark>{{else}}{{.Text}}{{end}}{{end}}</span>{{end}}
```

- [ ] **Step 6: Styles**

```css
.later-search input[type="search"] { width: 100%; }
.later-search-head { display: flex; flex-wrap: wrap; gap: .75rem; align-items: baseline; margin: 0; color: var(--c-text-dim); font-size: var(--fs-sm); }
.later-row-snippet { display: block; margin-top: .15rem; font-size: var(--fs-sm); color: var(--c-text-dim); overflow-wrap: anywhere; }
.later-row-snippet mark { background: var(--c-accent-attention-bg); color: inherit; border-radius: 2px; }
.later-snippet-in { color: var(--c-text-faint); }
```

- [ ] **Step 7: User guide**

In `docs/user/later.md`, after the "Tags" section, add:

```markdown
## Searching

Type in **Search all articles…** above the list. Results appear as you
type, from **Unread**, **Reading** and **Archived** together, best match
first. ON Later searches titles, article text, your highlights and their
comments, and your notes. Under each result a line shows where it matched —
*In a highlight*, *In your note* or *In the text* — with the words you
searched for marked.

A selected tag narrows the search too. Click **Clear search**, or empty the
box, to go back to the list you were on.
```

- [ ] **Step 8: Run the tests**

Run: `go test ./internal/apps/later/ -count=1`
Expected: PASS.

- [ ] **Step 9: Full check and commit**

```bash
git add internal/apps/later internal/ui/static/app.css docs/user/later.md
git commit -m "feat(later): live search across all articles with match snippets"
```

---

## After the last task

- Run the app (`.claude/launch.json` / the `run` skill) and check in the
  browser, light and dark, desktop and phone width: save with tags; chips
  filter and clear; tab counts follow the tag; edit tags from a row menu
  and from the reading view; reading-view tag links; type in the search
  box and watch results replace the tabs, with snippets and state pills;
  clear the search; Load more during a search; the bookmarklet popup's
  tags field.
- PR: `feat(later): ON Later L3 — tags and search (#484)`, body
  `Closes #484`, listing the "Decisions made in this plan" above and the
  dropped favourites. Edit #484's scope line to say favourites were left
  out by design (spec "Decisions at a glance").
