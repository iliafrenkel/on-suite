package apptest_test

import (
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apptest"
)

func TestClockFollowsRealTimeUntilSet(t *testing.T) {
	var c apptest.Clock
	before := time.Now().UTC()
	got := c.Now()
	after := time.Now().UTC()
	if got.Before(before) || got.After(after) {
		t.Fatalf("unset clock = %v, want between %v and %v", got, before, after)
	}
	if got.Location() != time.UTC {
		t.Errorf("unset clock location = %v, want UTC", got.Location())
	}
}

func TestClockSetAndAdvance(t *testing.T) {
	var c apptest.Clock
	pinned := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	c.Set(pinned)
	if got := c.Now(); !got.Equal(pinned) {
		t.Fatalf("Now after Set = %v, want %v", got, pinned)
	}
	c.Advance(90 * time.Minute)
	if got, want := c.Now(), pinned.Add(90*time.Minute); !got.Equal(want) {
		t.Fatalf("Now after Advance = %v, want %v", got, want)
	}
}

func TestClockSetConvertsToUTC(t *testing.T) {
	var c apptest.Clock
	c.Set(time.Date(2026, 3, 10, 23, 0, 0, 0, time.FixedZone("AEDT", 11*3600)))
	if got := c.Now(); got.Location() != time.UTC || got.Hour() != 12 {
		t.Fatalf("Now = %v, want 12:00 UTC", got)
	}
}
