package later_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
)

func TestParseTags(t *testing.T) {
	long := strings.Repeat("x", 50)
	var many []string
	for i := 0; i < 25; i++ {
		many = append(many, string(rune('a'+i)))
	}
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"Essays, AI", []string{"essays", "ai"}},
		{" #go ,  go,GO ", []string{"go"}},
		{"to   cook", []string{"to cook"}},
		{"", nil},
		{" , ,", nil},
		{"##", nil},
		{"a\x1fb", []string{"a b"}},
		{long, []string{strings.Repeat("x", later.MaxTagRunes)}},
		{"Ünïcode, РУССКИЙ, עברית", []string{"ünïcode", "русский", "עברית"}},
		{strings.Join(many, ","), many[:later.MaxTags]},
	} {
		if got := later.ParseTags(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("ParseTags(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// saveTagged stores an article for userID with tags.
func (f *fixture) saveTagged(t *testing.T, userID int64, url string, tags ...string) later.Article {
	t.Helper()
	a, _, err := f.store.Save(context.Background(), userID, later.NewArticle{
		URL: url, Title: url, ContentHTML: "<p>body</p>", Tags: tags,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (f *fixture) articleTags(t *testing.T, userID, id int64) []string {
	t.Helper()
	got, err := f.store.ArticleTags(context.Background(), userID, id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (f *fixture) tagNames(t *testing.T, userID int64) []string {
	t.Helper()
	got, err := f.store.TagNames(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSaveLinksTags(t *testing.T) {
	f := newFixture(t)
	a := f.saveTagged(t, f.alice.ID, "https://a.example/1", "Essays", "ai", "essays")
	if got := f.articleTags(t, f.alice.ID, a.ID); !slices.Equal(got, []string{"ai", "essays"}) {
		t.Errorf("ArticleTags = %q, want [ai essays]", got)
	}
	if got := f.tagNames(t, f.alice.ID); !slices.Equal(got, []string{"ai", "essays"}) {
		t.Errorf("TagNames = %q, want [ai essays]", got)
	}
}

func TestSetTagsReplacesAndRemovesUnusedTags(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a1 := f.saveTagged(t, f.alice.ID, "https://a.example/1", "x", "y")
	f.saveTagged(t, f.alice.ID, "https://a.example/2", "y")

	if err := f.store.SetTags(ctx, f.alice.ID, a1.ID, []string{"Z"}); err != nil {
		t.Fatal(err)
	}
	if got := f.articleTags(t, f.alice.ID, a1.ID); !slices.Equal(got, []string{"z"}) {
		t.Errorf("ArticleTags = %q, want [z]", got)
	}
	// x lost its last article; y is still used by the second one.
	if got := f.tagNames(t, f.alice.ID); !slices.Equal(got, []string{"y", "z"}) {
		t.Errorf("TagNames = %q, want [y z]", got)
	}

	if err := f.store.SetTags(ctx, f.alice.ID, a1.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.articleTags(t, f.alice.ID, a1.ID); len(got) != 0 {
		t.Errorf("ArticleTags after clearing = %q, want none", got)
	}
	if got := f.tagNames(t, f.alice.ID); !slices.Equal(got, []string{"y"}) {
		t.Errorf("TagNames = %q, want [y]", got)
	}
}

func TestTagsArePerUser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.saveTagged(t, f.alice.ID, "https://a.example/1", "shared", "alice-only")
	b := f.saveTagged(t, f.bob.ID, "https://a.example/1", "shared")

	if err := f.store.SetTags(ctx, f.bob.ID, a.ID, []string{"hijack"}); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("SetTags on someone else's article = %v, want ErrNotFound", err)
	}
	if got := f.articleTags(t, f.bob.ID, a.ID); len(got) != 0 {
		t.Errorf("ArticleTags for someone else's article = %q, want none", got)
	}
	if err := f.store.SetTags(ctx, f.bob.ID, b.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.tagNames(t, f.alice.ID); !slices.Equal(got, []string{"alice-only", "shared"}) {
		t.Errorf("alice's TagNames = %q, want her two tags untouched", got)
	}
	if got := f.tagNames(t, f.bob.ID); len(got) != 0 {
		t.Errorf("bob's TagNames = %q, want none", got)
	}
}

func TestSetTagsOnAMissingArticleIsNotFound(t *testing.T) {
	f := newFixture(t)
	if err := f.store.SetTags(context.Background(), f.alice.ID, 999, []string{"x"}); !errors.Is(err, later.ErrNotFound) {
		t.Errorf("SetTags = %v, want ErrNotFound", err)
	}
	if got := f.tagNames(t, f.alice.ID); len(got) != 0 {
		t.Errorf("TagNames = %q, want none created", got)
	}
}

func TestDeleteRemovesUnusedTags(t *testing.T) {
	f := newFixture(t)
	a1 := f.saveTagged(t, f.alice.ID, "https://a.example/1", "solo", "shared")
	f.saveTagged(t, f.alice.ID, "https://a.example/2", "shared")
	if err := f.store.Delete(context.Background(), f.alice.ID, a1.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.tagNames(t, f.alice.ID); !slices.Equal(got, []string{"shared"}) {
		t.Errorf("TagNames = %q, want [shared]", got)
	}
}
