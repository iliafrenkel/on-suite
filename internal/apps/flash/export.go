// internal/apps/flash/export.go
package flash

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ON Flash has two exports (#426), for two different jobs:
//
//   - DeckExport is one deck in exactly the JSON format Import accepts
//     (importJSON), so a deck downloaded from the deck pane can be imported
//     back or handed to someone as is. It is content only: no review
//     progress, since Import makes fresh cards anyway.
//   - App.Export joins onsuite export's whole-account JSON (app.Exporter),
//     like Notes, Paste and Reader: every deck with its settings, every card
//     with its review schedule, and the per-day review counts.
//
// Neither carries media bytes. A card's image or audio fetched from a URL
// keeps that URL; an uploaded file has none to give, so DeckExport drops it
// (Import takes only URLs) and App.Export flags it as uploaded. The data
// directory stays the real backup, as docs/self-hosting/deploying.md says.

// deckFile is the in-app export's shape, importJSON's own field for field.
type deckFile struct {
	Deck struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	} `json:"deck"`
	Cards []deckFileCard `json:"cards"`
}

type deckFileCard struct {
	Type  string   `json:"type"`
	Front string   `json:"front"`
	Back  string   `json:"back,omitempty"`
	Notes string   `json:"notes,omitempty"`
	Tags  []string `json:"tags,omitempty"`
	Image string   `json:"image,omitempty"`
	Audio string   `json:"audio,omitempty"`
}

// exportCard is one card as both exports read it: the card row plus, for
// each media kind, whether it has one and the URL it came from ("" for an
// upload).
type exportCard struct {
	ID                 int64
	CardType           string
	Front, Back, Notes string
	CreatedAt          time.Time
	HasImage, HasAudio bool
	ImageURL, AudioURL string
	Tags               []string
}

// exportCards reads one of userID's decks' cards oldest first, so an
// imported copy keeps the original order, with each card's media URLs
// and tags. Two queries, each read to the end before the next starts:
// internal/platform/db sets SetMaxOpenConns(1).
func (st *Store) exportCards(ctx context.Context, userID, deckID int64) ([]exportCard, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT c.id, c.card_type, c.front, c.back, c.notes, c.created_at,
		       c.image_hash IS NOT NULL, coalesce(im.source_url, ''),
		       c.audio_hash IS NOT NULL, coalesce(au.source_url, '')
		  FROM flash_cards c
		  LEFT JOIN flash_media im ON im.hash = c.image_hash
		  LEFT JOIN flash_media au ON au.hash = c.audio_hash
		 WHERE c.user_id = ? AND c.deck_id = ?
		 ORDER BY c.created_at, c.id`, userID, deckID)
	if err != nil {
		return nil, fmt.Errorf("flash: export cards: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []exportCard
	for rows.Next() {
		var c exportCard
		var created string
		if err := rows.Scan(&c.ID, &c.CardType, &c.Front, &c.Back, &c.Notes, &created,
			&c.HasImage, &c.ImageURL, &c.HasAudio, &c.AudioURL); err != nil {
			return nil, fmt.Errorf("flash: scan export card: %w", err)
		}
		if c.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flash: iterate export cards: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("flash: close export cards: %w", err)
	}

	tags, err := st.CardTagsInDeck(ctx, userID, deckID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Tags = tags[out[i].ID]
	}
	return out, nil
}

// DeckExport is one of userID's decks as an Import-format file. Somebody
// else's deck is ErrNotFound, via DeckByID.
func (st *Store) DeckExport(ctx context.Context, userID, deckID int64) (deckFile, error) {
	d, err := st.DeckByID(ctx, userID, deckID)
	if err != nil {
		return deckFile{}, err
	}
	cards, err := st.exportCards(ctx, userID, deckID)
	if err != nil {
		return deckFile{}, err
	}
	var out deckFile
	out.Deck.Name, out.Deck.Description = d.Name, d.Description
	out.Cards = make([]deckFileCard, 0, len(cards))
	for _, c := range cards {
		out.Cards = append(out.Cards, deckFileCard{
			Type: c.CardType, Front: c.Front, Back: c.Back, Notes: c.Notes, Tags: c.Tags,
			Image: c.ImageURL, Audio: c.AudioURL,
		})
	}
	return out, nil
}

// backupPayload is App.Export's shape: onsuite export's "flash" key.
type backupPayload struct {
	Decks []backupDeck `json:"decks"`
}

type backupDeck struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Color          string     `json:"color"`
	NewCardsPerDay int        `json:"new_cards_per_day"`
	ReviewsPerDay  *int       `json:"reviews_per_day"` // null = unlimited
	SnoozedUntil   *time.Time `json:"snoozed_until,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`

	Cards      []backupCard      `json:"cards"`
	ReviewDays []backupReviewDay `json:"review_days"`
}

type backupCard struct {
	ID            int64           `json:"id"`
	Type          string          `json:"type"`
	Front         string          `json:"front"`
	Back          string          `json:"back"`
	Notes         string          `json:"notes"`
	Tags          []string        `json:"tags"`
	ImageURL      string          `json:"image_url,omitempty"`
	ImageUploaded bool            `json:"image_uploaded,omitempty"`
	AudioURL      string          `json:"audio_url,omitempty"`
	AudioUploaded bool            `json:"audio_uploaded,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	Schedule      *backupSchedule `json:"schedule,omitempty"` // nil = never reviewed
}

// backupSchedule is a card's FSRS state (cardSchedule). The one-step undo
// buffer beside it in flash_card_state is left out: it only matters for the
// next few seconds after a grade.
type backupSchedule struct {
	State          string     `json:"state"`
	DueAt          time.Time  `json:"due_at"`
	Stability      float64    `json:"stability"`
	Difficulty     float64    `json:"difficulty"`
	ScheduledDays  uint64     `json:"scheduled_days"`
	Reps           uint64     `json:"reps"`
	Lapses         uint64     `json:"lapses"`
	RemainingSteps int        `json:"remaining_steps"`
	LastReviewAt   *time.Time `json:"last_review_at,omitempty"`
}

// backupReviewDay is one flash_review_counts row: what the streak, the daily
// limits and the Reviews-per-day chart are built from. Day is formatDay's
// calendar date.
type backupReviewDay struct {
	Day    string `json:"day"`
	New    int    `json:"new"`
	Review int    `json:"review"`
	Again  int    `json:"again"`
	Hard   int    `json:"hard"`
	Good   int    `json:"good"`
	Easy   int    `json:"easy"`
}

// Export implements app.Exporter. It takes the database directly, like every
// Exporter, so onsuite export works without the HTTP stack.
func (a *App) Export(ctx context.Context, handle *sql.DB, userID int64) (any, error) {
	st := NewStore(handle)
	decks, err := st.ListDecks(ctx, userID)
	if err != nil {
		return nil, err
	}
	schedules, err := st.exportSchedules(ctx, userID)
	if err != nil {
		return nil, err
	}
	reviewDays, err := st.exportReviewDays(ctx, userID)
	if err != nil {
		return nil, err
	}

	out := backupPayload{Decks: make([]backupDeck, 0, len(decks))}
	for _, d := range decks {
		cards, err := st.exportCards(ctx, userID, d.ID)
		if err != nil {
			return nil, err
		}
		bd := backupDeck{
			ID: d.ID, Name: d.Name, Description: d.Description, Color: d.Color,
			NewCardsPerDay: d.NewCardsPerDay, ReviewsPerDay: d.ReviewsPerDay,
			SnoozedUntil: d.SnoozedUntil, CreatedAt: d.CreatedAt,
			Cards:      make([]backupCard, 0, len(cards)),
			ReviewDays: reviewDays[d.ID],
		}
		if bd.ReviewDays == nil {
			bd.ReviewDays = []backupReviewDay{}
		}
		for _, c := range cards {
			tags := c.Tags
			if tags == nil {
				tags = []string{}
			}
			bd.Cards = append(bd.Cards, backupCard{
				ID: c.ID, Type: c.CardType, Front: c.Front, Back: c.Back, Notes: c.Notes, Tags: tags,
				ImageURL: c.ImageURL, ImageUploaded: c.HasImage && c.ImageURL == "",
				AudioURL: c.AudioURL, AudioUploaded: c.HasAudio && c.AudioURL == "",
				CreatedAt: c.CreatedAt, Schedule: schedules[c.ID],
			})
		}
		out.Decks = append(out.Decks, bd)
	}
	return out, nil
}

// exportSchedules is every reviewed card's schedule for userID, by card id.
func (st *Store) exportSchedules(ctx context.Context, userID int64) (map[int64]*backupSchedule, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT card_id, state, due_at, stability, difficulty, scheduled_days, reps, lapses,
		       remaining_steps, last_review_at
		  FROM flash_card_state WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("flash: export schedules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[int64]*backupSchedule{}
	for rows.Next() {
		var (
			cardID       int64
			s            backupSchedule
			due          string
			lastReviewAt sql.NullString
		)
		if err := rows.Scan(&cardID, &s.State, &due, &s.Stability, &s.Difficulty, &s.ScheduledDays,
			&s.Reps, &s.Lapses, &s.RemainingSteps, &lastReviewAt); err != nil {
			return nil, fmt.Errorf("flash: scan export schedule: %w", err)
		}
		if s.DueAt, err = parseTime(due); err != nil {
			return nil, err
		}
		if lastReviewAt.Valid {
			t, err := parseTime(lastReviewAt.String)
			if err != nil {
				return nil, err
			}
			s.LastReviewAt = &t
		}
		out[cardID] = &s
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flash: iterate export schedules: %w", err)
	}
	return out, nil
}

// exportReviewDays is userID's flash_review_counts, oldest day first, by
// deck id.
func (st *Store) exportReviewDays(ctx context.Context, userID int64) (map[int64][]backupReviewDay, error) {
	rows, err := st.db.QueryContext(ctx, `
		SELECT deck_id, day, new_count, review_count, again_count, hard_count, good_count, easy_count
		  FROM flash_review_counts WHERE user_id = ?
		 ORDER BY deck_id, day`, userID)
	if err != nil {
		return nil, fmt.Errorf("flash: export review days: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[int64][]backupReviewDay{}
	for rows.Next() {
		var deckID int64
		var d backupReviewDay
		if err := rows.Scan(&deckID, &d.Day, &d.New, &d.Review, &d.Again, &d.Hard, &d.Good, &d.Easy); err != nil {
			return nil, fmt.Errorf("flash: scan export review day: %w", err)
		}
		out[deckID] = append(out[deckID], d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flash: iterate export review days: %w", err)
	}
	return out, nil
}
