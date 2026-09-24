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
func TestReadUploadTreatsARealFormFileErrorAsAFailure(t *testing.T) {
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

	tmpDir := t.TempDir()
	before, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range before {
		seen[e.Name()] = true
	}

	r := httptest.NewRequest("POST", "/", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	// A 0-byte memory limit forces the file part to spill straight to a
	// temp file rather than staying in memory.
	if err := r.ParseMultipartForm(0); err != nil {
		t.Fatalf("ParseMultipartForm: %v", err)
	}

	after, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var spilled string
	for _, e := range after {
		if !seen[e.Name()] && strings.Contains(e.Name(), "multipart") {
			spilled = filepath.Join(os.TempDir(), e.Name())
		}
	}
	if spilled == "" {
		t.Skip("the multipart part did not spill to a temp file on this platform/Go version; cannot reproduce a real FormFile error")
	}
	defer func() { _ = os.Remove(spilled) }() // no-op if the test already removed it
	if err := os.Rename(spilled, filepath.Join(tmpDir, "moved-out-from-under-it")); err != nil {
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
