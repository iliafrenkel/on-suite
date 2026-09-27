package jobsadmin_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iliafrenkel/on-suite/internal/htmlassert"
	"github.com/iliafrenkel/on-suite/internal/platform/jobs"
)

// twoJobs is a registry with one enabled and one disabled job.
func twoJobs() *jobs.Registry {
	reg := jobs.NewRegistry()
	reg.Register("nightly thing", "does a nightly thing", time.Hour, func(context.Context) error { return nil })
	reg.Register("disabled thing", "would do a thing", 0, func(context.Context) error { return nil })
	return reg
}

func TestAnonymousIsSentToLoginFromTheJobsPage(t *testing.T) {
	s := newServer(t, twoJobs())
	for _, path := range []string{"/admin/jobs", "/admin/jobs/table"} {
		rec := s.get(t, nil, path)
		if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
			t.Errorf("GET %s: status = %d, Location = %q; want 303 to /login", path, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestANonAdminGetsTheSame404AsAMissingPageOnEveryJobsRoute(t *testing.T) {
	s := newServer(t, twoJobs())
	missing := s.get(t, s.plain, "/no-such-page")
	for _, path := range []string{"/admin/jobs", "/admin/jobs/table"} {
		got := s.get(t, s.plain, path)
		if got.Code != http.StatusNotFound || got.Body.String() != missing.Body.String() {
			t.Errorf("GET %s as non-admin = %d, want the missing-page 404", path, got.Code)
		}
	}

	missingPost := s.post(t, s.plain, "/no-such-page", url.Values{})
	rec := s.post(t, s.plain, "/admin/jobs/nightly-thing/run", url.Values{})
	if rec.Code != http.StatusNotFound || rec.Body.String() != missingPost.Body.String() {
		t.Errorf("POST run as non-admin = %d, want the missing-page 404", rec.Code)
	}
	if s.jobs.Snapshot()[0].Runs != 0 {
		t.Error("a non-admin's POST ran the job")
	}
}

func TestTheJobsPageListsEveryJobWithARunButton(t *testing.T) {
	s := newServer(t, twoJobs())
	rec := s.get(t, s.root, "/admin/jobs")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	d := doc(t, rec)
	body := d.Text()
	for _, want := range []string{"nightly thing", "does a nightly thing", "disabled thing", "disabled"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not mention %q", want)
		}
	}
	d.MustHave(`form[action="/admin/jobs/nightly-thing/run"]`)
	d.MustHave(`form[action="/admin/jobs/disabled-thing/run"]`)
	d.MustNotHave("[hx-trigger]")
}

func TestRunNowTriggersTheJobAndRedirectsBack(t *testing.T) {
	s := newServer(t, twoJobs())
	rec := s.post(t, s.root, "/admin/jobs/disabled-thing/run", url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/jobs" {
		t.Fatalf("status = %d, Location = %q; want 303 to /admin/jobs", rec.Code, rec.Header().Get("Location"))
	}
	s.jobs.Wait()

	st := s.jobs.Snapshot()[1]
	if st.Runs != 1 || !st.LastManual {
		t.Errorf("Runs = %d, LastManual = %v; want one manual run", st.Runs, st.LastManual)
	}
	if !strings.Contains(s.logs.String(), "job triggered") || !strings.Contains(s.logs.String(), "by=root") {
		t.Errorf("no audit log line for the trigger; logs:\n%s", s.logs.String())
	}

	page := doc(t, s.get(t, s.root, "/admin/jobs"))
	if !strings.Contains(page.Text(), "(manual)") {
		t.Error("the page does not mark the last run as manual")
	}
}

func TestRunNowWithoutACSRFTokenIsRejected(t *testing.T) {
	s := newServer(t, twoJobs())
	req := httptestPost("/admin/jobs/nightly-thing/run")
	rec := s.do(t, s.root, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	s.jobs.Wait()
	if s.jobs.Snapshot()[0].Runs != 0 {
		t.Error("a POST without a CSRF token ran the job")
	}
}

func TestRunNowOfAnUnknownJobIs404(t *testing.T) {
	s := newServer(t, twoJobs())
	if rec := s.post(t, s.root, "/admin/jobs/nope/run", url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestRunNowOfARunningJobIs422WithANotice(t *testing.T) {
	reg := jobs.NewRegistry()
	release := blockingJob(reg, "slow thing")
	s := newServer(t, reg)
	defer func() { close(release); reg.Wait() }()

	if rec := s.post(t, s.root, "/admin/jobs/slow-thing/run", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("first run = %d, want 303", rec.Code)
	}
	rec := s.post(t, s.root, "/admin/jobs/slow-thing/run", url.Values{})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("second run = %d, want 422", rec.Code)
	}
	notice := doc(t, rec).MustHave(".notice-error")
	if got := htmlassert.Text(notice); !strings.Contains(got, "slow thing is already running") {
		t.Errorf("notice = %q", got)
	}
}

func TestTheTablePollsOnlyWhileAJobIsRunning(t *testing.T) {
	reg := jobs.NewRegistry()
	release := blockingJob(reg, "slow thing")
	s := newServer(t, reg)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); reg.Wait() }()

	if err := reg.Trigger("slow-thing"); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/admin/jobs", "/admin/jobs/table"} {
		d := doc(t, s.get(t, s.root, path))
		poll := d.MustHave(`[hx-trigger]`)
		if v, _ := htmlassert.Attr(poll, "hx-get"); v != "/admin/jobs/table" {
			t.Errorf("GET %s: polling element hx-get = %q", path, v)
		}
		d.MustHave(".admin-tag-running")
		d.MustHave(`button[disabled]`)
	}

	unblock()
	reg.Wait()

	d := doc(t, s.get(t, s.root, "/admin/jobs/table"))
	d.MustNotHave("[hx-trigger]")
	d.MustHave(".admin-tag-ok")
}

func TestAFailedManualRunShowsItsError(t *testing.T) {
	reg := jobs.NewRegistry()
	reg.Register("broken thing", "", time.Hour, func(context.Context) error { return errors.New("disk on fire") })
	s := newServer(t, reg)

	s.post(t, s.root, "/admin/jobs/broken-thing/run", url.Values{})
	reg.Wait()

	d := doc(t, s.get(t, s.root, "/admin/jobs"))
	d.MustHave(".admin-tag-failed")
	if !strings.Contains(d.Text(), "disk on fire") {
		t.Error("the failed run's error text is not shown")
	}
}

// httptestPost builds a form POST with no CSRF token in it.
func httptestPost(path string) *http.Request {
	req := httptest.NewRequest("POST", path, strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}
