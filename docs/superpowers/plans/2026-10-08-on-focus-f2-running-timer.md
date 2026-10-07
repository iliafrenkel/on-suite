# ON Focus F2 — Running a timer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace F1's placeholder running page with the real one: a focus-mode view with a progress ring, round dots, Pause / Skip / Restart / Full screen / Exit with keyboard shortcuts, auto-advance or waiting at phase boundaries, synthesised chimes, notifications, a live tab title, `localStorage` resume, and a resume banner on the home page.

**Architecture:** The server only renders the page and embeds the timer and its expanded phase list as JSON (`<script type="application/json">`). Three plain scripts do the rest: `session.js` is the pure state and time maths (no DOM, shared by the running page and the home page), `chimes.js` synthesises the sounds with Web Audio (shared by the running page and the timer form's ▶ preview), and `focus.js` drives the running page. Focus mode hides the suite chrome with CSS `:has()`, exactly as ON Later's reading view does.

**Tech Stack:** Go 1.22+ `ServeMux`, `html/template`, plain ES5-style JavaScript (no build step), Web Audio API, Notifications API, Fullscreen API, `localStorage`.

**Spec:** [docs/superpowers/specs/2026-10-07-on-focus-design.md](../specs/2026-10-07-on-focus-design.md) — sections "The runner (`focus.js`)", "Running page", "Resume", "Phases" (F2 row).

## Global Constraints

- Routes stay under `/focus/`; every class is prefixed `focus-` (plus the shared `swatch-c-*`).
- CSP: no inline `<script>` code and no `style=""` attributes. A `<script type="application/json">` data block is fine. Scripts may set SVG attributes and toggle classes.
- Scripts are served by `(*App).script` (sign-in required, `Cache-Control: no-cache`) and loaded with `defer` from a template's `head` block, in dependency order: `session.js`, then `chimes.js`, then the page script.
- `localStorage` key: `onsuite.focus.session`. State version field `v: 1`. One running session per browser.
- Time left is always derived from `Date.now()` and stored timestamps; ticks only redraw.
- Countdown format (Go `Clock` and JS `clock` must agree): `05:00`, `50:00`, `1:30:00`.
- Tab title: `31:12 · Deep work` (focus), `07:40 · Break` (any break), `Paused · Deep work`, `Ready · Deep work` (waiting at a boundary), `Done · Deep work`.
- Notification texts: `Deep work — break time`, `Deep work — round 3`, `Deep work — done`. Only while the tab is hidden and permission is granted.
- Phase-end chime plays once per tick however many phases ended.
- Recording sessions is **F3 (#495)**: in F2, a finished or exited session is simply cleared from `localStorage`. Each such spot carries a comment naming #495 so F3 can find it.
- Dialogs live outside any `.stack` (F1 lesson: `.stack > * + *` overrides a modal dialog's `margin: auto`).
- A rule that sets `display` on an element that also uses `hidden` must be paired with a `[hidden]` rule (the ON Reader favicons lesson).
- Introduce Go helpers in the task that first calls them (staticcheck U1000).
- `internal/htmlassert` supports one qualifier per selector; for more, use `findEl` from `form_test.go`.
- Full check must stay green on every commit:
  ```bash
  gofmt -l .
  go vet ./...
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.0 ./...
  go mod tidy && git diff --exit-code go.mod go.sum
  go test ./... -race -count=1
  ```
- Work on branch `feat/focus-f2-running-timer` in a worktree, never on `main`. Open the PR as `iliafrenkel` (`env -u GH_TOKEN gh …`); never merge.

## Decisions made while planning (Ilia, 2026-10-08)

- **Focus mode** hides the chrome the way ON Later does: `body:has(.focus-runner) .shell-bar, .app-sidebar, .app-footer { display: none }` and `main:has(.focus-runner) { max-width: none; padding: 0 }`. No platform change.
- **Sound unlock:** ▶ on the home page navigates to a new page, so audio may still be locked when the timer starts. The timer starts at once as specced; if audio is locked, the page shows "Sound is off — click anywhere to turn it on", and the first click or key unlocks it. Notification permission is asked in the ▶ click on the home page (waiting for the answer before navigating, or the prompt would vanish), and again on the first control click if still undecided.
- **Before F3:** the Done screen shows "Done — 3h 20m focused" with a Back button; Exit asks "End this session?". Both clear `localStorage`. Nothing is recorded until F3 adds the POST in the same places.

## Other planning choices

- The phase label under the countdown uses F1's `Phase.Label`: "Focus · round 2 of 4", "Short break", "Long break" (the mockup showed just "Focus"; the round number helps screen-reader users, for whom the dots are hidden). While waiting at a boundary it reads "Round 2 done" or "Break over".
- **Skip** on the last phase ends the session as *not completed* (it didn't run to its end). **Skip** while waiting starts the next phase. **Restart** is hidden while waiting.
- **Skip** and **Restart** bank the focus time already done in the current phase ("focus time actually done").
- Skipping always starts the next phase straight away, even with auto-advance off — the person asked to move on.
- A session for *another* timer that has already run out is replaced without asking (nothing to record until F3).
- The state carries two fields beyond the spec's list, which F3 will need: `roundsDone`, `endedAt`, plus `completed` and `finished`.
- Background tabs: Chrome throttles hidden-tab intervals to once a minute after five minutes, so a notification can be up to a minute late. Accepted for F2; F4's Wake Lock work can revisit it.

## File map

| File | Responsibility |
|---|---|
| `internal/apps/focus/static/session.js` | create / advance / pause / resume / skip / restart / next; time left, progress, focus seconds; labels; clock; `localStorage` load/save/clear |
| `internal/apps/focus/static/chimes.js` | Web Audio chimes `bell`, `bowl`, `soft`; locked/unlock; the form's ▶ preview |
| `internal/apps/focus/static/focus.js` | running page: start/resume/replace, tick, render, controls, keys, dialog, full screen, notifications, sound hint, Done |
| `internal/apps/focus/static/home.js` | add: resume banner; ask notification permission on ▶ |
| `internal/apps/focus/focus.go` | script routes for the three new files |
| `internal/apps/focus/phases.go` | add `Clock` |
| `internal/apps/focus/handlers.go` | `runConfig`, `runPhase`, `runView`, `newRunView`; `run` renders the real page |
| `internal/apps/focus/templates/run.html` | running page markup |
| `internal/apps/focus/templates/index.html` | load `session.js`; banner slot |
| `internal/apps/focus/templates/form.html` | load `chimes.js`; ▶ preview button |
| `internal/ui/static/app.css` | "ON Focus" section: running page and banner |
| `internal/apps/focus/*_test.go` | tests |
| `docs/user/focus.md` | "Running a timer" section |
| `docs/superpowers/specs/2026-10-07-on-focus-design.md` | record the decisions above |

---

### Task 0: Worktree and branch

- [ ] **Step 1: Create the worktree**

```bash
cd /Users/iliaf/src/WEB/on-suite
git fetch origin
git worktree add ../on-suite-focus-f2 -b feat/focus-f2-running-timer origin/main
cd ../on-suite-focus-f2
go build ./cmd/onsuite
```
Expected: builds with no output. All later commands run in `../on-suite-focus-f2`.

---

### Task 1: The session engine (`session.js`)

**Files:**
- Create: `internal/apps/focus/static/session.js`
- Modify: `internal/apps/focus/focus.go` (route)
- Test: `internal/apps/focus/handlers_test.go`

**Interfaces:**
- Consumes: the run page's config JSON shape (built in Task 3): `{id, name, color, chime, autoAdvance, keepHistory, rounds, phases: [{kind, seconds, round, label}]}`.
- Produces: `window.OnFocus.session` with
  - `create(config, now) → state`
  - `advance(s, now) → number` (phases that ended)
  - `pause(s, now)`, `resume(s, now)`, `skip(s, now)`, `restart(s, now)`, `next(s, now)` — callers must call `advance(s, now)` first
  - `current(s) → phase`, `upcoming(s) → phase|undefined`
  - `elapsed(s, now)`, `remaining(s, now)` (ms), `progress(s, now)` (0–1), `focusSeconds(s, now)`
  - `startLabel(s)`, `notice(s)`, `clock(ms)`, `focused(seconds)`
  - `load() → state|null`, `save(s)`, `clear()`
  - State fields: `v, clientId, timerId, timerName, color, chime, autoAdvance, keepHistory, rounds, phases, startedAt, endedAt, phaseIndex, phaseStartedAt, pausedAt, pausedTotalInPhase, focusSecondsBanked, roundsDone, waiting, finished, completed`. Times are ms since the epoch; `pausedAt` and `endedAt` are `null` when not set.
  - Route `GET /focus/session.js`.

- [ ] **Step 1: Write the failing route test**

Append to `internal/apps/focus/handlers_test.go`:

```go
func TestScriptsAreServed(t *testing.T) {
	s := newServer(t)
	for _, name := range []string{
		"home.js",
		"session.js",
	} {
		rec := s.Do(t, s.Alice, httptestGet("/focus/"+name))
		if rec.Code != http.StatusOK {
			t.Errorf("GET /focus/%s = %d, want 200", name, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
			t.Errorf("GET /focus/%s Content-Type = %q", name, ct)
		}
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/apps/focus/ -run TestScriptsAreServed -count=1`
Expected: FAIL — `GET /focus/session.js = 404` (no route).

- [ ] **Step 3: Write `session.js`**

`internal/apps/focus/static/session.js`:

```js
// ON Focus's running-session engine (spec: "The runner (focus.js)" —
// "State and timing"). Pure state and time maths, shared by the running
// page (focus.js) and the home page's resume banner (home.js); nothing here
// touches the page.
//
// Time left is always derived from Date.now() and the stored timestamps,
// never counted down, so a throttled background tab never drifts. Every
// function takes `now` (ms since the epoch) rather than reading the clock,
// which keeps the maths checkable by hand.
"use strict";

(function () {
	var KEY = "onsuite.focus.session";
	var VERSION = 1;

	function current(s) { return s.phases[s.phaseIndex]; }
	function upcoming(s) { return s.phases[s.phaseIndex + 1]; }
	function isLast(s) { return s.phaseIndex === s.phases.length - 1; }

	function newID() {
		if (window.crypto && window.crypto.randomUUID) return window.crypto.randomUUID();
		// randomUUID needs a secure context; a self-hosted suite on plain
		// http on the LAN doesn't have one.
		return Date.now().toString(36) + "-" + Math.random().toString(36).slice(2);
	}

	// create starts a session of the page's timer. Settings are copied in
	// here, so editing the timer mid-session doesn't change this one.
	function create(config, now) {
		return {
			v: VERSION,
			clientId: newID(),
			timerId: config.id,
			timerName: config.name,
			color: config.color,
			chime: config.chime,
			autoAdvance: config.autoAdvance,
			keepHistory: config.keepHistory,
			rounds: config.rounds,
			phases: config.phases,
			startedAt: now,
			endedAt: null,
			phaseIndex: 0,
			phaseStartedAt: now,
			pausedAt: null,
			pausedTotalInPhase: 0,
			focusSecondsBanked: 0,
			roundsDone: 0,
			waiting: false,
			finished: false,
			completed: false
		};
	}

	// elapsed is how much of the current phase has run, in ms, pauses
	// excluded. A phase waiting at its end or a finished session counts as
	// fully run.
	function elapsed(s, now) {
		var length = current(s).seconds * 1000;
		if (s.waiting || s.finished) return length;
		var until = s.pausedAt !== null ? s.pausedAt : now;
		return Math.min(length, Math.max(0, until - s.phaseStartedAt - s.pausedTotalInPhase));
	}

	function remaining(s, now) { return current(s).seconds * 1000 - elapsed(s, now); }

	function progress(s, now) { return elapsed(s, now) / (current(s).seconds * 1000); }

	// advance walks s forward to now: every phase whose time has run out
	// ends, in order, as it would have with the tab open. It stops after the
	// last phase, at the first boundary when auto-advance is off, and while
	// paused. It returns how many phases ended, so the caller chimes once
	// however many were missed (spec: "Phase end").
	function advance(s, now) {
		var ended = 0;
		while (!s.finished && !s.waiting && s.pausedAt === null) {
			var p = current(s);
			var end = s.phaseStartedAt + s.pausedTotalInPhase + p.seconds * 1000;
			if (now < end) break;
			ended++;
			if (p.kind === "focus") {
				s.focusSecondsBanked += p.seconds;
				s.roundsDone++;
			}
			if (isLast(s)) {
				s.finished = true;
				s.completed = true;
				s.endedAt = end;
			} else if (!s.autoAdvance) {
				s.waiting = true;
			} else {
				s.phaseIndex++;
				s.phaseStartedAt = end;
				s.pausedTotalInPhase = 0;
			}
		}
		return ended;
	}

	function startPhase(s, index, now) {
		s.phaseIndex = index;
		s.phaseStartedAt = now;
		s.pausedTotalInPhase = 0;
		s.pausedAt = null;
		s.waiting = false;
	}

	// bankCurrent counts the focus time done so far in the current phase:
	// Skip and Restart keep time actually focused (spec: "Controls").
	function bankCurrent(s, now) {
		if (current(s).kind === "focus" && !s.waiting && !s.finished) {
			s.focusSecondsBanked += Math.floor(elapsed(s, now) / 1000);
		}
	}

	function pause(s, now) {
		if (s.finished || s.waiting || s.pausedAt !== null) return;
		s.pausedAt = now;
	}

	function resume(s, now) {
		if (s.pausedAt === null) return;
		s.pausedTotalInPhase += now - s.pausedAt;
		s.pausedAt = null;
	}

	// next starts the phase after a boundary the session is waiting at
	// ("Start break", "Start round 3").
	function next(s, now) {
		if (!s.waiting) return;
		startPhase(s, s.phaseIndex + 1, now);
	}

	// skip ends the current phase now and starts the next one straight
	// away. Skipping the last phase ends the session, not completed — it
	// didn't run to its end. A skipped focus round isn't counted as done.
	function skip(s, now) {
		if (s.finished) return;
		if (s.waiting) {
			next(s, now);
			return;
		}
		bankCurrent(s, now);
		if (isLast(s)) {
			s.finished = true;
			s.completed = false;
			s.endedAt = now;
			return;
		}
		startPhase(s, s.phaseIndex + 1, now);
	}

	// restart runs the current phase again from its start, unpaused.
	function restart(s, now) {
		if (s.finished || s.waiting) return;
		bankCurrent(s, now);
		startPhase(s, s.phaseIndex, now);
	}

	// focusSeconds is the focus time done so far, pauses and breaks
	// excluded.
	function focusSeconds(s, now) {
		var n = s.focusSecondsBanked;
		if (current(s).kind === "focus" && !s.waiting && !s.finished) {
			n += Math.floor(elapsed(s, now) / 1000);
		}
		return n;
	}

	// startLabel names the button at a boundary.
	function startLabel(s) {
		var p = upcoming(s);
		if (p.kind === "break") return "Start break";
		if (p.kind === "long_break") return "Start long break";
		return "Start round " + p.round;
	}

	// notice is the phase-end notification: what's happening now.
	function notice(s) {
		if (s.finished) return s.timerName + " — done";
		var p = s.waiting ? upcoming(s) : current(s);
		if (p.kind === "focus") return s.timerName + " — round " + p.round;
		return s.timerName + " — break time";
	}

	function pad(n) { return (n < 10 ? "0" : "") + n; }

	// clock renders ms left as the countdown shows it, rounding up so
	// 00:00 only shows at the very end. Go's Clock must agree.
	function clock(ms) {
		var total = Math.ceil(ms / 1000);
		var h = Math.floor(total / 3600);
		var m = Math.floor((total % 3600) / 60);
		var sec = total % 60;
		return h > 0 ? h + ":" + pad(m) + ":" + pad(sec) : pad(m) + ":" + pad(sec);
	}

	// focused renders focus time for the Done screen: "3h 20m", "50m".
	function focused(seconds) {
		var m = Math.floor(seconds / 60);
		if (m < 1) return "less than a minute";
		var h = Math.floor(m / 60);
		if (h === 0) return m + "m";
		return m % 60 ? h + "h " + (m % 60) + "m" : h + "h";
	}

	function valid(s) {
		return !!s && s.v === VERSION &&
			typeof s.timerId === "number" && typeof s.timerName === "string" &&
			Array.isArray(s.phases) && s.phases.length > 0 &&
			s.phases.every(function (p) {
				return p && typeof p.kind === "string" && typeof p.seconds === "number" && p.seconds > 0;
			}) &&
			typeof s.phaseIndex === "number" && s.phaseIndex >= 0 && s.phaseIndex < s.phases.length &&
			typeof s.phaseStartedAt === "number" && typeof s.pausedTotalInPhase === "number" &&
			(s.pausedAt === null || typeof s.pausedAt === "number");
	}

	// load returns the stored session, or null. Anything unreadable is
	// discarded with a warning, as if nothing were running (spec: "Errors").
	function load() {
		var raw;
		try {
			raw = window.localStorage.getItem(KEY);
		} catch (e) {
			return null;
		}
		if (!raw) return null;
		try {
			var s = JSON.parse(raw);
			if (valid(s)) return s;
		} catch (e) {
			// fall through to discard it
		}
		console.warn("ON Focus: discarding an unreadable running session");
		clear();
		return null;
	}

	function save(s) {
		try {
			window.localStorage.setItem(KEY, JSON.stringify(s));
		} catch (e) {
			console.warn("ON Focus: couldn't save the running session", e);
		}
	}

	function clear() {
		try {
			window.localStorage.removeItem(KEY);
		} catch (e) {
			// nothing stored, nothing to clear
		}
	}

	window.OnFocus = window.OnFocus || {};
	window.OnFocus.session = {
		create: create, advance: advance,
		pause: pause, resume: resume, skip: skip, restart: restart, next: next,
		current: current, upcoming: upcoming,
		elapsed: elapsed, remaining: remaining, progress: progress, focusSeconds: focusSeconds,
		startLabel: startLabel, notice: notice, clock: clock, focused: focused,
		load: load, save: save, clear: clear
	};
})();
```

- [ ] **Step 4: Add the route**

In `internal/apps/focus/focus.go`, after the `home.js` route:

```go
	r.HandleFunc("GET /session.js", a.script("session.js"))
```

- [ ] **Step 5: Run the route test**

Run: `go test ./internal/apps/focus/ -run TestScriptsAreServed -count=1`
Expected: PASS.

- [ ] **Step 6: Check the maths with a throwaway Node script (never committed)**

The suite has no JS test harness (spec: "Testing"), but `session.js` is pure, so check it once by hand. Write this to your scratchpad directory as `session-check.js` and run `node <scratchpad>/session-check.js <worktree>/internal/apps/focus/static/session.js`:

```js
const fs = require("fs"), vm = require("vm"), assert = require("assert");
const store = {};
const ctx = {
	console,
	localStorage: {
		getItem: k => (k in store ? store[k] : null),
		setItem: (k, v) => { store[k] = String(v); },
		removeItem: k => { delete store[k]; }
	}
};
ctx.window = ctx;
vm.runInNewContext(fs.readFileSync(process.argv[2], "utf8"), ctx);
const S = ctx.OnFocus.session;
const MIN = 60000;
// Deep work: 50/10 × 4, long 30 every 2 → F1 B F2 L F3 B F4.
const deep = {
	id: 7, name: "Deep work", color: "teal", chime: "bowl", autoAdvance: true, keepHistory: true, rounds: 4,
	phases: [
		{kind: "focus", seconds: 3000, round: 1}, {kind: "break", seconds: 600, round: 1},
		{kind: "focus", seconds: 3000, round: 2}, {kind: "long_break", seconds: 1800, round: 2},
		{kind: "focus", seconds: 3000, round: 3}, {kind: "break", seconds: 600, round: 3},
		{kind: "focus", seconds: 3000, round: 4}
	]
};
const t0 = 1_700_000_000_000;

let s = S.create(deep, t0);
assert.strictEqual(S.advance(s, t0 + 49 * MIN), 0);
assert.strictEqual(S.remaining(s, t0 + 49 * MIN), MIN);
assert.strictEqual(S.clock(S.remaining(s, t0 + 49 * MIN)), "01:00");

// Two phases pass while away: one advance, two ended, now in round 2.
assert.strictEqual(S.advance(s, t0 + 65 * MIN), 2);
assert.strictEqual(s.phaseIndex, 2);
assert.strictEqual(s.phaseStartedAt, t0 + 60 * MIN);
assert.strictEqual(s.focusSecondsBanked, 3000);
assert.strictEqual(s.roundsDone, 1);
assert.strictEqual(S.focusSeconds(s, t0 + 65 * MIN), 3300);

// Paused time doesn't count.
S.pause(s, t0 + 70 * MIN);
assert.strictEqual(S.advance(s, t0 + 500 * MIN), 0);
S.resume(s, t0 + 100 * MIN);
assert.strictEqual(S.remaining(s, t0 + 100 * MIN), 40 * MIN);

// The whole run: finished, completed, every round counted.
s = S.create(deep, t0);
assert.strictEqual(S.advance(s, t0 + 600 * MIN), 7);
assert.ok(s.finished && s.completed);
assert.strictEqual(s.endedAt, t0 + 250 * MIN);
assert.strictEqual(s.focusSecondsBanked, 12000);
assert.strictEqual(s.roundsDone, 4);
assert.strictEqual(S.notice(s), "Deep work — done");
assert.strictEqual(S.focused(s.focusSecondsBanked), "3h 20m");

// Auto-advance off stops at the first boundary however long it's been.
s = S.create(Object.assign({}, deep, {autoAdvance: false}), t0);
assert.strictEqual(S.advance(s, t0 + 600 * MIN), 1);
assert.ok(s.waiting);
assert.strictEqual(s.phaseIndex, 0);
assert.strictEqual(S.remaining(s, t0 + 600 * MIN), 0);
assert.strictEqual(S.startLabel(s), "Start break");
assert.strictEqual(S.notice(s), "Deep work — break time");
S.next(s, t0 + 601 * MIN);
assert.ok(!s.waiting);
assert.strictEqual(s.phaseIndex, 1);
assert.strictEqual(S.remaining(s, t0 + 601 * MIN), 10 * MIN);

// Skip banks the focus done and doesn't count the round.
s = S.create(deep, t0);
S.skip(s, t0 + 20 * MIN);
assert.strictEqual(s.phaseIndex, 1);
assert.strictEqual(s.focusSecondsBanked, 1200);
assert.strictEqual(s.roundsDone, 0);

// Restart banks the focus done and runs the phase again, unpaused.
s = S.create(deep, t0);
S.pause(s, t0 + 10 * MIN);
S.restart(s, t0 + 15 * MIN);
assert.strictEqual(s.focusSecondsBanked, 600);
assert.strictEqual(s.pausedAt, null);
assert.strictEqual(S.remaining(s, t0 + 15 * MIN), 50 * MIN);

// Skipping the last phase ends the session, not completed.
const single = {id: 1, name: "Reading", color: "purple", chime: "bell", autoAdvance: true, keepHistory: true, rounds: 0,
	phases: [{kind: "focus", seconds: 1800, round: 1}]};
s = S.create(single, t0);
S.skip(s, t0 + 5 * MIN);
assert.ok(s.finished && !s.completed);
assert.strictEqual(s.endedAt, t0 + 5 * MIN);
assert.strictEqual(S.focusSeconds(s, t0 + 6 * MIN), 300);

// Clock format agrees with Go's Clock.
assert.strictEqual(S.clock(31 * MIN + 12000), "31:12");
assert.strictEqual(S.clock(90 * MIN), "1:30:00");
assert.strictEqual(S.clock(1), "00:01");
assert.strictEqual(S.clock(0), "00:00");

// Storage round trip; junk is discarded.
S.save(s);
assert.strictEqual(S.load().clientId, s.clientId);
store["onsuite.focus.session"] = "{not json";
ctx.console = {warn() {}};
assert.strictEqual(S.load(), null);
assert.ok(!("onsuite.focus.session" in store));
console.log("session.js: all checks pass");
```
Expected: `session.js: all checks pass`. If an assertion fails, fix `session.js` (not the check) unless the check contradicts the Interfaces block above.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/focus/static/session.js internal/apps/focus/focus.go internal/apps/focus/handlers_test.go
git commit -m "feat(focus): session engine for the running page (#494)"
```

---

### Task 2: Chimes and the form's ▶ preview (`chimes.js`)

**Files:**
- Create: `internal/apps/focus/static/chimes.js`
- Modify: `internal/apps/focus/focus.go` (route), `internal/apps/focus/templates/form.html`, `internal/ui/static/app.css`
- Test: `internal/apps/focus/handlers_test.go`, `internal/apps/focus/form_test.go`

**Interfaces:**
- Produces: `window.OnFocus.chimes` with `play(name)` (`"silent"` and unknown names play nothing), `locked() → bool` (true only when Web Audio exists and is not yet running), `unlock()` (call from a click or key handler). Route `GET /focus/chimes.js`. The form's `button[data-focus-chime-preview="focus-chime"]`.

- [ ] **Step 1: Write the failing tests**

In `TestScriptsAreServed` (`handlers_test.go`), add `"chimes.js",` to the list after `"session.js",`.

Append to `internal/apps/focus/form_test.go`:

```go
func TestFormHasAChimePreview(t *testing.T) {
	s := newServer(t)
	for _, path := range []string{"/focus/new", "/focus/timers/" + itoa(seedTimer(t, s, s.Alice.User.ID, single("Reading", 30)).ID) + "/edit"} {
		doc := s.Get(t, s.Alice, path)
		doc.MustHave(`script[src="/focus/chimes.js"]`)
		// Hidden until chimes.js shows it: it does nothing without JavaScript.
		findEl(t, doc, "button", map[string]string{"data-focus-chime-preview": "focus-chime", "type": "button", "hidden": ""})
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/apps/focus/ -run 'TestScriptsAreServed|TestFormHasAChimePreview' -count=1`
Expected: FAIL — `chimes.js = 404`, and no `script[src="/focus/chimes.js"]`.

- [ ] **Step 3: Write `chimes.js`**

`internal/apps/focus/static/chimes.js`:

```js
// ON Focus's phase-end chimes (spec: "Chimes"), synthesised with the Web
// Audio API so there are no audio files. Browsers keep audio locked until
// the person clicks or presses a key on the page: unlock() is called from
// such a gesture, and locked() lets the running page say sound is off.
"use strict";

(function () {
	var AudioCtx = window.AudioContext || window.webkitAudioContext;
	var ctx = null;

	function context() {
		if (!ctx && AudioCtx) ctx = new AudioCtx();
		return ctx;
	}

	// locked reports whether a chime would be silent right now. Without Web
	// Audio there's nothing to unlock, so that isn't "locked".
	function locked() {
		var c = context();
		return !!c && c.state !== "running";
	}

	function unlock() {
		var c = context();
		if (c && c.state === "suspended") c.resume();
	}

	// tone plays one sine partial: a quick rise, then an exponential fade.
	function tone(c, out, freq, start, gain, attack, decay) {
		var osc = c.createOscillator();
		var g = c.createGain();
		osc.type = "sine";
		osc.frequency.value = freq;
		g.gain.setValueAtTime(0.0001, start);
		g.gain.exponentialRampToValueAtTime(gain, start + attack);
		g.gain.exponentialRampToValueAtTime(0.0001, start + attack + decay);
		osc.connect(g);
		g.connect(out);
		osc.start(start);
		osc.stop(start + attack + decay + 0.05);
	}

	var sounds = {
		// Two strikes of a small bell: inharmonic partials, fast decay.
		bell: function (c, out, t) {
			[0, 0.6].forEach(function (d) {
				tone(c, out, 660, t + d, 0.5, 0.005, 2.2);
				tone(c, out, 660 * 2.76, t + d, 0.18, 0.005, 1.2);
				tone(c, out, 660 * 5.4, t + d, 0.06, 0.005, 0.6);
			});
		},
		// A low singing bowl: slow swell, long ring; the detuned pair beats.
		bowl: function (c, out, t) {
			tone(c, out, 220, t, 0.45, 0.08, 5);
			tone(c, out, 221.6, t, 0.3, 0.08, 5);
			tone(c, out, 220 * 2.71, t, 0.1, 0.08, 3);
		},
		// Two quiet rising notes.
		soft: function (c, out, t) {
			tone(c, out, 523.25, t, 0.25, 0.03, 1.2);
			tone(c, out, 659.25, t + 0.35, 0.25, 0.03, 1.4);
		}
	};

	function play(name) {
		var make = sounds[name];
		var c = context();
		if (!make || !c) return; // "silent", or no Web Audio
		if (c.state === "suspended") c.resume();
		var out = c.createGain();
		out.gain.value = 0.6;
		out.connect(c.destination);
		make(c, out, c.currentTime + 0.02);
	}

	window.OnFocus = window.OnFocus || {};
	window.OnFocus.chimes = { play: play, locked: locked, unlock: unlock };

	// The timer form's ▶ plays the chime picked in the select it names. It
	// ships hidden because it needs this script.
	document.querySelectorAll("[data-focus-chime-preview]").forEach(function (button) {
		button.hidden = false;
		button.addEventListener("click", function () {
			var select = document.getElementById(button.dataset.focusChimePreview);
			if (!select) return;
			unlock();
			play(select.value);
		});
	});
})();
```

- [ ] **Step 4: Add the route, the button and its style**

`focus.go`, after the `session.js` route:

```go
	r.HandleFunc("GET /chimes.js", a.script("chimes.js"))
```

`templates/form.html` — add as the first line of the file:

```html
{{define "head"}}<script src="/focus/chimes.js" defer></script>{{end}}

```

and replace the chime `<select …>…</select>` block with a row holding the select and the button:

```html
		<div class="focus-chime-row">
			<select id="focus-chime" name="chime"{{if index $d.Errors "chime"}} aria-invalid="true" aria-describedby="focus-chime-error"{{end}}>
				{{range $d.Chimes}}<option value="{{.Name}}"{{if eq .Name $d.Values.Chime}} selected{{end}}>{{.Label}}</option>{{end}}
			</select>
			<button type="button" class="focus-chime-preview" data-focus-chime-preview="focus-chime" aria-label="Play this chime" hidden>▶</button>
		</div>
```

`internal/ui/static/app.css` — at the end of the "ON Focus" section:

```css
.focus-chime-row { display: flex; align-items: center; gap: var(--s-2); }
.focus-chime-preview { padding: var(--s-1) var(--s-3); }
.focus-chime-preview[hidden] { display: none; }
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/apps/focus/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/focus internal/ui/static/app.css
git commit -m "feat(focus): synthesised chimes with a preview on the timer form (#494)"
```

---

### Task 3: The running page, server side

**Files:**
- Modify: `internal/apps/focus/phases.go` (add `Clock`), `internal/apps/focus/handlers.go` (replace `phaseRow`, `runView`, `run`), `internal/apps/focus/templates/run.html` (rewrite)
- Test: `internal/apps/focus/phases_test.go`, `internal/apps/focus/handlers_test.go`

**Interfaces:**
- Consumes: `Phases`, `Phase.Label`, `Timer` (F1).
- Produces:
  - `func Clock(seconds int) string`
  - The page markup `focus.js` (Task 5) and the CSS (Task 4) rely on:
    - `#focus-runner.focus-runner.swatch-c-<color>` (plus `focus-runner-single` for single timers)
    - `script#focus-run-config[type=application/json]` — `{id, name, color, chime, autoAdvance, keepHistory, rounds, phases: [{kind, seconds, round, label}]}`
    - `a[data-focus-exit]`, `button[data-focus-fullscreen][hidden]`, `[data-focus-main]`, `circle[data-focus-bar]` (`pathLength="1000"`), `[data-focus-digits]`, `[data-focus-phase]` (intervals only), `li.focus-dot` × rounds (intervals only), `[data-focus-controls]` with `button[data-focus-pause]`, `button[data-focus-skip]` (intervals only), `button[data-focus-restart]`; `[data-focus-waiting][hidden]` with `button[data-focus-next]`; `p[data-focus-sound-hint][hidden]`; `[data-focus-done][hidden]` with `[data-focus-done-text]`
    - `dialog#focus-run-dialog` with `#focus-run-dialog-message`, `#focus-run-dialog-ok`, `#focus-run-dialog-cancel`

- [ ] **Step 1: Write the failing `Clock` test**

Append to `internal/apps/focus/phases_test.go`:

```go
func TestClock(t *testing.T) {
	for _, c := range []struct {
		seconds int
		want    string
	}{
		{0, "00:00"}, {59, "00:59"}, {300, "05:00"}, {3000, "50:00"},
		{3600, "1:00:00"}, {5400, "1:30:00"}, {10800, "3:00:00"},
	} {
		if got := focus.Clock(c.seconds); got != c.want {
			t.Errorf("Clock(%d) = %q, want %q", c.seconds, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Replace the placeholder page tests with the real page's**

In `internal/apps/focus/handlers_test.go`, delete `TestRunPageListsThePhases` and `TestRunPageForASingleTimer` (keep `TestRunPageIsNotFoundForSomeoneElse`), add `"encoding/json"` to the imports, and append:

```go
// runConfig is the JSON the running page embeds for focus.js.
type runConfig struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Chime       string `json:"chime"`
	AutoAdvance bool   `json:"autoAdvance"`
	KeepHistory bool   `json:"keepHistory"`
	Rounds      int    `json:"rounds"`
	Phases      []struct {
		Kind    string `json:"kind"`
		Seconds int    `json:"seconds"`
		Round   int    `json:"round"`
		Label   string `json:"label"`
	} `json:"phases"`
}

func runConfigOf(t *testing.T, doc *htmlassert.Doc) runConfig {
	t.Helper()
	node := doc.MustHave("script#focus-run-config")
	if typ, _ := htmlassert.Attr(node, "type"); typ != "application/json" {
		t.Fatalf("config script type = %q", typ)
	}
	var cfg runConfig
	if err := json.Unmarshal([]byte(htmlassert.Text(node)), &cfg); err != nil {
		t.Fatalf("config JSON: %v\n%s", err, htmlassert.Text(node))
	}
	return cfg
}

func TestRunPageForAnIntervalsTimer(t *testing.T) {
	s := newServer(t)
	tm := seedTimer(t, s, s.Alice.User.ID, validIntervals()) // Deep work, blue, bowl, 50/10 × 4, long 30 every 2
	doc := s.Get(t, s.Alice, "/focus/run/"+itoa(tm.ID))

	for _, src := range []string{"/focus/session.js", "/focus/chimes.js", "/focus/focus.js"} {
		doc.MustHave(`script[src="` + src + `"]`)
	}
	root := doc.MustHave("#focus-runner")
	if class, _ := htmlassert.Attr(root, "class"); !strings.Contains(class, "focus-runner") ||
		!strings.Contains(class, "swatch-c-blue") || strings.Contains(class, "focus-runner-single") {
		t.Errorf("runner class = %q", class)
	}

	cfg := runConfigOf(t, doc)
	if cfg.ID != tm.ID || cfg.Name != "Deep work" || cfg.Color != "blue" || cfg.Chime != "bowl" ||
		!cfg.AutoAdvance || !cfg.KeepHistory || cfg.Rounds != 4 || len(cfg.Phases) != 7 {
		t.Fatalf("config = %+v", cfg)
	}
	if p := cfg.Phases[0]; p.Kind != "focus" || p.Seconds != 3000 || p.Round != 1 || p.Label != "Focus · round 1 of 4" {
		t.Errorf("phase 0 = %+v", p)
	}
	if p := cfg.Phases[1]; p.Kind != "break" || p.Seconds != 600 || p.Round != 1 || p.Label != "Short break" {
		t.Errorf("phase 1 = %+v", p)
	}
	if p := cfg.Phases[3]; p.Kind != "long_break" || p.Seconds != 1800 || p.Round != 2 || p.Label != "Long break" {
		t.Errorf("phase 3 = %+v", p)
	}

	if got := htmlassert.Text(doc.MustHave("[data-focus-digits]")); got != "50:00" {
		t.Errorf("digits = %q, want 50:00", got)
	}
	if got := htmlassert.Text(doc.MustHave("[data-focus-phase]")); got != "Focus · round 1 of 4" {
		t.Errorf("phase label = %q", got)
	}
	if n := len(doc.QueryAll(".focus-dot")); n != 4 {
		t.Errorf("%d dots, want 4", n)
	}
	for _, sel := range []string{
		"[data-focus-pause]", "[data-focus-skip]", "[data-focus-restart]", "[data-focus-next]",
		"[data-focus-exit]", "[data-focus-fullscreen]", "[data-focus-sound-hint]", "[data-focus-done]",
		"circle[data-focus-bar]", "dialog#focus-run-dialog",
	} {
		doc.MustHave(sel)
	}
	// Hidden until the script shows them.
	for _, sel := range []string{"[data-focus-waiting]", "[data-focus-sound-hint]", "[data-focus-done]", "[data-focus-fullscreen]"} {
		if _, ok := htmlassert.Attr(doc.MustHave(sel), "hidden"); !ok {
			t.Errorf("%s should start hidden", sel)
		}
	}
}

func TestRunPageForASingleTimer(t *testing.T) {
	s := newServer(t)
	in := single("Daily Reflection", 15)
	in.Color, in.AutoAdvance, in.KeepHistory = "coral", false, false
	tm := seedTimer(t, s, s.Alice.User.ID, in)
	doc := s.Get(t, s.Alice, "/focus/run/"+itoa(tm.ID))

	if class, _ := htmlassert.Attr(doc.MustHave("#focus-runner"), "class"); !strings.Contains(class, "focus-runner-single") {
		t.Errorf("runner class = %q, want focus-runner-single", class)
	}
	cfg := runConfigOf(t, doc)
	if cfg.Rounds != 0 || len(cfg.Phases) != 1 || cfg.Phases[0].Seconds != 900 || cfg.AutoAdvance || cfg.KeepHistory {
		t.Errorf("config = %+v", cfg)
	}
	if got := htmlassert.Text(doc.MustHave("[data-focus-digits]")); got != "15:00" {
		t.Errorf("digits = %q, want 15:00", got)
	}
	// Spec: single timers have no phase label, no dots and no Skip.
	doc.MustNotHave("[data-focus-phase]")
	doc.MustNotHave(".focus-dot")
	doc.MustNotHave("[data-focus-skip]")
	doc.MustHave("[data-focus-pause]")
	doc.MustHave("[data-focus-restart]")
}
```

- [ ] **Step 3: Run them to see them fail**

Run: `go test ./internal/apps/focus/ -run 'TestClock|TestRunPage' -count=1`
Expected: FAIL — `undefined: focus.Clock` (compile error).

- [ ] **Step 4: Add `Clock`**

Append to `internal/apps/focus/phases.go`:

```go
// Clock renders a length as the running page's countdown shows it:
// "05:00", "50:00", "1:30:00". focus.js's clock must agree.
func Clock(seconds int) string {
	h, m, s := seconds/3600, seconds%3600/60, seconds%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
```

- [ ] **Step 5: Replace the placeholder view and handler**

In `internal/apps/focus/handlers.go`, replace everything from `// phaseRow is one line of the placeholder running page.` to the end of the file with:

```go
// runPhase is one phase as focus.js reads it.
type runPhase struct {
	Phase
	Label string `json:"label"`
}

// runConfig is the timer as focus.js runs it, embedded in the page as JSON
// (spec: "The runner"). The browser copies it into its own state at Start,
// so editing the timer mid-session doesn't change a running one.
type runConfig struct {
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

func newRunView(t Timer) runView {
	cfg := runConfig{
		ID: t.ID, Name: t.Name, Color: t.Color, Chime: t.Chime,
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
	a.render(w, r, http.StatusOK, "focus/run", t.Name, newRunView(t))
}
```

- [ ] **Step 6: Rewrite `run.html`**

`internal/apps/focus/templates/run.html`:

```html
{{define "head"}}<script src="/focus/session.js" defer></script><script src="/focus/chimes.js" defer></script><script src="/focus/focus.js" defer></script>{{end}}

{{define "content"}}
{{$d := .Data}}
{{/* Focus mode: app.css hides the suite chrome around .focus-runner, as it
     does for ON Later's reading view. */}}
<div id="focus-runner" class="focus-runner swatch-c-{{$d.Color}}{{if $d.Single}} focus-runner-single{{end}}">
	<script type="application/json" id="focus-run-config">{{$d.Config}}</script>
	<div class="focus-runner-top">
		<a class="focus-runner-quiet" href="/focus/" data-focus-exit aria-keyshortcuts="Escape">← Exit</a>
		<button type="button" class="focus-runner-quiet focus-runner-fullscreen" data-focus-fullscreen aria-keyshortcuts="F" hidden>⛶ Full screen</button>
	</div>
	<div class="focus-runner-main" data-focus-main>
		<div class="focus-ring">
			<svg class="focus-ring-svg" viewBox="0 0 220 220" aria-hidden="true">
				<circle class="focus-ring-track" cx="110" cy="110" r="100"></circle>
				<circle class="focus-ring-bar" cx="110" cy="110" r="100" pathLength="1000" stroke-dashoffset="1000" data-focus-bar></circle>
			</svg>
			<div class="focus-ring-inner">
				<p class="focus-runner-name">{{$d.Name}}</p>
				<p class="focus-digits" role="timer" data-focus-digits>{{$d.Clock}}</p>
				{{if not $d.Single}}
				<p class="focus-phase" data-focus-phase>{{$d.Label}}</p>
				<ol class="focus-dots" aria-hidden="true">{{range $d.Dots}}<li class="focus-dot"></li>{{end}}</ol>
				{{end}}
			</div>
		</div>
		<div class="focus-controls" data-focus-controls>
			<button type="button" class="primary" data-focus-pause aria-keyshortcuts="Space">Pause</button>
			{{if not $d.Single}}<button type="button" data-focus-skip aria-keyshortcuts="S">Skip</button>{{end}}
			<button type="button" data-focus-restart aria-keyshortcuts="R">Restart</button>
		</div>
		<div class="focus-controls" data-focus-waiting hidden>
			<button type="button" class="primary" data-focus-next aria-keyshortcuts="Space">Start</button>
		</div>
		<p class="focus-sound-hint" data-focus-sound-hint hidden>Sound is off — click anywhere to turn it on.</p>
		<noscript><p class="focus-sound-hint">The timer needs JavaScript. <a href="/focus/">Back to timers</a></p></noscript>
	</div>
	<div class="focus-done" data-focus-done hidden>
		<p class="focus-done-title" data-focus-done-text>Done</p>
		<a class="button primary" href="/focus/">Back to timers</a>
	</div>
</div>
{{/* The running page's one confirmation dialog: "End this session?" and
     "End Deep work and start Reading?". focus.js sets its texts. */}}
<dialog id="focus-run-dialog" class="focus-dialog">
	<p id="focus-run-dialog-message"></p>
	<div class="dialog-actions">
		<button type="button" id="focus-run-dialog-ok" class="primary">OK</button>
		<button type="button" id="focus-run-dialog-cancel">Cancel</button>
	</div>
</dialog>
{{end}}
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/apps/focus/ -count=1`
Expected: PASS. (`/focus/focus.js` is referenced but not served until Task 5; nothing tests that yet.)

- [ ] **Step 8: Commit**

```bash
git add internal/apps/focus
git commit -m "feat(focus): running page markup with the timer as JSON (#494)"
```

---

### Task 4: Running page styles

**Files:**
- Modify: `internal/ui/static/app.css` (end of the "ON Focus" section)

**Interfaces:**
- Consumes: Task 3's markup.
- Produces: state classes `focus-runner-break`, `focus-runner-paused`, `focus-runner-waiting` and dot classes `focus-dot-done`, `focus-dot-now`, which `focus.js` toggles (Task 5). `--focus-ring` is the ring/button colour: the timer's `--swatch` in focus, warm grey in breaks.

- [ ] **Step 1: Add the styles**

Append to the end of the "ON Focus" section of `internal/ui/static/app.css`:

```css
/* Running page (spec: "Running page"). Focus mode hides the suite chrome,
 * exactly as ON Later's reading view does. */
body:has(.focus-runner) .shell-bar,
body:has(.focus-runner) .app-sidebar,
body:has(.focus-runner) .app-footer { display: none; }
main:has(.focus-runner) { max-width: none; padding: 0; }

.focus-runner {
	--focus-break: #a89a88;          /* breaks are a warm grey, never the timer's colour */
	--focus-ring: var(--swatch);
	position: relative;
	min-height: 100vh;
	min-height: 100dvh;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	padding: var(--s-6) var(--s-4);
	background: var(--c-bg-subtle);
	color: var(--c-text);
}
:root[data-theme="dark"] .focus-runner { --focus-break: #7a6c5d; }
.focus-runner-break { --focus-ring: var(--focus-break); }
.focus-runner [hidden] { display: none; }

.focus-runner-top {
	position: absolute;
	top: var(--s-3);
	left: var(--s-4);
	right: var(--s-4);
	display: flex;
	align-items: center;
}
.focus-runner-quiet {
	padding: var(--s-1) var(--s-2);
	border: 0;
	border-radius: var(--radius);
	background: none;
	color: var(--c-text-faint);
	font: inherit;
	font-size: var(--fs-sm);
	text-decoration: none;
	cursor: pointer;
}
.focus-runner-quiet:hover { background: var(--c-bg-inset); color: var(--c-text); text-decoration: none; }
.focus-runner-fullscreen { margin-left: auto; }

.focus-runner-main { display: flex; flex-direction: column; align-items: center; gap: var(--s-5); }
.focus-ring {
	position: relative;
	width: min(22rem, 80vw, 60vh);
	aspect-ratio: 1;
	display: flex;
	align-items: center;
	justify-content: center;
}
.focus-ring-svg { position: absolute; inset: 0; width: 100%; height: 100%; transform: rotate(-90deg); }
.focus-ring-track, .focus-ring-bar { fill: none; stroke-width: 6; }
.focus-ring-track { stroke: var(--c-bg-inset); }
/* stroke-dashoffset is an SVG attribute focus.js sets; CSS must not set it,
 * or the stylesheet would win over the attribute. */
.focus-ring-bar { stroke: var(--focus-ring); stroke-linecap: round; stroke-dasharray: 1000; transition: stroke-dashoffset .5s linear; }

.focus-ring-inner { position: relative; display: flex; flex-direction: column; align-items: center; gap: var(--s-2); max-width: 75%; text-align: center; }
.focus-ring-inner p { margin: 0; }
.focus-runner-name { color: var(--c-text-dim); font-size: var(--fs-sm); letter-spacing: .04em; text-transform: uppercase; overflow-wrap: anywhere; }
.focus-digits { font-size: clamp(3rem, 12vmin, 4.5rem); font-weight: 300; font-variant-numeric: tabular-nums; letter-spacing: -.02em; line-height: 1; }
.focus-phase { color: var(--focus-ring); font-size: var(--fs-sm); font-weight: 600; }
.focus-runner-break .focus-phase { color: var(--c-text-dim); }

.focus-dots { display: flex; gap: .4rem; margin: 0; padding: 0; list-style: none; }
.focus-dot { width: .5rem; height: .5rem; border: 1.5px solid var(--c-text-faint); border-radius: 50%; }
.focus-dot-done { background: var(--focus-ring); border-color: var(--focus-ring); }
.focus-dot-now { border-color: var(--swatch); box-shadow: 0 0 0 2px var(--swatch-soft); }

.focus-runner-paused .focus-ring-bar,
.focus-runner-paused .focus-digits { opacity: .45; }
.focus-runner-paused .focus-phase { color: var(--c-text-dim); }

.focus-controls { display: flex; flex-wrap: wrap; justify-content: center; gap: var(--s-2); }
.focus-controls button { padding: var(--s-2) var(--s-4); border-radius: 999px; }
.focus-controls .primary,
.focus-controls .primary:hover { background: var(--focus-ring); border-color: var(--focus-ring); color: #fff; }

.focus-sound-hint { margin: 0; color: var(--c-text-faint); font-size: var(--fs-sm); text-align: center; }
.focus-done { display: flex; flex-direction: column; align-items: center; gap: var(--s-4); text-align: center; }
.focus-done-title { margin: 0; font-size: var(--fs-xl); }
```

- [ ] **Step 2: Look at it**

Rebuild and start the local server (`go build -o /tmp/onsuite-bin ./cmd/onsuite`, then `preview_start` `{name: "onsuite"}`; `.claude/launch.json` is untracked, so first copy it from the main checkout: `mkdir -p .claude && cp ../on-suite/.claude/launch.json .claude/`). Sign in, create a timer (an intervals one), and open `/focus/run/<id>`. The page has no `focus.js` yet, so it shows the static first-phase view.

Check: no suite header, sidebar or footer; the ring track is drawn and empty; name, `50:00`, the round label and four hollow dots sit centred inside it; Pause / Skip / Restart pills below, Pause in the timer's colour; ← Exit top left. Toggle dark mode from the user menu on another page and come back: still readable. Resize to `mobile`: the ring shrinks to fit, nothing scrolls sideways; reset with `desktop`.

- [ ] **Step 3: Run the full check**

Run the full check from Global Constraints. Expected: all green.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/static/app.css
git commit -m "feat(focus): focus-mode styles for the running page (#494)"
```

---

### Task 5: The runner (`focus.js`)

**Files:**
- Create: `internal/apps/focus/static/focus.js`
- Modify: `internal/apps/focus/focus.go` (route)
- Test: `internal/apps/focus/handlers_test.go`

**Interfaces:**
- Consumes: `window.OnFocus.session` (Task 1), `window.OnFocus.chimes` (Task 2), Task 3's markup, Task 4's state classes.
- Produces: route `GET /focus/focus.js`.

- [ ] **Step 1: Extend the route test**

In `TestScriptsAreServed`, add `"focus.js",` after `"chimes.js",`.

Run: `go test ./internal/apps/focus/ -run TestScriptsAreServed -count=1`
Expected: FAIL — `GET /focus/focus.js = 404`.

- [ ] **Step 2: Write `focus.js`**

`internal/apps/focus/static/focus.js`:

```js
// ON Focus's running page (spec: "The runner (focus.js)", "Running page").
// The server embeds the timer and its phase list as JSON; this script runs
// it with session.js's state and time maths, keeps the state in
// localStorage on every change, and draws the ring, dots and controls.
"use strict";

(function () {
	var root = document.getElementById("focus-runner");
	if (!root) return;
	var S = window.OnFocus.session;
	var chimes = window.OnFocus.chimes;
	var config = JSON.parse(document.getElementById("focus-run-config").textContent);

	var el = {
		main: root.querySelector("[data-focus-main]"),
		bar: root.querySelector("[data-focus-bar]"),
		digits: root.querySelector("[data-focus-digits]"),
		phase: root.querySelector("[data-focus-phase]"), // null for a single timer
		dots: root.querySelectorAll(".focus-dot"),
		controls: root.querySelector("[data-focus-controls]"),
		pause: root.querySelector("[data-focus-pause]"),
		skip: root.querySelector("[data-focus-skip]"), // null for a single timer
		restart: root.querySelector("[data-focus-restart]"),
		waiting: root.querySelector("[data-focus-waiting]"),
		next: root.querySelector("[data-focus-next]"),
		soundHint: root.querySelector("[data-focus-sound-hint]"),
		done: root.querySelector("[data-focus-done]"),
		doneText: root.querySelector("[data-focus-done-text]"),
		exit: root.querySelector("[data-focus-exit]"),
		fullscreen: root.querySelector("[data-focus-fullscreen]")
	};

	var s = null; // the running session, once there is one
	var ticker = 0;

	// ---- Drawing ----------------------------------------------------------

	function render(now) {
		var p = S.current(s);
		var isBreak = p.kind !== "focus";
		var paused = s.pausedAt !== null;
		var left = S.clock(S.remaining(s, now));

		root.classList.toggle("focus-runner-break", isBreak);
		root.classList.toggle("focus-runner-paused", paused);
		root.classList.toggle("focus-runner-waiting", s.waiting);

		el.digits.textContent = left;
		el.bar.setAttribute("stroke-dashoffset", String(Math.round(1000 * (1 - S.progress(s, now)))));
		if (el.phase) {
			if (s.waiting) el.phase.textContent = isBreak ? "Break over" : "Round " + p.round + " done";
			else if (paused) el.phase.textContent = "Paused";
			else el.phase.textContent = p.label;
		}

		// Dots: filled = rounds behind us, ringed = the round in progress
		// (spec: "Dots"). A break's round is the one just finished.
		var inFocus = p.kind === "focus" && !s.waiting;
		var behind = inFocus ? p.round - 1 : p.round;
		for (var i = 0; i < el.dots.length; i++) {
			el.dots[i].classList.toggle("focus-dot-done", i < behind);
			el.dots[i].classList.toggle("focus-dot-now", inFocus && i === p.round - 1);
		}

		el.controls.hidden = s.waiting;
		el.waiting.hidden = !s.waiting;
		if (s.waiting) el.next.textContent = S.startLabel(s);
		el.pause.textContent = paused ? "Resume" : "Pause";

		if (paused) document.title = "Paused · " + s.timerName;
		else if (s.waiting) document.title = "Ready · " + s.timerName;
		else document.title = left + " · " + (isBreak ? "Break" : s.timerName);
	}

	// finish shows the Done screen. Recording the session is F3 (#495);
	// until then a finished session is simply cleared.
	function finish(now) {
		window.clearInterval(ticker);
		S.clear();
		el.main.hidden = true;
		el.done.hidden = false;
		el.doneText.textContent = "Done — " + S.focused(S.focusSeconds(s, now)) + " focused";
		document.title = "Done · " + s.timerName;
	}

	// ---- Time passing -----------------------------------------------------

	// notify shows a browser notification, only while the tab is hidden and
	// only if the person allowed it (spec: "Phase end").
	function notify(text) {
		if (!document.hidden || !("Notification" in window) || Notification.permission !== "granted") return;
		try {
			new Notification(text, { tag: "on-focus" });
		} catch (e) {
			// Some mobile browsers only notify from a service worker; chimes still play.
		}
	}

	// tick walks the session forward to now and redraws: one chime and at
	// most one notification however many phases ended since the last tick.
	function tick() {
		var now = Date.now();
		if (S.advance(s, now) > 0) {
			S.save(s);
			chimes.play(s.chime);
			notify(S.notice(s));
		}
		if (s.finished) finish(now);
		else render(now);
	}

	function run() {
		tick();
		if (s.finished) return;
		ticker = window.setInterval(tick, 500);
		showSoundHint();
	}

	function begin() {
		s = S.create(config, Date.now());
		S.save(s);
		run();
	}

	// ---- Controls ---------------------------------------------------------

	// act runs one control: catch up to now first (so a phase that just
	// ended chimes and counts), then change the session, save and redraw.
	function act(change) {
		if (!s || s.finished) return;
		tick();
		if (s.finished) return;
		var now = Date.now();
		change(now);
		S.save(s);
		if (s.finished) finish(now);
		else render(now);
	}

	function togglePause() {
		act(function (now) {
			if (s.waiting) S.next(s, now);
			else if (s.pausedAt !== null) S.resume(s, now);
			else S.pause(s, now);
		});
	}
	function skip() { act(function (now) { S.skip(s, now); }); }
	function restart() { act(function (now) { S.restart(s, now); }); }

	// askToNotify asks for notification permission from a click or key
	// (spec: "Notification permission"), if the home page's ▶ didn't.
	function askToNotify() {
		if ("Notification" in window && Notification.permission === "default") Notification.requestPermission();
	}

	// A mouse click leaves focus on the button, and Space would then press
	// it again instead of pausing. Dropping focus after a pointer click
	// (detail > 0) keeps Space meaning Pause; keyboard presses keep focus.
	function control(button, handler) {
		if (!button) return;
		button.addEventListener("click", function (e) {
			askToNotify();
			handler();
			if (e.detail > 0) button.blur();
		});
	}
	control(el.pause, togglePause);
	control(el.next, togglePause);
	control(el.skip, skip);
	control(el.restart, restart);

	function toggleFullscreen() {
		if (!document.fullscreenEnabled) return;
		if (document.fullscreenElement) document.exitFullscreen();
		else document.documentElement.requestFullscreen().catch(function () {});
	}
	if (document.fullscreenEnabled) {
		el.fullscreen.hidden = false;
		control(el.fullscreen, toggleFullscreen);
		document.addEventListener("fullscreenchange", function () {
			el.fullscreen.textContent = document.fullscreenElement ? "Exit full screen" : "⛶ Full screen";
		});
	}

	// confirmThen asks in the page's dialog. onCancel runs on Cancel and on
	// Esc. Listeners belong to this one opening (home.js's pattern), so a
	// dismissed question can never fire later.
	function confirmThen(message, okLabel, cancelLabel, onOK, onCancel) {
		var dialog = document.getElementById("focus-run-dialog");
		var ok = document.getElementById("focus-run-dialog-ok");
		var cancel = document.getElementById("focus-run-dialog-cancel");
		document.getElementById("focus-run-dialog-message").textContent = message;
		ok.textContent = okLabel;
		cancel.textContent = cancelLabel;
		var chosen = false;
		var controller = new AbortController();
		dialog.addEventListener("close", function () {
			controller.abort();
			if (!chosen && onCancel) onCancel();
		}, { once: true });
		ok.addEventListener("click", function () {
			chosen = true;
			dialog.close();
			onOK();
		}, { signal: controller.signal });
		cancel.addEventListener("click", function () { dialog.close(); }, { signal: controller.signal });
		dialog.showModal();
	}

	// exit asks first while a session is running. Recording on Exit is F3
	// (#495); until then the session is simply cleared.
	function exit() {
		if (!s || s.finished) {
			window.location.assign("/focus/");
			return;
		}
		confirmThen("End this session?", "End session", "Keep going", function () {
			window.clearInterval(ticker);
			S.clear();
			window.location.assign("/focus/");
		});
	}
	el.exit.addEventListener("click", function (e) {
		e.preventDefault();
		exit();
	});

	document.addEventListener("keydown", function (e) {
		if (e.metaKey || e.ctrlKey || e.altKey) return;
		if (document.querySelector("dialog[open]")) return; // the dialog's own keys
		switch (e.key) {
		case " ":
			// Space on a focused control presses that control, natively.
			if (e.target.closest && e.target.closest("button, a")) return;
			e.preventDefault();
			askToNotify();
			togglePause();
			break;
		case "s":
		case "S":
			if (el.skip) skip();
			break;
		case "r":
		case "R":
			restart();
			break;
		case "f":
		case "F":
			toggleFullscreen();
			break;
		case "Escape":
			// In real full screen the browser takes Esc to leave it.
			if (document.fullscreenElement) return;
			e.preventDefault();
			exit();
			break;
		}
	});

	// A tab coming back catches up at once rather than on the next tick.
	document.addEventListener("visibilitychange", function () {
		if (s && !s.finished && !document.hidden) tick();
	});

	// Browsers keep audio locked until the person clicks or presses a key
	// on this page, and ▶ was a click on the home page. Say so, and unlock
	// on the first gesture (F2 plan: "Sound unlock").
	function showSoundHint() {
		if (s.chime === "silent" || !chimes.locked()) return;
		el.soundHint.hidden = false;
		function unlock() {
			chimes.unlock();
			el.soundHint.hidden = true;
			document.removeEventListener("pointerdown", unlock, true);
			document.removeEventListener("keydown", unlock, true);
		}
		document.addEventListener("pointerdown", unlock, true);
		document.addEventListener("keydown", unlock, true);
	}

	// ---- Start, resume or replace (spec: "Resume") -------------------------

	var stored = S.load();
	if (stored) S.advance(stored, Date.now());
	if (!stored) {
		begin();
	} else if (stored.timerId === config.id) {
		s = stored;
		run();
	} else if (stored.finished) {
		// Another timer's session already ran out. Recording it is F3
		// (#495); until then it is simply replaced.
		S.clear();
		begin();
	} else {
		confirmThen(
			"End " + stored.timerName + " and start " + config.name + "?",
			"Start " + config.name, "Back to " + stored.timerName,
			function () {
				S.clear();
				begin();
			},
			function () { window.location.assign("/focus/run/" + stored.timerId); }
		);
	}
})();
```

- [ ] **Step 3: Add the route**

`focus.go`, after the `chimes.js` route:

```go
	r.HandleFunc("GET /focus.js", a.script("focus.js"))
```

- [ ] **Step 4: Run the tests and the full check**

Run: `go test ./internal/apps/focus/ -count=1`, then the full check from Global Constraints.
Expected: all green.

- [ ] **Step 5: Try it in the browser**

Rebuild (`go build -o /tmp/onsuite-bin ./cmd/onsuite`), `preview_stop` / `preview_start` `{name: "onsuite"}`. Create a single timer "One" (1 min, Bell) and an intervals timer "Short" (focus 1, break 1, long break 1, rounds 3, long break every 2, auto-advance **off**). To skip ahead without waiting, move the stored start back with `javascript_tool` and reload — inspection only, never committed:

```js
var s = JSON.parse(localStorage.getItem("onsuite.focus.session")); s.phaseStartedAt -= 55000; localStorage.setItem("onsuite.focus.session", JSON.stringify(s)); location.reload();
```

Check each, and `read_console_messages` for errors throughout:
1. ▶ on "One": the ring starts filling, digits count down, title reads `00:59 · One`; the sound hint shows if audio is locked and disappears on a click.
2. Space pauses (ring and digits dim, title `Paused · One`, button says Resume); Space resumes; time paused isn't lost.
3. R restarts the phase from `01:00`.
4. Reload mid-phase: it carries on from the same time left.
5. Run it to the end: one chime, the Done screen says "Done — 1m focused", and `localStorage` no longer has the key.
6. ▶ on "Short": label "Focus · round 1 of 3", first dot ringed. S skips to "Short break" (grey ring, title `01:00 · Break`, first dot filled).
7. Let the break run out: it waits — full ring, "Break over", a "Start round 2" button; Space starts round 2.
8. Shift time past two phase ends and reload: it stops at the first boundary (auto-advance off) with one chime, not two.
9. Edit "Short" to auto-advance **on**, start it, shift time past two phase ends, reload: it is two phases on, one chime.
10. F toggles full screen; Esc in full screen only leaves full screen; Esc again asks "End this session?" — Keep going keeps it running, End session goes home and clears the key.
11. With "Short" running, open "One"'s run page: it asks "End Short and start One?"; "Back to Short" returns to Short's page, still running.
12. Notifications: allow them, start "One", switch to another tab past the end — a "One — done" notification appears.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/focus
git commit -m "feat(focus): run timers in the browser — controls, keys, chimes, resume (#494)"
```

---

### Task 6: Home page — resume banner and notification permission

**Files:**
- Modify: `internal/apps/focus/templates/index.html`, `internal/apps/focus/static/home.js`, `internal/ui/static/app.css`
- Test: `internal/apps/focus/handlers_test.go`

**Interfaces:**
- Consumes: `window.OnFocus.session` (Task 1): `load`, `advance`, `remaining`, `clock`, `upcoming`, `focusSeconds`, `focused`, `clear`.
- Produces: `p[data-focus-resume][hidden].focus-resume` on the home page.

- [ ] **Step 1: Write the failing test**

Append to `internal/apps/focus/handlers_test.go`:

```go
func TestIndexHasTheResumeBannerSlot(t *testing.T) {
	s := newServer(t)
	// Both with and without timers: a session can outlive its timer's tile.
	for _, seed := range []bool{false, true} {
		if seed {
			seedTimer(t, s, s.Alice.User.ID, single("Reading", 30))
		}
		doc := s.Get(t, s.Alice, "/focus/")
		doc.MustHave(`script[src="/focus/session.js"]`)
		banner := doc.MustHave("[data-focus-resume]")
		if _, ok := htmlassert.Attr(banner, "hidden"); !ok {
			t.Errorf("seed=%v: the banner should start hidden", seed)
		}
	}
}
```

Run: `go test ./internal/apps/focus/ -run TestIndexHasTheResumeBannerSlot -count=1`
Expected: FAIL — no `script[src="/focus/session.js"]`.

- [ ] **Step 2: Update `index.html`**

Replace the first line with:

```html
{{define "head"}}<script src="/focus/session.js" defer></script><script src="/focus/home.js" defer></script>{{end}}
```

and insert the banner right after the toolbar's closing `</div>`, before `{{if .Data.Tiles}}`:

```html
	{{/* home.js fills this in when a session is running in this browser
	     (spec: "Resume"). */}}
	<p class="focus-resume" data-focus-resume hidden></p>
```

- [ ] **Step 3: Extend `home.js`**

In `internal/apps/focus/static/home.js`, update the header comment's first sentence to:

```js
// ON Focus's home-page script. Forms marked data-focus-confirm ask first,
// in the app's own dialog (later.js's pattern); without JavaScript the form
// simply submits. Tiles can also be dragged to reorder. It also shows the
// resume banner and asks for notification permission on ▶.
```

and add, just before the closing `})();`:

```js
	// Ask for notification permission on the first ▶ (spec: "Notification
	// permission": on a Start click, never on page load). The prompt would
	// vanish if the page navigated away under it, so wait for the answer.
	document.addEventListener("click", function (e) {
		if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
		var play = e.target.closest && e.target.closest(".focus-play");
		if (!play || !("Notification" in window) || Notification.permission !== "default") return;
		e.preventDefault();
		function go() { window.location.assign(play.href); }
		Notification.requestPermission().then(go, go);
	});

	// The resume banner (spec: "Resume"): a session running in this
	// browser, read from the state the running page keeps.
	var S = window.OnFocus && window.OnFocus.session;
	var banner = document.querySelector("[data-focus-resume]");
	var stored = S && banner ? S.load() : null;
	if (stored) {
		var link = document.createElement("a");
		link.href = "/focus/run/" + stored.timerId;
		banner.classList.add("swatch-c-" + stored.color);
		banner.appendChild(link);
		banner.hidden = false;
		var bannerTicker = window.setInterval(updateBanner, 1000);
		updateBanner();
	}

	function updateBanner() {
		var now = Date.now();
		S.advance(stored, now);
		if (stored.finished) {
			// Recording it is F3 (#495); until then a finished session is
			// simply cleared.
			window.clearInterval(bannerTicker);
			S.clear();
			banner.textContent = stored.timerName + " finished — " + S.focused(S.focusSeconds(stored, now)) + " focused.";
			return;
		}
		if (stored.waiting) {
			var up = S.upcoming(stored);
			link.textContent = stored.timerName + " — ready for " + (up.kind === "focus" ? "round " + up.round : "a break");
			return;
		}
		var left = S.clock(S.remaining(stored, now)) + " left";
		link.textContent = "Resume " + stored.timerName + " — " + (stored.pausedAt !== null ? "paused, " + left : left);
	}
```

(Function declarations are hoisted inside the IIFE, so `updateBanner` is callable above its definition; `bannerTicker` is assigned before the first call that can clear it.)

- [ ] **Step 4: Style the banner**

Append to the "ON Focus" section of `app.css`:

```css
.focus-resume {
	margin: 0;
	padding: var(--s-2) var(--s-3);
	border: var(--border);
	border-left: 4px solid var(--swatch, var(--c-accent));
	border-radius: var(--radius);
	color: var(--c-text-dim);
	font-size: var(--fs-sm);
}
.focus-resume a { font-weight: 600; }
```

- [ ] **Step 5: Run the tests and the full check**

Run: `go test ./internal/apps/focus/ -count=1`, then the full check.
Expected: all green.

- [ ] **Step 6: Try it in the browser**

Rebuild and restart the preview. Then:
1. Start "Short" (from Task 5), go back with the browser's back button (not Exit): the home page shows "Resume Short — 00:4x left" ticking down, with a teal-or-whatever left border in the timer's colour; clicking it resumes.
2. Pause it, go home: "Resume Short — paused, …".
3. Let it reach a boundary with auto-advance off: "Short — ready for a break".
4. Shift its time past the end (Task 5's snippet, on the run page, then go home): "Short finished — …m focused.", and the key is gone after reload (no banner).
5. With permission reset to "Ask" (site settings in the pane), ▶ shows the permission prompt before the page changes.

- [ ] **Step 7: Commit**

```bash
git add internal/apps/focus internal/ui/static/app.css
git commit -m "feat(focus): resume banner and notification prompt on the home page (#494)"
```

---

### Task 7: User guide and spec notes

**Files:**
- Modify: `docs/user/focus.md`, `docs/superpowers/specs/2026-10-07-on-focus-design.md`

- [ ] **Step 1: Add "Running a timer" to the user guide**

In `docs/user/focus.md`, replace the line `Click **▶** on a tile to start that timer.` with `Click **▶** on a tile to start that timer — see [Running a timer](#running-a-timer).`, and append at the end of the file:

```markdown
## Running a timer

**▶** starts the timer straight away in a calm, full-window view. The ring
around the countdown fills as time passes — in the timer's colour while you
focus, warm grey during breaks. Interval timers also show a dot per round:
filled for rounds done, ringed for the one you're in.

| Button | Key | What it does |
|---|---|---|
| **Pause** / **Resume** | Space | Paused time doesn't count. |
| **Skip** | S | Ends this focus round or break and moves on. Interval timers only. |
| **Restart** | R | Starts this focus round or break again. |
| **Full screen** | F | Fills the whole screen. Esc leaves it. |
| **← Exit** | Esc | Asks first, then ends the session. |

When a focus round or break ends, the timer's chime plays and the next one
starts by itself — unless you unticked **Start next phase automatically**,
in which case the timer waits for you to click **Start break** or
**Start round 2** (or press Space).

The first time you click **▶**, your browser asks whether ON Focus may show
notifications. Allow it to get a notification when a phase ends while
you're in another tab. If the timer says **Sound is off**, click anywhere
on it: browsers keep sound off until you've clicked on the page.

### Leaving and coming back

The running timer lives in your browser. Reloading the page, or closing it
and coming back, carries on where it was; the ON Focus home page shows a
**Resume** link. One timer runs at a time — starting another asks before it
ends the first. A timer started in one browser doesn't show up in another.
```

- [ ] **Step 2: Record the planning decisions in the spec**

In `docs/superpowers/specs/2026-10-07-on-focus-design.md`:

1. Under "### Chimes", append a paragraph:

```markdown
Browsers keep audio locked until the person interacts with the page, and
▶ is a click on the home page, not the running page. If audio is still
locked when the timer starts, the running page says "Sound is off — click
anywhere to turn it on" and the first click or key unlocks it.
```

2. Replace the "### Notification permission" paragraph with:

```markdown
Requested in the ▶ click on the home page (the page waits for the answer
before opening the running page), or on the first control click on the
running page if it was opened another way; never on page load. Denied or
unsupported means chimes only; nothing else changes.
```

3. In "### Running page", replace `the phase label below it, and round dots below that;` with `the phase label below it ("Focus · round 2 of 4", "Short break"), and round dots below that;`, and replace the sentence beginning `How the page opts out of` (through `from `Deps.Page`.`) with:

```markdown
The page opts out of the shell chrome the way ON Later's reading view does:
app.css hides `.shell-bar`, `.app-sidebar` and `.app-footer` with
`body:has(.focus-runner)`, so the page still gets CSRF, theme and the
logged-in user from `Deps.Page`.
```

4. In the "## Phases" table, append to the F2 row's scope: ` Until F3, finishing or exiting a session clears it without recording`.

- [ ] **Step 3: Check the docs build**

Run: `go test ./docs/... ./internal/platform/help/ -count=1`
Expected: PASS (the help guides load and the doc links resolve).

- [ ] **Step 4: Commit**

```bash
git add docs/user/focus.md docs/superpowers/specs/2026-10-07-on-focus-design.md
git commit -m "docs(focus): running a timer in the user guide; record F2 decisions (#494)"
```

---

### Task 8: Verify in the browser and open the PR

- [ ] **Step 1: Walk through the whole feature once more**

Rebuild (`go build -o /tmp/onsuite-bin ./cmd/onsuite`) and restart the preview. Repeat Task 5 Step 5's list and Task 6 Step 6's list quickly, plus:
1. The timer form's ▶ next to the chime select plays each chime; Silent plays nothing.
2. Dark mode: the running page (focus and break), dots, buttons, Done screen and banner stay readable.
3. Mobile width (`resize_window` preset `mobile`): ring, controls and Exit fit without sideways scroll; reset with `desktop`.
4. ON Later's reading view still hides the chrome and nothing else in the suite lost its header (the `:has()` rules are scoped to `.focus-runner`).

`read_console_messages` with `onlyErrors` on each page: none (the "AudioContext was not allowed to start" warning is expected before the first click). Take screenshots of a focus phase, a break and the home banner for the PR.

- [ ] **Step 2: Final full check**

Run the full check from Global Constraints. Expected: all green.

- [ ] **Step 3: Push and open the PR as Ilia**

```bash
env -u GIT_SSH_COMMAND git push -u origin feat/focus-f2-running-timer
env -u GH_TOKEN gh pr create --title "feat(focus): ON Focus F2 — running a timer" --body "$(cat <<'EOF'
Closes #494. Part of #492.

## What
- The real running page: focus mode (suite chrome hidden, as ON Later's reading view), progress ring, round dots, Pause / Skip / Restart / Full screen / Exit with Space / S / R / F / Esc.
- Auto-advance or wait at each boundary; one chime and one notification (tab hidden only) per catch-up.
- Synthesised chimes (bell, bowl, soft) with a ▶ preview on the timer form.
- Live tab title; running state in `localStorage` — reload or come back and it resumes; starting another timer asks first.
- Home page: resume banner; notification permission asked on ▶.

## Notes
- `session.js` holds the pure state/time maths (checked once with a throwaway Node script; the suite has no JS harness). `focus.js` and `home.js` share it.
- Recording sessions is F3 (#495): for now Done and Exit just clear the state. Each spot has a #495 comment.
- Decisions made while planning are in the plan and now in the spec (sound hint, permission on ▶, chrome hiding).

## Testing
- `go test ./... -race` and the full check are green.
- Manual browser checks from the plan (pause/resume, skip, restart, auto-advance on and off, reload mid-phase, reopen after it ran out, notification and chime, full screen, tab title, banner, dark mode, mobile).
EOF
)"
```

Expected: the PR URL. Do not merge.
