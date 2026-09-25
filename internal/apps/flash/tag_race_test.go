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

// TestSetCardTagsConcurrentWithDeleteCardChecksOwnershipInsideItsTransaction
// pins the #288 comment: SetCardTags used to check ownership with
// cardOwnerCheck on st.db, before BeginTx. With one database connection,
// that hands the connection back between the check and the write, so a
// concurrent DeleteCard of the same card can run to completion in between —
// the card is gone by the time SetCardTags's own transaction starts, and its
// INSERT into flash_card_tags fails a foreign-key check instead of the
// friendly ErrNotFound every other "not yours/not there" case in this
// package returns. Moving the ownership check onto the transaction (as
// GradeCard and UndoLastGrade already do, #294) makes the whole check-and-
// write atomic: a concurrent delete now either finishes entirely before
// SetCardTags's transaction starts (ErrNotFound) or entirely after it
// (SetCardTags succeeds normally) — never the interleaving above.
//
// Genuinely forcing the old interleaving is a matter of goroutine scheduling
// luck, so this repeats the race many times over fresh cards: with the old
// code it reliably produces at least one non-ErrNotFound error within a few
// dozen trials; with the fix the invariant holds on every trial.
func TestSetCardTagsConcurrentWithDeleteCardChecksOwnershipInsideItsTransaction(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deck := qDeck(t, f, "Spanish")

	const trials = 50
	for i := 0; i < trials; i++ {
		card := qCard(t, f, deck.ID, "hola")

		results := concurrentlyOps([]func() error{
			func() error { return f.store.SetCardTags(ctx, f.alice.ID, card.ID, []string{"hard"}) },
			func() error { return f.store.DeleteCard(ctx, f.alice.ID, deck.ID, card.ID) },
		})
		setErr, delErr := results[0], results[1]

		if delErr != nil {
			t.Fatalf("trial %d: DeleteCard: %v", i, delErr)
		}
		if setErr != nil && !errors.Is(setErr, flash.ErrNotFound) {
			t.Fatalf("trial %d: SetCardTags raced with DeleteCard = %v, want nil or ErrNotFound", i, setErr)
		}
	}
}
