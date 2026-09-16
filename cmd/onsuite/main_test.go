package main

import (
	"strings"
	"testing"

	"github.com/iliafrenkel/on-suite/internal/platform/app"
)

func appIDs(apps []app.App) []string {
	ids := make([]string, len(apps))
	for i, a := range apps {
		ids[i] = a.Meta().ID
	}
	return ids
}

func TestFilterAppsKeepsEverythingWhenNothingDisabled(t *testing.T) {
	all := []app.App{
		fakeHomeApp{id: "notes", name: "ON Notes"},
		fakeHomeApp{id: "paste", name: "ON Paste"},
	}
	got, err := filterApps(all, nil)
	if err != nil {
		t.Fatalf("filterApps: %v", err)
	}
	if want := "notes,paste"; strings.Join(appIDs(got), ",") != want {
		t.Errorf("got %v, want %s", appIDs(got), want)
	}
}

func TestFilterAppsRemovesDisabledAppsInOrder(t *testing.T) {
	all := []app.App{
		fakeHomeApp{id: "notes", name: "ON Notes"},
		fakeHomeApp{id: "paste", name: "ON Paste"},
		fakeHomeApp{id: "reader", name: "ON Reader"},
	}
	got, err := filterApps(all, []string{"paste"})
	if err != nil {
		t.Fatalf("filterApps: %v", err)
	}
	if want := "notes,reader"; strings.Join(appIDs(got), ",") != want {
		t.Errorf("got %v, want %s", appIDs(got), want)
	}
}

func TestFilterAppsTreatsADuplicateDisabledIDAsHarmless(t *testing.T) {
	all := []app.App{
		fakeHomeApp{id: "notes", name: "ON Notes"},
		fakeHomeApp{id: "paste", name: "ON Paste"},
	}
	got, err := filterApps(all, []string{"paste", "paste"})
	if err != nil {
		t.Fatalf("filterApps: %v", err)
	}
	if want := "notes"; strings.Join(appIDs(got), ",") != want {
		t.Errorf("got %v, want %s", appIDs(got), want)
	}
}

func TestFilterAppsRejectsAnUnknownID(t *testing.T) {
	all := []app.App{fakeHomeApp{id: "notes", name: "ON Notes"}}
	_, err := filterApps(all, []string{"bogus"})
	if err == nil {
		t.Fatal("filterApps succeeded, want an error for an unknown app ID")
	}
	if !strings.Contains(err.Error(), "bogus") || !strings.Contains(err.Error(), "notes") {
		t.Errorf("error %q should name the bad ID and the known IDs", err)
	}
}

func TestFilterAppsRejectsDisablingEverything(t *testing.T) {
	all := []app.App{
		fakeHomeApp{id: "notes", name: "ON Notes"},
		fakeHomeApp{id: "paste", name: "ON Paste"},
	}
	_, err := filterApps(all, []string{"notes", "paste"})
	if err == nil {
		t.Fatal("filterApps succeeded, want an error when every app is disabled")
	}
}
