package books

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits on a book's fields, in characters after Normalize (spec "Data
// model"; this is a reading log, not a catalogue).
const (
	MaxTitleRunes        = 300
	MaxAuthorsRunes      = 300
	MaxSeriesRunes       = 200
	MaxSeriesNumberRunes = 10
	MaxDescriptionRunes  = 10000
	MaxPages             = 100000
	MaxYear              = 9999
)

// BookInput is a book's own details, as the form (and, from B1b, Open
// Library) supplies them. Year and Pages are 0 when unknown.
type BookInput struct {
	Title, Subtitle, Authors string
	Year, Pages              int
	// ISBN is as typed until Normalize turns a valid one into ISBN-13.
	ISBN                     string
	SeriesName, SeriesNumber string
	Description              string
}

// FieldErrors maps a form field name to what is wrong with it.
type FieldErrors map[string]string

// ValidationError is a book the store refuses, with a message per field.
type ValidationError struct{ Fields FieldErrors }

func (e *ValidationError) Error() string { return fmt.Sprintf("books: invalid book: %v", e.Fields) }

// Unwrap makes a ValidationError an ErrInvalid for errors.Is.
func (e *ValidationError) Unwrap() error { return ErrInvalid }

// Normalize tidies what was typed: one-line fields lose newlines and runs
// of spaces, the description keeps its line breaks, and a valid ISBN-10 or
// ISBN-13 becomes plain ISBN-13 digits. An invalid ISBN is left (trimmed)
// for Validate to report.
func (in BookInput) Normalize() BookInput {
	in.Title = oneLine(in.Title)
	in.Subtitle = oneLine(in.Subtitle)
	in.Authors = oneLine(in.Authors)
	in.SeriesName = oneLine(in.SeriesName)
	in.SeriesNumber = oneLine(in.SeriesNumber)
	in.ISBN = strings.TrimSpace(in.ISBN)
	if isbn, ok := ISBN13(in.ISBN); ok {
		in.ISBN = isbn
	}
	in.Description = strings.TrimSpace(strings.ReplaceAll(in.Description, "\r\n", "\n"))
	return in
}

// oneLine turns control characters into spaces and collapses whitespace.
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// Validate checks normalized input; nil means it is fine.
func (in BookInput) Validate() FieldErrors {
	errs := FieldErrors{}
	tooLong := func(field, value string, max int) {
		if utf8.RuneCountInString(value) > max {
			errs[field] = fmt.Sprintf("Keep it to %d characters or fewer.", max)
		}
	}
	if in.Title == "" {
		errs["title"] = "Enter the book's title."
	} else {
		tooLong("title", in.Title, MaxTitleRunes)
	}
	tooLong("subtitle", in.Subtitle, MaxTitleRunes)
	tooLong("authors", in.Authors, MaxAuthorsRunes)
	if in.Year < 0 || in.Year > MaxYear {
		errs["year"] = fmt.Sprintf("Enter a year from 1 to %d, or leave it empty.", MaxYear)
	}
	if in.Pages < 0 || in.Pages > MaxPages {
		errs["pages"] = fmt.Sprintf("Enter a page count from 1 to %d, or leave it empty.", MaxPages)
	}
	if in.ISBN != "" {
		if _, ok := ISBN13(in.ISBN); !ok {
			errs["isbn"] = "That isn't a valid ISBN-10 or ISBN-13."
		}
	}
	tooLong("series_name", in.SeriesName, MaxSeriesRunes)
	tooLong("series_number", in.SeriesNumber, MaxSeriesNumberRunes)
	if in.SeriesNumber != "" && in.SeriesName == "" {
		errs["series_name"] = "Name the series this number belongs to."
	}
	tooLong("description", in.Description, MaxDescriptionRunes)
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// ISBN13 returns s as a 13-digit ISBN if, once spaces and hyphens are
// dropped, it is a valid ISBN-13 (978/979 prefix, right check digit) or a
// valid ISBN-10, which is converted (spec: "ISBN-10 converted on input").
func ISBN13(s string) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ' || r == '-':
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == 'X' || r == 'x':
			b.WriteByte('X')
		default:
			return "", false
		}
	}
	d := b.String()
	switch len(d) {
	case 13:
		if strings.Contains(d, "X") || !(strings.HasPrefix(d, "978") || strings.HasPrefix(d, "979")) {
			return "", false
		}
		if checkDigit13(d[:12]) != d[12] {
			return "", false
		}
		return d, true
	case 10:
		if strings.Contains(d[:9], "X") {
			return "", false
		}
		sum := 0
		for i := 0; i < 10; i++ {
			v := int(d[i] - '0')
			if d[i] == 'X' {
				v = 10
			}
			sum += v * (10 - i)
		}
		if sum%11 != 0 {
			return "", false
		}
		body := "978" + d[:9]
		return body + string(checkDigit13(body)), true
	}
	return "", false
}

// checkDigit13 is the ISBN-13 check digit of twelve digits: weights 1 and 3
// alternating, then whatever brings the sum to a multiple of ten.
func checkDigit13(twelve string) byte {
	sum := 0
	for i := 0; i < 12; i++ {
		v := int(twelve[i] - '0')
		if i%2 == 1 {
			v *= 3
		}
		sum += v
	}
	return byte('0' + (10-sum%10)%10)
}
