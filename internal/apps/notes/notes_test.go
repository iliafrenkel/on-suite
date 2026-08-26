package notes_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/notes"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

func TestValidateAcceptsAnEmptyBullet(t *testing.T) {
	if err := notes.Validate("", ""); err != nil {
		t.Fatalf(`Validate("", "") = %v; an empty bullet is what Enter creates`, err)
	}
}

func TestValidateRejectsBadText(t *testing.T) {
	tests := []struct{ name, title, note string }{
		{"title too long", strings.Repeat("a", notes.MaxTitleRunes+1), ""},
		{"note too long", "", strings.Repeat("a", notes.MaxNoteRunes+1)},
		{"title is not utf-8", "\xff\xfe", ""},
		{"note is not utf-8", "", "\xff\xfe"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := notes.Validate(tc.title, tc.note)
			if !errors.Is(err, notes.ErrInvalid) {
				t.Fatalf("Validate = %v; want ErrInvalid", err)
			}
		})
	}
}

func TestValidateCountsRunesNotBytes(t *testing.T) {
	// MaxTitleRunes Cyrillic characters is twice that many bytes and must
	// still be accepted: the bound is on what a person typed, not on how
	// UTF-8 happens to encode it.
	if err := notes.Validate(strings.Repeat("щ", notes.MaxTitleRunes), ""); err != nil {
		t.Fatalf("Validate(%d Cyrillic runes) = %v; want nil", notes.MaxTitleRunes, err)
	}
}

func TestValidateDueAcceptsEmpty(t *testing.T) {
	if err := notes.ValidateDue(""); err != nil {
		t.Fatalf(`ValidateDue("") = %v; empty means "no due date"`, err)
	}
}

func TestValidateDueAcceptsAWellFormedDate(t *testing.T) {
	if err := notes.ValidateDue("2026-03-05"); err != nil {
		t.Fatalf("ValidateDue(2026-03-05) = %v", err)
	}
}

func TestValidateDueRejectsBadInput(t *testing.T) {
	for _, due := range []string{
		"not-a-date",
		"2026-13-01", // no month 13
		"2026-02-30", // February has no 30th; time.Parse would normalise
		// this into March rather than reject it — the round
		// trip in ValidateDue is what catches that
		"03/05/2026", // wrong format entirely
		"2026-3-5",   // not zero-padded
	} {
		if err := notes.ValidateDue(due); !errors.Is(err, notes.ErrInvalid) {
			t.Errorf("ValidateDue(%q) = %v; want ErrInvalid", due, err)
		}
	}
}

func TestMigrationsApply(t *testing.T) {
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
	appSchema, err := db.Collect(notes.ID, notes.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Apply(ctx, handle, append(platform, appSchema...)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var name string
	err = handle.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'notes_nodes'`).Scan(&name)
	if err != nil {
		t.Fatalf("notes_nodes was not created: %v", err)
	}
}
