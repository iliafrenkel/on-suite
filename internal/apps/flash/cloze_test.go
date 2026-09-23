// internal/apps/flash/cloze_test.go
package flash

import (
	"strings"
	"testing"
)

const blank = `<span class="flash-blank"><span class="visually-hidden">blank</span></span>`

func TestRenderCloze(t *testing.T) {
	tests := []struct {
		name, front, wantQ, wantA string
	}{
		{
			name:  "numbered deletion",
			front: "The capital of France is {{c1::Paris}}.",
			wantQ: "The capital of France is " + blank + ".",
			wantA: `The capital of France is <mark class="flash-fill">Paris</mark>.`,
		},
		{
			name:  "bare braces",
			front: "{{Water}} boils at 100°C.",
			wantQ: blank + " boils at 100°C.",
			wantA: `<mark class="flash-fill">Water</mark> boils at 100°C.`,
		},
		{
			name:  "hint",
			front: "{{c1::Madrid::a city}} is in Spain.",
			wantQ: `<span class="flash-blank"><span class="visually-hidden">blank</span><span class="flash-blank-hint">a city</span></span> is in Spain.`,
			wantA: `<mark class="flash-fill">Madrid</mark> is in Spain.`,
		},
		{
			name:  "several deletions blank together",
			front: "{{c1::Red}} and {{c2::blue}} make {{c3::purple}}.",
			wantQ: blank + " and " + blank + " make " + blank + ".",
			wantA: `<mark class="flash-fill">Red</mark> and <mark class="flash-fill">blue</mark> make <mark class="flash-fill">purple</mark>.`,
		},
	}
	for _, tt := range tests {
		q, a := renderCloze(tt.front)
		if string(q) != tt.wantQ {
			t.Errorf("%s: question =\n  %s\nwant\n  %s", tt.name, q, tt.wantQ)
		}
		if string(a) != tt.wantA {
			t.Errorf("%s: answer =\n  %s\nwant\n  %s", tt.name, a, tt.wantA)
		}
	}
}

func TestRenderClozeLeavesUnmatchedBraces(t *testing.T) {
	q, a := renderCloze("Opens with {{ but never closes")
	if string(q) != "Opens with {{ but never closes" || string(a) != string(q) {
		t.Errorf("unmatched: q=%q a=%q, want the text unchanged", q, a)
	}
	q, _ = renderCloze("Empty {{}} marker")
	if string(q) != "Empty {{}} marker" {
		t.Errorf("empty marker: q=%q, want it left literal", q)
	}
}

func TestRenderClozeEscapesHTML(t *testing.T) {
	q, a := renderCloze(`<b>bold</b> & {{c1::<script>x</script>::<i>hint</i>}}`)
	for _, got := range []string{string(q), string(a)} {
		if strings.Contains(got, "<b>") || strings.Contains(got, "<script>") || strings.Contains(got, "<i>") {
			t.Errorf("unescaped HTML in %q", got)
		}
	}
	if !strings.Contains(string(q), "&lt;b&gt;bold&lt;/b&gt; &amp; ") {
		t.Errorf("question = %q, want the surrounding text escaped", q)
	}
	if !strings.Contains(string(q), "&lt;i&gt;hint&lt;/i&gt;") {
		t.Errorf("question = %q, want the hint escaped", q)
	}
	if !strings.Contains(string(a), "&lt;script&gt;x&lt;/script&gt;") {
		t.Errorf("answer = %q, want the deletion text escaped", a)
	}
}

func TestRenderCardFaces(t *testing.T) {
	q, a := renderCardFaces(Card{CardType: CardTypeBasic, Front: "2 < 3?", Back: "yes & no"})
	if string(q) != "2 &lt; 3?" || string(a) != "yes &amp; no" {
		t.Errorf("basic: q=%q a=%q", q, a)
	}
	q, a = renderCardFaces(Card{CardType: CardTypeCloze, Front: "{{c1::Paris}}", Back: "ignored"})
	if string(q) != blank || string(a) != `<mark class="flash-fill">Paris</mark>` {
		t.Errorf("cloze: q=%q a=%q", q, a)
	}
}
