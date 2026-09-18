// internal/apps/flash/share_test.go
package flash_test

import (
	"context"
	"errors"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestShareDeckCreatesPendingShare(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sh.Status != flash.ShareStatusPending {
		t.Errorf("Status = %q, want %q", sh.Status, flash.ShareStatusPending)
	}
	if sh.DeckID != d.ID || sh.FromUserID != f.alice.ID || sh.ToUserID != f.bob.ID {
		t.Errorf("share = %+v, want deck %d alice->bob", sh, d.ID)
	}
}

func TestShareDeckRejectsSelfShare(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.alice.ID)
	if !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestShareDeckRejectsSomeoneElsesDeck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.store.ShareDeck(ctx, f.bob.ID, d.ID, f.alice.ID)
	if !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestShareDeckIsIdempotentWhilePending(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}

	first, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Errorf("re-sharing while pending created a second row: %d != %d", first.ID, second.ID)
	}
}

func TestRevokeShareRequiresPendingAndOwnership(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.RevokeShare(ctx, f.bob.ID, sh.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("revoke by non-owner: err = %v, want ErrNotFound", err)
	}
	if err := f.store.RevokeShare(ctx, f.alice.ID, sh.ID); err != nil {
		t.Fatal(err)
	}
	// revoking again (no longer pending) fails
	if err := f.store.RevokeShare(ctx, f.alice.ID, sh.ID); !errors.Is(err, flash.ErrInvalid) {
		t.Errorf("re-revoke: err = %v, want ErrInvalid", err)
	}
}

func TestDeclineShareThenReshareCreatesFreshOffer(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.DeclineShare(ctx, f.bob.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	// a declined share never blocks a fresh offer
	second, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Error("re-share after decline reused the declined row instead of creating a fresh one")
	}
	if second.Status != flash.ShareStatusPending {
		t.Errorf("Status = %q, want pending", second.Status)
	}
}

func TestDeleteDeckCascadesShares(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.store.CreateDeck(ctx, f.alice.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.store.ShareDeck(ctx, f.alice.ID, d.ID, f.bob.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.store.DeleteDeck(ctx, f.alice.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	// the share row is gone: revoking it now reports ErrNotFound the same as
	// a bad ID would
	if err := f.store.RevokeShare(ctx, f.alice.ID, sh.ID); !errors.Is(err, flash.ErrNotFound) {
		t.Errorf("revoke after cascade delete: err = %v, want ErrNotFound", err)
	}
}
