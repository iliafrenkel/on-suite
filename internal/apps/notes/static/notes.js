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
	// a mismatch between this regex and the Go one can only ever produce an
	// unnecessary round trip (this says yes, ParseMarkdown says no, and the
	// user sees an error) rather than silently mishandling a paste; it can
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

	initFocusSync();
	initKeyboard();
	initPaste();
})();
