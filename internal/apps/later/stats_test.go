package later_test

import (
	"context"
	"slices"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

const storedImagesHint = "pictures kept with saved articles, for everyone"

func TestStatsCountEveryonesArticles(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	pic, _, err := f.store.Save(ctx, f.alice.ID, later.NewArticle{
		URL: "https://a.example/pic", Title: "Pic", ContentHTML: "<p>body</p>",
		Images: map[string]string{"h1": "https://a.example/1.png", "h2": "https://a.example/2.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveImageBytes(ctx, "h1", "image/png", make([]byte, 2048)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddHighlight(ctx, pic.ID, pic.ContentText, 0, 4, "body", ""); err != nil {
		t.Fatal(err)
	}
	reading := f.save(t, "https://a.example/reading", "Reading")
	if err := f.store.MarkOpened(ctx, f.alice.ID, reading.ID); err != nil {
		t.Fatal(err)
	}
	done := f.save(t, "https://a.example/done", "Done")
	if err := f.store.SetState(ctx, f.alice.ID, done.ID, later.StateArchived); err != nil {
		t.Fatal(err)
	}
	bobs, _, err := f.store.Save(ctx, f.bob.ID, later.NewArticle{URL: "https://b.example/1", Title: "Bob's", ContentHTML: "<p>body</p>"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AddHighlight(ctx, bobs.ID, bobs.ContentText, 0, 4, "body", "Mine."); err != nil {
		t.Fatal(err)
	}

	got, err := later.New().Stats(ctx, f.db)
	if err != nil {
		t.Fatal(err)
	}
	want := []app.Stat{
		{Label: "Articles", Value: "4"},
		{Label: "Unread", Value: "2"},
		{Label: "Reading", Value: "1"},
		{Label: "Archived", Value: "1"},
		{Label: "Highlights", Value: "2"},
		{Label: "Stored images", Value: "2.0 KiB", Hint: storedImagesHint},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Stats =\n%v\nwant\n%v", got, want)
	}
}

func TestStatsOfAnEmptyInstall(t *testing.T) {
	f := newFixture(t)
	got, err := f.store.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []app.Stat{
		{Label: "Articles", Value: "0"},
		{Label: "Unread", Value: "0"},
		{Label: "Reading", Value: "0"},
		{Label: "Archived", Value: "0"},
		{Label: "Highlights", Value: "0"},
		{Label: "Stored images", Value: "0 B", Hint: storedImagesHint},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Stats =\n%v\nwant\n%v", got, want)
	}
}
