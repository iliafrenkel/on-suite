package later_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

func TestDownloadMarkdown(t *testing.T) {
	s := newServer(t)
	ctx, uid := context.Background(), s.Alice.User.ID
	art := seed(t, s, uid, later.NewArticle{
		URL: "https://essays.example.com/slow", Title: "Reading slowly",
		ContentHTML: "<p>The quick brown fox</p><p><img src=\"/later/img/h1\" alt=\"Fox\"></p>",
		Images:      map[string]string{"h1": "https://essays.example.com/fox.jpg"},
		Tags:        []string{"essays"},
	})
	if _, err := s.Store.AddHighlight(ctx, art.ID, art.ContentText, 4, 9, "quick", "Nice word."); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetNote(ctx, uid, art.ID, "Worth a re-read."); err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, s.Alice, httptest.NewRequest("GET", fmt.Sprintf("/later/a/%d/markdown", art.ID), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET markdown = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="later-article.md"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"# Reading slowly\n",
		"Source: <https://essays.example.com/slow>",
		"Tags: essays",
		"## Note\n\nWorth a re-read.",
		"## Highlights\n\n> quick\n\nNice word.",
		"## Article\n\nThe quick brown fox\n\n[Fox](https://essays.example.com/fox.jpg)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("markdown is missing %q:\n%s", want, body)
		}
	}

	// Downloading isn't reading: the article stays unread.
	got, err := s.Store.Article(ctx, uid, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != later.StateUnread {
		t.Errorf("state after download = %q, want unread", got.State)
	}
}

func TestDownloadMarkdownIsOwnerScoped(t *testing.T) {
	s := newServer(t)
	art := seed(t, s, s.Alice.User.ID, later.NewArticle{URL: "https://a.example/1", Title: "Mine", ContentHTML: words(5)})
	path := fmt.Sprintf("/later/a/%d/markdown", art.ID)
	if rec := s.Do(t, s.Bob, httptest.NewRequest("GET", path, nil)); rec.Code != http.StatusNotFound {
		t.Errorf("Bob GET %s = %d, want 404", path, rec.Code)
	}
	if rec := s.Do(t, s.Alice, httptest.NewRequest("GET", "/later/a/999/markdown", nil)); rec.Code != http.StatusNotFound {
		t.Errorf("missing article = %d, want 404", rec.Code)
	}
	if rec := s.Do(t, nil, httptest.NewRequest("GET", path, nil)); rec.Code != http.StatusSeeOther {
		t.Errorf("anonymous = %d, want a 303 to the login page", rec.Code)
	}
}

func TestReadingViewMenuOffersMarkdownDownload(t *testing.T) {
	s := newServer(t)
	art := seed(t, s, s.Alice.User.ID, later.NewArticle{URL: "https://a.example/1", Title: "Mine", ContentHTML: words(5)})
	doc := s.Get(t, s.Alice, fmt.Sprintf("/later/a/%d", art.ID))
	link := doc.MustHave(fmt.Sprintf(`a[href="/later/a/%d/markdown"]`, art.ID))
	if _, ok := htmlassert.Attr(link, "download"); !ok {
		t.Error("Download as Markdown link has no download attribute")
	}
	if got := strings.TrimSpace(htmlassert.Text(link)); got != "Download as Markdown" {
		t.Errorf("link text = %q", got)
	}
}
