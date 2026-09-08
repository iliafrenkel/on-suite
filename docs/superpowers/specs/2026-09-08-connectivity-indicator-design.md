# Connectivity indicator + offline-aware error handling

## Problem

When the browser loses its connection to the server (network drop, server
restart, etc.), interactive actions — drag/drop, collapse/expand, inline
edits — currently fail silently or inconsistently. Some apps (Notes) already
revert and show an error for a server-level failure (`htmx:responseError`),
but there is no signal at all for a network-level failure
(`htmx:sendError`), and no persistent indication anywhere in the UI that the
user is currently offline. A user can keep interacting for a while before
noticing nothing is actually being saved.

## Goals

- A small, always-visible connectivity indicator in the shared shell bar,
  so any page in any app shows current online/offline state without the
  user needing to trigger an action first.
- Detection combines three signals: browser `online`/`offline` events, a
  periodic poll of the existing `/healthz` endpoint, and any HTMX
  network-level failure (`htmx:sendError`) — because `navigator.onLine`
  alone is unreliable (true even when the server is unreachable) and
  `/healthz` also catches a "server up, DB down" state that `online`
  events can't see.
- A shared, reusable inline-error + revert helper so every app's
  interactive HTMX actions behave consistently on failure, instead of each
  app inventing its own handling.
- Applies globally: the indicator and shared error helper live in common
  code (`base.html`, a new shared JS module), not duplicated per app.

## Non-goals (explicitly deferred)

- Auto-retry or queueing of failed actions on reconnect. On recovery, the
  user simply redoes whatever they attempted while offline.
- Pre-emptively disabling drag handles, collapse toggles, etc. while
  offline. Controls stay enabled; a failed attempt reverts and shows an
  inline error.
- Hysteresis/debouncing beyond "one failed signal = offline, one
  successful signal = online." If real-world use shows the indicator
  flickering on flaky connections, that's a follow-up.
- Cross-tab coordination. Each open tab detects and polls independently.
- Any change to `/healthz` itself — it's reused as-is.

## Design

### Components

- **`internal/ui/static/connectivity.js`** (new, shared): the only place
  that owns connectivity state (`online` / `offline`). Loaded once from
  `base.html`, alongside the existing `theme.js`, so it runs on every page
  in every app with no per-app wiring.
- **Shell-bar indicator** (`internal/ui/templates/base.html` +
  `internal/ui/static/app.css`): a small dot next to `.shell-user`, styled
  using the existing `.outline-dot` visual pattern
  (`data-status="online"` / `"offline"`), with a `title` tooltip —
  "All changes are saving normally" vs. "You're offline — changes won't be
  saved until you reconnect."
- **Shared inline-error helper**: the `.notice-error` + revert pattern
  currently duplicated three times in `internal/apps/notes/static/notes.js`
  (paste, drag-to-move, inline edit — see `notes.js:581`, `:846`, `:975`,
  `:981`) is pulled into one small shared function any app can call from
  its own `htmx:responseError` / `htmx:sendError` listeners. This
  deduplicates existing behavior rather than introducing a new UI pattern,
  and lets Paste (and future apps) reuse it instead of reinventing it.
- `/healthz` (`cmd/onsuite/serve.go:135`) is reused unchanged as the poll
  target; the poll is a plain `fetch()`, never an HTMX request, so it
  cannot recursively trigger HTMX's own error events.

### Data flow

- **Page load:** `connectivity.js` assumes online, immediately fires one
  `/healthz` check to establish real initial state, then polls every 15
  seconds thereafter.
- **Browser fires `offline`:** state flips to offline immediately, dot
  updates. Browser fires `online`: state flips to online immediately, but
  is still subject to correction by the next poll or HTMX result, since
  `navigator.onLine` can be wrong.
- **An HTMX request fails at the network level** (`htmx:sendError`): state
  flips to offline immediately (does not wait for the next poll), the dot
  updates, and the app's own listener (in `notes.js` etc.) reverts that
  action's optimistic UI change and shows an inline `.notice-error` via
  the shared helper.
- **An HTMX request gets a 4xx/5xx response** (`htmx:responseError`,
  server reachable): not treated as a connectivity issue — the dot stays
  online. Handled purely by the app's existing revert + inline-error
  logic, same as today.
- **Poll succeeds after being offline:** state flips to online, dot
  updates. `connectivity.js` dispatches a `connectivity:change` custom
  event (`{ detail: { online } }`) on `document` for any app code that
  wants to react, though no app currently needs to — no auto-retry, per
  the non-goals above.

### Edge cases

- **Flapping:** no hysteresis — one failed poll/request means offline, one
  success means online. Deliberately simple; revisit only if real usage
  shows flicker on flaky connections.
- **Multiple tabs:** each tab polls independently. Negligible cost at one
  small request per 15s per open tab; no cross-tab state sharing.
- **Server up, DB down:** `/healthz` already pings the DB
  (`cmd/onsuite/serve.go:135`), so this state correctly shows as offline
  even though the network and HTTP server are technically reachable —
  matches the user's real experience, since saves would fail either way.
- **Apps with no existing revert handling:** `notes.js` already reverts on
  `htmx:responseError` for drag-to-move, paste, and inline edit; this
  design extends those same listeners to also treat `htmx:sendError`
  identically (revert + shared inline error). Any interactive action in
  any app that currently has no revert handling at all is a gap to be
  identified and filled during planning, not a change to this design.

## Testing

- **Manual, browser devtools:** use network throttling ("Offline") to
  verify the shell-bar dot flips within one poll cycle (or immediately via
  the browser `offline` event), that a drag/drop or collapse/expand
  attempted while offline fails-and-reverts with a visible inline error,
  and that the dot recovers once throttling is turned off.
- **Manual, server-down simulation:** stop the local server process while
  a page is open with the browser still "online" — confirms detection via
  the `/healthz` poll path (network-level `htmx:sendError` in this case),
  independent of browser-level online/offline events.
- **Manual, DB-down simulation:** if feasible in the dev environment, take
  the database offline while the HTTP server keeps running — confirms
  `/healthz` correctly reports offline via its DB ping.
- No new Go tests are needed — `/healthz` itself is unchanged. This is a
  frontend-only feature, validated in-browser.
