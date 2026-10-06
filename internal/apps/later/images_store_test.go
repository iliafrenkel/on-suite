package later_test

import (
	"context"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
)

// A deleted user's articles and image links go by cascade, but the
// content-addressed image rows don't (#509); the sweep frees them, and only
// them.
func TestSweepOrphanImagesFreesADeletedUsersImages(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	carol, err := auth.NewStore(f.db).CreateUser(ctx, "carol", apptest.PasswordHash, false)
	if err != nil {
		t.Fatal(err)
	}
	save := func(userID int64, url string, images map[string]string) {
		t.Helper()
		if _, _, err := f.store.Save(ctx, userID, later.NewArticle{
			URL: url, Title: "T", ContentHTML: "<p>body</p>", Images: images,
		}); err != nil {
			t.Fatal(err)
		}
	}
	save(f.alice.ID, "https://example.com/a", map[string]string{"shared": "https://example.com/s.png"})
	save(carol.ID, "https://example.com/c", map[string]string{
		"shared": "https://example.com/s.png",
		"own":    "https://example.com/o.png",
	})
	if err := auth.NewStore(f.db).DeleteUser(ctx, carol.ID); err != nil {
		t.Fatal(err)
	}

	n, err := f.store.SweepOrphanImages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("swept = %d, want 1", n)
	}
	if got := f.countRows(t, `SELECT count(*) FROM later_images WHERE hash = 'own'`); got != 0 {
		t.Error("carol's own image is still stored")
	}
	if got := f.countRows(t, `SELECT count(*) FROM later_images WHERE hash = 'shared'`); got != 1 {
		t.Error("the image alice still uses was swept")
	}
	if n, err := f.store.SweepOrphanImages(ctx); err != nil || n != 0 {
		t.Errorf("second sweep = %d, %v; want 0, nil", n, err)
	}
}
