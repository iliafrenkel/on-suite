// internal/apps/notes/static/notes.js
//
// Implements no behaviour of its own — spec §7. Every request it issues is
// one a plain form already issues and a handler test in handlers_test.go
// already covers; N2 built and tested all of them before this file existed.
// It has no tests of its own, by design (spec §17): what a handler test
// cannot see is caret placement after a swap, which is verified by hand
// instead — see the final task's QA checklist.

(function () {
	"use strict";

	// ---- focus sync -------------------------------------------------------
	//
	// spec §7: every structural POST must carry the text of whatever bullet
	// is actually focused, not just the bullet whose button was clicked.
	// Clicking one row's control while typing in another is the case N2's
	// forms could never produce — each row was its own isolated form — and
	// it is real now that every row's inputs autosave independently.
	//
	// lastFocus, not document.activeElement read at request time: by the
	// time a click's handler runs, the browser has already moved focus to
	// the button that was clicked (browsers focus a clicked control before
	// dispatching its click event), so activeElement would report the
	// button, never the field the user was actually editing a moment ago.
	var lastFocus = null; // {id, field}
	// pendingFocus is {id, field, offset}, {afterID, field, offset}, or
	// {first: true, field, offset} — see restoreFocus for what each resolves
	// to once the swapped DOM exists.
	var pendingFocus = null;

	function isOutlineField(el) {
		return !!(el && el.matches && el.matches(".outline-title, .outline-note"));
	}

	// isTypingTarget: used only by the "/" search-focus binding below. Unlike
	// isOutlineField (deliberately narrow — title/note only, so Tab/arrow-key/
	// Backspace outline-navigation stays scoped to those two fields), this
	// asks the broader question "is the user typing anywhere right now,"
	// so it also covers fields isOutlineField was never meant to reach, such
	// as N5's due-date input (.outline-due-input). Without this, "/" typed
	// into a date (e.g. "26/08/2026") would preventDefault and yank focus to
	// the search box mid-entry, discarding the rest of what was typed.
	function isTypingTarget(el) {
		if (!el) return false;
		var tag = el.tagName;
		return tag === "INPUT" || tag === "TEXTAREA" || !!el.isContentEditable;
	}

	function rowOf(el) {
		return el && el.closest && el.closest(".outline-row");
	}

	function trackFocus() {
		document.addEventListener("focusin", function (e) {
			var input = e.target;
			if (!isOutlineField(input)) return;
			var row = rowOf(input);
			if (!row || !row.hasAttribute("data-id")) return; // the empty-outline's bootstrap field
			lastFocus = { id: row.getAttribute("data-id"), field: input.name };
		});
	}

	// augmentRequest runs before every HTMX request this page issues. A
	// hand-built request (see splitAndCreate and maybeDeleteEmptyBullet in
	// the keyboard module) already knows exactly which focus_id/title/note
	// it wants and marks itself with _skipFocusOverride so this does not
	// clobber them.
	function augmentRequest(e) {
		var params = e.detail.parameters;
		if (params._skipFocusOverride) {
			delete params._skipFocusOverride;
			return;
		}

		// Issue #61: cleared unconditionally, not just where this function
		// used to leave it alone. A request that never swaps #outline (e.g. a
		// plain text autosave, answered with an OOB fragment targeting the
		// input itself) never reaches restoreFocus, so a value set here would
		// otherwise sit unconsumed until some later, unrelated #outline swap
		// wrongly adopts it. Every branch below that still needs a value sets
		// its own before returning.
		pendingFocus = null;

		if (!lastFocus) {
			// The empty outline's bootstrap field is deliberately untracked
			// (it has no data-id — see trackFocus), so a brand-new outline
			// reaches its first Enter with lastFocus still null and nothing
			// asking for the caret back. The swap that turns that field into
			// the first real bullet would then drop focus to <body>, and the
			// user would have to click to type bullet two.
			//
			// Identified positively rather than assumed: the page holds no
			// real row at all, which is true only of the empty outline, and
			// the request replaces #outline. Any other untracked request
			// leaves pendingFocus null and behaves exactly as before.
			if (!document.querySelector(".outline-row[data-id]") && e.detail.target && e.detail.target.id === "outline") {
				// Caret at the end of what was typed, not at 0: the new
				// bullet carries that same text, so this is where the user
				// left off.
				var bootstrap = document.querySelector("#outline input.outline-title");
				pendingFocus = { first: true, field: "title", offset: bootstrap ? bootstrap.value.length : 0 };
			}
			return;
		}

		// A row's own text autosave (hx-trigger="input changed delay:600ms,
		// blur changed" on .outline-title/.outline-note) must never be
		// rewritten with another row's text. setText ignores focus_id and
		// writes to the path id, so a stale debounce timer firing after
		// focus has already moved would save the newly-focused row's text
		// onto the row the request is actually about. Such a request needs
		// no override at all: it already carries its own field's current
		// value and targets its own id. Only a structural request from a
		// different row needs the focused row's live text substituted in.
		var eltRow = rowOf(e.detail.elt);
		if (isOutlineField(e.detail.elt) && (!eltRow || eltRow.getAttribute("data-id") !== lastFocus.id)) {
			return;
		}

		var row = document.querySelector('.outline-row[data-id="' + lastFocus.id + '"]');
		var input = row && row.querySelector('input[name="' + lastFocus.field + '"]');
		if (!row || !input) return;

		var title = row.querySelector('input[name="title"]');
		var note = row.querySelector('input[name="note"]');
		params.focus_id = lastFocus.id;
		if (title) params.title = title.value;
		if (note) params.note = note.value;

		pendingFocus = { id: lastFocus.id, field: lastFocus.field, offset: input.selectionStart || 0 };
	}

	// restoreFocus runs after every HTMX swap of #outline. hx-swap=innerHTML
	// destroys and recreates every row, so the browser drops focus to
	// <body> unless this puts it back. afterID (rather than id) is how
	// splitAndCreate asks for "the row after this one": the new row's id is
	// assigned by the server and unknown until the response arrives.
	function restoreFocus(e) {
		// Only a swap of #outline destroys rows, and only those need the
		// caret put back. N4 gave setText a real response body (the OOB
		// rendered spans, targeting the input itself — see outline.html's
		// text-update), where before it answered 204 and swapped nothing:
		// every autosave now fires this event too. Without this guard an
		// autosave landing just before a structural response would consume
		// the pendingFocus that response is about to need, and the caret
		// would be lost to <body> when #outline is finally replaced. Same
		// positive identification as augmentRequest's: the request that
		// replaces #outline, not "anything that is not a text save".
		if (!e || !e.detail || !e.detail.target || e.detail.target.id !== "outline") return;
		if (!pendingFocus) return;
		var input;

		if (pendingFocus.first) {
			// The bootstrap case: there is no anchor id to look up, because
			// before the swap there was no row at all. The first title input
			// in the fresh DOM is the bullet the user just created.
			input = titleInputs()[0];
		} else if (pendingFocus.afterID !== undefined) {
			var anchor = document.querySelector('.outline-row[data-id="' + pendingFocus.afterID + '"]');
			var li = anchor && anchor.closest(".outline-item");
			var nextLi = li && li.nextElementSibling;
			var nextRow = nextLi && nextLi.querySelector(".outline-row");
			input = nextRow && nextRow.querySelector('input[name="' + pendingFocus.field + '"]');
		} else {
			var row = document.querySelector('.outline-row[data-id="' + pendingFocus.id + '"]');
			input = row && row.querySelector('input[name="' + pendingFocus.field + '"]');
		}

		if (input) {
			input.focus();
			var pos = Math.min(pendingFocus.offset, input.value.length);
			input.setSelectionRange(pos, pos);
		}
		pendingFocus = null;
	}

	// N4 (Markdown) adds no listener here: setText's response carries its
	// own hx-swap-oob elements, and htmx applies those on its own the moment
	// a response contains one, regardless of the triggering element's own
	// hx-swap. What it did change is that those responses now swap at all,
	// so the htmx:afterSwap below fires for autosaves too — restoreFocus
	// guards on the swap target for exactly that reason.
	function initFocusSync() {
		if (!document.getElementById("outline")) return;
		trackFocus();
		document.body.addEventListener("htmx:configRequest", augmentRequest);
		document.body.addEventListener("htmx:afterSwap", restoreFocus);
	}

	// ---- keyboard: reuse an existing button --------------------------------

	function click(btn) {
		if (btn && !btn.disabled) btn.click();
	}

	// indentButton, outdentButton, and moveButton's selectors each match TWO
	// elements per row: one in the "···" menu (.outline-menu-list) and one in
	// the hover overlay (.outline-overlay). querySelector returns the first
	// DOM-order match, which today is always the menu's copy, since the menu
	// renders before the overlay in outline.html. That's fine — both copies
	// are wired identically, so clicking either has the same effect — but if
	// the row markup is ever reordered so the overlay renders first, these
	// would silently start clicking the overlay's copy instead.
	function indentButton(row) { return row.querySelector('button[formaction$="/indent"]'); }
	function outdentButton(row) { return row.querySelector('button[formaction$="/outdent"]'); }
	function moveButton(row, dir) { return row.querySelector('button[name="dir"][value="' + dir + '"]'); }
	function collapseButton(row) { return row.querySelector("button.outline-chevron"); }
	function doneButton(row) { return row.querySelector("button.outline-done"); }

	// handleEscape: first press leaves editing (blur); a second press,
	// with nothing left focused in the outline, zooms out one level via the
	// breadcrumb's last link — its own immediate parent. No extra state is
	// needed to tell "first press" from "second": document.activeElement
	// already tells the two apart, since the first press moves it out of
	// the outline.
	function handleEscape() {
		var active = document.activeElement;
		if (isOutlineField(active)) {
			// Issue #60: same data-id exclusion every other keyboard binding
			// gives the empty-outline's bootstrap field (see handleKeydown
			// below) — without it, Escape there blurred the field, harmless
			// but inconsistent with every other binding treating it as
			// untracked.
			var row = rowOf(active);
			if (row && row.hasAttribute("data-id")) {
				active.blur();
			}
			return;
		}
		// Issue #60: scoped to #outline (or no focused element at all, i.e.
		// <body>), not global — harmless today since the outline page has
		// nothing else Escape-worthy, but without this a future control
		// using Escape for its own purpose elsewhere on the page would also
		// zoom the outline out from under it.
		if (active !== document.body && !(active && active.closest && active.closest("#outline"))) {
			return;
		}
		var crumbs = document.querySelectorAll(".outline-crumbs a");
		if (crumbs.length === 0) return;
		// crumbs[...].click(), not location.href = crumbs[...].href — reuses
		// an existing control the way the rest of this module does,
		// behaviorally identical here since the breadcrumb anchor carries
		// no hx-*.
		crumbs[crumbs.length - 1].click();
	}

	function handleKeydown(e) {
		// Issue #62: during an active IME composition (e.g. CJK input),
		// Enter commits the composition rather than ending the line, so it
		// must not reach splitAndCreate/maybeDeleteEmptyBullet/the arrow-key
		// bindings below. keyCode 229 is the legacy fallback for browsers
		// (older Safari) that don't set isComposing.
		if (e.isComposing || e.keyCode === 229) return;
		if (e.key === "Escape") {
			handleEscape();
			return;
		}
		// isTypingTarget, not isOutlineField: this guard must catch every
		// field the user might be typing into (including the due-date
		// input, which isOutlineField deliberately does not match — see
		// isTypingTarget above), not just the outline's title/note fields.
		// The old explicit "!== notes-search-input" id check is dropped as
		// redundant, not forgotten: the search box is itself an INPUT, so
		// isTypingTarget(e.target) is already true while it's focused, which
		// makes the "/" guard false without needing the id check too.
		if (e.key === "/" && !isTypingTarget(e.target)) {
			var search = document.getElementById("notes-search-input");
			if (search) {
				e.preventDefault();
				search.focus();
			}
			return;
		}

		var el = e.target;
		if (!isOutlineField(el)) return;
		var row = rowOf(el);
		if (!row || !row.hasAttribute("data-id")) return; // the empty-outline's bootstrap field

		// Issue #58: Tab/Shift+Tab always preventDefault, even when the
		// indent/outdent button they click is disabled (first sibling, or
		// depth 0) — this is deliberate, standard outliner behavior, not an
		// oversight, so a keyboard-only user cannot Tab out of a title/note
		// field to reach anything past it (the delete button, the bullet
		// dot, shell navigation). That is not a WCAG 2.1.2 keyboard trap:
		// Escape (handleEscape, first stage) always blurs the field and
		// remains the documented way out.
		if (e.key === "Tab" && !e.shiftKey) {
			e.preventDefault();
			click(indentButton(row));
			return;
		}
		if (e.key === "Tab" && e.shiftKey) {
			e.preventDefault();
			click(outdentButton(row));
			return;
		}
		if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key === "ArrowUp") {
			e.preventDefault();
			click(moveButton(row, "up"));
			return;
		}
		if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key === "ArrowDown") {
			e.preventDefault();
			click(moveButton(row, "down"));
			return;
		}
		if (e.key === "ArrowUp" && !e.metaKey && !e.ctrlKey && !e.shiftKey) {
			var up = verticalNeighborTitle(row, -1);
			if (up) {
				e.preventDefault();
				var upPos = Math.min(el.selectionStart || 0, up.value.length);
				up.focus();
				up.setSelectionRange(upPos, upPos);
			}
			return;
		}
		if (e.key === "ArrowDown" && !e.metaKey && !e.ctrlKey && !e.shiftKey) {
			var down = verticalNeighborTitle(row, 1);
			if (down) {
				e.preventDefault();
				var downPos = Math.min(el.selectionStart || 0, down.value.length);
				down.focus();
				down.setSelectionRange(downPos, downPos);
			}
			return;
		}
		if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
			e.preventDefault();
			click(doneButton(row));
			return;
		}
		if ((e.metaKey || e.ctrlKey) && e.key === ".") {
			e.preventDefault();
			click(collapseButton(row));
			return;
		}
		if (e.key === "Enter" && e.shiftKey) {
			e.preventDefault();
			focusNote(row);
			return;
		}
		if (e.key === "Enter" && !e.shiftKey) {
			e.preventDefault();
			splitAndCreate(el, row);
			return;
		}
		if (e.key === "Backspace") {
			if (maybeDeleteEmptyBullet(el, row)) {
				e.preventDefault();
			}
			return;
		}
	}

	// ---- keyboard: new requests --------------------------------------------

	function titleInputs() {
		return Array.prototype.slice.call(document.querySelectorAll("#outline input.outline-title"));
	}

	// verticalNeighborTitle: the title input immediately above (dir -1) or
	// below (dir +1) row's own title, in titleInputs()'s DOM-order list —
	// already visual/tree order, so this is "the bullet above/below on
	// screen" regardless of nesting depth. Plain Up/Down always targets a
	// title, even when the keypress happened in a note field (row's own
	// title is the anchor, not el).
	function verticalNeighborTitle(row, dir) {
		var inputs = titleInputs();
		var current = row.querySelector("input.outline-title");
		var idx = inputs.indexOf(current);
		if (idx === -1) return null;
		return inputs[idx + dir] || null;
	}

	function appendSiblingBelow(row) {
		click(row.querySelector('button[formaction="/notes/new"]'));
	}

	// focusNote moves the caret to the note line under the current bullet,
	// at the end of whatever text is already there — spec §8's Shift+Enter.
	function focusNote(row) {
		var note = row.querySelector("input.outline-note");
		if (!note) return;
		note.focus();
		var pos = note.value.length;
		note.setSelectionRange(pos, pos);
	}

	// splitAndCreate is spec §8's Enter: a new sibling below, splitting the
	// title at the caret. In the note field there is nothing to split — a
	// note is not the tree structure — so Enter there behaves like the "+"
	// button instead.
	function splitAndCreate(el, row) {
		if (!el.classList.contains("outline-title")) {
			appendSiblingBelow(row);
			return;
		}

		var pos = el.selectionStart;
		var head = el.value.slice(0, pos);
		var tail = el.value.slice(pos);
		var note = row.querySelector('input[name="note"]');
		var rootField = row.querySelector('input[name="root"]');
		var id = row.getAttribute("data-id");

		// The new row's id is assigned by the server and unknown until the
		// response arrives, so the caret target is "the row right after
		// this one" rather than an id — see restoreFocus's afterID branch.
		pendingFocus = { afterID: id, field: "title", offset: 0 };

		htmx.ajax("POST", "/notes/new", {
			source: document.body,
			target: "#outline",
			swap: "innerHTML",
			values: {
				root: rootField.value,
				focus_id: id,
				title: head,
				note: note ? note.value : "",
				new_title: tail,
				_skipFocusOverride: "1"
			}
		});
	}

	// maybeDeleteEmptyBullet is spec §8's Backspace: only when the bullet is
	// genuinely empty (no title, no note, no children) and the caret sits
	// at its very start — a leaf with nothing in it loses nothing by going
	// away without the confirmation the visible delete button asks for.
	function maybeDeleteEmptyBullet(el, row) {
		if (!el.classList.contains("outline-title")) return false;
		if (el.selectionStart !== 0 || el.selectionEnd !== 0) return false;
		if (el.value !== "") return false;

		var note = row.querySelector('input[name="note"]');
		if (note && note.value !== "") return false;
		// The chevron button, not a rendered .outline-list, is what says "this
		// bullet has children": Store.Outline stops descending into a
		// collapsed node, so a collapsed parent renders no child list at all
		// while still rendering its chevron ({{if .HasChildren}} in
		// outline.html, pinned by TestCollapsedBulletHidesItsChildren). Asking
		// the DOM for a subtree would call a collapsed parent a leaf and
		// delete its hidden children with it.
		if (collapseButton(row)) return false; // has children (collapsed or not)

		var inputs = titleInputs();
		var idx = inputs.indexOf(el);
		if (idx <= 0) return false; // nothing before it to land on

		var prev = inputs[idx - 1];
		var rootField = row.querySelector('input[name="root"]');
		var id = row.getAttribute("data-id");

		pendingFocus = { id: rowOf(prev).getAttribute("data-id"), field: "title", offset: prev.value.length };

		htmx.ajax("POST", "/notes/" + id + "/delete", {
			source: document.body,
			target: "#outline",
			swap: "innerHTML",
			values: { root: rootField.value, _skipFocusOverride: "1" }
		});
		return true;
	}

	function initKeyboard() {
		if (!document.getElementById("outline")) return;
		document.addEventListener("keydown", handleKeydown);
	}

	// ---- paste: outline-shaped blocks only ---------------------------------
	//
	// looksLikeOutline is a cheap client-side mirror of ParseMarkdown's own
	// bullet-line rule (import.go): does at least one line start with an
	// even number of spaces followed by "- ". It only decides WHETHER to
	// intercept — the server runs the real parse once the request lands, so
	// a mismatch between this regex and the Go one (this says yes,
	// ParseMarkdown says no — a bad due date, oversized text, or some other
	// shape this heuristic missed) surfaces to the user as a visible error
	// (see initPasteErrors below), not as a silently discarded paste; it can
	// never make the browser's own default paste behaviour do the wrong
	// thing, since that path is untouched unless this test passes.
	function looksLikeOutline(text) {
		return /^(?: {2})*- /m.test(text);
	}

	// handlePaste is spec §14's "paste-a-multi-line-block-into-a-bullet":
	// only intercepted when the clipboard content already looks like this
	// app's own export format (see looksLikeOutline above) — anything else
	// is left entirely to the browser's default paste, which is why every
	// early return here happens before preventDefault.
	function handlePaste(e) {
		var el = e.target;
		if (!isOutlineField(el)) return;

		var clipboard = e.clipboardData || window.clipboardData;
		var text = clipboard ? clipboard.getData("text/plain") : "";
		if (!looksLikeOutline(text)) return;

		var row = rowOf(el);
		// The empty outline's bootstrap field has no data-id (see
		// trackFocus) and so nothing to paste children under yet — left to
		// the browser's default paste, the same as any other unmatched
		// case, rather than swallowing the paste with nothing to show for
		// it.
		if (!row || !row.hasAttribute("data-id")) return;

		e.preventDefault();
		var id = row.getAttribute("data-id");
		var rootField = row.querySelector('input[name="root"]');
		var title = row.querySelector('input[name="title"]');
		var note = row.querySelector('input[name="note"]');

		htmx.ajax("POST", "/notes/" + id + "/paste", {
			source: document.body,
			target: "#outline",
			swap: "innerHTML",
			values: {
				root: rootField.value,
				focus_id: id,
				title: title ? title.value : "",
				note: note ? note.value : "",
				text: text,
				_skipFocusOverride: "1"
			}
		});
	}

	function initPaste() {
		if (!document.getElementById("outline")) return;
		document.addEventListener("paste", handlePaste);
	}

	// ---- paste: surface a server-side rejection ----------------------------
	//
	// htmx 2.0.10's default responseHandling treats any 4xx/5xx as
	// swap:false ([{code:"[45]..",swap:false,error:true}] in
	// internal/ui/static/htmx.min.js's own config) — so when POST
	// /notes/{id}/paste answers 400 (not outline-shaped after all, an
	// oversized paste, or a bad due date), nothing is rendered and, without
	// a listener of its own, the user would see nothing happen at all: the
	// pasted content just vanishes with no explanation. This is the one
	// case handlePaste's own preventDefault cannot recover from — the
	// browser's default paste already didn't happen, and there is no way to
	// re-trigger it after the fact — so a clear, visible error is what
	// stands in for the lost paste instead.
	function pasteRequestPath(evt) {
		var detail = evt.detail || {};
		if (detail.pathInfo && detail.pathInfo.requestPath) return detail.pathInfo.requestPath;
		if (detail.requestConfig && detail.requestConfig.path) return detail.requestConfig.path;
		return "";
	}

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

	// ---- drag-to-move -------------------------------------------------------
	//
	// Mouse only — spec §20 puts touch drag-and-drop out of scope, and this
	// section adds no touch event listeners at all, so a touch device simply
	// keeps whatever it already had (tap the dot to zoom in; Tab/Shift+Tab,
	// the row menu's Move up/down, and Indent/Outdent for restructuring).
	//
	// This uses plain mousedown/mousemove/mouseup rather than the native
	// HTML5 Drag and Drop API. The native API's own event sequence
	// (dragstart/dragover/drop, with a DataTransfer object and a
	// preventDefault-on-dragover requirement to even accept a drop) buys
	// nothing here — there is no cross-window or cross-app drag to support —
	// and costs the fine, continuous control this needs to compute a
	// before/after/child zone from the cursor's exact position inside
	// whatever row is currently under it, every few pixels of movement.
	//
	// DRAG_THRESHOLD_PX is what tells a drag apart from the plain click that
	// already zooms in when you click the dot (outline.html's ".outline-dot"
	// anchor): below this distance, mouseup on the same element still fires
	// its native click as normal, since nothing here ever calls
	// preventDefault on the initiating mousedown itself.
	var DRAG_THRESHOLD_PX = 4;
	// MAX_POSITION mirrors tree.go's own maxPosition (1 << 30) — "append as
	// the last child", the same sentinel Ops.Indent already passes to
	// Ops.Move for exactly this meaning.
	var MAX_POSITION = 1 << 30;

	// dragState is null between drags. While one is in progress it is
	// { id, startX, startY, dragging, dropRow, dropMode } — dragging only
	// becomes true once the pointer has moved past DRAG_THRESHOLD_PX, and
	// dropRow/dropMode (set by updateDropTarget) are null until the pointer
	// is over a valid destination.
	var dragState = null;

	function clearDropIndicator() {
		var marked = document.querySelectorAll(".outline-drop-before, .outline-drop-after, .outline-drop-child");
		for (var i = 0; i < marked.length; i++) {
			marked[i].classList.remove("outline-drop-before", "outline-drop-after", "outline-drop-child");
		}
	}

	// updateDropTarget recomputes dragState.dropRow/dropMode from the
	// pointer's current position, and marks that row (if any) with the
	// matching CSS class from Step 2. The top and bottom quarters of a row
	// mean "insert as the previous/next sibling"; the middle half means
	// "nest as the last child" — matching Workflowy's own three-zone
	// behaviour, the reference spec §1 names for this whole app.
	function updateDropTarget(x, y) {
		clearDropIndicator();
		dragState.dropRow = null;
		dragState.dropMode = null;

		var el = document.elementFromPoint(x, y);
		var row = rowOf(el);
		if (!row || !row.hasAttribute("data-id")) return;
		var targetID = row.getAttribute("data-id");
		if (targetID === dragState.id) return; // a row cannot become its own sibling/parent

		var draggedRow = document.querySelector('.outline-row[data-id="' + dragState.id + '"]');
		// A visible descendant of the dragged row: dropping onto one of
		// these would be an obviously-invalid cycle. The server's own
		// Ops.Move rejects the cycle regardless (ErrCycle) — this is a
		// client-side courtesy so the drop indicator never invites a move
		// that is only going to 400, not the source of truth for what is
		// actually a cycle. A descendant hidden by collapse or by
		// show-completed is not visible in the DOM at all, so this check
		// cannot see it either; the server-side check is what actually
		// guards those.
		//
		// A row's children live in a <ul> that is a *sibling* of its own
		// .outline-row div inside the shared <li class="outline-item">
		// (see outline.html's outline-rows template), not nested inside
		// the row div itself — so draggedRow.contains(row) would never be
		// true for a genuine descendant. Walk up to the dragged row's
		// enclosing .outline-item instead, since that's what actually
		// contains the descendant subtree.
		var draggedItem = draggedRow && draggedRow.closest(".outline-item");
		if (draggedItem && draggedItem.contains(row)) return;

		var rect = row.getBoundingClientRect();
		var relativeY = (y - rect.top) / rect.height;
		var mode = relativeY < 0.25 ? "before" : relativeY > 0.75 ? "after" : "child";

		row.classList.add("outline-drop-" + mode);
		dragState.dropRow = row;
		dragState.dropMode = mode;
	}

	// suppressNextClick is armed the moment a drag crosses the threshold,
	// and fires at most once: if the pointer happens to come back to rest
	// over the same dot it started on (a drag that ends up going nowhere),
	// the browser still fires a native click there, which would otherwise
	// zoom into that bullet immediately after the user tried to drag it.
	function suppressNextClick(e) {
		e.preventDefault();
		e.stopPropagation();
		document.removeEventListener("click", suppressNextClick, true);
	}

	function handleDragMouseMove(e) {
		if (!dragState) return;
		if (!dragState.dragging) {
			var dx = e.clientX - dragState.startX;
			var dy = e.clientY - dragState.startY;
			if (Math.sqrt(dx * dx + dy * dy) < DRAG_THRESHOLD_PX) return;
			dragState.dragging = true;
			var draggedRow = document.querySelector('.outline-row[data-id="' + dragState.id + '"]');
			if (draggedRow) draggedRow.classList.add("outline-row-dragging");
			document.body.classList.add("outline-dragging-active");
			document.addEventListener("click", suppressNextClick, true);
		}
		e.preventDefault();
		updateDropTarget(e.clientX, e.clientY);
	}

	// issueMove sends the drop as a POST /notes/{id}/move, dir=to — Task 2.
	// _skipFocusOverride: nothing was being typed when this fired (the
	// gesture starts on the dot, never a text field), so there is no
	// focused row's text to substitute in — see augmentRequest's own
	// handling of this flag.
	function issueMove(id, targetRow, mode) {
		var parent, position;
		if (mode === "child") {
			parent = targetRow.getAttribute("data-id");
			position = MAX_POSITION;
		} else {
			parent = targetRow.getAttribute("data-parent-id");
			position = parseInt(targetRow.getAttribute("data-position"), 10);
			if (mode === "after") position += 1;
		}

		var row = document.querySelector('.outline-row[data-id="' + id + '"]');
		var rootField = row && row.querySelector('input[name="root"]');

		htmx.ajax("POST", "/notes/" + id + "/move", {
			source: document.body,
			target: "#outline",
			swap: "innerHTML",
			values: {
				root: rootField ? rootField.value : "0",
				dir: "to",
				parent: parent,
				position: position,
				focus_id: "0",
				_skipFocusOverride: "1"
			}
		});
	}

	function handleDragMouseUp() {
		document.removeEventListener("mousemove", handleDragMouseMove);
		document.removeEventListener("mouseup", handleDragMouseUp);
		if (!dragState) return;

		var state = dragState;
		dragState = null;
		clearDropIndicator();
		document.body.classList.remove("outline-dragging-active");
		var draggedRow = document.querySelector('.outline-row[data-id="' + state.id + '"]');
		if (draggedRow) draggedRow.classList.remove("outline-row-dragging");

		if (!state.dragging || !state.dropRow) return;
		issueMove(state.id, state.dropRow, state.dropMode);
	}

	function handleDotMouseDown(e) {
		if (e.button !== 0) return; // left button only
		var dot = e.target.closest && e.target.closest(".outline-dot");
		if (!dot) return;
		var row = rowOf(dot);
		if (!row || !row.hasAttribute("data-id")) return; // the empty-outline's bootstrap row has no dot with an id to drag

		dragState = { id: row.getAttribute("data-id"), startX: e.clientX, startY: e.clientY, dragging: false, dropRow: null, dropMode: null };
		document.addEventListener("mousemove", handleDragMouseMove);
		document.addEventListener("mouseup", handleDragMouseUp);
	}

	// Delegated on document, not bound per-dot: hx-swap="innerHTML" replaces
	// every row's markup (including every dot) on each structural response,
	// so a listener attached to a specific dot element would stop working
	// after the very first move. Every other keyboard/paste binding in this
	// file already follows the same delegated pattern for the same reason.
	function initDragToMove() {
		if (!document.getElementById("outline")) return;
		document.addEventListener("mousedown", handleDotMouseDown);
	}

	initFocusSync();
	initKeyboard();
	initPaste();
	initPasteErrors();
	initDragToMove();
})();
