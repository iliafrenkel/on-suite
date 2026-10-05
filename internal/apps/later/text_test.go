package later_test

import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/apps/later"
)

func TestContentTextIsTheConcatenatedTextNodes(t *testing.T) {
	got := later.ContentText(`<h1>Title</h1><p>One <em>two</em> &amp; three.</p><p>Next</p>`)
	if want := "TitleOne two & three.Next"; got != want {
		t.Errorf("ContentText = %q, want %q", got, want)
	}
}

func TestContentTextKeepsMultiByteText(t *testing.T) {
	got := later.ContentText(`<p>Привет 👋 שלום</p>`)
	if got != "Привет 👋 שלום" {
		t.Errorf("ContentText = %q", got)
	}
}

func TestWordCountSeparatesAtElementBoundaries(t *testing.T) {
	if got := later.WordCount(`<p>end.</p><p>Next one</p>`); got != 3 {
		t.Errorf("WordCount = %d, want 3", got)
	}
}

func TestReadingMinutes(t *testing.T) {
	for words, want := range map[int]int{0: 1, 1: 1, 230: 1, 231: 2, 2300: 10} {
		if got := later.ReadingMinutes(words); got != want {
			t.Errorf("ReadingMinutes(%d) = %d, want %d", words, got, want)
		}
	}
}

func TestPastedHTMLEscapesAndSplitsParagraphs(t *testing.T) {
	got := later.PastedHTML("First <b>line</b>\nstill first\n\n\n  Second  \n")
	want := "<p>First &lt;b&gt;line&lt;/b&gt;\nstill first</p><p>Second</p>"
	if got != want {
		t.Errorf("PastedHTML = %q, want %q", got, want)
	}
	if strings.Contains(later.PastedHTML("   \n\n  "), "<p>") {
		t.Error("blank input produced paragraphs")
	}
}
