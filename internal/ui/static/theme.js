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

	function initCopyLink() {
		document.querySelectorAll("[data-copy-link]").forEach(function (btn) {
			btn.addEventListener("click", function () {
				var url = location.origin + btn.getAttribute("data-copy-link");
				copyText(url).then(function () {
					var original = btn.textContent;
					btn.textContent = "Copied";
					setTimeout(function () { btn.textContent = original; }, 1500);
				});
			});
		});
	}

	initThemeSwitch();
	initFontSwitch();
	initSidebarToggle();
	initCopyLink();
})();
