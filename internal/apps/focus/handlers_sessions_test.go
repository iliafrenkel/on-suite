package focus_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// postSession sends a session the way record.js does: JSON, with the CSRF
// token in the header htmx also uses.
func postSession(t *testing.T, s *server, sess *apptest.Session, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/focus/sessions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(web.CSRFHeader, s.CSRFToken(t, sess))
	return s.Do(t, sess, req)
}

// sessionBody is a 50-minute completed round of timerID, as record.js sends it.
func sessionBody(clientID string, timerID int64, start time.Time) map[string]any {
	return map[string]any{
		"client_id": clientID, "timer_id": timerID, "timer_name": "Deep work", "color": "blue",
		"started_at": start.UnixMilli(), "ended_at": start.Add(50 * time.Minute).UnixMilli(),
		"focus_seconds": 3000, "rounds_done": 1, "completed": true,
	}
}

func recordedID(t *testing.T, rec *httptest.ResponseRecorder) int64 {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.ID == 0 {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return out.ID
}

func TestRecordingASession(t *testing.T) {
	s := newServer(t)
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	s.Clock.Set(now)
	tm := seedTimer(t, s, s.Alice.User.ID, validIntervals())
	start := now.Add(-time.Hour)

	rec := postSession(t, s, s.Alice, sessionBody("c-1", tm.ID, start))
	if rec.Code != http.StatusCreated {
		t.Fatalf("first POST = %d, want 201; body %s", rec.Code, rec.Body.String())
	}
	id := recordedID(t, rec)

	again := postSession(t, s, s.Alice, sessionBody("c-1", tm.ID, start))
	if again.Code != http.StatusOK {
		t.Fatalf("repeat POST = %d, want 200", again.Code)
	}
	if got := recordedID(t, again); got != id {
		t.Errorf("repeat returned id %d, want %d", got, id)
	}

	// Recording the same client id once more returns the stored row as it is.
	stored := storedSession(t, s, s.Alice.User.ID, "c-1")
	if stored.ID != id || stored.TimerID != tm.ID || stored.FocusSeconds != 3000 ||
		!stored.StartedAt.Equal(start) || !stored.Completed || stored.Color != "blue" {
		t.Errorf("stored %+v", stored)
	}
}

// storedSession reads back the session clientID names, through the store's
// idempotency: a repeat of a recorded client id returns the row unchanged.
func storedSession(t *testing.T, s *server, userID int64, clientID string) focus.Session {
	t.Helper()
	got, created, err := s.Store.RecordSession(context.Background(), userID, focus.SessionInput{
		ClientID: clientID, TimerName: "lookup", Color: "teal",
		StartedAt: s.Clock.Now().Add(-time.Hour), EndedAt: s.Clock.Now(), FocusSeconds: 60,
	})
	if err != nil || created {
		t.Fatalf("lookup of %q: created %v, err %v", clientID, created, err)
	}
	return got
}

func TestRecordingRefusesBadSessions(t *testing.T) {
	s := newServer(t)
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	s.Clock.Set(now)

	short := sessionBody("short", 0, now.Add(-time.Hour))
	short["focus_seconds"] = 59
	if rec := postSession(t, s, s.Alice, short); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("59 s of focus = %d, want 422", rec.Code)
	}
	future := sessionBody("future", 0, now.Add(time.Hour))
	if rec := postSession(t, s, s.Alice, future); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("starts in an hour = %d, want 422", rec.Code)
	}
	missing := sessionBody("missing", 0, now.Add(-time.Hour))
	delete(missing, "started_at")
	if rec := postSession(t, s, s.Alice, missing); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("no started_at = %d, want 422", rec.Code)
	}

	req := httptest.NewRequest("POST", "/focus/sessions", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(web.CSRFHeader, s.CSRFToken(t, s.Alice))
	if rec := s.Do(t, s.Alice, req); rec.Code != http.StatusBadRequest {
		t.Errorf("bad JSON = %d, want 400", rec.Code)
	}
}

func TestRecordingNeedsTheCSRFHeader(t *testing.T) {
	s := newServer(t)
	raw, _ := json.Marshal(sessionBody("c-1", 0, time.Now().Add(-time.Hour)))
	req := httptest.NewRequest("POST", "/focus/sessions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if rec := s.Do(t, s.Alice, req); rec.Code != http.StatusForbidden {
		t.Errorf("no CSRF header = %d, want 403", rec.Code)
	}
}

func TestRecordingStoresSomeoneElsesTimerAsNone(t *testing.T) {
	s := newServer(t)
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	s.Clock.Set(now)
	bobs := seedTimer(t, s, s.Bob.User.ID, validIntervals())
	if rec := postSession(t, s, s.Alice, sessionBody("c-1", bobs.ID, now.Add(-time.Hour))); rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d, want 201", rec.Code)
	}
	if got := storedSession(t, s, s.Alice.User.ID, "c-1"); got.TimerID != 0 {
		t.Errorf("timer_id = %d, want none", got.TimerID)
	}
}

func TestDeleteSessionFromHistory(t *testing.T) {
	s := newServer(t)
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	s.Clock.Set(now)
	in := sessionInput("c-1", "Reading", now.Add(-time.Hour), 30)
	sess, _, err := s.Store.RecordSession(context.Background(), s.Alice.User.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	path := "/focus/sessions/" + itoa(sess.ID) + "/delete"

	if rec := s.Post(t, s.Bob, path, url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("bob = %d, want 404", rec.Code)
	}
	if rec := s.Post(t, s.Alice, "/focus/sessions/abc/delete", url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("non-numeric id = %d, want 404", rec.Code)
	}
	s.Submit(t, s.Alice, path, url.Values{"page": {"3"}}, "/focus/history?page=3")
	if rec := s.Post(t, s.Alice, path, url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("deleting twice = %d, want 404", rec.Code)
	}

	// Page 1, no page, and junk all go back to the first page.
	for i, page := range []string{"", "1", "0", "x"} {
		other, _, err := s.Store.RecordSession(context.Background(), s.Alice.User.ID,
			sessionInput("p-"+itoa(int64(i)), "Reading", now.Add(-time.Hour), 30))
		if err != nil {
			t.Fatal(err)
		}
		s.Submit(t, s.Alice, "/focus/sessions/"+itoa(other.ID)+"/delete", url.Values{"page": {page}}, "/focus/history")
	}
}
