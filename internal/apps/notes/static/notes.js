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
	var pendingFocus = null; // {id, field, offset} or {afterID, field, offset}

	function isOutlineField(el) {
		return !!(el && el.matches && el.matches(".outline-title, .outline-note"));
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
		if (!lastFocus) return;

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
	function restoreFocus() {
		if (!pendingFocus) return;
		var input;

		if (pendingFocus.afterID !== undefined) {
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

	function initFocusSync() {
		if (!document.getElementById("outline")) return;
		trackFocus();
		document.body.addEventListener("htmx:configRequest", augmentRequest);
		document.body.addEventListener("htmx:afterSwap", restoreFocus);
	}

	initFocusSync();
})();
