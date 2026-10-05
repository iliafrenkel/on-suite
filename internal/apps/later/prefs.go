package later

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// Prefs are one reader's Aa settings.
type Prefs struct {
	Font  string
	Size  int
	Width string
}

// DefaultPrefs apply until the reader changes something.
var DefaultPrefs = Prefs{Font: "serif", Size: 3, Width: "medium"}

// Valid reports whether every field is one the reading view has a class for.
func (p Prefs) Valid() bool {
	return (p.Font == "serif" || p.Font == "sans") &&
		p.Size >= 1 && p.Size <= 5 &&
		(p.Width == "narrow" || p.Width == "medium" || p.Width == "wide")
}

func (st *Store) Prefs(ctx context.Context, userID int64) (Prefs, error) {
	var p Prefs
	err := st.db.QueryRowContext(ctx,
		`SELECT font, size, width FROM later_prefs WHERE user_id = ?`, userID).
		Scan(&p.Font, &p.Size, &p.Width)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultPrefs, nil
	}
	if err != nil {
		return Prefs{}, fmt.Errorf("later: load prefs: %w", err)
	}
	return p, nil
}

func (st *Store) SetPrefs(ctx context.Context, userID int64, p Prefs) error {
	if !p.Valid() {
		return ErrInvalid
	}
	_, err := st.db.ExecContext(ctx, `
		INSERT INTO later_prefs (user_id, font, size, width, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET font = excluded.font, size = excluded.size,
			width = excluded.width, updated_at = excluded.updated_at`,
		userID, p.Font, p.Size, p.Width, db.FormatTime(st.now()))
	if err != nil {
		return fmt.Errorf("later: save prefs: %w", err)
	}
	return nil
}
