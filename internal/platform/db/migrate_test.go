package db

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCollectOrdersAndParses(t *testing.T) {
	fsys := fstest.MapFS{
		"0002_second.sql":      {Data: []byte("CREATE TABLE b (id INTEGER PRIMARY KEY);")},
		"0001_first_thing.sql": {Data: []byte("CREATE TABLE a (id INTEGER PRIMARY KEY);")},
		"0010_tenth.sql":       {Data: []byte("CREATE TABLE c (id INTEGER PRIMARY KEY);")},
	}
	got, err := Collect("paste", fsys)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	wantIDs := []string{"0001", "0002", "0010"}
	if len(got) != len(wantIDs) {
		t.Fatalf("got %d migrations, want %d", len(got), len(wantIDs))
	}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Errorf("migration %d id = %q, want %q", i, got[i].ID, want)
		}
		if got[i].Namespace != "paste" {
			t.Errorf("migration %d namespace = %q, want paste", i, got[i].Namespace)
		}
	}
	if got[0].Name != "first_thing" {
		t.Errorf("name = %q, want first_thing", got[0].Name)
	}
	if got[0].Key() != "paste:0001" {
		t.Errorf("Key() = %q, want paste:0001", got[0].Key())
	}
}

func TestCollectRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		fsys fstest.MapFS
	}{
		{"unnumbered filename", fstest.MapFS{"init.sql": {Data: []byte("SELECT 1;")}}},
		{"too few digits", fstest.MapFS{"1_a.sql": {Data: []byte("SELECT 1;")}}},
		{"uppercase name", fstest.MapFS{"0001_Thing.sql": {Data: []byte("SELECT 1;")}}},
		{"non-sql file", fstest.MapFS{"notes.txt": {Data: []byte("hi")}}},
		{"duplicate id", fstest.MapFS{
			"0001_a.sql": {Data: []byte("SELECT 1;")},
			"0001_b.sql": {Data: []byte("SELECT 1;")},
		}},
		{"empty migration", fstest.MapFS{"0001_a.sql": {Data: []byte("   \n")}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Collect("platform", tt.fsys); err == nil {
				t.Fatal("Collect succeeded, want error")
			}
		})
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	handle, _ := open(t)
	ctx := context.Background()
	ms := []Migration{
		{Namespace: "platform", ID: "0001", Name: "first",
			SQL: "CREATE TABLE a (id INTEGER PRIMARY KEY);"},
		{Namespace: "platform", ID: "0002", Name: "second",
			SQL: "CREATE TABLE b (id INTEGER PRIMARY KEY);"},
	}

	n, err := Apply(ctx, handle, ms)
	if err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if n != 2 {
		t.Fatalf("first Apply ran %d migrations, want 2", n)
	}

	// Running the same set again must be a no-op, not an error.
	n, err = Apply(ctx, handle, ms)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if n != 0 {
		t.Errorf("second Apply ran %d migrations, want 0", n)
	}

	// A newly added migration runs on its own.
	ms = append(ms, Migration{Namespace: "platform", ID: "0003", Name: "third",
		SQL: "CREATE TABLE c (id INTEGER PRIMARY KEY);"})
	n, err = Apply(ctx, handle, ms)
	if err != nil {
		t.Fatalf("third Apply: %v", err)
	}
	if n != 1 {
		t.Errorf("third Apply ran %d migrations, want 1", n)
	}
}

// TestApplyNamespacesAreIndependent proves two apps can both own a 0001
// without colliding — the property that lets each app ship its own
// migrations without coordinating numbering.
func TestApplyNamespacesAreIndependent(t *testing.T) {
	handle, _ := open(t)
	ctx := context.Background()

	n, err := Apply(ctx, handle, []Migration{
		{Namespace: "paste", ID: "0001", Name: "snippets",
			SQL: "CREATE TABLE paste_snippets (id INTEGER PRIMARY KEY);"},
		{Namespace: "notes", ID: "0001", Name: "nodes",
			SQL: "CREATE TABLE notes_nodes (id INTEGER PRIMARY KEY);"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if n != 2 {
		t.Errorf("applied %d, want 2", n)
	}

	var keys int
	if err := handle.QueryRow(
		"SELECT count(*) FROM schema_migrations WHERE id = '0001'").Scan(&keys); err != nil {
		t.Fatal(err)
	}
	if keys != 2 {
		t.Errorf("recorded 0001 rows = %d, want 2", keys)
	}
}

// TestApplyRollsBackAFailedMigration is the important one: a broken
// migration must leave neither partial schema nor a record claiming success,
// so that fixing the file and rerunning works.
func TestApplyRollsBackAFailedMigration(t *testing.T) {
	handle, _ := open(t)
	ctx := context.Background()

	_, err := Apply(ctx, handle, []Migration{
		{Namespace: "platform", ID: "0001", Name: "good",
			SQL: "CREATE TABLE good (id INTEGER PRIMARY KEY);"},
		{Namespace: "platform", ID: "0002", Name: "broken",
			SQL: `CREATE TABLE half (id INTEGER PRIMARY KEY);
			      CREATE TABLE half (id INTEGER PRIMARY KEY);`}, // duplicate table
	})
	if err == nil {
		t.Fatal("Apply succeeded on a broken migration, want error")
	}
	if !strings.Contains(err.Error(), "platform:0002") {
		t.Errorf("error %q does not name the failing migration", err)
	}

	// The good migration stays applied.
	var good int
	if err := handle.QueryRow(
		"SELECT count(*) FROM schema_migrations WHERE key = 'platform:0001'").Scan(&good); err != nil {
		t.Fatal(err)
	}
	if good != 1 {
		t.Errorf("platform:0001 recorded %d times, want 1", good)
	}

	// The broken one is neither recorded nor partially applied.
	var bad int
	if err := handle.QueryRow(
		"SELECT count(*) FROM schema_migrations WHERE key = 'platform:0002'").Scan(&bad); err != nil {
		t.Fatal(err)
	}
	if bad != 0 {
		t.Errorf("broken migration recorded %d times, want 0", bad)
	}

	var halfTables int
	if err := handle.QueryRow(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='half'").Scan(&halfTables); err != nil {
		t.Fatal(err)
	}
	if halfTables != 0 {
		t.Errorf("table 'half' exists after rollback, want it absent")
	}
}
