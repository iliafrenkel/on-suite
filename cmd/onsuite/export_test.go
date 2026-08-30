package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// seedDatabase creates a data directory with one account, using the real
// user-add path.
func seedDatabase(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	err := userAdd([]string{"ilia", "--data-dir", dir}, nil,
		stdinFrom(t, "a-sufficiently-long-password\n"), io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("userAdd: %v", err)
	}
	return dir
}

func TestExportCmdWritesJSONToStdout(t *testing.T) {
	dir := seedDatabase(t)

	var out, errOut bytes.Buffer
	if err := exportCmd([]string{"ilia", "--data-dir", dir}, nil, &out, &errOut); err != nil {
		t.Fatalf("exportCmd: %v", err)
	}

	var doc struct {
		Format     int            `json:"format"`
		User       string         `json:"user"`
		ExportedAt string         `json:"exported_at"`
		Apps       map[string]any `json:"apps"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if doc.Format != exportFormat {
		t.Errorf("format = %d, want %d", doc.Format, exportFormat)
	}
	if doc.User != "ilia" {
		t.Errorf("user = %q", doc.User)
	}
	if doc.ExportedAt == "" {
		t.Error("exported_at is empty")
	}
	if _, ok := doc.Apps["paste"]; !ok {
		t.Errorf("no paste key in apps: %v", doc.Apps)
	}
}

func TestExportCmdWritesToAFile(t *testing.T) {
	dir := seedDatabase(t)
	target := filepath.Join(t.TempDir(), "dump.json")

	var out, errOut bytes.Buffer
	if err := exportCmd([]string{"ilia", "--data-dir", dir, "--out", target}, nil, &out, &errOut); err != nil {
		t.Fatalf("exportCmd: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout got %d bytes when writing to a file", out.Len())
	}
	if !strings.Contains(errOut.String(), target) {
		t.Errorf("no confirmation naming the file: %q", errOut.String())
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	// An export holds everything the user has written.
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("file mode = %o, want 600", perm)
		}
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Error("the file does not contain valid JSON")
	}
}

func TestExportCmdRejectsBadInput(t *testing.T) {
	dir := seedDatabase(t)
	tests := []struct {
		name string
		args []string
	}{
		{"no username", []string{"--data-dir", dir}},
		{"two usernames", []string{"ilia", "someone", "--data-dir", dir}},
		{"unknown user", []string{"nobody", "--data-dir", dir}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := exportCmd(tt.args, nil, io.Discard, io.Discard); err == nil {
				t.Fatal("exportCmd succeeded, want an error")
			}
		})
	}
}
