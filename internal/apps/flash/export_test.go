package flash

import (
	"context"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/webfetch"
)

// AllowPrivateFetchesForTest lets this app's HTTP client reach loopback, so
// handler tests can point it at an httptest origin. Production never calls
// it; the real guard is what TestDefaultMediaClientRefusesPrivateAddresses
// exercises.
//
// It must be called after Mount, which is where a.mediaClient is built —
// apptest.NewServer has already mounted by the time it hands the App back.
//
// This lives in a _test.go file (the standard Go export_test.go idiom) so it
// is compiled into the test binary — where package flash_test can still call
// it, since Go links internal and external test files together — but never
// into the production binary.
func (a *App) AllowPrivateFetchesForTest() {
	a.mediaClient.DenyAddr = func(string) error { return nil }
}

// MaxMediaFetchAttemptsForTest and MediaRetryBackoffForTest are the
// thresholds the media handler applies via webfetch.GivenUp, so a test can
// assert against the real values rather than a hardcoded copy that could
// silently drift out of sync with them.
const (
	MaxMediaFetchAttemptsForTest = webfetch.MaxImageAttempts
	MediaRetryBackoffForTest     = webfetch.ImageRetryBackoff
)

// DeckSummariesPerDeckForTest is the reference DeckSummaries is checked
// against: ListDecks, then the single-deck path (deckSummary, the one
// QueueFront uses for a one-deck scope) once per deck. The batched
// DeckSummaries must return exactly this.
func (st *Store) DeckSummariesPerDeckForTest(ctx context.Context, userID int64, now time.Time) ([]DeckSummary, error) {
	decks, err := st.ListDecks(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]DeckSummary, 0, len(decks))
	for _, d := range decks {
		s, err := st.deckSummary(ctx, userID, d, now)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
