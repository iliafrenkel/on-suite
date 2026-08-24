// Package arch contains no code. It holds one test that enforces the import
// boundaries the design depends on.
//
// These rules are stated in the spec and in both implementation plans. A rule
// that is only written down gets violated during a late-night change; this one
// fails the build.
package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
// in production code.
func TestHTMLAssertIsTestOnly(t *testing.T) {
	imports := scan(t)
	for pkg, deps := range imports.prod {
		if pkg == "internal/htmlassert" {
			continue
		}
		for _, dep := range deps {
			if dep == "internal/htmlassert" {
				t.Errorf("non-test code in %q imports internal/htmlassert", pkg)
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
