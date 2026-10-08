// ON Focus's session recording (spec: "Recording a session"), shared by
// the running page (focus.js) and the home page's banner (home.js). It only
// talks to the server; callers clear the stored session once it is saved,
// so an unsaved one stays in localStorage for Retry.
"use strict";

(function () {
	var S = window.OnFocus.session;

	// eligible: an ended session whose timer had Keep history on when it
	// started, with at least a minute of focus.
	function eligible(s) {
		return s.finished && s.keepHistory && S.focusSeconds(s, s.endedAt) >= 60;
	}

	function body(s) {
		return {
			client_id: s.clientId,
			timer_id: s.timerId,
			timer_name: s.timerName,
			color: s.color,
			started_at: s.startedAt,
			ended_at: s.endedAt,
			focus_seconds: S.focusSeconds(s, s.endedAt),
			rounds_done: s.roundsDone,
			completed: s.completed
		};
	}

	// csrfToken is the token htmx sends, from <body hx-headers>; mirrors
	// later.js's.
	function csrfToken() {
		try {
			return JSON.parse(document.body.getAttribute("hx-headers"))["X-CSRF-Token"] || "";
		} catch (e) {
			return "";
		}
	}

	// send POSTs an ended session. It resolves to "saved" (the server has
	// it, including "already recorded"), "rejected" (400/422: it never
	// will) or "failed" (offline, signed out, a server error: worth a
	// Retry). keepalive lets the request finish if the page unloads first —
	// Exit, then close the tab — and, unlike sendBeacon, carries the CSRF
	// header (F3 plan). redirect "manual" makes a bounce to the login page
	// a failure rather than a 200.
	function send(s) {
		return fetch("/focus/sessions", {
			method: "POST",
			credentials: "same-origin",
			keepalive: true,
			redirect: "manual",
			headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken() },
			body: JSON.stringify(body(s))
		}).then(function (res) {
			if (res.ok) return "saved";
			if (res.status === 400 || res.status === 422) {
				console.warn("ON Focus: the server refused this session (" + res.status + "); dropping it");
				return "rejected";
			}
			return "failed";
		}, function () {
			return "failed";
		});
	}

	window.OnFocus.record = { eligible: eligible, send: send };
})();
