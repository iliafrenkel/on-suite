# App Test Clock Implementation Plan (#357)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a handler test control the clock the app under test actually reads, in every app, so no test needs a real-wall-clock window again.

**Architecture:** `app.Deps` gains `Now func() time.Time` (nil in production = real clock). Each app's `Mount` hands a non-nil `deps.Now` to its own Store via `SetClock`, and every clock read in an app — store, handlers, poller, jobs — goes through that Store's `now()`. `apptest` gets a `Clock` that follows real time until a test pins it; `NewServer` always wires it into both `Deps.Now` and the harness's `s.Store`, exposed as `s.Clock`. An arch test keeps `time.Now` out of app code except each app's `NewStore` default.

**Tech Stack:** Go, SQLite, `internal/apptest` harness.

## Global Constraints

- Production behaviour is unchanged: `cmd/onsuite/stack.go` leaves `Deps.Now` nil.
- Clock values stay UTC (`time.Now().UTC()`), as every Store default already is.
- notes' handlers currently use local `time.Now()` for due/overdue dates; keep that by calling `a.store.now().Local()` — do not change it to UTC.
- Stored timestamps keep using `db.FormatTime`/`db.ParseTime` (#366 arch test).
- Handler tests' `s.Store.SetClock` alone does NOT reach the app — tests must use `s.Clock`. (This is the bug being fixed; don't write new tests that rely on the old behaviour.)
- Full check (AGENTS.md) green on every commit: `gofmt -l .`, `go vet ./...`, `go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...`, `go mod tidy && git diff --exit-code go.mod go.sum`, `go test ./... -race -count=1`.
- Branch: `fix/357-app-test-clock`. Commit messages end with the repo's usual attribution.

---

### Task 1: `Deps.Now` and `apptest.Clock`

**Files:**
- Modify: `internal/platform/app/app.go` (`Deps` struct, ~line 76)
- Modify: `internal/apptest/apptest.go` (`Server`, `NewServer`)
- Create: `internal/apptest/clock.go`
- Test: `internal/apptest/clock_test.go`, `internal/apptest/apptest_test.go`

**Interfaces:**
- Produces: `app.Deps.Now func() time.Time`; `apptest.Clock` with `Now() time.Time`, `Set(time.Time)`, `Advance(time.Duration)`; `Server[S].Clock *apptest.Clock`.

- [ ] **Step 1: Write failing Clock tests** (`internal/apptest/clock_test.go`)

```go
package apptest_test

import (
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apptest"
)

func TestClockFollowsRealTimeUntilSet(t *testing.T) {
	var c apptest.Clock
	before := time.Now().UTC()
	got := c.Now()
	after := time.Now().UTC()
	if got.Before(before) || got.After(after) {
		t.Fatalf("unset clock = %v, want between %v and %v", got, before, after)
	}
	if got.Location() != time.UTC {
		t.Errorf("unset clock location = %v, want UTC", got.Location())
	}
}

func TestClockSetAndAdvance(t *testing.T) {
	var c apptest.Clock
	pinned := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	c.Set(pinned)
	if got := c.Now(); !got.Equal(pinned) {
		t.Fatalf("Now after Set = %v, want %v", got, pinned)
	}
	c.Advance(90 * time.Minute)
	if got, want := c.Now(), pinned.Add(90*time.Minute); !got.Equal(want) {
		t.Fatalf("Now after Advance = %v, want %v", got, want)
	}
}

func TestClockSetConvertsToUTC(t *testing.T) {
	var c apptest.Clock
	c.Set(time.Date(2026, 3, 10, 23, 0, 0, 0, time.FixedZone("AEDT", 11*3600)))
	if got := c.Now(); got.Location() != time.UTC || got.Hour() != 12 {
		t.Fatalf("Now = %v, want 12:00 UTC", got)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/apptest/ -run Clock` — expect compile failure (`apptest.Clock` undefined).

- [ ] **Step 3: Implement** `internal/apptest/clock.go`

```go
package apptest

import (
	"sync"
	"time"
)

// Clock is the time source NewServer hands both the app under test (through
// app.Deps.Now) and the harness's own Store. Until a test calls Set or
// Advance it simply follows the real clock, so tests that never touch it
// behave exactly as before (#357).
//
// Safe for concurrent use: handlers and jobs read it from other goroutines
// under -race.
type Clock struct {
	mu     sync.Mutex
	pinned bool
	t      time.Time
}

// Now returns the pinned time, or the real time (UTC) if nothing pinned it.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.pinned {
		return time.Now().UTC()
	}
	return c.t
}

// Set pins the clock at t (converted to UTC, like every Store's default).
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pinned, c.t = true, t.UTC()
}

// Advance moves the clock forward by d, pinning it first at the real time
// if it was not already pinned.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.pinned {
		c.pinned, c.t = true, time.Now().UTC()
	}
	c.t = c.t.Add(d)
}
```

- [ ] **Step 4: Add `Deps.Now`** in `internal/platform/app/app.go`, after `Secure bool`:

```go
	// Now, if set, is the clock the app must read instead of the real one.
	// Only tests set it (apptest.NewServer, issue #357); production leaves
	// it nil and each app keeps its Store's real-UTC default. An app's Mount
	// passes a non-nil Now to its Store's SetClock, and every clock read in
	// the app goes through that Store.
	Now func() time.Time
```

(add `"time"` to the imports).

- [ ] **Step 5: Wire it in the harness** (`internal/apptest/apptest.go`):
  - Add field to `Server[S]`, with doc: `Clock *Clock // the app's clock and s.Store's; pin it with Set/Advance`.
  - In `NewServer`, before `registry.Mount`: `clock := &Clock{}`; pass `Now: clock.Now` in the `app.Deps{...}` literal.
  - After building `store := newStore(handle)`:

```go
	// The harness's own Store is a separate instance from the one the app
	// builds in Mount; give it the same clock so a test's writes through
	// s.Store and the handler's reads agree on "now".
	if c, ok := any(store).(interface{ SetClock(func() time.Time) }); ok {
		c.SetClock(clock.Now)
	}
```
  - Set `Store: store, Clock: clock` on the returned Server. Update the `Server` doc comment to mention `Clock`.

- [ ] **Step 6: Harness test** in `internal/apptest/apptest_test.go`: a stub app (implementing `app.App` with whatever minimal methods the interface needs — copy the smallest existing stub in `internal/platform/app/app_test.go`) whose one route `GET /` writes `deps.Now().Format("2006-01-02 15:04")`. The test pins `s.Clock.Set(2026-03-10 12:00 UTC)`, GETs the route as Alice, asserts body `2026-03-10 12:00`. Also assert a store type with `SetClock` (a tiny test-local `type clockStore struct{ now func() time.Time }` with `SetClock`) receives the same clock: `s.Store.now()` equals the pinned time.

- [ ] **Step 7: Run** `go test ./internal/apptest/ ./internal/platform/app/ -race -count=1` — PASS.

- [ ] **Step 8: Commit** `test(apptest): shared Clock wired into Deps.Now and the harness store (#357)`

---

### Task 2: flash, notes, paste honour `Deps.Now`

**Files:**
- Modify: `internal/apps/flash/flash.go` (Mount, ~line 139), `internal/apps/notes/app.go` (Mount, ~line 114), `internal/apps/paste/paste.go` (Mount, ~line 63)
- Modify: `internal/apps/notes/handlers.go` lines ~144, 165, 234, 248, 480, 753 (`time.Now()` → `a.store.now().Local()`)
- Test: `internal/apps/paste/handlers_test.go`, `internal/apps/notes/handlers_test.go`, flash tests listed below

**Interfaces:**
- Consumes: `app.Deps.Now`, `Server.Clock` (Task 1).

- [ ] **Step 1: Failing paste test** — pin `s.Clock.Set(time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC))`, create a snippet via the paste create POST (copy the form used by an existing create test in `handlers_test.go`), GET the snippet page, assert the `<time>` text is `10 Mar 2026 12:00`. Run → FAIL (handler store uses real clock).

- [ ] **Step 2: Mount wiring** — in each of the three `Mount`s, right after `a.store = NewStore(deps.DB)`:

```go
	if deps.Now != nil {
		a.store.SetClock(deps.Now)
	}
```

- [ ] **Step 3: notes handlers** — replace each `time.Now()` in `internal/apps/notes/handlers.go` with `a.store.now().Local()` (Local keeps today's server-local due-date behaviour). Drop the `time` import if unused.

- [ ] **Step 4: notes test** — rewrite `TestAFutureDueChipIsNotMarkedOverdue` and `TestOverdueChipIsMarked` to pin `s.Clock.Set(2026-03-10 12:00 UTC)` and use due dates `2026-03-11` (not overdue) / `2026-03-09` (overdue); update the comment above `TestOverdueChipIsMarked` (no longer "year 2000"). Add a third case: due `2026-03-10` is not overdue (today).

- [ ] **Step 5: Convert the flash wall-clock workarounds** to a pinned clock. For each, delete the "SetClock has no effect / real wall clock" comment, add `s.Clock.Set(t0)` with a fixed `t0 := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)`, grade/snooze at explicit times relative to `t0`, then `s.Clock.Set(...)`/`s.Clock.Advance(...)` to the moment the page is viewed:
  - `TestReviewedCardHasNoCorner` (`handlers_review_test.go` ~360): grade at `t0`, `s.Clock.Advance(3*time.Hour)`, GET.
  - `TestReviewOfASnoozedDeckSaysItIsOnABreak` (~558): `until := t0.AddDate(0, 0, 7)`.
  - `TestReviewAllShowsTheMostOverdueCardFirst` (~606): replace `now := time.Now().UTC()` with `t0`, pin before any write.
  - `TestStatsPageRendersConsistentNumbers` (`handlers_stats_test.go` ~83): grade at `t0`…, then `s.Clock.Set(t0.Add(3*time.Hour))` before the GET; rewrite the long comment to one line.
  - `TestSnoozeDaysMustBeBetweenOneAndAYear` (`handlers_decks_test.go` ~633): replace the `before`/`after` window with an exact `t0.AddDate(0, 0, 365)` assertion.
  - Then `grep -rn "time.Now" internal/apps/flash/*_test.go` — any other handler test (one using `newServer`/`apptest.NewServer`) that computes "now" to line up with the handler: convert the same way. Store-level tests (`newStoreFixture`, direct `SetClock`) stay as they are.

- [ ] **Step 6: Run** `go test ./internal/apps/flash/ ./internal/apps/notes/ ./internal/apps/paste/ -race -count=1` — PASS. Sanity check the conversions really depend on the clock: temporarily comment out the flash Mount `SetClock` lines, rerun flash tests, confirm at least the snooze and stats tests FAIL; restore.

- [ ] **Step 7: Commit** `fix(flash,notes,paste): apps read the clock from Deps.Now (#357)`

---

### Task 3: reader gets a clock

**Files:**
- Modify: `internal/apps/reader/store.go` (Store struct ~line 51, NewStore, Subscribe ~206)
- Modify: `internal/apps/reader/stats.go` (~150), `poll.go` (~95, ~147), `app.go` (Mount ~85, jobs ~153-156), `handlers.go` (~490, 509, 533, 548, 1069, 1111), `imgproxy.go` (~105, 141), `faviconproxy.go` (~79, 110)
- Test: `internal/apps/reader/stats_test.go`, `internal/apps/reader/handlers_test.go`

**Interfaces:**
- Produces: `(*reader.Store).SetClock(func() time.Time)`, unexported `(*Store).now()`-style field `now func() time.Time` matching flash/notes/paste.

- [ ] **Step 1: Failing test** in `stats_test.go`: in the test at ~line 15 that backdates `added_at` with a raw `UPDATE`, replace that with `f.store.SetClock(func() time.Time { return now.Add(-24 * time.Hour) })` before `Subscribe`, then `f.store.SetClock(func() time.Time { return now })` after; delete the backdating comment and `UPDATE`. Run → compile failure (`SetClock` undefined).

- [ ] **Step 2: Store clock** — `store.go`:

```go
// Store is the only thing in this package that touches SQL.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(handle *sql.DB) *Store {
	return &Store{db: handle, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the time source, for tests.
func (s *Store) SetClock(now func() time.Time) { s.now = now }
```

Replace `time.Now().UTC()` with `s.now()` in `Subscribe` and `DailyStats`.

- [ ] **Step 3: Everything else** — `poll.go`: `p.store.now()`; `app.go` jobs and `handlers.go`/`imgproxy.go`/`faviconproxy.go`: `a.store.now()`. Mount: same `if deps.Now != nil { a.store.SetClock(deps.Now) }` right after `NewStore`, before `NewPoller`. `grep -n "time.Now" internal/apps/reader/*.go | grep -v _test` must print only the `NewStore` default.

- [ ] **Step 4: Handler test** — add `TestMarkReadUsesTheAppClock` in `handlers_test.go`: pin `s.Clock.Set(t0)`, subscribe + save one item via `s.Store` (copy the seeding used by the nearest existing mark-read test), POST the mark-read route, then read the item's `read_at` via `s.Store.DB()` and assert it equals `db.FormatTime(t0)`. Also look at the tests around lines 1426 and 1460 (comments about wall-clock order): if pinning `s.Clock` makes their workaround unnecessary, simplify them; otherwise leave them.

- [ ] **Step 5: Run** `go test ./internal/apps/reader/ -race -count=1` — PASS.

- [ ] **Step 6: Commit** `fix(reader): route every clock read through the Store (#357)`

---

### Task 4: arch test + docs

**Files:**
- Modify: `internal/arch/arch_test.go` (new test after `TestRFC3339IsContained`)
- Modify: `AGENTS.md` (testing section), `PATTERNS.md` (new entry)

- [ ] **Step 1: Arch test** `TestAppsReadTheirStoreClock`: walk `internal/apps` non-`_test.go` files the same way `TestRFC3339IsContained` walks the repo (reuse its `ast.Inspect` shape), collecting files with a `time.Now` selector. Want exactly:

```go
	want := []string{
		"internal/apps/flash/store.go",
		"internal/apps/notes/store.go",
		"internal/apps/paste/store.go",
		"internal/apps/reader/store.go",
	}
```

Failure message: `"time.Now outside an app's NewStore default: %v — read the clock through the Store (a.store.now()) so apptest's Clock reaches it (#357)"`. Run it; PASS. Temporarily add `_ = time.Now()` to `internal/apps/paste/handlers.go`, confirm FAIL, revert.

- [ ] **Step 2: AGENTS.md** — next to the handler-test guidance, add:

> Handler tests control time with `s.Clock` (`apptest.Clock`): `s.Clock.Set(t)` / `s.Clock.Advance(d)` pins the clock for both the app under test and `s.Store`. Never compute "now" from the real clock to line up with a handler, and don't call `s.Store.SetClock` in a handler test — it doesn't reach the app. App code reads time only through its Store's `now()` (arch test `TestAppsReadTheirStoreClock`).

- [ ] **Step 3: PATTERNS.md** — entry "**Pinned test clock** — reach for this whenever a handler test depends on 'now' … Canonical: `internal/apptest/clock.go`, used by `TestReviewOfASnoozedDeckSaysItIsOnABreak`."

- [ ] **Step 4: Full check** (Global Constraints). All green.

- [ ] **Step 5: Commit** `test(arch): apps read time only through their Store's clock (#357)`
