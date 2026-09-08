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
