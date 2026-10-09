package books_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// readBook adds one of Alice's books, already read, with pages pages.
func readBook(t *testing.T, s *server, title string, pages int) int64 {
	t.Helper()
	nb := titled(title, "", books.ShelfRead)
	nb.Pages = pages
	return add(t, s, s.Alice.User.ID, nb)
}

func noteIDs(t *testing.T, s *server, id int64) []int64 {
	t.Helper()
	ns, err := s.Store.Notes(context.Background(), s.Alice.User.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	var out []int64
	for _, n := range ns {
		out = append(out, n.ID)
	}
	return out
}

// texts is the text of every element selector matches, in page order.
func texts(doc *htmlassert.Doc, selector string) []string {
	var out []string
	for _, n := range doc.QueryAll(selector) {
		out = append(out, htmlassert.Text(n))
	}
	return out
}

func isOpen(doc *htmlassert.Doc, selector string) bool {
	_, ok := htmlassert.Attr(doc.MustHave(selector), "open")
	return ok
}

func TestTheBookPaneShowsNotesNewestFirst(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	uid := s.Alice.User.ID
	s.Clock.Set(time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC))
	id := readBook(t, s, "Dune", 600)
	if _, err := s.Store.AddNote(ctx, uid, id, books.NoteInput{Page: 112, Body: "The **spice**."}); err != nil {
		t.Fatal(err)
	}
	s.Clock.Set(time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC))
	if _, err := s.Store.AddNote(ctx, uid, id, books.NoteInput{Body: "No page."}); err != nil {
		t.Fatal(err)
	}
	rec := s.Do(t, s.Alice, httptestGet(fmt.Sprintf("/books/b/%d", id)))
	doc := htmlassert.Parse(t, rec.Body.String())
	if got := strings.Join(texts(doc, ".books-note .books-entry-meta"), "|"); got != "5 Oct 2026|1 Oct 2026 · p. 112" {
		t.Errorf("note dates = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave(".books-note strong")); got != "spice" {
		t.Errorf("Markdown in a note = %q", got)
	}
	if isOpen(doc, "details#books-note-new") {
		t.Error("the add-note box is open on a plain page load")
	}
	if v, _ := htmlassert.Attr(doc.MustHave("input#books-note-new-page"), "max"); v != "600" {
		t.Errorf("page box max = %q, want 600", v)
	}
	if n := len(doc.QueryAll(".books-note button[hx-confirm]")); n != 2 {
		t.Errorf("%d delete buttons, want one per note", n)
	}
	body := rec.Body.String()
	if !(strings.Index(body, `id="books-notes"`) < strings.Index(body, `id="books-history-head"`)) {
		t.Error("notes come after the reading history; want them before (spec order)")
	}

	other := add(t, s, uid, titled("Emma", "", books.ShelfWant))
	doc = s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", other))
	doc.MustHave("details#books-note-new")
	doc.MustNotHave(".books-note")
}

func TestAddingANote(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	path := fmt.Sprintf("/books/notes/%d", id)
	s.Submit(t, s.Alice, path, url.Values{"shelf": {"read"}, "page": {" 112 "}, "body": {"Without JavaScript."}},
		fmt.Sprintf("/books/b/%d?shelf=read", id))

	rec := postHXTo(t, s, path, "books-notes", url.Values{"shelf": {"read"}, "body": {"With htmx."}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx add = %d, want 200", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("section#books-notes")
	if v, _ := htmlassert.Attr(doc.MustHave("#books-list"), "hx-swap-oob"); v != "true" {
		t.Errorf("list hx-swap-oob = %q, want the list out of band", v)
	}
	doc.MustNotHave("#books-panes")
	if got := strings.Join(texts(doc, ".books-entry-text"), "|"); got != "With htmx.|Without JavaScript." {
		t.Errorf("notes = %q", got)
	}
	if n := len(noteIDs(t, s, id)); n != 2 {
		t.Errorf("%d notes stored, want 2", n)
	}
}

func TestARefusedNoteComesBackInItsForm(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	path := fmt.Sprintf("/books/notes/%d", id)
	form := url.Values{"shelf": {"read"}, "page": {"0"}, "body": {"Keep what I typed."}}
	rec := postHXTo(t, s, path, "books-notes", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx refusal = %d, want a 200 fragment", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	if !isOpen(doc, "details#books-note-new") {
		t.Error("the add-note box is closed; want it open with the message")
	}
	if got := htmlassert.Text(doc.MustHave("#books-note-new-error")); got != "Enter a page from 1 to 600." {
		t.Errorf("message = %q", got)
	}
	if got := htmlassert.Text(doc.MustHave("textarea#books-note-new-body")); got != "Keep what I typed." {
		t.Errorf("textarea = %q, want what was typed", got)
	}
	if v, _ := htmlassert.Attr(doc.MustHave("input#books-note-new-page"), "value"); v != "0" {
		t.Errorf("page box = %q, want what was typed", v)
	}

	rec = s.Post(t, s.Alice, path, url.Values{"shelf": {"read"}, "body": {"  "}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("refused without JavaScript = %d, want 422", rec.Code)
	}
	doc = htmlassert.Parse(t, rec.Body.String())
	if got := htmlassert.Text(doc.MustHave("#books-note-new-error")); got != "Write something in the note first." {
		t.Errorf("message = %q", got)
	}
	for _, page := range []string{"abc", "-3", "2.5"} {
		rec = postHXTo(t, s, path, "books-notes", url.Values{"page": {page}, "body": {"x"}})
		doc = htmlassert.Parse(t, rec.Body.String())
		if got := htmlassert.Text(doc.MustHave("#books-note-new-error")); got != "Enter a page from 1 to 600." {
			t.Errorf("page %q: message = %q", page, got)
		}
	}
	if n := len(noteIDs(t, s, id)); n != 0 {
		t.Errorf("%d notes stored by refused posts", n)
	}
}

func TestEditingAndDeletingANoteInThePane(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	nid, err := s.Store.AddNote(context.Background(), s.Alice.User.ID, id, books.NoteInput{Page: 5, Body: "Draft."})
	if err != nil {
		t.Fatal(err)
	}
	doc := s.Get(t, s.Alice, fmt.Sprintf("/books/b/%d", id))
	if got := htmlassert.Text(doc.MustHave(fmt.Sprintf("textarea#books-note-%d-body", nid))); got != "Draft." {
		t.Errorf("edit box = %q", got)
	}
	if v, _ := htmlassert.Attr(doc.MustHave(fmt.Sprintf("input#books-note-%d-page", nid)), "value"); v != "5" {
		t.Errorf("edit page box = %q", v)
	}

	edit := fmt.Sprintf("/books/notes/%d/%d", id, nid)
	s.Submit(t, s.Alice, edit, url.Values{"shelf": {"read"}, "page": {""}, "body": {"Final."}}, fmt.Sprintf("/books/b/%d?shelf=read", id))
	ns, _ := s.Store.Notes(context.Background(), s.Alice.User.ID, id)
	if len(ns) != 1 || ns[0].Body != "Final." || ns[0].Page != 0 {
		t.Errorf("edited notes = %+v", ns)
	}

	rec := postHXTo(t, s, edit, "books-notes", url.Values{"body": {""}})
	doc = htmlassert.Parse(t, rec.Body.String())
	if !isOpen(doc, ".books-entry-edit") {
		t.Error("the refused note's Edit box is closed")
	}
	if got := htmlassert.Text(doc.MustHave(fmt.Sprintf("#books-note-%d-error", nid))); got != "Write something in the note first." {
		t.Errorf("message = %q", got)
	}
	if isOpen(doc, "details#books-note-new") {
		t.Error("the add-note box opened for an edit's refusal")
	}

	rec = postHXTo(t, s, edit+"/delete", "books-notes", url.Values{"shelf": {"read"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx delete = %d", rec.Code)
	}
	htmlassert.Parse(t, rec.Body.String()).MustNotHave(".books-note")
	if n := len(noteIDs(t, s, id)); n != 0 {
		t.Errorf("%d notes after delete", n)
	}
}

func TestNoteRoutesAreNotFoundForOthers(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	nid, err := s.Store.AddNote(context.Background(), s.Alice.User.ID, id, books.NoteInput{Body: "Mine."})
	if err != nil {
		t.Fatal(err)
	}
	other := add(t, s, s.Alice.User.ID, titled("Emma", "", books.ShelfWant))
	for _, tt := range []struct {
		sess string
		path string
	}{
		{"bob", fmt.Sprintf("/books/notes/%d", id)},
		{"bob", fmt.Sprintf("/books/notes/%d/%d", id, nid)},
		{"bob", fmt.Sprintf("/books/notes/%d/%d/delete", id, nid)},
		{"alice", fmt.Sprintf("/books/notes/%d/%d", other, nid)},
		{"alice", fmt.Sprintf("/books/notes/%d/%d/delete", other, nid)},
		{"alice", fmt.Sprintf("/books/notes/%d/x", id)},
	} {
		sess := s.Alice
		if tt.sess == "bob" {
			sess = s.Bob
		}
		if rec := s.Post(t, sess, tt.path, url.Values{"body": {"x"}}); rec.Code != http.StatusNotFound {
			t.Errorf("%s POST %s = %d, want 404", tt.sess, tt.path, rec.Code)
		}
	}
	if ns, _ := s.Store.Notes(context.Background(), s.Alice.User.ID, id); len(ns) != 1 || ns[0].Body != "Mine." {
		t.Errorf("notes = %+v, want Alice's note untouched", ns)
	}
}

func TestDeletingANoteWithoutJavaScript(t *testing.T) {
	s := newServer(t)
	id := readBook(t, s, "Dune", 600)
	nid, err := s.Store.AddNote(context.Background(), s.Alice.User.ID, id, books.NoteInput{Body: "Gone."})
	if err != nil {
		t.Fatal(err)
	}
	s.Submit(t, s.Alice, fmt.Sprintf("/books/notes/%d/%d/delete", id, nid), url.Values{"shelf": {"read"}},
		fmt.Sprintf("/books/b/%d?shelf=read", id))
	if n := len(noteIDs(t, s, id)); n != 0 {
		t.Errorf("%d notes after delete", n)
	}
}
