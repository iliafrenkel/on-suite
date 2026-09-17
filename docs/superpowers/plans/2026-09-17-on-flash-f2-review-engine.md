# ON Flash F2 — Review Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the review loop to ON Flash: FSRS-based scheduling via `go-fsrs`, 4-point grading (Again/Hard/Good/Easy), per-deck daily limits, single-step undo, and two manual overrides (snooze a deck, adjust its pace).

**Architecture:** `go-fsrs` is imported from exactly one new file (`fsrs.go`), containment-tested the same way `internal/apps/reader` contains `go-readability`. A new per-account `flash_card_state` table (absence = never reviewed, mirroring `internal/apps/reader`'s `reader_item_state`) holds each card's live FSRS schedule plus a one-slot undo buffer (the `go-fsrs` `ReviewLog` needed to call the library's own `Rollback`). `flash_decks` gains pace/snooze columns; a new `flash_review_counts` table tracks today's consumption per account per deck. A queue-construction method assembles "what's due right now" across one or all decks, and a small set of HTTP handlers drive the actual review session.

**Tech Stack:** Go, `database/sql` + SQLite, `github.com/open-spaced-repetition/go-fsrs/v4`, `html/template` + htmx, one small hand-written JS file for keyboard shortcuts (no build step).

## Global Constraints

- Design doc: [docs/superpowers/specs/2026-09-17-on-flash-f2-review-engine-design.md](../specs/2026-09-17-on-flash-f2-review-engine-design.md) — every schema and behavior decision below traces back to it.
- `github.com/open-spaced-repetition/go-fsrs/v4` is imported from exactly one file, `internal/apps/flash/fsrs.go`; nothing else in the codebase may import it, enforced by `internal/arch/arch_test.go`'s `TestFSRSIsContained` (mirrors the existing `TestReadabilityIsContained`).
- Default FSRS weights only (`fsrs.DefaultParam()`) — no per-user parameter optimization in F2.
- Undo is single-step: only the most recently graded review of a given card can be undone, via the `go-fsrs` `Rollback` function, not a hand-rolled reversal.
- Every new/modified table stays `flash_`-prefixed, `STRICT`, and scoped by `user_id` where the row is per-account.
- Timestamps are `TEXT`, `RFC3339Nano` UTC via this package's existing `formatTime`/`parseTime`; the new `flash_review_counts.day` column is a plain `TEXT` calendar date (`2006-01-02`, UTC), a distinct, coarser convention used only for daily-limit bucketing.
- Default deck pace: `new_cards_per_day = 20`, `reviews_per_day = NULL` (unlimited).
- Card ordering within a review session: due reviews before new cards.
- `internal/apps/flash` continues to import only `internal/platform/*` plus the one now-permitted `go-fsrs` import in `fsrs.go`; apps never import each other.
- Full check (`gofmt -l .`, `go vet ./...`, `staticcheck`, `go mod tidy` diff, `go test ./... -race -count=1`) must stay green after every task.

---

### Task 1: go-fsrs wrapper and containment

**Files:**
- Create: `internal/apps/flash/fsrs.go`
- Create: `internal/apps/flash/fsrs_test.go`
- Modify: `internal/arch/arch_test.go` (append `TestFSRSIsContained`)
- Modify: `go.mod`, `go.sum` (via `go get`)

**Interfaces:**
- Consumes: nothing from earlier ON Flash tasks (this is F2's first task).
- Produces: `flash.RatingAgain`, `flash.RatingHard`, `flash.RatingGood`, `flash.RatingEasy` (all `= 1..4`, exported ints); unexported `cardSchedule` struct (`State string`, `DueAt time.Time`, `Stability float64`, `Difficulty float64`, `ScheduledDays uint64`, `Reps uint64`, `Lapses uint64`, `RemainingSteps int`, `LastReviewAt time.Time`); unexported `reviewLog` struct (`Rating int`, `Due time.Time`, `ScheduledDays uint64`, `Review time.Time`, `State string`, `Stability float64`, `Difficulty float64`, `RemainingSteps int`); unexported `newCardSchedule(now time.Time) cardSchedule`; unexported `gradeCard(current cardSchedule, rating int, now time.Time) (cardSchedule, reviewLog, error)`; unexported `rollbackCard(current cardSchedule, log reviewLog) (cardSchedule, error)`. Task 2's store methods call these four names directly.

- [ ] **Step 1: Add the dependency**

Run:
```bash
go get github.com/open-spaced-repetition/go-fsrs/v4@latest
```
Expected: `go.mod` gains a `require github.com/open-spaced-repetition/go-fsrs/v4 vX.Y.Z // indirect` line (the `// indirect` marker is expected at this point — nothing imports the package yet, since `fsrs.go` doesn't exist until Step 4). Do **not** run `go mod tidy` yet: with no importer, tidy would remove the requirement it just added rather than merely dropping the `// indirect` marker. Step 5 runs `go mod tidy` once `fsrs.go` gives it something to attach to.

- [ ] **Step 2: Write the failing wrapper test**

```go
// internal/apps/flash/fsrs_test.go
package flash

import (
	"reflect"
	"testing"
	"time"
)

func TestNewCardScheduleIsUnreviewed(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	c := newCardSchedule(now)
	if c.State != "new" {
		t.Errorf("State = %q, want %q", c.State, "new")
	}
	if !c.DueAt.Equal(now) {
		t.Errorf("DueAt = %v, want %v", c.DueAt, now)
	}
	if c.Reps != 0 {
		t.Errorf("Reps = %d, want 0", c.Reps)
	}
	if !c.LastReviewAt.IsZero() {
		t.Errorf("LastReviewAt = %v, want zero", c.LastReviewAt)
	}
}

func TestGradeCardRejectsInvalidRating(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	c := newCardSchedule(now)
	for _, rating := range []int{0, 5, -1} {
		if _, _, err := gradeCard(c, rating, now); err == nil {
			t.Errorf("gradeCard(rating=%d) = nil error, want an error", rating)
		}
	}
}

// TestGradeThenRollbackRestoresOriginalSchedule is the property that matters
// here: our wrapper is correctly wired to go-fsrs's own Next/Rollback pair,
// for every one of the four ratings. It deliberately does not assert on
// go-fsrs's internal stability/difficulty math (that is the library's own
// test suite's job) — only that grading a card and then rolling that single
// grade back reproduces the exact schedule that was there before.
func TestGradeThenRollbackRestoresOriginalSchedule(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	original := newCardSchedule(now)

	for _, rating := range []int{RatingAgain, RatingHard, RatingGood, RatingEasy} {
		graded, log, err := gradeCard(original, rating, now)
		if err != nil {
			t.Fatalf("gradeCard(rating=%d): %v", rating, err)
		}
		if graded.Reps != original.Reps+1 {
			t.Errorf("gradeCard(rating=%d): Reps = %d, want %d", rating, graded.Reps, original.Reps+1)
		}
		if graded.DueAt.Before(now) {
			t.Errorf("gradeCard(rating=%d): DueAt = %v, want at or after %v", rating, graded.DueAt, now)
		}

		restored, err := rollbackCard(graded, log)
		if err != nil {
			t.Fatalf("rollbackCard(rating=%d): %v", rating, err)
		}
		if !reflect.DeepEqual(restored, original) {
			t.Errorf("rollbackCard(rating=%d) = %+v, want original %+v", rating, restored, original)
		}
	}
}

// TestGradeTwiceThenRollbackRestoresTheIntermediateMemory proves rollback
// only undoes the most recent grade, not the whole history. It does not
// assert DueAt equality against the intermediate schedule: go-fsrs's own
// Rollback deliberately sets the reverted card's Due to the timestamp of
// the review being undone (log.Review), not the due date the card carried
// immediately before that review — a card being rolled back was, by
// definition, already due at that review's time, and go-fsrs has no way to
// recover what its due date was before it became overdue. Reps, State,
// Stability, Difficulty, and LastReviewAt are exactly recoverable and are
// what this test checks; only a rollback of a card's very first-ever
// review (from New, see TestGradeThenRollbackRestoresOriginalSchedule)
// reconstructs DueAt exactly too, because New's own rollback branch uses
// log.Due (the pre-review due date) rather than log.Review. (This
// distinction was found by actually running go-fsrs during plan
// validation, not derived from its docs — the library's rollback.go
// switches behavior by State for exactly this reason.)
func TestGradeTwiceThenRollbackRestoresTheIntermediateMemory(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	later := now.Add(24 * time.Hour)
	original := newCardSchedule(now)

	firstGrade, _, err := gradeCard(original, RatingGood, now)
	if err != nil {
		t.Fatalf("first gradeCard: %v", err)
	}
	secondGrade, secondLog, err := gradeCard(firstGrade, RatingGood, later)
	if err != nil {
		t.Fatalf("second gradeCard: %v", err)
	}

	restored, err := rollbackCard(secondGrade, secondLog)
	if err != nil {
		t.Fatalf("rollbackCard: %v", err)
	}
	if restored.Reps != firstGrade.Reps {
		t.Errorf("Reps = %d, want %d", restored.Reps, firstGrade.Reps)
	}
	if restored.State != firstGrade.State {
		t.Errorf("State = %q, want %q", restored.State, firstGrade.State)
	}
	if restored.Stability != firstGrade.Stability {
		t.Errorf("Stability = %v, want %v", restored.Stability, firstGrade.Stability)
	}
	if restored.Difficulty != firstGrade.Difficulty {
		t.Errorf("Difficulty = %v, want %v", restored.Difficulty, firstGrade.Difficulty)
	}
	if !restored.LastReviewAt.Equal(firstGrade.LastReviewAt) {
		t.Errorf("LastReviewAt = %v, want %v", restored.LastReviewAt, firstGrade.LastReviewAt)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/apps/flash/... -run TestNewCardScheduleIsUnreviewed -v`
Expected: FAIL — compile error, `newCardSchedule` and friends do not exist yet.

- [ ] **Step 4: Write `fsrs.go`**

```go
// internal/apps/flash/fsrs.go
//
// This is the only file in the codebase that imports go-fsrs. Every other
// file in this package works with cardSchedule and reviewLog, this file's
// own plain-Go types — internal/arch/arch_test.go's TestFSRSIsContained
// enforces that a second importer never sneaks in, the same way
// TestReadabilityIsContained does for go-readability in internal/apps/reader.
package flash

import (
	"fmt"
	"time"

	"github.com/open-spaced-repetition/go-fsrs/v4"
)

// scheduler runs FSRS with the library's default weights. There is no
// per-user parameter optimization in F2 — see the design doc's "Out of
// scope" section.
var scheduler = fsrs.NewFSRS(fsrs.DefaultParam())

// The four ratings a review session can apply, matching go-fsrs's own
// Rating enum values exactly (Again=1 .. Easy=4), so no translation table
// is needed anywhere that passes a plain int across this file's boundary.
const (
	RatingAgain = 1
	RatingHard  = 2
	RatingGood  = 3
	RatingEasy  = 4
)

// cardSchedule is this package's own representation of a card's FSRS
// scheduling state, decoupled from go-fsrs's Card type.
type cardSchedule struct {
	State          string // "new" | "learning" | "review" | "relearning"
	DueAt          time.Time
	Stability      float64
	Difficulty     float64
	ScheduledDays  uint64
	Reps           uint64
	Lapses         uint64
	RemainingSteps int
	LastReviewAt   time.Time // zero value: never reviewed
}

// reviewLog is this package's own representation of go-fsrs's ReviewLog: the
// undo buffer produced by one grade, consumed by rollbackCard to reconstruct
// the schedule immediately before that grade.
type reviewLog struct {
	Rating         int
	Due            time.Time
	ScheduledDays  uint64
	Review         time.Time
	State          string
	Stability      float64
	Difficulty     float64
	RemainingSteps int
}

// newCardSchedule is the state of a card that has never been reviewed.
func newCardSchedule(now time.Time) cardSchedule {
	return fromFSRSCard(fsrs.NewCard(now))
}

// gradeCard applies rating (RatingAgain..RatingEasy) to current's schedule at
// now, returning the new schedule and the log needed to undo this one grade
// via rollbackCard.
func gradeCard(current cardSchedule, rating int, now time.Time) (cardSchedule, reviewLog, error) {
	info, err := scheduler.Next(toFSRSCard(current), now, fsrs.Rating(rating))
	if err != nil {
		return cardSchedule{}, reviewLog{}, fmt.Errorf("flash: grade card: %w", err)
	}
	return fromFSRSCard(info.Card), fromFSRSLog(info.ReviewLog), nil
}

// rollbackCard reverses the single grade described by log, reconstructing
// current's schedule as it was immediately before that grade was applied.
func rollbackCard(current cardSchedule, log reviewLog) (cardSchedule, error) {
	reverted, err := scheduler.Rollback(toFSRSCard(current), toFSRSLog(log))
	if err != nil {
		return cardSchedule{}, fmt.Errorf("flash: rollback card: %w", err)
	}
	return fromFSRSCard(reverted), nil
}

func toFSRSState(s string) fsrs.State {
	switch s {
	case "learning":
		return fsrs.Learning
	case "review":
		return fsrs.Review
	case "relearning":
		return fsrs.Relearning
	default:
		return fsrs.New
	}
}

func fromFSRSState(s fsrs.State) string {
	switch s {
	case fsrs.Learning:
		return "learning"
	case fsrs.Review:
		return "review"
	case fsrs.Relearning:
		return "relearning"
	default:
		return "new"
	}
}

func toFSRSCard(c cardSchedule) fsrs.Card {
	return fsrs.Card{
		Due:            c.DueAt,
		Stability:      c.Stability,
		Difficulty:     c.Difficulty,
		ScheduledDays:  c.ScheduledDays,
		Reps:           c.Reps,
		Lapses:         c.Lapses,
		State:          toFSRSState(c.State),
		LastReview:     c.LastReviewAt,
		RemainingSteps: c.RemainingSteps,
	}
}

func fromFSRSCard(c fsrs.Card) cardSchedule {
	return cardSchedule{
		State:          fromFSRSState(c.State),
		DueAt:          c.Due,
		Stability:      c.Stability,
		Difficulty:     c.Difficulty,
		ScheduledDays:  c.ScheduledDays,
		Reps:           c.Reps,
		Lapses:         c.Lapses,
		RemainingSteps: c.RemainingSteps,
		LastReviewAt:   c.LastReview,
	}
}

func toFSRSLog(l reviewLog) fsrs.ReviewLog {
	return fsrs.ReviewLog{
		Rating:         fsrs.Rating(l.Rating),
		Due:            l.Due,
		ScheduledDays:  l.ScheduledDays,
		Review:         l.Review,
		State:          toFSRSState(l.State),
		Stability:      l.Stability,
		Difficulty:     l.Difficulty,
		RemainingSteps: l.RemainingSteps,
	}
}

func fromFSRSLog(l fsrs.ReviewLog) reviewLog {
	return reviewLog{
		Rating:         int(l.Rating),
		Due:            l.Due,
		ScheduledDays:  l.ScheduledDays,
		Review:         l.Review,
		State:          fromFSRSState(l.State),
		Stability:      l.Stability,
		Difficulty:     l.Difficulty,
		RemainingSteps: l.RemainingSteps,
	}
}
```

- [ ] **Step 5: Run the test to verify it passes, and settle go.mod**

Run:
```bash
go test ./internal/apps/flash/... -v
go mod tidy
git diff --exit-code go.mod go.sum
```
Expected: the four new tests PASS; `go mod tidy` now drops the `// indirect` marker on the `go-fsrs/v4` requirement line, since `fsrs.go` imports it directly — the final `git diff --exit-code` should then report no further changes (if it does show a diff, `go mod tidy` just made one; that is expected exactly once here, not a failure).

- [ ] **Step 6: Add the containment test**

Append to `internal/arch/arch_test.go` (it already has the imports this needs — `filepath`, `os`, `parser`, `token`, `strconv`, `strings`, `slices` — from `TestReadabilityIsContained`; no new imports needed):

```go
// TestFSRSIsContained: go-fsrs is ON Flash's scheduling engine, and F2's
// design accepted it only on the condition that it stays behind one file —
// see fsrs.go's own doc comment. A second importer makes it load-bearing
// elsewhere too, which is a different decision and should be made
// deliberately, the same rule TestReadabilityIsContained enforces for
// go-readability.
func TestFSRSIsContained(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	const lib = "github.com/open-spaced-repetition/go-fsrs/v4"

	var importers []string
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
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return nil
		}
		for _, spec := range f.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if imported == lib {
				rel, _ := filepath.Rel(root, path)
				importers = append(importers, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"internal/apps/flash/fsrs.go"}
	if !slices.Equal(importers, want) {
		t.Errorf("go-fsrs is imported by %v, want only %v", importers, want)
	}
}
```

- [ ] **Step 7: Run the arch test**

Run: `go test ./internal/arch/... -run TestFSRSIsContained -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git checkout -b feat/flash-f2-review-engine
git add go.mod go.sum internal/apps/flash/fsrs.go internal/apps/flash/fsrs_test.go internal/arch/arch_test.go
git commit -m "feat(flash): add go-fsrs wrapper, contained to one file"
```

---

### Task 2: Card scheduling state, daily counters, and grade/undo

**Files:**
- Create: `internal/apps/flash/migrations/0004_review_state.sql`
- Create: `internal/apps/flash/review.go`
- Create: `internal/apps/flash/review_test.go`

**Interfaces:**
- Consumes: `newCardSchedule`, `gradeCard`, `rollbackCard`, `cardSchedule`, `reviewLog`, `RatingAgain..RatingEasy` (Task 1); `Store.cardOwnerCheck(ctx, userID, cardID) (int64, error)` (existing, from `tag.go`); `Store.rowScanner`, `formatTime`, `parseTime`, `ErrNotFound`, `ErrInvalid` (existing, `store.go`).
- Produces: `formatDay(t time.Time) string`; `Store.CardState(ctx, userID, cardID int64) (cardSchedule, bool, error)`; `Store.GradeCard(ctx, userID, cardID int64, rating int, now time.Time) (cardSchedule, error)`; `Store.UndoLastGrade(ctx, userID, cardID int64, now time.Time) (cardSchedule, bool, error)`. Task 4 adds `Store.DueQueue` to this same file, reusing `formatDay` and the scan helpers defined here.

- [ ] **Step 1: Write the failing store test**

```go
// internal/apps/flash/review_test.go
package flash_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestCardStateIsUnreviewedForANeverGradedCard(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}

	_, reviewed, err := f.store.CardState(ctx, f.alice.ID, card.ID)
	if err != nil {
		t.Fatalf("CardState: %v", err)
	}
	if reviewed {
		t.Error("CardState reports a never-graded card as reviewed")
	}
}

func TestCardStateRejectsSomeoneElsesCard(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.store.CardState(ctx, f.bob.ID, card.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("CardState as another user = %v, want ErrNotFound", err)
	}
}

func TestGradeCardCreatesStateOnFirstReview(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, now); err != nil {
		t.Fatalf("GradeCard: %v", err)
	}

	_, reviewed, err := f.store.CardState(ctx, f.alice.ID, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reviewed {
		t.Error("CardState still reports unreviewed after grading")
	}
}

func TestGradeCardRejectsSomeoneElsesCard(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	if _, err := f.store.GradeCard(ctx, f.bob.ID, card.ID, flash.RatingGood, now); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("GradeCard as another user = %v, want ErrNotFound", err)
	}
}

func TestUndoLastGradeRestoresPreviousSchedule(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}

	_, undone, err := f.store.UndoLastGrade(ctx, f.alice.ID, card.ID, now)
	if err != nil {
		t.Fatalf("UndoLastGrade: %v", err)
	}
	if !undone {
		t.Fatal("UndoLastGrade reported nothing to undo right after a grade")
	}

	_, reviewed, err := f.store.CardState(ctx, f.alice.ID, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reviewed {
		t.Error("undoing a card's only grade should leave it unreviewed again")
	}
}

func TestUndoLastGradeIsANoOpWhenNothingToUndo(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}
	if _, undone, err := f.store.UndoLastGrade(ctx, f.alice.ID, card.ID, now); err != nil || !undone {
		t.Fatalf("first UndoLastGrade = undone=%v, err=%v; want undone=true", undone, err)
	}
	// Nothing left to undo: the log was cleared by the undo above.
	if _, undone, err := f.store.UndoLastGrade(ctx, f.alice.ID, card.ID, now); err != nil || undone {
		t.Errorf("second UndoLastGrade = undone=%v, err=%v; want undone=false, err=nil", undone, err)
	}
}

func TestUndoLastGradeOnlyUndoesTheMostRecentGrade(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	later := now.Add(24 * time.Hour)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, later); err != nil {
		t.Fatal(err)
	}

	_, undone, err := f.store.UndoLastGrade(ctx, f.alice.ID, card.ID, later)
	if err != nil || !undone {
		t.Fatalf("UndoLastGrade = undone=%v, err=%v", undone, err)
	}
	_, reviewed, err := f.store.CardState(ctx, f.alice.ID, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reviewed {
		t.Error("undoing the second grade should still leave the card reviewed once (from the first grade)")
	}
}

func TestGradeCardTracksDailyCounts(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	cardA, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	cardB, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "c", "d", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	// Grading a never-before-seen card counts as "new"; grading it again
	// later the same day counts as "review".
	if _, err := f.store.GradeCard(ctx, f.alice.ID, cardA.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, cardB.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, cardA.ID, flash.RatingGood, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	newCount, reviewCount, err := f.store.DailyCountsForTest(ctx, f.alice.ID, deck.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if newCount != 2 {
		t.Errorf("new_count = %d, want 2", newCount)
	}
	if reviewCount != 1 {
		t.Errorf("review_count = %d, want 1", reviewCount)
	}
}

func TestUndoLastGradeDecrementsTheRightDailyCounter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingGood, now); err != nil {
		t.Fatal(err)
	}
	if _, undone, err := f.store.UndoLastGrade(ctx, f.alice.ID, card.ID, now); err != nil || !undone {
		t.Fatalf("UndoLastGrade: undone=%v, err=%v", undone, err)
	}

	newCount, reviewCount, err := f.store.DailyCountsForTest(ctx, f.alice.ID, deck.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if newCount != 0 || reviewCount != 0 {
		t.Errorf("counts after undoing the only grade = new=%d review=%d, want 0/0", newCount, reviewCount)
	}
}
```

`Store.DailyCountsForTest`, used above, does not exist yet — it is written in Step 5, alongside the rest of this task's production code. It exists purely so this external `flash_test` package can assert on the private `flash_review_counts` table without F2 needing a public API surface for it otherwise.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/apps/flash/... -run TestCardStateIsUnreviewedForANeverGradedCard -v`
Expected: FAIL — compile error, `CardState`, `GradeCard`, `UndoLastGrade`, `DailyCountsForTest` do not exist yet.

- [ ] **Step 3: Write the migration**

```sql
-- internal/apps/flash/migrations/0004_review_state.sql

-- One row per (user, card) that has ever been graded at least once. Absence
-- of a row means "never reviewed" — a card starts in FSRS's "new" state
-- implicitly, the same convention internal/apps/reader's reader_item_state
-- uses for read/starred state, so a deck full of cards costs nothing per
-- account until one is actually studied.
--
-- The log_* columns are a one-slot undo buffer: go-fsrs's own ReviewLog for
-- the most recent grade, consumed by go-fsrs's Rollback to reconstruct the
-- schedule as it was immediately before that grade. NULL log_rating means
-- there is nothing left to undo (never reviewed, or the last grade was
-- already undone). log_was_new records whether that grade was originally
-- counted as "new" or "review" against the daily limit, so UndoLastGrade
-- decrements the correct one of flash_review_counts' two counters without
-- having to re-derive it.
CREATE TABLE flash_card_state (
    user_id             INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    card_id             INTEGER NOT NULL REFERENCES flash_cards (id) ON DELETE CASCADE,
    state               TEXT    NOT NULL,
    due_at              TEXT    NOT NULL,
    stability           REAL    NOT NULL,
    difficulty          REAL    NOT NULL,
    scheduled_days      INTEGER NOT NULL,
    reps                INTEGER NOT NULL,
    lapses              INTEGER NOT NULL,
    remaining_steps     INTEGER NOT NULL,
    last_review_at      TEXT,
    log_rating          INTEGER,
    log_due             TEXT,
    log_scheduled_days  INTEGER,
    log_review          TEXT,
    log_state           TEXT,
    log_stability       REAL,
    log_difficulty      REAL,
    log_remaining_steps INTEGER,
    log_was_new         INTEGER,
    PRIMARY KEY (user_id, card_id)
) STRICT, WITHOUT ROWID;

-- due_at is queried per user across every one of their decks to build the
-- review queue (Task 4), so it is the hot lookup column.
CREATE INDEX flash_card_state_user_due_idx ON flash_card_state (user_id, due_at);

-- One row per (user, deck, calendar day), created the first time either
-- counter needs to move. day is a plain "YYYY-MM-DD" UTC date, not the
-- RFC3339Nano timestamp convention this package uses elsewhere — a coarser
-- granularity is the whole point of a daily counter.
CREATE TABLE flash_review_counts (
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    deck_id      INTEGER NOT NULL REFERENCES flash_decks (id) ON DELETE CASCADE,
    day          TEXT    NOT NULL,
    new_count    INTEGER NOT NULL DEFAULT 0,
    review_count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, deck_id, day)
) STRICT, WITHOUT ROWID;
```

- [ ] **Step 4: Write `review.go`**

```go
// internal/apps/flash/review.go
package flash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// formatDay is flash_review_counts' calendar-day convention: a plain UTC
// date, coarser than this package's usual RFC3339Nano timestamps, because a
// daily counter only ever needs to compare "same day or not."
func formatDay(t time.Time) string { return t.UTC().Format("2006-01-02") }

// CardState returns cardID's current schedule for userID, and whether it has
// ever been reviewed. A never-reviewed card returns a fresh cardSchedule
// (as newCardSchedule would build) and false, not an error.
func (st *Store) CardState(ctx context.Context, userID, cardID int64) (cardSchedule, bool, error) {
	if _, err := st.cardOwnerCheck(ctx, userID, cardID); err != nil {
		return cardSchedule{}, false, err
	}
	return st.scanCardState(ctx, userID, cardID)
}

func (st *Store) scanCardState(ctx context.Context, userID, cardID int64) (cardSchedule, bool, error) {
	var (
		c                   cardSchedule
		dueAt, lastReviewAt sql.NullString
	)
	err := st.db.QueryRowContext(ctx, `
		SELECT state, due_at, stability, difficulty, scheduled_days, reps, lapses, remaining_steps, last_review_at
		FROM flash_card_state WHERE user_id = ? AND card_id = ?`, userID, cardID,
	).Scan(&c.State, &dueAt, &c.Stability, &c.Difficulty, &c.ScheduledDays, &c.Reps, &c.Lapses, &c.RemainingSteps, &lastReviewAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return cardSchedule{}, false, nil
	case err != nil:
		return cardSchedule{}, false, fmt.Errorf("flash: scan card state: %w", err)
	}
	if c.DueAt, err = parseTime(dueAt.String); err != nil {
		return cardSchedule{}, false, err
	}
	if lastReviewAt.Valid {
		if c.LastReviewAt, err = parseTime(lastReviewAt.String); err != nil {
			return cardSchedule{}, false, err
		}
	}
	return c, true, nil
}

// formatNullableTime is like formatTime, but a zero time.Time (a card that
// has never actually been reviewed, which rollbackCard can produce by
// undoing a card's only grade) stores as SQL NULL instead of a formatted
// zero-value timestamp.
func formatNullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return formatTime(t)
}

// GradeCard applies rating to cardID's current schedule (starting a fresh
// one if this is its first-ever review), persists the result, saves the
// pre-grade log so the grade can be undone, and records it against today's
// daily limit for cardID's deck. All three writes happen in one transaction:
// a grade that updated the schedule but not the daily count (or vice versa)
// would leave the queue-construction logic in Task 4 lying about what is
// still due today.
func (st *Store) GradeCard(ctx context.Context, userID, cardID int64, rating int, now time.Time) (cardSchedule, error) {
	deckID, err := st.cardOwnerCheck(ctx, userID, cardID)
	if err != nil {
		return cardSchedule{}, err
	}

	current, hadState, err := st.scanCardState(ctx, userID, cardID)
	if err != nil {
		return cardSchedule{}, err
	}
	if !hadState {
		current = newCardSchedule(now)
	}

	graded, log, err := gradeCard(current, rating, now)
	if err != nil {
		return cardSchedule{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}

	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return cardSchedule{}, fmt.Errorf("flash: grade card: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	wasNew := 0
	if !hadState {
		wasNew = 1
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO flash_card_state (
			user_id, card_id, state, due_at, stability, difficulty, scheduled_days, reps, lapses, remaining_steps, last_review_at,
			log_rating, log_due, log_scheduled_days, log_review, log_state, log_stability, log_difficulty, log_remaining_steps, log_was_new
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, card_id) DO UPDATE SET
			state = excluded.state, due_at = excluded.due_at, stability = excluded.stability,
			difficulty = excluded.difficulty, scheduled_days = excluded.scheduled_days,
			reps = excluded.reps, lapses = excluded.lapses, remaining_steps = excluded.remaining_steps,
			last_review_at = excluded.last_review_at,
			log_rating = excluded.log_rating, log_due = excluded.log_due,
			log_scheduled_days = excluded.log_scheduled_days, log_review = excluded.log_review,
			log_state = excluded.log_state, log_stability = excluded.log_stability,
			log_difficulty = excluded.log_difficulty, log_remaining_steps = excluded.log_remaining_steps,
			log_was_new = excluded.log_was_new`,
		userID, cardID, graded.State, formatTime(graded.DueAt), graded.Stability, graded.Difficulty,
		graded.ScheduledDays, graded.Reps, graded.Lapses, graded.RemainingSteps, formatNullableTime(graded.LastReviewAt),
		log.Rating, formatTime(log.Due), log.ScheduledDays, formatTime(log.Review), log.State, log.Stability, log.Difficulty, log.RemainingSteps, wasNew,
	); err != nil {
		return cardSchedule{}, fmt.Errorf("flash: grade card: %w", err)
	}

	newInc, reviewInc := 0, 1
	if !hadState {
		newInc, reviewInc = 1, 0
	}
	if err := st.bumpDailyCounts(ctx, tx, userID, deckID, now, newInc, reviewInc); err != nil {
		return cardSchedule{}, err
	}

	if err := tx.Commit(); err != nil {
		return cardSchedule{}, fmt.Errorf("flash: grade card: %w", err)
	}
	return graded, nil
}

// UndoLastGrade reverses cardID's most recent grade, if any. hasUndo is
// false, with no error, when there is nothing to undo — a card that has
// never been graded, or whose one undo slot was already spent.
func (st *Store) UndoLastGrade(ctx context.Context, userID, cardID int64, now time.Time) (schedule cardSchedule, hasUndo bool, err error) {
	deckID, err := st.cardOwnerCheck(ctx, userID, cardID)
	if err != nil {
		return cardSchedule{}, false, err
	}

	current, log, wasNew, hasLog, err := st.scanCardStateWithLog(ctx, userID, cardID)
	if err != nil {
		return cardSchedule{}, false, err
	}
	if !hasLog {
		return cardSchedule{}, false, nil
	}

	reverted, err := rollbackCard(current, log)
	if err != nil {
		return cardSchedule{}, false, fmt.Errorf("flash: undo last grade: %w", err)
	}

	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return cardSchedule{}, false, fmt.Errorf("flash: undo last grade: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if wasNew {
		// Undoing a card's first-ever review must restore "never reviewed"
		// as an absent row, not a row full of New-state zero values — the
		// rest of this package (CardState, DueQueue's new-card query)
		// determines "never reviewed" by row absence alone. Confirmed by
		// plan validation: an UPDATE-only implementation here leaves
		// CardState reporting "reviewed" forever after undoing a card's
		// only grade, since the row still exists.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM flash_card_state WHERE user_id = ? AND card_id = ?`,
			userID, cardID,
		); err != nil {
			return cardSchedule{}, false, fmt.Errorf("flash: undo last grade: %w", err)
		}
	} else if _, err := tx.ExecContext(ctx, `
		UPDATE flash_card_state SET
			state = ?, due_at = ?, stability = ?, difficulty = ?, scheduled_days = ?, reps = ?, lapses = ?, remaining_steps = ?, last_review_at = ?,
			log_rating = NULL, log_due = NULL, log_scheduled_days = NULL, log_review = NULL,
			log_state = NULL, log_stability = NULL, log_difficulty = NULL, log_remaining_steps = NULL, log_was_new = NULL
		WHERE user_id = ? AND card_id = ?`,
		reverted.State, formatTime(reverted.DueAt), reverted.Stability, reverted.Difficulty,
		reverted.ScheduledDays, reverted.Reps, reverted.Lapses, reverted.RemainingSteps, formatNullableTime(reverted.LastReviewAt),
		userID, cardID,
	); err != nil {
		return cardSchedule{}, false, fmt.Errorf("flash: undo last grade: %w", err)
	}

	newDec, reviewDec := 0, -1
	if wasNew {
		newDec, reviewDec = -1, 0
	}
	if err := st.bumpDailyCounts(ctx, tx, userID, deckID, now, newDec, reviewDec); err != nil {
		return cardSchedule{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return cardSchedule{}, false, fmt.Errorf("flash: undo last grade: %w", err)
	}
	return reverted, true, nil
}

func (st *Store) scanCardStateWithLog(ctx context.Context, userID, cardID int64) (current cardSchedule, log reviewLog, wasNew bool, hasLog bool, err error) {
	var (
		dueAt, lastReviewAt                                          sql.NullString
		logDue, logReview, logState                                  sql.NullString
		logRating, logScheduledDays, logRemainingSteps, logWasNewCol sql.NullInt64
		logStability, logDifficulty                                  sql.NullFloat64
	)
	err = st.db.QueryRowContext(ctx, `
		SELECT state, due_at, stability, difficulty, scheduled_days, reps, lapses, remaining_steps, last_review_at,
		       log_rating, log_due, log_scheduled_days, log_review, log_state, log_stability, log_difficulty, log_remaining_steps, log_was_new
		FROM flash_card_state WHERE user_id = ? AND card_id = ?`, userID, cardID,
	).Scan(
		&current.State, &dueAt, &current.Stability, &current.Difficulty, &current.ScheduledDays, &current.Reps, &current.Lapses, &current.RemainingSteps, &lastReviewAt,
		&logRating, &logDue, &logScheduledDays, &logReview, &logState, &logStability, &logDifficulty, &logRemainingSteps, &logWasNewCol,
	)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return cardSchedule{}, reviewLog{}, false, false, nil
	case err != nil:
		return cardSchedule{}, reviewLog{}, false, false, fmt.Errorf("flash: scan card state: %w", err)
	}
	if current.DueAt, err = parseTime(dueAt.String); err != nil {
		return cardSchedule{}, reviewLog{}, false, false, err
	}
	if lastReviewAt.Valid {
		if current.LastReviewAt, err = parseTime(lastReviewAt.String); err != nil {
			return cardSchedule{}, reviewLog{}, false, false, err
		}
	}
	if !logRating.Valid {
		return current, reviewLog{}, false, false, nil
	}
	log.Rating = int(logRating.Int64)
	if log.Due, err = parseTime(logDue.String); err != nil {
		return cardSchedule{}, reviewLog{}, false, false, err
	}
	log.ScheduledDays = uint64(logScheduledDays.Int64)
	if log.Review, err = parseTime(logReview.String); err != nil {
		return cardSchedule{}, reviewLog{}, false, false, err
	}
	log.State = logState.String
	log.Stability = logStability.Float64
	log.Difficulty = logDifficulty.Float64
	log.RemainingSteps = int(logRemainingSteps.Int64)
	return current, log, logWasNewCol.Int64 == 1, true, nil
}

// bumpDailyCounts adds newDelta/reviewDelta (either can be negative, for
// UndoLastGrade) to userID's counters for deckID on now's calendar day,
// creating the row if this is the first count of the day.
func (st *Store) bumpDailyCounts(ctx context.Context, tx *sql.Tx, userID, deckID int64, now time.Time, newDelta, reviewDelta int) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO flash_review_counts (user_id, deck_id, day, new_count, review_count)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, deck_id, day) DO UPDATE SET
			new_count = new_count + excluded.new_count,
			review_count = review_count + excluded.review_count`,
		userID, deckID, formatDay(now), newDelta, reviewDelta)
	if err != nil {
		return fmt.Errorf("flash: update daily counts: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Add the test-only daily-counts accessor**

Append to `review.go` (kept in production code rather than a `_test.go` file because `Store` is this package's own type — an external `flash_test` package has no other way to read `flash_review_counts`, and every ON Flash store test already lives in `flash_test`):

```go
// DailyCountsForTest exposes flash_review_counts to tests outside this
// package, for asserting GradeCard/UndoLastGrade updated the right counter.
func (st *Store) DailyCountsForTest(ctx context.Context, userID, deckID int64, now time.Time) (newCount, reviewCount int, err error) {
	err = st.db.QueryRowContext(ctx, `
		SELECT new_count, review_count FROM flash_review_counts
		WHERE user_id = ? AND deck_id = ? AND day = ?`, userID, deckID, formatDay(now),
	).Scan(&newCount, &reviewCount)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("flash: daily counts for test: %w", err)
	}
	return newCount, reviewCount, nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -v`
Expected: PASS — every test in Task 2, plus all of Tasks 1-1 through F1 (no regressions).

- [ ] **Step 7: Commit**

```bash
git add internal/apps/flash/migrations/0004_review_state.sql internal/apps/flash/review.go internal/apps/flash/review_test.go
git commit -m "feat(flash): add card scheduling state, daily counts, grade and undo"
```

---

### Task 3: Deck pace and snooze

**Files:**
- Create: `internal/apps/flash/migrations/0005_deck_pace_snooze.sql`
- Modify: `internal/apps/flash/deck.go`
- Modify: `internal/apps/flash/handlers_decks.go`
- Modify: `internal/apps/flash/templates/decks.html`
- Modify: `internal/apps/flash/flash.go` (two new routes)
- Modify: `internal/apps/flash/deck_test.go`
- Modify: `internal/apps/flash/handlers_decks_test.go`

**Interfaces:**
- Consumes: `Deck`, `Store.DeckByID`, `ErrNotFound`, `ErrInvalid`, `formatTime`, `parseTime` (existing, F1); `a.userID`, `a.deckIDFromPath`, `a.fail`, `a.render`, `userMessage` (existing, `handlers_decks.go`).
- Produces: `Deck.NewCardsPerDay int`, `Deck.ReviewsPerDay *int`, `Deck.SnoozedUntil *time.Time`, `Deck.IsSnoozed(now time.Time) bool`; `ValidateDeckSettings(newCardsPerDay int, reviewsPerDay *int) error`; `Store.UpdateDeckSettings(ctx, userID, id int64, newCardsPerDay int, reviewsPerDay *int) (Deck, error)`; `Store.SnoozeDeck(ctx, userID, id int64, until time.Time) (Deck, error)`; `Store.UnsnoozeDeck(ctx, userID, id int64) (Deck, error)`. Task 4's queue construction reads `Deck.NewCardsPerDay`, `Deck.ReviewsPerDay`, and calls `Deck.IsSnoozed`.

- [ ] **Step 1: Write the failing store test**

Append to `internal/apps/flash/deck_test.go`:

```go
func TestUpdateDeckSettings(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if created.NewCardsPerDay != 20 {
		t.Errorf("default NewCardsPerDay = %d, want 20", created.NewCardsPerDay)
	}
	if created.ReviewsPerDay != nil {
		t.Errorf("default ReviewsPerDay = %v, want nil (unlimited)", created.ReviewsPerDay)
	}

	reviewCap := 50
	updated, err := f.store.UpdateDeckSettings(ctx, f.alice.ID, created.ID, 10, &reviewCap)
	if err != nil {
		t.Fatalf("UpdateDeckSettings: %v", err)
	}
	if updated.NewCardsPerDay != 10 {
		t.Errorf("NewCardsPerDay = %d, want 10", updated.NewCardsPerDay)
	}
	if updated.ReviewsPerDay == nil || *updated.ReviewsPerDay != 50 {
		t.Errorf("ReviewsPerDay = %v, want 50", updated.ReviewsPerDay)
	}

	// Setting it back to nil restores "unlimited."
	unlimited, err := f.store.UpdateDeckSettings(ctx, f.alice.ID, created.ID, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if unlimited.ReviewsPerDay != nil {
		t.Errorf("ReviewsPerDay after clearing = %v, want nil", unlimited.ReviewsPerDay)
	}
}

func TestUpdateDeckSettingsRejectsNegativeValues(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateDeckSettings(ctx, f.alice.ID, created.ID, -1, nil); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("UpdateDeckSettings(new=-1) = %v, want ErrInvalid", err)
	}
	neg := -5
	if _, err := f.store.UpdateDeckSettings(ctx, f.alice.ID, created.ID, 10, &neg); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("UpdateDeckSettings(reviews=-5) = %v, want ErrInvalid", err)
	}
}

func TestUpdateDeckSettingsRejectsSomeoneElsesDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateDeckSettings(ctx, f.bob.ID, created.ID, 10, nil); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestSnoozeAndUnsnoozeDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	until := now.AddDate(0, 0, 7)

	snoozed, err := f.store.SnoozeDeck(ctx, f.alice.ID, created.ID, until)
	if err != nil {
		t.Fatalf("SnoozeDeck: %v", err)
	}
	if !snoozed.IsSnoozed(now) {
		t.Error("IsSnoozed(now) = false right after snoozing until a week from now")
	}
	if snoozed.IsSnoozed(until.AddDate(0, 0, 1)) {
		t.Error("IsSnoozed should be false once the snooze period has passed")
	}

	unsnoozed, err := f.store.UnsnoozeDeck(ctx, f.alice.ID, created.ID)
	if err != nil {
		t.Fatalf("UnsnoozeDeck: %v", err)
	}
	if unsnoozed.IsSnoozed(now) {
		t.Error("IsSnoozed(now) = true after unsnoozing")
	}
}

func TestSnoozeDeckRejectsSomeoneElsesDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SnoozeDeck(ctx, f.bob.ID, created.ID, time.Now()); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/apps/flash/... -run TestUpdateDeckSettings -v`
Expected: FAIL — `Deck.NewCardsPerDay`, `Store.UpdateDeckSettings`, etc. do not exist yet.

- [ ] **Step 3: Write the migration**

```sql
-- internal/apps/flash/migrations/0005_deck_pace_snooze.sql
ALTER TABLE flash_decks ADD COLUMN new_cards_per_day INTEGER NOT NULL DEFAULT 20;
ALTER TABLE flash_decks ADD COLUMN reviews_per_day INTEGER;   -- NULL = unlimited
ALTER TABLE flash_decks ADD COLUMN snoozed_until TEXT;        -- NULL = not snoozed
```

- [ ] **Step 4: Modify `deck.go`**

Add a constant to the existing `const` block (`MaxDeckNameRunes`/`MaxDeckDescriptionRunes`):

```go
const (
	MaxDeckNameRunes        = 120
	MaxDeckDescriptionRunes = 500
	// DefaultNewCardsPerDay must match migrations/0005_deck_pace_snooze.sql's
	// "new_cards_per_day INTEGER NOT NULL DEFAULT 20" — CreateDeck (below)
	// sets it explicitly on the Deck value it returns, rather than relying
	// on the DB default to also show up there: CreateDeck never re-fetches
	// the row after inserting it, so without this the returned Deck's
	// NewCardsPerDay would be Go's zero value (0) instead of 20 until the
	// next DeckByID/ListDecks call. Found by plan validation.
	DefaultNewCardsPerDay = 20
)
```

Replace the `Deck` struct:

```go
// Deck is one flash-card deck.
type Deck struct {
	ID          int64
	UserID      int64
	Name        string
	Description string
	CreatedAt   time.Time

	NewCardsPerDay int
	ReviewsPerDay  *int       // nil = unlimited
	SnoozedUntil   *time.Time // nil = not snoozed
}

// IsSnoozed reports whether the deck is hidden from the review queue at now.
func (d Deck) IsSnoozed(now time.Time) bool {
	return d.SnoozedUntil != nil && d.SnoozedUntil.After(now)
}
```

Modify `CreateDeck` (from Task 1) to set the new field explicitly, so the `Deck` it returns matches what a fresh `DeckByID` would show instead of Go's zero value:

```go
// CreateDeck stores a new deck.
func (st *Store) CreateDeck(ctx context.Context, userID int64, name, description string) (Deck, error) {
	name = strings.TrimSpace(name)
	if err := ValidateDeck(name, description); err != nil {
		return Deck{}, err
	}

	d := Deck{UserID: userID, Name: name, Description: description, CreatedAt: st.now(), NewCardsPerDay: DefaultNewCardsPerDay}
	err := st.db.QueryRowContext(ctx,
		`INSERT INTO flash_decks (user_id, name, description, created_at)
		 VALUES (?, ?, ?, ?)
		 RETURNING id`,
		d.UserID, d.Name, d.Description, formatTime(d.CreatedAt),
	).Scan(&d.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return Deck{}, fmt.Errorf("%w: you already have a deck named %q", ErrInvalid, name)
		}
		return Deck{}, fmt.Errorf("flash: create deck: %w", err)
	}
	return d, nil
}
```

(Only the `d := Deck{...}` line actually changes — it gains `, NewCardsPerDay: DefaultNewCardsPerDay`. The rest of `CreateDeck` is unchanged from Task 1 and is shown here in full only so the diff is unambiguous.)

Replace `DeckByID` and `ListDecks`' `SELECT` statements to include the three new columns:

```go
// DeckByID fetches one of userID's own decks.
func (st *Store) DeckByID(ctx context.Context, userID, id int64) (Deck, error) {
	return scanDeck(st.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, description, created_at, new_cards_per_day, reviews_per_day, snoozed_until
		 FROM flash_decks WHERE id = ? AND user_id = ?`, id, userID))
}

// ListDecks returns userID's decks, newest first.
func (st *Store) ListDecks(ctx context.Context, userID int64) ([]Deck, error) {
	rows, err := st.db.QueryContext(ctx,
		`SELECT id, user_id, name, description, created_at, new_cards_per_day, reviews_per_day, snoozed_until
		 FROM flash_decks WHERE user_id = ?
		 ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("flash: list decks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Deck
	for rows.Next() {
		d, err := scanDeckRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flash: list decks: %w", err)
	}
	return out, nil
}
```

Replace `scanDeckRow`:

```go
func scanDeckRow(row rowScanner) (Deck, error) {
	var (
		d             Deck
		createdAt     string
		reviewsPerDay sql.NullInt64
		snoozedUntil  sql.NullString
	)
	err := row.Scan(&d.ID, &d.UserID, &d.Name, &d.Description, &createdAt,
		&d.NewCardsPerDay, &reviewsPerDay, &snoozedUntil)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Deck{}, sql.ErrNoRows // translated by scanDeck
	case err != nil:
		return Deck{}, fmt.Errorf("flash: scan deck: %w", err)
	}
	if d.CreatedAt, err = parseTime(createdAt); err != nil {
		return Deck{}, err
	}
	if reviewsPerDay.Valid {
		n := int(reviewsPerDay.Int64)
		d.ReviewsPerDay = &n
	}
	if snoozedUntil.Valid {
		t, err := parseTime(snoozedUntil.String)
		if err != nil {
			return Deck{}, err
		}
		d.SnoozedUntil = &t
	}
	return d, nil
}
```

Append `ValidateDeckSettings` and the three new store methods at the end of the file:

```go
// ValidateDeckSettings checks a deck's pace fields.
func ValidateDeckSettings(newCardsPerDay int, reviewsPerDay *int) error {
	if newCardsPerDay < 0 {
		return fmt.Errorf("%w: new cards per day cannot be negative", ErrInvalid)
	}
	if reviewsPerDay != nil && *reviewsPerDay < 0 {
		return fmt.Errorf("%w: reviews per day cannot be negative", ErrInvalid)
	}
	return nil
}

// UpdateDeckSettings overwrites userID's own deck's pace. reviewsPerDay of
// nil means unlimited.
func (st *Store) UpdateDeckSettings(ctx context.Context, userID, id int64, newCardsPerDay int, reviewsPerDay *int) (Deck, error) {
	if err := ValidateDeckSettings(newCardsPerDay, reviewsPerDay); err != nil {
		return Deck{}, err
	}
	var reviewsArg any
	if reviewsPerDay != nil {
		reviewsArg = *reviewsPerDay
	}
	res, err := st.db.ExecContext(ctx,
		`UPDATE flash_decks SET new_cards_per_day = ?, reviews_per_day = ? WHERE id = ? AND user_id = ?`,
		newCardsPerDay, reviewsArg, id, userID)
	if err != nil {
		return Deck{}, fmt.Errorf("flash: update deck settings: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Deck{}, fmt.Errorf("flash: update deck settings: %w", err)
	}
	if n == 0 {
		return Deck{}, ErrNotFound
	}
	return st.DeckByID(ctx, userID, id)
}

// SnoozeDeck hides userID's own deck from the review queue until until.
func (st *Store) SnoozeDeck(ctx context.Context, userID, id int64, until time.Time) (Deck, error) {
	res, err := st.db.ExecContext(ctx,
		`UPDATE flash_decks SET snoozed_until = ? WHERE id = ? AND user_id = ?`,
		formatTime(until), id, userID)
	if err != nil {
		return Deck{}, fmt.Errorf("flash: snooze deck: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Deck{}, fmt.Errorf("flash: snooze deck: %w", err)
	}
	if n == 0 {
		return Deck{}, ErrNotFound
	}
	return st.DeckByID(ctx, userID, id)
}

// UnsnoozeDeck clears userID's own deck's snooze immediately.
func (st *Store) UnsnoozeDeck(ctx context.Context, userID, id int64) (Deck, error) {
	res, err := st.db.ExecContext(ctx,
		`UPDATE flash_decks SET snoozed_until = NULL WHERE id = ? AND user_id = ?`,
		id, userID)
	if err != nil {
		return Deck{}, fmt.Errorf("flash: unsnooze deck: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Deck{}, fmt.Errorf("flash: unsnooze deck: %w", err)
	}
	if n == 0 {
		return Deck{}, ErrNotFound
	}
	return st.DeckByID(ctx, userID, id)
}
```

- [ ] **Step 5: Run the store tests to verify they pass**

Run: `go test ./internal/apps/flash/... -v`
Expected: PASS — every new and existing deck test.

- [ ] **Step 6: Write the failing handler test**

Append to `internal/apps/flash/handlers_decks_test.go`:

```go
func TestUpdateDeckSettingsOverHTTP(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID), url.Values{
		"name": {"Spanish"}, "description": {""},
		"new_cards_per_day": {"5"}, "reviews_per_day": {"30"},
	}, "/flash/"+itoa(deck.ID))

	updated, err := s.Store.DeckByID(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.NewCardsPerDay != 5 {
		t.Errorf("NewCardsPerDay = %d, want 5", updated.NewCardsPerDay)
	}
	if updated.ReviewsPerDay == nil || *updated.ReviewsPerDay != 30 {
		t.Errorf("ReviewsPerDay = %v, want 30", updated.ReviewsPerDay)
	}
}

func TestUpdateDeckSettingsRejectsNegativeOverHTTP(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.Post(t, s.Alice, "/flash/"+itoa(deck.ID), url.Values{
		"name": {"Spanish"}, "description": {""},
		"new_cards_per_day": {"-1"}, "reviews_per_day": {""},
	})
	if rec.Code != 400 {
		t.Errorf("negative new_cards_per_day = %d, want 400", rec.Code)
	}
}

func TestSnoozeAndUnsnoozeDeckOverHTTP(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/snooze", url.Values{"days": {"7"}}, "/flash/"+itoa(deck.ID))

	snoozed, err := s.Store.DeckByID(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !snoozed.IsSnoozed(time.Now()) {
		t.Error("deck is not snoozed after posting /snooze")
	}

	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/unsnooze", url.Values{}, "/flash/"+itoa(deck.ID))
	unsnoozed, err := s.Store.DeckByID(t.Context(), s.Alice.User.ID, deck.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unsnoozed.IsSnoozed(time.Now()) {
		t.Error("deck is still snoozed after posting /unsnooze")
	}
}

func TestSnoozeDeckRequiresCSRF(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httpPost(t, "/flash/"+itoa(deck.ID)+"/snooze", url.Values{"days": {"7"}})
	rec := s.Do(t, s.Alice, req)
	if rec.Code != 403 {
		t.Errorf("snooze without CSRF = %d, want 403", rec.Code)
	}
}
```

Add `"time"` to this test file's import block if it is not already there.

- [ ] **Step 7: Run the test to verify it fails**

Run: `go test ./internal/apps/flash/... -run TestUpdateDeckSettingsOverHTTP -v`
Expected: FAIL — the `new_cards_per_day`/`reviews_per_day` fields are not read yet, and `/snooze`/`/unsnooze` routes do not exist.

- [ ] **Step 8: Modify `handlers_decks.go`**

Add two fields to `deckDetailView` and update the two detail-builder functions that populate edit mode:

```go
// Add to the deckDetailView struct, after DescriptionValue:
//   NewCardsPerDayValue string
//   ReviewsPerDayValue  string
```

Replace `editDeckDetail`:

```go
func (a *App) editDeckDetail(r *http.Request, d Deck, errMsg, name, description, newCardsPerDay, reviewsPerDay string) deckDetailView {
	return deckDetailView{
		Mode: "edit", Deck: d, NameValue: name, DescriptionValue: description,
		NewCardsPerDayValue: newCardsPerDay, ReviewsPerDayValue: reviewsPerDay,
		Error: errMsg, CSRFToken: web.CSRFToken(r.Context()),
	}
}
```

Replace `editDeckForm` and `updateDeck`:

```go
func (a *App) editDeckForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	d, err := a.store.DeckByID(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	reviewsStr := ""
	if d.ReviewsPerDay != nil {
		reviewsStr = strconv.Itoa(*d.ReviewsPerDay)
	}
	a.renderDeckIndex(w, r, userID, http.StatusOK,
		a.editDeckDetail(r, d, "", d.Name, d.Description, strconv.Itoa(d.NewCardsPerDay), reviewsStr))
}

// parseDeckSettings turns the edit form's two pace fields into
// UpdateDeckSettings' arguments. An empty reviewsPerDayStr means unlimited.
func parseDeckSettings(newCardsPerDayStr, reviewsPerDayStr string) (int, *int, error) {
	newCardsPerDay, err := strconv.Atoi(strings.TrimSpace(newCardsPerDayStr))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: new cards per day must be a whole number", ErrInvalid)
	}
	var reviewsPerDay *int
	if s := strings.TrimSpace(reviewsPerDayStr); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, nil, fmt.Errorf("%w: reviews per day must be a whole number, or blank for unlimited", ErrInvalid)
		}
		reviewsPerDay = &n
	}
	return newCardsPerDay, reviewsPerDay, nil
}

func (a *App) updateDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	d, err := a.store.DeckByID(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	name := r.PostFormValue("name")
	description := r.PostFormValue("description")
	newCardsPerDayStr := r.PostFormValue("new_cards_per_day")
	reviewsPerDayStr := r.PostFormValue("reviews_per_day")

	if err := ValidateDeck(name, description); err != nil {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest,
			a.editDeckDetail(r, d, userMessage(err), name, description, newCardsPerDayStr, reviewsPerDayStr))
		return
	}
	newCardsPerDay, reviewsPerDay, err := parseDeckSettings(newCardsPerDayStr, reviewsPerDayStr)
	if err != nil {
		a.renderDeckIndex(w, r, userID, http.StatusBadRequest,
			a.editDeckDetail(r, d, userMessage(err), name, description, newCardsPerDayStr, reviewsPerDayStr))
		return
	}

	updated, err := a.store.UpdateDeck(r.Context(), userID, id, name, description)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderDeckIndex(w, r, userID, http.StatusBadRequest,
				a.editDeckDetail(r, d, userMessage(err), name, description, newCardsPerDayStr, reviewsPerDayStr))
			return
		}
		a.fail(w, r, err)
		return
	}
	updated, err = a.store.UpdateDeckSettings(r.Context(), userID, id, newCardsPerDay, reviewsPerDay)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			a.renderDeckIndex(w, r, userID, http.StatusBadRequest,
				a.editDeckDetail(r, updated, userMessage(err), name, description, newCardsPerDayStr, reviewsPerDayStr))
			return
		}
		a.fail(w, r, err)
		return
	}

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/flash/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", "/flash/"+strconv.FormatInt(id, 10))
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, a.viewDeckDetail(r, updated))
}
```

Append the two new handlers at the end of the file:

```go
func (a *App) snoozeDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	days, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("days")))
	if err != nil || days <= 0 {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	until := a.store.now().AddDate(0, 0, days)
	updated, err := a.store.SnoozeDeck(r.Context(), userID, id, until)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("deck snoozed", "app", ID, "user_id", userID, "deck_id", id, "days", days)

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/flash/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", "/flash/"+strconv.FormatInt(id, 10))
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, a.viewDeckDetail(r, updated))
}

func (a *App) unsnoozeDeck(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	updated, err := a.store.UnsnoozeDeck(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.deps.Log.Info("deck unsnoozed", "app", ID, "user_id", userID, "deck_id", id)

	if !web.IsHTMX(r) {
		http.Redirect(w, r, "/flash/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Push-Url", "/flash/"+strconv.FormatInt(id, 10))
	a.renderDeckDetailWithList(w, r, userID, http.StatusOK, a.viewDeckDetail(r, updated))
}
```

Add `"fmt"` and `"strings"` to this file's import block (both are newly needed by `parseDeckSettings`).

- [ ] **Step 9: Register the two new routes in `flash.go`**

Add inside `Mount`, after the existing `POST /{deckID}/delete` line:

```go
// Two more literal-suffix POST routes on a deck, same non-ambiguity shape
// as /{deckID}/delete above: a wildcard followed by a distinct literal
// word, so none of the three can be confused with each other.
r.HandleFunc("POST /{deckID}/snooze", a.snoozeDeck)
r.HandleFunc("POST /{deckID}/unsnooze", a.unsnoozeDeck)
```

- [ ] **Step 10: Modify `templates/decks.html`**

Add snooze/unsnooze controls to `deck-detail-view`, right after the existing toolbar `<div class="row">...</div>` block (before its closing `</div>` for `#deck-detail-view`):

```html
{{if .Deck.SnoozedUntil}}
<div class="notice row">
	<span>Snoozed until {{.Deck.SnoozedUntil.Format "2 Jan 2006"}}.</span>
	<form method="post" action="/flash/{{.Deck.ID}}/unsnooze" hx-post="/flash/{{.Deck.ID}}/unsnooze" hx-target="#deck-detail" hx-swap="innerHTML">
		<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
		<button type="submit" class="button">Unsnooze</button>
	</form>
</div>
{{else}}
<div class="row">
	<form method="post" action="/flash/{{.Deck.ID}}/snooze" hx-post="/flash/{{.Deck.ID}}/snooze" hx-target="#deck-detail" hx-swap="innerHTML">
		<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
		<input type="hidden" name="days" value="7">
		<button type="submit" class="toolbar-btn">Snooze 1 week</button>
	</form>
	<form method="post" action="/flash/{{.Deck.ID}}/snooze" hx-post="/flash/{{.Deck.ID}}/snooze" hx-target="#deck-detail" hx-swap="innerHTML">
		<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
		<input type="hidden" name="days" value="30">
		<button type="submit" class="toolbar-btn">Snooze 1 month</button>
	</form>
</div>
{{end}}
```

Add the two pace fields to `deck-detail-edit`, right after the existing description field's closing `</div>` and before the final `<div class="row">` (Save/Cancel):

```html
<div class="field">
	<label for="deck-new-per-day-{{.Deck.ID}}">New cards per day</label>
	<input id="deck-new-per-day-{{.Deck.ID}}" type="number" min="0" name="new_cards_per_day" value="{{.NewCardsPerDayValue}}" required>
</div>
<div class="field">
	<label for="deck-reviews-per-day-{{.Deck.ID}}">Reviews per day (blank for unlimited)</label>
	<input id="deck-reviews-per-day-{{.Deck.ID}}" type="number" min="0" name="reviews_per_day" value="{{.ReviewsPerDayValue}}">
</div>
```

- [ ] **Step 11: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -v && go test ./internal/arch/...`
Expected: PASS.

- [ ] **Step 12: Commit**

```bash
git add internal/apps/flash/migrations/0005_deck_pace_snooze.sql internal/apps/flash/deck.go internal/apps/flash/handlers_decks.go internal/apps/flash/templates/decks.html internal/apps/flash/flash.go internal/apps/flash/deck_test.go internal/apps/flash/handlers_decks_test.go
git commit -m "feat(flash): add deck pace settings and snooze"
```

---

### Task 4: Review queue construction

**Files:**
- Modify: `internal/apps/flash/review.go`
- Modify: `internal/apps/flash/review_test.go`

**Interfaces:**
- Consumes: `Deck.IsSnoozed`, `Deck.NewCardsPerDay`, `Deck.ReviewsPerDay`, `Store.ListDecks`, `Store.DeckByID` (Task 3); `Card`, `Store.CardByID` (existing, F1 `card.go`); `formatDay`, `Store.DailyCountsForTest`'s underlying `flash_review_counts` table (Task 2).
- Produces: `QueueCard{Card Card, Deck Deck, IsNew bool}`; `Store.DueQueue(ctx context.Context, userID int64, deckID *int64, now time.Time) ([]QueueCard, error)`. Task 5's review handlers call this directly.

- [ ] **Step 1: Write the failing store test**

Append to `internal/apps/flash/review_test.go`:

```go
func TestDueQueueReturnsReviewsBeforeNewCards(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	dueCard, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "due", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	newCard, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "new", "x", "")
	if err != nil {
		t.Fatal(err)
	}

	past := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	if _, err := f.store.GradeCard(ctx, f.alice.ID, dueCard.ID, flash.RatingAgain, past); err != nil {
		t.Fatal(err)
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatalf("DueQueue: %v", err)
	}
	if len(queue) != 2 {
		t.Fatalf("DueQueue returned %d cards, want 2", len(queue))
	}
	if queue[0].Card.ID != dueCard.ID || queue[0].IsNew {
		t.Errorf("queue[0] = %+v, want the due review card marked IsNew=false", queue[0])
	}
	if queue[1].Card.ID != newCard.ID || !queue[1].IsNew {
		t.Errorf("queue[1] = %+v, want the new card marked IsNew=true", queue[1])
	}
}

func TestDueQueueExcludesNotYetDueCards(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	// Easy on a brand-new card schedules it well into the future.
	if _, err := f.store.GradeCard(ctx, f.alice.ID, card.ID, flash.RatingEasy, now); err != nil {
		t.Fatal(err)
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, qc := range queue {
		if qc.Card.ID == card.ID {
			t.Errorf("DueQueue returned a card scheduled into the future: %+v", qc)
		}
	}
}

func TestDueQueueRespectsNewCardsPerDay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateDeckSettings(ctx, f.alice.ID, deck.ID, 1, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "x", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "b", "x", ""); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 {
		t.Fatalf("DueQueue with new_cards_per_day=1 returned %d cards, want 1", len(queue))
	}
}

func TestDueQueueRespectsReviewsPerDay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	limit := 1
	if _, err := f.store.UpdateDeckSettings(ctx, f.alice.ID, deck.ID, 0, &limit); err != nil {
		t.Fatal(err)
	}
	past := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	cardA, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	cardB, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "b", "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, cardA.ID, flash.RatingAgain, past); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GradeCard(ctx, f.alice.ID, cardB.ID, flash.RatingAgain, past); err != nil {
		t.Fatal(err)
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 {
		t.Fatalf("DueQueue with reviews_per_day=1 (both cards due) returned %d cards, want 1", len(queue))
	}
}

func TestDueQueueExcludesSnoozedDecks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "x", ""); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	if _, err := f.store.SnoozeDeck(ctx, f.alice.ID, deck.ID, now.AddDate(0, 0, 7)); err != nil {
		t.Fatal(err)
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 0 {
		t.Errorf("DueQueue for an account with only a snoozed deck = %d cards, want 0", len(queue))
	}

	// Requesting that specific deck's review page directly is also empty,
	// per the design doc: snoozing affects queue construction the same way
	// whether it is cross-deck or scoped to one deck.
	scoped, err := f.store.DueQueue(ctx, f.alice.ID, &deck.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != 0 {
		t.Errorf("DueQueue(deckID) for a snoozed deck = %d cards, want 0", len(scoped))
	}
}

func TestDueQueueScopedToOneDeckIgnoresOtherDecks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deckA, err := f.store.CreateDeck(ctx, f.alice.ID, "A", "")
	if err != nil {
		t.Fatal(err)
	}
	deckB, err := f.store.CreateDeck(ctx, f.alice.ID, "B", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deckA.ID, flash.CardTypeBasic, "a", "x", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deckB.ID, flash.CardTypeBasic, "b", "x", ""); err != nil {
		t.Fatal(err)
	}

	queue, err := f.store.DueQueue(ctx, f.alice.ID, &deckA.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 || queue[0].Deck.ID != deckA.ID {
		t.Errorf("DueQueue(deckA) = %+v, want exactly one card from deckA", queue)
	}
}

func TestDueQueueIsOwnerScoped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck, err := f.store.CreateDeck(ctx, f.alice.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateCard(ctx, f.alice.ID, deck.ID, flash.CardTypeBasic, "a", "x", ""); err != nil {
		t.Fatal(err)
	}
	queue, err := f.store.DueQueue(ctx, f.bob.ID, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 0 {
		t.Errorf("bob's DueQueue sees alice's cards: %+v", queue)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/apps/flash/... -run TestDueQueueReturnsReviewsBeforeNewCards -v`
Expected: FAIL — `QueueCard` and `Store.DueQueue` do not exist yet.

- [ ] **Step 3: Append `DueQueue` and its helpers to `review.go`**

```go
// QueueCard is one card ready for review, with enough context to render it
// and to know which of a deck's two daily budgets grading it will spend.
type QueueCard struct {
	Card  Card
	Deck  Deck
	IsNew bool
}

// DueQueue returns cards eligible for review right now, in review-then-new
// order. If deckID is non-nil, only that deck is considered — and only if
// it is not currently snoozed, the same rule applied to every deck when
// deckID is nil (every non-snoozed deck belonging to userID).
func (st *Store) DueQueue(ctx context.Context, userID int64, deckID *int64, now time.Time) ([]QueueCard, error) {
	decks, err := st.dueQueueDecks(ctx, userID, deckID, now)
	if err != nil {
		return nil, err
	}

	var reviews, fresh []QueueCard
	for _, d := range decks {
		newCount, reviewCount, err := st.DailyCountsForTest(ctx, userID, d.ID, now)
		if err != nil {
			return nil, err
		}

		reviewsRemaining := -1 // sentinel: unlimited
		if d.ReviewsPerDay != nil {
			reviewsRemaining = *d.ReviewsPerDay - reviewCount
			if reviewsRemaining < 0 {
				reviewsRemaining = 0
			}
		}
		due, err := st.dueReviewCards(ctx, userID, d, now, reviewsRemaining)
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, due...)

		newRemaining := d.NewCardsPerDay - newCount
		if newRemaining < 0 {
			newRemaining = 0
		}
		newCards, err := st.newQueueCards(ctx, userID, d, newRemaining)
		if err != nil {
			return nil, err
		}
		fresh = append(fresh, newCards...)
	}
	return append(reviews, fresh...), nil
}

// dueQueueDecks resolves which decks DueQueue should consider: the one
// named by deckID (if it is not snoozed), or every one of userID's decks
// that are not currently snoozed.
func (st *Store) dueQueueDecks(ctx context.Context, userID int64, deckID *int64, now time.Time) ([]Deck, error) {
	if deckID != nil {
		d, err := st.DeckByID(ctx, userID, *deckID)
		if err != nil {
			return nil, err
		}
		if d.IsSnoozed(now) {
			return nil, nil
		}
		return []Deck{d}, nil
	}

	all, err := st.ListDecks(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Deck, 0, len(all))
	for _, d := range all {
		if !d.IsSnoozed(now) {
			out = append(out, d)
		}
	}
	return out, nil
}

// dueReviewCards returns d's cards that are due at or before now, oldest
// due date first, up to limit (a negative limit means unlimited).
func (st *Store) dueReviewCards(ctx context.Context, userID int64, d Deck, now time.Time, limit int) ([]QueueCard, error) {
	if limit == 0 {
		return nil, nil
	}
	query := `
		SELECT c.id, c.deck_id, c.user_id, c.card_type, c.front, c.back, c.notes, c.created_at
		FROM flash_cards c
		JOIN flash_card_state s ON s.card_id = c.id AND s.user_id = c.user_id
		WHERE c.deck_id = ? AND c.user_id = ? AND s.due_at <= ?
		ORDER BY s.due_at ASC`
	args := []any{d.ID, userID, formatTime(now)}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	return st.queryQueueCards(ctx, query, args, d, false)
}

// newQueueCards returns up to limit of d's cards that have never been
// reviewed, oldest-created first.
func (st *Store) newQueueCards(ctx context.Context, userID int64, d Deck, limit int) ([]QueueCard, error) {
	if limit <= 0 {
		return nil, nil
	}
	query := `
		SELECT c.id, c.deck_id, c.user_id, c.card_type, c.front, c.back, c.notes, c.created_at
		FROM flash_cards c
		LEFT JOIN flash_card_state s ON s.card_id = c.id AND s.user_id = c.user_id
		WHERE c.deck_id = ? AND c.user_id = ? AND s.card_id IS NULL
		ORDER BY c.created_at ASC
		LIMIT ?`
	return st.queryQueueCards(ctx, query, []any{d.ID, userID, limit}, d, true)
}

func (st *Store) queryQueueCards(ctx context.Context, query string, args []any, d Deck, isNew bool) ([]QueueCard, error) {
	rows, err := st.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("flash: due queue: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []QueueCard
	for rows.Next() {
		c, err := scanCardRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, QueueCard{Card: c, Deck: d, IsNew: isNew})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flash: due queue: %w", err)
	}
	return out, nil
}
```

`dueReviewCards` reuses `scanCardRow` from `card.go` (same package, unexported, already scans exactly the eight `flash_cards` columns selected here).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -v`
Expected: PASS — every `DueQueue` test, plus no regressions in Tasks 1-3.

- [ ] **Step 5: Commit**

```bash
git add internal/apps/flash/review.go internal/apps/flash/review_test.go
git commit -m "feat(flash): add cross-deck and per-deck review queue construction"
```

---

### Task 5: Review session — handlers, templates, keyboard shortcuts

**Files:**
- Create: `internal/apps/flash/handlers_review.go`
- Create: `internal/apps/flash/templates/review.html`
- Create: `internal/apps/flash/static/flash-review.js`
- Modify: `internal/apps/flash/flash.go` (embed the script, register 5 new routes)
- Create: `internal/apps/flash/handlers_review_test.go`

**Interfaces:**
- Consumes: `Store.DueQueue`, `QueueCard` (Task 4); `Store.GradeCard`, `Store.UndoLastGrade`, `RatingAgain..RatingEasy` (Task 2/1); `a.userID`, `a.deckIDFromPath` (existing, `handlers_decks.go`), `a.fail`, `a.render` (existing, `handlers_decks.go`).
- Produces: `a.cardIDFromForm` (new, this task — grade/undo take the card id from the POST body, not a path wildcard); routes `GET /flash/review`, `GET /flash/review/{deckID}`, `POST /flash/review/grade`, `POST /flash/review/undo`, `GET /flash/flash-review.js`. This is F2's last task.

- [ ] **Step 1: Write the failing handler test**

```go
// internal/apps/flash/handlers_review_test.go
package flash_test

import (
	"net/url"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestReviewShowsACardAcrossAllDecks(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", ""); err != nil {
		t.Fatal(err)
	}

	doc := s.Get(t, s.Alice, "/flash/review")
	doc.MustHave(".flash-review-front")
}

func TestReviewShowsNothingDueWhenQueueIsEmpty(t *testing.T) {
	s := newServer(t)
	doc := s.Get(t, s.Alice, "/flash/review")
	doc.MustNotHave(".flash-review-front")
}

func TestReviewScopedToOneDeck(t *testing.T) {
	s := newServer(t)
	deckA, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "A", "")
	if err != nil {
		t.Fatal(err)
	}
	deckB, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "B", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deckB.ID, flash.CardTypeBasic, "b1", "x", ""); err != nil {
		t.Fatal(err)
	}

	// deckA has no cards, so its own review page shows nothing due even
	// though deckB (a different deck) has one.
	doc := s.Get(t, s.Alice, "/flash/review/"+itoa(deckA.ID))
	doc.MustNotHave(".flash-review-front")
}

func TestGradingACardAdvancesTheQueue(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}

	rec := s.PostHX(t, s.Alice, "/flash/review/grade", url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}})
	if rec.Code != 200 {
		t.Fatalf("grade = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustNotHave(".flash-review-front") // the only card was just graded; nothing left due
	doc.MustHave(".flash-undo-btn")
}

func TestGradingRequiresCSRF(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httpPost(t, "/flash/review/grade", url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}})
	rec := s.Do(t, s.Alice, req)
	if rec.Code != 403 {
		t.Errorf("grade without CSRF = %d, want 403", rec.Code)
	}
}

func TestGradingRejectsAnInvalidRating(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.PostHX(t, s.Alice, "/flash/review/grade", url.Values{"card_id": {itoa(card.ID)}, "rating": {"7"}})
	if rec.Code != 400 {
		t.Errorf("grade with rating=7 = %d, want 400", rec.Code)
	}
}

func TestGradingSomeoneElsesCardIs404(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "alice's", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "a", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := s.PostHX(t, s.Bob, "/flash/review/grade", url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}})
	if rec.Code != 404 {
		t.Errorf("grading someone else's card = %d, want 404", rec.Code)
	}
}

func TestUndoAfterGradingReshowsTheCard(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	s.PostHX(t, s.Alice, "/flash/review/grade", url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}})

	rec := s.PostHX(t, s.Alice, "/flash/review/undo", url.Values{"card_id": {itoa(card.ID)}})
	if rec.Code != 200 {
		t.Fatalf("undo = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".flash-review-front")
	doc.MustNotHave(".flash-undo-btn") // the undo slot was just spent
}

func TestUndoingTwiceIsRejected(t *testing.T) {
	s := newServer(t)
	deck, err := s.Store.CreateDeck(t.Context(), s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.Store.CreateCard(t.Context(), s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	s.PostHX(t, s.Alice, "/flash/review/grade", url.Values{"card_id": {itoa(card.ID)}, "rating": {"3"}})
	s.PostHX(t, s.Alice, "/flash/review/undo", url.Values{"card_id": {itoa(card.ID)}})

	rec := s.PostHX(t, s.Alice, "/flash/review/undo", url.Values{"card_id": {itoa(card.ID)}})
	if rec.Code != 400 {
		t.Errorf("second undo = %d, want 400", rec.Code)
	}
}

func TestReviewScriptIsServed(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, s.Alice, httpGet(t, "/flash/flash-review.js"))
	if rec.Code != 200 {
		t.Fatalf("GET /flash/flash-review.js = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
}
```

Add `"github.com/iliafrenkel/on-suite/internal/htmlassert"` to this file's import block (used by `htmlassert.Parse` above).

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/apps/flash/... -run TestReviewShowsACardAcrossAllDecks -v`
Expected: FAIL — the review routes and `handlers_review.go` do not exist yet.

- [ ] **Step 3: Write `handlers_review.go`**

```go
// internal/apps/flash/handlers_review.go
package flash

import (
	"net/http"
	"strconv"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// deckScopeFromQuery parses the optional ?deck= query parameter used to
// scope a review session to one deck; its absence means "every deck."
func (a *App) deckScopeFromQuery(w http.ResponseWriter, r *http.Request) (*int64, bool) {
	raw := r.URL.Query().Get("deck")
	if raw == "" {
		return nil, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return nil, false
	}
	return &id, true
}

func deckScopeString(deckID *int64) string {
	if deckID == nil {
		return ""
	}
	return strconv.FormatInt(*deckID, 10)
}

// cardIDFromForm parses the card_id form field grade/undo use in place of a
// path wildcard — see flash.go's Mount comment on why these two routes take
// the card id from the POST body rather than the URL path.
func (a *App) cardIDFromForm(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PostFormValue("card_id"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

type reviewCardView struct {
	Card  Card
	Deck  Deck
	IsNew bool
}

// reviewView is what templates/review.html's "review-body" block renders,
// both as a full page and as an HTMX fragment after grading or undoing.
type reviewView struct {
	Current          *reviewCardView
	DeckScope        string // "" (all decks) or a deck id, threaded into every form action below
	LastGradedCardID int64  // 0 = nothing to undo yet
	CSRFToken        string
}

// review backs GET /review: the cross-deck queue, or one deck's queue if
// ?deck= names it.
func (a *App) review(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	deckID, ok := a.deckScopeFromQuery(w, r)
	if !ok {
		return
	}
	if deckID != nil {
		if _, err := a.store.DeckByID(r.Context(), userID, *deckID); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	a.renderReview(w, r, userID, deckID, http.StatusOK, 0)
}

// deckReview backs GET /review/{deckID}: always scoped to the deck named in
// the path, regardless of any ?deck= query value.
func (a *App) deckReview(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.deckIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := a.store.DeckByID(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderReview(w, r, userID, &id, http.StatusOK, 0)
}

func (a *App) renderReview(w http.ResponseWriter, r *http.Request, userID int64, deckID *int64, status int, lastGradedCardID int64) {
	queue, err := a.store.DueQueue(r.Context(), userID, deckID, time.Now())
	if err != nil {
		a.deps.Errors.Internal(w, r, err)
		return
	}
	view := reviewView{
		DeckScope:        deckScopeString(deckID),
		LastGradedCardID: lastGradedCardID,
		CSRFToken:        web.CSRFToken(r.Context()),
	}
	if len(queue) > 0 {
		view.Current = &reviewCardView{Card: queue[0].Card, Deck: queue[0].Deck, IsNew: queue[0].IsNew}
	}

	if web.IsHTMX(r) {
		if err := a.deps.Render.Fragment(w, status, "flash/review", "review-body", view); err != nil {
			a.deps.Errors.Internal(w, r, err)
		}
		return
	}
	page := a.deps.Page(r, "Review")
	page.Data = view
	a.render(w, r, status, "flash/review", page)
}

func (a *App) gradeCardHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	cardID, ok := a.cardIDFromForm(w, r)
	if !ok {
		return
	}
	deckID, ok := a.deckScopeFromQuery(w, r)
	if !ok {
		return
	}

	rating, err := strconv.Atoi(r.PostFormValue("rating"))
	if err != nil || rating < RatingAgain || rating > RatingEasy {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	if _, err := a.store.GradeCard(r.Context(), userID, cardID, rating, time.Now()); err != nil {
		a.fail(w, r, err)
		return
	}
	a.renderReview(w, r, userID, deckID, http.StatusOK, cardID)
}

func (a *App) undoGradeHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	cardID, ok := a.cardIDFromForm(w, r)
	if !ok {
		return
	}
	deckID, ok := a.deckScopeFromQuery(w, r)
	if !ok {
		return
	}

	_, undone, err := a.store.UndoLastGrade(r.Context(), userID, cardID, time.Now())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if !undone {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	a.renderReview(w, r, userID, deckID, http.StatusOK, 0)
}
```

Note why `reviewView` has no `Title`/`Shell` fields the way `deckIndexView`/`cardIndexView` do: `review.html` carries no shell-crumb-tail OOB block, since a single-card review page has no list pane whose title needs syncing — so this file never needs the `render` package at all.

- [ ] **Step 4: Write `static/flash-review.js`**

```js
// internal/apps/flash/static/flash-review.js
//
// Keyboard shortcuts for the review page: Space reveals the answer, 1-4
// grade it (Again/Hard/Good/Easy), U undoes the last grade. Mirrors
// internal/apps/reader/static/reader.js's press()/keydown pattern.
(function () {
	"use strict";

	function isTyping(el) {
		if (!el) return false;
		var tag = el.tagName;
		return tag === "INPUT" || tag === "TEXTAREA" || el.isContentEditable;
	}

	function press(selector) {
		var el = document.querySelector(selector);
		if (el) el.click();
	}

	function reveal() {
		var answer = document.querySelector(".flash-review-answer");
		if (answer) answer.hidden = false;
	}

	document.addEventListener("keydown", function (e) {
		if (e.metaKey || e.ctrlKey || e.altKey) return;
		if (isTyping(e.target)) return;

		switch (e.key) {
			case " ":
				reveal();
				break;
			case "1":
				press(".flash-grade-again");
				break;
			case "2":
				press(".flash-grade-hard");
				break;
			case "3":
				press(".flash-grade-good");
				break;
			case "4":
				press(".flash-grade-easy");
				break;
			case "u":
			case "U":
				press(".flash-undo-btn");
				break;
			default:
				return;
		}
		e.preventDefault();
	});

	document.addEventListener("click", function (e) {
		if (e.target.closest(".flash-reveal-btn")) reveal();
	});
})();
```

- [ ] **Step 5: Write `templates/review.html`**

```html
{{define "head"}}
<script src="/flash/flash-review.js" defer></script>
{{end}}

{{define "review-body"}}
{{if .Current}}
<div class="stack flash-review-card" id="review-card">
	<p class="faint">{{.Current.Deck.Name}} · {{if .Current.IsNew}}new{{else}}review{{end}}</p>
	<div class="flash-review-front">{{.Current.Card.Front}}</div>
	<button type="button" class="toolbar-btn flash-reveal-btn">Reveal (Space)</button>
	<div class="flash-review-answer" hidden>
		{{if eq .Current.Card.CardType "basic"}}<div class="flash-review-back">{{.Current.Card.Back}}</div>{{end}}
		{{with .Current.Card.Notes}}<p class="dim">{{.}}</p>{{end}}
		<form method="post"
		      action="/flash/review/grade{{if .DeckScope}}?deck={{.DeckScope}}{{end}}"
		      hx-post="/flash/review/grade{{if .DeckScope}}?deck={{.DeckScope}}{{end}}"
		      hx-target="#review-body" hx-swap="innerHTML" class="row">
			<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
			<input type="hidden" name="card_id" value="{{.Current.Card.ID}}">
			<button type="submit" name="rating" value="1" class="toolbar-btn flash-grade-again">Again (1)</button>
			<button type="submit" name="rating" value="2" class="toolbar-btn flash-grade-hard">Hard (2)</button>
			<button type="submit" name="rating" value="3" class="toolbar-btn flash-grade-good">Good (3)</button>
			<button type="submit" name="rating" value="4" class="toolbar-btn flash-grade-easy">Easy (4)</button>
		</form>
	</div>
</div>
{{else}}
<p class="dim">All done for now.</p>
{{end}}
{{if .LastGradedCardID}}
<form method="post"
      action="/flash/review/undo{{if .DeckScope}}?deck={{.DeckScope}}{{end}}"
      hx-post="/flash/review/undo{{if .DeckScope}}?deck={{.DeckScope}}{{end}}"
      hx-target="#review-body" hx-swap="innerHTML">
	<input type="hidden" name="{{csrfField}}" value="{{.CSRFToken}}">
	<input type="hidden" name="card_id" value="{{.LastGradedCardID}}">
	<button type="submit" class="toolbar-btn flash-undo-btn">Undo (U)</button>
</form>
{{end}}
{{end}}

{{define "content"}}
<div class="stack">
	<div class="row list-head">
		<h1>Review</h1>
	</div>
	<div id="review-body">
		{{template "review-body" .Data}}
	</div>
</div>
{{end}}
```

- [ ] **Step 6: Embed the script and register routes in `flash.go`**

Add near the top of the file, alongside the existing `templateFiles` embed:

```go
//go:embed static/flash-review.js
var scriptFiles embed.FS
```

Add this method (placed anywhere in `flash.go`, e.g. right after `Templates`):

```go
// script serves flash-review.js, behind the same sign-in requirement as
// every other route here — no page loads it without already being on an
// authenticated Flash page. Mirrors internal/apps/reader's own script
// method for reader.js.
func (a *App) script(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, scriptFiles, "static/flash-review.js")
}
```

Add `"net/http"` to `flash.go`'s import block (needed by `http.ServeFileFS` above).

Add the five new routes inside `Mount`, after the tag-filter route:

```go
// Review routes. "review" and "flash-review.js" are literal single
// segments alongside the existing wildcard single-segment routes
// (/{deckID}, /tags/{tagName}'s "tags" is also a literal single segment at
// this same position but a different word) — literals never conflict with
// each other or with a same-position wildcard, per the precedent already
// established by /new and /tags/{tagName} above.
//
// The per-deck review route is GET /review/{deckID}, not GET
// /{deckID}/review: the latter shape is the exact ambiguity paste.go's own
// Mount warns about, just one level removed — a wildcard-then-literal GET
// pattern here genuinely conflicts with the existing literal-then-wildcard
// GET /edit/{deckID} above, since "/flash/edit/review" matches both (as
// GET /edit/{deckID} with deckID="review", and as GET /{deckID}/review
// with deckID="edit") and neither is more specific than the other —
// confirmed by a startup panic during plan validation. GET
// /review/{deckID} sidesteps this entirely: it is a literal-then-wildcard
// shape like /edit/{deckID}, just with a different literal, so the two
// families never collide.
//
// Grading and undo take the card id from the POST body (card_id), not the
// URL path, so their routes are POST /review/grade and POST /review/undo —
// 2-segment, literal-then-literal. A first attempt used POST
// /review/{cardID}/grade (3-segment, literal-wildcard-literal) and it
// structurally conflicted with the existing POST /{deckID}/cards/{cardID}
// (3-segment, wildcard-literal-wildcard): Go's mux flags any (L,W,L) vs
// (W,L,W) pair of the same length as ambiguous regardless of which words
// the literals are, since the path built from the first pattern's own
// literals — here "/review/cards/grade" — always satisfies both patterns'
// wildcard slots too, again confirmed by a startup panic during
// validation. Ending a route in a literal that differs from every other
// same-length pattern's final literal (as /review/grade and /review/undo
// do against /{deckID}/delete, /{deckID}/snooze, /{deckID}/unsnooze) is
// what actually guarantees safety: a literal-vs-literal mismatch at any
// one position makes overlap impossible no matter how the remaining
// positions compare.
r.HandleFunc("GET /review", a.review)
r.HandleFunc("GET /review/{deckID}", a.deckReview)
r.HandleFunc("POST /review/grade", a.gradeCardHandler)
r.HandleFunc("POST /review/undo", a.undoGradeHandler)
r.HandleFunc("GET /flash-review.js", a.script)
```

**This entire plan was validated by actually running it against the real
codebase before handing it to an implementer** (build the code, run every
test, fix what failed) — both of the route conflicts above are exactly the
class of mistake this task's own reasoning was trying to guard against
elsewhere, caught only because the pattern was registered for real rather
than reasoned about on paper. If a future edit changes any route in this
file, re-run `go build ./internal/apps/flash/...` and the app's full test
suite before trusting a new shape's safety comment.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/apps/flash/... -v`
Expected: PASS — every review handler test, plus no regressions across all of F1 and F2 so far.

- [ ] **Step 8: Run the full check**

Run:
```bash
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
go mod tidy && git diff --exit-code go.mod go.sum
go test ./... -race -count=1
go test ./internal/arch/...
```
Expected: every command is clean/green, matching [AGENTS.md](../../../AGENTS.md)'s definition of the full check, plus the two containment tests (`TestReadabilityIsContained`, `TestFSRSIsContained`) both passing.

- [ ] **Step 9: Commit**

```bash
git add internal/apps/flash/handlers_review.go internal/apps/flash/templates/review.html internal/apps/flash/static/flash-review.js internal/apps/flash/flash.go internal/apps/flash/handlers_review_test.go
git commit -m "feat(flash): add the review session with keyboard shortcuts"
```

---

## After this plan

F2 leaves ON Flash with a complete FSRS-driven review loop: grading, undo, daily limits, and snooze. Still missing: import (F3), media (F4), sharing (F5), and stats (F6), each its own plan against [docs/superpowers/specs/2026-09-17-on-flash-features.md](../specs/2026-09-17-on-flash-features.md).
