package apptest_test

import (
	"database/sql"
	"io/fs"
	"net/http"
	"testing"
	"testing/fstest"
	"time"

	"github.com/iliafrenkel/on-suite/internal/apptest"
	"github.com/iliafrenkel/on-suite/internal/platform/app"
	"github.com/iliafrenkel/on-suite/internal/platform/auth"
)

// PasswordHash is a hand-generated constant, so nothing else would notice if it
// stopped matching Password — every fixture user would simply fail to log in,
// and the failure would surface as a confusing 401 in some unrelated app's
// handler test rather than here.
func TestPasswordHashMatchesPassword(t *testing.T) {
	ok, err := auth.VerifyPassword(apptest.PasswordHash, apptest.Password)
	if err != nil {
		t.Fatalf("VerifyPassword: %v — PasswordHash is not a usable PHC string", err)
	}
	if !ok {
		t.Fatal("PasswordHash does not match Password; regenerate it with the snippet in its doc comment")
	}
}

// The cheap parameters are the whole point, so pin them: someone pasting a
// production-parameter hash in here would silently give back the 4m18s suite.
func TestPasswordHashUsesCheapParameters(t *testing.T) {
	const want = "$argon2id$v=19$m=64,t=1,p=1$"
	if len(apptest.PasswordHash) < len(want) || apptest.PasswordHash[:len(want)] != want {
		t.Errorf("PasswordHash parameters changed; want the cheap %q prefix, got %q",
			want, apptest.PasswordHash)
	}
}

// clockApp is the smallest possible app.App: its one route reports whatever
// clock deps.Now points at, so a test can tell NewServer actually wired it up
// (issue #357) rather than just not crashing.
type clockApp struct{}

func (clockApp) Meta() app.Meta    { return app.Meta{ID: "clockapp", Name: "ON Clockapp", Summary: "x"} }
func (clockApp) Migrations() fs.FS { return fstest.MapFS{} }
func (clockApp) Templates() fs.FS  { return fstest.MapFS{"x.html": &fstest.MapFile{}} }
func (clockApp) Mount(r *app.Router, d app.Deps) {
	r.HandleFunc("GET /{$}", func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(d.Now().Format("2006-01-02 15:04")))
	})
}

// clockStore is a minimal store implementing the SetClock capability
// NewServer looks for by type assertion, so the test can confirm the
// harness's own Store gets the same clock as the app.
type clockStore struct {
	now func() time.Time
}

func (c *clockStore) SetClock(now func() time.Time) { c.now = now }

func TestNewServerWiresSharedClockIntoDepsAndStore(t *testing.T) {
	s := apptest.NewServer(t, clockApp{}, func(handle *sql.DB) *clockStore {
		return &clockStore{}
	})

	pinned := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	s.Clock.Set(pinned)

	doc := s.Get(t, s.Alice, "/clockapp/")
	if got, want := doc.Text(), "2026-03-10 12:00"; got != want {
		t.Fatalf("GET /clockapp/ = %q, want %q", got, want)
	}

	if s.Store.now == nil {
		t.Fatal("s.Store.SetClock was never called")
	}
	if got := s.Store.now(); !got.Equal(pinned) {
		t.Fatalf("s.Store's clock = %v, want %v", got, pinned)
	}
}
