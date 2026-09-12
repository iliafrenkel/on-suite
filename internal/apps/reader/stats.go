package reader

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// dayFormat is the key format for reader_daily_stats. Date-only and UTC, so
// lexical order is chronological order and a day means the same thing in
// January and July.
const dayFormat = "2006-01-02"

// DayStat is one day's numbers for one user.
type DayStat struct {
	Day     time.Time
	Fetched int
	Read    int
	Backlog int
	// Reconstructed is true for a row derived from surviving articles rather
	// than measured on the day.
	Reconstructed bool
}

// RecordDailyStats writes today's row for every user.
//
// It must run *before* the purge in the same job: retention deletes the
// articles a later chart would need, which is the entire reason this table
// exists.
func (s *Store) RecordDailyStats(ctx context.Context, day time.Time) error {
	key := day.UTC().Format(dayFormat)

	// One statement per user rather than a single grouped INSERT, because the
	// backlog figure is UnreadCounts' own predicate and duplicating that SQL
	// here is how the two quietly stop agreeing.
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT user_id FROM reader_subs`)
	if err != nil {
		return fmt.Errorf("reader: list users for stats: %w", err)
	}
	var userIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("reader: scan user for stats: %w", err)
		}
		userIDs = append(userIDs, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("reader: iterate users for stats: %w", err)
	}
	// Closed before the writes: internal/platform/db sets SetMaxOpenConns(1),
	// so an open *sql.Rows holds the only connection and the first write would
	// deadlock against it.
	if err := rows.Close(); err != nil {
		return fmt.Errorf("reader: close users for stats: %w", err)
	}

	for _, userID := range userIDs {
		counts, err := s.UnreadCounts(ctx, userID)
		if err != nil {
			return err
		}

		var fetched, read int
		if err := s.db.QueryRowContext(ctx, `
			SELECT count(*)
			  FROM reader_items i
			  JOIN reader_subs sub ON sub.feed_id = i.feed_id AND sub.user_id = ?
			 WHERE i.fetched_at >= sub.added_at AND date(i.fetched_at) = ?`,
			userID, key).Scan(&fetched); err != nil {
			return fmt.Errorf("reader: count fetched for stats: %w", err)
		}
		if err := s.db.QueryRowContext(ctx, `
			SELECT count(*) FROM reader_item_state
			 WHERE user_id = ? AND read_at IS NOT NULL AND date(read_at) = ?`,
			userID, key).Scan(&read); err != nil {
			return fmt.Errorf("reader: count read for stats: %w", err)
		}

		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO reader_daily_stats (day, user_id, fetched, read, backlog, reconstructed)
			VALUES (?, ?, ?, ?, ?, 0)
			ON CONFLICT (day, user_id) DO UPDATE SET
				fetched = excluded.fetched,
				read = excluded.read,
				backlog = excluded.backlog,
				reconstructed = 0`,
			key, userID, fetched, read, counts.Total); err != nil {
			return fmt.Errorf("reader: record daily stats: %w", err)
		}
	}
	return nil
}

// BackfillDailyStats reconstructs history from the articles that still exist,
// so the page is useful on the day it ships rather than in two months.
//
// It writes only days that have no row yet, so a measured day is never
// replaced by a reconstruction, and everything it writes is marked
// reconstructed. Backlog is deliberately left at zero for these rows: it
// cannot be reconstructed even approximately once retention has removed the
// read articles, and a plausible-looking wrong number is worse than an absent
// one.
func (s *Store) BackfillDailyStats(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO reader_daily_stats (day, user_id, fetched, read, backlog, reconstructed)
		SELECT d.day, d.user_id, sum(d.fetched), sum(d.read), 0, 1
		  FROM (
			SELECT date(i.fetched_at) AS day, sub.user_id AS user_id,
			       count(*) AS fetched, 0 AS read
			  FROM reader_items i
			  JOIN reader_subs sub ON sub.feed_id = i.feed_id
			 WHERE i.fetched_at >= sub.added_at
			 GROUP BY date(i.fetched_at), sub.user_id
			UNION ALL
			SELECT date(st.read_at) AS day, st.user_id AS user_id, 0 AS fetched, count(*) AS read
			  FROM reader_item_state st
			 WHERE st.read_at IS NOT NULL
			 GROUP BY date(st.read_at), st.user_id
		  ) AS d
		 WHERE NOT EXISTS (
			SELECT 1 FROM reader_daily_stats existing
			 WHERE existing.day = d.day AND existing.user_id = d.user_id)
		 GROUP BY d.day, d.user_id`)
	if err != nil {
		return 0, fmt.Errorf("reader: backfill daily stats: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("reader: backfill rows: %w", err)
	}
	return int(n), nil
}

// DailyStats returns the last n days, oldest first, including days with no row
// at all.
//
// Density matters: a bar chart that omits quiet days compresses its x-axis and
// shows a busier reading habit than the real one.
func (s *Store) DailyStats(ctx context.Context, userID int64, days int) ([]DayStat, error) {
	if days <= 0 {
		return nil, nil
	}
	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -(days - 1))

	rows, err := s.db.QueryContext(ctx, `
		SELECT day, fetched, read, backlog, reconstructed
		  FROM reader_daily_stats
		 WHERE user_id = ? AND day >= ? AND day <= ?`,
		userID, start.Format(dayFormat), end.Format(dayFormat))
	if err != nil {
		return nil, fmt.Errorf("reader: load daily stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byDay := map[string]DayStat{}
	for rows.Next() {
		var key string
		var d DayStat
		if err := rows.Scan(&key, &d.Fetched, &d.Read, &d.Backlog, &d.Reconstructed); err != nil {
			return nil, fmt.Errorf("reader: scan daily stat: %w", err)
		}
		byDay[key] = d
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reader: iterate daily stats: %w", err)
	}

	out := make([]DayStat, 0, days)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		stat := byDay[d.Format(dayFormat)]
		stat.Day = d
		out = append(out, stat)
	}
	return out, nil
}

// Stats describes the installation for the admin page.
//
// These are deliberately operator questions, not reader questions: how much is
// stored, how much work the poller is doing, and whether anything is broken.
// What someone reads is on the reading-stats page instead.
func (s *Store) Stats(ctx context.Context) ([]app.Stat, error) {
	var feeds, subs, items, failing, cachedImages int
	var lastPoll sql.NullString

	for _, q := range []struct {
		sql  string
		dest any
	}{
		{`SELECT count(*) FROM reader_feeds`, &feeds},
		{`SELECT count(*) FROM reader_subs`, &subs},
		{`SELECT count(*) FROM reader_items`, &items},
		{`SELECT count(*) FROM reader_feeds WHERE error_count > 0`, &failing},
		{`SELECT count(*) FROM reader_images WHERE bytes IS NOT NULL`, &cachedImages},
		{`SELECT max(last_fetch_at) FROM reader_feeds`, &lastPoll},
	} {
		if err := s.db.QueryRowContext(ctx, q.sql).Scan(q.dest); err != nil {
			return nil, fmt.Errorf("reader: stats query: %w", err)
		}
	}

	out := []app.Stat{
		{Label: "Feeds", Value: strconv.Itoa(feeds), Hint: "distinct feed URLs, polled once each however many people subscribe"},
		{Label: "Subscriptions", Value: strconv.Itoa(subs)},
		{Label: "Articles", Value: strconv.Itoa(items)},
		{Label: "Cached images", Value: strconv.Itoa(cachedImages)},
	}
	if lastPoll.Valid {
		out = append(out, app.Stat{Label: "Last poll", Value: humanTime(parseTime(lastPoll.String))})
	}
	// Only shown when it is non-zero: a permanent "Failing feeds: 0" teaches
	// people to stop reading the line, which is the opposite of what it is for.
	if failing > 0 {
		out = append(out, app.Stat{
			Label: "Failing feeds",
			Value: strconv.Itoa(failing),
			Hint:  "these are backing off; see the warning marks in the tree",
		})
	}
	return out, nil
}

// FeedStat is one subscription's numbers, for the per-feed table.
type FeedStat struct {
	SubID    int64
	Name     string
	FeedURL  string
	Articles int
	Read     int
	// LastArticle is the newest article still stored for this feed, zero if
	// none remain.
	LastArticle time.Time
	// AddedAt is when this user subscribed, so a feed too young to judge is
	// not called dead.
	AddedAt time.Time
	Failing bool
}

// ReadRatio is the fraction read, 0 when there is nothing to read.
func (f FeedStat) ReadRatio() float64 {
	if f.Articles == 0 {
		return 0
	}
	return float64(f.Read) / float64(f.Articles)
}

// ReadPercent is ReadRatio as a whole number, for display and for a bar width.
func (f FeedStat) ReadPercent() int { return int(f.ReadRatio()*100 + 0.5) }

// Quiet reports a feed that has published nothing recently — usually dead.
//
// It is judged on the newest article still stored, so a feed whose articles
// have all been purged reads as quiet. That is the right answer: retention only
// removes articles older than RetentionAge, so if nothing newer survives,
// nothing newer arrived.
//
// A subscription younger than the threshold is never called quiet, whatever it
// has published. A feed added last week has not had time to be dead, and
// telling someone to prune the feed they just added is the fastest way to make
// them stop trusting the page.
func (f FeedStat) Quiet() bool {
	if time.Since(f.AddedAt) < QuietAfter {
		return false
	}
	return f.LastArticle.IsZero() || time.Since(f.LastArticle) > QuietAfter
}

// QuietAfter is how long a feed must go without publishing to be called quiet.
const QuietAfter = 60 * 24 * time.Hour

// Neglected reports a feed that produces a lot and is read very little — the
// other prune candidate, and the one people are usually surprised by.
func (f FeedStat) Neglected() bool {
	return f.Articles >= NeglectedMinArticles && f.ReadRatio() < NeglectedRatio
}

const (
	NeglectedMinArticles = 20
	NeglectedRatio       = 0.1
)

// FeedStats returns one row per subscription, busiest first.
func (s *Store) FeedStats(ctx context.Context, userID int64) ([]FeedStat, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sub.id,
		       coalesce(nullif(sub.title, ''), nullif(f.title, ''), f.url),
		       f.url,
		       count(i.id),
		       count(st.read_at),
		       coalesce(max(i.published_at), ''),
		       sub.added_at,
		       f.error_count > 0
		  FROM reader_subs sub
		  JOIN reader_feeds f ON f.id = sub.feed_id
		  LEFT JOIN reader_items i
		         ON i.feed_id = sub.feed_id AND i.fetched_at >= sub.added_at
		  LEFT JOIN reader_item_state st
		         ON st.item_id = i.id AND st.user_id = sub.user_id
		 WHERE sub.user_id = ?
		 GROUP BY sub.id
		 ORDER BY count(i.id) DESC, 2`, userID)
	if err != nil {
		return nil, fmt.Errorf("reader: feed stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []FeedStat
	for rows.Next() {
		var f FeedStat
		var last, added string
		if err := rows.Scan(&f.SubID, &f.Name, &f.FeedURL, &f.Articles, &f.Read,
			&last, &added, &f.Failing); err != nil {
			return nil, fmt.Errorf("reader: scan feed stat: %w", err)
		}
		if last != "" {
			f.LastArticle = parseTime(last)
		}
		f.AddedAt = parseTime(added)
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reader: iterate feed stats: %w", err)
	}
	return out, nil
}
