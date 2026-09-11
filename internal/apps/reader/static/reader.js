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
