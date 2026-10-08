package focus_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apps/focus"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

// exported mirrors the JSON shape onsuite export writes for ON Focus.
type exported struct {
	Timers []struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		Color        string `json:"color"`
		Kind         string `json:"kind"`
		FocusMinutes int    `json:"focus_minutes"`
		Rounds       int    `json:"rounds"`
		KeepHistory  bool   `json:"keep_history"`
	} `json:"timers"`
	Sessions []struct {
		ID           int64     `json:"id"`
		TimerID      *int64    `json:"timer_id"`
		TimerName    string    `json:"timer_name"`
		Color        string    `json:"color"`
		ClientID     string    `json:"client_id"`
		StartedAt    time.Time `json:"started_at"`
		FocusSeconds int       `json:"focus_seconds"`
		Completed    bool      `json:"completed"`
	} `json:"sessions"`
}

func TestExport(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deep, err := f.store.CreateTimer(ctx, f.alice.ID, validIntervals())
	if err != nil {
		t.Fatal(err)
	}
	gone, err := f.store.CreateTimer(ctx, f.alice.ID, focus.TimerInput{
		Name: "Old", Color: "pink", Kind: focus.KindSingle, FocusMinutes: 20, KeepHistory: true, Chime: "bell",
	})
	if err != nil {
		t.Fatal(err)
	}
	in := sessionInput("a", "Deep work", f.now.Add(-3*time.Hour), 50)
	in.TimerID, in.Color = deep.ID, "blue"
	if _, _, err := f.store.RecordSession(ctx, f.alice.ID, in); err != nil {
		t.Fatal(err)
	}
	old := sessionInput("b", "Old", f.now.Add(-2*time.Hour), 20)
	old.TimerID, old.Color = gone.ID, "pink"
	if _, _, err := f.store.RecordSession(ctx, f.alice.ID, old); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteTimer(ctx, f.alice.ID, gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.store.RecordSession(ctx, f.bob.ID, sessionInput("z", "Bob's", f.now.Add(-time.Hour), 30)); err != nil {
		t.Fatal(err)
	}

	payload, err := focus.New().Export(ctx, f.db, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "user_id") {
		t.Errorf("export carries user_id: %s", raw)
	}
	var out exported
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Timers) != 1 || out.Timers[0].ID != deep.ID || out.Timers[0].Name != "Deep work" ||
		out.Timers[0].Kind != "intervals" || out.Timers[0].Rounds != 4 || !out.Timers[0].KeepHistory {
		t.Errorf("timers = %+v", out.Timers)
	}
	if len(out.Sessions) != 2 {
		t.Fatalf("sessions = %+v, want Alice's two", out.Sessions)
	}
	first, second := out.Sessions[0], out.Sessions[1] // oldest first
	if first.TimerID == nil || *first.TimerID != deep.ID || first.TimerName != "Deep work" || first.FocusSeconds != 3000 {
		t.Errorf("first session = %+v", first)
	}
	if second.TimerID != nil || second.TimerName != "Old" || second.Color != "pink" || second.ClientID != "b" {
		t.Errorf("deleted timer's session = %+v", second)
	}
}

func TestExportForSomeoneWithNothing(t *testing.T) {
	f := newFixture(t)
	payload, err := focus.New().Export(context.Background(), f.db, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(payload)
	if string(raw) != `{"timers":[],"sessions":[]}` {
		t.Errorf("empty export = %s", raw)
	}
}

func TestAdminStats(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, uid := range []int64{f.alice.ID, f.bob.ID} {
		if _, err := f.store.CreateTimer(ctx, uid, validIntervals()); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := f.store.RecordSession(ctx, f.alice.ID, sessionInput("a", "x", f.now.Add(-3*time.Hour), 90)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.store.RecordSession(ctx, f.bob.ID, sessionInput("b", "y", f.now.Add(-2*time.Hour), 40)); err != nil {
		t.Fatal(err)
	}
	got, err := focus.New().Stats(ctx, f.db)
	if err != nil {
		t.Fatal(err)
	}
	want := []app.Stat{
		{Label: "Timers", Value: "2"},
		{Label: "Sessions recorded", Value: "2"},
		{Label: "Focus time", Value: "2h 10m", Hint: "recorded sessions, for everyone"},
	}
	if len(got) != len(want) {
		t.Fatalf("Stats = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("stat %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
