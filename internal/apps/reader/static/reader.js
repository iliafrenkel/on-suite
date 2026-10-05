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

	// Shared by keyboard nav (select, below) and the plain-click handler
	// further down: exactly one row is ever "is-active" — the currently open
	// article — whichever of the two last touched it.
	function highlightRow(link) {
		rows().forEach(function (a) {
			a.parentElement.classList.remove("is-active");
		});
		if (link) link.parentElement.classList.add("is-active");
	}

	function select(index) {
		var all = rows();
		if (!all.length) return;
		if (index < 0) index = 0;
		if (index >= all.length) index = all.length - 1;

		var link = all[index];
		highlightRow(link);
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
		var root = document.documentElement;
		if (!DESKTOP_QUERY.matches) {
			root.style.removeProperty("--reader-tree-w");
			root.style.removeProperty("--reader-list-w");
			return;
		}
		var stored = loadPaneWidths();
		if (!stored) return;
		root.style.setProperty("--reader-tree-w", clampPx(stored.tree, "tree") + "px");
		root.style.setProperty("--reader-list-w", clampPx(stored.list, "list") + "px");
	}

	function clampPx(rawPx, key) {
		var min = remToPx(PANE_MIN[key]);
		var max = remToPx(PANE_MAX[key]);
		return Math.min(max, Math.max(min, rawPx));
	}

	function initResizablePanes() {
		var row = document.getElementById("reader-panes-row");
		if (!row) return;

		var dragging = null; // { key: "tree"|"list", startX, startWidth }

		function setWidth(key, px) {
			document.documentElement.style.setProperty("--reader-" + key + "-w", clampPx(px, key) + "px");
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
	}

	// --- Favicon fallback ------------------------------------------------
	//
	// The sidebar tree renders <img class="reader-favicon"> with a sibling
	// <span class="reader-favicon" hidden>{{rss glyph}}</span> fallback. This
	// used to be an inline onerror="..." attribute, but the suite's CSP
	// (script-src 'self', no unsafe-inline/unsafe-hashes) silently blocks
	// inline event-handler attributes, so that fallback never ran. The DOM
	// "error" event does not bubble, so a plain document-level listener
	// would never see it either — capture phase (the `true` below) is what
	// makes delegation work here. One listener, added once, correctly
	// handles every favicon image already on the page and any added later
	// by htmx swaps.
	document.addEventListener("error", function (e) {
		var img = e.target;
		if (!img.matches || !img.matches("img.reader-favicon")) return;
		var fallback = img.nextElementSibling;
		if (fallback) fallback.hidden = false;
		img.remove();
	}, true);

	// The widths live on <html>, not on #reader-panes-row: the row is inside
	// every full panes swap, and a width kept on it was wiped on each one and
	// only restored once htmx settled, so all three panes visibly snapped to
	// the CSS defaults and back (issue #453). Nothing swaps <html>, so this
	// runs once per page load, and so does the media-query listener — it used
	// to be added again on every swap.
	document.addEventListener("DOMContentLoaded", function () {
		syncPaneWidths();
		DESKTOP_QUERY.addEventListener("change", syncPaneWidths);
		initResizablePanes();
	});
	// A full panes swap (subscribing, refreshing, deleting and the other tree
	// edits) replaces #reader-panes-row and its gutters, so their drag and
	// keyboard listeners have to be bound again. The widths themselves live on
	// <html> and survive the swap. A list-only swap leaves the row alone and
	// needs none of this.
	document.addEventListener("htmx:afterSettle", function (e) {
		if (e.target && e.target.id === "reader-panes") initResizablePanes();
	});
	// Back/forward restores <body> from htmx's history snapshot: new gutters
	// with no listeners, and no afterSettle on #reader-panes to rebind them
	// (issue #456).
	document.addEventListener("htmx:historyRestore", initResizablePanes);

	// --- Tree sync after a list-only swap --------------------------------
	//
	// A list navigation (a feed, All/Starred, a filter, a search, Mark all
	// read) swaps only #reader-list, so the tree keeps its DOM: collapsed
	// folders, its scroll position and its favicons all survive (issue #453).
	// What a full render would have drawn differently in the tree arrives on
	// the new list as data-*: which list is selected, and, with "hide read"
	// on, which feeds and folders the server's filtered tree dropped. This
	// copies that across and decides nothing itself. It can only remove feeds;
	// one that has to come back is handled by the X-Reader-Tree fallback below.
	function idList(el, name) {
		return (el.getAttribute(name) || "").split(" ").filter(Boolean);
	}

	function removeById(prefix) {
		return function (id) {
			var el = document.getElementById(prefix + id);
			if (el) el.remove();
		};
	}

	// The tree's and the dialogs' forms each carry the list on screen in
	// their reader-ctx hidden fields, so a rename or refresh re-renders the
	// list the reader is looking at. A list-only swap does not re-render
	// them, so without this they would still name the list from the last
	// full render, and renaming a feed after clicking All would land the
	// panes back on the previous feed. The article pane is skipped: it is
	// empty after a list swap, and an open article's forms carry their own
	// context (including "view").
	var CTX_FIELDS = { scope: "data-scope", sub: "data-sub", filter: "data-filter", q: "data-q" };

	function syncTree() {
		var list = document.getElementById("reader-list");
		if (!list) return;
		var scope = list.getAttribute("data-scope");
		var activeRow = scope === "feed" ? "reader-sub-" + list.getAttribute("data-sub") : "";
		document.querySelectorAll(".reader-tree .reader-sub").forEach(function (row) {
			row.classList.toggle("is-active", row.id === activeRow);
		});
		document.querySelectorAll(".reader-tree-nav a[data-scope]").forEach(function (link) {
			link.classList.toggle("toolbar-btn-active", link.getAttribute("data-scope") === scope);
		});
		idList(list, "data-hidden-subs").forEach(removeById("reader-sub-"));
		idList(list, "data-hidden-folders").forEach(removeById("reader-folder-"));
		var panes = document.getElementById("reader-panes");
		if (panes) {
			Object.keys(CTX_FIELDS).forEach(function (name) {
				var value = list.getAttribute(CTX_FIELDS[name]) || "";
				panes.querySelectorAll('input[type="hidden"][name="' + name + '"]').forEach(function (input) {
					if (!input.closest("#reader-article")) input.value = value;
				});
			});
		}
	}

	// A list-only swap cannot add a feed to the tree, only take one away. So
	// every list request says which feeds the tree has, and when the server
	// would show one it lacks (a hidden feed that has unread items again, or a
	// subscription made elsewhere), it answers with the whole panes instead.
	document.addEventListener("htmx:configRequest", function (e) {
		if (!e.detail.target || e.detail.target.id !== "reader-list") return;
		var ids = [];
		document.querySelectorAll(".reader-tree .reader-sub").forEach(function (row) {
			ids.push(row.id.replace("reader-sub-", ""));
		});
		e.detail.headers["X-Reader-Tree"] = ids.join(" ");
	});

	// Read the list back by id rather than off the event: after an outerHTML
	// swap, the element the event names is not guaranteed to be the one now
	// in the document.
	document.addEventListener("htmx:afterSettle", function (e) {
		if (e.target && e.target.id === "reader-list") syncTree();
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

	// A plain mouse click bypasses select()/move() entirely — those are what
	// keep .is-active in sync for j/k navigation. Without this, clicking a
	// different article with the mouse left whichever row a keyboard press
	// had last highlighted (or nothing at all) still marked, instead of the
	// row someone actually just opened. The server also renders the truly-
	// open article's row as is-active on a full list render (indexView.List
	// .ActiveID); this covers the same-list, mouse-driven case that render
	// never sees.
	document.addEventListener("click", function (e) {
		var link = e.target.closest(".reader-row a");
		if (link) highlightRow(link);
	});

	// A new list means new ids, whether it came with the whole panes or alone:
	// anything prefetched against the old one is stale, and keeping it would
	// swap the wrong article into the pane.
	document.addEventListener("htmx:afterSwap", function (e) {
		if (e.target && (e.target.id === "reader-panes" || e.target.id === "reader-list")) {
			prefetched = Object.create(null);
		}
	});

	// --- Overflow menu ---------------------------------------------------

	function closeMenu(menu) {
		var list = menu.querySelector(".reader-menu-list");
		var toggle = menu.querySelector(".reader-menu-toggle");
		if (list) list.hidden = true;
		if (toggle) toggle.setAttribute("aria-expanded", "false");
	}

	document.addEventListener("click", function (e) {
		var toggle = e.target.closest(".reader-menu-toggle");
		document.querySelectorAll(".reader-menu").forEach(function (menu) {
			if (toggle && menu.contains(toggle)) {
				var list = menu.querySelector(".reader-menu-list");
				var wasOpen = !list.hidden;
				closeMenu(menu);
				if (!wasOpen) {
					list.hidden = false;
					toggle.setAttribute("aria-expanded", "true");
				}
			} else if (!menu.contains(e.target)) {
				closeMenu(menu);
			}
		});
	});

	// Escape closes an open overflow menu. (Dialogs already close on Escape
	// natively via <dialog>; this menu is a plain div, so it needs its own
	// handler.)
	document.addEventListener("keydown", function (e) {
		if (e.key !== "Escape") return;
		document.querySelectorAll(".reader-menu").forEach(function (menu) {
			var list = menu.querySelector(".reader-menu-list");
			if (list && !list.hidden) closeMenu(menu);
		});
	});

	// --- Copy feed URL -----------------------------------------------------

	// A plain client-side action: no server round trip, so it does not go
	// through htmx at all. The button's own label is the confirmation —
	// swapped to "Copied!" and back — rather than a toast, since this is a
	// small, single-purpose menu action.
	document.addEventListener("click", function (e) {
		var btn = e.target.closest(".reader-copy-feed-url");
		if (!btn) return;
		var url = btn.getAttribute("data-feed-url");
		if (!url) return;

		var original = btn.textContent;
		function flash(label) {
			btn.textContent = label;
			setTimeout(function () { btn.textContent = original; }, 1500);
		}

		if (!navigator.clipboard || !navigator.clipboard.writeText) {
			flash("Couldn't copy");
			return;
		}
		navigator.clipboard.writeText(url).then(function () {
			flash("Copied!");
		}, function () {
			flash("Couldn't copy");
		});
	});

	// --- Dialogs -----------------------------------------------------------

	document.addEventListener("click", function (e) {
		var opener = e.target.closest("[data-open-dialog]");
		if (opener) {
			var dialog = document.getElementById(opener.getAttribute("data-open-dialog"));
			if (dialog && typeof dialog.showModal === "function") dialog.showModal();
			return;
		}
		var closer = e.target.closest(".reader-dialog-cancel, .reader-dialog-close");
		if (closer) {
			var open = closer.closest("dialog");
			if (open) open.close();
		}
	});

	function reopenFlaggedDialogs() {
		document.querySelectorAll("dialog[data-reopen]").forEach(function (d) {
			if (typeof d.showModal === "function") d.showModal();
		});
	}
	document.addEventListener("DOMContentLoaded", reopenFlaggedDialogs);
	document.addEventListener("htmx:afterSwap", function (e) {
		if (e.target && e.target.id === "reader-panes") reopenFlaggedDialogs();
	});

	// --- Confirm dialog, replacing window.confirm for hx-confirm ----------
	//
	// Scoped to #reader-panes so this never intercepts confirm() elsewhere in
	// the suite - theme.js's own data-confirm gate (internal/ui/static/theme.js)
	// is untouched and keeps handling every other app.
	document.addEventListener("htmx:confirm", function (e) {
		var panes = document.getElementById("reader-panes");
		if (!panes || !e.target || !panes.contains(e.target)) return;
		var msg = e.target.getAttribute("hx-confirm");
		if (!msg) return;

		var dialog = document.getElementById("reader-confirm-dialog");
		if (!dialog || typeof dialog.showModal !== "function") return; // let htmx fall back to window.confirm

		e.preventDefault();
		var messageEl = document.getElementById("reader-confirm-message");
		var ok = document.getElementById("reader-confirm-ok");
		if (messageEl) messageEl.textContent = msg;

		// Bind onOk through an AbortController tied to the dialog's own `close`
		// event, which fires no matter how the dialog closes (Confirm, Cancel,
		// or Escape). Without this, cancelling or pressing Escape left onOk
		// attached forever, so confirming a *later*, unrelated action would
		// also re-fire the earlier, declined one.
		var controller = new AbortController();
		dialog.addEventListener(
			"close",
			function () {
				controller.abort();
			},
			{ once: true }
		);

		function onOk() {
			dialog.close();
			// The `true` here is htmx's "skip the confirm gate" flag: without it
			// issueRequest() re-enters the same htmx:confirm check and htmx falls
			// back to its own window.confirm(), popping a second native dialog
			// right after ours.
			e.detail.issueRequest(true);
		}
		ok.addEventListener("click", onOk, { signal: controller.signal });
		dialog.showModal();
	});

	// ---- Read later failures --------------------------------------------------
	//
	// A rejected or failed save to ON Later would otherwise leave the button
	// looking as if nothing happened (htmx does not swap 4xx/5xx). Show the
	// suite's shared dismissable notice instead and leave the form in place so
	// the user can try again. Same shape as notes.js's initPasteErrors.
	function laterRequest(evt) {
		var detail = evt.detail || {};
		var path = (detail.pathInfo && detail.pathInfo.requestPath) ||
			(detail.requestConfig && detail.requestConfig.path) || "";
		return path.indexOf("/later/save") !== -1;
	}

	function laterNotices() {
		return (window.OnSuite && window.OnSuite.notices) || null;
	}

	function showLaterError(evt) {
		var n = laterNotices();
		if (!n || !laterRequest(evt)) return;
		n.show(evt.detail.elt, "reader-later-error", "Couldn't save to ON Later. Try again.");
	}

	document.body.addEventListener("htmx:responseError", showLaterError);
	document.body.addEventListener("htmx:sendError", showLaterError);
	document.body.addEventListener("htmx:beforeRequest", function (evt) {
		var n = laterNotices();
		if (n && laterRequest(evt)) n.clear("reader-later-error");
	});
})();
