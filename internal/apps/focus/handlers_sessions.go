package focus

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// sessionBody is what record.js POSTs (spec: "Recording endpoint"). Times
// are ms since the epoch, as the browser keeps them.
type sessionBody struct {
	ClientID     string `json:"client_id"`
	TimerID      int64  `json:"timer_id"`
	TimerName    string `json:"timer_name"`
	Color        string `json:"color"`
	StartedAt    int64  `json:"started_at"`
	EndedAt      int64  `json:"ended_at"`
	FocusSeconds int    `json:"focus_seconds"`
	RoundsDone   int    `json:"rounds_done"`
	Completed    bool   `json:"completed"`
}

// sessionBodyMaxBytes is far more than a session summary needs.
const sessionBodyMaxBytes = 4 << 10

// fromMillis reads a browser timestamp; anything not positive is "missing",
// which the store refuses.
func fromMillis(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// recordSession stores a session the running page or the home banner
// reports. The browser clears its copy on any 2xx, so a repeat answers 200
// with the row it already has.
func (a *App) recordSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	var body sessionBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, sessionBodyMaxBytes)).Decode(&body); err != nil {
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
		return
	}
	s, created, err := a.store.RecordSession(r.Context(), userID, SessionInput{
		ClientID: body.ClientID, TimerID: body.TimerID, TimerName: body.TimerName, Color: body.Color,
		StartedAt: fromMillis(body.StartedAt), EndedAt: fromMillis(body.EndedAt),
		FocusSeconds: body.FocusSeconds, RoundsDone: body.RoundsDone, Completed: body.Completed,
	})
	if errors.Is(err, ErrInvalid) {
		a.deps.Errors.Status(w, r, http.StatusUnprocessableEntity)
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		ID int64 `json:"id"`
	}{s.ID})
}

// historyURL is the History page, on page (1 is the first).
func historyURL(page int) string {
	if page <= 1 {
		return "/focus/history"
	}
	return "/focus/history?page=" + strconv.Itoa(page)
}

// deleteSession removes one session from the History page and goes back
// to the page it was on.
func (a *App) deleteSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteSession(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	page, _ := strconv.Atoi(r.PostFormValue("page"))
	http.Redirect(w, r, historyURL(page), http.StatusSeeOther)
}
