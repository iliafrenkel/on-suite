// internal/ui/static/theme.js
//
// Theme, font and sidebar-collapsed state are plain, non-HttpOnly cookies:
// this script is the only thing that ever writes them. The server
// (internal/platform/web/prefs.go) only reads them, so there is no endpoint
// and no CSRF concern here — flipping a preference is a page-level
// enhancement, not a request to the server.

(function () {
	"use strict";

	function setCookie(name, value) {
		var secure = location.protocol === "https:" ? "; Secure" : "";
		document.cookie = name + "=" + value + "; Path=/; Max-Age=31536000; SameSite=Lax" + secure;
	}

	function markActive(buttons, attr, value) {
		for (var i = 0; i < buttons.length; i++) {
			buttons[i].classList.toggle("active", buttons[i].getAttribute(attr) === value);
		}
	}

	function initThemeSwitch() {
		var group = document.querySelector("[data-theme-switch]");
		if (!group) return;
		var buttons = group.querySelectorAll("[data-theme-value]");
		markActive(buttons, "data-theme-value", document.documentElement.getAttribute("data-theme"));
		buttons.forEach(function (btn) {
			btn.addEventListener("click", function () {
				var value = btn.getAttribute("data-theme-value");
				document.documentElement.setAttribute("data-theme", value);
				setCookie("onsuite_theme", value);
				markActive(buttons, "data-theme-value", value);
			});
		});
	}

	function initFontSwitch() {
		var group = document.querySelector("[data-font-switch]");
		if (!group) return;
		var buttons = group.querySelectorAll("[data-font-value]");
		markActive(buttons, "data-font-value", document.documentElement.getAttribute("data-font"));
		buttons.forEach(function (btn) {
			btn.addEventListener("click", function () {
				var value = btn.getAttribute("data-font-value");
				document.documentElement.setAttribute("data-font", value);
				setCookie("onsuite_font", value);
				markActive(buttons, "data-font-value", value);
			});
		});
	}

	function initSidebarToggle() {
		var sidebar = document.querySelector("[data-sidebar]");
		var toggle = document.querySelector("[data-sidebar-toggle]");
		if (!sidebar || !toggle) return;
		toggle.addEventListener("click", function () {
			var collapsed = sidebar.hasAttribute("data-collapsed");
			if (collapsed) {
				sidebar.removeAttribute("data-collapsed");
				setCookie("onsuite_sidebar", "expanded");
			} else {
				sidebar.setAttribute("data-collapsed", "");
				setCookie("onsuite_sidebar", "collapsed");
			}
		});
	}

	// The server has no reliable notion of its own origin (it may sit behind
	// a reverse proxy), so the absolute share URL is built here from
	// location.origin instead of being rendered server-side.
	//
	// navigator.clipboard can still reject (e.g. no permission, or a
	// document that never received focus), so there is always a fallback
	// rather than leaving the click silently do nothing.
	function legacyCopy(text) {
		var input = document.createElement("textarea");
		input.className = "visually-hidden";
		input.value = text;
		document.body.appendChild(input);
		input.select();
		document.execCommand("copy");
		document.body.removeChild(input);
	}

	function copyText(text) {
		if (navigator.clipboard && window.isSecureContext) {
			return navigator.clipboard.writeText(text).catch(function () {
				legacyCopy(text);
			});
		}
		legacyCopy(text);
		return Promise.resolve();
	}

	function flashCopied(btn) {
		var original = btn.textContent;
		btn.textContent = "Copied";
		setTimeout(function () { btn.textContent = original; }, 1500);
	}

	// Delegated on document rather than attached per-button via
	// querySelectorAll at load time, for the same reason as initConfirm
	// below: a Copy/Copy-link button can arrive later via an htmx swap
	// (e.g. the paste split-view's detail pane) and would otherwise never
	// get a listener.
	function initCopyLink() {
		document.addEventListener("click", function (e) {
			var btn = e.target.closest("[data-copy-link]");
			if (!btn) return;
			var url = location.origin + btn.getAttribute("data-copy-link");
			copyText(url).then(function () { flashCopied(btn); });
		});
	}

	// The snippet's own content, not just its share link — fetched from the
	// same raw endpoint the "Raw" link points to rather than duplicating the
	// body into the page, so this works from the copy verbatim (no
	// highlighting markup) with no risk of drifting from what "Raw" shows.
	//
	// Delegated on document for the same reason as initCopyLink above.
	function initCopyRaw() {
		document.addEventListener("click", function (e) {
			var btn = e.target.closest("[data-copy-raw]");
			if (!btn) return;
			fetch(btn.getAttribute("data-copy-raw"))
				.then(function (res) { return res.text(); })
				.then(function (text) { return copyText(text); })
				.then(function () { flashCopied(btn); })
				.catch(function () {
					var original = btn.textContent;
					btn.textContent = "Copy failed";
					setTimeout(function () { btn.textContent = original; }, 1500);
				});
		});
	}

	// The server renders an absolute timestamp so the page is correct with
	// JS disabled; this replaces it with a relative one for anything recent
	// enough that "how long ago" is more useful than a clock reading. The
	// absolute time survives in the title attribute either way, for hover.
	function relativeTime(then, now) {
		var seconds = Math.round((now - then) / 1000);
		if (seconds < 5) return "just now";
		if (seconds < 60) return seconds + " seconds ago";
		var minutes = Math.round(seconds / 60);
		if (minutes < 60) return minutes + (minutes === 1 ? " minute ago" : " minutes ago");
		var hours = Math.round(minutes / 60);
		if (hours < 24) return hours + (hours === 1 ? " hour ago" : " hours ago");
		var days = Math.round(hours / 24);
		if (days < 7) return days + (days === 1 ? " day ago" : " days ago");
		return null; // further back reads more clearly as an absolute date
	}

	function initRelativeTimes() {
		document.querySelectorAll("time[datetime]").forEach(function (el) {
			var then = new Date(el.getAttribute("datetime"));
			if (isNaN(then.getTime())) return;
			var label = relativeTime(then, new Date());
			if (label) el.textContent = label;
		});
	}

	// A generic confirmation gate for any destructive form — data-confirm
	// carries the message, so a new destructive action anywhere in the
	// suite gets this for free instead of needing its own JS.
	//
	// This is delegated on document rather than attached per-form via
	// querySelectorAll at load time, because forms can arrive later via an
	// htmx swap (e.g. the paste split-view's detail pane) and would
	// otherwise never get a listener.
	//
	// A form with hx-post/hx-get is handled entirely through htmx's own
	// "htmx:confirm" event rather than the plain "submit" event: htmx
	// registers its own submit-interception on such forms, and calling
	// preventDefault() from a *different* submit listener does not stop
	// that other listener from running, so the plain-submit path alone
	// cannot actually block an HTMX-driven request. Routing HTMX-enhanced
	// forms through "htmx:confirm" instead (and never through both) avoids
	// double dialogs and makes Cancel actually cancel.
	// HTMX_VERBS mirrors htmx 2.0's own verb attributes (htmx.min.js), so
	// isHtmxForm recognizes any of them rather than just the two this file
	// happened to use first — hx-delete or hx-put on a future data-confirm
	// form would otherwise fall through to the plain-submit branch and
	// reintroduce the exact Cancel-bypass bug the split above exists to
	// prevent. The "data-hx-" prefix is htmx's own alternative spelling for
	// hosts where a bare "hx-*" attribute isn't valid (e.g. some template
	// engines); this app only ever writes the bare form, but a form is
	// still htmx-driven either way.
	var HTMX_VERBS = ["get", "post", "put", "patch", "delete"];

	function isHtmxForm(form) {
		return HTMX_VERBS.some(function (verb) {
			return form.hasAttribute("hx-" + verb) || form.hasAttribute("data-hx-" + verb);
		});
	}

	function initConfirm() {
		document.addEventListener("submit", function (e) {
			var form = e.target.closest("form[data-confirm]");
			if (!form || isHtmxForm(form)) return;
			if (!window.confirm(form.getAttribute("data-confirm"))) {
				e.preventDefault();
			}
		});

		document.addEventListener("htmx:confirm", function (e) {
			var form = e.target.closest("form[data-confirm]");
			if (!form || !isHtmxForm(form)) return;
			e.preventDefault();
			if (window.confirm(form.getAttribute("data-confirm"))) {
				e.detail.issueRequest();
			}
		});
	}

	initThemeSwitch();
	initFontSwitch();
	initSidebarToggle();
	initCopyLink();
	initCopyRaw();
	initRelativeTimes();
	initConfirm();
})();
