package flash

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadUploadTreatsARealFormFileErrorAsAFailure is the regression test
// for #302.2: readUpload used to return "no file" (nil, "") for any
// r.FormFile error, which silently swallowed a real read failure the same
// way as an absent file. This reproduces a real, non-ErrMissingFile,
// non-ErrNotMultipart error by forcing the "image" part to spill to a temp
// file (ParseMultipartForm's memory limit of 0 forces every file part to
// disk) and then deleting that temp file before FormFile ever opens it —
// the same failure mode a full disk or a concurrent cleanup would produce.
//
// The spill directory is a private one set via TMPDIR (t.Setenv), not the
// shared os.TempDir(): scanning the real system temp directory for a
// filename fragment was racy under `go test ./... -race -count=1`, since
// other tests and even other packages running in parallel can spill their
// own multipart-* files into the same directory at the same time. TMPDIR
// only takes effect for a fresh call to os.TempDir() — set it before
// ParseMultipartForm runs, since that is what os.CreateTemp (via
// mime/multipart) consults.
//
// t.Setenv forbids t.Parallel on this test, which is fine: it is the only
// test in this file.
func TestReadUploadTreatsARealFormFileErrorAsAFailure(t *testing.T) {
	spillDir := t.TempDir()
	t.Setenv("TMPDIR", spillDir)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("image", "cat.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte{0x89, 0x50, 0x4E, 0x47}); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("POST", "/", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	// A 0-byte memory limit forces the file part to spill straight to a
	// temp file rather than staying in memory.
	if err := r.ParseMultipartForm(0); err != nil {
		t.Fatalf("ParseMultipartForm: %v", err)
	}

	entries, err := os.ReadDir(spillDir)
	if err != nil {
		t.Fatal(err)
	}
	var spilled string
	for _, e := range entries {
		if strings.Contains(e.Name(), "multipart") {
			spilled = filepath.Join(spillDir, e.Name())
			break
		}
	}
	if spilled == "" {
		// Go is pinned by go.mod (see AGENTS.md), so this platform/version
		// combination is fixed for this repository: a missing spill file
		// means mime/multipart's disk-spill behavior changed underneath
		// this test's assumption, not a one-off environment fluke, and
		// deserves a hard failure rather than a silent skip.
		t.Fatal("the multipart part did not spill to a temp file in TMPDIR; the disk-spill assumption this test relies on broke")
	}
	if err := os.Rename(spilled, filepath.Join(spillDir, "moved-out-from-under-it")); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	up, msg := readUpload(w, r, MediaKindImage, "image", MaxImageFetchBytes)
	if up != nil {
		t.Error("readUpload returned a pendingUpload despite the underlying file being gone")
	}
	if msg != "That upload could not be read." {
		t.Errorf("message = %q, want the real-failure message, not the silent nil, \"\" a missing file gets", msg)
	}
}
