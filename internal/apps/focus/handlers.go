package focus

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/iliafrenkel/on-suite/internal/platform/web"
)

// userID is the signed-in user; every route is registered with HandleFunc.
func (a *App) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u, ok := web.UserFrom(r.Context())
	if !ok {
		a.deps.Errors.Status(w, r, http.StatusUnauthorized)
		return 0, false
	}
	return u.ID, true
}

// pathID parses the {id} path segment; anything but a positive integer is a
// 404, the same as a timer that isn't there.
func (a *App) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.deps.Errors.Status(w, r, http.StatusNotFound)
		return 0, false
	}
	return id, true
}

// fail maps a store error to its response: someone else's row is a 404,
// bad input a 400, anything else a logged 500.
func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		a.deps.Errors.Status(w, r, http.StatusNotFound)
	case errors.Is(err, ErrInvalid):
		a.deps.Errors.Status(w, r, http.StatusBadRequest)
	default:
		a.deps.Errors.Internal(w, r, err)
	}
}

// render draws a full page with the shell around it.
func (a *App) render(w http.ResponseWriter, r *http.Request, status int, name, title string, data any) {
	page := a.deps.Page(r, title)
	page.Data = data
	if err := a.deps.Render.Page(w, status, name, page); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}

// tileView is one timer tile on the home page.
type tileView struct {
	ID      int64
	Name    string
	Color   string
	Summary string // "15 min" or "50 / 10 × 4 · long 30"
	Total   string // the pill: "15 min", "3h 20m"
}

type indexView struct {
	UserID int64 // the resume banner only shows this user's session
	Today  *todayView
	Tiles  []tileView
}

// todayView is the home page's today strip (spec: "Home").
type todayView struct {
	Focus, Sessions, Week string
}

// newToday is the strip for t, or nil for someone who has never recorded
// a session: the strip only appears once there is history (F3 plan).
func newToday(t Totals) *todayView {
	if !t.Any {
		return nil
	}
	sessions := strconv.Itoa(t.TodaySessions) + " sessions"
	if t.TodaySessions == 1 {
		sessions = "1 session"
	}
	return &todayView{Focus: FormatFocus(t.Today), Sessions: sessions, Week: FormatFocus(t.Week)}
}

func newTile(t Timer) tileView {
	return tileView{
		ID: t.ID, Name: t.Name, Color: t.Color,
		Summary: Summary(t.TimerInput),
		Total:   FormatLength(TotalSeconds(Phases(t.TimerInput))),
	}
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	timers, err := a.store.Timers(r.Context(), userID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	totals, err := a.store.Totals(r.Context(), userID, a.store.now())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	view := indexView{UserID: userID, Today: newToday(totals)}
	for _, t := range timers {
		view.Tiles = append(view.Tiles, newTile(t))
	}
	a.render(w, r, http.StatusOK, "focus/index", "Timers", view)
}

func (a *App) duplicate(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if _, err := a.store.DuplicateTimer(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/focus/", http.StatusSeeOther)
}

func (a *App) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteTimer(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/focus/", http.StatusSeeOther)
}

func (a *App) newForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.userID(w, r); !ok {
		return
	}
	view := newFormView("/focus/timers", "New timer", "Create timer", valuesOf(DefaultInput()), nil)
	a.render(w, r, http.StatusOK, "focus/form", "New timer", view)
}

func (a *App) editForm(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	t, err := a.store.Timer(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	view := newFormView(fmt.Sprintf("/focus/timers/%d", id), "Edit timer", "Save changes", editValues(t), nil)
	a.render(w, r, http.StatusOK, "focus/form", "Edit timer", view)
}

func (a *App) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	in, values, errs := parseForm(r.PostFormValue)
	if len(errs) == 0 {
		_, err := a.store.CreateTimer(r.Context(), userID, in)
		if err == nil {
			http.Redirect(w, r, "/focus/", http.StatusSeeOther)
			return
		}
		var ve *ValidationError
		if !errors.As(err, &ve) {
			a.fail(w, r, err)
			return
		}
		errs = ve.Fields
	} else {
		errs = merge(errs, in.Normalize().Validate())
	}
	view := newFormView("/focus/timers", "New timer", "Create timer", values, errs)
	a.render(w, r, http.StatusUnprocessableEntity, "focus/form", "New timer", view)
}

func (a *App) update(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	// Someone else's timer is a 404 even when the form is also invalid.
	if _, err := a.store.Timer(r.Context(), userID, id); err != nil {
		a.fail(w, r, err)
		return
	}
	in, values, errs := parseForm(r.PostFormValue)
	if len(errs) == 0 {
		_, err := a.store.UpdateTimer(r.Context(), userID, id, in)
		if err == nil {
			http.Redirect(w, r, "/focus/", http.StatusSeeOther)
			return
		}
		var ve *ValidationError
		if !errors.As(err, &ve) {
			a.fail(w, r, err)
			return
		}
		errs = ve.Fields
	} else {
		errs = merge(errs, in.Normalize().Validate())
	}
	view := newFormView(fmt.Sprintf("/focus/timers/%d", id), "Edit timer", "Save changes", values, errs)
	a.render(w, r, http.StatusUnprocessableEntity, "focus/form", "Edit timer", view)
}

// order saves the tile order after a drag. ids is the comma-separated list
// home.js sends; anything unparseable is a 400, like a list the store
// refuses.
func (a *App) order(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	var ids []int64
	for _, part := range strings.Split(r.PostFormValue("ids"), ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || id <= 0 {
			a.deps.Errors.Status(w, r, http.StatusBadRequest)
			return
		}
		ids = append(ids, id)
	}
	if err := a.store.ReorderTimers(r.Context(), userID, ids); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// runPhase is one phase as focus.js reads it.
type runPhase struct {
	Phase
	Label string `json:"label"`
}

// runConfig is the timer as focus.js runs it, embedded in the page as JSON
// (spec: "The runner"). The browser copies it into its own state at Start,
// so editing the timer mid-session doesn't change a running one.
type runConfig struct {
	// UserID: the browser ignores a stored session that isn't this user's.
	UserID      int64      `json:"userId"`
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Color       string     `json:"color"`
	Chime       string     `json:"chime"`
	AutoAdvance bool       `json:"autoAdvance"`
	KeepHistory bool       `json:"keepHistory"`
	Rounds      int        `json:"rounds"` // 0 for a single timer
	Phases      []runPhase `json:"phases"`
}

// runView is the running page. Clock and Label are the first phase's, so
// the page looks right before focus.js takes over.
type runView struct {
	Config runConfig
	Name   string
	Color  string
	Single bool
	Clock  string
	Label  string
	Dots   []int // round numbers, interval timers only
}

func newRunView(t Timer, userID int64) runView {
	cfg := runConfig{
		UserID: userID, ID: t.ID, Name: t.Name, Color: t.Color, Chime: t.Chime,
		AutoAdvance: t.AutoAdvance, KeepHistory: t.KeepHistory, Rounds: t.Rounds,
	}
	for _, p := range Phases(t.TimerInput) {
		cfg.Phases = append(cfg.Phases, runPhase{Phase: p, Label: p.Label(t.Rounds)})
	}
	v := runView{
		Config: cfg, Name: t.Name, Color: t.Color, Single: t.Kind != KindIntervals,
		Clock: Clock(cfg.Phases[0].Seconds), Label: cfg.Phases[0].Label,
	}
	if !v.Single {
		for round := 1; round <= t.Rounds; round++ {
			v.Dots = append(v.Dots, round)
		}
	}
	return v
}

// run is the running page (spec: "Running page"). The server only draws
// it; focus.js runs the timer in the browser.
func (a *App) run(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	t, err := a.store.Timer(r.Context(), userID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, "focus/run", t.Name, newRunView(t, userID))
}

// today is the today strip alone, for home.js to refresh it after the
// banner records a session.
func (a *App) today(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.userID(w, r)
	if !ok {
		return
	}
	totals, err := a.store.Totals(r.Context(), userID, a.store.now())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.deps.Render.Fragment(w, http.StatusOK, "focus/index", "focus-today", newToday(totals)); err != nil {
		a.deps.Errors.Internal(w, r, err)
	}
}
