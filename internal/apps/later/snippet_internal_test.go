package later

import (
	"slices"
	"testing"
)

func TestSnippetParts(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []snippetPart
	}{
		{"", nil},
		{"plain", []snippetPart{{Text: "plain"}}},
		{"a \x02b\x03 c", []snippetPart{{Text: "a "}, {Text: "b", Hit: true}, {Text: " c"}}},
		{"\x02x\x03\x02y\x03", []snippetPart{{Text: "x", Hit: true}, {Text: "y", Hit: true}}},
		{"one\n\n  two \x02three\x03", []snippetPart{{Text: "one two "}, {Text: "three", Hit: true}}},
		{"cut \x02off", []snippetPart{{Text: "cut "}, {Text: "off", Hit: true}}},
	} {
		if got := snippetParts(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("snippetParts(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}
