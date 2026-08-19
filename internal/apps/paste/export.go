package paste

import (
	"context"
	"database/sql"
	"time"
)

// exportedSnippet is the on-disk shape of a snippet.
//
// It deliberately omits share_slug. A slug is a credential — anyone holding it
// can read the snippet — and an export is a portable file that may be copied
// or emailed. Whether a snippet was shared is recorded; the secret that shares
// it is not. Use onsuite backup for a restorable copy.
type exportedSnippet struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Language  string    `json:"language"`
	Body      string    `json:"body"`
	Shared    bool      `json:"shared"`
	CreatedAt time.Time `json:"created_at"`
}

type exportPayload struct {
	Snippets []exportedSnippet `json:"snippets"`
}

// Export implements app.Exporter.
func (a *App) Export(ctx context.Context, handle *sql.DB, userID int64) (any, error) {
	snippets, err := NewStore(handle).All(ctx, userID)
	if err != nil {
		return nil, err
	}

	out := exportPayload{Snippets: make([]exportedSnippet, 0, len(snippets))}
	for _, s := range snippets {
		out.Snippets = append(out.Snippets, exportedSnippet{
			ID:        s.ID,
			Title:     s.Title,
			Language:  s.Language,
			Body:      s.Body,
			Shared:    s.Shared(),
			CreatedAt: s.CreatedAt,
		})
	}
	return out, nil
}
