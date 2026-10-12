package books

import (
	"cmp"
	"slices"
	"strings"
)

// titleKey is a title as the Title sort reads it: lower case, without a
// leading English article, so "The Left Hand of Darkness" files under L
// (UI redesign spec "Sorting"). A title that is only an article keeps it.
func titleKey(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	for _, article := range []string{"the ", "a ", "an "} {
		if rest, ok := strings.CutPrefix(t, article); ok && strings.TrimSpace(rest) != "" {
			return strings.TrimSpace(rest)
		}
	}
	return t
}

// authorKey is the Author sort's key: the last word of the first author
// (authors are free text, "Terry Pratchett, Neil Gaiman"), lower case; ""
// for a book with no author. "Ursula K. Le Guin" files under G: a
// "sort as" field would fix that, and the spec chose not to have one.
func authorKey(authors string) string {
	first, _, _ := strings.Cut(authors, ",")
	words := strings.Fields(first)
	if len(words) == 0 {
		return ""
	}
	return strings.ToLower(words[len(words)-1])
}

// sortItems puts items in Title or Author order (by), A–Z or, reversed,
// Z–A. Under Author a book with no author is last either way, and a tie
// goes by title. Equal titles keep the order they were added in.
func sortItems(items []ListItem, by Sort, reversed bool) {
	dir := 1
	if reversed {
		dir = -1
	}
	slices.SortStableFunc(items, func(a, b ListItem) int {
		if by == SortAuthor {
			ka, kb := authorKey(a.Authors), authorKey(b.Authors)
			switch {
			case ka == "" && kb != "":
				return 1
			case ka != "" && kb == "":
				return -1
			}
			if c := strings.Compare(ka, kb); c != 0 {
				return dir * c
			}
		}
		if c := strings.Compare(titleKey(a.Title), titleKey(b.Title)); c != 0 {
			return dir * c
		}
		return dir * cmp.Compare(a.ID, b.ID)
	})
}
