package app_test

import (
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// fakeApp is a minimal App for exercising the framework.
type fakeApp struct {
	meta       app.Meta
	migrations fs.FS
	templates  fs.FS
	mount      func(*app.Router, app.Deps)
}

func (f fakeApp) Meta() app.Meta    { return f.meta }
func (f fakeApp) Migrations() fs.FS { return f.migrations }
func (f fakeApp) Templates() fs.FS  { return f.templates }
func (f fakeApp) Mount(r *app.Router, d app.Deps) {
	if f.mount != nil {
		f.mount(r, d)
		return
	}
	r.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {})
}

func newFake(id, name string, order int) fakeApp {
	return fakeApp{
		meta: app.Meta{ID: id, Name: name, Summary: "does things", Order: order},
		migrations: fstest.MapFS{
			"0001_init.sql": {Data: []byte("CREATE TABLE " + id + "_things (id INTEGER PRIMARY KEY);")},
		},
		templates: fstest.MapFS{
			"index.html": {Data: []byte(`{{define "content"}}hello{{end}}`)},
		},
	}
}

func TestMetaValidate(t *testing.T) {
	tests := []struct {
		name    string
		meta    app.Meta
		wantErr bool
	}{
		{"good", app.Meta{ID: "paste", Name: "ON Paste", Summary: "s"}, false},
		{"id with uppercase", app.Meta{ID: "Paste", Name: "ON Paste", Summary: "s"}, true},
		{"id with dash", app.Meta{ID: "on-paste", Name: "ON Paste", Summary: "s"}, true},
		{"id too short", app.Meta{ID: "p", Name: "ON Paste", Summary: "s"}, true},
		{"id starts with digit", app.Meta{ID: "1paste", Name: "ON Paste", Summary: "s"}, true},
		{"name missing prefix", app.Meta{ID: "paste", Name: "Paste", Summary: "s"}, true},
		{"name lowercase word", app.Meta{ID: "paste", Name: "ON paste", Summary: "s"}, true},
		{"no summary", app.Meta{ID: "paste", Name: "ON Paste"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.meta.Validate()
			if tt.wantErr && err == nil {
				t.Error("Validate accepted invalid meta")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Validate rejected valid meta: %v", err)
			}
		})
	}
}

func TestMetaPath(t *testing.T) {
	if got := (app.Meta{ID: "paste"}).Path(); got != "/paste/" {
		t.Errorf("Path() = %q, want /paste/", got)
	}
}

func TestRegistryOrdersByOrderThenID(t *testing.T) {
	reg, err := app.NewRegistry(
		newFake("reader", "ON Reader", 20),
		newFake("flash", "ON Flash", 10),
		newFake("notes", "ON Notes", 10),
	)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	var ids []string
	for _, item := range reg.NavItems() {
		ids = append(ids, item.ID)
	}
	if got, want := strings.Join(ids, ","), "flash,notes,reader"; got != want {
		t.Errorf("order = %q, want %q", got, want)
	}
}

func TestRegistryNavItems(t *testing.T) {
	reg, err := app.NewRegistry(newFake("paste", "ON Paste", 0))
	if err != nil {
		t.Fatal(err)
	}
	items := reg.NavItems()
	if len(items) != 1 {
		t.Fatalf("got %d items", len(items))
	}
	if items[0].Name != "ON Paste" || items[0].Path != "/paste/" {
		t.Errorf("item = %+v", items[0])
	}
}

func TestRegistryRejectsDuplicateAndInvalid(t *testing.T) {
	if _, err := app.NewRegistry(newFake("paste", "ON Paste", 0), newFake("paste", "ON Paste", 1)); err == nil {
		t.Error("NewRegistry accepted a duplicate id")
	}
	if _, err := app.NewRegistry(newFake("Paste", "ON Paste", 0)); err == nil {
		t.Error("NewRegistry accepted an invalid id")
	}
}

// TestRegistryMigrationsAreNamespaced proves two apps can each own a 0001.
func TestRegistryMigrationsAreNamespaced(t *testing.T) {
	reg, err := app.NewRegistry(newFake("paste", "ON Paste", 0), newFake("notes", "ON Notes", 1))
	if err != nil {
		t.Fatal(err)
	}
	ms, err := reg.Migrations()
	if err != nil {
		t.Fatalf("Migrations: %v", err)
	}
	if len(ms) != 2 {
		t.Fatalf("got %d migrations, want 2", len(ms))
	}

	keys := map[string]bool{}
	for _, m := range ms {
		keys[m.Key()] = true
	}
	for _, want := range []string{"notes:0001", "paste:0001"} {
		if !keys[want] {
			t.Errorf("missing migration %q; got %v", want, keys)
		}
	}
}

func TestRegistryEmptyIsUsable(t *testing.T) {
	reg, err := app.NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry with no apps: %v", err)
	}
	if len(reg.NavItems()) != 0 {
		t.Error("an empty registry produced nav items")
	}
	ms, err := reg.Migrations()
	if err != nil || len(ms) != 0 {
		t.Errorf("Migrations = %v, %v", ms, err)
	}
}
