# ON Focus — design

- **Status:** approved design, 2026-10-07
- **Tracking:** milestone "ON Focus", parent issue #492, phases #493–#496
- **Mockups:** [2026-10-07-on-focus-mockups.html](2026-10-07-on-focus-mockups.html)

## Why

A simple, calm focus timer. Timers are created once and reused: a 15-minute
"Daily Reflection", a 50/10 "Deep work" with breaks, a 30-minute "Reading"
session. Pick one, start it, and get a quiet full-screen view that gets out
of the way. Finished sessions are kept so there is a simple record of where
the focused time went.

Single user today; per-user scoping as in every other app.

## Decisions at a glance

| Topic | Decision |
|---|---|
| Where a running timer lives | In the browser only. The server stores saved timers and recorded sessions, nothing else |
| Reload / accidental close | Running state mirrored to `localStorage`; reopening Focus in the same browser resumes it |
| Timer kinds | Two fixed kinds: *single* (one block) and *intervals* (focus / break / long break, rounds, long break every N). No custom step sequences |
| Phase change | Per-timer "Start next phase automatically", default on; off means wait at 00:00 for a click |
| Sound | Phase-end chime per timer from a small synthesised set (Web Audio), or silent. No ambient sounds |
| Notifications | Browser notification at phase end, only when the tab is hidden. Permission asked on first Start |
| Running view | In-page focus mode (suite chrome hidden) with optional real full-screen (Fullscreen API) |
| Running visuals | Progress ring around the countdown; round dots inside the ring for interval timers |
| Breaks | Neutral look (warm grey ring) so they never blur with a timer's colour |
| Single timers | No phase label under the countdown; the name above it is enough |
| Home | Colour tiles (like ON Flash decks) plus a "today" strip |
| Colours | ON Flash's 8-colour palette, renamed suite-wide from `deck-c-*` to `swatch-c-*` |
| What gets recorded | Any session with at least a minute of focus, finished or stopped early, if the timer's "Keep history" is on (default on). Decided when the session starts |
| Deleting a timer | Its sessions stay, with the timer's name and colour copied onto each one |
| History | Totals today / this week / this month / this year, 30-day bar chart, time per timer this month, recent sessions (deletable) |
| Weeks and days | Weeks start Monday; days in server local time (`time.Local`), as in Reader and Flash |
| Export / admin | `Exporter` and `Stater`; no background jobs |

## Architecture

A new app, `internal/apps/focus`, ID `focus`, name "ON Focus", tables
`focus_*`, mounted at `/focus/`. It is the same shape as ON Flash and ON
Later: Go templates with htmx for every page, and one plain script,
`focus.js`, for the running page. No JSON API beyond the one endpoint that
records a session.

The browser owns the running timer. The server owns two things: the saved
timers, and the sessions it is told about when they end. This keeps the
server simple (no live state, no jobs) at the cost of cross-device resume,
which is out of scope.

To keep the riskiest logic testable in Go, the **server expands a timer into
its phase list** and embeds it as JSON on the running page. `focus.js` only
walks that list and does the time maths.

### Swatch rename

ON Flash's deck palette moves from Flash-named to suite-wide names in
`internal/ui/static/app.css`:

- `.deck-c-<name>` → `.swatch-c-<name>`
- `--deck` / `--deck-soft` → `--swatch` / `--swatch-soft`

Same eight names (`teal blue purple pink coral amber green gray`), same
values, same dark-mode overrides. Flash's templates, CSS and tests are
updated to the new names; no behaviour change. Focus keeps its own copy of
the name list in Go (apps never import each other — PATTERNS.md "Cross-app
mirroring"); the CSS is the shared part.

## Data model

Migrations `focus:0001_timers.sql` and `focus:0002_sessions.sql`.
Timestamps are TEXT in `db.TimeLayout` like every other table.

### `focus_timers`

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | |
| `user_id` | INTEGER NOT NULL | FK users, `ON DELETE CASCADE` |
| `name` | TEXT NOT NULL | 1–80 characters after trimming |
| `color` | TEXT NOT NULL DEFAULT `'teal'` | one of the palette names |
| `kind` | TEXT NOT NULL | `single` or `intervals` (CHECK) |
| `focus_minutes` | INTEGER NOT NULL | 1–180 |
| `break_minutes` | INTEGER NULL | intervals only, 1–60 |
| `long_break_minutes` | INTEGER NULL | intervals only, 1–60 |
| `rounds` | INTEGER NULL | intervals only, 1–12 |
| `long_break_every` | INTEGER NULL | intervals only, 1–`rounds` |
| `auto_advance` | INTEGER NOT NULL DEFAULT 1 | intervals only in practice; stored for both |
| `chime` | TEXT NOT NULL DEFAULT `'bell'` | `bell`, `bowl`, `soft` or `silent` |
| `keep_history` | INTEGER NOT NULL DEFAULT 1 | |
| `position` | INTEGER NOT NULL | tile order; new timers go last |
| `created_at`, `updated_at` | TEXT NOT NULL | |

Interval columns are NULL for `single` and required for `intervals`; the
store enforces it and a CHECK constraint backs it up. Saving a timer as
`single` clears them.

"Long break every N" with N = `rounds` means the long break would only come
after the last round, which never happens (there is no trailing break) — so
it simply means "no long break". The form says so in its hint text.

### `focus_sessions`

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | |
| `user_id` | INTEGER NOT NULL | FK users, `ON DELETE CASCADE` |
| `timer_id` | INTEGER NULL | FK `focus_timers`, `ON DELETE SET NULL` |
| `timer_name` | TEXT NOT NULL | copy at session start |
| `color` | TEXT NOT NULL | copy at session start |
| `client_id` | TEXT NOT NULL | random ID from the browser; `UNIQUE(user_id, client_id)` |
| `started_at`, `ended_at` | TEXT NOT NULL | |
| `focus_seconds` | INTEGER NOT NULL | focus time actually done, excluding pauses and breaks; ≥ 60 |
| `rounds_done` | INTEGER NOT NULL | focus phases that ran to their end; a skipped or interrupted one doesn't count (a finished single timer is 1) |
| `completed` | INTEGER NOT NULL | ran to the end vs stopped early |

Index on `(user_id, started_at)` for history and stats.

A session belongs to the local day it **started** on. A session crossing
midnight is not split.

## Phase list

`Phases(t Timer) []Phase`, where `Phase` is `{Kind, Seconds, Round}` and
`Kind` is `focus`, `break` or `long_break`.

- Single: one `focus` phase, round 1.
- Intervals: `focus` round 1, then for each following round a break and a
  `focus`. The break after round *k* is `long_break` when *k* is a multiple
  of `long_break_every`, otherwise `break`. No break after the last round.

Example — Deep work, 50/10 × 4, long 30 every 2:
`F1 50 · B 10 · F2 50 · L 30 · F3 50 · B 10 · F4 50`.

## The runner (`focus.js`)

Lives in `internal/apps/focus/static/focus.js`, loaded only on the running
page. The page carries the phase list and timer settings (`id`, name,
colour, chime, auto-advance, keep-history) as a JSON `<script
type="application/json">` block, which is CSP-safe.

### State and timing

State, mirrored to `localStorage` under one key (one active session per
browser) on every change:

```
{clientId, timerId, timerName, color, chime, autoAdvance, keepHistory,
 phases, startedAt, phaseIndex, phaseStartedAt, pausedAt,
 pausedTotalInPhase, focusSecondsBanked, waiting}
```

Time left is always derived from `Date.now()` and these timestamps; the
once-a-second tick only redraws. Background-tab timer throttling therefore
never makes it drift.

Settings are copied into the state at Start, so editing a timer mid-session
doesn't change the running one — including Keep history.

### Controls

| Control | Key | Effect |
|---|---|---|
| Pause / Resume | Space | Ring dims while paused; paused time doesn't count |
| Skip | S | Ends the current phase now and moves on; focus time already done still counts. Hidden for single timers |
| Restart | R | Restarts the *current phase* |
| Full screen | F | Toggles the Fullscreen API |
| Exit | Esc | Asks "End this session?"; records it if eligible, then returns home. Esc in real full-screen only leaves full-screen (the browser handles it) |

### Phase end

1. Play the timer's chime (unless silent).
2. If the tab is hidden and permission was granted, show a notification:
   "Deep work — break time" / "Deep work — round 3" / "Deep work — done".
3. Auto-advance on: start the next phase. Off: show a full ring and a
   "Start break" / "Start round 3" button, with Space starting it.
4. After the last phase: a "Done — 3h 20m focused" screen with a Back
   button. The session is recorded.

If the page is opened (or the tab wakes) after several phases have passed,
the runner walks forward through them in one go and plays one chime, not
one per missed phase. With auto-advance off it stops at the first boundary,
as it would have if the tab had stayed open.

### Chimes

Synthesised with the Web Audio API — no audio files. `bell` (default),
`bowl`, `soft`, `silent`. The form has a ▶ button to preview each one, which
also gets the browser's audio context unlocked by a user gesture.

Browsers keep audio locked until the person interacts with the page, and
▶ is a click on the home page, not the running page. If audio is still
locked when the timer starts, the running page says "Sound is off — click
anywhere to turn it on" and the first click or key unlocks it.

### Tab title

`31:12 · Deep work` during focus, `07:40 · Break` during a break,
`Paused · Deep work` when paused.

### Notification permission

Requested in the ▶ click on the home page (the page waits for the answer
before opening the running page), or on the first control click on the
running page if it was opened another way; never on page load. Denied or
unsupported means chimes only; nothing else changes.

### Resume

- Opening `/focus/run/{id}` with a stored session for the same timer
  resumes it. With a stored session for a *different* timer, the page asks
  "End Deep work and start Reading?" before replacing it.
- The home page reads `localStorage` and shows a banner: "Resume Deep work
  — 31:12 left", linking to its running page. A session waiting at a phase
  boundary (auto-advance off) shows "Deep work — ready for round 3". If the
  stored session already ran to its end, the banner says "Deep work
  finished" and the session is recorded on the spot. While a session runs,
  the banner also has an **End** button: it asks, then ends and records the
  session like Exit (#546 — this also clears a session whose timer was
  deleted).

The stored session carries the signed-in user's ID, and a page ignores a
session that belongs to someone else, so accounts sharing a browser don't
see or record each other's sessions. Starting a timer still replaces it:
there is one session per browser.

### Recording a session

On finish or Exit, if `keepHistory` and focus time ≥ 60 s, POST the summary
to `/focus/sessions`. On `pagehide` with a session in progress nothing is
sent — the session is still running and will be resumed. The POST is a
`fetch` with `keepalive: true`, so it still completes if the page unloads
first (Exit confirmed, then the tab closed); unlike `sendBeacon` it can
carry the CSRF header.

The stored state is cleared only once the server confirms (2xx, including
"already recorded"), or refuses it for good (400/422 — retrying can't
help). Anything else — offline, signed out, a server error — keeps it: the
Done screen says "Couldn't save this session" with a Retry button, and the
home banner offers the same. A session being replaced by another timer's is
recorded first; if that fails it is dropped with a console warning.

## Pages and routes

Everything requires a signed-in user. Missing or foreign IDs are 404.

| Route | Purpose |
|---|---|
| `GET /focus/` | Home |
| `GET /focus/new` | New timer form |
| `POST /focus/timers` | Create |
| `GET /focus/timers/{id}/edit` | Edit form |
| `POST /focus/timers/{id}` | Update |
| `POST /focus/timers/{id}/duplicate` | Copy as "<name> (copy)", placed right after the original |
| `POST /focus/timers/{id}/delete` | Delete; confirm dialog says history is kept |
| `POST /focus/timers/order` | Save tile order (list of IDs) |
| `GET /focus/run/{id}` | Running page |
| `POST /focus/sessions` | Record one session (JSON) |
| `GET /focus/history` | History and stats |
| `GET /focus/today` | The today strip alone, for the home page to refresh after recording |
| `POST /focus/sessions/{id}/delete` | Delete one session |

### Home

Toolbar: "Timers", "History" and "+ New timer". Under it the **today
strip**: today's focus time, today's session count, this week's focus time.
It appears once the user has recorded any session, and then stays, showing
0m on quiet days. Then the resume
banner (if any) and the **tiles**: a coloured top edge, name, a summary line
(`15 min` or `50 / 10 × 4 · long 30`), a pill with total length, a ▶ button
in the timer's colour that opens the running page and starts at once, and a
⋯ menu with Edit / Duplicate / Delete. Tiles can be dragged to reorder,
using native drag and drop as ON Notes does. Empty state: a short line and a
"Create your first timer" button.

### Timer form

Name; colour swatches (the `swatch-c-*` picker, as Flash's deck form);
Single / Intervals toggle — the interval fields (break, long break, rounds,
long break every) show only for Intervals; "Start next phase
automatically" (intervals only); chime select with ▶ preview; "Keep
history". Validation errors re-render the form with messages next to the
fields.

### Running page

Focus mode: the suite nav and toolbar are hidden. The page opts out of the
shell chrome the way ON Later's reading view does: app.css hides `.shell-bar`,
`.app-sidebar` and `.app-footer` with `body:has(.focus-runner)`, so the page
still gets CSRF, theme and the logged-in user from `Deps.Page`.

Layout, centred: Exit (top left) and Full screen (top right) as quiet text
buttons; the ring with the timer name above the countdown, the phase label
below it ("Focus · round 2 of 4", "Short break"), and round dots below that;
Pause / Skip / Restart under the ring.

- **Focus:** ring and filled dots in the timer's colour (`--swatch`),
  background the page's subtle cream.
- **Break:** warm grey ring, labelled "Short break" or "Long break".
- **Paused:** ring and digits dimmed, "Paused" replaces the phase label.
- **Dots:** one per round; filled = done, ringed = current, hollow = to
  come. Interval timers only.
- **Single timers:** no phase label, no dots, no Skip.

### History

- Four totals: today, this week, this month, this year.
- Bar chart of focus minutes per day for the last 30 days, oldest first,
  including empty days. Server-rendered SVG bars, like Flash's stats
  (sizes in SVG attributes, so the CSP holds).
- Time per timer this month, largest first, grouped by `timer_name`.
- Recent sessions, newest first, 50 per page with an "Older" link: date and
  time, timer name with its colour, focus time, rounds, a "stopped early"
  marker when not completed, and a delete button.

## Recording endpoint

`POST /focus/sessions`, JSON, CSRF token in the header as htmx requests
send it. Body:

```
{client_id, timer_id, timer_name, color, started_at, ended_at,
 focus_seconds, rounds_done, completed}
```

`started_at` and `ended_at` are milliseconds since the epoch, as the
browser keeps them. A new session answers 201, a repeat 200, both with
`{"id": …}`.

Server rules:

- `focus_seconds` < 60 → 422, nothing stored (the client shouldn't send
  these; the server enforces it anyway).
- `focus_seconds` > `ended_at` − `started_at`, `ended_at` before
  `started_at`, or `started_at` or `ended_at` more than 5 minutes in the
  future → 422 (the browser's clock and the server's can disagree a
  little).
- `timer_id` not found or not the user's → stored as NULL (the timer may
  have been deleted mid-session); never an error.
- `color` not in the palette → stored as `gray`; `timer_name` trimmed and
  capped at 80 characters.
- Duplicate `(user_id, client_id)` → 200 with the existing row, nothing
  changed.

The server does not check `keep_history`: that was decided at session start
and lives in the client's state.

## Export and admin

- `Exporter`: `{timers: [...], sessions: [...]}` for the user, all fields
  except `user_id`. Sessions reference timers by ID; a deleted timer's
  sessions keep their copied name and colour.
- `Stater`: an admin card with the number of timers, sessions recorded, and
  total focus hours.
- No `Scheduler`.

## Errors

Follow PATTERNS.md's error-surfacing patterns.

- Form validation: re-render with field messages (422).
- Delete / duplicate / reorder failures: the standard htmx notice.
- Session recording failures: Done screen and home banner offer Retry; the
  session stays in `localStorage` until confirmed.
- Corrupt or unreadable `localStorage` state is discarded with a console
  warning; the page behaves as if nothing was running.

## Testing

- Store tests against a real SQLite file in a temp dir; handler tests with
  `apptest.Clock`.
- Table-driven validation: limits, interval-only fields required for
  `intervals` and cleared for `single`, `long_break_every` ≤ `rounds`,
  unknown colour or chime rejected.
- `Phases`: single; intervals with and without long breaks;
  `long_break_every` = `rounds`; one round; no trailing break.
- Recording: under-a-minute rejected; impossible timings rejected; duplicate
  `client_id` stored once; foreign `timer_id` stored as NULL; deleting a
  timer keeps its sessions with name and colour.
- Stats: Monday week start, local-day boundaries, month and year totals,
  30-day series including empty days. Day-keyed tests use a fixed local
  noon (the #521 lesson).
- Ownership: every route 404s on another user's timer or session.
- Arch test passes; Flash's tests pass after the swatch rename.
- `focus.js` has no automated tests (the suite has no JS test harness). Each
  phase plan includes manual checks in the browser pane: pause/resume,
  skip, restart, auto-advance on and off, reload mid-phase, reopen after the
  session ran out, notification and chime, full-screen, tab title.

## Docs

A user guide page for ON Focus in the suite help, with screenshots, in F4.
AGENTS.md's app list and the registered-app tests are updated in F1.

## Phases

One PR each, in order.

| Phase | Issue | Scope |
|---|---|---|
| F1 | #493 Saved timers | Swatch rename; app skeleton and registration; both migrations; `Phases`; home with tiles; create, edit, duplicate, delete, reorder (home.js); short user guide. ▶ opens a placeholder running page |
| F2 | #494 Running a timer | Focus mode page, ring and dots, controls and keys, auto-advance and waiting, chimes, notifications, tab title, `localStorage` resume, home resume banner. Until F3, finishing or exiting a session clears it without recording |
| F3 | #495 History and stats | Recording endpoint (keepalive `fetch`) with Retry; today strip; History page; session delete; `Exporter`; admin card; banner End (#546) |
| F4 | #496 Polish | Screen Wake Lock while running; home shortcuts (N new timer, 1–9 start the *n*th timer); dark-mode pass; user guide and screenshots |

## Out of scope

- Ambient sounds (rain, noise)
- Custom step sequences ("Stretch 5 → Meditate 10 → Journal 15")
- Server-side running state and cross-device resume
- Syncing a running session across tabs of the same browser
- Linking a session to a Notes item or a book in ON Books
- A free-text note per session
- An optional label under the countdown for single timers
