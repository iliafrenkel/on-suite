# ON Focus F3 — History and stats Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record finished and exited sessions on the server, and show them: a today strip on the home page, a History page (totals, 30-day chart, time per timer, recent sessions with delete), an `Exporter` and an admin card. The home banner records a session that ran out, offers Retry when saving failed, and gets an End action (#546).

**Architecture:** The browser still owns the running session. When it ends (finished, Exit, End on the banner, or replaced by another timer), a new shared script `record.js` POSTs a summary as JSON to `POST /focus/sessions` with `fetch(…, {keepalive: true})`; `localStorage` is cleared only once the server has it. The server validates and stores it idempotently by `(user_id, client_id)`. Stats are plain SQL over `focus_sessions` with local-day boundaries computed in Go (`time.Local`, Monday weeks), rendered server-side like ON Flash's stats.

**Tech Stack:** Go 1.22+ `ServeMux`, `html/template`, SQLite (modernc), plain ES5-style JavaScript (no build step), `fetch` with `keepalive`, htmx for the today-strip refresh.

**Spec:** [docs/superpowers/specs/2026-10-07-on-focus-design.md](../specs/2026-10-07-on-focus-design.md) — sections "Recording a session", "Recording endpoint", "History", "Home", "Export and admin", "Testing", "Phases" (F3 row).

## Global Constraints

- Routes stay under `/focus/`; every class is prefixed `focus-` (plus the shared `swatch-c-*`).
- CSP: no inline `<script>` code and no `style=""` attributes. Bar sizes go in SVG attributes (`width`, `height`, `x`, `y`), as ON Flash's stats do.
- Scripts are served by `(*App).script` and loaded with `defer` from a template's `head` block in dependency order: `session.js`, then `chimes.js` (running page only), then `record.js`, then the page script.
- `localStorage` key `onsuite.focus.session`, state version stays `v: 2`. No new state fields: F2 already stores `clientId`, `startedAt`, `endedAt`, `roundsDone`, `completed`, `finished`.
- A session is recorded when `keepHistory` is on and focus time ≥ 60 s. Under that, or with Keep history off, it is cleared without a request.
- `POST /focus/sessions` body (JSON; times are **ms since the epoch**, as the browser keeps them):
  `{client_id, timer_id, timer_name, color, started_at, ended_at, focus_seconds, rounds_done, completed}`.
  Responses: `201` new, `200` already recorded (both `{"id": n}`), `400` unreadable JSON, `422` refused, `403` CSRF.
- Client outcome of a POST: `2xx` → "saved", clear the state; `400`/`422` → "rejected", clear with a console warning (retrying can't help); anything else, a redirect, or a network error → "failed", keep the state and offer Retry.
- Days are the server's local day (`time.Local`), weeks start Monday. A session belongs to the day it started on. Day-keyed tests use fixed local times (the #521 lesson); `main_test.go` pins `time.Local` to `Australia/Melbourne`, as ON Flash's does.
- Focus amounts render as `0m`, `45m`, `1h`, `1h 40m` (Go `FormatFocus`; JS `focused` already matches above a minute).
- Dialogs live outside any `.stack` (F1 lesson). A rule that sets `display` on an element that also uses `hidden` must be paired with a `[hidden]` rule.
- Introduce Go helpers in the task that first calls them (staticcheck U1000).
- `internal/htmlassert` supports one qualifier per selector; for more, use `findEl` from `form_test.go`.
- go vet rejects unkeyed composite literals of another package's types in `_test` packages — always key `focus.SessionInput{…}` fields.
- Full check must stay green on every commit:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- Work on branch `feat/focus-f3-history` in a worktree, never on `main`. Open the PR as `iliafrenkel` (`env -u GH_TOKEN gh …`, push with `env -u GIT_SSH_COMMAND`); never merge.

## Decisions made while planning (Ilia, 2026-10-08)

- **Transport:** `fetch` with `keepalive: true` for every recording POST instead of `sendBeacon`. A beacon can't carry the `X-CSRF-Token` header; a keepalive fetch can, survives the page unloading the same way, and keeps one code path. The spec line about `sendBeacon` is updated.
- **#546 folded in:** the home banner gets an **End** button for a session that is running, paused or waiting. It asks "End Deep work?", then ends and records it like Exit. A deleted timer's session is stored with no timer (`timer_id` NULL), so the banner can no longer get stuck. Closes #546.
- **History link:** a **History** button in the home toolbar next to **+ New timer**; the History page has **← Timers** back.
- **Today strip:** shown once the user has recorded any session, ever; then always shown, with `0m` on quiet days. Hidden for someone with no history.

## Other planning choices

- `started_at` may be up to **5 minutes** in the future before the server refuses it (`clockSkew`): the browser's clock and the server's can disagree a little, and a strict check would refuse short sessions from a slightly fast laptop. Recorded in the spec.
- A 400/422 answer clears the session (it would never be accepted). Anything else keeps it for Retry, including a 303 to the login page (`redirect: "manual"` makes that a failure instead of a false "saved").
- **Exit** shows the Done screen with "Saving…" and goes home once the server has the session; if saving fails it stays on the Done screen with **Retry**. Exit on a session that isn't eligible goes home at once, as in F2.
- **Replacing** another timer's session ("End Deep work and start Reading?", or one that already ran out) records the old one first. If that POST fails, the old session is dropped with a console warning and the new one starts: one session per browser, and being offline at exactly that moment is rare (accepted).
- After the banner records a session, the today strip refreshes in place through `GET /focus/today` (an htmx fragment), rather than reloading the page.
- "Time per timer this month" groups by `timer_name`; each row takes the colour of that name's most recent session (SQLite's bare-column rule with `max()`).
- Recent sessions page with `?page=N` (offset, 50 a page, "Newer" / "Older" links). One user's history is small; keyset paging isn't worth it.
- `record.js` is a new shared script (network, no DOM) rather than more code in `session.js`, which stays pure state and time maths.
- Session delete uses the existing `data-focus-confirm` dialog from `home.js`; its OK button label becomes configurable (`data-focus-confirm-ok`, default "Delete") so the banner's End can say "End session".
- `session.js` `valid()` now also type-checks `clientId`, `color`, `startedAt`, `endedAt`, `focusSecondsBanked`, `roundsDone`, `waiting` and `finished`: these are now sent to the server or used as a class name. That is the first bullet of #545; the rest of #545 stays open.

## File map

| File | Responsibility |
|---|---|
| `internal/apps/focus/session.go` | `SessionInput`, `Session`, validation, `RecordSession`, `DeleteSession`, `scanSession` |
| `internal/apps/focus/stats.go` | local calendar helpers, `FormatFocus`, `Totals`, `Daily`, `ByTimer`, `RecentSessions` |
| `internal/apps/focus/export.go` | `Exporter`, `Stater` |
| `internal/apps/focus/handlers_sessions.go` | `POST /focus/sessions`, `POST /focus/sessions/{id}/delete` |
| `internal/apps/focus/handlers_history.go` | `GET /focus/history`, chart and view building |
| `internal/apps/focus/handlers.go` | index: today strip; `GET /focus/today` |
| `internal/apps/focus/focus.go` | routes, `record.js`, interface assertions |
| `internal/apps/focus/templates/history.html` | History page |
| `internal/apps/focus/templates/index.html` | History button, today strip, banner buttons, `record.js` |
| `internal/apps/focus/templates/run.html` | `record.js`, Done status and Retry |
| `internal/apps/focus/static/record.js` | `eligible`, `send` |
| `internal/apps/focus/static/session.js` | `end`; stricter `valid` |
| `internal/apps/focus/static/focus.js` | record on finish, Exit and replace; Retry |
| `internal/apps/focus/static/home.js` | banner End / record / Retry; confirm OK label; today refresh |
| `internal/ui/static/app.css` | today strip, banner layout, History page |
| `internal/apps/focus/*_test.go` | tests; new `main_test.go` |
| `docs/user/focus.md` | "History" section, banner End |
| `docs/superpowers/specs/2026-10-07-on-focus-design.md` | record the decisions above |
| `AGENTS.md` | ON Focus description |

---

### Task 0: Worktree and branch

- [ ] **Step 1: Create the worktree**

```bash
cd /Users/iliaf/src/WEB/on-suite
git fetch origin
git worktree add ../on-suite-focus-f3 -b feat/focus-f3-history origin/main
cd ../on-suite-focus-f3
go build ./cmd/onsuite
```
Expected: builds with no output. All later commands run in `../on-suite-focus-f3`.

---

### Task 1: Recording sessions in the store

**Files:**
- Create: `internal/apps/focus/session.go`
- Test: `internal/apps/focus/session_test.go`

**Interfaces:**
- Consumes: `Store` (`st.db`, `st.now`), `rowScanner`, `ErrNotFound`, `ErrInvalid`, `Colors`, `MaxNameRunes`, `maxRounds` (all in `store.go` / `timer.go`); migration `0002_sessions.sql` (already there).
- Produces:
  - `type SessionInput struct { ClientID string; TimerID int64; TimerName, Color string; StartedAt, EndedAt time.Time; FocusSeconds, RoundsDone int; Completed bool }`
  - `type Session struct { ID, UserID, TimerID int64; TimerName, Color, ClientID string; StartedAt, EndedAt time.Time; FocusSeconds, RoundsDone int; Completed bool }` — `TimerID` 0 means no timer.
  - `const MinFocusSeconds = 60`
  - `func (st *Store) RecordSession(ctx, userID int64, in SessionInput) (Session, bool, error)` — bool is "created"; refusals wrap `ErrInvalid`.
  - `func (st *Store) DeleteSession(ctx, userID, id int64) error` — `ErrNotFound` for missing or someone else's.
  - `const sessionColumns`, `func scanSession(rowScanner) (Session, error)` (used again in Task 3).

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/focus/session_test.go`:

```go
package focus_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
)

// sessionInput is a finished, completed session of minutes of focus that
// started at start.
func sessionInput(clientID, name string, start time.Time, minutes int) focus.SessionInput {
	return focus.SessionInput{
		ClientID: clientID, TimerName: name, Color: "teal",
		StartedAt: start, EndedAt: start.Add(time.Duration(minutes) * time.Minute),
		FocusSeconds: minutes * 60, RoundsDone: 1, Completed: true,
	}
}

func countSessions(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM focus_sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRecordSessionStoresEveryField(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tm, err := f.store.CreateTimer(ctx, f.alice.ID, validIntervals())
	if err != nil {
		t.Fatal(err)
	}
	start := f.now.Add(-4 * time.Hour)
	in := focus.SessionInput{
		ClientID: "c-1", TimerID: tm.ID, TimerName: "Deep work", Color: "blue",
		StartedAt: start, EndedAt: start.Add(3 * time.Hour),
		FocusSeconds: 2*3000 + 1200, RoundsDone: 2, Completed: false,
	}
	got, created, err := f.store.RecordSession(ctx, f.alice.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if !created || got.ID == 0 || got.UserID != f.alice.ID || got.TimerID != tm.ID ||
		got.TimerName != "Deep work" || got.Color != "blue" || got.ClientID != "c-1" ||
		!got.StartedAt.Equal(start) || !got.EndedAt.Equal(start.Add(3*time.Hour)) ||
		got.FocusSeconds != 7200 || got.RoundsDone != 2 || got.Completed {
		t.Errorf("recorded %+v, created %v", got, created)
	}
}

func TestRecordSessionIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first, created, err := f.store.RecordSession(ctx, f.alice.ID, sessionInput("same", "Reading", f.now.Add(-time.Hour), 30))
	if err != nil || !created {
		t.Fatalf("first: created %v, err %v", created, err)
	}
	// A retry of the same session, even with different numbers, changes nothing.
	again, created, err := f.store.RecordSession(ctx, f.alice.ID, sessionInput("same", "Other", f.now.Add(-time.Hour), 45))
	if err != nil {
		t.Fatal(err)
	}
	if created || again.ID != first.ID || again.TimerName != "Reading" || again.FocusSeconds != 1800 {
		t.Errorf("retry: created %v, got %+v", created, again)
	}
	// Bob's browser may well pick the same id; it is his own row.
	if _, created, err := f.store.RecordSession(ctx, f.bob.ID, sessionInput("same", "Bob's", f.now.Add(-time.Hour), 30)); err != nil || !created {
		t.Errorf("bob: created %v, err %v", created, err)
	}
	if n := countSessions(t, f); n != 2 {
		t.Errorf("%d rows, want 2", n)
	}
}

func TestRecordSessionRefusesImpossibleSessions(t *testing.T) {
	f := newFixture(t)
	start := f.now.Add(-time.Hour)
	cases := map[string]func(*focus.SessionInput){
		"under a minute":     func(in *focus.SessionInput) { in.FocusSeconds = 59 },
		"no client id":       func(in *focus.SessionInput) { in.ClientID = "  " },
		"long client id":     func(in *focus.SessionInput) { in.ClientID = strings.Repeat("x", 65) },
		"no name":            func(in *focus.SessionInput) { in.TimerName = " " },
		"no start":           func(in *focus.SessionInput) { in.StartedAt = time.Time{} },
		"no end":             func(in *focus.SessionInput) { in.EndedAt = time.Time{} },
		"ends before start":  func(in *focus.SessionInput) { in.EndedAt = start.Add(-time.Minute) },
		"more focus than time": func(in *focus.SessionInput) { in.FocusSeconds = 30*60 + 1 },
		"negative rounds":    func(in *focus.SessionInput) { in.RoundsDone = -1 },
		"too many rounds":    func(in *focus.SessionInput) { in.RoundsDone = 13 },
		"starts in the future": func(in *focus.SessionInput) {
			in.StartedAt = f.now.Add(6 * time.Minute)
			in.EndedAt = in.StartedAt.Add(30 * time.Minute)
		},
	}
	for name, mutate := range cases {
		in := sessionInput("c-"+name, "Reading", start, 30)
		mutate(&in)
		if _, _, err := f.store.RecordSession(context.Background(), f.alice.ID, in); !errors.Is(err, focus.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if n := countSessions(t, f); n != 0 {
		t.Errorf("%d rows stored, want none", n)
	}
}

func TestRecordSessionAllowsALittleClockSkew(t *testing.T) {
	f := newFixture(t)
	in := sessionInput("fast-clock", "Reading", f.now.Add(4*time.Minute), 30)
	if _, _, err := f.store.RecordSession(context.Background(), f.alice.ID, in); err != nil {
		t.Errorf("a browser clock 4 minutes fast: %v", err)
	}
}

func TestRecordSessionTidiesNameAndColour(t *testing.T) {
	f := newFixture(t)
	in := sessionInput("c-1", "  "+strings.Repeat("é", 90)+"  ", f.now.Add(-time.Hour), 30)
	in.Color = "chartreuse"
	got, _, err := f.store.RecordSession(context.Background(), f.alice.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.TimerName != strings.Repeat("é", focus.MaxNameRunes) {
		t.Errorf("name = %q (%d runes)", got.TimerName, len([]rune(got.TimerName)))
	}
	if got.Color != "gray" {
		t.Errorf("colour = %q, want gray", got.Color)
	}
}

func TestRecordSessionDropsATimerThatIsNotTheUsers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	bobs, err := f.store.CreateTimer(ctx, f.bob.ID, validIntervals())
	if err != nil {
		t.Fatal(err)
	}
	for i, timerID := range []int64{bobs.ID, 99999} {
		in := sessionInput("c-"+itoa(int64(i)), "Deep work", f.now.Add(-time.Hour), 30)
		in.TimerID = timerID
		got, _, err := f.store.RecordSession(ctx, f.alice.ID, in)
		if err != nil {
			t.Fatalf("timer %d: %v", timerID, err)
		}
		if got.TimerID != 0 {
			t.Errorf("timer %d stored as %d, want none", timerID, got.TimerID)
		}
	}
}

func TestDeletingATimerKeepsItsSessions(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tm, err := f.store.CreateTimer(ctx, f.alice.ID, validIntervals())
	if err != nil {
		t.Fatal(err)
	}
	in := sessionInput("c-1", "Deep work", f.now.Add(-time.Hour), 50)
	in.TimerID, in.Color = tm.ID, "blue"
	if _, _, err := f.store.RecordSession(ctx, f.alice.ID, in); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteTimer(ctx, f.alice.ID, tm.ID); err != nil {
		t.Fatal(err)
	}
	var timerID *int64
	var name, color string
	if err := f.db.QueryRow(`SELECT timer_id, timer_name, color FROM focus_sessions`).Scan(&timerID, &name, &color); err != nil {
		t.Fatal(err)
	}
	if timerID != nil || name != "Deep work" || color != "blue" {
		t.Errorf("after delete: timer_id %v, %q, %q", timerID, name, color)
	}
}

func TestDeleteSession(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s, _, err := f.store.RecordSession(ctx, f.alice.ID, sessionInput("c-1", "Reading", f.now.Add(-time.Hour), 30))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteSession(ctx, f.bob.ID, s.ID); !errors.Is(err, focus.ErrNotFound) {
		t.Errorf("bob deletes alice's session: %v, want ErrNotFound", err)
	}
	if err := f.store.DeleteSession(ctx, f.alice.ID, s.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteSession(ctx, f.alice.ID, s.ID); !errors.Is(err, focus.ErrNotFound) {
		t.Errorf("second delete: %v, want ErrNotFound", err)
	}
	if n := countSessions(t, f); n != 0 {
		t.Errorf("%d rows left", n)
	}
}
```

`itoa` already lives in `handlers_test.go` (same package).

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/focus/ -run 'RecordSession|DeleteSession|DeletingATimer' -count=1`
Expected: FAIL to compile — `focus.SessionInput`, `RecordSession`, `DeleteSession`, `MinFocusSeconds` undefined.

- [ ] **Step 3: Implement**

Create `internal/apps/focus/session.go`:

```go
package focus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// MinFocusSeconds is the least focus a session needs to be recorded (spec:
// "What gets recorded"). The browser doesn't send shorter ones; the server
// refuses them anyway, and the schema's CHECK backs it up.
const MinFocusSeconds = 60

const (
	// maxClientIDLen bounds the browser's random session id: a UUID is 36.
	maxClientIDLen = 64
	// clockSkew is how far in the future started_at may be before it is
	// refused: the browser's clock and the server's can disagree a little
	// (F3 plan).
	clockSkew = 5 * time.Minute
	// fallbackColor is what an unknown colour is stored as (spec:
	// "Recording endpoint").
	fallbackColor = "gray"
)

// SessionInput is one ended session as the running page reports it
// (spec: "Recording endpoint"). TimerID 0 means none.
type SessionInput struct {
	ClientID     string
	TimerID      int64
	TimerName    string
	Color        string
	StartedAt    time.Time
	EndedAt      time.Time
	FocusSeconds int
	RoundsDone   int
	Completed    bool
}

// Session is one recorded session. TimerID is 0 once its timer is deleted;
// TimerName and Color are the copies taken when it started.
type Session struct {
	ID, UserID   int64
	TimerID      int64
	TimerName    string
	Color        string
	ClientID     string
	StartedAt    time.Time
	EndedAt      time.Time
	FocusSeconds int
	RoundsDone   int
	Completed    bool
}

// sessionColumns is every column scanSession reads, in order.
const sessionColumns = `id, user_id, timer_id, timer_name, color, client_id, started_at,
	ended_at, focus_seconds, rounds_done, completed`

func scanSession(row rowScanner) (Session, error) {
	var s Session
	var timerID sql.NullInt64
	var started, ended string
	err := row.Scan(&s.ID, &s.UserID, &timerID, &s.TimerName, &s.Color, &s.ClientID,
		&started, &ended, &s.FocusSeconds, &s.RoundsDone, &s.Completed)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("focus: load session: %w", err)
	}
	s.TimerID = timerID.Int64
	if s.StartedAt, err = db.ParseTime(started); err != nil {
		return Session{}, fmt.Errorf("focus: session started_at: %w", err)
	}
	if s.EndedAt, err = db.ParseTime(ended); err != nil {
		return Session{}, fmt.Errorf("focus: session ended_at: %w", err)
	}
	return s, nil
}

// checkSession tidies what can be tidied (name, colour) and refuses what
// can't be true (spec: "Recording endpoint").
func checkSession(in SessionInput, now time.Time) (SessionInput, error) {
	in.ClientID = strings.TrimSpace(in.ClientID)
	in.TimerName = strings.TrimSpace(in.TimerName)
	if r := []rune(in.TimerName); len(r) > MaxNameRunes {
		in.TimerName = strings.TrimSpace(string(r[:MaxNameRunes]))
	}
	if !slices.Contains(Colors, in.Color) {
		in.Color = fallbackColor
	}
	var why string
	switch {
	case in.ClientID == "" || len(in.ClientID) > maxClientIDLen:
		why = fmt.Sprintf("client_id must be 1 to %d characters", maxClientIDLen)
	case in.TimerName == "":
		why = "timer_name is empty"
	case in.FocusSeconds < MinFocusSeconds:
		why = fmt.Sprintf("under %d seconds of focus", MinFocusSeconds)
	case in.RoundsDone < 0 || in.RoundsDone > maxRounds:
		why = fmt.Sprintf("rounds_done must be 0 to %d", maxRounds)
	case in.StartedAt.IsZero() || in.EndedAt.IsZero():
		why = "started_at and ended_at are required"
	case in.EndedAt.Before(in.StartedAt):
		why = "ended_at is before started_at"
	case in.StartedAt.After(now.Add(clockSkew)):
		why = "started_at is in the future"
	case time.Duration(in.FocusSeconds)*time.Second > in.EndedAt.Sub(in.StartedAt):
		why = "more focus than the session lasted"
	}
	if why != "" {
		return in, fmt.Errorf("%w: %s", ErrInvalid, why)
	}
	return in, nil
}

// RecordSession stores one of the user's ended sessions. The browser keeps
// trying until it hears back, so the same session can arrive twice: a
// client_id the user already has returns that row unchanged, with created
// false.
func (st *Store) RecordSession(ctx context.Context, userID int64, in SessionInput) (Session, bool, error) {
	in, err := checkSession(in, st.now())
	if err != nil {
		return Session{}, false, err
	}
	// A timer deleted mid-session, or an id that isn't this user's, is
	// stored as no timer — never an error (spec: "Recording endpoint").
	var timerID sql.NullInt64
	if in.TimerID > 0 {
		err := st.db.QueryRowContext(ctx,
			`SELECT id FROM focus_timers WHERE user_id = ? AND id = ?`, userID, in.TimerID).Scan(&timerID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Session{}, false, fmt.Errorf("focus: record session: %w", err)
		}
	}
	res, err := st.db.ExecContext(ctx, `
		INSERT INTO focus_sessions (user_id, timer_id, timer_name, color, client_id,
			started_at, ended_at, focus_seconds, rounds_done, completed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, client_id) DO NOTHING`,
		userID, timerID, in.TimerName, in.Color, in.ClientID,
		db.FormatTime(in.StartedAt), db.FormatTime(in.EndedAt),
		in.FocusSeconds, in.RoundsDone, flag(in.Completed))
	if err != nil {
		return Session{}, false, fmt.Errorf("focus: record session: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Session{}, false, fmt.Errorf("focus: record session: %w", err)
	}
	s, err := scanSession(st.db.QueryRowContext(ctx,
		`SELECT `+sessionColumns+` FROM focus_sessions WHERE user_id = ? AND client_id = ?`,
		userID, in.ClientID))
	if err != nil {
		return Session{}, false, err
	}
	return s, n == 1, nil
}

// DeleteSession removes one of the user's recorded sessions.
func (st *Store) DeleteSession(ctx context.Context, userID, id int64) error {
	res, err := st.db.ExecContext(ctx, `DELETE FROM focus_sessions WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return fmt.Errorf("focus: delete session: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("focus: delete session: %w", err)
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}
```

Also update the package comment in `store.go` from `blocks or Pomodoro-style intervals) and, from F3, a history of sessions.` to `blocks or Pomodoro-style intervals) and a history of the sessions run with them.`

- [ ] **Step 4: Run the tests**

Run: `gofmt -w internal/apps/focus && go test ./internal/apps/focus/ -count=1`
Expected: PASS (all focus tests, old and new).

- [ ] **Step 5: Commit**

```bash
git add internal/apps/focus/session.go internal/apps/focus/session_test.go internal/apps/focus/store.go
git commit -m "feat(focus): record sessions in the store (#495)"
```

---

### Task 2: Recording endpoint and session delete

**Files:**
- Create: `internal/apps/focus/handlers_sessions.go`
- Modify: `internal/apps/focus/focus.go` (routes)
- Test: `internal/apps/focus/handlers_sessions_test.go`

**Interfaces:**
- Consumes: `RecordSession`, `DeleteSession`, `ErrInvalid` (Task 1); `(*App).userID`, `pathID`, `fail` (`handlers.go`).
- Produces:
  - `POST /focus/sessions` — JSON body as in Global Constraints; `201`/`200` with `{"id": n}`; `400` bad JSON; `422` refused.
  - `POST /focus/sessions/{id}/delete` — form; optional `page` field; `303` to `/focus/history` (or `/focus/history?page=N` for N > 1); `404` for missing or someone else's.
  - `func historyURL(page int) string` (used again in Task 4).

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/focus/handlers_sessions_test.go`:

```go
package focus_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// postSession sends a session the way record.js does: JSON, with the CSRF
// token in the header htmx also uses.
func postSession(t *testing.T, s *server, sess *apptest.Session, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/focus/sessions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(web.CSRFHeader, s.CSRFToken(t, sess))
	return s.Do(t, sess, req)
}

// sessionBody is a 50-minute completed round of timerID, as record.js sends it.
func sessionBody(clientID string, timerID int64, start time.Time) map[string]any {
	return map[string]any{
		"client_id": clientID, "timer_id": timerID, "timer_name": "Deep work", "color": "blue",
		"started_at": start.UnixMilli(), "ended_at": start.Add(50 * time.Minute).UnixMilli(),
		"focus_seconds": 3000, "rounds_done": 1, "completed": true,
	}
}

func recordedID(t *testing.T, rec *httptest.ResponseRecorder) int64 {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.ID == 0 {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return out.ID
}

func TestRecordingASession(t *testing.T) {
	s := newServer(t)
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	s.Clock.Set(now)
	tm := seedTimer(t, s, s.Alice.User.ID, validIntervals())
	start := now.Add(-time.Hour)

	rec := postSession(t, s, s.Alice, sessionBody("c-1", tm.ID, start))
	if rec.Code != http.StatusCreated {
		t.Fatalf("first POST = %d, want 201; body %s", rec.Code, rec.Body.String())
	}
	id := recordedID(t, rec)

	again := postSession(t, s, s.Alice, sessionBody("c-1", tm.ID, start))
	if again.Code != http.StatusOK {
		t.Fatalf("repeat POST = %d, want 200", again.Code)
	}
	if got := recordedID(t, again); got != id {
		t.Errorf("repeat returned id %d, want %d", got, id)
	}

	// Recording the same client id once more returns the stored row as it is.
	stored := storedSession(t, s, s.Alice.User.ID, "c-1")
	if stored.ID != id || stored.TimerID != tm.ID || stored.FocusSeconds != 3000 ||
		!stored.StartedAt.Equal(start) || !stored.Completed || stored.Color != "blue" {
		t.Errorf("stored %+v", stored)
	}
}

// storedSession reads back the session clientID names, through the store's
// idempotency: a repeat of a recorded client id returns the row unchanged.
func storedSession(t *testing.T, s *server, userID int64, clientID string) focus.Session {
	t.Helper()
	got, created, err := s.Store.RecordSession(context.Background(), userID, focus.SessionInput{
		ClientID: clientID, TimerName: "lookup", Color: "teal",
		StartedAt: s.Clock.Now().Add(-time.Hour), EndedAt: s.Clock.Now(), FocusSeconds: 60,
	})
	if err != nil || created {
		t.Fatalf("lookup of %q: created %v, err %v", clientID, created, err)
	}
	return got
}

func TestRecordingRefusesBadSessions(t *testing.T) {
	s := newServer(t)
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	s.Clock.Set(now)

	short := sessionBody("short", 0, now.Add(-time.Hour))
	short["focus_seconds"] = 59
	if rec := postSession(t, s, s.Alice, short); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("59 s of focus = %d, want 422", rec.Code)
	}
	future := sessionBody("future", 0, now.Add(time.Hour))
	if rec := postSession(t, s, s.Alice, future); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("starts in an hour = %d, want 422", rec.Code)
	}
	missing := sessionBody("missing", 0, now.Add(-time.Hour))
	delete(missing, "started_at")
	if rec := postSession(t, s, s.Alice, missing); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("no started_at = %d, want 422", rec.Code)
	}

	req := httptest.NewRequest("POST", "/focus/sessions", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(web.CSRFHeader, s.CSRFToken(t, s.Alice))
	if rec := s.Do(t, s.Alice, req); rec.Code != http.StatusBadRequest {
		t.Errorf("bad JSON = %d, want 400", rec.Code)
	}
}

func TestRecordingNeedsTheCSRFHeader(t *testing.T) {
	s := newServer(t)
	raw, _ := json.Marshal(sessionBody("c-1", 0, time.Now().Add(-time.Hour)))
	req := httptest.NewRequest("POST", "/focus/sessions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if rec := s.Do(t, s.Alice, req); rec.Code != http.StatusForbidden {
		t.Errorf("no CSRF header = %d, want 403", rec.Code)
	}
}

func TestRecordingStoresSomeoneElsesTimerAsNone(t *testing.T) {
	s := newServer(t)
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	s.Clock.Set(now)
	bobs := seedTimer(t, s, s.Bob.User.ID, validIntervals())
	if rec := postSession(t, s, s.Alice, sessionBody("c-1", bobs.ID, now.Add(-time.Hour))); rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d, want 201", rec.Code)
	}
	if got := storedSession(t, s, s.Alice.User.ID, "c-1"); got.TimerID != 0 {
		t.Errorf("timer_id = %d, want none", got.TimerID)
	}
}

func TestDeleteSessionFromHistory(t *testing.T) {
	s := newServer(t)
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	s.Clock.Set(now)
	in := sessionInput("c-1", "Reading", now.Add(-time.Hour), 30)
	sess, _, err := s.Store.RecordSession(context.Background(), s.Alice.User.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	path := "/focus/sessions/" + itoa(sess.ID) + "/delete"

	if rec := s.Post(t, s.Bob, path, url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("bob = %d, want 404", rec.Code)
	}
	if rec := s.Post(t, s.Alice, "/focus/sessions/abc/delete", url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("non-numeric id = %d, want 404", rec.Code)
	}
	s.Submit(t, s.Alice, path, url.Values{"page": {"3"}}, "/focus/history?page=3")
	if rec := s.Post(t, s.Alice, path, url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("deleting twice = %d, want 404", rec.Code)
	}

	// Page 1, no page, and junk all go back to the first page.
	for i, page := range []string{"", "1", "0", "x"} {
		other, _, err := s.Store.RecordSession(context.Background(), s.Alice.User.ID,
			sessionInput("p-"+itoa(int64(i)), "Reading", now.Add(-time.Hour), 30))
		if err != nil {
			t.Fatal(err)
		}
		s.Submit(t, s.Alice, "/focus/sessions/"+itoa(other.ID)+"/delete", url.Values{"page": {page}}, "/focus/history")
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/focus/ -run 'Recording|DeleteSessionFromHistory' -count=1`
Expected: FAIL — `POST /focus/sessions` is 404/405 (no route yet).

- [ ] **Step 3: Implement the handlers**

Create `internal/apps/focus/handlers_sessions.go`:

```go
package focus

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// sessionBody is what record.js POSTs (spec: "Recording endpoint"). Times
// are ms since the epoch, as the browser keeps them.
type sessionBody struct {
	ClientID     string `json:"client_id"`
	TimerID      int64  `json:"timer_id"`
	TimerName    string `json:"timer_name"`
	Color        string `json:"color"`
	StartedAt    int64  `json:"started_at"`
	EndedAt      int64  `json:"ended_at"`
	FocusSeconds int    `json:"focus_seconds"`
	RoundsDone   int    `json:"rounds_done"`
	Completed    bool   `json:"completed"`
}

// sessionBodyMaxBytes is far more than a session summary needs.
const sessionBodyMaxBytes = 4 << 10

// fromMillis reads a browser timestamp; anything not positive is "missing",
// which the store refuses.
func fromMillis(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// recordSession stores a session the running page or the home banner
// reports. The browser clears its copy on any 2xx, so a repeat answers 200
// with the row it already has.
func (a *App) recordSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	var body sessionBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, sessionBodyMaxBytes)).Decode(&body); err != nil {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	s, created, err := a.store.RecordSession(r.Context(), userID, SessionInput{
		ClientID: body.ClientID, TimerID: body.TimerID, TimerName: body.TimerName, Color: body.Color,
		StartedAt: fromMillis(body.StartedAt), EndedAt: fromMillis(body.EndedAt),
		FocusSeconds: body.FocusSeconds, RoundsDone: body.RoundsDone, Completed: body.Completed,
	})
	if errors.Is(err, ErrInvalid) {
		a.deps.Errors.Status(w, r, http.StatusUnprocessableEntity)
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		ID int64 `json:"id"`
	}{s.ID})
}

// historyURL is the History page, on page (1 is the first).
func historyURL(page int) string {
	if page <= 1 {
		return "/focus/history"
	}
	return "/focus/history?page=" + strconv.Itoa(page)
}

// deleteSession removes one session from the History page and goes back
// to the page it was on.
func (a *App) deleteSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteSession(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	page, _ := strconv.Atoi(r.PostFormValue("page"))
	http.Redirect(w, r, historyURL(page), http.StatusSeeOther)
}
```

In `internal/apps/focus/focus.go` `Mount`, after `r.HandleFunc("GET /run/{id}", a.run)` add:

```go
	r.HandleFunc("POST /sessions", a.recordSession)
	r.HandleFunc("POST /sessions/{id}/delete", a.deleteSession)
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -w internal/apps/focus && go test ./internal/apps/focus/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/focus/handlers_sessions.go internal/apps/focus/handlers_sessions_test.go internal/apps/focus/focus.go
git commit -m "feat(focus): POST /focus/sessions records a session; delete one (#495)"
```

---

### Task 3: Stats in the store

**Files:**
- Create: `internal/apps/focus/stats.go`, `internal/apps/focus/main_test.go`
- Test: `internal/apps/focus/stats_test.go`

**Interfaces:**
- Consumes: `Session`, `sessionColumns`, `scanSession` (Task 1).
- Produces:
  - `func FormatFocus(seconds int) string` — `"0m"`, `"45m"`, `"1h"`, `"1h 40m"`.
  - `type Totals struct { Today, Week, Month, Year int; TodaySessions int; Any bool }` — seconds of focus; `Any` = the user has any session at all.
  - `func (st *Store) Totals(ctx, userID int64, now time.Time) (Totals, error)`
  - `type DayTotal struct { Day time.Time; Seconds int }`
  - `func (st *Store) Daily(ctx, userID int64, days int, now time.Time) ([]DayTotal, error)` — oldest first, `days` entries, empty days included.
  - `type TimerTotal struct { Name, Color string; Seconds int }`
  - `func (st *Store) ByTimer(ctx, userID int64, since time.Time) ([]TimerTotal, error)` — largest first.
  - `func (st *Store) RecentSessions(ctx, userID int64, page, perPage int) ([]Session, bool, error)` — newest first; bool = an older page exists.
  - `func startOfMonth(t time.Time) time.Time` (used by Task 4); `startOfDay`, `startOfWeek`, `startOfYear`.
  - `func (st *Store) querySessions(ctx, query string, args ...any) ([]Session, error)` (used again in Task 8).

- [ ] **Step 1: Pin the local zone for the package's tests**

Create `internal/apps/focus/main_test.go`:

```go
package focus_test

import (
	"os"
	"testing"
	"time"
	_ "time/tzdata" // LoadLocation must not depend on the test machine's zoneinfo
)

// TestMain pins the process's local zone to one well east of UTC, so the
// day boundary ON Focus follows (the server's local date) is exercised the
// same way on every machine. Mirrors ON Flash's main_test.go.
func TestMain(m *testing.M) {
	loc, err := time.LoadLocation("Australia/Melbourne")
	if err != nil {
		panic(err)
	}
	time.Local = loc
	os.Exit(m.Run())
}
```

Run: `go test ./internal/apps/focus/ -count=1`
Expected: PASS (nothing in F1/F2 depends on the local zone).

- [ ] **Step 2: Write the failing tests**

Create `internal/apps/focus/stats_test.go`:

```go
package focus_test

import (
	"context"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
)

// Melbourne is UTC+11 in October 2026 (summer time began 4 Oct), so the
// UTC date changes at 11 am local. 7 Oct 2026 is a Wednesday; its week
// began Monday 5 Oct.

// localAt is a wall-clock time on the pinned local zone (main_test.go).
func localAt(month time.Month, day, hour, minute int) time.Time {
	return time.Date(2026, month, day, hour, minute, 0, 0, time.Local)
}

// statsFixture records sessions around Wednesday 7 Oct 2026, noon local:
//
//	Wed 7 Oct 08:00, 30m   today, week, month, year
//	Wed 7 Oct 00:30, 5m    today — Tue 13:30 UTC, the previous UTC day
//	Tue 6 Oct 20:00, 20m   week, month, year
//	Sun 4 Oct 10:00, 15m   month, year (the week starts Monday)
//	Wed 30 Sep 10:00, 25m  year
//	Mon 7 Sep 23:30, 10m   year; one day before the 30-day window
//	31 Dec 2025 23:00, 10m none
//
// plus one of Bob's today, which never counts for Alice.
func statsFixture(t *testing.T) (*fixture, time.Time) {
	t.Helper()
	f := newFixture(t)
	now := localAt(10, 7, 12, 0)
	f.now = now
	ctx := context.Background()
	add := func(userID int64, id, name, color string, start time.Time, minutes int) {
		t.Helper()
		in := sessionInput(id, name, start, minutes)
		in.Color = color
		if _, _, err := f.store.RecordSession(ctx, userID, in); err != nil {
			t.Fatal(err)
		}
	}
	add(f.alice.ID, "a", "Deep work", "blue", localAt(10, 7, 8, 0), 30)
	add(f.alice.ID, "b", "Reading", "green", localAt(10, 7, 0, 30), 5)
	add(f.alice.ID, "c", "Deep work", "blue", localAt(10, 6, 20, 0), 20)
	add(f.alice.ID, "d", "Reading", "amber", localAt(10, 4, 10, 0), 15)
	add(f.alice.ID, "e", "Deep work", "blue", localAt(9, 30, 10, 0), 25)
	add(f.alice.ID, "f", "Deep work", "blue", localAt(9, 7, 23, 30), 10)
	add(f.alice.ID, "g", "Deep work", "blue", time.Date(2025, 12, 31, 23, 0, 0, 0, time.Local), 10)
	add(f.bob.ID, "a", "Bob's", "teal", localAt(10, 7, 9, 0), 60)
	return f, now
}

func TestTotalsFollowTheLocalCalendar(t *testing.T) {
	f, now := statsFixture(t)
	got, err := f.store.Totals(context.Background(), f.alice.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	want := focus.Totals{
		Today: 35 * 60, TodaySessions: 2,
		Week:  55 * 60,
		Month: 70 * 60,
		Year:  105 * 60,
		Any:   true,
	}
	if got != want {
		t.Errorf("Totals = %+v\nwant     %+v", got, want)
	}
}

func TestTotalsForSomeoneWithNoHistory(t *testing.T) {
	f := newFixture(t)
	got, err := f.store.Totals(context.Background(), f.alice.ID, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if got != (focus.Totals{}) {
		t.Errorf("Totals = %+v, want zero", got)
	}
}

func TestTotalsWeekReachesBackIntoLastYear(t *testing.T) {
	// Fri 1 Jan 2027: the week began Mon 28 Dec 2026, the year today.
	f := newFixture(t)
	now := time.Date(2027, 1, 1, 12, 0, 0, 0, time.Local)
	f.now = now
	in := sessionInput("dec", "Reading", time.Date(2026, 12, 29, 9, 0, 0, 0, time.Local), 40)
	if _, _, err := f.store.RecordSession(context.Background(), f.alice.ID, in); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.Totals(context.Background(), f.alice.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Week != 40*60 || got.Year != 0 || got.Month != 0 || got.Today != 0 {
		t.Errorf("Totals = %+v; want 40m this week and nothing this year", got)
	}
}

func TestDailyHasEveryDayOldestFirst(t *testing.T) {
	f, now := statsFixture(t)
	days, err := f.store.Daily(context.Background(), f.alice.ID, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 30 {
		t.Fatalf("%d days, want 30", len(days))
	}
	first, last := days[0].Day, days[29].Day
	if !first.Equal(localAt(9, 8, 0, 0)) || !last.Equal(localAt(10, 7, 0, 0)) {
		t.Errorf("window %v – %v, want 8 Sep – 7 Oct local midnights", first, last)
	}
	want := map[string]int{"2026-10-07": 35 * 60, "2026-10-06": 20 * 60, "2026-10-04": 15 * 60, "2026-09-30": 25 * 60}
	for _, d := range days {
		key := d.Day.Format("2006-01-02")
		if d.Seconds != want[key] {
			t.Errorf("%s: %d s, want %d", key, d.Seconds, want[key])
		}
	}
}

func TestByTimerThisMonthLargestFirst(t *testing.T) {
	f, _ := statsFixture(t)
	got, err := f.store.ByTimer(context.Background(), f.alice.ID, localAt(10, 1, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	want := []focus.TimerTotal{
		{Name: "Deep work", Color: "blue", Seconds: 50 * 60},
		// Reading's newest session this month is green, its older one amber.
		{Name: "Reading", Color: "green", Seconds: 20 * 60},
	}
	if len(got) != len(want) {
		t.Fatalf("ByTimer = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestRecentSessionsPages(t *testing.T) {
	f, _ := statsFixture(t)
	ctx := context.Background()
	var all []string
	for page := 1; ; page++ {
		got, more, err := f.store.RecentSessions(ctx, f.alice.ID, page, 3)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range got {
			all = append(all, s.ClientID)
		}
		if !more {
			break
		}
		if page > 5 {
			t.Fatal("paging never ends")
		}
	}
	// Newest first by start time, Alice's only.
	if got := fmt.Sprint(all); got != "[a b c d e f g]" {
		t.Errorf("sessions = %s", got)
	}
	empty, more, err := f.store.RecentSessions(ctx, f.alice.ID, 9, 3)
	if err != nil || len(empty) != 0 || more {
		t.Errorf("page past the end: %d sessions, more %v, err %v", len(empty), more, err)
	}
}

func TestFormatFocus(t *testing.T) {
	for seconds, want := range map[int]string{
		0: "0m", 59: "0m", 60: "1m", 45 * 60: "45m", 3600: "1h", 3600 + 40*60 + 30: "1h 40m", 25 * 3600: "25h",
	} {
		if got := focus.FormatFocus(seconds); got != want {
			t.Errorf("FormatFocus(%d) = %q, want %q", seconds, got, want)
		}
	}
}
```

Add `"fmt"` to the file's imports (used by `TestRecentSessionsPages`).

Check the year total by hand before trusting it: a 30 + b 5 + c 20 + d 15 + e 25 + f 10 = 105 minutes in 2026; g is 2025.

- [ ] **Step 3: Run the tests to see them fail**

Run: `go test ./internal/apps/focus/ -run 'Totals|Daily|ByTimer|RecentSessions|FormatFocus' -count=1`
Expected: FAIL to compile — `Totals`, `Daily`, `ByTimer`, `RecentSessions`, `FormatFocus`, `TimerTotal` undefined.

- [ ] **Step 4: Implement**

Create `internal/apps/focus/stats.go`:

```go
package focus

import (
	"context"
	"fmt"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// The local calendar (spec: "Weeks and days"): days in the server's local
// zone, weeks starting Monday. A session belongs to the day it started on.
// startOfDay mirrors ON Flash's (PATTERNS.md, "Cross-app mirroring"):
// walking days with AddDate keeps landing on midnight across a DST change.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Local().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

func startOfWeek(t time.Time) time.Time {
	day := startOfDay(t)
	sinceMonday := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -sinceMonday)
}

func startOfMonth(t time.Time) time.Time {
	y, m, _ := t.Local().Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.Local)
}

func startOfYear(t time.Time) time.Time {
	return time.Date(t.Local().Year(), 1, 1, 0, 0, 0, 0, time.Local)
}

// FormatFocus renders an amount of focus: "0m", "45m", "1h", "1h 40m".
// focus.js's focused agrees from a minute up.
func FormatFocus(seconds int) string {
	m := seconds / 60
	h, m := m/60, m%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// Totals is a user's focus, in seconds, over the calendar's spans.
type Totals struct {
	Today, Week, Month, Year int
	TodaySessions            int
	// Any is whether the user has recorded a session ever: the home page
	// shows its today strip only then (F3 plan).
	Any bool
}

// Totals adds up the user's focus today, this week, this month and this
// year, as of now. The week can begin last year (1 Jan on a Friday), so the
// scan starts at whichever of the two comes first.
func (st *Store) Totals(ctx context.Context, userID int64, now time.Time) (Totals, error) {
	day, week, month, year := startOfDay(now), startOfWeek(now), startOfMonth(now), startOfYear(now)
	from := year
	if week.Before(from) {
		from = week
	}
	var t Totals
	err := st.db.QueryRowContext(ctx, `
		SELECT coalesce(sum(CASE WHEN started_at >= ? THEN focus_seconds END), 0),
		       coalesce(sum(CASE WHEN started_at >= ? THEN 1 END), 0),
		       coalesce(sum(CASE WHEN started_at >= ? THEN focus_seconds END), 0),
		       coalesce(sum(CASE WHEN started_at >= ? THEN focus_seconds END), 0),
		       coalesce(sum(CASE WHEN started_at >= ? THEN focus_seconds END), 0),
		       EXISTS (SELECT 1 FROM focus_sessions WHERE user_id = ?)
		  FROM focus_sessions
		 WHERE user_id = ? AND started_at >= ?`,
		db.FormatTime(day), db.FormatTime(day), db.FormatTime(week), db.FormatTime(month),
		db.FormatTime(year), userID, userID, db.FormatTime(from),
	).Scan(&t.Today, &t.TodaySessions, &t.Week, &t.Month, &t.Year, &t.Any)
	if err != nil {
		return Totals{}, fmt.Errorf("focus: totals: %w", err)
	}
	return t, nil
}

// DayTotal is one local day's focus.
type DayTotal struct {
	Day     time.Time // local midnight
	Seconds int
}

// Daily is the user's focus per day for the last days days ending today,
// oldest first, including days with none — a chart that dropped quiet days
// would show a busier habit than the real one.
func (st *Store) Daily(ctx context.Context, userID int64, days int, now time.Time) ([]DayTotal, error) {
	if days <= 0 {
		return nil, nil
	}
	end := startOfDay(now)
	start := end.AddDate(0, 0, -(days - 1))
	rows, err := st.db.QueryContext(ctx, `
		SELECT started_at, focus_seconds FROM focus_sessions
		 WHERE user_id = ? AND started_at >= ? AND started_at < ?`,
		userID, db.FormatTime(start), db.FormatTime(end.AddDate(0, 0, 1)))
	if err != nil {
		return nil, fmt.Errorf("focus: daily: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byDay := map[string]int{}
	for rows.Next() {
		var started string
		var seconds int
		if err := rows.Scan(&started, &seconds); err != nil {
			return nil, fmt.Errorf("focus: daily: %w", err)
		}
		t, err := db.ParseTime(started)
		if err != nil {
			return nil, fmt.Errorf("focus: daily: %w", err)
		}
		byDay[startOfDay(t).Format("2006-01-02")] += seconds
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("focus: daily: %w", err)
	}
	out := make([]DayTotal, 0, days)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i)
		out = append(out, DayTotal{Day: d, Seconds: byDay[d.Format("2006-01-02")]})
	}
	return out, nil
}

// TimerTotal is one timer name's focus over a span.
type TimerTotal struct {
	Name, Color string
	Seconds     int
}

// ByTimer is the user's focus per timer name since since, largest first
// (spec: "History"). Sessions are grouped by the name they were recorded
// under, so a deleted timer still has its row. Color is that name's newest
// session's: with max(started_at) in the select, SQLite takes the bare
// color column from the row holding the max.
func (st *Store) ByTimer(ctx context.Context, userID int64, since time.Time) ([]TimerTotal, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT timer_name, color, sum(focus_seconds) AS total, max(started_at)
		  FROM focus_sessions
		 WHERE user_id = ? AND started_at >= ?
		 GROUP BY timer_name
		 ORDER BY total DESC, timer_name`,
		userID, db.FormatTime(since))
	if err != nil {
		return nil, fmt.Errorf("focus: by timer: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []TimerTotal
	for rows.Next() {
		var t TimerTotal
		var newest string
		if err := rows.Scan(&t.Name, &t.Color, &t.Seconds, &newest); err != nil {
			return nil, fmt.Errorf("focus: by timer: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("focus: by timer: %w", err)
	}
	return out, nil
}

// querySessions runs a SELECT of sessionColumns and reads every row.
func (st *Store) querySessions(ctx context.Context, query string, args ...any) ([]Session, error) {
	rows, err := st.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("focus: list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("focus: list sessions: %w", err)
	}
	return out, nil
}

// RecentSessions is one page of the user's sessions, newest first, and
// whether an older page exists. page counts from 1.
func (st *Store) RecentSessions(ctx context.Context, userID int64, page, perPage int) ([]Session, bool, error) {
	if page < 1 {
		page = 1
	}
	out, err := st.querySessions(ctx, `SELECT `+sessionColumns+` FROM focus_sessions
		 WHERE user_id = ? ORDER BY started_at DESC, id DESC LIMIT ? OFFSET ?`,
		userID, perPage+1, (page-1)*perPage)
	if err != nil {
		return nil, false, err
	}
	if len(out) > perPage {
		return out[:perPage], true, nil
	}
	return out, false, nil
}
```

- [ ] **Step 5: Run the tests**

Run: `gofmt -w internal/apps/focus && go test ./internal/apps/focus/ -count=1`
Expected: PASS. If `TestDailyHasEveryDayOldestFirst` fails on `first`, check `start` uses `AddDate` from local midnight (not `Add(-24h)`), since 4 Oct is a DST change.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/focus/stats.go internal/apps/focus/stats_test.go internal/apps/focus/main_test.go
git commit -m "feat(focus): history totals, daily series, per-timer totals (#495)"
```

---

### Task 4: History page

**Files:**
- Create: `internal/apps/focus/handlers_history.go`, `internal/apps/focus/templates/history.html`
- Modify: `internal/apps/focus/focus.go` (route), `internal/apps/focus/templates/focus.partial.html` (confirm dialog OK label hook), `internal/ui/static/app.css`
- Test: `internal/apps/focus/handlers_history_test.go`

**Interfaces:**
- Consumes: `Totals`, `Daily`, `ByTimer`, `RecentSessions`, `FormatFocus`, `startOfMonth` (Task 3); `historyURL` (Task 2); `focus-confirm` partial and `home.js`'s `data-focus-confirm` handling.
- Produces: `GET /focus/history[?page=N]`; `const sessionsPerPage = 50`; CSS classes `focus-totals`, `focus-total`, `focus-chart`, `focus-by-timer*`, `focus-sessions`, `focus-early`, `focus-session-timer`, `focus-pager`, `focus-toolbar-actions`, `focus-muted`.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/focus/handlers_history_test.go`:

```go
package focus_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// seedSession records in for userID through the server's own store.
func seedSession(t *testing.T, s *server, userID int64, in focus.SessionInput) focus.Session {
	t.Helper()
	got, _, err := s.Store.RecordSession(context.Background(), userID, in)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func texts(doc *htmlassert.Doc, selector string) []string {
	var out []string
	for _, n := range doc.QueryAll(selector) {
		out = append(out, htmlassert.Text(n))
	}
	return out
}

func TestHistoryShowsTotalsChartTimersAndSessions(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(localAt(10, 7, 12, 0)) // Wednesday noon
	uid := s.Alice.User.ID
	deep := sessionInput("a", "Deep work", localAt(10, 7, 8, 0), 30)
	deep.Color = "blue"
	seedSession(t, s, uid, deep)
	early := sessionInput("b", "Reading", localAt(10, 6, 20, 0), 20)
	early.Completed = false
	seedSession(t, s, uid, early)
	seedSession(t, s, uid, sessionInput("c", "Reading", localAt(9, 30, 10, 0), 25))
	seedSession(t, s, s.Bob.User.ID, sessionInput("z", "Bob's", localAt(10, 7, 9, 0), 60))

	doc := s.Get(t, s.Alice, "/focus/history")
	doc.MustHave(`a[href="/focus/"]`)
	doc.MustHave(`script[src="/focus/home.js"]`)

	if got := strings.Join(texts(doc, ".focus-total .value"), "|"); got != "30m|50m|50m|1h 15m" {
		t.Errorf("totals = %s, want today|week|month|year = 30m|50m|50m|1h 15m", got)
	}
	if got := strings.Join(texts(doc, ".focus-total .label"), "|"); got != "Today|This week|This month|This year" {
		t.Errorf("total labels = %s", got)
	}

	if bars := doc.QueryAll(".focus-chart rect"); len(bars) != 30 {
		t.Errorf("%d chart bars, want 30", len(bars))
	}
	if got := strings.Join(texts(doc, ".focus-by-timer-name"), "|"); got != "Deep work|Reading" {
		t.Errorf("this month by timer = %s", got)
	}

	rows := doc.QueryAll(".focus-sessions tbody tr")
	if len(rows) != 3 {
		t.Fatalf("%d session rows, want 3 (Bob's must not show)", len(rows))
	}
	if got := strings.Join(texts(doc, ".focus-session-timer"), "|"); got != "Deep work|Reading|Reading" {
		t.Errorf("sessions newest first = %s", got)
	}
	if n := len(doc.QueryAll(".focus-early")); n != 1 {
		t.Errorf("%d stopped-early markers, want 1", n)
	}
	if !strings.Contains(htmlassert.Text(rows[0]), "Wed 7 Oct 2026 · 08:00") {
		t.Errorf("first row = %q, want the local start time", htmlassert.Text(rows[0]))
	}
	form := doc.MustHave(".focus-sessions form")
	if action, _ := htmlassert.Attr(form, "action"); !strings.HasPrefix(action, "/focus/sessions/") {
		t.Errorf("delete form action = %q", action)
	}
	if _, ok := htmlassert.Attr(form, "data-focus-confirm"); !ok {
		t.Error("deleting a session should ask first")
	}
	doc.MustHave("#focus-confirm-dialog")
	doc.MustNotHave(".focus-pager a")
}

func TestHistoryPagesOlderSessions(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(localAt(10, 7, 12, 0))
	for i := 0; i < 51; i++ {
		start := localAt(10, 7, 11, 0).Add(-time.Duration(i) * time.Hour)
		seedSession(t, s, s.Alice.User.ID, sessionInput(fmt.Sprintf("s%02d", i), "Reading", start, 30))
	}
	first := s.Get(t, s.Alice, "/focus/history")
	if n := len(first.QueryAll(".focus-sessions tbody tr")); n != 50 {
		t.Errorf("page 1 has %d rows, want 50", n)
	}
	first.MustHave(`.focus-pager a[href="/focus/history?page=2"]`)

	second := s.Get(t, s.Alice, "/focus/history?page=2")
	if n := len(second.QueryAll(".focus-sessions tbody tr")); n != 1 {
		t.Errorf("page 2 has %d rows, want 1", n)
	}
	second.MustHave(`.focus-pager a[href="/focus/history"]`)
	if v, _ := htmlassert.Attr(second.MustHave(`.focus-sessions input[name="page"]`), "value"); v != "2" {
		t.Errorf("delete form page = %q, want 2", v)
	}

	junk := s.Get(t, s.Alice, "/focus/history?page=abc")
	if n := len(junk.QueryAll(".focus-sessions tbody tr")); n != 50 {
		t.Errorf("?page=abc has %d rows, want page 1's 50", n)
	}
}

func TestHistoryEmptyState(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/focus/history")
	doc.MustHave(".focus-empty")
	doc.MustNotHave(".focus-sessions")
	doc.MustHave(".focus-chart .empty")
	if got := strings.Join(texts(doc, ".focus-total .value"), "|"); got != "0m|0m|0m|0m" {
		t.Errorf("totals = %s", got)
	}
}
```

`localAt` and `sessionInput` come from `stats_test.go` / `session_test.go` (same package).

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/focus/ -run History -count=1`
Expected: FAIL — `GET /focus/history` is 404.

- [ ] **Step 3: Implement the handler**

Create `internal/apps/focus/handlers_history.go`:

```go
package focus

import (
	"fmt"
	"net/http"
	"strconv"
)

// sessionsPerPage is the History page's page size (spec: "History").
const sessionsPerPage = 50

// chartDays is how many days the History chart covers.
const chartDays = 30

type totalTile struct{ Label, Value string }

// chartBar is one already-positioned bar: html/template can't do
// arithmetic. Mirrors ON Flash's chartBar (PATTERNS.md, "Cross-app
// mirroring").
type chartBar struct {
	X, Y, Width, Height float64
	Label               string
}

type chartView struct {
	Bars          []chartBar
	Width, Height float64
	Peak          string
	Empty         bool
}

// dayRow is the chart's text alternative.
type dayRow struct{ Day, Focus string }

// timerRow is one line of "This month by timer". Pct is its share of the
// largest row, 0–100, the SVG bar's width.
type timerRow struct {
	Name, Color, Total string
	Pct                float64
}

type sessionRow struct {
	ID          int64
	When        string
	Name, Color string
	Focus       string
	Rounds      int
	Completed   bool
}

type historyView struct {
	Totals             []totalTile
	Chart              chartView
	Days               []dayRow
	Timers             []timerRow
	Sessions           []sessionRow
	Page               int
	NewerURL, OlderURL string
}

// buildChart lays out the daily focus as bars of minutes, the way ON
// Flash's buildChart lays out reviews.
func buildChart(days []DayTotal) chartView {
	const (
		height = 120.0
		barW   = 8.0
		barGap = 2.0
	)
	out := chartView{Height: height, Width: float64(len(days)) * (barW + barGap)}
	peak := 0
	for _, d := range days {
		peak = max(peak, d.Seconds)
	}
	out.Peak, out.Empty = FormatFocus(peak), peak == 0
	for i, d := range days {
		h := 0.0
		if peak > 0 {
			h = float64(d.Seconds) / float64(peak) * height
		}
		out.Bars = append(out.Bars, chartBar{
			X: float64(i) * (barW + barGap), Y: height - h, Width: barW, Height: h,
			Label: fmt.Sprintf("%s: %s", d.Day.Format("2 Jan"), FormatFocus(d.Seconds)),
		})
	}
	return out
}

func newTimerRows(totals []TimerTotal) []timerRow {
	var out []timerRow
	for _, t := range totals {
		pct := 0.0
		if totals[0].Seconds > 0 {
			pct = float64(t.Seconds) / float64(totals[0].Seconds) * 100
		}
		out = append(out, timerRow{Name: t.Name, Color: t.Color, Total: FormatFocus(t.Seconds), Pct: pct})
	}
	return out
}

// history is the History page (spec: "History").
func (a *App) history(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	ctx, now := r.Context(), a.store.now()

	totals, err := a.store.Totals(ctx, userID, now)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	days, err := a.store.Daily(ctx, userID, chartDays, now)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	byTimer, err := a.store.ByTimer(ctx, userID, startOfMonth(now))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	sessions, more, err := a.store.RecentSessions(ctx, userID, page, sessionsPerPage)
	if err != nil {
		a.fail(w, r, err)
		return
	}

	view := historyView{
		Totals: []totalTile{
			{"Today", FormatFocus(totals.Today)},
			{"This week", FormatFocus(totals.Week)},
			{"This month", FormatFocus(totals.Month)},
			{"This year", FormatFocus(totals.Year)},
		},
		Chart:  buildChart(days),
		Timers: newTimerRows(byTimer),
		Page:   page,
	}
	for _, d := range days {
		view.Days = append(view.Days, dayRow{Day: d.Day.Format("Mon 2 Jan"), Focus: FormatFocus(d.Seconds)})
	}
	for _, s := range sessions {
		view.Sessions = append(view.Sessions, sessionRow{
			ID: s.ID, When: s.StartedAt.Local().Format("Mon 2 Jan 2006 · 15:04"),
			Name: s.TimerName, Color: s.Color, Focus: FormatFocus(s.FocusSeconds),
			Rounds: s.RoundsDone, Completed: s.Completed,
		})
	}
	if page > 1 {
		view.NewerURL = historyURL(page - 1)
	}
	if more {
		view.OlderURL = historyURL(page + 1)
	}
	a.render(w, r, http.StatusOK, "focus/history", "History", view)
}
```

In `focus.go` `Mount`, after `r.HandleFunc("GET /run/{id}", a.run)` add `r.HandleFunc("GET /history", a.history)`.

- [ ] **Step 4: Make the confirm dialog's OK label configurable**

`home.js` will need the confirm dialog to say "End session" (Task 7); give it the hook now so the History page and home page share one dialog. In `internal/apps/focus/static/home.js`, replace `confirmThen` and the submit listener's call:

```js
	// confirmThen asks message in the app's dialog, with okLabel on the OK
	// button, and calls onOK on OK.
	function confirmThen(message, okLabel, onOK) {
		var dialog = document.getElementById("focus-confirm-dialog");
		document.getElementById("focus-confirm-message").textContent = message;
		var ok = document.getElementById("focus-confirm-ok");
		ok.textContent = okLabel;

		// Listeners are tied to this one opening of the dialog, so a
		// cancelled confirmation can never fire later (the bug reader.js
		// documents).
		var controller = new AbortController();
		dialog.addEventListener("close", function () { controller.abort(); }, { once: true });
		ok.addEventListener("click", function () {
			dialog.close();
			onOK();
		}, { signal: controller.signal });
		document.getElementById("focus-confirm-cancel").addEventListener("click", function () {
			dialog.close();
		}, { signal: controller.signal });
		dialog.showModal();
	}
```

and in the `submit` listener change `confirmThen(form.dataset.focusConfirm, function () {` to `confirmThen(form.dataset.focusConfirm, form.dataset.focusConfirmOk || "Delete", function () {`. Update the file's header comment's first sentence to: `// ON Focus's home and History page script. Forms marked data-focus-confirm ask first,`.

- [ ] **Step 5: The template**

Create `internal/apps/focus/templates/history.html`:

```html
{{/* home.js gives the session delete forms their confirmation dialog. */}}
{{define "head"}}<script src="/focus/home.js" defer></script>{{end}}

{{define "content"}}
{{$d := .Data}}
<div class="focus-page stack">
	<div class="focus-toolbar">
		<h1>History</h1>
		<a class="button" href="/focus/">← Timers</a>
	</div>

	<div class="focus-totals">
		{{range $d.Totals}}
		<div class="focus-total">
			<div class="value">{{.Value}}</div>
			<div class="label">{{.Label}}</div>
		</div>
		{{end}}
	</div>

	<figure class="focus-chart">
		<figcaption>Focus per day, last 30 days</figcaption>
		{{if $d.Chart.Empty}}
		<p class="empty">Nothing in the last 30 days.</p>
		{{else}}
		<svg viewBox="0 0 {{$d.Chart.Width}} {{$d.Chart.Height}}" role="img"
		     aria-label="Focus per day, last 30 days, peak {{$d.Chart.Peak}}" preserveAspectRatio="none">
			{{range $d.Chart.Bars}}
			<rect x="{{.X}}" y="{{.Y}}" width="{{.Width}}" height="{{.Height}}" rx="1"><title>{{.Label}}</title></rect>
			{{end}}
		</svg>
		{{end}}
	</figure>
	{{if not $d.Chart.Empty}}
	<details class="focus-daily">
		<summary>Daily figures (text alternative to the chart above)</summary>
		<table class="focus-table">
			<thead><tr><th>Day</th><th>Focus</th></tr></thead>
			<tbody>{{range $d.Days}}<tr><td>{{.Day}}</td><td>{{.Focus}}</td></tr>{{end}}</tbody>
		</table>
	</details>
	{{end}}

	<h2>This month by timer</h2>
	{{if $d.Timers}}
	<ul class="focus-by-timer">
		{{range $d.Timers}}
		<li class="swatch-c-{{.Color}}">
			<span class="focus-by-timer-name">{{.Name}}</span>
			<svg class="focus-by-timer-bar" viewBox="0 0 100 4" preserveAspectRatio="none" aria-hidden="true"><rect width="{{.Pct}}" height="4" rx="1"></rect></svg>
			<span class="focus-by-timer-total">{{.Total}}</span>
		</li>
		{{end}}
	</ul>
	{{else}}
	<p class="focus-muted">Nothing this month yet.</p>
	{{end}}

	<h2>Recent sessions</h2>
	{{if $d.Sessions}}
	<table class="focus-table focus-sessions">
		<thead><tr><th>When</th><th>Timer</th><th>Focus</th><th>Rounds</th><th><span class="visually-hidden">Actions</span></th></tr></thead>
		<tbody>
			{{range $d.Sessions}}
			<tr>
				<td>{{.When}}</td>
				<td><span class="focus-session-timer swatch-c-{{.Color}}">{{.Name}}</span>{{if not .Completed}} <span class="focus-early">stopped early</span>{{end}}</td>
				<td>{{.Focus}}</td>
				<td>{{.Rounds}}</td>
				<td>
					<form method="post" action="/focus/sessions/{{.ID}}/delete" data-focus-confirm="Delete this {{.Name}} session from your history?">
						<input type="hidden" name="{{csrfField}}" value="{{$.Shell.CSRFToken}}">
						<input type="hidden" name="page" value="{{$d.Page}}">
						<button class="danger" type="submit" aria-label="Delete the {{.Name}} session of {{.When}}">Delete</button>
					</form>
				</td>
			</tr>
			{{end}}
		</tbody>
	</table>
	{{else}}
	<div class="focus-empty"><p>No sessions here yet. Finish a timer with <b>Keep history</b> on and it shows up here.</p></div>
	{{end}}
	{{if or $d.NewerURL $d.OlderURL}}
	<nav class="focus-pager" aria-label="Session pages">
		{{with $d.NewerURL}}<a href="{{.}}">← Newer</a>{{end}}
		{{with $d.OlderURL}}<a href="{{.}}">Older →</a>{{end}}
	</nav>
	{{end}}
</div>
{{template "focus-confirm"}}
{{end}}
```

- [ ] **Step 6: Styles**

In `internal/ui/static/app.css`, after the `.focus-resume a { … }` rule at the end of the ON Focus section, add:

```css
/* ON Focus: History (spec: "History"). Totals and chart mirror ON Flash's
   stats tiles and chart. */
.focus-totals {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(8rem, 1fr));
	gap: var(--s-3);
}
.focus-total { background: var(--c-bg-subtle); border-radius: var(--radius); padding: var(--s-3); }
.focus-total .value { font-size: var(--fs-xl); }
.focus-total .label { color: var(--c-text-dim); font-size: var(--fs-xs); }

.focus-chart { margin: 0; }
.focus-chart figcaption { font-size: var(--fs-sm); color: var(--c-text-dim); margin-bottom: var(--s-2); }
.focus-chart svg { width: 100%; height: 120px; display: block; overflow: visible; }
.focus-chart rect { fill: var(--c-accent); }
.focus-chart rect:hover { fill: var(--c-text); }
.focus-chart .empty, .focus-muted { color: var(--c-text-dim); }

.focus-by-timer { list-style: none; padding: 0; margin: 0; display: grid; gap: var(--s-2); }
.focus-by-timer li {
	display: grid;
	grid-template-columns: minmax(6rem, 14rem) 1fr auto;
	align-items: center;
	gap: var(--s-3);
}
.focus-by-timer-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.focus-by-timer-bar { width: 100%; height: 0.5rem; display: block; }
.focus-by-timer-bar rect { fill: var(--swatch); }
.focus-by-timer-total { font-variant-numeric: tabular-nums; }

.focus-table { width: 100%; border-collapse: collapse; font-size: var(--fs-sm); }
.focus-table th, .focus-table td { text-align: left; padding: var(--s-2); border-bottom: var(--border); }
.focus-table th { color: var(--c-text-dim); font-weight: 500; }
.focus-sessions td:last-child { text-align: right; }
.focus-session-timer::before {
	content: "";
	display: inline-block;
	width: 0.6em;
	height: 0.6em;
	margin-right: 0.4em;
	border-radius: 50%;
	background: var(--swatch);
}
.focus-early {
	font-size: var(--fs-xs);
	color: var(--c-text-dim);
	border: var(--border);
	border-radius: 999px;
	padding: 0 var(--s-2);
	white-space: nowrap;
}
.focus-pager { display: flex; justify-content: space-between; }
.focus-pager a:only-child:last-child { margin-left: auto; }

@media (max-width: 640px) {
	.focus-by-timer li { grid-template-columns: 1fr auto; }
	.focus-by-timer-bar { grid-column: 1 / -1; grid-row: 2; }
	.focus-sessions th:nth-child(4), .focus-sessions td:nth-child(4) { display: none; }
}
```

The ON Focus section has no media-query block yet, so this one starts it; later Focus mobile rules go into it (PATTERNS.md "Mobile/touch CSS added to the existing media-query block").

- [ ] **Step 7: Run the tests**

Run: `gofmt -w internal/apps/focus && go test ./internal/apps/focus/ -count=1`
Expected: PASS.

- [ ] **Step 8: Look at it in the browser**

Build and run on a scratch data dir (`go build -o /tmp/onsuite-f3 ./cmd/onsuite`, `onsuite user add` once, `serve`), configure `.claude/launch.json` for it if it isn't already, open it with `preview_start`. Record two or three sessions by POSTing from the browser console (`fetch("/focus/sessions", …)` with the CSRF header from `document.body`'s `hx-headers`), then open `/focus/history`: totals, chart bars, per-timer bars in their colours, the sessions table, the delete dialog (Cancel and Delete), light and dark mode, and mobile width (`resize_window` preset `mobile`, then `desktop`).

- [ ] **Step 9: Commit**

```bash
git add internal/apps/focus/handlers_history.go internal/apps/focus/handlers_history_test.go internal/apps/focus/templates/history.html internal/apps/focus/focus.go internal/apps/focus/static/home.js internal/ui/static/app.css
git commit -m "feat(focus): History page — totals, 30-day chart, per timer, sessions (#495)"
```

---

### Task 5: Home — History button and today strip

**Files:**
- Modify: `internal/apps/focus/handlers.go`, `internal/apps/focus/focus.go`, `internal/apps/focus/templates/index.html`, `internal/ui/static/app.css`
- Test: `internal/apps/focus/handlers_test.go`

**Interfaces:**
- Consumes: `Totals`, `FormatFocus` (Task 3).
- Produces:
  - `type todayView struct { Focus, Sessions, Week string }`; `func newToday(t Totals) *todayView` (nil when `!t.Any`).
  - `indexView.Today *todayView`.
  - Block `focus-today` in `index.html`, inside `<div data-focus-today>`.
  - `GET /focus/today` — the `focus-today` fragment (empty body when there is no history).

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/focus/handlers_test.go`:

```go
func TestIndexLinksToHistory(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/focus/")
	doc.MustHave(`.focus-toolbar a[href="/focus/history"]`)
}

func TestTodayStripOnlyOnceThereIsHistory(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(localAt(10, 7, 12, 0))
	uid := s.Alice.User.ID
	seedTimer(t, s, uid, single("Reading", 30))

	doc := s.Get(t, s.Alice, "/focus/")
	doc.MustHave("[data-focus-today]")
	doc.MustNotHave(".focus-today")

	// Only an old session: the strip shows, with nothing today.
	seedSession(t, s, uid, sessionInput("old", "Reading", localAt(9, 1, 9, 0), 30))
	doc = s.Get(t, s.Alice, "/focus/")
	if got := htmlassert.Text(doc.MustHave(".focus-today")); got != "Today 0m focused 0 sessions This week 0m" {
		t.Errorf("strip = %q", got)
	}

	seedSession(t, s, uid, sessionInput("a", "Reading", localAt(10, 7, 8, 0), 30))
	seedSession(t, s, uid, sessionInput("b", "Reading", localAt(10, 6, 8, 0), 45))
	seedSession(t, s, s.Bob.User.ID, sessionInput("z", "Bob's", localAt(10, 7, 9, 0), 60))
	doc = s.Get(t, s.Alice, "/focus/")
	if got := htmlassert.Text(doc.MustHave(".focus-today")); got != "Today 30m focused 1 session This week 1h 15m" {
		t.Errorf("strip = %q", got)
	}
}

func TestTodayFragment(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(localAt(10, 7, 12, 0))
	rec := s.Do(t, s.Alice, httptestGet("/focus/today"))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "" {
		t.Errorf("no history: %d %q, want 200 and an empty body", rec.Code, rec.Body.String())
	}
	seedSession(t, s, s.Alice.User.ID, sessionInput("a", "Reading", localAt(10, 7, 8, 0), 30))
	rec = s.Do(t, s.Alice, httptestGet("/focus/today"))
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := htmlassert.Text(doc.MustHave(".focus-today")); got != "Today 30m focused 1 session This week 30m" {
		t.Errorf("fragment = %q", got)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("the fragment must not be a whole page")
	}
}
```

`htmlassert.Text` collapses runs of whitespace, so template indentation doesn't matter.

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/focus/ -run 'IndexLinksToHistory|TodayStrip|TodayFragment' -count=1`
Expected: FAIL — no History link, no `[data-focus-today]`, `/focus/today` 404.

- [ ] **Step 3: Implement**

In `internal/apps/focus/handlers.go`:

1. Add after `type indexView struct { … }` (and add the `Today` field to it):

```go
type indexView struct {
	UserID int64 // the resume banner only shows this user's session
	Today  *todayView
	Tiles  []tileView
}

// todayView is the home page's today strip (spec: "Home").
type todayView struct {
	Focus, Sessions, Week string
}

// newToday is the strip for t, or nil for someone who has never recorded
// a session: the strip only appears once there is history (F3 plan).
func newToday(t Totals) *todayView {
	if !t.Any {
		return nil
	}
	sessions := strconv.Itoa(t.TodaySessions) + " sessions"
	if t.TodaySessions == 1 {
		sessions = "1 session"
	}
	return &todayView{Focus: FormatFocus(t.Today), Sessions: sessions, Week: FormatFocus(t.Week)}
}
```

2. In `index`, after loading timers:

```go
	totals, err := a.store.Totals(r.Context(), userID, a.store.now())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	view := indexView{UserID: userID, Today: newToday(totals)}
```

(replacing `view := indexView{UserID: userID}`).

3. Add a handler:

```go
// today is the today strip alone, for home.js to refresh it after the
// banner records a session.
func (a *App) today(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	totals, err := a.store.Totals(r.Context(), userID, a.store.now())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.deps.Render.Fragment(w, http.StatusOK, "focus/index", "focus-today", newToday(totals)); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
```

In `focus.go` `Mount`, after `r.HandleFunc("GET /{$}", a.index)` add `r.HandleFunc("GET /today", a.today)`.

In `internal/apps/focus/templates/index.html`, replace the toolbar and add the strip under it:

```html
	<div class="focus-toolbar">
		<h1>Timers</h1>
		<div class="focus-toolbar-actions">
			<a class="button" href="/focus/history">History</a>
			<a class="button primary" href="/focus/new">+ New timer</a>
		</div>
	</div>
	{{/* home.js refreshes this from GET /focus/today after the banner
	     records a session. */}}
	<div data-focus-today>{{template "focus-today" .Data.Today}}</div>
```

and at the end of the file (after `{{end}}` of `content`):

```html
{{/* focus-today is the today strip (spec: "Home"); nothing for someone
     with no history yet. */}}
{{define "focus-today"}}{{with .}}
<p class="focus-today">
	<span>Today <b>{{.Focus}}</b> focused</span>
	<span><b>{{.Sessions}}</b></span>
	<span>This week <b>{{.Week}}</b></span>
</p>
{{end}}{{end}}
```

Note: `{{.Sessions}}` already reads "3 sessions"; the bold covers the whole phrase, as the mockup bolds the number. If the test's expected text differs only by spacing, fix the test's whitespace handling, not the markup.

In `app.css`, next to `.focus-toolbar`:

```css
.focus-toolbar-actions { display: flex; gap: var(--s-2); flex-wrap: wrap; }
.focus-today {
	margin: 0;
	padding: var(--s-2) var(--s-3);
	background: var(--c-bg-subtle);
	border-radius: var(--radius);
	color: var(--c-text-dim);
	font-size: var(--fs-sm);
	display: flex;
	flex-wrap: wrap;
	gap: var(--s-2) var(--s-5);
}
.focus-today b { color: var(--c-text); }
```

- [ ] **Step 4: Run the tests**

Run: `gofmt -w internal/apps/focus && go test ./internal/apps/focus/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/focus/handlers.go internal/apps/focus/handlers_test.go internal/apps/focus/focus.go internal/apps/focus/templates/index.html internal/ui/static/app.css
git commit -m "feat(focus): History button and today strip on the home page (#495)"
```

---

### Task 6: Recording from the running page (`record.js`)

**Files:**
- Create: `internal/apps/focus/static/record.js`
- Modify: `internal/apps/focus/static/session.js`, `internal/apps/focus/static/focus.js`, `internal/apps/focus/templates/run.html`, `internal/apps/focus/templates/index.html` (script tag), `internal/apps/focus/focus.go`, `internal/ui/static/app.css`
- Test: `internal/apps/focus/handlers_test.go`

**Interfaces:**
- Consumes: `POST /focus/sessions` (Task 2); `window.OnFocus.session` (F2).
- Produces:
  - `OnFocus.session.end(s, now)` — ends the session now: banks focus done so far, `finished = true`, `completed = false`, `waiting = false`, `endedAt = now`. No-op if already finished.
  - `window.OnFocus.record = { eligible(s) → bool, send(s) → Promise<"saved"|"rejected"|"failed"> }`.
  - Route `GET /focus/record.js`.
  - Run page markup: `[data-focus-done-status]` (role=status), `button[data-focus-retry]` (hidden).

- [ ] **Step 1: Write the failing tests**

In `internal/apps/focus/handlers_test.go`:

1. In `TestScriptsAreServed`, add `"record.js",` to the list.
2. In `TestRunPageForAnIntervalsTimer`, change the script list to `[]string{"/focus/session.js", "/focus/chimes.js", "/focus/record.js", "/focus/focus.js"}`.
3. In `TestIndexHasTheResumeBannerSlot`, after `doc.MustHave(`script[src="/focus/session.js"]`)` add `doc.MustHave(`script[src="/focus/record.js"]`)`.
4. Append:

```go
func TestRunPageDoneScreenCanRetry(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
	doc := s.Get(t, s.Alice, "/focus/run/"+itoa(tm.ID))
	status := doc.MustHave("[data-focus-done] [data-focus-done-status]")
	if role, _ := htmlassert.Attr(status, "role"); role != "status" {
		t.Errorf("done status role = %q, want status", role)
	}
	retry := doc.MustHave("[data-focus-done] button[data-focus-retry]")
	if _, ok := htmlassert.Attr(retry, "hidden"); !ok {
		t.Error("Retry should start hidden")
	}
}

// The scripts must load in dependency order: each uses what the one
// before it defines.
func TestScriptOrder(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
	for path, want := range map[string]string{
		"/focus/run/" + itoa(tm.ID): "/focus/session.js /focus/chimes.js /focus/record.js /focus/focus.js",
		"/focus/":                   "/focus/session.js /focus/record.js /focus/home.js",
	} {
		var got []string
		for _, n := range s.Get(t, s.Alice, path).QueryAll("script[src]") {
			if src, _ := htmlassert.Attr(n, "src"); strings.HasPrefix(src, "/focus/") {
				got = append(got, src)
			}
		}
		if strings.Join(got, " ") != want {
			t.Errorf("%s scripts = %v, want %s", path, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./internal/apps/focus/ -run 'Scripts|RunPage|ResumeBanner|ScriptOrder' -count=1`
Expected: FAIL — `/focus/record.js` 404, script missing, no done status.

- [ ] **Step 3: `record.js`**

Create `internal/apps/focus/static/record.js`:

```js
// ON Focus's session recording (spec: "Recording a session"), shared by
// the running page (focus.js) and the home page's banner (home.js). It only
// talks to the server; callers clear the stored session once it is saved,
// so an unsaved one stays in localStorage for Retry.
"use strict";

(function () {
	var S = window.OnFocus.session;

	// eligible: an ended session whose timer had Keep history on when it
	// started, with at least a minute of focus.
	function eligible(s) {
		return s.finished && s.keepHistory && S.focusSeconds(s, s.endedAt) >= 60;
	}

	function body(s) {
		return {
			client_id: s.clientId,
			timer_id: s.timerId,
			timer_name: s.timerName,
			color: s.color,
			started_at: s.startedAt,
			ended_at: s.endedAt,
			focus_seconds: S.focusSeconds(s, s.endedAt),
			rounds_done: s.roundsDone,
			completed: s.completed
		};
	}

	// csrfToken is the token htmx sends, from <body hx-headers>; mirrors
	// later.js's.
	function csrfToken() {
		try {
			return JSON.parse(document.body.getAttribute("hx-headers"))["X-CSRF-Token"] || "";
		} catch (e) {
			return "";
		}
	}

	// send POSTs an ended session. It resolves to "saved" (the server has
	// it, including "already recorded"), "rejected" (400/422: it never
	// will) or "failed" (offline, signed out, a server error: worth a
	// Retry). keepalive lets the request finish if the page unloads first —
	// Exit, then close the tab — and, unlike sendBeacon, carries the CSRF
	// header (F3 plan). redirect "manual" makes a bounce to the login page
	// a failure rather than a 200.
	function send(s) {
		return fetch("/focus/sessions", {
			method: "POST",
			credentials: "same-origin",
			keepalive: true,
			redirect: "manual",
			headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken() },
			body: JSON.stringify(body(s))
		}).then(function (res) {
			if (res.ok) return "saved";
			if (res.status === 400 || res.status === 422) {
				console.warn("ON Focus: the server refused this session (" + res.status + "); dropping it");
				return "rejected";
			}
			return "failed";
		}, function () {
			return "failed";
		});
	}

	window.OnFocus.record = { eligible: eligible, send: send };
})();
```

In `focus.go` `Mount`, add `r.HandleFunc("GET /record.js", a.script("record.js"))` after the `session.js` route.

- [ ] **Step 4: `session.js` — `end` and a stricter `valid`**

In `internal/apps/focus/static/session.js`:

1. After `restart`, add:

```js
	// end stops the session now, as Exit and the home banner's End do:
	// focus time done so far counts, and it didn't run to its end. Callers
	// advance(s, now) first, so a phase that ran out still counts as done.
	function end(s, now) {
		if (s.finished) return;
		bankCurrent(s, now);
		s.finished = true;
		s.completed = false;
		s.waiting = false;
		s.endedAt = now;
	}
```

2. Export it: add `end: end,` to the `window.OnFocus.session = { … }` object (on the controls line: `pause: pause, resume: resume, skip: skip, restart: restart, next: next, end: end,`).

3. Replace `valid` with (the fields F3 now sends to the server, or uses as a class name, are checked too — first bullet of #545):

```js
	function valid(s) {
		return !!s && s.v === VERSION && typeof s.userId === "number" &&
			typeof s.clientId === "string" && s.clientId !== "" &&
			typeof s.timerId === "number" && typeof s.timerName === "string" &&
			typeof s.color === "string" && /^[a-z]+$/.test(s.color) &&
			Array.isArray(s.phases) && s.phases.length > 0 &&
			s.phases.every(function (p) {
				return p && typeof p.kind === "string" && typeof p.seconds === "number" && p.seconds > 0;
			}) &&
			typeof s.phaseIndex === "number" && s.phaseIndex >= 0 && s.phaseIndex < s.phases.length &&
			typeof s.startedAt === "number" && (s.endedAt === null || typeof s.endedAt === "number") &&
			typeof s.phaseStartedAt === "number" && typeof s.pausedTotalInPhase === "number" &&
			(s.pausedAt === null || typeof s.pausedAt === "number") &&
			typeof s.focusSecondsBanked === "number" && typeof s.roundsDone === "number" &&
			typeof s.waiting === "boolean" && typeof s.finished === "boolean";
	}
```

- [ ] **Step 5: Run page markup and script tags**

In `internal/apps/focus/templates/run.html`:

1. The `head` block becomes:

```html
{{define "head"}}<script src="/focus/session.js" defer></script><script src="/focus/chimes.js" defer></script><script src="/focus/record.js" defer></script><script src="/focus/focus.js" defer></script>{{end}}
```

2. Replace the Done block with:

```html
	<div class="focus-done" data-focus-done hidden>
		<p class="focus-done-title" data-focus-done-text>Done</p>
		<p class="focus-done-status" data-focus-done-status role="status"></p>
		<div class="focus-controls">
			<button type="button" data-focus-retry hidden>Retry</button>
			<a class="button primary" href="/focus/">Back to timers</a>
		</div>
	</div>
```

In `index.html`, the `head` block becomes:

```html
{{define "head"}}<script src="/focus/session.js" defer></script><script src="/focus/record.js" defer></script><script src="/focus/home.js" defer></script>{{end}}
```

In `app.css`, after `.focus-done-title { … }`:

```css
.focus-done-status { margin: 0; color: var(--c-text-dim); min-height: 1.5em; }
```

(`.focus-runner [hidden]` already hides the Retry button.)

- [ ] **Step 6: `focus.js` — record on finish, Exit and replace**

In `internal/apps/focus/static/focus.js`:

1. After `var chimes = window.OnFocus.chimes;` add `var R = window.OnFocus.record;`.
2. In `el`, add after `doneText: …`:

```js
		doneStatus: root.querySelector("[data-focus-done-status]"),
		retry: root.querySelector("[data-focus-retry]"),
```

3. Replace `finish` with:

```js
	// finish shows the Done screen and records the session (spec:
	// "Recording a session"). leaving is Exit: once the server has it, go
	// home. Exit on a session too short to keep goes home at once.
	function finish(now, leaving) {
		window.clearInterval(ticker);
		if (leaving && !R.eligible(s)) {
			S.clear();
			window.location.assign("/focus/");
			return;
		}
		el.main.hidden = true;
		el.done.hidden = false;
		el.doneText.textContent = "Done — " + S.focused(S.focusSeconds(s, now)) + " focused";
		document.title = "Done · " + s.timerName;
		record(leaving);
	}

	// record sends the ended session. It stays in localStorage until the
	// server has it, so Retry here — or the home page's banner later — can
	// try again.
	function record(leaving) {
		el.retry.hidden = true;
		if (!R.eligible(s)) {
			S.clear();
			el.doneStatus.textContent = s.keepHistory ? "Under a minute of focus — not added to your history." : "";
			return;
		}
		el.doneStatus.textContent = "Saving…";
		R.send(s).then(function (result) {
			if (result === "failed") {
				el.doneStatus.textContent = "Couldn't save this session.";
				el.retry.hidden = false;
				return;
			}
			S.clear();
			if (leaving) {
				window.location.assign("/focus/");
				return;
			}
			el.doneStatus.textContent = result === "saved" ? "Saved to your history." : "Couldn't add this session to your history.";
		});
	}
	el.retry.addEventListener("click", function () { record(false); });
```

4. Replace `exit` with:

```js
	// exit asks first while a session is running, then ends and records it
	// (spec: "Controls": Exit).
	function exit() {
		if (!s || s.finished) {
			window.location.assign("/focus/");
			return;
		}
		confirmThen("End this session?", "End session", "Keep going", function () {
			var now = Date.now();
			S.advance(s, now); // a phase that ran out while the dialog was open still counts
			S.end(s, now);
			S.save(s);
			finish(now, true);
		});
	}
```

5. In the "Start, resume or replace" section, replace the `else { … }` branch for another timer's session with:

```js
	} else {
		S.advance(stored, Date.now());
		if (stored.finished) {
			// Another timer's session already ran out: record it, then
			// start this one.
			recordThenBegin(stored);
		} else {
			confirmThen(
				"End " + stored.timerName + " and start " + config.name + "?",
				"Start " + config.name, "Back to " + stored.timerName,
				function () {
					S.end(stored, Date.now());
					recordThenBegin(stored);
				},
				function () {
					var timerId = Number(stored.timerId);
					if (!Number.isFinite(timerId) || Math.floor(timerId) !== timerId) return;
					window.location.assign("/focus/run/" + encodeURIComponent(String(timerId)));
				}
			);
		}
	}
```

and add, just above the "Start, resume or replace" comment:

```js
	// recordThenBegin records a session being replaced before this timer's
	// takes its place: there is one session per browser. If it can't be
	// saved it is dropped with a warning (F3 plan: being offline at exactly
	// this moment is rare).
	function recordThenBegin(old) {
		if (!R.eligible(old)) {
			begin();
			return;
		}
		R.send(old).then(function (result) {
			if (result === "failed") console.warn("ON Focus: couldn't save " + old.timerName + " before replacing it");
			begin();
		});
	}
```

6. Update the file header comment's last line to: `// localStorage on every change, draws the ring, dots and controls, and records the session when it ends (record.js).` Remove the remaining "#495" comments in `focus.js` that the changes above replaced (`grep -n 495 internal/apps/focus/static/focus.js` → nothing).

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/apps/focus/ -count=1`
Expected: PASS.

- [ ] **Step 8: Check `session.end` and `record.body` by hand**

With Node available (`node --version`), save this as a throwaway in the scratchpad (not the repo) and run it:

```js
global.window = { OnFocus: {}, localStorage: null };
global.document = { body: { getAttribute: () => '{"X-CSRF-Token":"t"}' } };
require(process.cwd() + "/internal/apps/focus/static/session.js");
const S = window.OnFocus.session;
const phases = [{ kind: "focus", seconds: 3000, round: 1 }, { kind: "break", seconds: 600, round: 1 }, { kind: "focus", seconds: 3000, round: 2 }];
const s = S.create({ userId: 1, id: 7, name: "Deep work", color: "blue", chime: "bell", autoAdvance: true, keepHistory: true, rounds: 2, phases }, 0);
S.advance(s, 3600e3 + 20 * 60e3); // first round, break, 20 min of round 2
S.end(s, 3600e3 + 20 * 60e3);
console.log(s.finished, s.completed, s.endedAt, s.roundsDone, S.focusSeconds(s, s.endedAt)); // true false 4800000 1 4200
```

Expected output: `true false 4800000 1 4200` (50 min + 20 min of focus; the interrupted round isn't counted as done).

- [ ] **Step 9: Verify in the browser**

Rebuild, restart the preview. With a 1-minute single timer ("Test", Keep history on) and a 2-round 1/1 intervals timer:
1. Let the single timer run out: Done screen says "Saving…" then "Saved to your history."; `/focus/history` lists it as completed; `localStorage.getItem("onsuite.focus.session")` is `null`.
2. Start it, Exit after 10 s: goes home at once, nothing recorded (under a minute).
3. Start the intervals timer, wait past 1 min, Exit → End session: Done flashes "Saving…" and goes home; History lists it with "stopped early".
4. Offline save: in the console run `window.fetch = () => Promise.reject(new Error("offline"))` on the running page before it finishes; at the end the Done screen says "Couldn't save this session." with Retry and the state stays in `localStorage`. Reload the page (restoring real `fetch`): the same session shows Done and saves. (Retry itself: set `fetch` back to the real one with `delete window.fetch` and click Retry.)
5. Timer with Keep history off: Done shows no status line and nothing is recorded.
6. Replace: start the intervals timer, wait past a minute, open the single timer's ▶ → "End Deep work and start Test?" → Start: History gains the intervals session; the single timer runs.
7. `read_console_messages` with `onlyErrors`: none.

- [ ] **Step 10: Commit**

```bash
git add internal/apps/focus/static/record.js internal/apps/focus/static/session.js internal/apps/focus/static/focus.js internal/apps/focus/templates/run.html internal/apps/focus/templates/index.html internal/apps/focus/focus.go internal/apps/focus/handlers_test.go internal/ui/static/app.css
git commit -m "feat(focus): record sessions from the running page, with Retry (#495)"
```

---

### Task 7: Home banner — record, Retry and End (#546)

**Files:**
- Modify: `internal/apps/focus/templates/index.html`, `internal/apps/focus/static/home.js`, `internal/ui/static/app.css`
- Test: `internal/apps/focus/handlers_test.go`

**Interfaces:**
- Consumes: `OnFocus.session` (`load`, `advance`, `end`, `save`, `clear`, `focused`, `focusSeconds`, `clock`, `remaining`, `upcoming`), `OnFocus.record` (Task 6), `confirmThen(message, okLabel, onOK)` (Task 4), `GET /focus/today` and `[data-focus-today]` (Task 5).
- Produces: banner markup `[data-focus-resume]` containing `[data-focus-resume-text]`, `button[data-focus-resume-end]`, `button[data-focus-resume-retry]` (both buttons start hidden).

- [ ] **Step 1: Write the failing test**

In `TestIndexHasTheResumeBannerSlot` (`handlers_test.go`), after the `data-user-id` check inside the loop, add:

```go
		doc.MustHave("[data-focus-resume] [data-focus-resume-text]")
		for _, sel := range []string{"[data-focus-resume] button[data-focus-resume-end]", "[data-focus-resume] button[data-focus-resume-retry]"} {
			if _, ok := htmlassert.Attr(doc.MustHave(sel), "hidden"); !ok {
				t.Errorf("seed=%v: %s should start hidden", seed, sel)
			}
		}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/apps/focus/ -run ResumeBanner -count=1`
Expected: FAIL — no `[data-focus-resume-text]`.

- [ ] **Step 3: Banner markup and styles**

In `index.html`, replace the banner paragraph and its comment with:

```html
	{{/* home.js fills this in when a session is stored in this browser
	     (spec: "Resume"): a link back to it and End, or — once it has
	     ended — whether it was saved, with Retry. */}}
	<div class="focus-resume" data-focus-resume data-user-id="{{.Data.UserID}}" hidden>
		<span class="focus-resume-text" data-focus-resume-text></span>
		<button type="button" class="focus-resume-action" data-focus-resume-end hidden>End</button>
		<button type="button" class="focus-resume-action" data-focus-resume-retry hidden>Retry</button>
	</div>
```

In `app.css`, replace the `.focus-resume { … }` rule with:

```css
.focus-resume {
	margin: 0;
	padding: var(--s-2) var(--s-3);
	border: var(--border);
	border-left: 4px solid var(--swatch, var(--c-accent));
	border-radius: var(--radius);
	color: var(--c-text-dim);
	font-size: var(--fs-sm);
	display: flex;
	align-items: center;
	flex-wrap: wrap;
	gap: var(--s-2) var(--s-3);
}
.focus-resume[hidden], .focus-resume [hidden] { display: none; }
.focus-resume-text { flex: 1 1 16rem; }
.focus-resume-action { font-size: var(--fs-sm); padding: var(--s-1) var(--s-3); }
```

- [ ] **Step 4: `home.js` banner**

Replace everything in `home.js` from the comment `// The resume banner (spec: "Resume"): …` to the end of `updateBanner` with:

```js
	// The resume banner (spec: "Resume"): a session stored in this browser,
	// read from the state the running page keeps. While it runs: a link
	// back to it, and End (#546 — it also clears a session whose timer was
	// deleted). Once it has ended: recorded on the spot, with Retry if that
	// fails.
	var S = window.OnFocus && window.OnFocus.session;
	var R = window.OnFocus && window.OnFocus.record;
	var banner = document.querySelector("[data-focus-resume]");
	var stored = S && R && banner ? S.load(Number(banner.dataset.userId)) : null;
	var bannerTicker = 0;
	var text, link, endButton, retryButton;
	if (stored) {
		var timerId = Number(stored.timerId);
		if (!Number.isFinite(timerId) || timerId <= 0 || Math.floor(timerId) !== timerId) {
			S.clear();
			stored = null;
		} else {
			text = banner.querySelector("[data-focus-resume-text]");
			endButton = banner.querySelector("[data-focus-resume-end]");
			retryButton = banner.querySelector("[data-focus-resume-retry]");
			link = document.createElement("a");
			link.href = "/focus/run/" + timerId;
			banner.classList.add("swatch-c-" + stored.color);
			banner.hidden = false;
			endButton.addEventListener("click", function () {
				confirmThen("End " + stored.timerName + "?", "End session", function () {
					var now = Date.now();
					S.advance(stored, now); // a phase that ran out still counts
					S.end(stored, now);
					S.save(stored);
					updateBanner();
				});
			});
			retryButton.addEventListener("click", recordStored);
			bannerTicker = window.setInterval(updateBanner, 1000);
			updateBanner();
		}
	}

	function updateBanner() {
		var now = Date.now();
		S.advance(stored, now);
		if (stored.finished) {
			window.clearInterval(bannerTicker);
			endButton.hidden = true;
			recordStored();
			return;
		}
		endButton.hidden = false;
		if (link.parentNode !== text) {
			text.textContent = "";
			text.appendChild(link);
		}
		if (stored.waiting) {
			var up = S.upcoming(stored);
			link.textContent = stored.timerName + " — ready for " + (up.kind === "focus" ? "round " + up.round : "a break");
			return;
		}
		var left = S.clock(S.remaining(stored, now)) + " left";
		link.textContent = "Resume " + stored.timerName + " — " + (stored.pausedAt !== null ? "paused, " + left : left);
	}

	// recordStored records the ended session (spec: "Resume": "recorded on
	// the spot"), then refreshes the today strip so it includes it.
	function recordStored() {
		retryButton.hidden = true;
		var summary = stored.timerName + (stored.completed ? " finished" : " ended") + " — " +
			S.focused(S.focusSeconds(stored, stored.endedAt)) + " focused.";
		if (!R.eligible(stored)) {
			S.clear();
			text.textContent = summary;
			return;
		}
		text.textContent = summary + " Saving…";
		R.send(stored).then(function (result) {
			if (result === "failed") {
				text.textContent = summary + " Couldn't save this session.";
				retryButton.hidden = false;
				return;
			}
			S.clear();
			text.textContent = result === "saved" ? summary + " Saved to your history." : summary;
			if (result === "saved" && window.htmx) {
				htmx.ajax("GET", "/focus/today", { target: "[data-focus-today]", swap: "innerHTML" });
			}
		});
	}
```

Update `home.js`'s header comment's last sentence to: `// It also shows the resume banner (and records a session that has ended) and asks for notification permission on ▶.`

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/apps/focus/ -count=1`
Expected: PASS.

- [ ] **Step 6: Verify in the browser**

Rebuild, restart the preview:
1. Start the 1-minute timer, go back to `/focus/` straight away: banner "Resume Test — 00:5x left" with **End**. Wait for it to run out on the home page: banner says "Test finished — 1m focused. Saving…" then "… Saved to your history."; the today strip appears (first session ever) or its numbers go up, without a page reload.
2. Start the intervals timer, pause it, go home, click **End** → dialog "End Deep work?" with an **End session** button → Cancel keeps it; End session records it if it had a minute of focus (History shows "stopped early") and the banner shows "Deep work ended — …".
3. #546: start a timer, pause it, go home, delete that timer from its ⋯ menu, back on home the banner still shows; **End** records it (History lists it under the old name and colour) and clears the banner. Reload: no banner.
4. Failure: on the home page, override `window.fetch = () => Promise.reject(new Error("offline"))` before a stored session ends; the banner shows "Couldn't save this session." with **Retry**; `delete window.fetch`, click Retry → saved.
5. The timer delete dialog still says **Delete** (the OK label default).
6. Dark mode and mobile width: banner text and buttons wrap without overflow.
7. `read_console_messages` with `onlyErrors`: none.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/focus/templates/index.html internal/apps/focus/static/home.js internal/apps/focus/handlers_test.go internal/ui/static/app.css
git commit -m "feat(focus): home banner records ended sessions, Retry, End (#495, #546)"
```

---

### Task 8: Export and admin card

**Files:**
- Create: `internal/apps/focus/export.go`
- Modify: `internal/apps/focus/focus.go` (interface assertions)
- Test: `internal/apps/focus/export_test.go`

**Interfaces:**
- Consumes: `Store.Timers`, `Store.querySessions`, `sessionColumns`, `FormatFocus`.
- Produces: `func (a *App) Export(ctx, handle *sql.DB, userID int64) (any, error)` (`app.Exporter`), `func (a *App) Stats(ctx, handle *sql.DB) ([]app.Stat, error)` (`app.Stater`); `Store.Export`, `Store.Stats`.

- [ ] **Step 1: Write the failing tests**

Create `internal/apps/focus/export_test.go`:

```go
package focus_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// exported mirrors the JSON shape onsuite export writes for ON Focus.
type exported struct {
	Timers []struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		Color        string `json:"color"`
		Kind         string `json:"kind"`
		FocusMinutes int    `json:"focus_minutes"`
		Rounds       int    `json:"rounds"`
		KeepHistory  bool   `json:"keep_history"`
	} `json:"timers"`
	Sessions []struct {
		ID           int64     `json:"id"`
		TimerID      *int64    `json:"timer_id"`
		TimerName    string    `json:"timer_name"`
		Color        string    `json:"color"`
		ClientID     string    `json:"client_id"`
		StartedAt    time.Time `json:"started_at"`
		FocusSeconds int       `json:"focus_seconds"`
		Completed    bool      `json:"completed"`
	} `json:"sessions"`
}

func TestExport(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deep, err := f.store.CreateTimer(ctx, f.alice.ID, validIntervals())
	if err != nil {
		t.Fatal(err)
	}
	gone, err := f.store.CreateTimer(ctx, f.alice.ID, focus.TimerInput{
		Name: "Old", Color: "pink", Kind: focus.KindSingle, FocusMinutes: 20, KeepHistory: true, Chime: "bell",
	})
	if err != nil {
		t.Fatal(err)
	}
	in := sessionInput("a", "Deep work", f.now.Add(-3*time.Hour), 50)
	in.TimerID, in.Color = deep.ID, "blue"
	if _, _, err := f.store.RecordSession(ctx, f.alice.ID, in); err != nil {
		t.Fatal(err)
	}
	old := sessionInput("b", "Old", f.now.Add(-2*time.Hour), 20)
	old.TimerID, old.Color = gone.ID, "pink"
	if _, _, err := f.store.RecordSession(ctx, f.alice.ID, old); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteTimer(ctx, f.alice.ID, gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.store.RecordSession(ctx, f.bob.ID, sessionInput("z", "Bob's", f.now.Add(-time.Hour), 30)); err != nil {
		t.Fatal(err)
	}

	payload, err := focus.New().Export(ctx, f.db, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "user_id") {
		t.Errorf("export carries user_id: %s", raw)
	}
	var out exported
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Timers) != 1 || out.Timers[0].ID != deep.ID || out.Timers[0].Name != "Deep work" ||
		out.Timers[0].Kind != "intervals" || out.Timers[0].Rounds != 4 || !out.Timers[0].KeepHistory {
		t.Errorf("timers = %+v", out.Timers)
	}
	if len(out.Sessions) != 2 {
		t.Fatalf("sessions = %+v, want Alice's two", out.Sessions)
	}
	first, second := out.Sessions[0], out.Sessions[1] // oldest first
	if first.TimerID == nil || *first.TimerID != deep.ID || first.TimerName != "Deep work" || first.FocusSeconds != 3000 {
		t.Errorf("first session = %+v", first)
	}
	if second.TimerID != nil || second.TimerName != "Old" || second.Color != "pink" || second.ClientID != "b" {
		t.Errorf("deleted timer's session = %+v", second)
	}
}

func TestExportForSomeoneWithNothing(t *testing.T) {
	f := newFixture(t)
	payload, err := focus.New().Export(context.Background(), f.db, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(payload)
	if string(raw) != `{"timers":[],"sessions":[]}` {
		t.Errorf("empty export = %s", raw)
	}
}

func TestAdminStats(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, uid := range []int64{f.alice.ID, f.bob.ID} {
		if _, err := f.store.CreateTimer(ctx, uid, validIntervals()); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := f.store.RecordSession(ctx, f.alice.ID, sessionInput("a", "x", f.now.Add(-3*time.Hour), 90)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.store.RecordSession(ctx, f.bob.ID, sessionInput("b", "y", f.now.Add(-2*time.Hour), 40)); err != nil {
		t.Fatal(err)
	}
	got, err := focus.New().Stats(ctx, f.db)
	if err != nil {
		t.Fatal(err)
	}
	want := []app.Stat{
		{Label: "Timers", Value: "2"},
		{Label: "Sessions recorded", Value: "2"},
		{Label: "Focus time", Value: "2h 10m", Hint: "recorded sessions, for everyone"},
	}
	if len(got) != len(want) {
		t.Fatalf("Stats = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("stat %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/focus/ -run 'Export|AdminStats' -count=1`
Expected: FAIL to compile — `Export`, `Stats` undefined on `*focus.App`.

- [ ] **Step 3: Implement**

Create `internal/apps/focus/export.go`:

```go
package focus

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// exportedTimer, exportedSession and exportPayload are the shapes
// `onsuite export` writes (spec: "Export and admin"). They are declared
// apart from the store's types so the backup format changes only when
// someone edits this file. Every field but user_id.
type exportedTimer struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	Color            string    `json:"color"`
	Kind             Kind      `json:"kind"`
	FocusMinutes     int       `json:"focus_minutes"`
	BreakMinutes     int       `json:"break_minutes,omitempty"`
	LongBreakMinutes int       `json:"long_break_minutes,omitempty"`
	Rounds           int       `json:"rounds,omitempty"`
	LongBreakEvery   int       `json:"long_break_every,omitempty"`
	AutoAdvance      bool      `json:"auto_advance"`
	Chime            string    `json:"chime"`
	KeepHistory      bool      `json:"keep_history"`
	Position         int       `json:"position"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type exportedSession struct {
	ID           int64     `json:"id"`
	TimerID      *int64    `json:"timer_id"` // null once the timer is deleted
	TimerName    string    `json:"timer_name"`
	Color        string    `json:"color"`
	ClientID     string    `json:"client_id"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at"`
	FocusSeconds int       `json:"focus_seconds"`
	RoundsDone   int       `json:"rounds_done"`
	Completed    bool      `json:"completed"`
}

type exportPayload struct {
	Timers   []exportedTimer   `json:"timers"`
	Sessions []exportedSession `json:"sessions"`
}

// Export implements app.Exporter, joining ON Focus to onsuite export's
// whole-account JSON backup.
func (a *App) Export(ctx context.Context, handle *sql.DB, userID int64) (any, error) {
	return NewStore(handle).Export(ctx, userID)
}

// Export gathers one user's timers in tile order and sessions oldest first.
func (st *Store) Export(ctx context.Context, userID int64) (exportPayload, error) {
	out := exportPayload{Timers: []exportedTimer{}, Sessions: []exportedSession{}}
	timers, err := st.Timers(ctx, userID)
	if err != nil {
		return exportPayload{}, err
	}
	for _, t := range timers {
		out.Timers = append(out.Timers, exportedTimer{
			ID: t.ID, Name: t.Name, Color: t.Color, Kind: t.Kind, FocusMinutes: t.FocusMinutes,
			BreakMinutes: t.BreakMinutes, LongBreakMinutes: t.LongBreakMinutes, Rounds: t.Rounds,
			LongBreakEvery: t.LongBreakEvery, AutoAdvance: t.AutoAdvance, Chime: t.Chime,
			KeepHistory: t.KeepHistory, Position: t.Position, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
		})
	}
	sessions, err := st.querySessions(ctx, `SELECT `+sessionColumns+` FROM focus_sessions
		 WHERE user_id = ? ORDER BY started_at, id`, userID)
	if err != nil {
		return exportPayload{}, err
	}
	for _, s := range sessions {
		e := exportedSession{
			ID: s.ID, TimerName: s.TimerName, Color: s.Color, ClientID: s.ClientID,
			StartedAt: s.StartedAt, EndedAt: s.EndedAt, FocusSeconds: s.FocusSeconds,
			RoundsDone: s.RoundsDone, Completed: s.Completed,
		}
		if s.TimerID != 0 {
			id := s.TimerID
			e.TimerID = &id
		}
		out.Sessions = append(out.Sessions, e)
	}
	return out, nil
}

// Stats implements app.Stater: ON Focus's card on the admin page.
func (a *App) Stats(ctx context.Context, handle *sql.DB) ([]app.Stat, error) {
	return NewStore(handle).Stats(ctx)
}

// Stats describes ON Focus across every user (spec: "Export and admin").
func (st *Store) Stats(ctx context.Context) ([]app.Stat, error) {
	var timers, sessions, seconds int64
	if err := st.db.QueryRowContext(ctx, `SELECT count(*) FROM focus_timers`).Scan(&timers); err != nil {
		return nil, fmt.Errorf("focus: stats timers: %w", err)
	}
	if err := st.db.QueryRowContext(ctx,
		`SELECT count(*), coalesce(sum(focus_seconds), 0) FROM focus_sessions`).Scan(&sessions, &seconds); err != nil {
		return nil, fmt.Errorf("focus: stats sessions: %w", err)
	}
	n := func(v int64) string { return strconv.FormatInt(v, 10) }
	return []app.Stat{
		{Label: "Timers", Value: n(timers)},
		{Label: "Sessions recorded", Value: n(sessions)},
		{Label: "Focus time", Value: FormatFocus(int(seconds)), Hint: "recorded sessions, for everyone"},
	}, nil
}
```

In `focus.go`, replace `var _ app.App = (*App)(nil)` with:

```go
var (
	_ app.App      = (*App)(nil)
	_ app.Exporter = (*App)(nil)
	_ app.Stater   = (*App)(nil)
)
```

(ON Later's `later.go` declares its assertions the same way.)

- [ ] **Step 4: Run the tests**

Run: `gofmt -w internal/apps/focus && go test ./internal/apps/focus/ ./cmd/onsuite/ -count=1`
Expected: PASS. `TestDisabledAppIsUnreachableAndOmittedFromExportAndStats` now also exercises Focus's exporter.

- [ ] **Step 5: Check the real export and admin page**

Run `go build -o /tmp/onsuite-f3 ./cmd/onsuite && /tmp/onsuite-f3 export <user> --data-dir <scratch dir> | head -c 2000` (the scratch data dir from Task 4 Step 8) and confirm a `"focus"` key with `timers` and `sessions`. Open `/admin` in the preview as an admin: an ON Focus card with Timers, Sessions recorded, Focus time.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/focus/export.go internal/apps/focus/export_test.go internal/apps/focus/focus.go
git commit -m "feat(focus): export timers and sessions; admin card (#495)"
```

---

### Task 9: User guide, spec and AGENTS.md

**Files:**
- Modify: `docs/user/focus.md`, `docs/superpowers/specs/2026-10-07-on-focus-design.md`, `AGENTS.md`

- [ ] **Step 1: User guide**

In `docs/user/focus.md`:

1. In "## Your timers", after the paragraph ending `how long the whole thing takes.`, add:

```markdown
Once you have finished a session or two, a strip above the tiles shows how
long you've focused today, how many sessions that was, and your total for
this week. **History** opens the full picture — see [History](#history).
```

2. In "### Leaving and coming back", replace `the ON Focus home page shows a **Resume** link.` with `the ON Focus home page shows a **Resume** link, and an **End** button to finish the session from there.`

3. Append at the end of the file:

```markdown
## History

When a session ends — it runs to the end, or you click **← Exit** or
**End** — ON Focus adds it to your history, as long as the timer has
**Keep history** ticked and you focused for at least a minute. Paused
time and breaks don't count.

Click **History** on the ON Focus home page to see:

- how long you've focused today, this week (from Monday), this month and
  this year;
- a bar chart of the last 30 days;
- this month's time for each timer, biggest first;
- your sessions, newest first, 50 to a page. A session you ended early is
  marked **stopped early**. Click **Delete** to remove one.

If ON Focus can't save a session — you're offline, say — the timer says
**Couldn't save this session** and offers **Retry**. The session waits in
your browser, and the home page offers **Retry** too, until it's saved.

Deleting a timer keeps its sessions in your history, under the name and
colour they had.
```

- [ ] **Step 2: Spec**

In `docs/superpowers/specs/2026-10-07-on-focus-design.md`:

1. In "### Recording a session", replace the sentence beginning `` `sendBeacon` is `` (through `` before `fetch` resolves). ``) with:

```markdown
The POST is a `fetch` with `keepalive: true`, so it still completes if the
page unloads first (Exit confirmed, then the tab closed); unlike
`sendBeacon` it can carry the CSRF header.
```

and replace the paragraph beginning `The stored state is cleared only once` with:

```markdown
The stored state is cleared only once the server confirms (2xx, including
"already recorded"), or refuses it for good (400/422 — retrying can't
help). Anything else — offline, signed out, a server error — keeps it: the
Done screen says "Couldn't save this session" with a Retry button, and the
home banner offers the same. A session being replaced by another timer's is
recorded first; if that fails it is dropped with a console warning.
```

2. In "### Resume", append to the banner bullet: `` While a session runs, the banner also has an **End** button: it asks, then ends and records the session like Exit (#546 — this also clears a session whose timer was deleted). ``

3. In "## Recording endpoint", after the body block, add `` `started_at` and `ended_at` are milliseconds since the epoch, as the browser keeps them. A new session answers 201, a repeat 200, both with `{"id": …}`. ``; and change the rule `` `started_at` in the future → 422. `` to `` `started_at` more than 5 minutes in the future → 422 (the browser's clock and the server's can disagree a little). ``

4. In the "## Pages and routes" table, add after the `GET /focus/history` row: `` | `GET /focus/today` | The today strip alone, for the home page to refresh after recording | ``.

5. In "### Home", change the toolbar sentence to `` Toolbar: "Timers", "History" and "+ New timer". `` and append to the today-strip sentence: ` It appears once the user has recorded any session, and then stays, showing 0m on quiet days.`

6. In the "## Phases" table, append to the F3 row's scope: `; banner End (#546)`.

- [ ] **Step 3: AGENTS.md**

Replace `(saved, reusable focus timers — single blocks or Pomodoro-style intervals; the running view and history are still being built out)` with `(saved, reusable focus timers — single blocks or Pomodoro-style intervals — with a calm running view and a history of focused time)`.

- [ ] **Step 4: Check the docs build**

Run: `go test ./docs/... ./internal/platform/help/ -count=1`
Expected: PASS (guides load, in-page links resolve).

- [ ] **Step 5: Commit**

```bash
git add docs/user/focus.md docs/superpowers/specs/2026-10-07-on-focus-design.md AGENTS.md
git commit -m "docs(focus): history in the user guide; record F3 decisions (#495)"
```

---

### Task 10: Verify in the browser and open the PR

- [ ] **Step 1: Walk through the whole feature once more**

Rebuild (`go build -o /tmp/onsuite-f3 ./cmd/onsuite`) and restart the preview. Repeat Task 6 Step 9 and Task 7 Step 6 quickly, plus:
1. A brand-new user: no today strip, History shows the empty states and `0m` totals.
2. History after a few sessions: totals agree with the sessions listed; deleting one updates totals, chart and list.
3. F2 behaviour unchanged: pause/resume, skip, restart, auto-advance off and Start, reload mid-phase, tab title.
4. Dark mode: History (tiles, chart, per-timer bars, table, stopped-early pill), today strip, banner.
5. Mobile width: History table and per-timer rows fit without sideways scroll; reset with `desktop`.

`read_console_messages` with `onlyErrors` on each page: none. Take screenshots of History, the home page with the today strip and a banner, and the Done screen saying "Saved to your history." for the PR.

- [ ] **Step 2: Final full check**

Run the full check from Global Constraints. Expected: all green.

- [ ] **Step 3: Push and open the PR as Ilia**

```bash
env -u GIT_SSH_COMMAND git push -u origin feat/focus-f3-history
env -u GH_TOKEN gh pr create --title "feat(focus): ON Focus F3 — history and stats" --body "$(cat <<'EOF'
Closes #495. Closes #546. Part of #492.

## What
- `POST /focus/sessions` records an ended session: validated, idempotent by the browser's session id, a deleted or foreign timer stored as none.
- The running page records on finish, Exit and when another timer replaces it; the state stays in `localStorage` until the server has it, with Retry on the Done screen.
- Home: **History** button, a today strip (once there is any history), and a banner that records an ended session on the spot, offers Retry, and has **End** for a running one (#546).
- History page: today / week / month / year totals, 30-day chart, this month by timer, recent sessions (50 a page) with delete.
- `Exporter` (timers and sessions) and an admin card (timers, sessions, focus time).

## Notes
- The POST uses `fetch` with `keepalive` instead of `sendBeacon`, which can't carry the CSRF header (decided while planning; spec updated).
- `started_at` may be up to 5 minutes in the future (client/server clock skew).
- Days follow the server's local zone and weeks start Monday; stats tests pin `time.Local` to Melbourne, as Flash's do.
- `session.js` `valid()` now type-checks the fields that go to the server — the first bullet of #545; the rest stays open.

## Testing
- `go test ./... -race` and the full check are green.
- Manual browser checks from the plan: finish/Exit/replace recording, offline + Retry on the Done screen and the banner, End on the banner (including a deleted timer), today strip refresh, History paging and delete, dark mode, mobile.
EOF
)"
```

Expected: the PR URL. Do not merge.
