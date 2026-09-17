package flash_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
)

func TestEnsureMediaURLCreatesAnUnfetchedRow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	hash, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, "https://example.com/cat.jpg")
	if err != nil {
		t.Fatalf("EnsureMediaURL: %v", err)
	}

	m, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatalf("MediaByHash: %v", err)
	}
	if m.Cached() {
		t.Error("a freshly-ensured URL row should not be cached yet")
	}
	if m.SourceURL != "https://example.com/cat.jpg" {
		t.Errorf("SourceURL = %q", m.SourceURL)
	}
	if m.Kind != flash.MediaKindImage {
		t.Errorf("Kind = %q, want image", m.Kind)
	}
}

func TestEnsureMediaURLIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	h1, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, "https://example.com/cat.jpg")
	if err != nil {
		t.Fatal(err)
	}
	h2, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, "https://example.com/cat.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("same URL produced different hashes: %q vs %q", h1, h2)
	}
}

func TestEnsureMediaURLDoesNotCollideAcrossKinds(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	imageHash, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, "https://example.com/shared.bin")
	if err != nil {
		t.Fatal(err)
	}
	audioHash, err := f.store.EnsureMediaURL(ctx, flash.MediaKindAudio, "https://example.com/shared.bin")
	if err != nil {
		t.Fatal(err)
	}
	if imageHash == audioHash {
		t.Fatal("the same URL used as image and audio produced the same hash — they will share one flash_media row")
	}

	img, err := f.store.MediaByHash(ctx, imageHash)
	if err != nil {
		t.Fatal(err)
	}
	if img.Kind != flash.MediaKindImage {
		t.Errorf("Kind = %q, want image", img.Kind)
	}
	aud, err := f.store.MediaByHash(ctx, audioHash)
	if err != nil {
		t.Fatal(err)
	}
	if aud.Kind != flash.MediaKindAudio {
		t.Errorf("Kind = %q, want audio", aud.Kind)
	}
}

func TestMediaByHashUnknownIsNotFound(t *testing.T) {
	f := newFixture(t)
	_, err := f.store.MediaByHash(context.Background(), strings.Repeat("0", 64))
	if !errors.Is(err, flash.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSaveMediaUploadStoresBytesImmediately(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	hash, err := f.store.SaveMediaUpload(ctx, flash.MediaKindAudio, "audio/mpeg", []byte("fake mp3 bytes"), time.Now().UTC())
	if err != nil {
		t.Fatalf("SaveMediaUpload: %v", err)
	}
	m, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Cached() {
		t.Error("an uploaded row should be cached immediately")
	}
	if m.SourceURL != "" {
		t.Errorf("SourceURL = %q, want empty for an upload", m.SourceURL)
	}
	if string(m.Bytes) != "fake mp3 bytes" {
		t.Errorf("Bytes = %q", m.Bytes)
	}
}

func TestSaveMediaBytesCachesAndClearsFailure(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	hash, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, "https://example.com/cat.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveMediaFailure(ctx, hash, "boom", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveMediaBytes(ctx, hash, "image/jpeg", []byte("jpeg-bytes"), time.Now().UTC()); err != nil {
		t.Fatalf("SaveMediaBytes: %v", err)
	}

	m, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Cached() {
		t.Error("should be cached after SaveMediaBytes")
	}
	if m.ErrorCount != 0 || m.LastError != "" {
		t.Errorf("failure was not cleared: count=%d last=%q", m.ErrorCount, m.LastError)
	}
}

func TestSaveMediaFailureIncrementsCount(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	hash, err := f.store.EnsureMediaURL(ctx, flash.MediaKindImage, "https://example.com/cat.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveMediaFailure(ctx, hash, "first failure", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveMediaFailure(ctx, hash, "second failure", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	m, err := f.store.MediaByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if m.ErrorCount != 2 {
		t.Errorf("ErrorCount = %d, want 2", m.ErrorCount)
	}
	if m.LastError != "second failure" {
		t.Errorf("LastError = %q, want %q", m.LastError, "second failure")
	}
}
