// internal/ui/static/connectivity.js
//
// Online/offline indicator for the shared shell bar, and the earliest
// possible offline signal for any HTMX action anywhere in the suite.
// navigator.onLine only reflects the network interface, not whether the
// server itself is reachable (the server can be down, or its database can
// be down behind a live server — see healthzHandler in
// cmd/onsuite/serve.go), so this combines three signals: the browser's own
// online/offline events, a periodic /healthz poll, and any htmx:sendError
// (a network-level HTMX failure) bubbling up from anywhere on the page.
// See docs/superpowers/specs/2026-09-08-connectivity-indicator-design.md.
(function () {
	"use strict";

	var POLL_INTERVAL_MS = 15000;
	var online = true;
	var indicator = null;
	// Incremented on every checkHealth() call, captured by that call's own
	// closure. A response only applies if it's still the most recent check
	// in flight — otherwise it's a straggler from a hung request that a
	// newer, faster check (the online event's own immediate re-check, in
	// particular) has already superseded, and applying it would flip the
	// indicator backwards to a stale answer.
	var healthCheckEpoch = 0;

	function setOnline(next) {
		if (next === online) return;
		online = next;
		indicator.setAttribute("data-status", online ? "online" : "offline");
		indicator.setAttribute("title", online
			? "All changes are saving normally"
			: "You're offline — changes won't be saved until you reconnect");
		indicator.setAttribute("aria-label", online
			? "Online — all changes are saving normally"
			: "Offline — changes won't be saved until you reconnect");
		document.dispatchEvent(new CustomEvent("connectivity:change", { detail: { online: online } }));
	}

	function checkHealth() {
		// A client-side timeout shorter than POLL_INTERVAL_MS guards against a
		// hung connection (captive portal, blackholed TCP, server accepting but
		// not responding) that would otherwise never reject and leave the dot
		// green until the browser's own much longer network timeout. 5000ms is
		// comfortably above /healthz's own 2s DB-ping budget (see healthzHandler
		// in cmd/onsuite/serve.go) and comfortably below the 15s poll interval.
		//
		// That alone would keep same-source polls from overlapping, but the
		// online event handler below can also trigger a check between polls —
		// so healthCheckEpoch guards against THAT overlap: a hung check's
		// eventual response is discarded if a newer check already answered.
		var epoch = ++healthCheckEpoch;
		fetch("/healthz", { cache: "no-store", signal: AbortSignal.timeout(5000) })
			.then(function (res) { if (epoch === healthCheckEpoch) setOnline(res.ok); })
			.catch(function () { if (epoch === healthCheckEpoch) setOnline(false); });
	}

	// Gated on the indicator's presence, the same way theme.js's own
	// init* functions each bail out when their target element is absent
	// (e.g. initSidebarToggle) — a logged-out or public page renders no
	// .shell-user (see PATTERNS.md's "Chrome visibility gated on
	// Shell.LoggedIn"), so there is nothing here to poll for.
	function init() {
		indicator = document.querySelector("[data-conn-indicator]");
		if (!indicator) return;

		window.addEventListener("online", function () { setOnline(true); checkHealth(); });
		window.addEventListener("offline", function () { setOnline(false); });
		document.addEventListener("htmx:sendError", function () { setOnline(false); });

		checkHealth();
		setInterval(checkHealth, POLL_INTERVAL_MS);
	}

	init();
})();
