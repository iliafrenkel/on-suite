package later_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
	"github.com/iliafrenkel/on-suite/internal/platform/db"
)

// fixture is a migrated database with two users and a clock the test moves.
type fixture struct {
	store *later.Store
	db    *sql.DB
	alice auth.User
	bob   auth.User
	now   time.Time
}

func newFixture(t *testing.T) *fixture {
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
	appMigrations, err := db.Collect(later.ID, later.Migrations())
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
	bob, err := users.CreateUser(ctx, "bob", apptest.PasswordHash, false)
	if err != nil {
		t.Fatal(err)
	}
	st := later.NewStore(handle)
	f := &fixture{store: st, db: handle, alice: alice, bob: bob, now: time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)}
	st.SetClock(func() time.Time { return f.now })
	return f
}

func (f *fixture) tick() { f.now = f.now.Add(time.Minute) }

// save stores a simple extracted article for alice.
func (f *fixture) save(t *testing.T, url, title string) later.Article {
	t.Helper()
	a, _, err := f.store.Save(context.Background(), f.alice.ID, later.NewArticle{URL: url, Title: title, ContentHTML: "<p>body</p>"})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func ids(items []later.ListItem) []int64 {
	var out []int64
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func sameIDs(got []later.ListItem, want ...int64) bool {
	g := ids(got)
	if len(g) != len(want) {
		return false
	}
	for i := range g {
		if g[i] != want[i] {
			return false
		}
	}
	return true
}

const (
	hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hashC = "cccccccccccccccccccccccccccccccc"
)

func TestSaveStoresAnExtractedArticle(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, created, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{
		URL: "https://www.example.com/post", Title: "Post",
		ContentHTML: `<p>One two</p><p>three</p>`,
		Images:      map[string]string{hashA: "https://cdn.example/a.png"},
	})
	if err != nil || !created {
		t.Fatalf("Save = %v, created %v", err, created)
	}
	if a.Content != later.ContentExtracted || a.ContentText != "One twothree" || a.WordCount != 3 || a.SiteHost != "example.com" || a.State != later.StateUnread {
		t.Errorf("article = %+v", a)
	}
	if a.ContentHTML != `<p>One two</p><p>three</p>` || a.Title != "Post" || a.UserID != f.alice.ID {
		t.Errorf("article = %+v", a)
	}
	if !a.SavedAt.Equal(f.now) || !a.UpdatedAt.Equal(f.now) || !a.OpenedAt.IsZero() || !a.ArchivedAt.IsZero() {
		t.Errorf("times = saved %v updated %v opened %v archived %v", a.SavedAt, a.UpdatedAt, a.OpenedAt, a.ArchivedAt)
	}
	img, err := f.store.ImageForUser(ctx, f.alice.ID, hashA)
	if err != nil {
		t.Fatalf("image not linked: %v", err)
	}
	if img.SrcURL != "https://cdn.example/a.png" || img.Cached() {
		t.Errorf("image = %+v", img)
	}
}

func TestSaveIsIdempotentPerUserAndURL(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first, _, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{URL: "https://example.com/a", Title: "First", ContentHTML: "<p>x</p>"})
	if err != nil {
		t.Fatal(err)
	}
	f.tick()
	again, created, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{URL: "https://example.com/a", Title: "Second", ContentHTML: "<p>y y</p>"})
	if err != nil || created {
		t.Fatalf("second Save = %v, created %v; want existing", err, created)
	}
	if again.ID != first.ID || again.Title != "First" || again.WordCount != 1 || !again.UpdatedAt.Equal(first.UpdatedAt) {
		t.Errorf("second = %+v, first = %+v; nothing may change", again, first)
	}
	theirs, created, err := f.store.Save(ctx, f.bob.ID, later.NewArticle{URL: "https://example.com/a", Title: "Bob's"})
	if err != nil || !created {
		t.Fatalf("bob Save = %v, created %v", err, created)
	}
	if theirs.ID == first.ID || theirs.UserID != f.bob.ID {
		t.Errorf("bob's article = %+v", theirs)
	}
}

func TestSaveWithoutContentIsLinkOnly(t *testing.T) {
	f := newFixture(t)
	a, created, err := f.store.Save(context.Background(), f.alice.ID, later.NewArticle{
		URL: "https://www.news.example.org/story", ExtractError: "fetch failed: 403",
	})
	if err != nil || !created {
		t.Fatalf("Save = %v, created %v", err, created)
	}
	if a.Content != later.ContentLinkOnly || a.ContentHTML != "" || a.ContentText != "" || a.WordCount != 0 {
		t.Errorf("article = %+v", a)
	}
	if a.ExtractError != "fetch failed: 403" {
		t.Errorf("ExtractError = %q", a.ExtractError)
	}
	if a.Title != "news.example.org" || a.SiteHost != "news.example.org" {
		t.Errorf("Title = %q, SiteHost = %q; title falls back to the host", a.Title, a.SiteHost)
	}
}

func TestListOrdersEachTabAndPages(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.save(t, "https://example.com/1", "One")
	f.tick()
	b := f.save(t, "https://example.com/2", "Two")
	f.tick()
	c := f.save(t, "https://example.com/3", "Three")
	f.tick()

	got, err := f.store.List(ctx, f.alice.ID, later.StateUnread, "", 0, 10)
	if err != nil || !sameIDs(got, c.ID, b.ID, a.ID) {
		t.Fatalf("unread = %v, %v; want newest first %v", ids(got), err, []int64{c.ID, b.ID, a.ID})
	}
	if got[0].Title != "Three" || got[0].SiteHost != "example.com" || got[0].Content != later.ContentExtracted || got[0].WordCount != 1 {
		t.Errorf("item = %+v", got[0])
	}
	if got, _ := f.store.List(ctx, f.alice.ID, later.StateUnread, "", 1, 1); !sameIDs(got, b.ID) {
		t.Errorf("offset 1 limit 1 = %v, want [%d]", ids(got), b.ID)
	}
	if got, _ := f.store.List(ctx, f.alice.ID, later.StateUnread, "", 2, 5); !sameIDs(got, a.ID) {
		t.Errorf("offset 2 = %v, want [%d]", ids(got), a.ID)
	}
	if _, err := f.store.List(ctx, f.alice.ID, "bogus", "", 0, 10); !errors.Is(err, later.ErrInvalid) {
		t.Errorf("bogus tab err = %v, want ErrInvalid", err)
	}

	// Reading is ordered by when each was opened, not when it was saved.
	for _, id := range []int64{b.ID, a.ID} {
		if err := f.store.MarkOpened(ctx, f.alice.ID, id); err != nil {
			t.Fatal(err)
		}
		f.tick()
	}
	if got, _ := f.store.List(ctx, f.alice.ID, later.StateReading, "", 0, 10); !sameIDs(got, a.ID, b.ID) {
		t.Errorf("reading = %v, want [%d %d]", ids(got), a.ID, b.ID)
	}
	if got, _ := f.store.List(ctx, f.alice.ID, later.StateUnread, "", 0, 10); !sameIDs(got, c.ID) {
		t.Errorf("unread = %v, want [%d]", ids(got), c.ID)
	}

	// Archived is ordered by when each was archived.
	for _, id := range []int64{a.ID, c.ID} {
		if err := f.store.SetState(ctx, f.alice.ID, id, later.StateArchived); err != nil {
			t.Fatal(err)
		}
		f.tick()
	}
	if got, _ := f.store.List(ctx, f.alice.ID, later.StateArchived, "", 0, 10); !sameIDs(got, c.ID, a.ID) {
		t.Errorf("archived = %v, want [%d %d]", ids(got), c.ID, a.ID)
	}

	// Ties on the sort column fall back to id DESC.
	d := f.save(t, "https://example.com/4", "Four")
	e := f.save(t, "https://example.com/5", "Five")
	if got, _ := f.store.List(ctx, f.alice.ID, later.StateUnread, "", 0, 10); !sameIDs(got, e.ID, d.ID) {
		t.Errorf("tied unread = %v, want [%d %d]", ids(got), e.ID, d.ID)
	}
	// Another user's list is empty.
	if got, _ := f.store.List(ctx, f.bob.ID, later.StateUnread, "", 0, 10); len(got) != 0 {
		t.Errorf("bob's list = %v", ids(got))
	}
}

func TestCountsByState(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.save(t, "https://example.com/1", "One")
	b := f.save(t, "https://example.com/2", "Two")
	c := f.save(t, "https://example.com/3", "Three")
	if err := f.store.MarkOpened(ctx, f.alice.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetState(ctx, f.alice.ID, c.ID, later.StateArchived); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.store.Save(ctx, f.bob.ID, later.NewArticle{URL: "https://example.com/1"}); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.Counts(ctx, f.alice.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if got[later.StateUnread] != 1 || got[later.StateReading] != 1 || got[later.StateArchived] != 1 {
		t.Errorf("counts = %v, want 1/1/1", got)
	}
	empty, err := newFixture(t).store.Counts(ctx, 999, "")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := empty[later.StateReading]; !ok || v != 0 || len(empty) != 3 {
		t.Errorf("empty counts = %v, want every state present at 0", empty)
	}
}

func TestMarkOpenedMovesUnreadToReadingOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.save(t, "https://example.com/1", "One")
	b := f.save(t, "https://example.com/2", "Two")
	opened := f.now.Add(time.Minute)
	f.now = opened
	if err := f.store.MarkOpened(ctx, f.alice.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.store.Article(ctx, f.alice.ID, a.ID)
	if got.State != later.StateReading || !got.OpenedAt.Equal(opened) || !got.UpdatedAt.Equal(opened) {
		t.Errorf("after open = %+v", got)
	}

	// Reading stays reading; opened_at moves.
	f.now = opened.Add(time.Hour)
	if err := f.store.MarkOpened(ctx, f.alice.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = f.store.Article(ctx, f.alice.ID, a.ID)
	if got.State != later.StateReading || !got.OpenedAt.Equal(f.now) {
		t.Errorf("reopened = %+v", got)
	}

	// Archived stays archived; opened_at still moves.
	if err := f.store.SetState(ctx, f.alice.ID, b.ID, later.StateArchived); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	if err := f.store.MarkOpened(ctx, f.alice.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = f.store.Article(ctx, f.alice.ID, b.ID)
	if got.State != later.StateArchived || !got.OpenedAt.Equal(f.now) || got.ArchivedAt.IsZero() {
		t.Errorf("archived then opened = %+v", got)
	}
}

func TestSetStateRules(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.save(t, "https://example.com/1", "One")
	if err := f.store.MarkOpened(ctx, f.alice.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	f.tick()
	if err := f.store.SetState(ctx, f.alice.ID, a.ID, later.StateArchived); err != nil {
		t.Fatal(err)
	}
	got, _ := f.store.Article(ctx, f.alice.ID, a.ID)
	if got.State != later.StateArchived || !got.ArchivedAt.Equal(f.now) || got.OpenedAt.IsZero() || !got.UpdatedAt.Equal(f.now) {
		t.Errorf("archived = %+v", got)
	}

	f.tick()
	if err := f.store.SetState(ctx, f.alice.ID, a.ID, later.StateUnread); err != nil {
		t.Fatal(err)
	}
	got, _ = f.store.Article(ctx, f.alice.ID, a.ID)
	if got.State != later.StateUnread || !got.ArchivedAt.IsZero() || !got.OpenedAt.IsZero() || !got.UpdatedAt.Equal(f.now) {
		t.Errorf("unread = %+v; archived_at and opened_at must be cleared", got)
	}

	for _, s := range []later.State{later.StateReading, "bogus"} {
		if err := f.store.SetState(ctx, f.alice.ID, a.ID, s); !errors.Is(err, later.ErrInvalid) {
			t.Errorf("SetState(%q) = %v, want ErrInvalid", s, err)
		}
	}
	got, _ = f.store.Article(ctx, f.alice.ID, a.ID)
	if got.State != later.StateUnread {
		t.Errorf("state after refused changes = %q", got.State)
	}
}

func TestSetPastedTextOnlyOnLinkOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	link, _, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{URL: "https://example.com/l", ExtractError: "blocked"})
	if err != nil {
		t.Fatal(err)
	}
	f.tick()
	if err := f.store.SetPastedText(ctx, f.alice.ID, link.ID, "First para here.\n\nSecond <b>para</b>."); err != nil {
		t.Fatal(err)
	}
	got, _ := f.store.Article(ctx, f.alice.ID, link.ID)
	wantHTML := "<p>First para here.</p><p>Second &lt;b&gt;para&lt;/b&gt;.</p>"
	if got.Content != later.ContentPasted || got.ContentHTML != wantHTML || got.ContentText != "First para here.Second <b>para</b>." || got.WordCount != 5 {
		t.Errorf("pasted = %+v", got)
	}
	if got.ExtractError != "" || !got.UpdatedAt.Equal(f.now) {
		t.Errorf("ExtractError = %q, UpdatedAt = %v", got.ExtractError, got.UpdatedAt)
	}
	// Already pasted: no second paste.
	if err := f.store.SetPastedText(ctx, f.alice.ID, link.ID, "again"); !errors.Is(err, later.ErrInvalid) {
		t.Errorf("paste over pasted = %v, want ErrInvalid", err)
	}

	ext := f.save(t, "https://example.com/e", "Ext")
	if err := f.store.SetPastedText(ctx, f.alice.ID, ext.ID, "text"); !errors.Is(err, later.ErrInvalid) {
		t.Errorf("paste over extracted = %v, want ErrInvalid", err)
	}
	if got, _ := f.store.Article(ctx, f.alice.ID, ext.ID); got.ContentHTML != "<p>body</p>" {
		t.Errorf("extracted article changed: %+v", got)
	}

	blank, _, _ := f.store.Save(ctx, f.alice.ID, later.NewArticle{URL: "https://example.com/b"})
	if err := f.store.SetPastedText(ctx, f.alice.ID, blank.ID, " \n\n \t "); !errors.Is(err, later.ErrInvalid) {
		t.Errorf("blank paste = %v, want ErrInvalid", err)
	}
	if got, _ := f.store.Article(ctx, f.alice.ID, blank.ID); got.Content != later.ContentLinkOnly {
		t.Errorf("blank paste changed content to %q", got.Content)
	}
}

func TestEveryMethodIsScopedToTheOwner(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, _, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{
		URL: "https://example.com/a", ContentHTML: "<p>x</p>", Images: map[string]string{hashA: "https://cdn.example/a.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	link, _, _ := f.store.Save(ctx, f.alice.ID, later.NewArticle{URL: "https://example.com/link"})

	if _, err := f.store.Article(ctx, f.bob.ID, a.ID); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("Article = %v, want ErrNotFound", err)
	}
	if _, err := f.store.ArticleByURL(ctx, f.bob.ID, "https://example.com/a"); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("ArticleByURL = %v, want ErrNotFound", err)
	}
	if err := f.store.MarkOpened(ctx, f.bob.ID, a.ID); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("MarkOpened = %v, want ErrNotFound", err)
	}
	if err := f.store.SetState(ctx, f.bob.ID, a.ID, later.StateArchived); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("SetState = %v, want ErrNotFound", err)
	}
	if err := f.store.SetPastedText(ctx, f.bob.ID, link.ID, "text"); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("SetPastedText = %v, want ErrNotFound", err)
	}
	if err := f.store.Delete(ctx, f.bob.ID, a.ID); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("Delete = %v, want ErrNotFound", err)
	}
	if _, err := f.store.ImageForUser(ctx, f.bob.ID, hashA); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("ImageForUser = %v, want ErrNotFound", err)
	}

	// None of that touched alice's article or image.
	got, err := f.store.Article(ctx, f.alice.ID, a.ID)
	if err != nil || got.State != later.StateUnread || got.Content != later.ContentExtracted {
		t.Errorf("alice's article = %+v, %v", got, err)
	}
	if got, _ := f.store.Article(ctx, f.alice.ID, link.ID); got.Content != later.ContentLinkOnly {
		t.Errorf("alice's link = %+v", got)
	}
	if _, err := f.store.ImageForUser(ctx, f.alice.ID, hashA); err != nil {
		t.Errorf("alice's image: %v", err)
	}
}

func TestDeleteKeepsSharedImagesAndRemovesOrphans(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first, _, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{
		URL: "https://example.com/1", ContentHTML: "<p>x</p>",
		Images: map[string]string{hashA: "https://cdn.example/shared.png", hashB: "https://cdn.example/only.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{
		URL: "https://example.com/2", ContentHTML: "<p>y</p>",
		Images: map[string]string{hashA: "https://cdn.example/shared.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Delete(ctx, f.alice.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Article(ctx, f.alice.ID, first.ID); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("deleted article err = %v, want ErrNotFound", err)
	}
	if _, err := f.store.ImageForUser(ctx, f.alice.ID, hashA); err != nil {
		t.Errorf("shared image gone: %v", err)
	}
	count := func(q string, args ...any) int {
		var n int
		if err := f.db.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM later_images WHERE hash = ?`, hashA); n != 1 {
		t.Errorf("shared image rows = %d, want 1", n)
	}
	if n := count(`SELECT count(*) FROM later_images WHERE hash = ?`, hashB); n != 0 {
		t.Errorf("orphan image rows = %d, want 0", n)
	}
	if n := count(`SELECT count(*) FROM later_article_images WHERE article_id = ?`, first.ID); n != 0 {
		t.Errorf("links of the deleted article = %d, want 0", n)
	}
	if n := count(`SELECT count(*) FROM later_article_images WHERE article_id = ?`, second.ID); n != 1 {
		t.Errorf("links of the kept article = %d, want 1", n)
	}
	// Deleting the last user removes the shared image too.
	if err := f.store.Delete(ctx, f.alice.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM later_images`); n != 0 {
		t.Errorf("images left = %d, want 0", n)
	}
}

func TestImageForUserSeesOnlyLinkedImages(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, u := range []int64{f.alice.ID, f.bob.ID} {
		if _, _, err := f.store.Save(ctx, u, later.NewArticle{
			URL: "https://example.com/s", ContentHTML: "<p>x</p>", Images: map[string]string{hashA: "https://cdn.example/a.png"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.store.ImageForUser(ctx, f.alice.ID, hashA); err != nil {
		t.Errorf("alice: %v", err)
	}
	if _, err := f.store.ImageForUser(ctx, f.bob.ID, hashA); err != nil {
		t.Errorf("bob: %v", err)
	}
	if _, err := f.store.ImageForUser(ctx, f.alice.ID, hashC); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("unknown hash = %v, want ErrNotFound", err)
	}
}

func TestSaveImageBytesAndFailure(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, _, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{
		URL: "https://example.com/s", ContentHTML: "<p>x</p>", Images: map[string]string{hashA: "https://cdn.example/a.png"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveImageFailure(ctx, hashA, "http 500"); err != nil {
		t.Fatal(err)
	}
	f.tick()
	if err := f.store.SaveImageFailure(ctx, hashA, "http 502"); err != nil {
		t.Fatal(err)
	}
	img, _ := f.store.ImageForUser(ctx, f.alice.ID, hashA)
	if img.ErrorCount != 2 || img.LastError != "http 502" || !img.FetchedAt.Equal(f.now) || img.Cached() {
		t.Errorf("after failures = %+v", img)
	}
	f.tick()
	if err := f.store.SaveImageBytes(ctx, hashA, "image/png", []byte("PNG")); err != nil {
		t.Fatal(err)
	}
	img, _ = f.store.ImageForUser(ctx, f.alice.ID, hashA)
	if !img.Cached() || string(img.Bytes) != "PNG" || img.ContentType != "image/png" || img.ErrorCount != 0 || img.LastError != "" || !img.FetchedAt.Equal(f.now) {
		t.Errorf("after bytes = %+v", img)
	}
	if err := f.store.SaveImageBytes(ctx, hashC, "image/png", []byte("x")); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("bytes for unknown hash = %v, want ErrNotFound", err)
	}
	if err := f.store.SaveImageFailure(ctx, hashC, "x"); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("failure for unknown hash = %v, want ErrNotFound", err)
	}
}

func TestImagesToFetchSkipsCachedAndGivenUp(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	hashes := map[string]string{}
	// One article per image, a minute apart, so "oldest saved first" is the
	// declaration order.
	order := []string{"cached", "dead", "recent", "old", "fresh"}
	for i, name := range order {
		h := strings.Repeat(string(rune('a'+i)), 32)
		hashes[name] = h
		if _, _, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{
			URL: "https://example.com/" + name, ContentHTML: "<p>x</p>", Images: map[string]string{h: "https://cdn.example/" + name},
		}); err != nil {
			t.Fatal(err)
		}
		f.tick()
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(f.store.SaveImageBytes(ctx, hashes["cached"], "image/png", []byte("x")))
	for range 3 {
		must(f.store.SaveImageFailure(ctx, hashes["dead"], "boom"))
	}
	must(f.store.SaveImageFailure(ctx, hashes["old"], "boom"))
	f.now = f.now.Add(2 * time.Hour)
	must(f.store.SaveImageFailure(ctx, hashes["recent"], "boom"))
	f.now = f.now.Add(5 * time.Minute)

	got, err := f.store.ImagesToFetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, img := range got {
		for n, h := range hashes {
			if h == img.Hash {
				names = append(names, n)
			}
		}
	}
	if strings.Join(names, ",") != "old,fresh" {
		t.Errorf("to fetch = %v, want [old fresh] in that order", names)
	}
	if len(got) == 2 && got[0].SrcURL != "https://cdn.example/old" {
		t.Errorf("first = %+v", got[0])
	}
	if got, _ := f.store.ImagesToFetch(ctx, 1); len(got) != 1 || got[0].Hash != hashes["old"] {
		t.Errorf("limit 1 = %+v", got)
	}
}

func TestListCountsHighlights(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	two := f.saveDoc(t, f.alice.ID, "https://a.example/two", helloHTML)
	none := f.saveDoc(t, f.alice.ID, "https://a.example/none", helloHTML)
	if _, err := f.store.AddHighlight(ctx, two.ID, two.ContentText, 6, 11, "brave", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddHighlight(ctx, two.ID, two.ContentText, 16, 21, "world", "c"); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.List(ctx, f.alice.ID, later.StateUnread, "", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[int64]int{}
	for _, it := range got {
		counts[it.ID] = it.Highlights
	}
	if len(counts) != 2 || counts[two.ID] != 2 || counts[none.ID] != 0 {
		t.Errorf("highlight counts = %v, want %d:2 and %d:0", counts, two.ID, none.ID)
	}
}

// Save is one transaction: an image that can't be written takes the
// article, and the images already written, down with it.
func TestSaveRollsBackWhenAnImageWriteFails(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.db.Exec(`
		CREATE TRIGGER fail_second_image BEFORE INSERT ON later_article_images
		WHEN (SELECT count(*) FROM later_article_images) >= 1
		BEGIN SELECT RAISE(ABORT, 'disk full'); END`); err != nil {
		t.Fatal(err)
	}
	_, _, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{
		URL: "https://example.com/p", Title: "T", ContentHTML: "<p>body</p>",
		Images: map[string]string{"h1": "https://example.com/1.png", "h2": "https://example.com/2.png"},
	})
	if err == nil {
		t.Fatal("Save succeeded despite a failing image write")
	}
	for _, table := range []string{"later_articles", "later_images", "later_article_images"} {
		if n := f.countRows(t, `SELECT count(*) FROM `+table); n != 0 {
			t.Errorf("%s has %d rows after the failed save, want 0", table, n)
		}
	}
}
