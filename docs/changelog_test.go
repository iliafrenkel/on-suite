package docs

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	changelogHeadingRe = regexp.MustCompile(`^## (\d+)\.(\d+)\.(\d+): \S.*$`)
	changelogDateRe    = regexp.MustCompile(`^\*\d{1,2} [A-Z][a-z]+ \d{4}\*$`)
)

// TestChangelogEntries keeps user/changelog.md in the shape the in-app
// "What's new" page and scripts/tag-message.sh rely on: one "## N.N.N: Title"
// heading per release, newest first, each followed by its date line.
func TestChangelogEntries(t *testing.T) {
	raw, err := os.ReadFile("user/changelog.md")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	if lines[0] != "# What's new" {
		t.Errorf("first line = %q, want %q", lines[0], "# What's new")
	}

	var prev [3]int
	entries := 0
	for i, line := range lines {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		m := changelogHeadingRe.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("line %d: heading %q is not \"## N.N.N: Title\"", i+1, line)
			continue
		}
		var v [3]int
		for j := range v {
			v[j], _ = strconv.Atoi(m[j+1])
		}
		if entries > 0 && !versionLess(v, prev) {
			t.Errorf("line %d: %d.%d.%d is not older than %d.%d.%d above it — newest first, no duplicates",
				i+1, v[0], v[1], v[2], prev[0], prev[1], prev[2])
		}
		if i+2 >= len(lines) || lines[i+1] != "" || !changelogDateRe.MatchString(lines[i+2]) {
			t.Errorf("line %d: %q must be followed by a blank line and a date like *26 November 2026*", i+1, line)
		}
		prev = v
		entries++
	}
	if entries == 0 {
		t.Error("changelog has no entries")
	}
}

func versionLess(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
