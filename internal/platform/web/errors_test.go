package web_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/render"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
	"github.com/iliafrenkel/on-suite/internal/ui"
)

func testErrors(t *testing.T) (*web.Errors, *bytes.Buffer) {
	t.Helper()
	assets, err := web.NewAssets(ui.Static(), "/static")
	if err != nil {
		t.Fatal(err)
	}
	rend, err := render.NewRenderer(render.Options{Layouts: ui.Templates(), AssetURL: assets.URL, CSRFFieldName: web.CSRFFormField})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	return web.NewErrors(rend, slog.New(slog.NewJSONHandler(&buf, nil))), &buf
}

func TestErrorsStatusRendersAPage(t *testing.T) {
	e, _ := testErrors(t)
	rec := httptest.NewRecorder()
	e.Status(rec, httptest.NewRequest("GET", "/nope", nil), http.StatusNotFound)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	doc := htmlassert.Parse(t, rec.Body.String())
	doc.MustHave("header.shell-bar") // it is a full document
	if body := doc.Text(); !strings.Contains(body, "Not found") {
		t.Errorf("page does not say what went wrong: %q", body)
	}
}

// TestErrorsStatusReturnsAFragmentForHTMX: a swap must not receive a whole
// document, or the page ends up nested inside itself.
func TestErrorsStatusReturnsAFragmentForHTMX(t *testing.T) {
	e, _ := testErrors(t)
	req := httptest.NewRequest("GET", "/nope", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.Status(rec, req, http.StatusNotFound)

	body := rec.Body.String()
	if strings.Contains(body, "<html") || strings.Contains(body, "shell-bar") {
		t.Errorf("HTMX error response contains document chrome: %q", body)
	}
	if !strings.Contains(body, "Not found") {
		t.Errorf("fragment does not say what went wrong: %q", body)
	}
}

// TestErrorsInternalHidesTheCause is the important one: the operator sees the
// real error, the user does not.
func TestErrorsInternalHidesTheCause(t *testing.T) {
	e, logged := testErrors(t)
	rec := httptest.NewRecorder()

	e.Internal(rec, httptest.NewRequest("GET", "/x", nil),
		errors.New("connection to secret-host:5432 refused"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret-host") {
		t.Error("the internal error message leaked into the response")
	}
	if !strings.Contains(logged.String(), "secret-host") {
		t.Error("the internal error was not logged")
	}
}

func TestErrorsUnknownStatusFallsBackTo500Wording(t *testing.T) {
	e, _ := testErrors(t)
	rec := httptest.NewRecorder()
	e.Status(rec, httptest.NewRequest("GET", "/x", nil), http.StatusTeapot)

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want the requested 418", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Something broke") {
		t.Error("unmapped status did not fall back to generic wording")
	}
}

func TestRecoverTurnsAPanicIntoA500(t *testing.T) {
	e, logged := testErrors(t)
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	h := web.Recover(log, e)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("database credentials are wrong")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "credentials") {
		t.Error("the panic value leaked into the response")
	}
	if !strings.Contains(buf.String(), "credentials") {
		t.Error("the panic was not logged")
	}
	if !strings.Contains(buf.String(), "stack") {
		t.Error("no stack trace was logged")
	}
	_ = logged
}

// TestRecoverRepanicsOnErrAbortHandler: that value is the documented way for a
// handler to abandon a response on purpose, and must not be reported as a bug.
func TestRecoverRepanicsOnErrAbortHandler(t *testing.T) {
	e, _ := testErrors(t)
	h := web.Recover(slog.New(slog.DiscardHandler), e)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic(http.ErrAbortHandler)
		}))

	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Errorf("recovered %v, want ErrAbortHandler to propagate", v)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}
