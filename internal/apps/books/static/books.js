// ON Books' script: the delete confirmation, resizable panes, keyboard
// shortcuts and the open book's row highlight. Vanilla and
// CSP-clean; the sections mirror reader.js's (apps never share app
// scripts).
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
		var panes = document.getElementById("books-panes");
		if (!panes || !e.target || !panes.contains(e.target)) return;
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

	// --- Resizable panes ---------------------------------------------------
	//
	// reader.js's, with Books' panes. Desktop only (the 900px breakpoint
	// where the CSS collapses the layout); below it the stored widths are
	// cleared rather than left to misapply. The widths live on <html>, not
	// on the row, so a full panes swap doesn't snap them back (#453).
	var PANE_STORE_KEY = "books.paneWidths";
	var PANE_MIN = { side: 10, list: 16 }; // rem
	var PANE_MAX = { side: 22, list: 36 }; // rem
	var DESKTOP_QUERY = window.matchMedia("(min-width: 901px)");

	function remToPx(rem) {
		var rootPx = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
		return rem * rootPx;
	}

	function clampPx(px, key) {
		return Math.min(remToPx(PANE_MAX[key]), Math.max(remToPx(PANE_MIN[key]), px));
	}

	function paneFor(row, key) {
		return row.querySelector(key === "side" ? ".books-side" : ".books-listpane");
	}

	function setWidth(key, px) {
		document.documentElement.style.setProperty("--books-" + key + "-w", clampPx(px, key) + "px");
	}

	function loadPaneWidths() {
		try {
			var parsed = JSON.parse(window.localStorage.getItem(PANE_STORE_KEY) || "null");
			if (!parsed || typeof parsed.side !== "number" || typeof parsed.list !== "number") return null;
			return parsed;
		} catch (e) {
			return null;
		}
	}

	function savePaneWidths(row) {
		try {
			window.localStorage.setItem(PANE_STORE_KEY, JSON.stringify({
				side: Math.round(paneFor(row, "side").getBoundingClientRect().width),
				list: Math.round(paneFor(row, "list").getBoundingClientRect().width),
			}));
		} catch (e) {
			// Private browsing or a full quota: the drag worked, it just won't be remembered.
		}
	}

	function syncPaneWidths() {
		var root = document.documentElement;
		if (!DESKTOP_QUERY.matches) {
			root.style.removeProperty("--books-side-w");
			root.style.removeProperty("--books-list-w");
			return;
		}
		var stored = loadPaneWidths();
		if (!stored) return;
		setWidth("side", stored.side);
		setWidth("list", stored.list);
	}

	function initResizablePanes() {
		var row = document.getElementById("books-panes-row");
		if (!row) return;
		var dragging = null; // { key, startX, startWidth }

		row.querySelectorAll(".pane-gutter").forEach(function (gutter) {
			var key = gutter.getAttribute("data-gutter-for");
			gutter.addEventListener("pointerdown", function (e) {
				if (!DESKTOP_QUERY.matches) return;
				dragging = { key: key, startX: e.clientX, startWidth: paneFor(row, key).getBoundingClientRect().width };
				gutter.classList.add("is-dragging");
				gutter.setPointerCapture(e.pointerId);
				e.preventDefault();
			});
			// The WAI-ARIA separator pattern: arrows nudge the pane.
			gutter.addEventListener("keydown", function (e) {
				if (!DESKTOP_QUERY.matches) return;
				if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
				setWidth(key, paneFor(row, key).getBoundingClientRect().width + (e.key === "ArrowRight" ? 16 : -16));
				savePaneWidths(row);
				e.preventDefault();
			});
		});

		row.addEventListener("pointermove", function (e) {
			if (!dragging) return;
			setWidth(dragging.key, dragging.startWidth + (e.clientX - dragging.startX));
		});
		function endDrag() {
			if (!dragging) return;
			dragging = null;
			row.querySelectorAll(".pane-gutter.is-dragging").forEach(function (g) {
				g.classList.remove("is-dragging");
			});
			savePaneWidths(row);
		}
		row.addEventListener("pointerup", endDrag);
		row.addEventListener("pointercancel", endDrag);
	}

	document.addEventListener("DOMContentLoaded", function () {
		syncPaneWidths();
		DESKTOP_QUERY.addEventListener("change", syncPaneWidths);
		initResizablePanes();
	});
	// A full panes swap (any change to a book) brings new gutters to bind.
	document.addEventListener("htmx:afterSettle", function (e) {
		if (e.target && e.target.id === "books-panes") initResizablePanes();
	});
	// Back/forward restores <body> from htmx's history cache: new gutters,
	// no afterSettle (#456).
	document.addEventListener("htmx:historyRestore", initResizablePanes);

	// --- The open book's row ------------------------------------------------
	//
	// A full render marks it on the server; these two cover what the server
	// doesn't see: a click that opens a book (only the book pane is swapped),
	// and a list swap, which leaves the book pane as it was.
	function markActive(id) {
		document.querySelectorAll(".books-row").forEach(function (li) {
			li.classList.toggle("is-active", id !== "" && li.getAttribute("data-book-id") === id);
		});
	}

	document.addEventListener("click", function (e) {
		var link = e.target.closest(".books-row-link");
		if (link) markActive(link.parentNode.getAttribute("data-book-id"));
	});

	document.addEventListener("htmx:afterSwap", function (e) {
		if (!e.target || e.target.id !== "books-list") return;
		var book = document.getElementById("books-book");
		markActive((book && book.getAttribute("data-book-id")) || "");
		syncBookContext(e.target, book);
	});

	// A list swap leaves the book pane alone, so its hidden shelf/tag/q
	// fields and Edit link would keep POSTing and returning to the previous
	// list while the address bar shows the new one. Copy the new list's
	// context (data-* on #books-list) into them, as reader.js does for its
	// own panes. tag and q are only rendered when non-empty, so they are
	// created and removed here too.
	function syncBookContext(list, book) {
		if (!book) return;
		var ctx = {
			shelf: list.getAttribute("data-shelf") || "",
			tag: list.getAttribute("data-tag") || "",
			q: list.getAttribute("data-q") || "",
		};
		book.querySelectorAll("input[name=shelf]").forEach(function (shelf) {
			shelf.value = ctx.shelf;
			["tag", "q"].forEach(function (name) {
				var field = shelf.parentNode.querySelector("input[name=" + name + "]");
				if (!ctx[name]) {
					if (field) field.remove();
					return;
				}
				if (!field) {
					field = document.createElement("input");
					field.type = "hidden";
					field.name = name;
					shelf.parentNode.appendChild(field);
				}
				field.value = ctx[name];
			});
		});
		var edit = book.querySelector(".books-edit-link");
		var id = book.getAttribute("data-book-id");
		if (edit && id) {
			var qs = new URLSearchParams();
			qs.set("shelf", ctx.shelf);
			if (ctx.tag) qs.set("tag", ctx.tag);
			if (ctx.q) qs.set("q", ctx.q);
			edit.setAttribute("href", "/books/edit/" + id + "?" + qs.toString());
		}
	}

	// --- Keyboard shortcuts --------------------------------------------------

	// Keys are ignored while typing, so "/" in the filter box is a slash.
	function isTyping(el) {
		if (!el) return false;
		var tag = el.tagName;
		return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable;
	}

	function step(delta) {
		var links = Array.prototype.slice.call(document.querySelectorAll(".books-row-link"));
		if (!links.length) return;
		var active = document.querySelector(".books-row.is-active .books-row-link");
		var i = active ? links.indexOf(active) : -1;
		var next = i === -1 ? 0 : Math.min(links.length - 1, Math.max(0, i + delta));
		if (next === i) return;
		links[next].click();
		links[next].scrollIntoView({ block: "nearest" });
	}

	document.addEventListener("keydown", function (e) {
		if (e.defaultPrevented || e.repeat || e.altKey || e.ctrlKey || e.metaKey) return;
		// The modal confirm owns the keyboard (Escape still closes it natively).
		if (document.querySelector("dialog[open]")) return;
		if (!document.getElementById("books-panes")) return;
		if (isTyping(e.target)) {
			if (e.key === "Escape") {
				if (e.target.id === "books-q") {
					e.target.blur();
				} else {
					// Esc from the date field of an open Finish/DNF disclosure (or
					// the menu) closes it and returns focus to its summary.
					var open = e.target.closest("details.books-close[open], details.books-menu[open]");
					if (open) {
						open.open = false;
						var summary = open.querySelector("summary");
						if (summary) summary.focus();
					}
				}
			}
			return;
		}
		switch (e.key) {
		case "j":
			step(1);
			break;
		case "k":
			step(-1);
			break;
		case "/":
			var q = document.getElementById("books-q");
			if (!q) return;
			q.focus();
			q.select();
			break;
		case "a":
			window.location.href = "/books/new";
			break;
		case "Escape":
			document.querySelectorAll("details.books-close[open], details.books-menu[open]").forEach(function (d) {
				d.open = false;
			});
			return;
		default:
			return;
		}
		e.preventDefault();
	});
})();
