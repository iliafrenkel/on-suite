// ON Books' script: the delete confirmation, the sidebar's resize gutter,
// focus kept in the sort, view and format controls across their swaps, and
// keyboard shortcuts (UI redesign spec,
// docs/superpowers/specs/2026-10-12-on-books-ui-redesign-design.md).
// Vanilla and CSP-clean; the sections mirror reader.js's (apps never share
// app scripts).
(function () {
	"use strict";

	// --- Confirm dialog, replacing window.confirm for hx-confirm ----------
	//
	// Mirrors reader.js's, including the #551 fix: the OK listener belongs
	// to one opening through an AbortController and goes as soon as OK or
	// Cancel is pressed — not on "close", which Chrome can hold back in a
	// hidden tab, so a declined delete would otherwise fire with the next.
	var confirmController = null;
	document.addEventListener("htmx:confirm", function (e) {
		var page = document.getElementById("books-page");
		if (!page || !e.target || !page.contains(e.target)) return;
		var msg = e.target.getAttribute("hx-confirm");
		if (!msg) return;
		var dialog = document.getElementById("books-confirm-dialog");
		if (!dialog || typeof dialog.showModal !== "function") return; // htmx falls back to window.confirm

		e.preventDefault();
		document.getElementById("books-confirm-message").textContent = msg;
		var ok = document.getElementById("books-confirm-ok");
		var cancel = dialog.querySelector(".books-dialog-cancel");

		if (confirmController) confirmController.abort();
		var controller = confirmController = new AbortController();
		dialog.addEventListener("close", function () {
			if (dialog.open) return;
			controller.abort();
		}, { signal: controller.signal });
		cancel.addEventListener("click", function () {
			controller.abort();
			dialog.close();
		}, { signal: controller.signal });
		ok.addEventListener("click", function () {
			controller.abort();
			dialog.close();
			// true: skip the confirm gate, or htmx would ask again natively.
			e.detail.issueRequest(true);
		}, { signal: controller.signal });
		dialog.showModal();
	});

	// --- Resizable sidebar -------------------------------------------------
	//
	// reader.js's gutter, for the one pane Books has left (spec "Sidebar").
	// Above the 640px drawer breakpoint only; below it the stored width is
	// cleared rather than left to misapply. The width lives on <html>, not
	// on the home, so a swap never snaps it back (#453). The sidebar is
	// swapped out of band on every list change, so it is looked up each
	// time rather than held.
	var SIDE_STORE_KEY = "books.sideWidth";
	var SIDE_MIN = 10; // rem
	var SIDE_MAX = 22; // rem
	var DESKTOP_QUERY = window.matchMedia("(min-width: 641px)");

	try {
		window.localStorage.removeItem("books.paneWidths"); // the three panes' widths, before UI1
	} catch (e) {
		// Storage blocked: nothing to tidy.
	}

	function remToPx(rem) {
		var rootPx = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
		return rem * rootPx;
	}

	function setSideWidth(px) {
		var clamped = Math.min(remToPx(SIDE_MAX), Math.max(remToPx(SIDE_MIN), px));
		document.documentElement.style.setProperty("--books-side-w", clamped + "px");
	}

	function sideWidth() {
		var side = document.getElementById("books-side");
		return side ? side.getBoundingClientRect().width : 0;
	}

	function saveSideWidth() {
		try {
			window.localStorage.setItem(SIDE_STORE_KEY, String(Math.round(sideWidth())));
		} catch (e) {
			// Private browsing or a full quota: the drag worked, it just won't be remembered.
		}
	}

	function syncSideWidth() {
		if (!DESKTOP_QUERY.matches) {
			document.documentElement.style.removeProperty("--books-side-w");
			return;
		}
		var stored = 0;
		try {
			stored = Number(window.localStorage.getItem(SIDE_STORE_KEY)) || 0;
		} catch (e) {
			stored = 0;
		}
		if (stored > 0) setSideWidth(stored);
	}

	function initGutter() {
		var home = document.getElementById("books-home");
		var gutter = home && home.querySelector('.pane-gutter[data-gutter-for="side"]');
		if (!gutter) return;
		var dragging = null; // { startX, startWidth }

		gutter.addEventListener("pointerdown", function (e) {
			if (!DESKTOP_QUERY.matches) return;
			dragging = { startX: e.clientX, startWidth: sideWidth() };
			gutter.classList.add("is-dragging");
			gutter.setPointerCapture(e.pointerId);
			e.preventDefault();
		});
		gutter.addEventListener("pointermove", function (e) {
			if (!dragging) return;
			setSideWidth(dragging.startWidth + (e.clientX - dragging.startX));
		});
		function endDrag() {
			if (!dragging) return;
			dragging = null;
			gutter.classList.remove("is-dragging");
			saveSideWidth();
		}
		gutter.addEventListener("pointerup", endDrag);
		gutter.addEventListener("pointercancel", endDrag);
		// The WAI-ARIA separator pattern: arrows nudge the pane.
		gutter.addEventListener("keydown", function (e) {
			if (!DESKTOP_QUERY.matches) return;
			if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
			setSideWidth(sideWidth() + (e.key === "ArrowRight" ? 16 : -16));
			saveSideWidth();
			e.preventDefault();
		});
	}

	document.addEventListener("DOMContentLoaded", function () {
		syncSideWidth();
		DESKTOP_QUERY.addEventListener("change", syncSideWidth);
		initGutter();
	});
	// Back/forward restores <body> from htmx's history cache: a new gutter,
	// no DOMContentLoaded (#456).
	document.addEventListener("htmx:historyRestore", initGutter);

	// --- Focus in the sort, view and format controls -------------------------
	//
	// Choosing a sort or a view swaps the whole controls form out of band
	// (it carries the pressed state and the list's context), and choosing a
	// format swaps the book page's body (the shelf pill and the progress
	// unit can follow). Either would drop focus to <body>; put it back on
	// the control that had it.
	var keptFocus = null;
	document.addEventListener("htmx:beforeSwap", function () {
		var el = document.activeElement;
		keptFocus = el && el.id && el.closest && el.closest("#books-prefs, .books-format") ? el.id : null;
	});
	document.addEventListener("htmx:afterSettle", function () {
		if (!keptFocus) return;
		var el = document.getElementById(keptFocus);
		keptFocus = null;
		if (el && !el.disabled) el.focus();
	});

	// The progress form swaps its whole box (input included), so focus would
	// drop to <body> after Enter. Put it back on the new input, selected, so
	// the next number replaces the old one — on success and on the inline
	// error alike. Only for swaps the progress form itself made.
	document.addEventListener("htmx:afterSwap", function (e) {
		var src = (e.detail && (e.detail.requestConfig && e.detail.requestConfig.elt || e.detail.elt)) || e.target;
		if (!src || !src.classList || !src.classList.contains("books-progress-form")) return;
		var input = document.getElementById("books-progress-input");
		if (!input) return;
		input.focus();
		input.select();
	});

	// --- Keyboard shortcuts --------------------------------------------------
	//
	// Home: j/k move through the books, Enter (natively) or o opens one,
	// / searches, a adds a book, Esc closes the drawer. Book page: p goes to
	// the progress box, n and q open the add-note and add-quote forms, e
	// edits, Esc closes an open popover or edit form — or, with none open,
	// goes back to the list (spec "Keyboard").

	// openEntry opens an add disclosure ("+ Add note", "+ Add quote") and
	// puts the cursor in its box; false when the page has none.
	function openEntry(detailsId, inputId) {
		var details = document.getElementById(detailsId);
		var input = document.getElementById(inputId);
		if (!details || !input) return false;
		details.open = true;
		input.focus();
		return true;
	}

	// Keys are ignored while typing, so "/" in the search box is a slash.
	function isTyping(el) {
		if (!el) return false;
		var tag = el.tagName;
		return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable;
	}

	function itemLinks() {
		return Array.prototype.slice.call(document.querySelectorAll(".books-item-link"));
	}

	function step(delta) {
		var links = itemLinks();
		if (!links.length) return;
		var i = links.indexOf(document.activeElement);
		var next = i === -1 ? 0 : Math.min(links.length - 1, Math.max(0, i + delta));
		links[next].focus();
		links[next].scrollIntoView({ block: "nearest" });
	}

	// closeDisclosures closes every open popover and edit form on the book
	// page; false when none was open. The Reading history is a section, not
	// a popup, so Esc leaves it as it is.
	function closeDisclosures() {
		var open = document.querySelectorAll("#books-book details[open]:not(.books-history)");
		open.forEach(function (d) {
			d.open = false;
		});
		return open.length > 0;
	}

	// closeDrawer closes the phone drawer; false when it isn't open. Focus
	// that was in the drawer, or on its checkbox, goes back to the checkbox:
	// the drawer button labels it and is outlined while it has focus, and
	// the link that had focus has just been hidden (#592).
	function closeDrawer(from) {
		var drawer = document.getElementById("books-drawer");
		if (!drawer || !drawer.checked) return false;
		drawer.checked = false;
		var side = document.getElementById("books-side");
		if (from === drawer || (side && side.contains(from))) drawer.focus();
		return true;
	}

	function homeKey(e) {
		switch (e.key) {
		case "j":
			step(1);
			return true;
		case "k":
			step(-1);
			return true;
		case "o":
			if (itemLinks().indexOf(document.activeElement) === -1) return false;
			document.activeElement.click();
			return true;
		case "/":
			var q = document.getElementById("books-q");
			if (!q) return false;
			q.focus();
			q.select();
			return true;
		case "a":
			var add = document.getElementById("books-add");
			if (!add) return false;
			window.location.href = add.href;
			return true;
		}
		return false;
	}

	function bookKey(e) {
		switch (e.key) {
		case "p":
			var progress = document.getElementById("books-progress-input");
			if (!progress) return false;
			progress.focus();
			progress.select();
			return true;
		case "n":
			return openEntry("books-note-new", "books-note-new-body");
		case "q":
			return openEntry("books-quote-new", "books-quote-new-text");
		case "e":
			var edit = document.querySelector(".books-edit-link");
			if (!edit) return false;
			window.location.href = edit.href;
			return true;
		case "Escape":
			// Focus on a button inside the popover (Mark as read, a history
			// edit's Save) would be left on a hidden element once it closes,
			// so it goes back to the popover's own summary.
			var active = document.activeElement;
			var host = active && active.closest ? active.closest("#books-book details[open]:not(.books-history)") : null;
			if (closeDisclosures()) {
				var toggle = host && host.querySelector("summary");
				if (toggle) toggle.focus();
				return true;
			}
			var back = document.querySelector(".books-back");
			if (!back) return false;
			window.location.href = back.href;
			return true;
		}
		return false;
	}

	document.addEventListener("keydown", function (e) {
		if (e.defaultPrevented || e.repeat || e.altKey || e.ctrlKey || e.metaKey) return;
		// The modal confirm owns the keyboard (Escape still closes it natively).
		if (document.querySelector("dialog[open]")) return;
		var home = document.getElementById("books-home");
		var page = document.getElementById("books-page");
		if (!home && !page) return;
		// Esc closes the phone drawer from anywhere, its own checkbox and the
		// links inside it included, before anything else sees the key (#592).
		// It leaves the search box too, so one Esc does what it does with the
		// drawer shut (#600).
		if (e.key === "Escape" && closeDrawer(e.target)) {
			if (e.target.id === "books-q") e.target.blur();
			e.preventDefault();
			return;
		}
		if (isTyping(e.target)) {
			if (e.key !== "Escape") return;
			if (e.target.id === "books-q" || e.target.id === "books-progress-input") {
				e.target.blur();
				return;
			}
			// Esc from a field in an open popover or edit form of the book
			// page (Finish…, Did not finish…, the review, an entry's ✎) closes
			// it and returns focus to its summary.
			var open = e.target.closest("#books-book details[open]:not(.books-history)");
			if (open) {
				open.open = false;
				var summary = open.querySelector("summary");
				if (summary) summary.focus();
				return;
			}
			// Nothing is typed into a select, so Esc on one outside a popover
			// (the reading's format) is Esc as anywhere else on the page: back
			// to the list (#600). An open select's own list takes Esc before
			// the page sees it.
			if (e.target.tagName !== "SELECT") return;
		}
		if (home ? homeKey(e) : bookKey(e)) e.preventDefault();
	});
})();
