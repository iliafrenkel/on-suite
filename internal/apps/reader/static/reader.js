// ON Reader keyboard navigation and prefetching.
//
// Vanilla, external, CSP-clean: the suite forbids inline script, and hx-* are
// plain attributes. This follows internal/apps/notes/static/notes.js rather
// than introducing a client-state framework — ON Reader is not the app that
// should make that decision for the suite.
(function () {
	"use strict";

	// Keys are ignored while typing, so "/" in the search box is a slash.
	function isTyping(el) {
		if (!el) return false;
		var tag = el.tagName;
		return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable;
	}

	function rows() {
		return Array.prototype.slice.call(document.querySelectorAll(".reader-row a"));
	}

	function currentIndex(all) {
		var active = document.querySelector(".reader-row.is-active a");
		return active ? all.indexOf(active) : -1;
	}

	// Prefetched article HTML, keyed by the item path. Cleared whenever the
	// list itself is replaced, since the ids in it may no longer exist.
	var prefetched = Object.create(null);

	function prefetch(link) {
		if (!link) return;
		var path = link.getAttribute("href");
		if (!path || prefetched[path]) return;
		// The read-only variant: rendering without marking read. Arrowing past
		// an article must not mark it read.
		fetch(path + (path.indexOf("?") === -1 ? "?" : "&") + "prefetch=1", {
			credentials: "same-origin",
			headers: { "HX-Request": "true" }
		}).then(function (res) {
			return res.ok ? res.text() : null;
		}).then(function (html) {
			if (html) prefetched[path] = html;
		}).catch(function () {
			// A failed prefetch is not worth reporting: the real open will
			// either work or surface its own error.
		});
	}

	function select(index) {
		var all = rows();
		if (!all.length) return;
		if (index < 0) index = 0;
		if (index >= all.length) index = all.length - 1;

		all.forEach(function (a) {
			a.parentElement.classList.remove("is-active");
		});
		var link = all[index];
		link.parentElement.classList.add("is-active");
		link.scrollIntoView({ block: "nearest" });
		link.focus({ preventScroll: true });

		prefetch(all[index + 1]);
		prefetch(all[index - 1]);
	}

	function move(delta) {
		var all = rows();
		select(currentIndex(all) + delta);
	}

	function open() {
		var link = document.querySelector(".reader-row.is-active a");
		if (!link) return;
		var path = link.getAttribute("href");
		var cached = prefetched[path];
		var target = document.querySelector("#reader-article");
		if (cached && target && window.htmx) {
			// Plain DOM replacement plus htmx.process, rather than htmx.swap:
			// process() has been public API across htmx versions, and this
			// vendored build is pinned at 2.0.10. The replacement HTML is full
			// of hx-* attributes, so it must be processed or its buttons go
			// dead.
			target.outerHTML = cached;
			var fresh = document.querySelector("#reader-article");
			if (fresh) window.htmx.process(fresh);
			// The prefetch deliberately did not mark it read. This does.
			markRead(link);
			return;
		}
		link.click();
	}

	function markRead(link) {
		var path = link.getAttribute("href");
		if (!window.htmx) return;
		// The href carries a query string (?scope=...&sub=...&filter=...), so
		// "/read" has to land before it, not after — appending blindly would
		// build a URL like "/item/17?filter=unread/read", which 404s and
		// silently leaves the article unread on the cached-swap fast path.
		var q = path.indexOf("?");
		var base = q === -1 ? path : path.slice(0, q);
		window.htmx.ajax("POST", base + "/read" + (q === -1 ? "" : path.slice(q)), { swap: "none" });

		// The pane just swapped in came from the prefetch cache, which never
		// marks an article read, so its read-toggle button still says "Mark
		// read" and posts to .../read. The request above is fire-and-forget
		// (swap: none) and won't correct that on its own — without this, "m"
		// pressed right after opening would hit the stale action and silently
		// re-confirm read instead of toggling to unread.
		var toggle = document.querySelector(".reader-article-read-toggle");
		var form = toggle && toggle.closest("form");
		if (form) {
			form.setAttribute("hx-post", base + "/unread");
			window.htmx.process(form);
		}
		if (toggle) toggle.textContent = "Mark unread";
	}

	// --- Resizable panes -----------------------------------------------
	//
	// Desktop-only (matches the 900px breakpoint where the CSS collapses to
	// two panes): below it the flex/gutter layout stops being interactive
	// and the CSS in app.css takes over pane widths entirely, so any stored
	// inline custom property has to be cleared rather than just ignored —
	// otherwise a width dragged wide on desktop would also apply the moment
	// the media query's own rules stopped overriding it.
	var PANE_STORE_KEY = "reader.paneWidths";
	var PANE_MIN = { tree: 10, list: 14 }; // rem
	var PANE_MAX = { tree: 24, list: 32 }; // rem
	var DESKTOP_QUERY = window.matchMedia("(min-width: 901px)");

	function remToPx(rem) {
		var rootPx = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
		return rem * rootPx;
	}

	function loadPaneWidths() {
		try {
			var raw = window.localStorage.getItem(PANE_STORE_KEY);
			if (!raw) return null;
			var parsed = JSON.parse(raw);
			if (typeof parsed.tree !== "number" || typeof parsed.list !== "number") return null;
			return parsed;
		} catch (e) {
			return null;
		}
	}

	function savePaneWidths(widths) {
		try {
			window.localStorage.setItem(PANE_STORE_KEY, JSON.stringify(widths));
		} catch (e) {
			// Private browsing or a full quota: the drag itself still worked,
			// it just won't be remembered next time.
		}
	}

	function currentPaneWidths(row) {
		var tree = row.querySelector(".reader-tree");
		var list = row.querySelector(".reader-list");
		return {
			tree: Math.round(tree.getBoundingClientRect().width),
			list: Math.round(list.getBoundingClientRect().width),
		};
	}

	function syncPaneWidths() {
		var row = document.getElementById("reader-panes-row");
		if (!row) return;
		if (!DESKTOP_QUERY.matches) {
			row.style.removeProperty("--reader-tree-w");
			row.style.removeProperty("--reader-list-w");
			return;
		}
		var stored = loadPaneWidths();
		if (!stored) return;
		row.style.setProperty("--reader-tree-w", clampPx(stored.tree, "tree") + "px");
		row.style.setProperty("--reader-list-w", clampPx(stored.list, "list") + "px");
	}

	function clampPx(rawPx, key) {
		var min = remToPx(PANE_MIN[key]);
		var max = remToPx(PANE_MAX[key]);
		return Math.min(max, Math.max(min, rawPx));
	}

	function initResizablePanes() {
		var row = document.getElementById("reader-panes-row");
		if (!row) return;
		syncPaneWidths();

		var dragging = null; // { key: "tree"|"list", startX, startWidth }

		function setWidth(key, px) {
			row.style.setProperty("--reader-" + key + "-w", clampPx(px, key) + "px");
		}

		row.querySelectorAll(".pane-gutter").forEach(function (gutter) {
			var key = gutter.getAttribute("data-gutter-for");

			gutter.addEventListener("pointerdown", function (e) {
				if (!DESKTOP_QUERY.matches) return;
				var target = row.querySelector(key === "tree" ? ".reader-tree" : ".reader-list");
				dragging = { key: key, startX: e.clientX, startWidth: target.getBoundingClientRect().width };
				gutter.classList.add("is-dragging");
				gutter.setPointerCapture(e.pointerId);
				e.preventDefault();
			});

			// The WAI-ARIA "separator" keyboard pattern: arrow keys nudge the
			// pane a fixed step, for anyone who cannot drag with a pointer.
			gutter.addEventListener("keydown", function (e) {
				if (!DESKTOP_QUERY.matches) return;
				if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
				var target = row.querySelector(key === "tree" ? ".reader-tree" : ".reader-list");
				var step = e.key === "ArrowRight" ? 16 : -16;
				setWidth(key, target.getBoundingClientRect().width + step);
				savePaneWidths(currentPaneWidths(row));
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
			savePaneWidths(currentPaneWidths(row));
		}
		row.addEventListener("pointerup", endDrag);
		row.addEventListener("pointercancel", endDrag);

		DESKTOP_QUERY.addEventListener("change", syncPaneWidths);
	}

	document.addEventListener("DOMContentLoaded", initResizablePanes);
	// A panes-wide swap (subscribing, refreshing, deleting) replaces
	// #reader-panes-row outerHTML-style, wiping any inline custom
	// properties JS had set — without re-running this, the layout would
	// silently fall back to the CSS defaults on the very next feed click.
	document.addEventListener("htmx:afterSwap", function (e) {
		if (e.target && e.target.id === "reader-panes") initResizablePanes();
	});

	function press(selector) {
		var el = document.querySelector(selector);
		if (el) el.click();
	}

	document.addEventListener("keydown", function (e) {
		if (e.metaKey || e.ctrlKey || e.altKey) return;
		if (isTyping(e.target) && e.key !== "Escape") return;

		switch (e.key) {
			case "j": move(1); break;
			case "k": move(-1); break;
			case "o":
			case "Enter": open(); break;
			case "m": press(".reader-article-read-toggle"); break;
			case "s": press(".reader-article-star"); break;
			case "r": press(".reader-refresh"); break;
			case "/": {
				var box = document.querySelector("#reader-search-input");
				if (box) { e.preventDefault(); box.focus(); box.select(); }
				return;
			}
			case "Escape":
				if (isTyping(e.target)) e.target.blur();
				return;
			default: return;
		}
		e.preventDefault();
	});

	// A new list means new ids: anything prefetched against the old one is
	// stale, and keeping it would swap the wrong article into the pane.
	document.addEventListener("htmx:afterSwap", function (e) {
		if (e.target && e.target.id === "reader-panes") {
			prefetched = Object.create(null);
		}
	});
})();
