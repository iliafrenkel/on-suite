// internal/ui/static/htmx-notices.js
//
// Shared implementation of the "server-rejection surfaced as a dismissable
// notice" pattern (see PATTERNS.md): an htmx action fails, and swap:false
// (htmx 2.0's default responseHandling for 4xx/5xx, and the only outcome
// of a network-level sendError) would otherwise leave the user with no
// feedback at all. Originally duplicated per-action in
// internal/apps/notes/static/notes.js (initPasteErrors/initMoveErrors).
// PATTERNS.md documents "duplicate, don't extract" as this project's normal
// convention for this kind of pattern (see "Cross-app mirroring instead of
// a shared package"), but that default is about cross-app domain-logic
// duplication — this is shared UI chrome infrastructure, the same category
// as theme.js. The project owner was consulted on that exact tension during
// planning and chose to extract this one anyway, even though notes.js is
// still the only caller (initPasteErrors/initMoveErrors) — not because a
// third or fourth caller appeared.
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
