package books

import (
	"hash/fnv"
	"strings"
)

// Colors is the suite's swatch palette (internal/ui/static/app.css's
// swatch-c-* classes). It mirrors ON Focus's own list — apps never import
// each other, so each keeps its copy; the CSS is the shared part.
var Colors = []string{"teal", "blue", "purple", "pink", "coral", "amber", "green", "gray"}

// SpineColor picks the colour of a book's generated spine from its title,
// so the same book always looks the same (spec "Architecture": generated
// spine).
func SpineColor(title string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(title)))
	return Colors[h.Sum32()%uint32(len(Colors))]
}
