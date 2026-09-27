# ON Suite — Trigger Jobs from the UI

**Date:** 2026-09-27
**Status:** Proposed
**Issue:** #311
**Scope:** Admins can run any registered background job on demand from a new
admin-only page, `/admin/jobs`. The job runs in the background and the page
refreshes itself until it finishes. No schema change, no new dependency.

## 1. Purpose

Every piece of background work runs on a timer: two platform jobs
(*sweep expired sessions*, *database snapshot*) and three app jobs (Reader's
*refresh feeds* and *purge old articles*, Flash's *purge orphan media and
tags*). To run one early you have to shell in (and only the snapshot and
session sweep have a CLI, `onsuite backup`). The common case is "take a snapshot now,
before I upgrade", and a deployment with `--backup-interval 0` (cron-driven
backups) cannot do even that from the browser.

## 2. Decisions

Settled during brainstorming.

| Question | Decision |
|---|---|
| Which jobs? | Every registered job, platform and app alike. Disabled jobs (interval 0) can be triggered too. |
| Backup downloads? | Out of scope. Serving the whole database (password hashes included) over HTTP is its own issue. |
| Where does it live? | A new admin-only page, `/admin/jobs`. `/admin/` stays read-only and its Jobs section gains a **Manage jobs →** link, the same shape as #310's users card. |
| Synchronous or background? | Background. The POST starts the job and redirects straight back; the table polls while anything is running. |
| Confirm step? | No. Every job is idempotent housekeeping, and admins are trusted to know what they are clicking. |

## 3. Scheduler changes (`internal/platform/jobs`)

The package stays a leaf: no new imports from this module.

### 3.1 Status

`Status` gains three fields:

- `Slug string` — URL-safe name derived once at `Register` from `Name`:
  lower-cased, runs of anything that is not `[a-z0-9]` collapsed to one `-`,
  trimmed of leading/trailing `-` ("database snapshot" → `database-snapshot`).
  `Register` panics on a duplicate slug; that is a programming error caught by
  the first test that builds the stack, the same way `app.NewRegistry` rejects
  a duplicate app id.
- `Running bool` — true from the moment a run starts until it is recorded.
- `LastManual bool` — whether the most recent recorded run was triggered by
  hand. The table renders it as "(manual)" after Last run.

### 3.2 Trigger

```go
var (
    ErrUnknownJob     = errors.New("jobs: unknown job")
    ErrAlreadyRunning = errors.New("jobs: job is already running")
    ErrNotStarted     = errors.New("jobs: scheduler is not running")
)

// Trigger starts the job with this slug in its own goroutine and returns
// without waiting for it.
func (r *Registry) Trigger(slug string) error
```

- The run uses the context `Run` was given, never a request's. Closing the
  browser tab cannot cancel a half-written snapshot; shutting the server down
  cancels manual and scheduled runs alike.
- `Run` records its context before anything else, **including** when no job
  is enabled and it returns immediately — so a deployment with every job
  disabled can still trigger them. `Trigger` before `Run` has been called
  returns `ErrNotStarted`.
- Checking `Running` and setting it happen under the registry mutex in one
  critical section, so two concurrent triggers cannot both start.

### 3.3 No overlap

A job never runs twice at once:

- A manual trigger of a running job returns `ErrAlreadyRunning`.
- A timer tick that fires while the job is running (manually started or a
  slow previous tick) is **skipped**, not queued. The next tick runs as normal.

### 3.4 Manual runs do not move the timer

Today `run()` sets `NextRun = end + Interval`. That is only true for a
scheduled run, because the ticker is not reset by anything else. A manual run
records `Runs`, `LastRun`, `LastDuration`, `LastErr` and `LastManual` and
leaves `NextRun` untouched, so Next run on the page stays honest. A disabled
job's `NextRun` stays zero.

### 3.5 Removals

`RunOnceForTest` and the doc comment saying the package "deliberately offers
no way to trigger a job from a request" are removed. Tests use `Trigger` plus
a small test helper that waits for `Running` to go false (§6).

## 4. The page (`internal/platform/jobsadmin`)

A new platform package, a sibling of `admin` and `usermgmt`. Its name is
about jobs, so it does not stretch `usermgmt` (which is about users). It
imports `app` (for `app.NewPage`), `jobs`, `render` and `web`, sits at the top
of the platform, and nothing imports it but `cmd/onsuite`.

```go
type Deps struct {
    Jobs    *jobs.Registry
    Render  *render.Renderer
    Errors  *web.Errors
    Log     *slog.Logger
    Nav     []render.NavItem
    Version string
}

func Routes(mux *http.ServeMux, rec *web.Recorder, authn *web.Auth, d Deps)
```

Mounted in `buildStack` next to `usermgmt.Routes`. Every pattern is exact (no
trailing `/`), for the same unguarded-redirect reason documented at the
`/admin` registration.

| Route | Guard | Does |
|---|---|---|
| `GET /admin/jobs` | `RequireAdmin` | Full page. |
| `GET /admin/jobs/table` | `RequireAdmin` | The table fragment only, for polling. |
| `POST /admin/jobs/{slug}/run` | `RequireAdmin` | `Trigger(slug)`. |

### 4.1 Run now

Each row has a plain `<form method="post" action="/admin/jobs/{slug}/run">`
with the CSRF token and a **Run now** button. No HTMX on the button: the
POST-redirect-GET round trip is the whole interaction.

- Success → `303 See Other` to `/admin/jobs`, which renders the row as
  running.
- `ErrAlreadyRunning` → re-render the page with status 422 and a
  `.notice.notice-error` ("… is already running."), the same shape as
  usermgmt's rejections.
- `ErrUnknownJob` → `Errors.NotFound`.
- `ErrNotStarted` → 503 via `Errors` (only reachable if the scheduler failed
  to start).
- While a row is running, its button is `disabled`.

### 4.2 Polling

The table is one `{{define}}` block rendered by both the page and the
fragment route. When any job has `Running`, the table element carries:

```html
hx-get="/admin/jobs/table" hx-trigger="every 2s" hx-swap="outerHTML"
```

When nothing is running, the fragment is rendered without those attributes,
so the swap that shows the finished outcome also stops the polling. No
JavaScript beyond HTMX; CSP untouched.

### 4.3 Columns

Same as the `/admin/` Jobs table — Job (name and description), Every, Last
run, Took, Outcome, Next run, Runs — plus the Run now button. Last run gains
"(manual)" when `LastManual`; Outcome shows a **running** tag while running.

### 4.4 `/admin/`

The Jobs section gets a **Manage jobs →** link to `/admin/jobs`. Nothing else
changes; `/admin/` stays read-only.

## 5. Logging

Every successful trigger logs at Info: `job triggered`, with `job` (the name)
and `by` (the admin's username, from `web.UserFrom`). The job's own success/failure logging is
unchanged.

## 6. Testing

| Package | Tests |
|---|---|
| `jobs` | slug derivation table; duplicate slug panics; `Trigger` runs the job and records `LastManual`; `Trigger` on a running job returns `ErrAlreadyRunning`; a tick during a run is skipped; manual runs leave `NextRun` alone (enabled and disabled jobs); a disabled job can be triggered; `Trigger` before `Run` returns `ErrNotStarted`; `Trigger` after `Run` returned early (nothing enabled) still works; unknown slug; a panicking manual run is recorded, not fatal; everything under `-race`. A package-internal test helper waits for a job's `Running` to clear. |
| `jobsadmin` | through the real `web.Stack`: anonymous → 303 to `/login`; non-admin → byte-identical to `/nope` for all three routes; admin GET → 200 with every job listed; admin POST → 303 and the job ran; POST without CSRF → rejected; unknown slug → 404; already running → 422 with the notice; the fragment carries `hx-trigger` only while a job is running. |
| `admin` | the Jobs section links to `/admin/jobs`. |
| `arch` | `jobsadmin` added to `TestScanSeesTheRealTree`. |

## 7. Out of scope

- Downloading snapshots, or listing the backups directory.
- Persisted run history (status still resets on restart).
- Editing schedules at runtime.
- Cancelling a running job.
- Per-job arguments (e.g. "refresh this one feed" — Reader already has a
  per-feed refresh).

## 8. Documentation

- `docs/superpowers/specs/2026-08-24-admin-page-design.md`: a note under §12
  that "trigger a backup, sweep sessions" is now covered by this spec.
- `AGENTS.md`: the operations paragraph mentions `jobsadmin` and that jobs can
  be triggered from `/admin/jobs`.
- `docs/DEPLOYING.md`: the backups section mentions **Run now** as the way to
  take an ad-hoc snapshot from the browser.
