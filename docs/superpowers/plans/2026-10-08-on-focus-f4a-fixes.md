# ON Focus F4a — Fixes and robustness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the open ON Focus follow-ups before the F4b polish: History past the last page (#552), Retry after a failed Exit (#553), the rest of the running-page robustness nits (#545) and the F1 test gaps (#541).

**Architecture:** Small, independent changes. One store query (`SessionCount`) and a redirect in the History handler; tests and comments on the Go side; a handful of guarded calls in the browser scripts. No schema change, no new routes, no new state fields.

**Tech Stack:** Go 1.22+ `ServeMux`, `html/template`, SQLite (modernc), plain ES5-style JavaScript (no build step).

**Spec:** [docs/superpowers/specs/2026-10-07-on-focus-design.md](../specs/2026-10-07-on-focus-design.md) — "F4 addendum" → "F4a — fixes and robustness"; "Phases" (F4a row).

## Global Constraints

- Routes stay under `/focus/`; every class is prefixed `focus-`.
- CSP: no inline `<script>` code and no `style=""` attributes.
- `localStorage` key `onsuite.focus.session`, state version stays `v: 2`. No new state fields.
- Introduce Go helpers in the task that first calls them (staticcheck U1000).
- `internal/htmlassert` supports one qualifier per selector; for more, use `findEl` from `form_test.go`.
- go vet rejects unkeyed composite literals of another package's types in `_test` packages — key every `focus.SessionInput{…}` / `focus.TimerInput{…}` field.
- Day-keyed tests use fixed local times (`localAt` in `stats_test.go`); `main_test.go` pins `time.Local` to `Australia/Melbourne`.
- The CSRF form field is `csrf_token` (`web.CSRFFormField`).
- Full check must stay green on every commit:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- Work on branch `feat/focus-f4a-fixes` in a worktree (`../on-suite-focus-f4a`), never on `main`. Open the PR as `iliafrenkel` (`env -u GH_TOKEN gh …`, push with `env -u GIT_SSH_COMMAND`); never merge.

## Decisions made while planning (Ilia, 2026-10-08)

- F4 is two PRs: this one (fixes), then F4b (polish, guide, screenshots).
- **#552:** an empty History page after page 1 redirects (303) to the last page that has sessions, or to page 1 when there are none. `deleteSession` is unchanged: it still goes back to the page it came from, and that page redirects if it is now empty.
- **#553:** Retry repeats what the failed attempt was for: after a failed Exit, a successful Retry goes home.

## Other planning choices

- `SessionCount` is only called when a page past the first comes back empty, so a normal History load runs no extra query.
- #545's first bullet (`valid()` type checks) was already done in F3, and `askToNotify` already runs on every control click (`control()` in `focus.js`). This plan covers the rest of it.
- #541's bullet about `TestRunPageForASingleTimer` printing a count instead of the text no longer applies: that test was rewritten in F2 and every message prints what it found. Nothing to do; say so in the PR.
- No JavaScript test runner exists in the repo (F2/F3 verified scripts in the browser); the JS task is verified in the preview pane.

## File map

| File | Change |
|---|---|
| `internal/apps/focus/stats.go` | `SessionCount` |
| `internal/apps/focus/handlers_history.go` | redirect an empty page past the first |
| `internal/apps/focus/handlers_history_test.go` | paging-past-the-end tests |
| `internal/apps/focus/timer_test.go` | limits at the maximum |
| `internal/apps/focus/handlers_test.go` | ⋯ menu CSRF, duplicate non-numeric id, reorder unchanged, config escaping |
| `internal/apps/focus/handlers.go` | comment in `newRunView` |
| `internal/apps/focus/static/chimes.js` | try/catch in `play()` |
| `internal/apps/focus/static/home.js` | `Promise.resolve` around `requestPermission` |
| `internal/apps/focus/static/focus.js` | Space guard, notify on S/R/F, Retry keeps `leaving` |
| `internal/apps/focus/static/session.js` | comment in `startLabel` |

---

### Task 1: History past the last page (#552)

**Files:**
- Modify: `internal/apps/focus/stats.go` (after `RecentSessions`, end of file)
- Modify: `internal/apps/focus/handlers_history.go:127-131`
- Test: `internal/apps/focus/handlers_history_test.go`

**Interfaces:**
- Produces: `func (st *Store) SessionCount(ctx context.Context, userID int64) (int, error)` — how many sessions the user has recorded.

- [ ] **Step 1: Write the failing tests**

Append to `internal/apps/focus/handlers_history_test.go` (it already imports `context`, `fmt`, `strings`, `testing`, `time`, `focus`, `htmlassert`; add `"net/http"` and `"net/url"` to its imports):

```go
// redirectOf GETs path as Alice and returns where a 303 sends her.
func redirectOf(t *testing.T, s *server, path string) string {
	t.Helper()
	rec := s.Do(t, s.Alice, httptestGet(path))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET %s = %d, want 303", path, rec.Code)
	}
	return rec.Header().Get("Location")
}

func TestHistoryPastTheLastPageGoesToTheLastPage(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(localAt(10, 7, 12, 0))
	if got := redirectOf(t, s, "/focus/history?page=3"); got != "/focus/history" {
		t.Errorf("no sessions, page 3 → %q, want /focus/history", got)
	}
	for i := 0; i < 51; i++ {
		start := localAt(10, 7, 11, 0).Add(-time.Duration(i) * time.Hour)
		seedSession(t, s, s.Alice.User.ID, sessionInput(fmt.Sprintf("s%02d", i), "Reading", start, 30))
	}
	if got := redirectOf(t, s, "/focus/history?page=9"); got != "/focus/history?page=2" {
		t.Errorf("51 sessions, page 9 → %q, want /focus/history?page=2", got)
	}
	// Page 2 itself still renders.
	s.Get(t, s.Alice, "/focus/history?page=2").MustHave(".focus-sessions")
}

func TestDeletingTheLastSessionOnAPageLandsOnTheOneBefore(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(localAt(10, 7, 12, 0))
	var oldest focus.Session
	for i := 0; i < 51; i++ {
		start := localAt(10, 7, 11, 0).Add(-time.Duration(i) * time.Hour)
		oldest = seedSession(t, s, s.Alice.User.ID, sessionInput(fmt.Sprintf("s%02d", i), "Reading", start, 30))
	}
	// The 51st (oldest) session is alone on page 2.
	s.Submit(t, s.Alice, "/focus/sessions/"+itoa(oldest.ID)+"/delete", url.Values{"page": {"2"}}, "/focus/history?page=2")
	if got := redirectOf(t, s, "/focus/history?page=2"); got != "/focus/history" {
		t.Errorf("emptied page 2 → %q, want /focus/history", got)
	}
}

func TestSessionCount(t *testing.T) {
	s := newServer(t)
	s.Clock.Set(localAt(10, 7, 12, 0))
	ctx := context.Background()
	seedSession(t, s, s.Alice.User.ID, sessionInput("a", "Reading", localAt(10, 7, 8, 0), 30))
	seedSession(t, s, s.Alice.User.ID, sessionInput("b", "Reading", localAt(10, 6, 8, 0), 30))
	seedSession(t, s, s.Bob.User.ID, sessionInput("z", "Bob's", localAt(10, 7, 9, 0), 30))
	if n, err := s.Store.SessionCount(ctx, s.Alice.User.ID); err != nil || n != 2 {
		t.Errorf("Alice SessionCount = %d, %v; want 2", n, err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/focus/ -run 'TestHistoryPastTheLastPage|TestDeletingTheLastSessionOnAPage|TestSessionCount' -count=1`
Expected: build failure, `s.Store.SessionCount undefined`.

- [ ] **Step 3: Add `SessionCount`**

Append to `internal/apps/focus/stats.go`:

```go
// SessionCount is how many sessions the user has recorded: the History
// page uses it to find its last page.
func (st *Store) SessionCount(ctx context.Context, userID int64) (int, error) {
	var n int
	if err := st.db.QueryRowContext(ctx,
		`SELECT count(*) FROM focus_sessions WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("focus: count sessions: %w", err)
	}
	return n, nil
}
```

(`stats.go` already imports `context` and `fmt`.)

- [ ] **Step 4: Redirect an empty page past the first**

In `internal/apps/focus/handlers_history.go`, replace:

```go
	sessions, more, err := a.store.RecentSessions(ctx, userID, page, sessionsPerPage)
	if err != nil {
		a.fail(w, r, err)
		return
	}
```

with:

```go
	sessions, more, err := a.store.RecentSessions(ctx, userID, page, sessionsPerPage)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	// Past the end — a typed ?page=99, or the last session on this page
	// just deleted (#552): go to the last page that has sessions.
	if len(sessions) == 0 && page > 1 {
		n, err := a.store.SessionCount(ctx, userID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		http.Redirect(w, r, historyURL((n+sessionsPerPage-1)/sessionsPerPage), http.StatusSeeOther)
		return
	}
```

`historyURL` already maps anything ≤ 1 (including 0 for no sessions) to `/focus/history`.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/apps/focus/ -count=1`
Expected: PASS, including the existing `TestHistoryPagesOlderSessions` and `TestDeleteSessionFromHistory`.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/focus/stats.go internal/apps/focus/handlers_history.go internal/apps/focus/handlers_history_test.go
git commit -m "fix(focus): History past the last page goes to the last page (#552)"
```

---

### Task 2: Go test gaps and safety comments (#541, #545)

**Files:**
- Test: `internal/apps/focus/timer_test.go`
- Test: `internal/apps/focus/handlers_test.go`
- Modify: `internal/apps/focus/handlers.go` (`newRunView`)

**Interfaces:** none new. Uses `findEl` (form_test.go), `runConfigOf`, `seedTimer`, `single`, `validIntervals`, `itoa`, `names`.

- [ ] **Step 1: Limits at the maximum**

Append to `internal/apps/focus/timer_test.go`:

```go
// The limits themselves are allowed: one past them is tested in
// TestValidateRejectsBadInput.
func TestValidateAcceptsLimitsAtTheMaximum(t *testing.T) {
	in := validIntervals()
	in.FocusMinutes, in.BreakMinutes, in.LongBreakMinutes = 180, 60, 60
	in.Rounds, in.LongBreakEvery = 12, 12 // a long break every 12 rounds of 12: none
	if errs := in.Normalize().Validate(); errs != nil {
		t.Errorf("Validate() at the limits = %v, want nil", errs)
	}
}
```

- [ ] **Step 2: Each tile's ⋯ menu forms carry the CSRF field**

In `internal/apps/focus/handlers_test.go`, at the end of `TestIndexShowsTilesInOrder` (after the `a[href="/focus/run/…"]` check) add:

```go
	// Each tile's ⋯ menu: Duplicate and Delete are forms with the CSRF field.
	for _, tile := range tiles {
		id, _ := htmlassert.Attr(tile, "data-id")
		for _, action := range []string{"duplicate", "delete"} {
			doc.MustHave(`.focus-menu form[action="/focus/timers/` + id + `/` + action + `"] input[name="csrf_token"]`)
		}
	}
```

Each part of that selector has one qualifier, which `htmlassert` supports (the History tests use `.focus-pager a[href="…"]` the same way).

- [ ] **Step 3: A non-numeric id on duplicate**

In `TestDuplicateAndDeleteAreNotFoundForSomeoneElse`, replace:

```go
	if rec := s.Post(t, s.Alice, "/focus/timers/abc/delete", url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("non-numeric id = %d, want 404", rec.Code)
	}
```

with:

```go
	for _, action := range []string{"duplicate", "delete"} {
		if rec := s.Post(t, s.Alice, "/focus/timers/abc/"+action, url.Values{}); rec.Code != http.StatusNotFound {
			t.Errorf("non-numeric id on %s = %d, want 404", action, rec.Code)
		}
	}
```

- [ ] **Step 4: A rejected reorder leaves the order alone**

At the end of `TestReorderRejectsBadIDs` add:

```go
	ts, err := s.Store.Timers(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(ts), ""); got != "AB" {
		t.Errorf("order after rejected requests = %s, want AB", got)
	}
```

- [ ] **Step 5: The embedded config survives an awkward name (#545)**

Append to `internal/apps/focus/handlers_test.go`:

```go
// The running page embeds the timer as JSON inside <script>; a name that
// looks like markup must not end the script early or break the JSON.
func TestRunPageConfigEscapesTheName(t *testing.T) {
	s := newServer(t)
	name := `</script>"&<b>`
	tm := seedTimer(t, s, s.Alice.User.ID, single(name, 15))
	rec := s.Do(t, s.Alice, httptestGet("/focus/run/"+itoa(tm.ID)))
	if strings.Contains(rec.Body.String(), `</script>"&<b>`) {
		t.Error("the raw name appears unescaped in the page")
	}
	cfg := runConfigOf(t, htmlassert.Parse(t, rec.Body.String()))
	if cfg.Name != name {
		t.Errorf("config name = %q, want %q", cfg.Name, name)
	}
}
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/apps/focus/ -count=1`
Expected: PASS. These pin behaviour that already works; if one fails, it found a real bug — stop and report it rather than changing the test.

- [ ] **Step 7: Say why `newRunView` may index `Phases[0]`**

In `internal/apps/focus/handlers.go` `newRunView`, put a comment on the `v := runView{` line:

```go
	// Phases is never empty: Validate requires at least one focus minute.
	v := runView{
```

- [ ] **Step 8: Full check and commit**

Run the full check from Global Constraints. Expected: all green.

```bash
git add internal/apps/focus/timer_test.go internal/apps/focus/handlers_test.go internal/apps/focus/handlers.go
git commit -m "test(focus): close F1 test gaps and pin config escaping (#541, #545)"
```

---

### Task 3: Script robustness and Retry after Exit (#545, #553)

**Files:**
- Modify: `internal/apps/focus/static/chimes.js` (`play`)
- Modify: `internal/apps/focus/static/home.js` (the ▶ click handler)
- Modify: `internal/apps/focus/static/focus.js` (`record`, Retry listener, keydown handler)
- Modify: `internal/apps/focus/static/session.js` (`startLabel`)

**Interfaces:**
- Produces: `record(leaving)` in `focus.js` remembers `leaving` for Retry (internal to the file).

- [ ] **Step 1: `chimes.js` — a broken audio context never stops the timer**

Replace `play`:

```js
    function play(name) {
        var make = sounds[name];
        var c = context();
        if (!make || !c) return; // "silent", or no Web Audio
        // A chime is a nicety: a closed or broken audio context must never
        // throw into the timer that called it.
        try {
            if (c.state === "suspended") c.resume();
            var out = c.createGain();
            out.gain.value = 0.6;
            out.connect(c.destination);
            make(c, out, c.currentTime + 0.02);
        } catch (e) {
            console.warn("ON Focus: couldn't play the chime", e);
        }
    }
```

- [ ] **Step 2: `home.js` — old Safari's callback-only `requestPermission`**

In the ▶ click handler, replace:

```js
        Notification.requestPermission().then(go, go);
```

with:

```js
        // Old Safari's requestPermission takes a callback and returns
        // nothing; Promise.resolve keeps ▶ working there too.
        Promise.resolve(Notification.requestPermission()).then(go, go);
```

- [ ] **Step 3: `focus.js` — Retry repeats what the failed attempt was for (#553)**

Replace the `record` function and the Retry listener:

```js
    // record sends the ended session. It stays in localStorage until the
    // server has it, so Retry here — or the home page's banner later — can
    // try again. Retry repeats the attempt it follows: after a failed Exit,
    // a successful Retry goes home too (#553).
    var leavingOnSave = false;
    function record(leaving) {
        leavingOnSave = leaving;
        el.retry.hidden = true;
        if (!R.eligible(s)) {
            S.clear();
            el.doneStatus.textContent = s.keepHistory ? "Under a minute of focus — not added to your history." : "";
            return;
        }
        el.doneStatus.textContent = "Saving…";
        R.send(s).then(function (result) {
            if (result === "failed") {
                el.doneStatus.textContent = "Couldn't save this session.";
                el.retry.hidden = false;
                return;
            }
            S.clear();
            if (leaving) {
                window.location.assign("/focus/");
                return;
            }
            el.doneStatus.textContent = result === "saved" ? "Saved to your history." : "Couldn't add this session to your history.";
        });
    }
    el.retry.addEventListener("click", function () { record(leavingOnSave); });
```

`finish` calls `record(leaving)` with `undefined` when a session runs out; `leavingOnSave` then holds `undefined`, which is falsy — the same as `false`.

- [ ] **Step 4: `focus.js` — Space and S / R / F**

In the keydown handler, replace the `" "`, `"s"`, `"r"` and `"f"` cases:

```js
        case " ":
            // Space on a focused button presses that button, natively.
            // Links don't activate on Space, so on "← Exit" it pauses.
            if (e.target.closest && e.target.closest("button")) return;
            e.preventDefault();
            askToNotify();
            togglePause();
            break;
        case "s":
        case "S":
            askToNotify();
            if (el.skip) skip();
            break;
        case "r":
        case "R":
            askToNotify();
            restart();
            break;
        case "f":
        case "F":
            askToNotify();
            toggleFullscreen();
            break;
```

- [ ] **Step 5: `session.js` — say why `startLabel` may read `upcoming`**

Replace the comment above `startLabel`:

```js
    // startLabel names the button at a boundary. Only called while waiting,
    // and advance() never waits after the last phase, so upcoming(s) exists.
    function startLabel(s) {
```

- [ ] **Step 6: Go tests still pass**

Run: `go test ./internal/apps/focus/ -count=1`
Expected: PASS (`TestScriptsAreServed` serves the edited files).

- [ ] **Step 7: Verify in the browser**

Build and run on a scratch data dir (`go build -o /tmp/onsuite-bin ./cmd/onsuite`; `/tmp/onsuite-bin user add f4check --data-dir /tmp/onsuite-manual-verify` once — it asks for a password; save it in `/tmp/onsuite-manual-verify/f4check-credentials` and don't repeat it in chat). `.claude/launch.json` already has an `onsuite` entry for `/tmp/onsuite-bin` and that data dir; open it with `preview_start {name: "onsuite"}` and sign in. Make a 1-minute single timer ("Test", Keep history on) and a 2-round 1/1 intervals timer with auto-advance off.

1. **Retry after Exit (#553):** start "Test", wait past 60 s, then in the console replace `window.fetch` with a function returning `Promise.reject(new TypeError("offline"))` (keep the original in a variable; don't `delete window.fetch` — F3 lesson). Press Esc → End session: the Done screen says "Couldn't save this session." with Retry. Restore `window.fetch`, click Retry: the page goes to `/focus/` and History lists the session.
2. **Retry after running out:** same, but let the session finish on its own with fetch failing; Retry after restoring stays on the Done screen with "Saved to your history.".
3. **Space on Exit:** Tab to "← Exit", press Space: the timer pauses (it used to do nothing). Tab to Pause and press Space: pressed once, not twice.
4. **Keys still work:** S skips (intervals timer), R restarts, F toggles full screen where allowed, Esc asks to end.
5. **Chime failure:** in the console, `OnFocus.chimes.play("bell")` still plays; then make the context throw (`AudioContext.prototype.createGain = function () { throw new Error("x"); }`), call `OnFocus.chimes.play("bell")`: a console warning, no exception.
6. **▶ on the home page** still starts a timer (permission prompt can't show in the pane: it's denied there, F2 note).

`read_console_messages` with `onlyErrors`: none.

- [ ] **Step 8: Commit**

```bash
git add internal/apps/focus/static/chimes.js internal/apps/focus/static/home.js internal/apps/focus/static/focus.js internal/apps/focus/static/session.js
git commit -m "fix(focus): Retry after Exit goes home; harden chimes, keys and permission (#553, #545)"
```

---

### Task 4: Final check and PR

- [ ] **Step 1: Final full check**

Run the full check from Global Constraints. Expected: all green.

- [ ] **Step 2: Push and open the PR as Ilia**

```bash
env -u GIT_SSH_COMMAND git push -u origin feat/focus-f4a-fixes
env -u GH_TOKEN gh pr create --title "fix(focus): ON Focus F4a — fixes and robustness" --body "$(cat <<'EOF'
Closes #552. Closes #553. Closes #545. Closes #541. Part of #496.

## What
- History: a page past the end (a typed `?page=99`, or the last session on a page just deleted) redirects to the last page that has sessions (#552).
- Running page: after a failed Exit save, a successful Retry goes home like Exit (#553).
- Scripts (#545): chimes can't throw into the timer; `Promise.resolve` around `Notification.requestPermission()` for old Safari; Space pauses even with focus on "← Exit"; S / R / F also ask for notification permission; comments on `startLabel` and `newRunView`; a test that a name like `</script>"&` survives the embedded config.
- Tests (#541): limits at the maximum, CSRF on each tile's ⋯ forms, non-numeric id on duplicate, reorder unchanged after a rejected request.

## Notes
- #545's `valid()` bullet was done in F3; #541's `TestRunPageForASingleTimer` message bullet no longer applies (that test was rewritten in F2).

## Testing
- `go test ./... -race` and the full check are green.
- Browser checks from the plan: Retry after a failed Exit and after running out, Space on Exit, S/R/F/Esc, a throwing audio context.
EOF
)"
```

Expected: the PR URL. Do not merge.
