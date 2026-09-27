// internal/apps/flash/tag_race_test.go
package flash_test

import (
	"context"
	"errors"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

// concurrentlyOps runs each of ops at once, released together by a start
// barrier so the calls genuinely overlap, and returns their errors in
// goroutine order — a sibling of review_race_test.go's concurrently, for
// racing two different operations against each other instead of running one
// function n times.
func concurrentlyOps(ops []func() error) []error {
	out := make([]error, len(ops))
	start := make(chan struct{})
	done := make(chan struct{})
	for i, op := range ops {
		i, op := i, op
		go func() {
			<-start
			out[i] = op()
			done <- struct{}{}
		}()
	}
	close(start)
	for range ops {
		<-done
	}
	return out
}

// TestSaveCardFormConcurrentWithDeleteCardChecksOwnershipInsideItsTransaction
// pins the #288 comment, now against SaveCardForm's update path (#382:
// SetCardTags itself is gone, but the same interleaving is still reachable
// through SaveCardForm's own ownership check, updateCardRow's WHERE clause,
// which runs inside SaveCardForm's transaction exactly where SetCardTags's
// cardOwner(ctx, tx, ...) check used to). With one database connection, an
// ownership check before BeginTx hands the connection back between the
// check and the write, so a concurrent DeleteCard of the same card can run
// to completion in between — the card would be gone by the time the update
// transaction starts, and its UPDATE/INSERT would either match no row or
// fail a foreign-key check instead of the friendly ErrNotFound every other
// "not yours/not there" case in this package returns. Running the ownership
// check inside the transaction (as GradeCard and UndoLastGrade already do,
// #294) makes the whole check-and-write atomic: a concurrent delete now
// either finishes entirely before SaveCardForm's transaction starts
// (ErrNotFound) or entirely after it (SaveCardForm succeeds normally) —
// never the interleaving above.
//
// Genuinely forcing the old interleaving is a matter of goroutine scheduling
// luck, so this repeats the race many times over fresh cards: with the old
// code it reliably produces at least one non-ErrNotFound error within a few
// dozen trials; with the fix the invariant holds on every trial. That
// reliability only holds under `go test -race`, which slows and interleaves
// goroutines enough to surface the race — without -race this test can pass
// even against the old, buggy code, so it is not a reliable regression
// guard on its own outside a -race run.
func TestSaveCardFormConcurrentWithDeleteCardChecksOwnershipInsideItsTransaction(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck := qDeck(t, f, "Spanish")

	const trials = 50
	for i := 0; i < trials; i++ {
		card := qCard(t, f, deck.ID, "hola")

		results := concurrentlyOps([]func() error{
			func() error {
				_, err := applyCardForm(t, ctx, f.store, f.alice.ID, deck.ID, card, flash.CardForm{Tags: []string{"hard"}})
				return err
			},
			func() error { return f.store.DeleteCard(ctx, f.alice.ID, deck.ID, card.ID) },
		})
		saveErr, delErr := results[0], results[1]

		if delErr != nil {
			t.Fatalf("trial %d: DeleteCard: %v", i, delErr)
		}
		if saveErr != nil && !errors.Is(saveErr, flash.ErrNotFound) {
			t.Fatalf("trial %d: SaveCardForm raced with DeleteCard = %v, want nil or ErrNotFound", i, saveErr)
		}
	}
}
