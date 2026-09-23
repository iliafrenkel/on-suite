// internal/apps/flash/cloze.go
package flash

import (
	"html"
	"html/template"
	"regexp"
	"strings"
)

// clozeMarker matches one {{...}} deletion. Non-greedy, and (?s) so a
// deletion may span a line break. "{{}}" (nothing inside) does not match
// and stays literal text.
var clozeMarker = regexp.MustCompile(`(?s)\{\{(.+?)\}\}`)

// clozeNumber is the optional Anki-style "c1::" prefix inside a marker.
var clozeNumber = regexp.MustCompile(`^c\d+::`)

// renderCloze turns a cloze card's front into its two faces: the question,
// where every deletion is a blank (showing its hint, if the marker has
// one), and the answer, where every deletion is filled in and highlighted.
// All deletions blank at once — one card, not one per number (see the F3
// import spec's "Cloze cards" section).
//
// This, with renderCardFaces, is the only place card text becomes
// template.HTML: everything outside the markers, and each marker's text and
// hint, is HTML-escaped before the fixed markup below is added.
func renderCloze(front string) (question, answer template.HTML) {
	var q, a strings.Builder
	last := 0
	for _, m := range clozeMarker.FindAllStringSubmatchIndex(front, -1) {
		plain := html.EscapeString(front[last:m[0]])
		q.WriteString(plain)
		a.WriteString(plain)

		inner := clozeNumber.ReplaceAllString(front[m[2]:m[3]], "")
		text, hint, _ := strings.Cut(inner, "::")

		q.WriteString(`<span class="flash-blank"><span class="visually-hidden">blank</span>`)
		if hint != "" {
			q.WriteString(`<span class="flash-blank-hint">` + html.EscapeString(hint) + `</span>`)
		}
		q.WriteString(`</span>`)
		a.WriteString(`<mark class="flash-fill">` + html.EscapeString(text) + `</mark>`)
		last = m[1]
	}
	rest := html.EscapeString(front[last:])
	q.WriteString(rest)
	a.WriteString(rest)
	return template.HTML(q.String()), template.HTML(a.String())
}

// renderCardFaces returns the question and answer faces for any card: a
// cloze card's come from renderCloze; a basic card's are its escaped front
// and back.
func renderCardFaces(c Card) (question, answer template.HTML) {
	if c.CardType == CardTypeCloze {
		return renderCloze(c.Front)
	}
	return template.HTML(html.EscapeString(c.Front)), template.HTML(html.EscapeString(c.Back))
}
