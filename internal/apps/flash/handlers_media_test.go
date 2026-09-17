package flash_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/flash"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/htmlassert"
)

// onePNG is the smallest thing http.DetectContentType calls an image/png.
var onePNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
}

// newServerWithApp is newServer, but it also hands back the App, so a test
// can call AllowPrivateFetchesForTest — the proxy's fetches go to an
// httptest origin on 127.0.0.1, which the SSRF guard refuses by
// construction. Mirrors internal/apps/reader's own newServerWithApp.
func newServerWithApp(t *testing.T) (*apptest.Server[*flash.Store], *flash.App) {
	t.Helper()
	a := flash.New()
	return apptest.NewServer(t, a, flash.NewStore), a
}

// seedCardWithImage creates a deck and a card carrying an unfetched
// URL-attached image, and returns the card and its image hash.
func seedCardWithImage(t *testing.T, s *apptest.Server[*flash.Store], srcURL string) (flash.Card, string) {
	t.Helper()
	ctx := context.Background()

	deck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Media deck", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := s.Store.EnsureMediaURL(ctx, flash.MediaKindImage, srcURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetCardMedia(ctx, s.Alice.User.ID, deck.ID, c.ID, flash.MediaKindImage, &hash); err != nil {
		t.Fatal(err)
	}
	return c, hash
}

func TestMediaFetchesCachesAndServes(t *testing.T) {
	s, a := newServerWithApp(t)
	var hits int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(onePNG)
	}))
	defer origin.Close()

	_, hash := seedCardWithImage(t, s, origin.URL+"/a.png")
	a.AllowPrivateFetchesForTest()

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("first request = %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("nosniff header missing")
	}

	rec2 := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("second request = %d", rec2.Code)
	}
	if hits != 1 {
		t.Errorf("origin was hit %d times; the second view must come from cache", hits)
	}
}

func TestMediaConditionalRequestReturns304(t *testing.T) {
	s, a := newServerWithApp(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(onePNG)
	}))
	defer origin.Close()

	_, hash := seedCardWithImage(t, s, origin.URL+"/a.png")
	a.AllowPrivateFetchesForTest()

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil))
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on first response")
	}

	req := httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil)
	req.Header.Set("If-None-Match", etag)
	rec2 := s.Do(t, s.Alice, req)
	if rec2.Code != http.StatusNotModified {
		t.Errorf("conditional request = %d, want 304", rec2.Code)
	}
}

func TestMediaRefusesAnUnknownHash(t *testing.T) {
	s := newServer(t)
	hash := strings.Repeat("0", 64)
	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown hash = %d, want 404", rec.Code)
	}
}

func TestMediaRefusesAMalformedHash(t *testing.T) {
	s := newServer(t)
	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/media/not-a-hash", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("malformed hash = %d, want 404", rec.Code)
	}
}

func TestMediaRejectsNonImageContent(t *testing.T) {
	s, a := newServerWithApp(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png") // lying
		_, _ = w.Write([]byte("<html><body>not an image</body></html>"))
	}))
	defer origin.Close()

	_, hash := seedCardWithImage(t, s, origin.URL+"/a.png")
	a.AllowPrivateFetchesForTest()

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("non-image content = %d, want 404", rec.Code)
	}
}

func TestMediaGivesUpPermanentlyPastAttemptCap(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	_, hash := seedCardWithImage(t, s, "http://127.0.0.1:1/unreachable")

	for i := 0; i < flash.MaxMediaFetchAttemptsForTest; i++ {
		if err := s.Store.SaveMediaFailure(ctx, hash, "boom", time.Now().UTC().Add(-2*flash.MediaRetryBackoffForTest)); err != nil {
			t.Fatal(err)
		}
	}

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("past the attempt cap = %d, want 404 (no further retry)", rec.Code)
	}
}

func TestMediaRefusesRetryWithinBackoffWindow(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	_, hash := seedCardWithImage(t, s, "http://127.0.0.1:1/unreachable")

	if err := s.Store.SaveMediaFailure(ctx, hash, "boom", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	rec := s.Do(t, s.Alice, httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("within backoff window = %d, want 404", rec.Code)
	}
}

func TestMediaRequiresSignIn(t *testing.T) {
	s := newServer(t)
	hash := strings.Repeat("0", 64)
	rec := s.Do(t, nil, httptest.NewRequest(http.MethodGet, "/flash/media/"+hash, nil))
	if rec.Code != 303 && rec.Code != 401 {
		t.Errorf("signed out = %d, want a redirect-to-login or 401", rec.Code)
	}
}

func TestUploadCardImageOverHTTP(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	deck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}

	path := "/flash/" + itoa(deck.ID) + "/cards/" + itoa(c.ID) + "/media"
	rec := s.UploadHX(t, s.Alice, path, "image", "cat.png", onePNG)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload = %d: %s", rec.Code, rec.Body.String())
	}

	updated, err := s.Store.CardByID(ctx, s.Alice.User.ID, deck.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ImageHash == nil {
		t.Fatal("ImageHash is nil after upload")
	}
	m, err := s.Store.MediaByHash(ctx, *updated.ImageHash)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Cached() {
		t.Error("an uploaded image should be cached immediately")
	}
}

func TestUploadCardImageRejectsWrongContentType(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	deck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}

	// An HTMX request renders its validation failure at 200 with a
	// .notice-error fragment, not a real 400 — htmx only swaps content on a
	// 2xx/3xx response, the same convention every other flash form follows.
	path := "/flash/" + itoa(deck.ID) + "/cards/" + itoa(c.ID) + "/media"
	rec := s.UploadHX(t, s.Alice, path, "image", "fake.png", []byte("<html>not an image</html>"))
	if rec.Code != http.StatusOK {
		t.Fatalf("wrong content type (HTMX) = %d, want 200 with a notice-error fragment", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave(".notice-error")

	updated, err := s.Store.CardByID(ctx, s.Alice.User.ID, deck.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ImageHash != nil {
		t.Error("ImageHash should not be set after a rejected upload")
	}
}

func TestUploadCardImageRejectsOversizedFile(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	deck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}

	// One byte over the per-field image cap (MaxImageFetchBytes = 5MB), but
	// still under the upload route's own MaxImageFetchBytes+MaxAudioFetchBytes
	// (15MB) override of the platform's global 1MB body cap (see flash.go's
	// Mount and web.LimitBody), so this request reaches the app's own
	// per-field size check in attachUpload rather than being rejected
	// earlier by the platform layer — the normal 200-with-notice-error HTMX
	// convention every other flash form follows.
	//
	// What matters here: the request must be rejected, one way or another,
	// before it is ever accepted and stored, and the card's ImageHash must
	// stay unset. That holds regardless of which layer does the rejecting,
	// and it is what this test asserts.
	oversized := make([]byte, flash.MaxImageFetchBytes+1)
	copy(oversized, onePNG)

	path := "/flash/" + itoa(deck.ID) + "/cards/" + itoa(c.ID) + "/media"
	rec := s.UploadHX(t, s.Alice, path, "image", "big.png", oversized)
	if rec.Code == http.StatusOK {
		doc := htmlassert.Parse(t, rec.Body.String())
		doc.MustHave(".notice-error")
	} else if rec.Code < 400 {
		t.Fatalf("oversized upload = %d, want a rejection (400s) or 200 with a notice-error fragment", rec.Code)
	}

	updated, err := s.Store.CardByID(ctx, s.Alice.User.ID, deck.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ImageHash != nil {
		t.Error("ImageHash should not be set after a rejected oversized upload")
	}
}

// TestUploadCardImageOverGlobalCapSucceeds is the regression test for the
// bug where the platform's global 1MB body cap (web.DefaultMaxBodyBytes,
// applied to every route by the shared middleware stack) ran ahead of this
// app's own per-route MaxImageFetchBytes+MaxAudioFetchBytes override,
// rejecting any upload over ~1MB with a generic platform-layer failure
// before ever reaching attachUpload's real 5MB image limit or its
// friendly "That file is larger than the NMB limit." message. A ~2MB file
// sits strictly between the old 1MB global cap and the real 5MB per-image
// limit, so it only succeeds once the route's own LimitBody override (see
// flash.go's Mount) is in effect.
func TestUploadCardImageOverGlobalCapSucceeds(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	deck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}

	// 2MB: well over the old global 1MB cap, well under the real 5MB
	// per-image limit. The PNG magic-byte header goes first so
	// http.DetectContentType still sniffs this as image/png, same as
	// TestUploadCardImageRejectsOversizedFile's padding trick above.
	const size = 2 << 20
	big := make([]byte, size)
	copy(big, onePNG)

	path := "/flash/" + itoa(deck.ID) + "/cards/" + itoa(c.ID) + "/media"
	rec := s.UploadHX(t, s.Alice, path, "image", "big.png", big)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload = %d: %s", rec.Code, rec.Body.String())
	}

	updated, err := s.Store.CardByID(ctx, s.Alice.User.ID, deck.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ImageHash == nil {
		t.Fatal("ImageHash is nil after a ~2MB upload that should have succeeded")
	}
}

func TestUploadCardMediaRequiresCSRF(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	deck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}

	req := httpPost(t, "/flash/"+itoa(deck.ID)+"/cards/"+itoa(c.ID)+"/media", nil)
	rec := s.Do(t, s.Alice, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("upload without CSRF = %d, want 403", rec.Code)
	}
}

func TestRemoveCardImageOverHTTP(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	deck, err := s.Store.CreateDeck(ctx, s.Alice.User.ID, "Spanish", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Store.CreateCard(ctx, s.Alice.User.ID, deck.ID, flash.CardTypeBasic, "Q", "A", "")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := s.Store.SaveMediaUpload(ctx, flash.MediaKindImage, "image/png", onePNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SetCardMedia(ctx, s.Alice.User.ID, deck.ID, c.ID, flash.MediaKindImage, &hash); err != nil {
		t.Fatal(err)
	}

	s.Submit(t, s.Alice, "/flash/"+itoa(deck.ID)+"/cards/"+itoa(c.ID)+"/media",
		url.Values{"remove_image": {"1"}}, "/flash/"+itoa(deck.ID)+"/cards/"+itoa(c.ID))

	updated, err := s.Store.CardByID(ctx, s.Alice.User.ID, deck.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ImageHash != nil {
		t.Error("ImageHash still set after removal")
	}
}
