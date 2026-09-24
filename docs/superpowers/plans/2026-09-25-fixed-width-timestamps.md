# Fixed-width timestamps (repo-wide) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Store every timestamp as fixed-width text, so that SQL's text comparisons (`ORDER BY`, `<`, `<=`, `min()`, `max()`) agree with time order, including within one second. Today they don't: `time.RFC3339Nano` trims trailing fractional zeros, and `'Z'` sorts after every digit, so `…12.244Z` sorts after `…12.244124Z`. That causes the ~1/1000 flake in `TestQueueFrontAgreesWithDueQueue` and real mis-ordering: imported and gifted Flash cards queue out of order, and `due_at <= ?`, `next_fetch_at <= ?` and `expires_at <= ?` misjudge sub-second boundaries.

**Architecture:**
- **One shared helper.** New `internal/platform/db/timefmt.go` holds `db.TimeLayout = "2006-01-02T15:04:05.000000000Z07:00"`, `db.FormatTime(t)` (always UTC, always 30 characters) and `db.ParseTime(s)`. `ParseTime` uses `time.RFC3339Nano`, which accepts a fraction of any length (0–9 digits). So it reads new rows, legacy rows, and any row a migration skipped.
- **Every writer switches to the helper.** Each package keeps its own `formatTime`/`parseTime`, so no call site changes: auth, flash, notes, paste and reader. The bodies now call `db.FormatTime`/`db.ParseTime`, and each package keeps its own error prefix (reader keeps its zero-on-error `parseTime`). `db.Apply` records `applied_at` with `FormatTime`. Nothing else writes a stored timestamp (see the verified table).
- **Existing rows are rewritten in SQL.** Each namespace gets one forward-only migration: platform `0002`, flash `0012`, notes `0007`, paste `0002` and reader `0010`. Each one pads every timestamp column in place, using `substr`/`length`/`GLOB` string surgery. The framework has no Go-side migration hook: `db.Migration` is only `SQL`, run by `tx.ExecContext` (`migrate.go:16-21`, `:136-157`). `strftime('%f')` keeps only milliseconds, so it would lose data. The rewrite is idempotent, leaves NULLs and unexpected values alone, and a property test checks it against `db.FormatTime` over 2,000 random values.
- **User-visible text stays as it is.** Display code parses first, so the UI is unaffected. Two places show raw stored strings: the Reader backup (`export.go`) and the admin page's migration table (`collect.go`). Both now re-render the stored value as RFC3339Nano, so their output is byte-identical to today's. A prototype run confirmed this: an `onsuite export` of a real dev database matched exactly before and after.
- **Workaround removed.** Notes' `Archive` goes back to a plain `ORDER BY n.archived_at DESC`, dropping `julianday()` (issue #108's workaround). `julianday` resolves only milliseconds, so it tied two archives a few µs apart. The prototype proved this: with fixed width, `julianday` still failed the new test.
- **The rule is enforced.** An arch test allows `time.RFC3339`/`time.RFC3339Nano` in production code in only three files. AGENTS.md and PATTERNS.md record the rule.

**Tech Stack:** Go 1.26, SQLite via `modernc.org/sqlite` v1.59.0 (SQLite 3.53.4), no new dependencies.

**Issue:** [#356](https://github.com/iliafrenkel/on-suite/issues/356), including its 2026-09-25 root-cause comment.

**Decisions already made (by the user):**
1. Fix it **repo-wide in one PR**: one shared format and parse helper in `internal/platform`, and every writer switched to it.
2. `ParseTime` accepts both fixed-width and legacy RFC3339Nano strings.
3. One migration per namespace rewrites every existing timestamp column. Date-only columns stay as they are.
4. Export formats that are not stored-and-compared stay unchanged.
5. Tests: helper unit tests, per-app migration tests with legacy seeds, and a deterministic reproduction of the flake. Clock-dependent tests stay at store level, because handler tests' `s.Store.SetClock` doesn't reach an app's own store.
6. Record the rule in AGENTS.md. Add an arch test if it is cheap and robust.

**Choices this plan makes (flag in the PR, easy to change):**
- **Helper in `internal/platform/db`,** as the user suggested. It's the storage layer. The arch test lets `auth` import `db` (only web/app/render are forbidden, `arch_test.go:150`). `db` imports nothing from the module, so there is no cycle. Apps may import any `internal/platform/*`.
- **`TimeLayout` ends `Z07:00`,** not a literal `Z`. `FormatTime` always converts to UTC first, so the output is identical either way. The layout still describes a real RFC 3339 value.
- **The rewrite happens in SQL, one `UPDATE` per column.** It is always the same 9-line expression; `migrations/0002` explains it once and every app's file points there. Two safeguards back it:
  - I checked it through the real driver (modernc v1.59.0) on 20,000 random values: 0 mismatches against `FormatTime`, and text order equals time order. A second run touches 0 rows, because the WHERE clause needs 20–29 characters.
  - The `TestFixedWidthMigrationMatchesFormatTime` property test now pins the expression.
- **FTS triggers fire during the rewrite.** Both `notes_fts_au` and `reader_items_fts_au` are `AFTER UPDATE` with no column list, so each rewritten row is re-indexed. That is redundant but correct. I measured about **1.0s per 20,000 Reader articles** of 5KB each, for all three `reader_items` columns. That is too cheap to justify dropping and recreating the triggers, which would copy their bodies into a new file.
- **Reader export and admin migration table: text unchanged** (see Architecture). The alternative is to let them show the 30-character form. It is still valid RFC 3339, so this is a product choice.
- **Reader test fixtures that write timestamps directly** (17 lines in 8 files) and `cmd/onsuite/backup_test.go` switch to `db.FormatTime`. They pass either way, because their values are hours apart. Switching them keeps test data in the production format, which is a cheap consistency fix. The arch test covers production code only, so tests can still build legacy values on purpose.
- **Share ordering is not touched.** The issue's first comment suggests `ORDER BY id DESC` for `SharesForDeck`/`SharesForRecipient`. With fixed width, the same-second part is fixed. A clock step backwards is a different bug, so it's left as a follow-up.

## Verified against current code (branch `fix/356-fixed-width-timestamps` @ `77eab10`)

| What | Where |
|---|---|
| migration framework is SQL-only: `Migration{Namespace, ID, Name, SQL}`; `applyOne` runs `tx.ExecContext(m.SQL)` then records `applied_at` in the same tx; no Go hook | `internal/platform/db/migrate.go:16-21`, `:136-157` |
| `applied_at` written as `time.Now().UTC().Format(time.RFC3339Nano)` | `migrate.go:150` |
| `schema_migrations` created by `Apply` itself (`CREATE TABLE IF NOT EXISTS`) before any migration | `migrate.go:79-95` |
| platform schema is namespace `"platform"`, one migration `0001_identity.sql`; applied before every app's (`cmd/onsuite/database.go:35-46`) | `internal/platform/auth/store.go:17`; `internal/platform/auth/migrations/` |
| auth `formatTime`/`parseTime` (RFC3339Nano) | `internal/platform/auth/store.go:226-236` |
| auth writers/readers: `CreateUser` `:99`, `ListAccounts` `:155`, `SessionCounts` `:198`; `CreateSession` `session.go:50`; renew `:98`; `DeleteExpiredSessions` `expires_at <= ?` `:134`; `SetClock` `store.go:66` | `internal/platform/auth/store.go`, `session.go` |
| layering: `auth` may import `db` (forbidden only web/app/render); `db` forbidden auth/web/app/render | `internal/arch/arch_test.go:150-151` |
| flash `formatTime`/`parseTime` (RFC3339Nano); `SetClock` | `internal/apps/flash/store.go:76-86`; `:58` |
| flash orderings on timestamps: new cards `c.created_at ASC` `review.go:614`; reviews `s.due_at ASC` `:594`; decks `created_at DESC` `deck.go:158`; cards `card.go:150`; shares `share.go:519-523`, `:577`; `min(due_at)` `deck_summary.go:129`, `:156` | `internal/apps/flash/*.go` |
| flash stamps each card with `st.now()` inside one tx (import, share copy) | `import_store.go:20`, `:64`; `share.go:257`, `:294` |
| `formatDay` comment names RFC3339Nano | `internal/apps/flash/review.go:12-15` |
| notes `formatTime` in tree.go; `parseTime` in store.go; `SetClock` (copied into `Ops` at `Do` time, `tree.go:24-28`) | `internal/apps/notes/tree.go:198`; `store.go:178-186`; `store.go:24` |
| notes `Archive` orders by `julianday(n.archived_at) DESC` with an issue-#108 comment | `internal/apps/notes/archive.go:56-63`, `:84` |
| #108 test seeds literal legacy strings directly into `archived_at` | `internal/apps/notes/archive_test.go:106-136` |
| paste `formatTime`/`parseTime`; `List` `ORDER BY created_at DESC, id DESC`; `SetClock` | `internal/apps/paste/store.go:379-389`; `:228`; `:126` |
| reader `timeFmt`/`formatTime`/`parseTime` (zero on error); no store clock — methods take `now`, `Subscribe` uses `time.Now()` | `internal/apps/reader/store.go:148-160`; `:202` |
| reader comparisons: `DueFeeds` `next_fetch_at <= ?` `store.go:832-839`; items `ORDER BY i.published_at DESC` `:623`; stats `date(fetched_at)`, `date(read_at)` `stats.go:75`, `:81`, `:115-125`; `max(last_fetch_at)` `:203`; `max(published_at)` `:298` | `internal/apps/reader/` |
| SQLite `date()`/`julianday()` accept a 9-digit fraction + `Z` (checked on modernc: `date('…T23:59:59.999999999Z')` = same day) | driver check |
| reader backup copies **raw** `published_at`/`starred_at` strings into JSON | `internal/apps/reader/export.go:72-91` |
| admin page shows **raw** `applied_at` (`MigrationInfo.AppliedAt string`) | `internal/platform/admin/collect.go:58-62`, `:91-105`; `internal/ui/templates/admin.html:49` |
| other exports marshal `time.Time` (unaffected): notes, paste, `onsuite export`'s `exported_at` | `internal/apps/notes/export.go:159-160`; `internal/apps/paste/export.go:21`; `cmd/onsuite/export.go:21`, `:68` |
| snapshot filenames use `"20060102T150405Z"` (a filename, not stored/compared) | `cmd/onsuite/backup.go:88` |
| **no other stored-timestamp writer**: every `RFC3339`/`.Format(` in non-test Go is one of the above or a display/day format (`"2006-01-02"`, `"2 Jan"`) | `grep -rn --include='*.go' -E 'RFC3339\|\.Format\(\|strftime\|datetime\(\|julianday' . \| grep -v _test.go` |
| no SQL default writes a timestamp (`DEFAULT` clauses are all non-time) and no `strftime`/`datetime('now')` anywhere | every file under `internal/**/migrations/` |
| date-only columns (already fixed width, left alone): `flash_review_counts.day`, `notes_nodes.due_on`, `reader_daily_stats.day` | `flash/migrations/0004_review_state.sql:124-132`; `notes/migrations/0002_done_due.sql:295`; `reader/migrations/0007_stats.sql:583` |
| FTS update triggers with no column list (fire on any UPDATE) | `notes/migrations/0003_search.sql:322-325`; `reader/migrations/0006_search.sql:565-570` |
| latest migration per namespace: platform `0001`, flash `0011`, notes `0006`, paste `0001`, reader `0009`; no open PRs (`gh pr list`) that add migrations | `ls internal/**/migrations` |
| tests writing `…Format(time.RFC3339Nano)` into the DB: reader `poll_test.go:210`, `handlers_test.go:1465,1823,2797`, `extract_store_test.go:174`, `retention_test.go:27,105`, `faviconproxy_test.go:104`, `images_store_test.go:99,153`, `imgproxy_test.go:63`, `stats_test.go:28,81,115,150,236,297`; `cmd/onsuite/backup_test.go:102` | — |
| no test asserts on a stored timestamp's exact text (other than the #108 test above) | `grep -rn 'RFC3339\|T[0-9][0-9]:[0-9][0-9]' --include='*_test.go'` |
| test helpers reused: flash `newFixture` (`f.store`, `f.db`, `f.alice`, `f.bob`) `deck_test.go:28`, `qDeck`/`qCard` `review_test.go:764`, `:773`; notes `newFixture` `store_test.go:29`, `f.mk` `tree_test.go:19`; paste `newFixture` `store_test.go:27`; reader `newStoreFixture` (`f.store`, `f.db`) `store_test.go:26`; auth `newStore` `store_test.go:18`; db `open` `db_test.go:17` | — |
| handler-harness clock gotcha | `internal/apps/flash/handlers_review_test.go:419` |
| Go: `time.Parse(time.RFC3339Nano, …)` accepts 0–9 fraction digits (so the new layout too); `time.Parse(TimeLayout, "…05.244Z")` **fails**, so `ParseTime` must use RFC3339Nano | checked with go1.27 |
| upgrade/rollback doc: snapshot first with the old binary; restore to roll back | `docs/DEPLOYING.md:100-121` |

### Timestamp column inventory (what each migration rewrites)

| Namespace (migration) | Table | Columns (NULLable in *italics*) |
|---|---|---|
| platform (`0002`) | `users` | `created_at` |
| | `sessions` | `created_at`, `expires_at` |
| | `schema_migrations` | `applied_at` |
| flash (`0012`) | `flash_decks` | `created_at`, *`snoozed_until`* |
| | `flash_cards` | `created_at` |
| | `flash_card_state` | `due_at`, *`last_review_at`*, *`log_due`*, *`log_review`* |
| | `flash_media` | *`fetched_at`* |
| | `flash_shares` | `created_at`, *`responded_at`* |
| notes (`0007`) | `notes_nodes` | `created_at`, `updated_at`, *`done_at`*, *`archived_at`* |
| paste (`0002`) | `paste_snippets` | `created_at` |
| reader (`0010`) | `reader_feeds` | *`last_fetch_at`*, `next_fetch_at` |
| | `reader_subs` | `added_at` |
| | `reader_items` | `published_at`, `fetched_at`, *`full_fetched_at`* |
| | `reader_images` | *`fetched_at`* |
| | `reader_item_state` | *`read_at`*, *`starred_at`* |
| | `reader_feed_icons` | *`fetched_at`* |

The table lists 29 columns. They are **not** rewritten: `flash_review_counts.day`, `notes_nodes.due_on` and `reader_daily_stats.day`, which are `YYYY-MM-DD` dates, and `reader_feeds.last_modified`, an HTTP header string that is echoed back to the server and never compared.

## Global Constraints

- Branch: `fix/356-fixed-width-timestamps` (already created off `main` at `77eab10`). Never push to `main`. Open a PR at the end.
- Full check before each Go commit:
  ```bash
  gofmt -l .                                              # must print nothing
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...  # pinned, not @latest
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./internal/arch/... -count=1
  go test ./... -race -count=1
  ```
- **No new dependencies.** Only `internal/platform/db`, `database/sql` and the stdlib.
- **Behaviour is otherwise unchanged.** No route, template, CSS, JS or HTMX changes. User-visible time text is unchanged: every UI label is built from a parsed `time.Time`, and the Reader backup and admin migration table keep their exact text (Tasks 2 and 7). Sort order changes only for rows that fall within one second of each other, and that is the fix.
- **Migrations are forward-only.** Start each new file with a `-- internal/…/migrations/…` path comment and explain *why*, as `flash/migrations/0010_deck_color.sql` does. Only `UPDATE`s: no schema change. **Don't edit an applied migration**, not even stale comments such as `auth/migrations/0001_identity.sql:24` or `flash/migrations/0004_review_state.sql:47`.
- **Keep names:** each package's `formatTime`/`parseTime`, with the same signatures and the same error prefixes (`auth:`, `flash:`, `notes:`, `paste:`). Reader's `parseTime` still returns the zero time on error.
- **No handler test relies on `s.Store.SetClock`** (`flash/handlers_review_test.go:419`). Every clock-dependent test in this plan works at store level.
- Commits: Conventional Commits, with scope `db`/`platform`/`auth`/`flash`/`notes`/`paste`/`reader` and `#356` in the subject. Use `fix`/`test`/`docs`, not `feat`.

---

## File map

| File | Change |
|---|---|
| `internal/platform/db/timefmt.go` | **Create**: `TimeLayout`, `FormatTime`, `ParseTime` |
| `internal/platform/db/timefmt_test.go` | **Create**: format, round-trip, legacy parse, ordering property, `applied_at` |
| `internal/platform/db/migrate.go` | `applied_at` via `FormatTime` |
| `internal/platform/admin/format.go`, `collect.go` | `appliedAtLabel`; migration table keeps its text |
| `internal/platform/admin/format_test.go` | **Create** (`package admin`) |
| `internal/platform/auth/store.go` | `formatTime`/`parseTime` delegate to `db` |
| `internal/platform/auth/migrations/0002_fixed_width_timestamps.sql` | **Create**: the canonical rewrite (users, sessions, schema_migrations) |
| `internal/platform/auth/timestamps_test.go` | **Create** (`package auth`): migration + property + idempotency + sweep regression |
| `cmd/onsuite/backup_test.go` | expired-session fixture via `db.FormatTime` |
| `internal/apps/flash/store.go`, `review.go` | helpers delegate; comment |
| `internal/apps/flash/migrations/0012_fixed_width_timestamps.sql` | **Create** (10 columns) |
| `internal/apps/flash/timestamps_test.go` | **Create**: migration + 4 regressions (queue, decks, import, due) |
| `internal/apps/notes/store.go`, `tree.go`, `archive.go` | helpers move to store.go and delegate; plain `ORDER BY archived_at` |
| `internal/apps/notes/migrations/0007_fixed_width_timestamps.sql` | **Create** (4 columns) |
| `internal/apps/notes/timestamps_test.go` | **Create**: migration (+ FTS survives) |
| `internal/apps/notes/archive_test.go` | #108 test goes through the store clock, adds a 4µs case |
| `internal/apps/paste/store.go` | helpers delegate |
| `internal/apps/paste/migrations/0002_fixed_width_timestamps.sql` | **Create** (1 column) |
| `internal/apps/paste/timestamps_test.go` | **Create**: migration + list regression |
| `internal/apps/reader/store.go`, `export.go` | helpers delegate; `exportTime` keeps backup text |
| `internal/apps/reader/migrations/0010_fixed_width_timestamps.sql` | **Create** (10 columns) |
| `internal/apps/reader/timestamps_test.go` | **Create**: migration + due-feed regression + export text |
| 8 reader `*_test.go` files | 17 fixture writes via `db.FormatTime` |
| `internal/arch/arch_test.go` | `TestRFC3339IsContained` |
| `AGENTS.md`, `PATTERNS.md` | storage rule |

---

### Task 1: The shared helper, and `applied_at` through it (db)

**Files:**
- Create: `internal/platform/db/timefmt.go`
- Modify: `internal/platform/db/migrate.go:150`
- Test: `internal/platform/db/timefmt_test.go` (new, `package db`)

**Interfaces:**
- Consumes: `open(t) (*sql.DB, string)` (`db_test.go:17`), `Apply`, `Migration`.
- Produces:
  ```go
  const TimeLayout = "2006-01-02T15:04:05.000000000Z07:00"
  func FormatTime(t time.Time) string            // UTC, 30 characters, always ends "Z"
  func ParseTime(s string) (time.Time, error)    // UTC; accepts TimeLayout and legacy RFC3339Nano; returns time.Parse's error unwrapped
  ```

- [ ] **Step 1: Write the failing tests**

Create `internal/platform/db/timefmt_test.go`:

```go
package db

import (
	"context"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestFormatTimeIsFixedWidthUTC(t *testing.T) {
	melbourne := time.FixedZone("AEST", 10*60*60)
	cases := []struct {
		in   time.Time
		want string
	}{
		{time.Date(2026, 9, 25, 12, 0, 5, 0, time.UTC), "2026-09-25T12:00:05.000000000Z"},
		{time.Date(2026, 9, 25, 12, 0, 5, 500_000_000, time.UTC), "2026-09-25T12:00:05.500000000Z"},
		{time.Date(2026, 9, 25, 12, 0, 5, 244_000_000, time.UTC), "2026-09-25T12:00:05.244000000Z"},
		{time.Date(2026, 9, 25, 12, 0, 5, 244_124_000, time.UTC), "2026-09-25T12:00:05.244124000Z"},
		{time.Date(2026, 9, 25, 12, 0, 5, 123_456_789, time.UTC), "2026-09-25T12:00:05.123456789Z"},
		// Not UTC on the way in: stored as the same instant in UTC.
		{time.Date(2026, 9, 25, 22, 0, 5, 1, melbourne), "2026-09-25T12:00:05.000000001Z"},
	}
	for _, tc := range cases {
		got := FormatTime(tc.in)
		if got != tc.want {
			t.Errorf("FormatTime(%v) = %q, want %q", tc.in, got, tc.want)
		}
		if len(got) != 30 {
			t.Errorf("FormatTime(%v) is %d characters, want 30", tc.in, len(got))
		}
	}
}

func TestParseTimeRoundTripsFormatTime(t *testing.T) {
	r := rand.New(rand.NewPCG(356, 1))
	base := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	for range 1000 {
		in := base.Add(time.Duration(r.Int64N(int64(365 * 24 * time.Hour))))
		got, err := ParseTime(FormatTime(in))
		if err != nil {
			t.Fatalf("ParseTime(FormatTime(%v)): %v", in, err)
		}
		if !got.Equal(in) || got.Location() != time.UTC {
			t.Fatalf("round trip of %v = %v, want the same instant in UTC", in, got)
		}
	}
}

// TestParseTimeAcceptsLegacyRFC3339Nano pins that rows written before #356
// (and any a migration somehow missed) still read back as the right
// instant: time.Parse accepts any fraction length after the seconds.
func TestParseTimeAcceptsLegacyRFC3339Nano(t *testing.T) {
	cases := map[string]time.Time{
		"2026-09-25T12:00:05Z":           time.Date(2026, 9, 25, 12, 0, 5, 0, time.UTC),
		"2026-09-25T12:00:05.5Z":         time.Date(2026, 9, 25, 12, 0, 5, 500_000_000, time.UTC),
		"2026-09-25T12:00:05.244Z":       time.Date(2026, 9, 25, 12, 0, 5, 244_000_000, time.UTC),
		"2026-09-25T12:00:05.244124Z":    time.Date(2026, 9, 25, 12, 0, 5, 244_124_000, time.UTC),
		"2026-09-25T12:00:05.123456789Z": time.Date(2026, 9, 25, 12, 0, 5, 123_456_789, time.UTC),
		"2026-09-25T22:00:05+10:00":      time.Date(2026, 9, 25, 12, 0, 5, 0, time.UTC),
	}
	for in, want := range cases {
		got, err := ParseTime(in)
		if err != nil {
			t.Errorf("ParseTime(%q): %v", in, err)
			continue
		}
		if !got.Equal(want) || got.Location() != time.UTC {
			t.Errorf("ParseTime(%q) = %v, want %v in UTC", in, got, want)
		}
		if FormatTime(got) != FormatTime(want) {
			t.Errorf("ParseTime(%q) re-formats as %q, want %q", in, FormatTime(got), FormatTime(want))
		}
	}
}

func TestParseTimeRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "garbage", "2026-09-25", "2026-09-25 12:00:05"} {
		if _, err := ParseTime(in); err == nil {
			t.Errorf("ParseTime(%q) succeeded, want an error", in)
		}
	}
}

// TestFormatTimeSortsAsText is the property #356 is about: for times inside
// one second — where every flake and mis-ordering came from — byte order of
// the stored strings is time order. The first case is the exact pair from
// the issue, which RFC3339Nano got backwards.
func TestFormatTimeSortsAsText(t *testing.T) {
	a := time.Date(2026, 9, 25, 12, 0, 12, 244_000_000, time.UTC)
	b := time.Date(2026, 9, 25, 12, 0, 12, 244_124_000, time.UTC)
	if !(a.Format(time.RFC3339Nano) > b.Format(time.RFC3339Nano)) {
		t.Fatal("setup: RFC3339Nano no longer mis-orders this pair; the regression below proves nothing")
	}
	if !(FormatTime(a) < FormatTime(b)) {
		t.Errorf("FormatTime(%v) = %q does not sort before FormatTime(%v) = %q", a, FormatTime(a), b, FormatTime(b))
	}

	r := rand.New(rand.NewPCG(356, 2))
	second := time.Date(2026, 9, 25, 12, 0, 12, 0, time.UTC)
	for round := range 200 {
		times := make([]time.Time, 20)
		for i := range times {
			ns := r.Int64N(int64(time.Second))
			switch r.IntN(4) { // make whole, milli- and microsecond values common
			case 0:
				ns = 0
			case 1:
				ns -= ns % int64(time.Millisecond)
			case 2:
				ns -= ns % int64(time.Microsecond)
			}
			times[i] = second.Add(time.Duration(ns))
		}
		byText := slices.Clone(times)
		slices.SortFunc(byText, func(x, y time.Time) int { return strings.Compare(FormatTime(x), FormatTime(y)) })
		byTime := slices.Clone(times)
		slices.SortFunc(byTime, func(x, y time.Time) int { return x.Compare(y) })
		for i := range byText {
			if !byText[i].Equal(byTime[i]) {
				t.Fatalf("round %d: text order %v != time order %v", round, byText, byTime)
			}
		}
	}
}

func TestApplyRecordsAppliedAtInTimeLayout(t *testing.T) {
	handle, _ := open(t)
	ctx := context.Background()
	if _, err := Apply(ctx, handle, []Migration{
		{Namespace: "platform", ID: "0001", Name: "first", SQL: "CREATE TABLE a (id INTEGER PRIMARY KEY);"},
	}); err != nil {
		t.Fatal(err)
	}
	var appliedAt string
	if err := handle.QueryRow(
		"SELECT applied_at FROM schema_migrations WHERE key = 'platform:0001'").Scan(&appliedAt); err != nil {
		t.Fatal(err)
	}
	if len(appliedAt) != 30 || !strings.HasSuffix(appliedAt, "Z") {
		t.Errorf("applied_at = %q, want a 30-character TimeLayout value", appliedAt)
	}
	if _, err := time.Parse(TimeLayout, appliedAt); err != nil {
		t.Errorf("applied_at %q does not parse as TimeLayout: %v", appliedAt, err)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

```bash
go test ./internal/platform/db/ -count=1
```
Expected: a build failure: `undefined: FormatTime`, `undefined: ParseTime` and `undefined: TimeLayout`.

- [ ] **Step 3: Create `internal/platform/db/timefmt.go`**

```go
package db

import "time"

// TimeLayout is how every timestamp in the database is stored: UTC, with
// exactly nine fractional-second digits, so every value is 30 characters
// and text order is time order. SQLite has no time type, and ORDER BY, <,
// <=, min() and max() on these TEXT columns compare bytes.
//
// time.RFC3339Nano, which this suite used before #356, trims trailing
// fractional zeros: "…:05.244Z" is shorter than "…:05.244124Z", and since
// 'Z' sorts after every digit it compared as the later of the two although
// it is 124µs earlier. Date-only columns ("2006-01-02") are already fixed
// width and do not use this.
const TimeLayout = "2006-01-02T15:04:05.000000000Z07:00"

// FormatTime renders t for storage. It always converts to UTC first, so the
// zone is always the literal "Z" and the width is always 30.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

// ParseTime reads a stored timestamp back, in UTC. It accepts TimeLayout and
// also any RFC 3339 value with zero to nine fractional digits — the legacy
// RFC3339Nano strings every row held before #356's migrations rewrote them —
// because time.Parse accepts a fraction of any length after the seconds
// whatever the layout says. The error is time.Parse's own; callers wrap it
// with their package's prefix.
func ParseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
```

- [ ] **Step 4: Run again: only `applied_at` should fail now**

```bash
go test ./internal/platform/db/ -count=1
```
Expected: `--- FAIL: TestApplyRecordsAppliedAtInTimeLayout`, with a value such as `applied_at = "2026-09-25T…:00.08985Z", want a 30-character TimeLayout value`. Every other test passes.

- [ ] **Step 5: Record `applied_at` with `FormatTime`**

In `internal/platform/db/migrate.go`, replace:

```go
		m.Key(), m.Namespace, m.ID, m.Name,
		time.Now().UTC().Format(time.RFC3339Nano),
```

with:

```go
		m.Key(), m.Namespace, m.ID, m.Name,
		FormatTime(time.Now()),
```

(`time` is still imported by `migrate.go` for `time.Now`.)

- [ ] **Step 6: Run the package**

```bash
go test ./internal/platform/db/ -count=1
```
Expected: `ok  	github.com/iliafrenkel/on-suite/internal/platform/db`.

- [ ] **Step 7: Full check and commit**

Run the full check. Expected: green. Other packages don't use the helper yet.

```bash
git add internal/platform/db/timefmt.go internal/platform/db/timefmt_test.go internal/platform/db/migrate.go
git commit -m "fix(db): add fixed-width timestamp helpers, record applied_at with them (#356)"
```

---

### Task 2: Keep the admin page's `applied_at` text unchanged (platform)

After Task 1, new `schema_migrations` rows are 30 characters wide, and Task 3 rewrites the old rows. The admin table prints the raw column (`collect.go:101`, `admin.html:49`). Without this task it would start showing `…:39.801891000Z` where it shows `…:39.801891Z` today.

**Files:**
- Modify: `internal/platform/admin/format.go`, `internal/platform/admin/collect.go:98-104`
- Test: `internal/platform/admin/format_test.go` (new, `package admin`, the package's first internal test)

**Interfaces:**
- Consumes: `db.ParseTime`.
- Produces: `func appliedAtLabel(stored string) string` (unexported). `MigrationInfo.AppliedAt` stays a `string`.

- [ ] **Step 1: Write the failing test**

Create `internal/platform/admin/format_test.go`:

```go
package admin

import "testing"

func TestAppliedAtLabelKeepsThePagesTimestampText(t *testing.T) {
	cases := map[string]string{
		"2026-09-25T12:00:05.000000000Z": "2026-09-25T12:00:05Z",
		"2026-09-25T12:00:05.500000000Z": "2026-09-25T12:00:05.5Z",
		"2026-09-25T12:00:05.244124000Z": "2026-09-25T12:00:05.244124Z",
		"2026-09-25T12:00:05.244124Z":    "2026-09-25T12:00:05.244124Z", // legacy, as stored before #356
		"not a time":                     "not a time",
	}
	for stored, want := range cases {
		if got := appliedAtLabel(stored); got != want {
			t.Errorf("appliedAtLabel(%q) = %q, want %q", stored, got, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
go test ./internal/platform/admin/ -run AppliedAtLabel -count=1
```
Expected: a build failure: `undefined: appliedAtLabel`.

- [ ] **Step 3: Add `appliedAtLabel` to `format.go`**

Replace the import block:

```go
import (
	"fmt"
	"strconv"
)
```

with:

```go
import (
	"fmt"
	"strconv"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)
```

and append at the end of the file:

```go

// appliedAtLabel shows a migration's applied_at as this page always has —
// RFC 3339 with trailing fractional zeros trimmed — although the database
// stores db.TimeLayout since #356. A value that does not parse is shown as
// stored, rather than hiding the row.
func appliedAtLabel(stored string) string {
	t, err := db.ParseTime(stored)
	if err != nil {
		return stored
	}
	return t.Format(time.RFC3339Nano)
}
```

- [ ] **Step 4: Use it in `collect.go`**

Replace:

```go
	for rows.Next() {
		var m MigrationInfo
		if err := rows.Scan(&m.Key, &m.Name, &m.AppliedAt); err != nil {
			return info, fmt.Errorf("scan migration: %w", err)
		}
		info.Migrations = append(info.Migrations, m)
	}
```

with:

```go
	for rows.Next() {
		var m MigrationInfo
		var appliedAt string
		if err := rows.Scan(&m.Key, &m.Name, &appliedAt); err != nil {
			return info, fmt.Errorf("scan migration: %w", err)
		}
		m.AppliedAt = appliedAtLabel(appliedAt)
		info.Migrations = append(info.Migrations, m)
	}
```

The query's `ORDER BY applied_at, key` stays as it is. It is only correct within one second once Task 3 has rewritten the old rows.

- [ ] **Step 5: Run the package and the arch test**

```bash
go test ./internal/platform/admin/ ./internal/arch/ -count=1
```
Expected: `ok` for both packages. `admin` importing `db` breaks no layering rule.

- [ ] **Step 6: Full check and commit**

```bash
git add internal/platform/admin/format.go internal/platform/admin/collect.go internal/platform/admin/format_test.go
git commit -m "fix(platform): keep the admin page's applied_at text unchanged (#356)"
```

---

### Task 3: Platform tables (auth) — helpers and migration `platform:0002`

This migration is the **canonical** rewrite. Every app's migration repeats its `UPDATE` shape, one statement per column, and this task's property test proves the expression against `db.FormatTime`.

**Files:**
- Modify: `internal/platform/auth/store.go:226-236` (and its import block)
- Create: `internal/platform/auth/migrations/0002_fixed_width_timestamps.sql`
- Modify: `cmd/onsuite/backup_test.go:102`
- Test: `internal/platform/auth/timestamps_test.go` (new, `package auth`)

**Interfaces:**
- Consumes: `db.FormatTime`, `db.ParseTime`, `db.Open`, `db.Collect`, `db.Apply`, `Namespace`, `Migrations()`, `newStore(t)` (`store_test.go:18`), `Store.SetClock`, `CreateUser`, `CreateSession`, `DeleteExpiredSessions`, `UserByUsername`.
- Produces: the migration. The test helpers are `legacyTimes`, `openPlatformThrough`, `migrationSQL` and `wantRewritten`. None of them collides with an existing `package auth` test identifier (checked by compiling).

- [ ] **Step 1: Write the failing tests**

Create `internal/platform/auth/timestamps_test.go`:

```go
package auth

import (
	"context"
	"database/sql"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// legacyTimes are values as time.RFC3339Nano wrote them before #356, all
// inside one second, listed in time order. As text they sort in a
// different order ('Z' sorts after every digit), which is the bug.
var legacyTimes = []string{
	"2026-09-25T12:00:05Z",
	"2026-09-25T12:00:05.123456789Z",
	"2026-09-25T12:00:05.244Z",
	"2026-09-25T12:00:05.244124Z",
	"2026-09-25T12:00:05.5Z",
}

// openPlatformThrough migrates a fresh database only up to and including
// lastID, so a test can seed rows the way an older binary wrote them, and
// returns every platform migration for the test to finish applying.
func openPlatformThrough(t *testing.T, lastID string) (*sql.DB, []db.Migration) {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	ms, err := db.Collect(Namespace, Migrations())
	if err != nil {
		t.Fatal(err)
	}
	var early []db.Migration
	for _, m := range ms {
		if m.ID <= lastID {
			early = append(early, m)
		}
	}
	if _, err := db.Apply(context.Background(), handle, early); err != nil {
		t.Fatal(err)
	}
	return handle, ms
}

// migrationSQL returns the body of one platform migration.
func migrationSQL(t *testing.T, ms []db.Migration, id string) string {
	t.Helper()
	for _, m := range ms {
		if m.ID == id {
			return m.SQL
		}
	}
	t.Fatalf("no platform migration %s", id)
	return ""
}

func wantRewritten(t *testing.T, legacy string) string {
	t.Helper()
	parsed, err := db.ParseTime(legacy)
	if err != nil {
		t.Fatalf("setup: %q does not parse: %v", legacy, err)
	}
	return db.FormatTime(parsed)
}

func TestFixedWidthMigrationRewritesPlatformTimestamps(t *testing.T) {
	handle, ms := openPlatformThrough(t, "0001")
	ctx := context.Background()

	for i, ts := range legacyTimes {
		id := strconv.Itoa(i)
		if _, err := handle.ExecContext(ctx,
			`INSERT INTO users (id, username, username_fold, password_hash, created_at)
			 VALUES (?, ?, ?, 'x', ?)`, i+1, "u"+id, "u"+id, ts); err != nil {
			t.Fatal(err)
		}
		if _, err := handle.ExecContext(ctx,
			`INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
			"s"+id, i+1, ts, ts); err != nil {
			t.Fatal(err)
		}
		if _, err := handle.ExecContext(ctx,
			`INSERT INTO schema_migrations (key, namespace, id, name, applied_at)
			 VALUES (?, 'legacy', ?, 'seeded', ?)`, "legacy:"+id, id, ts); err != nil {
			t.Fatal(err)
		}
	}

	n, err := db.Apply(ctx, handle, ms)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if n != len(ms)-1 {
		t.Fatalf("applied %d migrations, want %d", n, len(ms)-1)
	}

	for i, ts := range legacyTimes {
		id := strconv.Itoa(i)
		want := wantRewritten(t, ts)
		for _, q := range []struct{ query, arg string }{
			{`SELECT created_at FROM users WHERE username = ?`, "u" + id},
			{`SELECT created_at FROM sessions WHERE id = ?`, "s" + id},
			{`SELECT expires_at FROM sessions WHERE id = ?`, "s" + id},
			{`SELECT applied_at FROM schema_migrations WHERE key = ?`, "legacy:" + id},
		} {
			var got string
			if err := handle.QueryRowContext(ctx, q.query, q.arg).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("%s [%s] = %q, want %q (from %q)", q.query, q.arg, got, want, ts)
			}
		}
	}

	// Text order is now time order, which legacyTimes is listed in.
	rows, err := handle.QueryContext(ctx, `SELECT username FROM users ORDER BY created_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var order []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			t.Fatal(err)
		}
		order = append(order, u)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"u0", "u1", "u2", "u3", "u4"}; !slices.Equal(order, want) {
		t.Errorf("users by created_at = %v, want %v", order, want)
	}

	// The store reads the rewritten rows.
	u, err := NewStore(handle).UserByUsername(ctx, "u2")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 25, 12, 0, 5, 244_000_000, time.UTC); !u.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", u.CreatedAt, want)
	}
}

// TestFixedWidthMigrationMatchesFormatTime runs the migration's SQL over
// many random RFC3339Nano values and checks each against db.FormatTime, so
// the padding expression — repeated in every app's own 0002/0007/0010/0012
// — is proved on this one copy against every fraction length.
func TestFixedWidthMigrationMatchesFormatTime(t *testing.T) {
	handle, ms := openPlatformThrough(t, "0001")
	ctx := context.Background()
	if _, err := handle.ExecContext(ctx,
		`INSERT INTO users (id, username, username_fold, password_hash, created_at)
		 VALUES (1, 'u', 'u', 'x', '2026-09-25T12:00:05Z')`); err != nil {
		t.Fatal(err)
	}

	r := rand.New(rand.NewPCG(356, 3))
	base := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	want := map[string]time.Time{}
	for i := range 2000 {
		ns := r.Int64N(int64(3 * time.Second))
		switch r.IntN(4) { // whole, milli- and microsecond values are common in practice
		case 0:
			ns -= ns % int64(time.Second)
		case 1:
			ns -= ns % int64(time.Millisecond)
		case 2:
			ns -= ns % int64(time.Microsecond)
		}
		at := base.Add(time.Duration(ns))
		id := "r" + strconv.Itoa(i)
		want[id] = at
		if _, err := handle.ExecContext(ctx,
			`INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, 1, ?, ?)`,
			id, at.Format(time.RFC3339Nano), at.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	rows, err := handle.QueryContext(ctx, `SELECT id, expires_at FROM sessions ORDER BY expires_at, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var prev time.Time
	for rows.Next() {
		var id, got string
		if err := rows.Scan(&id, &got); err != nil {
			t.Fatal(err)
		}
		if w := db.FormatTime(want[id]); got != w {
			t.Fatalf("session %s expires_at = %q, want %q", id, got, w)
		}
		if want[id].Before(prev) {
			t.Fatalf("session %s (%v) sorted after a later %v", id, want[id], prev)
		}
		prev = want[id]
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// TestFixedWidthMigrationIsIdempotentAndLeavesOddValuesAlone: running the
// same SQL again changes nothing, and values RFC3339Nano in UTC never
// produced are skipped rather than mangled.
func TestFixedWidthMigrationIsIdempotentAndLeavesOddValuesAlone(t *testing.T) {
	handle, ms := openPlatformThrough(t, "0001")
	ctx := context.Background()
	if _, err := handle.ExecContext(ctx,
		`INSERT INTO users (id, username, username_fold, password_hash, created_at)
		 VALUES (1, 'u', 'u', 'x', '2026-09-25T12:00:05.5Z')`); err != nil {
		t.Fatal(err)
	}
	odd := []string{
		"2026-09-25T22:00:05+10:00",      // an offset: never written, still parses
		"2026-09-25T12:00:05.12a4Z",      // not digits
		"2026-09-25T12:00:05.Z",          // an empty fraction
		"2026-09-25T12:00:05.123456789Z", // already fixed width
		"not a time",
	}
	for i, v := range odd {
		if _, err := handle.ExecContext(ctx,
			`INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, 1, ?, ?)`,
			"o"+strconv.Itoa(i), v, v); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for i, v := range odd {
		var got string
		if err := handle.QueryRowContext(ctx,
			`SELECT expires_at FROM sessions WHERE id = ?`, "o"+strconv.Itoa(i)).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != v {
			t.Errorf("odd value %q was rewritten to %q", v, got)
		}
	}

	res, err := handle.ExecContext(ctx, migrationSQL(t, ms, "0002"))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 0 {
		t.Errorf("second run's last UPDATE touched %d rows (err %v), want 0", n, err)
	}
	var created string
	if err := handle.QueryRowContext(ctx, `SELECT created_at FROM users WHERE id = 1`).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if created != "2026-09-25T12:00:05.500000000Z" {
		t.Errorf("created_at after a second run = %q", created)
	}
}

// TestDeleteExpiredSessionsWithinTheSameSecond is #356 for sessions: a
// session that expired on a whole second must be swept half a second later.
// With RFC3339Nano, "...:00Z" <= "...:00.5Z" was false as text.
func TestDeleteExpiredSessionsWithinTheSameSecond(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "ilia", "$argon2id$fake", false)
	if err != nil {
		t.Fatal(err)
	}

	created := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return created })
	sess, err := s.CreateSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	s.SetClock(func() time.Time { return sess.ExpiresAt.Add(500 * time.Millisecond) })
	n, err := s.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("DeleteExpiredSessions removed %d sessions, want 1", n)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

```bash
go test ./internal/platform/auth/ -run 'FixedWidth|SameSecond' -count=1
```
Expected: four failures.
- `TestFixedWidthMigrationRewritesPlatformTimestamps` reports every seeded value unchanged, e.g. `created_at = "2026-09-25T12:00:05.244Z", want "2026-09-25T12:00:05.244000000Z"`, and `users by created_at = [u1 u3 u2 u4 u0]`.
- `TestFixedWidthMigrationMatchesFormatTime` fails with `expires_at = "…Z", want "…000Z"`.
- `TestFixedWidthMigrationIsIdempotentAndLeavesOddValuesAlone` stops at `no platform migration 0002`.
- `TestDeleteExpiredSessionsWithinTheSameSecond` fails with `DeleteExpiredSessions removed 0 sessions, want 1`.

- [ ] **Step 3: Switch the helpers in `internal/platform/auth/store.go`**

Replace:

```go
// Timestamps are stored as RFC 3339 nanosecond strings in UTC, which sort
// lexically in the same order they sort chronologically.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("auth: parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}
```

with:

```go
// Timestamps are stored in db.TimeLayout — UTC, always nine fractional
// digits — which sorts lexically in the same order it sorts chronologically,
// including within one second (#356).
func formatTime(t time.Time) string { return db.FormatTime(t) }

func parseTime(s string) (time.Time, error) {
	t, err := db.ParseTime(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("auth: parse timestamp %q: %w", s, err)
	}
	return t, nil
}
```

and in the import block replace:

```go
	"strings"
	"time"
)
```

with:

```go
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)
```

- [ ] **Step 4: Create `internal/platform/auth/migrations/0002_fixed_width_timestamps.sql`**

```sql
-- internal/platform/auth/migrations/0002_fixed_width_timestamps.sql
-- Rewrites every stored platform timestamp to db.TimeLayout's fixed width
-- (#356). Each app's own migration of the same name does its tables.
--
-- Timestamps used to be written with Go's time.RFC3339Nano, which trims
-- trailing fractional zeros, so values were 20 to 30 characters wide and
-- 'Z' (which sorts after every digit) landed at different offsets:
-- "...:05.244Z" compared as later than "...:05.244124Z". ORDER BY, <, <=,
-- min() and max() on these TEXT columns compare bytes, so within one second
-- text order and time order disagreed. db.FormatTime now always writes nine
-- fraction digits ("2006-01-02T15:04:05.000000000Z", 30 characters).
--
-- Each UPDATE pads one column in place: it keeps the first 19 characters
-- (through the seconds), right-pads the fraction, or an empty one, with
-- zeros to nine digits, and re-appends 'Z'. strftime('%f') is not used
-- because it keeps only milliseconds. The WHERE clause touches only what
-- RFC3339Nano wrote in UTC: 20-29 characters, the date-time shape, then
-- either 'Z' straight after the seconds or '.', one to eight digits and
-- 'Z'. NULLs, values already 30 wide, and anything unexpected (an offset
-- other than Z, a stray non-digit) are left alone, so running this twice
-- changes nothing, and db.ParseTime still reads whatever it skipped.
--
-- schema_migrations is created by db.Apply before any migration runs, so
-- its applied_at is rewritten here too. The row recording this migration is
-- written after it, already fixed-width.

UPDATE users
   SET created_at = substr(created_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(created_at, 20, 1) = '.'
                   THEN substr(created_at, 21, length(created_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(created_at) BETWEEN 20 AND 29
   AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(created_at) = 20
        OR (substr(created_at, 20, 1) = '.' AND length(created_at) >= 22
            AND substr(created_at, 21, length(created_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE sessions
   SET created_at = substr(created_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(created_at, 20, 1) = '.'
                   THEN substr(created_at, 21, length(created_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(created_at) BETWEEN 20 AND 29
   AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(created_at) = 20
        OR (substr(created_at, 20, 1) = '.' AND length(created_at) >= 22
            AND substr(created_at, 21, length(created_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE sessions
   SET expires_at = substr(expires_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(expires_at, 20, 1) = '.'
                   THEN substr(expires_at, 21, length(expires_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(expires_at) BETWEEN 20 AND 29
   AND expires_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(expires_at) = 20
        OR (substr(expires_at, 20, 1) = '.' AND length(expires_at) >= 22
            AND substr(expires_at, 21, length(expires_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE schema_migrations
   SET applied_at = substr(applied_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(applied_at, 20, 1) = '.'
                   THEN substr(applied_at, 21, length(applied_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(applied_at) BETWEEN 20 AND 29
   AND applied_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(applied_at) = 20
        OR (substr(applied_at, 20, 1) = '.' AND length(applied_at) >= 22
            AND substr(applied_at, 21, length(applied_at) - 21) NOT GLOB '*[^0-9]*'));
```

- [ ] **Step 5: Switch the backup test's session fixture**

In `cmd/onsuite/backup_test.go`, replace:

```go
	expiredAt := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
```

with:

```go
	expiredAt := db.FormatTime(time.Now().Add(-time.Hour))
```

(`db` is already imported there, at `backup_test.go:18`.)

- [ ] **Step 6: Run the tests**

```bash
go test ./internal/platform/auth/ ./cmd/onsuite/ ./internal/platform/admin/ -count=1
```
Expected: `ok` for all three packages.

- [ ] **Step 7: Full check and commit**

Every app's fixture applies the platform migrations, so the whole suite exercises `platform:0002` on a fresh database.

```bash
git add internal/platform/auth/store.go internal/platform/auth/migrations/0002_fixed_width_timestamps.sql internal/platform/auth/timestamps_test.go cmd/onsuite/backup_test.go
git commit -m "fix(auth): store platform timestamps fixed-width and migrate existing rows (#356)"
```

---

### Task 4: ON Flash — helpers, migration `flash:0012`, and the flake

**Files:**
- Modify: `internal/apps/flash/store.go:76-86` (and its import block), `internal/apps/flash/review.go:13`
- Create: `internal/apps/flash/migrations/0012_fixed_width_timestamps.sql`
- Test: `internal/apps/flash/timestamps_test.go` (new, `package flash_test`)

**Interfaces:**
- Consumes: `newFixture` (`deck_test.go:28`), `qDeck`/`qCard` (`review_test.go:764`, `:773`), `Store.SetClock`, `DueQueue`, `QueueFront`, `ListCards`, `ListDecks`, `ImportDeck`, `flash.ParseImport`, `GradeCard`, `flash.RatingAgain`, `flash.ID`, `flash.Migrations()`.
- Produces: the migration. The test helpers are `flashLegacyTimes`, `openFlashThrough`, `flashRewritten` and `orNull`, all new names in `flash_test`.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/flash/timestamps_test.go`:

```go
package flash_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// flashLegacyTimes are values as time.RFC3339Nano wrote them before #356,
// all inside one second and listed in time order. As text they sort
// differently ('Z' sorts after every digit), which is the bug.
var flashLegacyTimes = []string{
	"2026-09-25T12:00:05Z",
	"2026-09-25T12:00:05.123456789Z",
	"2026-09-25T12:00:05.244Z",
	"2026-09-25T12:00:05.244124Z",
	"2026-09-25T12:00:05.5Z",
}

// openFlashThrough is newFixture with Flash's schema stopped after lastID,
// so a test can seed rows the way an older binary wrote them. It returns
// every migration for the test to finish applying.
func openFlashThrough(t *testing.T, lastID string) (*sql.DB, []db.Migration, auth.User, auth.User) {
	t.Helper()
	ctx := context.Background()
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	platform, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	app, err := db.Collect(flash.ID, flash.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	early := slices.Clone(platform)
	for _, m := range app {
		if m.ID <= lastID {
			early = append(early, m)
		}
	}
	if _, err := db.Apply(ctx, handle, early); err != nil {
		t.Fatal(err)
	}
	users := auth.NewStore(handle)
	alice, err := users.CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := users.CreateUser(ctx, "bob", apptest.PasswordHash, false)
	if err != nil {
		t.Fatal(err)
	}
	return handle, append(platform, app...), alice, bob
}

func flashRewritten(t *testing.T, legacy string) string {
	t.Helper()
	parsed, err := db.ParseTime(legacy)
	if err != nil {
		t.Fatalf("setup: %q does not parse: %v", legacy, err)
	}
	return db.FormatTime(parsed)
}

// orNull is legacy on odd rows and NULL on even ones, so every nullable
// column is checked both ways.
func orNull(i int, legacy string) any {
	if i%2 == 0 {
		return nil
	}
	return legacy
}

func TestFixedWidthMigrationRewritesFlashTimestamps(t *testing.T) {
	handle, ms, alice, bob := openFlashThrough(t, "0011")
	ctx := context.Background()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := handle.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	for i, ts := range flashLegacyTimes {
		id := strconv.Itoa(i)
		exec(`INSERT INTO flash_decks (id, user_id, name, created_at, snoozed_until) VALUES (?, ?, ?, ?, ?)`,
			i+1, alice.ID, "d"+id, ts, orNull(i, ts))
		exec(`INSERT INTO flash_cards (id, deck_id, user_id, card_type, front, created_at) VALUES (?, 1, ?, 'basic', ?, ?)`,
			i+1, alice.ID, "c"+id, ts)
		exec(`INSERT INTO flash_card_state (user_id, card_id, state, due_at, stability, difficulty,
		        scheduled_days, reps, lapses, remaining_steps, last_review_at, log_due, log_review)
		      VALUES (?, ?, 'review', ?, 1, 5, 1, 1, 0, 0, ?, ?, ?)`,
			alice.ID, i+1, ts, orNull(i, ts), orNull(i, ts), orNull(i, ts))
		exec(`INSERT INTO flash_media (hash, kind, fetched_at) VALUES (?, 'image', ?)`, "h"+id, orNull(i, ts))
		exec(`INSERT INTO flash_shares (id, deck_id, from_user_id, to_user_id, status, created_at, responded_at)
		      VALUES (?, 1, ?, ?, 'pending', ?, ?)`, i+1, alice.ID, bob.ID, ts, orNull(i, ts))
	}
	exec(`INSERT INTO flash_review_counts (user_id, deck_id, day, new_count) VALUES (?, 1, '2026-09-25', 3)`, alice.ID)

	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for i, ts := range flashLegacyTimes {
		want := flashRewritten(t, ts)
		key := i + 1
		for _, c := range []struct {
			query    string
			arg      any
			nullable bool
		}{
			{`SELECT created_at FROM flash_decks WHERE id = ?`, key, false},
			{`SELECT snoozed_until FROM flash_decks WHERE id = ?`, key, true},
			{`SELECT created_at FROM flash_cards WHERE id = ?`, key, false},
			{`SELECT due_at FROM flash_card_state WHERE card_id = ?`, key, false},
			{`SELECT last_review_at FROM flash_card_state WHERE card_id = ?`, key, true},
			{`SELECT log_due FROM flash_card_state WHERE card_id = ?`, key, true},
			{`SELECT log_review FROM flash_card_state WHERE card_id = ?`, key, true},
			{`SELECT fetched_at FROM flash_media WHERE hash = ?`, "h" + strconv.Itoa(i), true},
			{`SELECT created_at FROM flash_shares WHERE id = ?`, key, false},
			{`SELECT responded_at FROM flash_shares WHERE id = ?`, key, true},
		} {
			var got sql.NullString
			if err := handle.QueryRowContext(ctx, c.query, c.arg).Scan(&got); err != nil {
				t.Fatalf("%s [%v]: %v", c.query, c.arg, err)
			}
			if c.nullable && i%2 == 0 {
				if got.Valid {
					t.Errorf("%s [%v] = %q, want NULL left alone", c.query, c.arg, got.String)
				}
				continue
			}
			if got.String != want {
				t.Errorf("%s [%v] = %q, want %q (from %q)", c.query, c.arg, got.String, want, ts)
			}
		}
	}

	var day string
	var newCount int
	if err := handle.QueryRowContext(ctx,
		`SELECT day, new_count FROM flash_review_counts WHERE deck_id = 1`).Scan(&day, &newCount); err != nil {
		t.Fatal(err)
	}
	if day != "2026-09-25" || newCount != 3 {
		t.Errorf("flash_review_counts row = (%q, %d), want the date-only day untouched", day, newCount)
	}

	// The store reads the rewritten rows, and text order is time order.
	decks, err := flash.NewStore(handle).ListDecks(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range decks {
		names = append(names, d.Name)
	}
	if want := []string{"d4", "d3", "d2", "d1", "d0"}; !slices.Equal(names, want) {
		t.Errorf("ListDecks (newest first) = %v, want %v", names, want)
	}
}

// TestNewCardsQueueInCreationOrderWithinOneSecond is the flake behind #356,
// made deterministic: n1 is created 124µs before n2 in the same second, at
// the instants RFC3339Nano wrote as "…12.244Z" and "…12.244124Z". The
// queue's head must be n1, and DueQueue and QueueFront must agree.
func TestNewCardsQueueInCreationOrderWithinOneSecond(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	second := time.Date(2026, 9, 24, 11, 0, 12, 0, time.UTC)

	f.store.SetClock(func() time.Time { return second })
	d := qDeck(t, f, "A")
	f.store.SetClock(func() time.Time { return second.Add(244 * time.Millisecond) })
	qCard(t, f, d.ID, "n1")
	f.store.SetClock(func() time.Time { return second.Add(244124 * time.Microsecond) })
	qCard(t, f, d.ID, "n2")

	now := second.Add(time.Hour)
	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	var fronts []string
	for _, q := range queue {
		fronts = append(fronts, q.Card.Front)
	}
	if want := []string{"n1", "n2"}; !slices.Equal(fronts, want) {
		t.Errorf("DueQueue = %v, want %v", fronts, want)
	}
	front, err := f.store.QueueFront(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if !front.HasHead || front.Head.Card.Front != "n1" {
		t.Errorf("QueueFront head = %+v, want n1", front.Head.Card)
	}

	cards, err := f.store.ListCards(ctx, f.alice.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 || cards[0].Front != "n2" || cards[1].Front != "n1" {
		t.Errorf("ListCards (newest first) = %+v, want [n2 n1]", cards)
	}
}

func TestListDecksNewestFirstWithinOneSecond(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	second := time.Date(2026, 9, 24, 11, 0, 12, 0, time.UTC)

	f.store.SetClock(func() time.Time { return second.Add(244 * time.Millisecond) })
	qDeck(t, f, "older")
	f.store.SetClock(func() time.Time { return second.Add(244124 * time.Microsecond) })
	qDeck(t, f, "newer")

	decks, err := f.store.ListDecks(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decks) != 2 || decks[0].Name != "newer" || decks[1].Name != "older" {
		t.Errorf("ListDecks = %+v, want [newer older]", decks)
	}
}

// TestImportedCardsKeepTheirOrderWithinOneSecond: ImportDeck stamps each
// card with st.now() inside one transaction, microseconds apart, so the
// whole deck usually lands inside one second. Its new cards must queue in
// the order they were written.
func TestImportedCardsKeepTheirOrderWithinOneSecond(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	second := time.Date(2026, 9, 24, 11, 0, 12, 0, time.UTC)
	ticks := []time.Time{
		second,                                    // the deck
		second.Add(244 * time.Millisecond),        // q1
		second.Add(244124 * time.Microsecond),     // q2
		second.Add(245 * time.Millisecond),        // q3
		second.Add(245 * time.Millisecond).Add(1), // anything after
	}
	calls := 0
	f.store.SetClock(func() time.Time {
		at := ticks[min(calls, len(ticks)-1)]
		calls++
		return at
	})

	parsed, err := flash.ParseImport(`{
		"deck": {"name": "Imported"},
		"cards": [
			{"type": "basic", "front": "q1", "back": "a"},
			{"type": "basic", "front": "q2", "back": "a"},
			{"type": "basic", "front": "q3", "back": "a"}
		]
	}`, "json")
	if err != nil {
		t.Fatal(err)
	}
	deck, err := f.store.ImportDeck(ctx, f.alice.ID, parsed.Name, parsed.Description, parsed.Cards)
	if err != nil {
		t.Fatal(err)
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, &deck.ID, second.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var fronts []string
	for _, q := range queue {
		fronts = append(fronts, q.Card.Front)
	}
	if want := []string{"q1", "q2", "q3"}; !slices.Equal(fronts, want) {
		t.Errorf("imported deck queues as %v, want %v", fronts, want)
	}
}

// TestReviewDueHalfASecondAgoIsDue: due_at <= ? compares stored text. A
// card due on a whole second must be in the queue half a second later —
// with RFC3339Nano, "…:00Z" <= "…:00.5Z" was false.
func TestReviewDueHalfASecondAgoIsDue(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d := qDeck(t, f, "A")
	c := qCard(t, f, d.ID, "review me")

	gradedAt := time.Date(2026, 9, 24, 11, 0, 0, 0, time.UTC)
	sched, err := f.store.GradeCard(ctx, f.alice.ID, c.ID, flash.RatingAgain, gradedAt)
	if err != nil {
		t.Fatal(err)
	}
	if sched.DueAt.Nanosecond() != 0 {
		t.Fatalf("setup: DueAt %v is not on a whole second; this test needs one", sched.DueAt)
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, &d.ID, sched.DueAt.Add(500*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 || queue[0].Card.ID != c.ID || queue[0].IsNew {
		t.Errorf("DueQueue half a second after DueAt = %+v, want the one review card", queue)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

```bash
go test ./internal/apps/flash/ -run 'FixedWidth|WithinOneSecond|HalfASecond' -count=1
```
Expected: five failures.
- `TestFixedWidthMigrationRewritesFlashTimestamps` fails with values such as `= "2026-09-25T12:00:05.244Z", want "2026-09-25T12:00:05.244000000Z"` and `ListDecks … = [d0 d4 d2 d3 d1]`.
- `TestNewCardsQueueInCreationOrderWithinOneSecond` fails with `DueQueue = [n2 n1]`.
- `TestListDecksNewestFirstWithinOneSecond` fails.
- `TestImportedCardsKeepTheirOrderWithinOneSecond` fails with `[q2 q1 q3]`.
- `TestReviewDueHalfASecondAgoIsDue` fails with an empty queue.

(The prototype confirmed that the four regression tests fail while `formatTime` still writes RFC3339Nano, and pass once it doesn't.)

- [ ] **Step 3: Switch the helpers in `internal/apps/flash/store.go`**

Replace:

```go
// Timestamps match the platform's convention: RFC 3339 nanoseconds in UTC,
// which sorts chronologically as text.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("flash: parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}
```

with:

```go
// Timestamps match the platform's convention, db.TimeLayout: UTC with
// exactly nine fractional digits, which sorts chronologically as text even
// within one second (#356) — the property ORDER BY created_at, due_at <= ?
// and min(due_at) all rely on.
func formatTime(t time.Time) string { return db.FormatTime(t) }

func parseTime(s string) (time.Time, error) {
	t, err := db.ParseTime(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("flash: parse timestamp %q: %w", s, err)
	}
	return t, nil
}
```

and in the import block replace:

```go
	"strings"
	"time"
)
```

with:

```go
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)
```

In `internal/apps/flash/review.go`, replace:

```go
// date, coarser than this package's usual RFC3339Nano timestamps, because a
```

with:

```go
// date, coarser than this package's usual db.TimeLayout timestamps, because a
```

- [ ] **Step 4: Create `internal/apps/flash/migrations/0012_fixed_width_timestamps.sql`**

```sql
-- internal/apps/flash/migrations/0012_fixed_width_timestamps.sql
-- Rewrites every Flash timestamp to db.TimeLayout's fixed width (#356).
--
-- Flash wrote timestamps with time.RFC3339Nano, which trims trailing
-- fractional zeros, so "...:12.244Z" sorted after "...:12.244124Z" as
-- text. That put new cards (ORDER BY created_at) and decks (ORDER BY
-- created_at DESC) out of order within one second — an imported or gifted
-- deck stamps its cards microseconds apart — and made due_at <= ? and
-- min(due_at) misjudge sub-second boundaries. formatTime now writes nine
-- fraction digits always.
--
-- The UPDATEs are the platform's
-- internal/platform/auth/migrations/0002_fixed_width_timestamps.sql, one
-- per column; its header explains each clause. NULLs (snoozed_until,
-- last_review_at, log_due, log_review, fetched_at, responded_at) and values
-- already 30 wide are left alone, so this is safe to run twice.
-- flash_review_counts.day is a 'YYYY-MM-DD' date, already fixed width, and
-- is not touched.

UPDATE flash_decks
   SET created_at = substr(created_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(created_at, 20, 1) = '.'
                   THEN substr(created_at, 21, length(created_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(created_at) BETWEEN 20 AND 29
   AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(created_at) = 20
        OR (substr(created_at, 20, 1) = '.' AND length(created_at) >= 22
            AND substr(created_at, 21, length(created_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_decks
   SET snoozed_until = substr(snoozed_until, 1, 19) || '.' ||
       substr(CASE WHEN substr(snoozed_until, 20, 1) = '.'
                   THEN substr(snoozed_until, 21, length(snoozed_until) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(snoozed_until) BETWEEN 20 AND 29
   AND snoozed_until GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(snoozed_until) = 20
        OR (substr(snoozed_until, 20, 1) = '.' AND length(snoozed_until) >= 22
            AND substr(snoozed_until, 21, length(snoozed_until) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_cards
   SET created_at = substr(created_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(created_at, 20, 1) = '.'
                   THEN substr(created_at, 21, length(created_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(created_at) BETWEEN 20 AND 29
   AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(created_at) = 20
        OR (substr(created_at, 20, 1) = '.' AND length(created_at) >= 22
            AND substr(created_at, 21, length(created_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_card_state
   SET due_at = substr(due_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(due_at, 20, 1) = '.'
                   THEN substr(due_at, 21, length(due_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(due_at) BETWEEN 20 AND 29
   AND due_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(due_at) = 20
        OR (substr(due_at, 20, 1) = '.' AND length(due_at) >= 22
            AND substr(due_at, 21, length(due_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_card_state
   SET last_review_at = substr(last_review_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(last_review_at, 20, 1) = '.'
                   THEN substr(last_review_at, 21, length(last_review_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(last_review_at) BETWEEN 20 AND 29
   AND last_review_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(last_review_at) = 20
        OR (substr(last_review_at, 20, 1) = '.' AND length(last_review_at) >= 22
            AND substr(last_review_at, 21, length(last_review_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_card_state
   SET log_due = substr(log_due, 1, 19) || '.' ||
       substr(CASE WHEN substr(log_due, 20, 1) = '.'
                   THEN substr(log_due, 21, length(log_due) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(log_due) BETWEEN 20 AND 29
   AND log_due GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(log_due) = 20
        OR (substr(log_due, 20, 1) = '.' AND length(log_due) >= 22
            AND substr(log_due, 21, length(log_due) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_card_state
   SET log_review = substr(log_review, 1, 19) || '.' ||
       substr(CASE WHEN substr(log_review, 20, 1) = '.'
                   THEN substr(log_review, 21, length(log_review) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(log_review) BETWEEN 20 AND 29
   AND log_review GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(log_review) = 20
        OR (substr(log_review, 20, 1) = '.' AND length(log_review) >= 22
            AND substr(log_review, 21, length(log_review) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_media
   SET fetched_at = substr(fetched_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(fetched_at, 20, 1) = '.'
                   THEN substr(fetched_at, 21, length(fetched_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(fetched_at) BETWEEN 20 AND 29
   AND fetched_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(fetched_at) = 20
        OR (substr(fetched_at, 20, 1) = '.' AND length(fetched_at) >= 22
            AND substr(fetched_at, 21, length(fetched_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_shares
   SET created_at = substr(created_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(created_at, 20, 1) = '.'
                   THEN substr(created_at, 21, length(created_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(created_at) BETWEEN 20 AND 29
   AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(created_at) = 20
        OR (substr(created_at, 20, 1) = '.' AND length(created_at) >= 22
            AND substr(created_at, 21, length(created_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE flash_shares
   SET responded_at = substr(responded_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(responded_at, 20, 1) = '.'
                   THEN substr(responded_at, 21, length(responded_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(responded_at) BETWEEN 20 AND 29
   AND responded_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(responded_at) = 20
        OR (substr(responded_at, 20, 1) = '.' AND length(responded_at) >= 22
            AND substr(responded_at, 21, length(responded_at) - 21) NOT GLOB '*[^0-9]*'));
```

- [ ] **Step 5: Run the package**

```bash
go test ./internal/apps/flash/ -count=1
```
Expected: `ok  	github.com/iliafrenkel/on-suite/internal/apps/flash`.

- [ ] **Step 6: Stress the formerly flaky test**

```bash
go test ./internal/apps/flash/ -run '^TestQueueFrontAgreesWithDueQueue$' -count=2000
```
Expected: `ok`, in about 3 minutes (the prototype took 169s). Before the fix, the issue saw 5 failures in 1,500 runs.

- [ ] **Step 7: Full check and commit**

```bash
git add internal/apps/flash/store.go internal/apps/flash/review.go internal/apps/flash/migrations/0012_fixed_width_timestamps.sql internal/apps/flash/timestamps_test.go
git commit -m "fix(flash): store timestamps fixed-width so same-second rows sort correctly (#356)"
```

---

### Task 5: ON Notes — helpers, migration `notes:0007`, and the archive order

**Files:**
- Modify: `internal/apps/notes/store.go:178-186` (and its import block), `internal/apps/notes/tree.go:198`, `internal/apps/notes/archive.go:56-63`, `:84`
- Modify: `internal/apps/notes/archive_test.go:1-8`, `:106-136`
- Create: `internal/apps/notes/migrations/0007_fixed_width_timestamps.sql`
- Test: `internal/apps/notes/timestamps_test.go` (new, `package notes_test`)

**Interfaces:**
- Consumes: `newFixture` (`store_test.go:29`), `f.mk` (`tree_test.go:19`), `Store.SetClock` (Ops copies the clock at `Do` time, `tree.go:24-28`), `SetArchived`, `Archive`, `Search(ctx, userID, query, showCompleted)`, `notes.ID`, `notes.Migrations()`.
- Produces: the migration. `formatTime` moves from `tree.go` to `store.go`, next to `parseTime`, so only `store.go` imports `db`. The test helpers are `notesLegacyTimes` and `openNotesThrough`.

- [ ] **Step 1: Write the failing migration test**

Create `internal/apps/notes/timestamps_test.go`:

```go
package notes_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// notesLegacyTimes are values as time.RFC3339Nano wrote them before #356,
// all inside one second and listed in time order. As text they sort
// differently ('Z' sorts after every digit), which is the bug.
var notesLegacyTimes = []string{
	"2026-09-25T12:00:05Z",
	"2026-09-25T12:00:05.123456789Z",
	"2026-09-25T12:00:05.244Z",
	"2026-09-25T12:00:05.244124Z",
	"2026-09-25T12:00:05.5Z",
}

// openNotesThrough is newFixture with Notes' schema stopped after lastID,
// so a test can seed rows the way an older binary wrote them. It returns
// every migration for the test to finish applying.
func openNotesThrough(t *testing.T, lastID string) (*sql.DB, []db.Migration, auth.User) {
	t.Helper()
	ctx := context.Background()
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	platform, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	app, err := db.Collect(notes.ID, notes.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	early := slices.Clone(platform)
	for _, m := range app {
		if m.ID <= lastID {
			early = append(early, m)
		}
	}
	if _, err := db.Apply(ctx, handle, early); err != nil {
		t.Fatal(err)
	}
	alice, err := auth.NewStore(handle).CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}
	return handle, append(platform, app...), alice
}

func TestFixedWidthMigrationRewritesNotesTimestamps(t *testing.T) {
	handle, ms, alice := openNotesThrough(t, "0006")
	ctx := context.Background()

	for i, ts := range notesLegacyTimes {
		var done any // NULL on even rows, so NULLs are checked too
		if i%2 == 1 {
			done = ts
		}
		if _, err := handle.ExecContext(ctx,
			`INSERT INTO notes_nodes (id, user_id, parent_id, position, title, note,
			        created_at, updated_at, done_at, due_on, archived_at)
			 VALUES (?, ?, NULL, ?, ?, '', ?, ?, ?, '2026-09-30', ?)`,
			i+1, alice.ID, i, "archived "+strconv.Itoa(i), ts, ts, done, ts); err != nil {
			t.Fatal(err)
		}
	}
	// One live node, to prove the FTS index survives the rewrite's UPDATEs.
	if _, err := handle.ExecContext(ctx,
		`INSERT INTO notes_nodes (id, user_id, parent_id, position, title, note, created_at, updated_at)
		 VALUES (6, ?, NULL, 5, 'unarchived needle', '', '2026-09-25T12:00:05.7Z', '2026-09-25T12:00:05.7Z')`,
		alice.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for i, ts := range notesLegacyTimes {
		parsed, err := db.ParseTime(ts)
		if err != nil {
			t.Fatal(err)
		}
		want := db.FormatTime(parsed)
		var created, updated, archived, dueOn string
		var done sql.NullString
		if err := handle.QueryRowContext(ctx,
			`SELECT created_at, updated_at, done_at, archived_at, due_on FROM notes_nodes WHERE id = ?`,
			i+1).Scan(&created, &updated, &done, &archived, &dueOn); err != nil {
			t.Fatal(err)
		}
		for name, got := range map[string]string{"created_at": created, "updated_at": updated, "archived_at": archived} {
			if got != want {
				t.Errorf("node %d %s = %q, want %q (from %q)", i+1, name, got, want, ts)
			}
		}
		switch {
		case i%2 == 0 && done.Valid:
			t.Errorf("node %d done_at = %q, want NULL left alone", i+1, done.String)
		case i%2 == 1 && done.String != want:
			t.Errorf("node %d done_at = %q, want %q", i+1, done.String, want)
		}
		if dueOn != "2026-09-30" {
			t.Errorf("node %d due_on = %q, want the date-only value untouched", i+1, dueOn)
		}
	}

	st := notes.NewStore(handle)
	archive, err := st.Archive(ctx, alice.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var order []int64
	for _, n := range archive {
		order = append(order, n.ID)
	}
	if want := []int64{5, 4, 3, 2, 1}; !slices.Equal(order, want) {
		t.Errorf("Archive (most recent first) = %v, want %v", order, want)
	}
	found, err := st.Search(ctx, alice.ID, "needle", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].ID != 6 {
		t.Errorf("Search(needle) after the rewrite = %+v, want node 6", found)
	}
}
```

- [ ] **Step 2: Rewrite the #108 test to go through the store's clock**

Two reasons. It seeded literal RFC3339Nano strings, which is no longer a reachable state. And a 4µs case is exactly what `julianday` gets wrong.

In `internal/apps/notes/archive_test.go`, replace the import block:

```go
import (
	"context"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)
```

with:

```go
import (
	"context"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
)
```

and replace the whole `TestArchiveOrdersChronologicallyAcrossWholeAndFractionalSeconds` function, doc comment included, with:

```go
// TestArchiveOrdersChronologicallyAcrossWholeAndFractionalSeconds is issue
// #108 and #356: "earlier" is archived on a whole second, "later" half a
// second after it and "latest" 4µs after that. RFC3339Nano wrote the first
// as "...T10:00:00Z", which sorted after "...T10:00:00.5Z" as text, and
// the julianday() workaround for that resolved only milliseconds, tying the
// last two. Stored as db.TimeLayout, a plain ORDER BY gets all three right.
func TestArchiveOrdersChronologicallyAcrossWholeAndFractionalSeconds(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	earlier := f.mk(t, notes.RootID, "earlier, whole second")
	later := f.mk(t, notes.RootID, "later, fractional second")
	latest := f.mk(t, notes.RootID, "latest, 4µs after later")

	second := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	for _, step := range []struct {
		id int64
		at time.Time
	}{
		{earlier.ID, second},
		{later.ID, second.Add(500 * time.Millisecond)},
		{latest.ID, second.Add(500*time.Millisecond + 4*time.Microsecond)},
	} {
		f.store.SetClock(func() time.Time { return step.at })
		if err := f.store.SetArchived(ctx, f.alice.ID, step.id, true); err != nil {
			t.Fatal(err)
		}
	}

	got, err := f.store.Archive(ctx, f.alice.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != latest.ID || got[1].ID != later.ID || got[2].ID != earlier.ID {
		t.Fatalf("Archive = %+v, want [latest, later, earlier] (most recent first)", got)
	}
}
```

- [ ] **Step 3: Run to verify they fail**

```bash
go test ./internal/apps/notes/ -run 'FixedWidth|Chronologically' -count=1
```
Expected: both tests fail. `TestFixedWidthMigrationRewritesNotesTimestamps` fails with `created_at = "…05.244Z", want "…05.244000000Z"`. `TestArchiveOrdersChronologicallyAcrossWholeAndFractionalSeconds` fails with `want [latest, later, earlier]`.

- [ ] **Step 4: Move and switch the helpers**

In `internal/apps/notes/tree.go`, delete:

```go

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
```

(the blank line before it too). `tree.go` still uses `time` for `Ops.now`.

In `internal/apps/notes/store.go`, replace:

```go
// Timestamps match the platform's convention: RFC 3339 nanoseconds in UTC,
// which sorts chronologically as text.
func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("notes: parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}
```

with:

```go
// Timestamps match the platform's convention, db.TimeLayout: UTC with
// exactly nine fractional digits, which sorts chronologically as text even
// within one second (#356). Archive's ORDER BY archived_at relies on it.
func formatTime(t time.Time) string { return db.FormatTime(t) }

func parseTime(s string) (time.Time, error) {
	t, err := db.ParseTime(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("notes: parse timestamp %q: %w", s, err)
	}
	return t, nil
}
```

and in its import block replace:

```go
	"strings"
	"time"
)
```

with:

```go
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)
```

- [ ] **Step 5: Order the archive by the column itself**

In `internal/apps/notes/archive.go`, replace:

```go
// julianday(n.archived_at), not a plain ORDER BY n.archived_at: formatTime
// (tree.go) writes RFC3339Nano, which strips a whole-second timestamp's
// trailing zero fraction, so "...T10:00:00Z" sorts after
// "...T10:00:00.5Z" under a byte-wise DESC compare ('Z' > '.') despite
// being chronologically earlier — issue #108. julianday parses the string
// as an actual instant rather than comparing bytes, so this orders
// correctly regardless of which timestamps in the table happen to land on
// a whole second.
```

with:

```go
// A plain ORDER BY n.archived_at is chronological because formatTime
// (store.go) writes db.TimeLayout, fixed width with nine fractional digits
// (#356). This used to be julianday(n.archived_at), a workaround for
// RFC3339Nano trimming "...T10:00:00Z" shorter than "...T10:00:00.5Z"
// (issue #108); julianday resolves only milliseconds, so it tied two
// archives a few microseconds apart, which the stored text does not.
```

and replace:

```go
		  ORDER BY julianday(n.archived_at) DESC`,
```

with:

```go
		  ORDER BY n.archived_at DESC`,
```

- [ ] **Step 6: Create `internal/apps/notes/migrations/0007_fixed_width_timestamps.sql`**

```sql
-- internal/apps/notes/migrations/0007_fixed_width_timestamps.sql
-- Rewrites every ON Notes timestamp to db.TimeLayout's fixed width (#356).
--
-- Notes wrote timestamps with time.RFC3339Nano, which trims trailing
-- fractional zeros, so text order and time order disagreed within one
-- second (issue #108 hit this for archived_at, worked around with
-- julianday; Archive now orders by the column itself). formatTime now
-- writes nine fraction digits always.
--
-- The UPDATEs are the platform's
-- internal/platform/auth/migrations/0002_fixed_width_timestamps.sql, one
-- per column; its header explains each clause. NULL done_at/archived_at and
-- values already 30 wide are left alone, so this is safe to run twice.
-- due_on is a 'YYYY-MM-DD' date, already fixed width, and is not touched.
--
-- notes_fts_au (0003_search.sql) fires for each updated row and re-indexes
-- its unchanged title and note: redundant but correct, and cheap at a
-- household's outline size.

UPDATE notes_nodes
   SET created_at = substr(created_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(created_at, 20, 1) = '.'
                   THEN substr(created_at, 21, length(created_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(created_at) BETWEEN 20 AND 29
   AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(created_at) = 20
        OR (substr(created_at, 20, 1) = '.' AND length(created_at) >= 22
            AND substr(created_at, 21, length(created_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE notes_nodes
   SET updated_at = substr(updated_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(updated_at, 20, 1) = '.'
                   THEN substr(updated_at, 21, length(updated_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(updated_at) BETWEEN 20 AND 29
   AND updated_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(updated_at) = 20
        OR (substr(updated_at, 20, 1) = '.' AND length(updated_at) >= 22
            AND substr(updated_at, 21, length(updated_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE notes_nodes
   SET done_at = substr(done_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(done_at, 20, 1) = '.'
                   THEN substr(done_at, 21, length(done_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(done_at) BETWEEN 20 AND 29
   AND done_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(done_at) = 20
        OR (substr(done_at, 20, 1) = '.' AND length(done_at) >= 22
            AND substr(done_at, 21, length(done_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE notes_nodes
   SET archived_at = substr(archived_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(archived_at, 20, 1) = '.'
                   THEN substr(archived_at, 21, length(archived_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(archived_at) BETWEEN 20 AND 29
   AND archived_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(archived_at) = 20
        OR (substr(archived_at, 20, 1) = '.' AND length(archived_at) >= 22
            AND substr(archived_at, 21, length(archived_at) - 21) NOT GLOB '*[^0-9]*'));
```

- [ ] **Step 7: Run the package**

```bash
go test ./internal/apps/notes/ -count=1
```
Expected: `ok  	github.com/iliafrenkel/on-suite/internal/apps/notes`.

The prototype checked both halves of the fix. With fixed width but `julianday` kept, the archive test still fails (`[later latest earlier]`) and the migration test's `Archive` returns `[5 3 4 2 1]`. So removing the workaround is part of the fix, not tidying.

- [ ] **Step 8: Full check and commit**

```bash
git add internal/apps/notes/store.go internal/apps/notes/tree.go internal/apps/notes/archive.go internal/apps/notes/archive_test.go internal/apps/notes/migrations/0007_fixed_width_timestamps.sql internal/apps/notes/timestamps_test.go
git commit -m "fix(notes): store timestamps fixed-width, order the archive by archived_at (#356)"
```

---

### Task 6: ON Paste — helpers and migration `paste:0002`

**Files:**
- Modify: `internal/apps/paste/store.go:379-389` (and its import block)
- Create: `internal/apps/paste/migrations/0002_fixed_width_timestamps.sql`
- Test: `internal/apps/paste/timestamps_test.go` (new, `package paste_test`)

**Interfaces:**
- Consumes: `newFixture` (`store_test.go:27`), `Store.SetClock`, `Create(ctx, userID, title, language, body)`, `List(ctx, userID, limit)`, `paste.ID`, `paste.Migrations()`.
- Produces: the migration. The test helpers are `pasteLegacyTimes` and `openPasteThrough`.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/paste/timestamps_test.go`:

```go
package paste_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/paste"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// pasteLegacyTimes are values as time.RFC3339Nano wrote them before #356,
// all inside one second and listed in time order. As text they sort
// differently ('Z' sorts after every digit), which is the bug.
var pasteLegacyTimes = []string{
	"2026-09-25T12:00:05Z",
	"2026-09-25T12:00:05.123456789Z",
	"2026-09-25T12:00:05.244Z",
	"2026-09-25T12:00:05.244124Z",
	"2026-09-25T12:00:05.5Z",
}

// openPasteThrough is newFixture with Paste's schema stopped after lastID,
// so a test can seed rows the way an older binary wrote them. It returns
// every migration for the test to finish applying.
func openPasteThrough(t *testing.T, lastID string) (*sql.DB, []db.Migration, auth.User) {
	t.Helper()
	ctx := context.Background()
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	platform, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	app, err := db.Collect(paste.ID, paste.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	early := slices.Clone(platform)
	for _, m := range app {
		if m.ID <= lastID {
			early = append(early, m)
		}
	}
	if _, err := db.Apply(ctx, handle, early); err != nil {
		t.Fatal(err)
	}
	alice, err := auth.NewStore(handle).CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}
	return handle, append(platform, app...), alice
}

func TestFixedWidthMigrationRewritesPasteTimestamps(t *testing.T) {
	handle, ms, alice := openPasteThrough(t, "0001")
	ctx := context.Background()

	for i, ts := range pasteLegacyTimes {
		if _, err := handle.ExecContext(ctx,
			`INSERT INTO paste_snippets (id, user_id, title, language, body, created_at)
			 VALUES (?, ?, ?, '', 'x', ?)`, i+1, alice.ID, "s"+strconv.Itoa(i), ts); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for i, ts := range pasteLegacyTimes {
		parsed, err := db.ParseTime(ts)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		if err := handle.QueryRowContext(ctx,
			`SELECT created_at FROM paste_snippets WHERE id = ?`, i+1).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if want := db.FormatTime(parsed); got != want {
			t.Errorf("snippet %d created_at = %q, want %q (from %q)", i+1, got, want, ts)
		}
	}

	list, err := paste.NewStore(handle).List(ctx, alice.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, s := range list {
		order = append(order, s.Title)
	}
	if want := []string{"s4", "s3", "s2", "s1", "s0"}; !slices.Equal(order, want) {
		t.Errorf("List (newest first) = %v, want %v", order, want)
	}
}

// TestListNewestFirstWithinOneSecond is #356 for Paste: "second" is saved
// 124µs after "first", at the instants RFC3339Nano wrote as "…12.244Z" and
// "…12.244124Z", and must list first.
func TestListNewestFirstWithinOneSecond(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 24, 11, 0, 12, 0, time.UTC)

	f.store.SetClock(func() time.Time { return at.Add(244 * time.Millisecond) })
	if _, err := f.store.Create(ctx, f.alice.ID, "first", "", "one"); err != nil {
		t.Fatal(err)
	}
	f.store.SetClock(func() time.Time { return at.Add(244124 * time.Microsecond) })
	if _, err := f.store.Create(ctx, f.alice.ID, "second", "", "two"); err != nil {
		t.Fatal(err)
	}

	list, err := f.store.List(ctx, f.alice.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Title != "second" || list[1].Title != "first" {
		t.Errorf("List = %+v, want [second first]", list)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

```bash
go test ./internal/apps/paste/ -run 'FixedWidth|WithinOneSecond' -count=1
```
Expected: both tests fail. The migration test reports values that were never rewritten and `List … = [s0 s4 s2 s3 s1]`. `TestListNewestFirstWithinOneSecond` fails with `want [second first]`.

- [ ] **Step 3: Switch the helpers in `internal/apps/paste/store.go`**

Replace:

```go
// Timestamps match the platform's convention: RFC 3339 nanoseconds in UTC,
// which sorts chronologically as text.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("paste: parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}
```

with:

```go
// Timestamps match the platform's convention, db.TimeLayout: UTC with
// exactly nine fractional digits, which sorts chronologically as text even
// within one second (#356). List's ORDER BY created_at relies on it.
func formatTime(t time.Time) string { return db.FormatTime(t) }

func parseTime(s string) (time.Time, error) {
	t, err := db.ParseTime(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("paste: parse timestamp %q: %w", s, err)
	}
	return t, nil
}
```

and in the import block replace:

```go
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)
```

with:

```go
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)
```

- [ ] **Step 4: Create `internal/apps/paste/migrations/0002_fixed_width_timestamps.sql`**

```sql
-- internal/apps/paste/migrations/0002_fixed_width_timestamps.sql
-- Rewrites paste_snippets.created_at to db.TimeLayout's fixed width (#356).
--
-- Paste wrote it with time.RFC3339Nano, which trims trailing fractional
-- zeros, so two snippets saved within one second could list in the wrong
-- order (ORDER BY created_at DESC compares text). formatTime now writes
-- nine fraction digits always.
--
-- The UPDATE is the platform's
-- internal/platform/auth/migrations/0002_fixed_width_timestamps.sql; its
-- header explains each clause. Values already 30 wide are left alone, so
-- this is safe to run twice.

UPDATE paste_snippets
   SET created_at = substr(created_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(created_at, 20, 1) = '.'
                   THEN substr(created_at, 21, length(created_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(created_at) BETWEEN 20 AND 29
   AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(created_at) = 20
        OR (substr(created_at, 20, 1) = '.' AND length(created_at) >= 22
            AND substr(created_at, 21, length(created_at) - 21) NOT GLOB '*[^0-9]*'));
```

- [ ] **Step 5: Run the package**

```bash
go test ./internal/apps/paste/ -count=1
```
Expected: `ok  	github.com/iliafrenkel/on-suite/internal/apps/paste`.

- [ ] **Step 6: Full check and commit**

```bash
git add internal/apps/paste/store.go internal/apps/paste/migrations/0002_fixed_width_timestamps.sql internal/apps/paste/timestamps_test.go
git commit -m "fix(paste): store timestamps fixed-width and migrate existing snippets (#356)"
```

---

### Task 7: ON Reader — helpers, migration `reader:0010`, backup text

**Files:**
- Modify: `internal/apps/reader/store.go:148-160` (and its import block)
- Modify: `internal/apps/reader/export.go` (imports, `:89-91`, a new `exportTime`)
- Create: `internal/apps/reader/migrations/0010_fixed_width_timestamps.sql`
- Test: `internal/apps/reader/timestamps_test.go` (new, `package reader_test`)
- Modify (separate commit): 8 reader test files' fixture writes

**Interfaces:**
- Consumes: `newStoreFixture` (`f.store`, `f.db`, `f.alice`; `store_test.go:26`), `Subscribe`, `SaveFetchResult(ctx, reader.FetchResult{…})`, `DueFeeds(ctx, now, limit)`, `SaveItems`, `ItemsForScope(ctx, userID, reader.ScopeAll, 0, reader.FilterAll, "", 10)`, `SetStarred`, `Store.Export(ctx, userID)`. It returns the unexported `exportPayload`, whose fields `Starred`, `PublishedAt` and `StarredAt` are exported and readable from `reader_test`. Also `reader.ID` and `reader.Migrations()`.
- Produces: the migration and `func exportTime(stored string) string` (unexported). The test helpers are `readerLegacyTimes`, `openReaderThrough` and `readerOrNull`.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/reader/timestamps_test.go`:

```go
package reader_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/reader"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// readerLegacyTimes are values as time.RFC3339Nano wrote them before #356,
// all inside one second and listed in time order. As text they sort
// differently ('Z' sorts after every digit), which is the bug.
var readerLegacyTimes = []string{
	"2026-09-25T12:00:05Z",
	"2026-09-25T12:00:05.123456789Z",
	"2026-09-25T12:00:05.244Z",
	"2026-09-25T12:00:05.244124Z",
	"2026-09-25T12:00:05.5Z",
}

// openReaderThrough is newStoreFixture with Reader's schema stopped after
// lastID, so a test can seed rows the way an older binary wrote them. It
// returns every migration for the test to finish applying.
func openReaderThrough(t *testing.T, lastID string) (*sql.DB, []db.Migration, auth.User) {
	t.Helper()
	ctx := context.Background()
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	platform, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	app, err := db.Collect(reader.ID, reader.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	early := slices.Clone(platform)
	for _, m := range app {
		if m.ID <= lastID {
			early = append(early, m)
		}
	}
	if _, err := db.Apply(ctx, handle, early); err != nil {
		t.Fatal(err)
	}
	alice, err := auth.NewStore(handle).CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}
	return handle, append(platform, app...), alice
}

// readerOrNull is legacy on odd rows and NULL on even ones, so every
// nullable column is checked both ways.
func readerOrNull(i int, legacy string) any {
	if i%2 == 0 {
		return nil
	}
	return legacy
}

func TestFixedWidthMigrationRewritesReaderTimestamps(t *testing.T) {
	handle, ms, alice := openReaderThrough(t, "0009")
	ctx := context.Background()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := handle.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	for i, ts := range readerLegacyTimes {
		id := strconv.Itoa(i)
		key := i + 1
		url := "https://f" + id + ".example/feed.xml"
		exec(`INSERT INTO reader_feeds (id, url, resolved_url, title, site_url, last_fetch_at, next_fetch_at)
		      VALUES (?, ?, ?, ?, '', ?, ?)`, key, url, url, "feed "+id, readerOrNull(i, ts), ts)
		exec(`INSERT INTO reader_subs (id, user_id, feed_id, added_at) VALUES (?, ?, ?, ?)`, key, alice.ID, key, ts)
		exec(`INSERT INTO reader_items (id, feed_id, guid, url, title, published_at, fetched_at, full_fetched_at)
		      VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, key, key, "g"+id, "https://f"+id+".example/1", "item "+id, ts, ts, readerOrNull(i, ts))
		exec(`INSERT INTO reader_images (url_hash, src_url, fetched_at) VALUES (?, ?, ?)`, "i"+id, "https://img.example/"+id, readerOrNull(i, ts))
		exec(`INSERT INTO reader_item_state (user_id, item_id, read_at, starred_at) VALUES (?, ?, ?, ?)`,
			alice.ID, key, readerOrNull(i, ts), ts)
		exec(`INSERT INTO reader_feed_icons (url_hash, src_url, fetched_at) VALUES (?, ?, ?)`, "fi"+id, "https://f"+id+".example/favicon.ico", readerOrNull(i, ts))
	}
	exec(`INSERT INTO reader_daily_stats (day, user_id, fetched) VALUES ('2026-09-25', ?, 4)`, alice.ID)
	exec(`UPDATE reader_feeds SET last_modified = 'Thu, 25 Sep 2026 12:00:05 GMT' WHERE id = 1`)

	if _, err := db.Apply(ctx, handle, ms); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for i, ts := range readerLegacyTimes {
		parsed, err := db.ParseTime(ts)
		if err != nil {
			t.Fatal(err)
		}
		want := db.FormatTime(parsed)
		id := strconv.Itoa(i)
		key := i + 1
		for _, c := range []struct {
			query    string
			arg      any
			nullable bool
		}{
			{`SELECT last_fetch_at FROM reader_feeds WHERE id = ?`, key, true},
			{`SELECT next_fetch_at FROM reader_feeds WHERE id = ?`, key, false},
			{`SELECT added_at FROM reader_subs WHERE id = ?`, key, false},
			{`SELECT published_at FROM reader_items WHERE id = ?`, key, false},
			{`SELECT fetched_at FROM reader_items WHERE id = ?`, key, false},
			{`SELECT full_fetched_at FROM reader_items WHERE id = ?`, key, true},
			{`SELECT fetched_at FROM reader_images WHERE url_hash = ?`, "i" + id, true},
			{`SELECT read_at FROM reader_item_state WHERE item_id = ?`, key, true},
			{`SELECT starred_at FROM reader_item_state WHERE item_id = ?`, key, false},
			{`SELECT fetched_at FROM reader_feed_icons WHERE url_hash = ?`, "fi" + id, true},
		} {
			var got sql.NullString
			if err := handle.QueryRowContext(ctx, c.query, c.arg).Scan(&got); err != nil {
				t.Fatalf("%s [%v]: %v", c.query, c.arg, err)
			}
			if c.nullable && i%2 == 0 {
				if got.Valid {
					t.Errorf("%s [%v] = %q, want NULL left alone", c.query, c.arg, got.String)
				}
				continue
			}
			if got.String != want {
				t.Errorf("%s [%v] = %q, want %q (from %q)", c.query, c.arg, got.String, want, ts)
			}
		}
	}

	var day, lastModified string
	if err := handle.QueryRowContext(ctx, `SELECT day FROM reader_daily_stats`).Scan(&day); err != nil {
		t.Fatal(err)
	}
	if err := handle.QueryRowContext(ctx, `SELECT last_modified FROM reader_feeds WHERE id = 1`).Scan(&lastModified); err != nil {
		t.Fatal(err)
	}
	if day != "2026-09-25" || lastModified != "Thu, 25 Sep 2026 12:00:05 GMT" {
		t.Errorf("day = %q, last_modified = %q; want both untouched", day, lastModified)
	}

	st := reader.NewStore(handle)
	due, err := st.DueFeeds(ctx, time.Date(2026, 9, 25, 12, 0, 6, 0, time.UTC), 10)
	if err != nil {
		t.Fatal(err)
	}
	var order []int64
	for _, f := range due {
		order = append(order, f.ID)
	}
	if want := []int64{1, 2, 3, 4, 5}; !slices.Equal(order, want) {
		t.Errorf("DueFeeds (oldest first) = %v, want %v", order, want)
	}

	// The backup keeps the text it always had.
	payload, err := st.Export(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	var starred []string
	for _, it := range payload.Starred {
		starred = append(starred, it.StarredAt)
		if it.PublishedAt != it.StarredAt {
			t.Errorf("exported published_at %q != starred_at %q; both were seeded from one value", it.PublishedAt, it.StarredAt)
		}
	}
	want := slices.Clone(readerLegacyTimes)
	slices.Reverse(want) // most recently starred first
	if !slices.Equal(starred, want) {
		t.Errorf("exported starred_at = %v, want %v", starred, want)
	}
}

// TestDueFeedsWithinTheSameSecond is #356 for Reader: a feed due on a whole
// second is due half a second later. With RFC3339Nano, "…:00Z" <= "…:00.5Z"
// was false as text.
func TestDueFeedsWithinTheSameSecond(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://a.example/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	next := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	if err := f.store.SaveFetchResult(ctx, reader.FetchResult{
		FeedID: sub.FeedID, ResolvedURL: "https://a.example/feed.xml", Status: 200,
		FetchedAt: next.Add(-time.Hour), NextFetchAt: next,
	}); err != nil {
		t.Fatal(err)
	}

	due, err := f.store.DueFeeds(ctx, next.Add(500*time.Millisecond), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != sub.FeedID {
		t.Errorf("DueFeeds half a second after next_fetch_at = %+v, want the feed", due)
	}
}

// TestExportKeepsTimestampText pins the backup format: the database stores
// db.TimeLayout since #356, but `onsuite export` still writes the
// RFC3339Nano text it always did.
func TestExportKeepsTimestampText(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	sub, err := f.store.Subscribe(ctx, f.alice.ID, "https://a.example/feed.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC() // after Subscribe's own added_at, so the item is visible
	if _, err := f.store.SaveItems(ctx, sub.FeedID, []reader.ParsedItem{
		{GUID: "a", Title: "Saved", URL: "https://a.example/1", PublishedAt: time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)},
	}, now); err != nil {
		t.Fatal(err)
	}
	items, err := f.store.ItemsForScope(ctx, f.alice.ID, reader.ScopeAll, 0, reader.FilterAll, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("setup: %d items, want 1", len(items))
	}
	if err := f.store.SetStarred(ctx, f.alice.ID, items[0].ID, true,
		time.Date(2026, 9, 21, 9, 30, 0, 500_000_000, time.UTC)); err != nil {
		t.Fatal(err)
	}

	var stored string
	if err := f.db.QueryRowContext(ctx,
		`SELECT starred_at FROM reader_item_state WHERE item_id = ?`, items[0].ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "2026-09-21T09:30:00.500000000Z" {
		t.Fatalf("stored starred_at = %q, want db.TimeLayout", stored)
	}

	payload, err := f.store.Export(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Starred) != 1 {
		t.Fatalf("exported %d starred items, want 1", len(payload.Starred))
	}
	got := payload.Starred[0]
	if got.PublishedAt != "2026-09-20T08:00:00Z" || got.StarredAt != "2026-09-21T09:30:00.5Z" {
		t.Errorf("exported published_at = %q, starred_at = %q; want %q and %q",
			got.PublishedAt, got.StarredAt, "2026-09-20T08:00:00Z", "2026-09-21T09:30:00.5Z")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

```bash
go test ./internal/apps/reader/ -run 'FixedWidth|SameSecond|KeepsTimestampText' -count=1
```
Expected: three failures.
- `TestFixedWidthMigrationRewritesReaderTimestamps` fails: the values are not rewritten and `DueFeeds … = [2 4 3 5 1]`.
- `TestDueFeedsWithinTheSameSecond` fails with `want the feed`.
- `TestExportKeepsTimestampText` fails with `stored starred_at = "2026-09-21T09:30:00.5Z", want db.TimeLayout`.

- [ ] **Step 3: Switch the helpers in `internal/apps/reader/store.go`**

Replace:

```go
// timeFmt is RFC3339 in UTC. Stored as TEXT because SQLite has no time type
// and lexical order then matches chronological order, which the due index
// depends on.
const timeFmt = time.RFC3339Nano

func formatTime(t time.Time) string { return t.UTC().Format(timeFmt) }

func parseTime(s string) time.Time {
	t, err := time.Parse(timeFmt, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
```

with:

```go
// formatTime writes db.TimeLayout: UTC with exactly nine fractional digits.
// Stored as TEXT because SQLite has no time type, and fixed width is what
// makes lexical order match chronological order even within one second
// (#356) — which the due index, next_fetch_at <= ? and every ORDER BY
// published_at depend on.
func formatTime(t time.Time) string { return db.FormatTime(t) }

// parseTime reads a stored timestamp, or the zero time for one that does not
// parse — callers treat a missing time and a garbled one alike.
func parseTime(s string) time.Time {
	t, err := db.ParseTime(s)
	if err != nil {
		return time.Time{}
	}
	return t
}
```

(`timeFmt` has no other use: `grep -n timeFmt internal/apps/reader/*.go` finds only these lines.) In the import block replace:

```go
	"strings"
	"time"
)
```

with:

```go
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)
```

- [ ] **Step 4: Keep the backup's text in `internal/apps/reader/export.go`**

Replace the import block:

```go
import (
	"context"
	"database/sql"
	"fmt"
)
```

with:

```go
import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)
```

replace:

```go
		it.URL = url.String
		it.PublishedAt = published.String
		out.Starred = append(out.Starred, it)
```

with:

```go
		it.URL = url.String
		it.PublishedAt = exportTime(published.String)
		it.StarredAt = exportTime(it.StarredAt)
		out.Starred = append(out.Starred, it)
```

and insert immediately above `func exportedSub(`:

```go
// exportTime keeps the backup's timestamps in the RFC3339Nano text they have
// always had (trailing fractional zeros trimmed), although the database now
// stores db.TimeLayout (#356): the backup format changes only when this file
// says so. A value that does not parse is passed through as stored.
func exportTime(stored string) string {
	t, err := db.ParseTime(stored)
	if err != nil {
		return stored
	}
	return t.Format(time.RFC3339Nano)
}

```

- [ ] **Step 5: Create `internal/apps/reader/migrations/0010_fixed_width_timestamps.sql`**

```sql
-- internal/apps/reader/migrations/0010_fixed_width_timestamps.sql
-- Rewrites every ON Reader timestamp to db.TimeLayout's fixed width (#356).
--
-- Reader wrote timestamps with time.RFC3339Nano, which trims trailing
-- fractional zeros, so within one second text order and time order
-- disagreed: next_fetch_at <= ? could call a due feed not due yet, and
-- ORDER BY published_at / starred_at could swap neighbours. formatTime now
-- writes nine fraction digits always.
--
-- The UPDATEs are the platform's
-- internal/platform/auth/migrations/0002_fixed_width_timestamps.sql, one
-- per column; its header explains each clause. NULLs (last_fetch_at,
-- full_fetched_at, fetched_at, read_at, starred_at) and values already 30
-- wide are left alone, so this is safe to run twice. reader_daily_stats.day
-- is a 'YYYY-MM-DD' date, already fixed width, and is not touched;
-- reader_feeds.last_modified is an HTTP header echoed back to the server,
-- not a timestamp this app compares, and is not touched either.
--
-- reader_items_fts_au (0006_search.sql) fires for each updated article row
-- and re-indexes its unchanged title and search_text: redundant but
-- correct. Measured at about a second per 20,000 articles for the three
-- reader_items columns together.

UPDATE reader_feeds
   SET last_fetch_at = substr(last_fetch_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(last_fetch_at, 20, 1) = '.'
                   THEN substr(last_fetch_at, 21, length(last_fetch_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(last_fetch_at) BETWEEN 20 AND 29
   AND last_fetch_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(last_fetch_at) = 20
        OR (substr(last_fetch_at, 20, 1) = '.' AND length(last_fetch_at) >= 22
            AND substr(last_fetch_at, 21, length(last_fetch_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_feeds
   SET next_fetch_at = substr(next_fetch_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(next_fetch_at, 20, 1) = '.'
                   THEN substr(next_fetch_at, 21, length(next_fetch_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(next_fetch_at) BETWEEN 20 AND 29
   AND next_fetch_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(next_fetch_at) = 20
        OR (substr(next_fetch_at, 20, 1) = '.' AND length(next_fetch_at) >= 22
            AND substr(next_fetch_at, 21, length(next_fetch_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_subs
   SET added_at = substr(added_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(added_at, 20, 1) = '.'
                   THEN substr(added_at, 21, length(added_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(added_at) BETWEEN 20 AND 29
   AND added_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(added_at) = 20
        OR (substr(added_at, 20, 1) = '.' AND length(added_at) >= 22
            AND substr(added_at, 21, length(added_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_items
   SET published_at = substr(published_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(published_at, 20, 1) = '.'
                   THEN substr(published_at, 21, length(published_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(published_at) BETWEEN 20 AND 29
   AND published_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(published_at) = 20
        OR (substr(published_at, 20, 1) = '.' AND length(published_at) >= 22
            AND substr(published_at, 21, length(published_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_items
   SET fetched_at = substr(fetched_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(fetched_at, 20, 1) = '.'
                   THEN substr(fetched_at, 21, length(fetched_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(fetched_at) BETWEEN 20 AND 29
   AND fetched_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(fetched_at) = 20
        OR (substr(fetched_at, 20, 1) = '.' AND length(fetched_at) >= 22
            AND substr(fetched_at, 21, length(fetched_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_items
   SET full_fetched_at = substr(full_fetched_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(full_fetched_at, 20, 1) = '.'
                   THEN substr(full_fetched_at, 21, length(full_fetched_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(full_fetched_at) BETWEEN 20 AND 29
   AND full_fetched_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(full_fetched_at) = 20
        OR (substr(full_fetched_at, 20, 1) = '.' AND length(full_fetched_at) >= 22
            AND substr(full_fetched_at, 21, length(full_fetched_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_images
   SET fetched_at = substr(fetched_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(fetched_at, 20, 1) = '.'
                   THEN substr(fetched_at, 21, length(fetched_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(fetched_at) BETWEEN 20 AND 29
   AND fetched_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(fetched_at) = 20
        OR (substr(fetched_at, 20, 1) = '.' AND length(fetched_at) >= 22
            AND substr(fetched_at, 21, length(fetched_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_item_state
   SET read_at = substr(read_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(read_at, 20, 1) = '.'
                   THEN substr(read_at, 21, length(read_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(read_at) BETWEEN 20 AND 29
   AND read_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(read_at) = 20
        OR (substr(read_at, 20, 1) = '.' AND length(read_at) >= 22
            AND substr(read_at, 21, length(read_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_item_state
   SET starred_at = substr(starred_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(starred_at, 20, 1) = '.'
                   THEN substr(starred_at, 21, length(starred_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(starred_at) BETWEEN 20 AND 29
   AND starred_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(starred_at) = 20
        OR (substr(starred_at, 20, 1) = '.' AND length(starred_at) >= 22
            AND substr(starred_at, 21, length(starred_at) - 21) NOT GLOB '*[^0-9]*'));

UPDATE reader_feed_icons
   SET fetched_at = substr(fetched_at, 1, 19) || '.' ||
       substr(CASE WHEN substr(fetched_at, 20, 1) = '.'
                   THEN substr(fetched_at, 21, length(fetched_at) - 21)
                   ELSE '' END || '000000000', 1, 9) || 'Z'
 WHERE length(fetched_at) BETWEEN 20 AND 29
   AND fetched_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]*Z'
   AND (length(fetched_at) = 20
        OR (substr(fetched_at, 20, 1) = '.' AND length(fetched_at) >= 22
            AND substr(fetched_at, 21, length(fetched_at) - 21) NOT GLOB '*[^0-9]*'));
```

- [ ] **Step 6: Run the package**

```bash
go test ./internal/apps/reader/ -count=1
```
Expected: `ok  	github.com/iliafrenkel/on-suite/internal/apps/reader`.

- [ ] **Step 7: Full check and commit**

```bash
git add internal/apps/reader/store.go internal/apps/reader/export.go internal/apps/reader/migrations/0010_fixed_width_timestamps.sql internal/apps/reader/timestamps_test.go
git commit -m "fix(reader): store timestamps fixed-width, keep export text unchanged (#356)"
```

- [ ] **Step 8: Switch the reader test fixtures that write timestamps directly**

These are the 17 `….Format(time.RFC3339Nano)` arguments in reader tests listed in the verified table. The first command rewrites each expression `X.Format(time.RFC3339Nano)` to `db.FormatTime(X)`. The second adds the `db` import after the `reader` import, and `gofmt -w` sorts the import block. List the files explicitly: zsh doesn't word-split variables.

```bash
perl -pi -e 's/((?:[\w.]+|\((?:[^()]|\([^()]*\))*\))+)\.Format\(time\.RFC3339Nano\)/db.FormatTime($1)/g' internal/apps/reader/poll_test.go internal/apps/reader/handlers_test.go internal/apps/reader/extract_store_test.go internal/apps/reader/retention_test.go internal/apps/reader/faviconproxy_test.go internal/apps/reader/images_store_test.go internal/apps/reader/imgproxy_test.go internal/apps/reader/stats_test.go
perl -pi -e 's|^(\t"github.com/iliafrenkel/on-suite/internal/apps/reader"\n)|$1\t"github.com/iliafrenkel/on-suite/internal/platform/db"\n|' internal/apps/reader/poll_test.go internal/apps/reader/handlers_test.go internal/apps/reader/extract_store_test.go internal/apps/reader/retention_test.go internal/apps/reader/faviconproxy_test.go internal/apps/reader/images_store_test.go internal/apps/reader/imgproxy_test.go internal/apps/reader/stats_test.go
gofmt -w internal/apps/reader/*_test.go
grep -n 'RFC3339Nano' internal/apps/reader/*_test.go
grep -c 'db.FormatTime' internal/apps/reader/poll_test.go internal/apps/reader/handlers_test.go internal/apps/reader/extract_store_test.go internal/apps/reader/retention_test.go internal/apps/reader/faviconproxy_test.go internal/apps/reader/images_store_test.go internal/apps/reader/imgproxy_test.go internal/apps/reader/stats_test.go
```

Expected:
- The first `grep` lists only the three comment lines in `timestamps_test.go` (`:18`, `:188`, `:216`).
- The counts are `poll_test.go:1`, `handlers_test.go:3`, `extract_store_test.go:1`, `retention_test.go:2`, `faviconproxy_test.go:1`, `images_store_test.go:2`, `imgproxy_test.go:1`, `stats_test.go:6`, 17 in total.

A few rewritten lines keep a now-redundant `.UTC()` inside the call, such as `db.FormatTime(time.Now().UTC().Add(time.Hour))`. That is harmless, because `FormatTime` converts to UTC anyway. Leave them.

```bash
go vet ./internal/apps/reader/ && go test ./internal/apps/reader/ -count=1
```
Expected: `ok`.

- [ ] **Step 9: Full check and commit**

```bash
git add internal/apps/reader/poll_test.go internal/apps/reader/handlers_test.go internal/apps/reader/extract_store_test.go internal/apps/reader/retention_test.go internal/apps/reader/faviconproxy_test.go internal/apps/reader/images_store_test.go internal/apps/reader/imgproxy_test.go internal/apps/reader/stats_test.go
git commit -m "test(reader): write fixture timestamps with db.FormatTime (#356)"
```

---

### Task 8: Contain `time.RFC3339` in an arch test, and record the rule

**Files:**
- Modify: `internal/arch/arch_test.go` (import block, a new test at the end)
- Modify: `AGENTS.md:100-108`, `PATTERNS.md` (after the "Real-SQLite-file store tests" entry, `:67-70`)

**Interfaces:**
- Produces: `TestRFC3339IsContained`. It parses every non-test `.go` file, skipping `.git`, `docs`, `dist` and `testdata` as `TestReadabilityIsContained` does. It finds `time.RFC3339` and `time.RFC3339Nano` selectors and requires exactly three files: `internal/apps/reader/export.go`, `internal/platform/admin/format.go` and `internal/platform/db/timefmt.go`.

- [ ] **Step 1: Add the test**

In `internal/arch/arch_test.go`, replace:

```go
import (
	"go/parser"
```

with:

```go
import (
	"go/ast"
	"go/parser"
```

and append at the end of the file:

```go
// TestRFC3339IsContained: stored timestamps go through db.FormatTime, whose
// fixed-width db.TimeLayout is what makes text order time order (#356).
// time.RFC3339Nano trims trailing fractional zeros, and a value written with
// it compares wrongly against its neighbours within one second — the bug
// that issue fixed across every app. So production code names time.RFC3339
// or time.RFC3339Nano only where the choice is deliberate: db.ParseTime
// (which accepts both widths) and the two places that keep a user-visible
// text as it always was. A new use is either storage, which should call
// db.FormatTime, or a display decision that belongs on this list.
func TestRFC3339IsContained(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	var users []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "docs", "dist", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil // unrelated to this check, as in TestReadabilityIsContained
		}
		found := false
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "RFC3339" && sel.Sel.Name != "RFC3339Nano") {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "time" {
				found = true
			}
			return true
		})
		if found {
			rel, _ := filepath.Rel(root, path)
			users = append(users, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"internal/apps/reader/export.go",    // backup text unchanged by #356
		"internal/platform/admin/format.go", // admin page text unchanged by #356
		"internal/platform/db/timefmt.go",   // ParseTime reads both widths
	}
	if !slices.Equal(users, want) {
		t.Errorf("time.RFC3339/RFC3339Nano used in %v, want only %v", users, want)
	}
}
```

- [ ] **Step 2: Run it, then prove it bites**

```bash
go test ./internal/arch/ -count=1
```
Expected: `ok`. Tasks 1–7 removed every other use.

To see it fail, add `var _ = time.RFC3339Nano` to the end of `internal/apps/paste/store.go` and rerun. Expected: `time.RFC3339/RFC3339Nano used in [internal/apps/paste/store.go internal/apps/reader/export.go …]`. Then **remove the line**. `git diff internal/apps/paste/store.go` must print nothing.

- [ ] **Step 3: Record the rule in `AGENTS.md`**

Replace:

```
against a real SQLite file in a temp dir, not `:memory:`, because the bugs
that matter live in SQL and WAL behavior `:memory:` doesn't reproduce.
```

with:

```
against a real SQLite file in a temp dir, not `:memory:`, because the bugs
that matter live in SQL and WAL behavior `:memory:` doesn't reproduce.
Timestamps are TEXT in `db.TimeLayout` — UTC, always nine fractional
digits, 30 characters — written with `db.FormatTime` and read with
`db.ParseTime` ([internal/platform/db/timefmt.go](internal/platform/db/timefmt.go));
fixed width is what makes `ORDER BY`, `<`, `min()` and `max()` on the text
agree with time order, including within one second (#356). Never store a
time formatted with `time.RFC3339Nano`, which trims trailing zeros;
`TestRFC3339IsContained` in the arch test enforces that. Date-only columns
use `"2006-01-02"`. A migration that adds a timestamp column needs nothing
special; one that backfills one must write the same 30-character form.
```

- [ ] **Step 4: Add the pattern to `PATTERNS.md`**

Directly after the entry that ends `` `internal/apps/notes/store_test.go`'s test fixture setup.``, insert, with a blank line before and after:

```
- **Stored timestamps through `db.FormatTime`** — reach for this whenever a
  time goes into (or is compared against) a TEXT column: each package keeps
  a one-line `formatTime`/`parseTime` wrapping `db.FormatTime`/
  `db.ParseTime`, whose fixed-width `db.TimeLayout` makes text order time
  order (#356). Canonical: `internal/platform/db/timefmt.go`, wrapped in
  `internal/apps/flash/store.go`.
```

- [ ] **Step 5: Full check and commit (two commits)**

```bash
git add internal/arch/arch_test.go
git commit -m "test(platform): contain time.RFC3339 to its three deliberate uses (#356)"
git add AGENTS.md PATTERNS.md
git commit -m "docs: record the fixed-width timestamp storage rule (#356)"
```

---

### Task 9: Full check, upgrade rehearsal, manual verification, PR

- [ ] **Step 1: Full check**

```bash
gofmt -l .                                              # prints nothing
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
go mod tidy && git diff --exit-code go.mod go.sum
go test ./internal/arch/... -count=1
go test ./... -race -count=1
```
Expected: no output from `gofmt`, no diff, and `ok` for every package. The prototype ran the whole suite green, and staticcheck was clean.

- [ ] **Step 2: Stress the original flake once more**

```bash
go test ./internal/apps/flash/ -run '^TestQueueFrontAgreesWithDueQueue$' -count=2000
```
Expected: `ok` (about 3 minutes).

- [ ] **Step 3: Upgrade rehearsal on the existing manual-verify database**

`/tmp/onsuite-manual-verify/onsuite.db` was written by pre-fix binaries, so it holds legacy rows. First keep a copy, following DEPLOYING.md's "snapshot first". Then export with `main`, export again with this branch (which runs the new migrations), and compare.

```bash
cp -R /tmp/onsuite-manual-verify /tmp/onsuite-manual-verify.pre356
rm -rf /tmp/onsuite-main-src && mkdir -p /tmp/onsuite-main-src && git archive main | tar -x -C /tmp/onsuite-main-src
(cd /tmp/onsuite-main-src && go build -o /tmp/onsuite-main ./cmd/onsuite)
go build -o /tmp/onsuite-bin ./cmd/onsuite
U=$(sqlite3 /tmp/onsuite-manual-verify/onsuite.db "SELECT username FROM users ORDER BY id LIMIT 1")
/tmp/onsuite-main export "$U" --data-dir /tmp/onsuite-manual-verify --out /tmp/export-before.json
/tmp/onsuite-bin export "$U" --data-dir /tmp/onsuite-manual-verify --out /tmp/export-after.json
python3 -c 'import json; a=json.load(open("/tmp/export-before.json")); b=json.load(open("/tmp/export-after.json")); a.pop("exported_at"); b.pop("exported_at"); print("export identical:", a == b)'
```
Expected: `export identical: True`. The prototype got this on a 4 MB dev database with 250 articles.

Then check that every timestamp column is 30 characters wide, and that the database and both FTS indexes are intact:

```bash
sqlite3 /tmp/onsuite-manual-verify/onsuite.db "
SELECT 'users.created_at', sum(length(created_at) <> 30) FROM users UNION ALL
SELECT 'sessions.created_at', sum(length(created_at) <> 30) FROM sessions UNION ALL
SELECT 'sessions.expires_at', sum(length(expires_at) <> 30) FROM sessions UNION ALL
SELECT 'schema_migrations.applied_at', sum(length(applied_at) <> 30) FROM schema_migrations UNION ALL
SELECT 'flash_decks.created_at', sum(length(created_at) <> 30) FROM flash_decks UNION ALL
SELECT 'flash_decks.snoozed_until', sum(length(snoozed_until) <> 30) FROM flash_decks UNION ALL
SELECT 'flash_cards.created_at', sum(length(created_at) <> 30) FROM flash_cards UNION ALL
SELECT 'flash_card_state.due_at', sum(length(due_at) <> 30) FROM flash_card_state UNION ALL
SELECT 'flash_card_state.last_review_at', sum(length(last_review_at) <> 30) FROM flash_card_state UNION ALL
SELECT 'flash_card_state.log_due', sum(length(log_due) <> 30) FROM flash_card_state UNION ALL
SELECT 'flash_card_state.log_review', sum(length(log_review) <> 30) FROM flash_card_state UNION ALL
SELECT 'flash_media.fetched_at', sum(length(fetched_at) <> 30) FROM flash_media UNION ALL
SELECT 'flash_shares.created_at', sum(length(created_at) <> 30) FROM flash_shares UNION ALL
SELECT 'flash_shares.responded_at', sum(length(responded_at) <> 30) FROM flash_shares UNION ALL
SELECT 'notes_nodes.created_at', sum(length(created_at) <> 30) FROM notes_nodes UNION ALL
SELECT 'notes_nodes.updated_at', sum(length(updated_at) <> 30) FROM notes_nodes UNION ALL
SELECT 'notes_nodes.done_at', sum(length(done_at) <> 30) FROM notes_nodes UNION ALL
SELECT 'notes_nodes.archived_at', sum(length(archived_at) <> 30) FROM notes_nodes UNION ALL
SELECT 'paste_snippets.created_at', sum(length(created_at) <> 30) FROM paste_snippets UNION ALL
SELECT 'reader_feeds.last_fetch_at', sum(length(last_fetch_at) <> 30) FROM reader_feeds UNION ALL
SELECT 'reader_feeds.next_fetch_at', sum(length(next_fetch_at) <> 30) FROM reader_feeds UNION ALL
SELECT 'reader_subs.added_at', sum(length(added_at) <> 30) FROM reader_subs UNION ALL
SELECT 'reader_items.published_at', sum(length(published_at) <> 30) FROM reader_items UNION ALL
SELECT 'reader_items.fetched_at', sum(length(fetched_at) <> 30) FROM reader_items UNION ALL
SELECT 'reader_items.full_fetched_at', sum(length(full_fetched_at) <> 30) FROM reader_items UNION ALL
SELECT 'reader_images.fetched_at', sum(length(fetched_at) <> 30) FROM reader_images UNION ALL
SELECT 'reader_item_state.read_at', sum(length(read_at) <> 30) FROM reader_item_state UNION ALL
SELECT 'reader_item_state.starred_at', sum(length(starred_at) <> 30) FROM reader_item_state UNION ALL
SELECT 'reader_feed_icons.fetched_at', sum(length(fetched_at) <> 30) FROM reader_feed_icons;
PRAGMA integrity_check;
INSERT INTO notes_fts(notes_fts) VALUES('integrity-check');
INSERT INTO reader_items_fts(reader_items_fts) VALUES('integrity-check');"
```
Expected: every row ends in `|0` or `|` (the latter for an empty table or all-NULL column). Then `ok`, and no error from either FTS `integrity-check`.

- [ ] **Step 4: Manual verification in the browser.** Start the `onsuite` preview from `.claude/launch.json` (`/tmp/onsuite-bin serve`, data dir `/tmp/onsuite-manual-verify`) and ask the user to sign in.
  1. **Paste:** the list order and "… ago" labels look as they did. Create two snippets quickly; the newer one is on top.
  2. **Notes:** archive two bullets a moment apart. `/notes/archive` lists the more recent one first. Due dates and done ticks are unchanged.
  3. **Reader:** the article list, dates (`2 Jan 2006` style), unread counts and the stats page look as before. "Refresh feed" still works.
  4. **Flash:** the deck list order is unchanged. Import a small deck of 5 cards and start its review: the new cards come in import order. The review screen's "due" count matches the deck card.
  5. **Admin** (`/admin/`): the migration table lists `platform:0002`, `flash:0012`, `notes:0007`, `paste:0002` and `reader:0010`. Its "applied at" values have **no** trailing zero padding (e.g. `…:39.801891Z`), the same as before. There are no console or CSP errors.

  Afterwards, `rm -rf /tmp/onsuite-manual-verify.pre356 /tmp/onsuite-main-src /tmp/onsuite-main /tmp/export-before.json /tmp/export-after.json` once the user is happy. Or keep the `.pre356` copy if they want to rehearse again.

- [ ] **Step 5: Open the PR**

```bash
git push -u origin fix/356-fixed-width-timestamps
gh pr create --title "fix: store timestamps fixed-width so text order is time order (#356)" --body "$(cat <<'EOF'
## Summary
- **Root cause (#356):** timestamps were stored as `time.RFC3339Nano` text, which trims trailing fractional zeros, so `…12.244Z` sorted *after* `…12.244124Z` (`'Z'` > any digit). Every `ORDER BY`, `<`, `<=`, `min()`/`max()` on those TEXT columns could disagree with time order within one second — the ~1/1000 `TestQueueFrontAgreesWithDueQueue` flake, imported/gifted Flash cards queuing out of order, and sub-second `due_at`/`next_fetch_at`/`expires_at` cut-offs misjudged.
- **One helper:** `internal/platform/db` gains `TimeLayout` (`2006-01-02T15:04:05.000000000Z07:00`), `FormatTime` (UTC, always 30 characters) and `ParseTime` (accepts the new form *and* legacy RFC3339Nano). Every package's `formatTime`/`parseTime` now delegates to them, and `db.Apply` records `applied_at` with it.
- **Existing rows rewritten** by one forward-only SQL migration per namespace — `platform:0002`, `flash:0012`, `notes:0007`, `paste:0002`, `reader:0010` — padding each of 29 timestamp columns in place (`substr`/`GLOB`; `strftime('%f')` would drop sub-millisecond digits). Idempotent; NULLs and unexpected values are left alone; a property test checks the SQL against `FormatTime` over 2,000 random values. Date-only columns (`flash_review_counts.day`, `notes_nodes.due_on`, `reader_daily_stats.day`) are already fixed width and untouched.
- **Workaround removed:** Notes' archive orders by `archived_at` again instead of `julianday()` (#108), which only resolved milliseconds.
- **No visible change:** UI labels come from parsed times. The Reader backup and the admin migration table print stored text, so both re-render it as RFC3339Nano — `onsuite export` of a real database is byte-identical before and after.
- **Guard:** `TestRFC3339IsContained` allows `time.RFC3339`/`RFC3339Nano` in production code only in `db/timefmt.go`, `admin/format.go` and `reader/export.go`. AGENTS.md and PATTERNS.md record the rule.

Startup cost: the migrations run once, in a transaction, at the next start (~1s per 20k Reader articles, since the FTS update trigger re-indexes each rewritten row). Rolling back means restoring the pre-upgrade snapshot, as DEPLOYING.md already says.

Not changed: Flash share "latest" ordering (`created_at DESC, id DESC`) is now correct within a second; the clock-step-back case from the issue's first comment is left for a follow-up.

No new dependencies, no template/CSS/HTMX changes.

Closes #356

Plan: docs/superpowers/plans/2026-09-25-fixed-width-timestamps.md

## Test plan
- [ ] `go test ./... -race -count=1`
- [ ] `go test ./internal/apps/flash/ -run '^TestQueueFrontAgreesWithDueQueue$' -count=2000`
- [ ] Upgrade rehearsal + manual checklist from the plan's Task 9
EOF
)"
```

---

## Self-review

- **Coverage:**
  - Shared helper → Task 1. Tests cover the exact 30-character output for 0/1/3/6/9 digits and for a non-UTC input; a round trip of 1,000 random times; legacy parse including a `+10:00` offset; garbage rejected; the ordering property over 200×20 same-second times, which also checks that the issue's exact pair was backwards under RFC3339Nano; and `applied_at`.
  - Every writer switched: `migrate.go` (Task 1), auth (3), flash (4), notes (5), paste (6), reader (7). The verified table's grep row shows there is no other writer, and `TestRFC3339IsContained` (Task 8) keeps it that way.
  - Migrations: all 29 columns in the inventory, one file per namespace.
    - Each app test seeds the five legacy shapes (whole second, 9, 3, 6 and 1 digits) **before** its migration runs, then asserts: exact rewritten text, NULLs preserved, date-only and non-timestamp columns untouched, the store reading the rows back, and text order equal to time order.
    - The platform test also covers the property (2,000 random values), idempotency, and odd values left alone (an offset, a non-digit, an empty fraction, already-fixed, garbage).
    - Notes and Reader check that the FTS index survives the rewrite (notes `Search`; FTS `integrity-check` in Task 9).
  - Deterministic flake reproduction → Task 4, `TestNewCardsQueueInCreationOrderWithinOneSecond`, using the issue's exact `…12.244Z`/`…12.244124Z` instants. Also in Task 4: deck order, import order and due-ness. Other apps: session sweep (3), archive order (5), list order (6), `DueFeeds` (7). The prototype confirmed that each fails with the old `formatTime` and passes with the new one.
  - Export and display text unchanged → Tasks 2 and 7 have unit tests, plus the real-database export diff (Task 9).
  - Workaround removed → Task 5. It was shown to be *necessary*, not just tidy: `julianday` fails the 4µs case even with fixed-width data.
  - Docs → Task 8.
- **Placeholders:** none. Every new file is given complete, including all five SQL files verbatim. Every edit is an exact before/after, and every command has its expected output. The Go and SQL here were compiled and run in a prototype copy of the tree at `77eab10`: full `go test ./... -race`, `go vet`, pinned staticcheck and `gofmt` were all clean. Then they were transcribed into this plan.
- **Type consistency:**
  - `db.FormatTime(time.Time) string` and `db.ParseTime(string) (time.Time, error)` are used identically by `migrate.go`, the five package wrappers, `appliedAtLabel`, `exportTime` and every test.
  - The package wrappers keep their signatures, so none of the ~60 existing call sites changes.
  - The test helper names are namespaced per package (`legacyTimes`/`openPlatformThrough` in `auth`; `flashLegacyTimes`/`openFlashThrough`/`orNull`; `notesLegacyTimes`/`openNotesThrough`; `pasteLegacyTimes`/`openPasteThrough`; `readerLegacyTimes`/`openReaderThrough`/`readerOrNull`) and compiled without collisions.
- **Ordering:**
  - Every commit is self-consistent: a package's writer switch and its migration land together.
  - Task 2 (admin label) lands before Task 3 rewrites `applied_at`, so the admin page's text never changes on any commit.
  - The arch test lands last, once nothing outside its allowlist is left.
- **Harness gotcha respected:** every clock-dependent test uses a store-level fixture (`newFixture`/`newStore`/`newStoreFixture`); none uses `s.Store.SetClock` in a handler test.
- **Risks, for the PR conversation:**
  - **Startup migration time and WAL growth.** Each rewritten page goes through the WAL once in the migration's transaction, so on a large Reader table the WAL briefly grows to about the size of the rewritten tables before the next checkpoint. That is fine for a household database. It's worth a line in the release notes.
  - **Rollback.** Forward-only, as ever: restore the pre-upgrade snapshot. If an older binary is run against a migrated database anyway, it reads everything fine (RFC3339Nano parsing accepts nine digits), but it writes variable-width rows again. Re-upgrading won't re-run the recorded migrations, so those rows stay legacy. The SQL is idempotent and can be re-run by hand.
  - **Values the rewrite skips** (offsets, garbage) stay as they are. Nothing in the codebase ever wrote them, and `ParseTime` still reads offsets.
  - **Year > 9999 or < 0** would not be 30 characters. No writer produces them, because Reader clamps a missing published date to `now`.
- **Follow-ups (not in this PR):** the Flash share "latest" ordering under a clock step back (issue comment 1).
