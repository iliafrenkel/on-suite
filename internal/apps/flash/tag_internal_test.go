// internal/apps/flash/tag_internal_test.go
package flash

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// newInternalFixture is deck_test.go's newFixture, duplicated here (rather
// than shared) because this file is package flash, not flash_test: it needs
// direct access to unexported store internals like cardOwner and st.db.
func newInternalFixture(t *testing.T) (*Store, *sql.DB, auth.User) {
	t.Helper()
	ctx := context.Background()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	migrations, err := db.Collect(auth.Namespace, auth.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	appMigrations, err := db.Collect(ID, Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(migrations, appMigrations...)); err != nil {
		t.Fatal(err)
	}

	users := auth.NewStore(handle)
	alice, err := users.CreateUser(ctx, "alice", apptest.PasswordHash, true)
	if err != nil {
		t.Fatal(err)
	}

	return NewStore(handle), handle, alice
}

// TestCardOwnerOnlyTreatsNoRowsAsNotFound pins #288: cardOwner used to map
// every error from its SELECT — a connection failure, a scan error, a
// cancelled context — to ErrNotFound, which handlers turn into a 404
// instead of the 500 a real database error deserves. Forcing a genuine
// driver error without heavy mocking is awkward, so this drives it through
// an already-committed transaction: querying a done tx returns
// sql.ErrTxDone, never sql.ErrNoRows, so cardOwner must wrap and return it
// rather than reporting "not found".
func TestCardOwnerOnlyTreatsNoRowsAsNotFound(t *testing.T) {
	store, handle, alice := newInternalFixture(t)
	ctx := context.Background()

	deck, err := store.CreateDeck(ctx, alice.ID, "Spanish", "", DefaultDeckColor)
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateCard(ctx, alice.ID, deck.ID, CardTypeBasic, "hola", "hello", "")
	if err != nil {
		t.Fatal(err)
	}

	tx, err := handle.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// tx is now done; any query against it fails with sql.ErrTxDone.

	_, err = cardOwner(ctx, tx, alice.ID, card.ID)
	if err == nil {
		t.Fatal("cardOwner on a done tx returned no error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("cardOwner on a done tx = %v, want a wrapped error, not ErrNotFound", err)
	}
	if !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("cardOwner on a done tx = %v, want it to wrap sql.ErrTxDone", err)
	}
}

// TestCardOwnerStillReturnsErrNotFoundForAMissingCard makes sure the fix
// above does not regress the actual not-found path: a real card owned by
// someone else must still come back as ErrNotFound, not a wrapped error.
func TestCardOwnerStillReturnsErrNotFoundForAMissingCard(t *testing.T) {
	store, _, alice := newInternalFixture(t)
	ctx := context.Background()

	if _, err := cardOwner(ctx, store.db, alice.ID, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("cardOwner(missing card) = %v, want ErrNotFound", err)
	}
}
