package ui_test

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/ui"
)

// appClassPattern matches a class name owned by one app's CSS section in
// app.css (reader-*, notes-*, paste-*, flash-*).
var appClassPattern = regexp.MustCompile(`^(reader|notes|paste|flash)-`)

var classAttrPattern = regexp.MustCompile(`class="([^"]*)"`)

// TestPlatformTemplatesUseNoAppClasses keeps the platform pages off app CSS
// (#398). A platform page borrowing, say, a reader-* class gets restyled by
// any later change to the reader, with nothing to show the two are linked.
// A style both need belongs in a shared class instead.
func TestPlatformTemplatesUseNoAppClasses(t *testing.T) {
	templates := ui.Templates()
	names, err := fs.Glob(templates, "*.html")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("no platform templates found")
	}
	for _, name := range names {
		src, err := fs.ReadFile(templates, name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range classAttrPattern.FindAllStringSubmatch(string(src), -1) {
			for _, class := range strings.Fields(m[1]) {
				if appClassPattern.MatchString(class) {
					t.Errorf("%s uses app class %q", name, class)
				}
			}
		}
	}
}
