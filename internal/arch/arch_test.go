// Package arch contains no code. It holds one test that enforces the import
// boundaries the design depends on.
//
// These rules are stated in the spec and in both implementation plans. A rule
// that is only written down gets violated during a late-night change; this one
// fails the build.
package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/iliafrenkel/on-suite"

// pkgImports maps a package path, relative to the module root, to the module's
// own packages it imports. Test files are recorded separately, because a test
// is allowed to import things production code may not.
type pkgImports struct {
	prod map[string][]string
	test map[string][]string
}

func scan(t *testing.T) pkgImports {
	t.Helper()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// Fail loudly rather than silently passing on an empty scan.
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("cannot find the module root from %s: %v", root, err)
	}

	out := pkgImports{prod: map[string][]string{}, test: map[string][]string{}}
	fset := token.NewFileSet()

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "docs", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(rel)

		target := out.prod
		if strings.HasSuffix(d.Name(), "_test.go") {
			target = out.test
		}
		// Record the package even when it imports nothing from this module,
		// so a package with no internal dependencies still counts as scanned.
		if _, ok := target[pkg]; !ok {
			target[pkg] = nil
		}
		for _, spec := range f.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(imported, module+"/") {
				continue // stdlib or third party: not our concern here
			}
			target[pkg] = append(target[pkg], strings.TrimPrefix(imported, module+"/"))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	if len(out.prod) == 0 {
		t.Fatal("scanned no packages; the walk is broken, not the code")
	}
	return out
}

// appName returns the app id for a package inside internal/apps, or "".
func appName(pkg string) string {
	const prefix = "internal/apps/"
	if !strings.HasPrefix(pkg, prefix) {
		return ""
	}
	return strings.SplitN(strings.TrimPrefix(pkg, prefix), "/", 2)[0]
}

// TestAppsDoNotImportEachOther is the rule that keeps each new app cheap: an
// app must be removable by deleting its package and one line in main.
func TestAppsDoNotImportEachOther(t *testing.T) {
	imports := scan(t)
	for pkg, deps := range imports.prod {
		self := appName(pkg)
		if self == "" {
			continue
		}
		for _, dep := range deps {
			other := appName(dep)
			if other != "" && other != self {
				t.Errorf("app %q imports app %q (%s -> %s)", self, other, pkg, dep)
			}
		}
	}
}

// TestPlatformDoesNotImportApps keeps the platform free of app-specific
// knowledge.
func TestPlatformDoesNotImportApps(t *testing.T) {
	imports := scan(t)
	for pkg, deps := range imports.prod {
		if !strings.HasPrefix(pkg, "internal/platform/") && pkg != "internal/ui" {
			continue
		}
		for _, dep := range deps {
			if strings.HasPrefix(dep, "internal/apps/") {
				t.Errorf("platform package %q imports %q", pkg, dep)
			}
		}
	}
}

// TestLayering encodes the dependency order the packages were designed with.
// render must not reach for web, or the two become mutually dependent and
// render stops being testable without a request.
func TestLayering(t *testing.T) {
	forbidden := map[string][]string{
		"internal/platform/render": {"internal/platform/web", "internal/platform/app", "internal/platform/auth"},
		"internal/platform/auth":   {"internal/platform/web", "internal/platform/app", "internal/platform/render"},
		"internal/platform/db":     {"internal/platform/web", "internal/platform/app", "internal/platform/render", "internal/platform/auth"},
		"internal/platform/config": {"internal/platform/web", "internal/platform/app", "internal/platform/render", "internal/platform/auth", "internal/platform/db"},
		"internal/platform/web":    {"internal/platform/app"},
		// jobs takes closures and nothing else. If it ever imports a platform
		// package, someone has taught the scheduler what a backup is.
		"internal/platform/jobs": {
			"internal/platform/web", "internal/platform/app", "internal/platform/render",
			"internal/platform/auth", "internal/platform/db", "internal/platform/config",
		},
	}

	imports := scan(t)
	for pkg, banned := range forbidden {
		for _, dep := range imports.prod[pkg] {
			for _, b := range banned {
				if dep == b || strings.HasPrefix(dep, b+"/") {
					t.Errorf("%q must not import %q", pkg, dep)
				}
			}
		}
	}
}

// TestUIIsALeaf: it holds embedded bytes, nothing more.
func TestUIIsALeaf(t *testing.T) {
	imports := scan(t)
	if deps := imports.prod["internal/ui"]; len(deps) != 0 {
		t.Errorf("internal/ui imports %v; it must be a leaf", deps)
	}
}

// TestHTMLAssertIsTestOnly. The helper lives in a normal package so several
// test packages can share it, which means only this check stops it being used
// in production code. internal/apptest is exempted, not just internal/
// htmlassert itself: it is the same kind of test-only helper (see
// TestAppTestIsTestOnly below) and imports htmlassert for its own Get.
func TestHTMLAssertIsTestOnly(t *testing.T) {
	imports := scan(t)
	for pkg, deps := range imports.prod {
		if pkg == "internal/htmlassert" || pkg == "internal/apptest" {
			continue
		}
		for _, dep := range deps {
			if dep == "internal/htmlassert" {
				t.Errorf("non-test code in %q imports internal/htmlassert", pkg)
			}
		}
	}
}

// TestAppTestIsTestOnly. internal/apptest (issue #50) is the shared handler-
// test harness every app's tests build on, in a normal package for the same
// reason internal/htmlassert is: several test packages need to import it, and
// only a _test.go file can import a normal package's own _test.go-suffixed
// files, so it cannot live in one. Only this check stops it being used in
// production code.
func TestAppTestIsTestOnly(t *testing.T) {
	imports := scan(t)
	for pkg, deps := range imports.prod {
		if pkg == "internal/apptest" {
			continue
		}
		for _, dep := range deps {
			if dep == "internal/apptest" {
				t.Errorf("non-test code in %q imports internal/apptest", pkg)
			}
		}
	}
}

// TestScanSeesTheRealTree guards the guard: if the walk silently stopped
// finding files, every test above would pass while checking nothing.
func TestScanSeesTheRealTree(t *testing.T) {
	imports := scan(t)
	for _, want := range []string{
		"cmd/onsuite",
		"internal/platform/web",
		"internal/platform/app",
		"internal/platform/render",
		"internal/platform/auth",
		"internal/platform/jobs",
		"internal/platform/admin",
	} {
		if _, ok := imports.prod[want]; !ok {
			t.Errorf("package %q was not scanned; known packages: %d", want, len(imports.prod))
		}
	}
	// A known-true edge: app must import web, since it uses the guard type.
	found := false
	for _, dep := range imports.prod["internal/platform/app"] {
		if dep == "internal/platform/web" {
			found = true
		}
	}
	if !found {
		t.Error("internal/platform/app does not import web; the scan is probably wrong")
	}
}

// TestReadabilityIsContained: go-readability brings two unmaintained
// transitive modules, and R4's plan accepted it only on the condition that it
// stays behind one file. A second importer makes it load-bearing, which is a
// different decision and should be made deliberately.
func TestReadabilityIsContained(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	const lib = "github.com/go-shiori/go-readability"

	var importers []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "docs", "dist", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		// A parse failure here is unrelated to what this test checks — a
		// fixture file containing deliberately malformed Go (a parser test's
		// testdata, say) must not abort the whole containment check. Treat it
		// as "no imports found" for that one file rather than failing loudly.
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return nil
		}
		for _, spec := range f.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if imported == lib {
				rel, _ := filepath.Rel(root, path)
				importers = append(importers, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"internal/apps/reader/extract.go"}
	if !slices.Equal(importers, want) {
		t.Errorf("go-readability is imported by %v, want only %v", importers, want)
	}
}

// TestFSRSIsContained: go-fsrs is ON Flash's scheduling engine, and F2's
// design accepted it only on the condition that it stays behind one file —
// see fsrs.go's own doc comment. A second importer makes it load-bearing
// elsewhere too, which is a different decision and should be made
// deliberately, the same rule TestReadabilityIsContained enforces for
// go-readability.
func TestFSRSIsContained(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	const lib = "github.com/open-spaced-repetition/go-fsrs/v4"

	var importers []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "docs", "dist", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return nil
		}
		for _, spec := range f.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if imported == lib {
				rel, _ := filepath.Rel(root, path)
				importers = append(importers, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"internal/apps/flash/fsrs.go"}
	if !slices.Equal(importers, want) {
		t.Errorf("go-fsrs is imported by %v, want only %v", importers, want)
	}
}

// TestRFC3339IsContained: stored timestamps go through db.FormatTime, whose
// fixed-width db.TimeLayout is what makes text order time order (#356).
// time.RFC3339Nano trims trailing fractional zeros, and a value written with
// it compares wrongly against its neighbours within one second — the bug
// that issue fixed across every app. So production code names time.RFC3339
// or time.RFC3339Nano only where the choice is deliberate: db.ParseTime
// (which accepts both widths) and the two places that keep a user-visible
// text as it always was. A new use is either storage, which should call
// db.FormatTime, or a display decision that belongs on this list.
//
// Besides the time.RFC3339/RFC3339Nano selector, this also flags a Go string
// literal that spells out an RFC 3339 time-of-day-plus-zone layout directly —
// e.g. "2006-01-02T15:04:05Z07:00" or "...05.999999999Z07:00" — by looking
// for "15:04:05" together with "Z07:00" in the same literal, so a hand-rolled
// layout string can't quietly reintroduce the same bug. A display layout like
// "2006-01-02 15:04 MST" has neither the seconds field nor "Z07:00" and does
// not match. This does not attempt to catch the layout built through an
// aliased or dot import of "time" (e.g. `. "time"` making a bare `RFC3339`
// identifier) — matching existing arch-test style, which also only checks
// the plain `time.` selector form.
func TestRFC3339IsContained(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	var users []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "docs", "dist", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil // unrelated to this check, as in TestReadabilityIsContained
		}
		found := false
		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.SelectorExpr:
				if v.Sel.Name != "RFC3339" && v.Sel.Name != "RFC3339Nano" {
					return true
				}
				if pkg, ok := v.X.(*ast.Ident); ok && pkg.Name == "time" {
					found = true
				}
			case *ast.BasicLit:
				if v.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(v.Value)
				if err != nil {
					return true
				}
				if strings.Contains(s, "15:04:05") && strings.Contains(s, "Z07:00") {
					found = true
				}
			}
			return true
		})
		if found {
			rel, _ := filepath.Rel(root, path)
			users = append(users, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"internal/apps/reader/export.go",    // backup text unchanged by #356
		"internal/platform/admin/format.go", // admin page text unchanged by #356
		"internal/platform/db/timefmt.go",   // ParseTime reads both widths; also defines TimeLayout itself
	}
	if !slices.Equal(users, want) {
		t.Errorf("time.RFC3339/RFC3339Nano used in %v, want only %v", users, want)
	}
}
