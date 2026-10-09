package books_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/books"
)

func TestNormalizeTidiesFields(t *testing.T) {
	got := books.BookInput{
		Title:        "  The \n Dispossessed ",
		Authors:      "Ursula K.\tLe Guin",
		ISBN:         " 0-306-40615-2 ",
		SeriesName:   " Hainish  Cycle ",
		SeriesNumber: " 6 ",
		Description:  "  One.\r\nTwo.  ",
	}.Normalize()
	want := books.BookInput{
		Title:        "The Dispossessed",
		Authors:      "Ursula K. Le Guin",
		ISBN:         "9780306406157",
		SeriesName:   "Hainish Cycle",
		SeriesNumber: "6",
		Description:  "One.\nTwo.",
	}
	if got != want {
		t.Errorf("Normalize() = %+v\nwant %+v", got, want)
	}
}

func TestNormalizeKeepsAnInvalidISBNForValidateToReport(t *testing.T) {
	if got := (books.BookInput{ISBN: " 12345 "}).Normalize().ISBN; got != "12345" {
		t.Errorf("ISBN = %q, want 12345", got)
	}
}

func TestValidateAcceptsAMinimalBook(t *testing.T) {
	if errs := (books.BookInput{Title: "Piranesi"}).Normalize().Validate(); errs != nil {
		t.Errorf("Validate() = %v, want nil", errs)
	}
}

func TestValidateRejectsBadInput(t *testing.T) {
	long := func(n int) string { return strings.Repeat("é", n) }
	tests := []struct {
		name  string
		in    books.BookInput
		field string
	}{
		{"no title", books.BookInput{Title: "  "}, "title"},
		{"long title", books.BookInput{Title: long(301)}, "title"},
		{"long subtitle", books.BookInput{Title: "x", Subtitle: long(301)}, "subtitle"},
		{"long authors", books.BookInput{Title: "x", Authors: long(301)}, "authors"},
		{"negative year", books.BookInput{Title: "x", Year: -1}, "year"},
		{"far year", books.BookInput{Title: "x", Year: 10000}, "year"},
		{"negative pages", books.BookInput{Title: "x", Pages: -5}, "pages"},
		{"huge pages", books.BookInput{Title: "x", Pages: 100001}, "pages"},
		{"bad isbn", books.BookInput{Title: "x", ISBN: "12345"}, "isbn"},
		{"long series", books.BookInput{Title: "x", SeriesName: long(201)}, "series_name"},
		{"long number", books.BookInput{Title: "x", SeriesName: "s", SeriesNumber: long(11)}, "series_number"},
		{"number without series", books.BookInput{Title: "x", SeriesNumber: "2"}, "series_name"},
		{"long description", books.BookInput{Title: "x", Description: long(10001)}, "description"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.in.Normalize().Validate()
			if errs[tt.field] == "" {
				t.Errorf("Validate() = %v, want a message for %q", errs, tt.field)
			}
		})
	}
}

func TestValidationErrorIsErrInvalid(t *testing.T) {
	var err error = &books.ValidationError{Fields: books.FieldErrors{"title": "x"}}
	if !errors.Is(err, books.ErrInvalid) {
		t.Error("ValidationError does not unwrap to ErrInvalid")
	}
}

func TestISBN13(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"9780306406157", "9780306406157", true},
		{"978-0-306-40615-7", "9780306406157", true},
		{"0306406152", "9780306406157", true},
		{"0-8044-2957-X", "9780804429573", true},
		{"080442957x", "9780804429573", true},
		{"9780306406158", "", false}, // wrong check digit
		{"0306406153", "", false},    // wrong check digit
		{"1234567890123", "", false}, // not a 978/979 prefix
		{"X306406152", "", false},    // X only as the last ISBN-10 digit
		{"978030640615", "", false},  // twelve digits
		{"ISBN 0306406152", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := books.ISBN13(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ISBN13(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
