# Connectivity Indicator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a shell-bar online/offline indicator, backed by browser events + a `/healthz` poll + HTMX network-failure detection, and extend Notes' existing per-action error handling to cover offline failures using a newly shared notice helper.

**Architecture:** Two small, framework-free JS modules loaded globally from `internal/ui/templates/base.html` (mirroring how `theme.js` is already loaded today): `connectivity.js` owns online/offline state and updates a dot in the shell bar; `htmx-notices.js` is a shared insert/clear helper for the `.notice.notice-error` pattern already used (and duplicated) in `internal/apps/notes/static/notes.js`. No Go backend changes — the existing `/healthz` endpoint (`cmd/onsuite/serve.go:135`) is reused as-is as the poll target.

**Tech Stack:** Go `html/template` + vanilla JS (no build step, no framework — see AGENTS.md), existing `htmx.min.js` 2.0.10.

## Global Constraints

- No Node/npm/JS build step and no new JS dependency — plain IIFE files, matching `internal/ui/static/theme.js`'s existing style exactly (`"use strict"`, `function` declarations, a single top-level call at the bottom).
- `main` is protected: every task's commit goes on the feature branch already checked out for this work; no direct push to `main`.
- Poll interval is exactly 15000ms (15s) per the approved spec.
- No auto-retry of failed actions, no pre-emptive disabling of drag/collapse/edit controls while offline — controls stay enabled and fail-and-revert on attempt, per the approved spec's non-goals.
- No hysteresis: one failed signal (poll, sendError, or browser `offline` event) flips state to offline; one successful signal flips it back to online.
- Full check (`gofmt -l .`, `go vet ./...`, `staticcheck ./...`, `go mod tidy` diff check, `go test ./... -race -count=1`) must stay green after every commit that touches `.go` files (per AGENTS.md).
- Spec reference for all behavior described here: [docs/superpowers/specs/2026-09-08-connectivity-indicator-design.md](../specs/2026-09-08-connectivity-indicator-design.md).

---

### Task 1: Shell-bar indicator markup + render test

**Files:**
- Modify: `internal/ui/templates/base.html:39-40`
- Modify: `internal/ui/static/app.css` (insert after line 267, before the `/* ---- App shell: sidebar + main ---- */` comment)
- Modify: `internal/platform/render/render_test.go` (new test, alongside `TestPageRendersADocumentWithTheShell`)

**Interfaces:**
- Produces: a `[data-conn-indicator]` element inside `.shell-user`, with `data-status` of `"online"` or `"offline"`, and a child `.conn-dot` for the visual dot. This is the element `connectivity.js` (Task 2) looks up and mutates.

This is a `<div>`, deliberately not a `<span>` — `TestPageRendersADocumentWithTheShell` asserts `.shell-user span` is the first `<span>` under `.shell-user` (the username). A new `<span>` inserted before it would silently break that assertion by matching first instead.

- [ ] **Step 1: Write the failing test**

Add to `internal/platform/render/render_test.go`, near `TestPageRendersADocumentWithTheShell`:

```go
// TestShellHasConnectivityIndicator covers the element connectivity.js
// looks up and mutates (see internal/ui/static/connectivity.js) — its
// initial state must be "online" so a page rendered before the script
// runs never flashes a false "offline" reading.
func TestShellHasConnectivityIndicator(t *testing.T) {
	r := testRenderer(t)
	rec := httptest.NewRecorder()

	err := r.Page(rec, http.StatusOK, "error", render.Page{
		Shell: render.Shell{LoggedIn: true, Username: "ilia", CSRFToken: "tok123"},
		Data:  map[string]any{"Status": 404, "Title": "Not found", "Message": "no such page"},
	})
	if err != nil {
		t.Fatalf("Page: %v", err)
	}

	doc := htmlassert.Parse(t, rec.Body.String())
	indicator := doc.MustHave("[data-conn-indicator]")
	if got, _ := htmlassert.Attr(indicator, "data-status"); got != "online" {
		t.Errorf("data-status = %q, want %q", got, "online")
	}
	doc.MustHave(".shell-user [data-conn-indicator] .conn-dot")

	// Still the username test's first .shell-user span, unaffected by the
	// new indicator (which is a div, not a span).
	if got := htmlassert.Text(doc.MustHave(".shell-user span")); got != "ilia" {
		t.Errorf("username = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/render/... -run TestShellHasConnectivityIndicator -v`
Expected: FAIL — `htmlassert: no element matches "[data-conn-indicator]"`

- [ ] **Step 3: Add the indicator markup**

In `internal/ui/templates/base.html`, change:

```html
	<div class="shell-user">
		<span>{{.Shell.Username}}</span>
```

to:

```html
	<div class="shell-user">
		<div class="conn-indicator" data-conn-indicator data-status="online" title="All changes are saving normally" aria-label="Online — all changes are saving normally">
			<span class="conn-dot" aria-hidden="true"></span>
		</div>
		<span>{{.Shell.Username}}</span>
```

- [ ] **Step 4: Add the CSS**

In `internal/ui/static/app.css`, insert after the `.shell-settings-menu button[data-theme-value].active` rule (line 267) and before the `/* ---- App shell: sidebar + main ---- */` comment:

```css
/* Online/offline dot — see internal/ui/static/connectivity.js, which owns
 * data-status on this element after page load. Initial markup ships
 * "online" so a page rendered before the script runs never flashes a
 * false "offline" reading. */
.conn-indicator {
	display: inline-flex;
	align-items: center;
}

.conn-dot {
	width: 0.5rem;
	height: 0.5rem;
	border-radius: 50%;
	background: #16a34a;
}

.conn-indicator[data-status="offline"] .conn-dot {
	background: var(--c-danger);
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/platform/render/... -run TestShellHasConnectivityIndicator -v`
Expected: PASS

- [ ] **Step 6: Run the full render package suite to check for regressions**

Run: `go test ./internal/platform/render/... -v`
Expected: PASS (all tests, including `TestPageRendersADocumentWithTheShell`)

- [ ] **Step 7: Commit**

```bash
git add internal/ui/templates/base.html internal/ui/static/app.css internal/platform/render/render_test.go
git commit -m "feat(ui): add shell-bar connectivity indicator markup"
```

---

### Task 2: connectivity.js — detection + indicator updates

**Files:**
- Create: `internal/ui/static/connectivity.js`
- Modify: `internal/ui/templates/base.html:9-12`

**Interfaces:**
- Consumes: `[data-conn-indicator]` (Task 1) — the element this script looks up via `document.querySelector` and mutates (`data-status`, `title`, `aria-label`).
- Produces: a `connectivity:change` `CustomEvent` dispatched on `document`, with `detail: { online: boolean }`. Not consumed by any task in this plan, but is the extension point the spec calls out for future app code.

No automated test exists for this step (the repo has no JS test runner by design — see AGENTS.md's "no Node/npm/JS build step"). Verification is manual, in-browser, per the spec's own testing plan.

- [ ] **Step 1: Create the file**

Create `internal/ui/static/connectivity.js`:

```js
// internal/ui/static/connectivity.js
//
// Online/offline indicator for the shared shell bar, and the earliest
// possible offline signal for any HTMX action anywhere in the suite.
// navigator.onLine only reflects the network interface, not whether the
// server itself is reachable (the server can be down, or its database can
// be down behind a live server — see healthzHandler in
// cmd/onsuite/serve.go), so this combines three signals: the browser's own
// online/offline events, a periodic /healthz poll, and any htmx:sendError
// (a network-level HTMX failure) bubbling up from anywhere on the page.
// See docs/superpowers/specs/2026-09-08-connectivity-indicator-design.md.
(function () {
	"use strict";

	var POLL_INTERVAL_MS = 15000;
	var online = true;
	var indicator = null;

	function setOnline(next) {
		if (next === online) return;
		online = next;
		indicator.setAttribute("data-status", online ? "online" : "offline");
		indicator.setAttribute("title", online
			? "All changes are saving normally"
			: "You're offline — changes won't be saved until you reconnect");
		indicator.setAttribute("aria-label", online
			? "Online — all changes are saving normally"
			: "Offline — changes won't be saved until you reconnect");
		document.dispatchEvent(new CustomEvent("connectivity:change", { detail: { online: online } }));
	}

	function checkHealth() {
		fetch("/healthz", { cache: "no-store" })
			.then(function (res) { setOnline(res.ok); })
			.catch(function () { setOnline(false); });
	}

	// Gated on the indicator's presence, the same way theme.js's own
	// init* functions each bail out when their target element is absent
	// (e.g. initSidebarToggle) — a logged-out or public page renders no
	// .shell-user (see PATTERNS.md's "Chrome visibility gated on
	// Shell.LoggedIn"), so there is nothing here to poll for.
	function init() {
		indicator = document.querySelector("[data-conn-indicator]");
		if (!indicator) return;

		window.addEventListener("online", function () { setOnline(true); });
		window.addEventListener("offline", function () { setOnline(false); });
		document.addEventListener("htmx:sendError", function () { setOnline(false); });

		checkHealth();
		setInterval(checkHealth, POLL_INTERVAL_MS);
	}

	init();
})();
```

- [ ] **Step 2: Load the script from base.html**

In `internal/ui/templates/base.html`, change:

```html
<script src="{{asset "htmx.min.js"}}" defer></script>
<script src="{{asset "theme.js"}}" defer></script>
{{block "head" .}}{{end}}
```

to:

```html
<script src="{{asset "htmx.min.js"}}" defer></script>
<script src="{{asset "theme.js"}}" defer></script>
<script src="{{asset "connectivity.js"}}" defer></script>
{{block "head" .}}{{end}}
```

- [ ] **Step 3: Manual verification — indicator reflects real state**

Run: `go build ./cmd/onsuite && ./onsuite serve --data-dir ./data` (or your usual local-run command), then open the app in a browser, logged in.

Expected, checked with browser devtools:
- The dot next to your username is green (`[data-conn-indicator]` has `data-status="online"`), and hovering shows "All changes are saving normally".
- Open devtools' Network panel, set throttling to "Offline". Within ~15s (or immediately if the browser fires its own `offline` event first), the dot turns red/danger-colored and the tooltip changes to "You're offline — changes won't be saved until you reconnect".
- Turn throttling back to "No throttling" (or "Online"). Within ~15s, the dot returns to green.

- [ ] **Step 4: Manual verification — server-down path**

Stop the local server process while the page stays open (simulates "server unreachable" independent of the browser's own online/offline events).

Expected: the dot turns red within one poll cycle (≤15s), since `checkHealth`'s `fetch` rejects.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/static/connectivity.js internal/ui/templates/base.html
git commit -m "feat(ui): detect online/offline state and drive the shell-bar indicator"
```

---

### Task 3: Shared htmx-notices.js helper + refactor Notes' duplicated notice functions

**Files:**
- Create: `internal/ui/static/htmx-notices.js`
- Modify: `internal/apps/notes/static/notes.js:559-577` (`showPasteError`, `clearPasteError`)
- Modify: `internal/apps/notes/static/notes.js:824-842` (`showMoveError`, `clearMoveError`)
- Modify: `internal/ui/templates/base.html:9-13` (load the new script before `notes.js`, which loads later via `outline.html`'s own `head` block)
- Modify: `PATTERNS.md:16-22` (update the "Server-rejection surfaced as a dismissable notice" entry's canonical pointer)

**Interfaces:**
- Produces: `window.OnSuite.notices.show(anchorEl, id, message)` and `window.OnSuite.notices.clear(id)`. Task 4 (`initPasteErrors`/`initMoveErrors`) calls these directly.
- Consumes: nothing from earlier tasks.

This step is a pure refactor — `showPasteError`/`showMoveError`'s behavior must not change yet (that's Task 4). No automated test exists for this either; verification is that Notes' existing paste/move error behavior is bit-for-bit the same after the refactor.

- [ ] **Step 1: Create the shared helper**

Create `internal/ui/static/htmx-notices.js`:

```js
// internal/ui/static/htmx-notices.js
//
// Shared implementation of the "server-rejection surfaced as a dismissable
// notice" pattern (see PATTERNS.md): an htmx action fails, and swap:false
// (htmx 2.0's default responseHandling for 4xx/5xx, and the only outcome
// of a network-level sendError) would otherwise leave the user with no
// feedback at all. Originally duplicated per-action in
// internal/apps/notes/static/notes.js (initPasteErrors/initMoveErrors);
// extracted here once the connectivity indicator's offline-aware error
// messages (notes.js) needed the exact same insert/clear logic for a
// third and fourth caller.
(function () {
	"use strict";

	function show(anchor, id, message) {
		if (!anchor || !anchor.parentNode) return;
		if (document.getElementById(id)) return; // already showing one
		var notice = document.createElement("div");
		notice.id = id;
		notice.className = "notice notice-error";
		notice.setAttribute("role", "alert");
		notice.textContent = message;
		anchor.parentNode.insertBefore(notice, anchor);
		notice.scrollIntoView({ block: "nearest" });
	}

	function clear(id) {
		var existing = document.getElementById(id);
		if (existing) existing.remove();
	}

	window.OnSuite = window.OnSuite || {};
	window.OnSuite.notices = { show: show, clear: clear };
})();
```

- [ ] **Step 2: Load it from base.html, before any app-specific script**

In `internal/ui/templates/base.html`, change:

```html
<script src="{{asset "htmx.min.js"}}" defer></script>
<script src="{{asset "theme.js"}}" defer></script>
<script src="{{asset "connectivity.js"}}" defer></script>
{{block "head" .}}{{end}}
```

to:

```html
<script src="{{asset "htmx.min.js"}}" defer></script>
<script src="{{asset "theme.js"}}" defer></script>
<script src="{{asset "htmx-notices.js"}}" defer></script>
<script src="{{asset "connectivity.js"}}" defer></script>
{{block "head" .}}{{end}}
```

All four scripts are `defer`, so they run in document order regardless of position — `htmx-notices.js` here always executes before `outline.html`'s own `<script src="/notes/notes.js" defer>`, which is appended later in the document via the `head` block.

- [ ] **Step 3: Refactor showPasteError/clearPasteError**

In `internal/apps/notes/static/notes.js`, change:

```js
	function showPasteError(status) {
		var outline = document.getElementById("outline");
		if (!outline || !outline.parentNode) return;
		if (document.getElementById("notes-paste-error")) return; // already showing one
		var notice = document.createElement("div");
		notice.id = "notes-paste-error";
		notice.className = "notice notice-error";
		notice.setAttribute("role", "alert");
		notice.textContent = status >= 500
			? "Something went wrong pasting that. Try again."
			: "Couldn't paste that: it doesn't look like valid outline text, or it's too large.";
		outline.parentNode.insertBefore(notice, outline);
		notice.scrollIntoView({ block: "nearest" });
	}

	function clearPasteError() {
		var existing = document.getElementById("notes-paste-error");
		if (existing) existing.remove();
	}
```

to:

```js
	function showPasteError(status) {
		var outline = document.getElementById("outline");
		OnSuite.notices.show(outline, "notes-paste-error", status >= 500
			? "Something went wrong pasting that. Try again."
			: "Couldn't paste that: it doesn't look like valid outline text, or it's too large.");
	}

	function clearPasteError() {
		OnSuite.notices.clear("notes-paste-error");
	}
```

- [ ] **Step 4: Refactor showMoveError/clearMoveError**

In `internal/apps/notes/static/notes.js`, change:

```js
	function showMoveError(status) {
		var outline = document.getElementById("outline");
		if (!outline || !outline.parentNode) return;
		if (document.getElementById("notes-move-error")) return; // already showing one
		var notice = document.createElement("div");
		notice.id = "notes-move-error";
		notice.className = "notice notice-error";
		notice.setAttribute("role", "alert");
		notice.textContent = status >= 500
			? "Something went wrong moving that. Try again."
			: "Couldn't move that there: it would create a cycle or nest too deep.";
		outline.parentNode.insertBefore(notice, outline);
		notice.scrollIntoView({ block: "nearest" });
	}

	function clearMoveError() {
		var existing = document.getElementById("notes-move-error");
		if (existing) existing.remove();
	}
```

to:

```js
	function showMoveError(status) {
		var outline = document.getElementById("outline");
		OnSuite.notices.show(outline, "notes-move-error", status >= 500
			? "Something went wrong moving that. Try again."
			: "Couldn't move that there: it would create a cycle or nest too deep.");
	}

	function clearMoveError() {
		OnSuite.notices.clear("notes-move-error");
	}
```

- [ ] **Step 5: Update PATTERNS.md's canonical pointer**

In `PATTERNS.md`, change:

```markdown
- **Server-rejection surfaced as a dismissable notice** — reach for this
  when an htmx request can be rejected (4xx/5xx) and swap:false would
  otherwise leave the user with no feedback at all: listen for
  `htmx:responseError`, insert a `.notice.notice-error` element, clear it on
  the next successful swap of the same target. Canonical:
  `internal/apps/notes/static/notes.js`'s `initPasteErrors` (copied by
  `initMoveErrors` in the same file).
```

to:

```markdown
- **Server-rejection surfaced as a dismissable notice** — reach for this
  when an htmx request can be rejected (4xx/5xx), or fails at the network
  level (offline), and swap:false would otherwise leave the user with no
  feedback at all: listen for `htmx:responseError` and `htmx:sendError`,
  call `OnSuite.notices.show`/`.clear` to insert/remove a
  `.notice.notice-error` element, clearing it on the next successful swap
  of the same target. Canonical: `internal/ui/static/htmx-notices.js`,
  used by `internal/apps/notes/static/notes.js`'s `initPasteErrors` and
  `initMoveErrors`.
```

- [ ] **Step 6: Manual verification — behavior unchanged**

Run: `go build ./cmd/onsuite && ./onsuite serve --data-dir ./data`, open Notes in a browser.

Expected (no behavior change from before this task):
- Pasting text that isn't valid outline text (or is oversized) into the outline still shows the same "Couldn't paste that..." notice.
- Dragging a note onto one of its own descendants (a cycle) still shows the same "Couldn't move that there..." notice.
- Both notices still clear on the next successful outline swap.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/static/htmx-notices.js internal/ui/templates/base.html internal/apps/notes/static/notes.js PATTERNS.md
git commit -m "refactor(notes): extract shared htmx-notices.js from paste/move error duplication"
```

---

### Task 4: Offline-aware error handling for paste and drag-to-move

**Files:**
- Modify: `internal/apps/notes/static/notes.js:579-586` (`initPasteErrors`)
- Modify: `internal/apps/notes/static/notes.js:844-849` (`initMoveErrors`)

**Interfaces:**
- Consumes: `OnSuite.notices.show`/`.clear` (Task 3).

This closes the actual gap identified during brainstorming: paste and drag-to-move already revert-and-notify on a 4xx/5xx (`htmx:responseError`), but not on a network-level failure (`htmx:sendError` — the offline case). `setRowStatus`'s inline-edit tracking already handles both (`notes.js:975-985`); this task brings paste and move in line with it.

- [ ] **Step 1: Add htmx:sendError handling to initPasteErrors**

In `internal/apps/notes/static/notes.js`, change:

```js
	function initPasteErrors() {
		if (!document.getElementById("outline")) return;
		document.body.addEventListener("htmx:responseError", function (evt) {
			if (pasteRequestPath(evt).indexOf("/paste") === -1) return;
			showPasteError(evt.detail && evt.detail.xhr && evt.detail.xhr.status);
		});
		// Any later successful swap of #outline — a retried paste, or any
		// other structural action — clears a stale error rather than
		// leaving it to sit there forever. A failed request never reaches
		// htmx:afterSwap at all (responseHandling's swap:false means
		// nothing swaps), so this never races with showPasteError above.
		document.body.addEventListener("htmx:afterSwap", function (evt) {
			if (evt && evt.detail && evt.detail.target && evt.detail.target.id === "outline") {
				clearPasteError();
			}
		});
	}
```

to:

```js
	function initPasteErrors() {
		if (!document.getElementById("outline")) return;
		document.body.addEventListener("htmx:responseError", function (evt) {
			if (pasteRequestPath(evt).indexOf("/paste") === -1) return;
			showPasteError(evt.detail && evt.detail.xhr && evt.detail.xhr.status);
		});
		// A network-level failure (offline, server unreachable) never
		// reaches htmx:responseError at all — there is no xhr response to
		// have a status. showPasteError(undefined) is the offline-specific
		// message branch added below.
		document.body.addEventListener("htmx:sendError", function (evt) {
			if (pasteRequestPath(evt).indexOf("/paste") === -1) return;
			showPasteError(undefined);
		});
		// Any later successful swap of #outline — a retried paste, or any
		// other structural action — clears a stale error rather than
		// leaving it to sit there forever. A failed request never reaches
		// htmx:afterSwap at all (responseHandling's swap:false means
		// nothing swaps), so this never races with showPasteError above.
		document.body.addEventListener("htmx:afterSwap", function (evt) {
			if (evt && evt.detail && evt.detail.target && evt.detail.target.id === "outline") {
				clearPasteError();
			}
		});
	}
```

- [ ] **Step 2: Add the offline message branch to showPasteError**

In `internal/apps/notes/static/notes.js`, change:

```js
	function showPasteError(status) {
		var outline = document.getElementById("outline");
		OnSuite.notices.show(outline, "notes-paste-error", status >= 500
			? "Something went wrong pasting that. Try again."
			: "Couldn't paste that: it doesn't look like valid outline text, or it's too large.");
	}
```

to:

```js
	function showPasteError(status) {
		var outline = document.getElementById("outline");
		OnSuite.notices.show(outline, "notes-paste-error", status === undefined
			? "You're offline: that couldn't be saved. Try again once you're back online."
			: status >= 500
				? "Something went wrong pasting that. Try again."
				: "Couldn't paste that: it doesn't look like valid outline text, or it's too large.");
	}
```

- [ ] **Step 3: Add htmx:sendError handling to initMoveErrors**

In `internal/apps/notes/static/notes.js`, change:

```js
	function initMoveErrors() {
		if (!document.getElementById("outline")) return;
		document.body.addEventListener("htmx:responseError", function (evt) {
			if (requestPath(evt).indexOf("/move") === -1) return;
			showMoveError(evt.detail && evt.detail.xhr && evt.detail.xhr.status);
		});
		// See initPasteErrors' identical afterSwap handler: any later
		// successful #outline swap clears a stale error, and a failed
		// request never reaches htmx:afterSwap at all, so this never races
		// with showMoveError above.
		document.body.addEventListener("htmx:afterSwap", function (evt) {
			if (evt && evt.detail && evt.detail.target && evt.detail.target.id === "outline") {
				clearMoveError();
			}
		});
	}
```

to:

```js
	function initMoveErrors() {
		if (!document.getElementById("outline")) return;
		document.body.addEventListener("htmx:responseError", function (evt) {
			if (requestPath(evt).indexOf("/move") === -1) return;
			showMoveError(evt.detail && evt.detail.xhr && evt.detail.xhr.status);
		});
		// See initPasteErrors' identical htmx:sendError handler: a
		// network-level failure never reaches htmx:responseError, so the
		// offline case needs its own listener.
		document.body.addEventListener("htmx:sendError", function (evt) {
			if (requestPath(evt).indexOf("/move") === -1) return;
			showMoveError(undefined);
		});
		// See initPasteErrors' identical afterSwap handler: any later
		// successful #outline swap clears a stale error, and a failed
		// request never reaches htmx:afterSwap at all, so this never races
		// with showMoveError above.
		document.body.addEventListener("htmx:afterSwap", function (evt) {
			if (evt && evt.detail && evt.detail.target && evt.detail.target.id === "outline") {
				clearMoveError();
			}
		});
	}
```

- [ ] **Step 4: Add the offline message branch to showMoveError**

In `internal/apps/notes/static/notes.js`, change:

```js
	function showMoveError(status) {
		var outline = document.getElementById("outline");
		OnSuite.notices.show(outline, "notes-move-error", status >= 500
			? "Something went wrong moving that. Try again."
			: "Couldn't move that there: it would create a cycle or nest too deep.");
	}
```

to:

```js
	function showMoveError(status) {
		var outline = document.getElementById("outline");
		OnSuite.notices.show(outline, "notes-move-error", status === undefined
			? "You're offline: that couldn't be saved. Try again once you're back online."
			: status >= 500
				? "Something went wrong moving that. Try again."
				: "Couldn't move that there: it would create a cycle or nest too deep.");
	}
```

- [ ] **Step 5: Manual verification — offline paste and drag-to-move**

Run: `go build ./cmd/onsuite && ./onsuite serve --data-dir ./data`, open Notes in a browser, logged in.

Expected, using devtools Network throttling set to "Offline":
- Paste some outline-shaped text into the outline. The shell-bar dot is already red (Task 2); the paste itself shows "You're offline: that couldn't be saved. Try again once you're back online." and the pasted content does not appear in the outline (matches the "revert" behavior already in place for the 4xx/5xx case — nothing swaps).
- Drag a row to reorder it. The row snaps back to its original position (existing drag-to-move revert behavior, unchanged by this task) and the same offline notice appears.
- Turn throttling back off, repeat both actions: they succeed normally with no notice.

- [ ] **Step 6: Commit**

```bash
git add internal/apps/notes/static/notes.js
git commit -m "feat(notes): surface offline failures on paste and drag-to-move, not just 4xx/5xx"
```

---

## Self-Review

**Spec coverage:**
- Shell-bar indicator, styled like `.outline-dot` — Task 1. ✓
- Detection via browser events + `/healthz` poll (15s) + `htmx:sendError` — Task 2. ✓
- Shared inline-error helper (per the user's explicit choice to keep this despite PATTERNS.md's existing duplication convention) — Task 3. ✓
- Fail-and-revert behavior extended to the offline case for paste and drag-to-move — Task 4. ✓
- `connectivity:change` event — Task 2, produced but intentionally unconsumed (spec's non-goals rule out auto-retry, the only thing that would consume it). ✓
- No `/healthz` changes — confirmed, no task touches `cmd/onsuite/serve.go`. ✓
- Testing plan (manual devtools throttling, manual server-down simulation) — reflected in Tasks 2 and 4's verification steps. ✓

**Placeholder scan:** No TBD/TODO markers; every step carries complete code or a fully specified manual verification procedure.

**Type/name consistency:** `OnSuite.notices.show(anchor, id, message)` and `.clear(id)` (Task 3) are called with matching argument order and count in Task 4's edits. `[data-conn-indicator]` (Task 1's markup) is the exact selector `connectivity.js` (Task 2) queries. `connectivity:change`'s `detail.online` boolean is the only thing Task 2 produces that nothing in this plan consumes — expected, per the spec's own non-goals.
