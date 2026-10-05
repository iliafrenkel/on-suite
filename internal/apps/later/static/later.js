// ON Later's only script. Forms marked data-later-confirm ask first, in the
// app's own dialog. Without JavaScript the form simply submits.
"use strict";

(function () {
	// A broken site icon is swapped for its letter badge. "error" doesn't
	// bubble, so listen in the capture phase.
	document.addEventListener("error", function (e) {
		var img = e.target;
		if (!(img instanceof HTMLImageElement) || !img.classList.contains("later-favicon")) return;
		var badge = img.nextElementSibling;
		img.remove();
		if (badge) badge.hidden = false;
	}, true);
	// An icon can fail before this deferred script registers the listener
	// (a cached bare 404, say), so also sweep the ones already broken.
	document.querySelectorAll("img.later-favicon").forEach(function (img) {
		if (!(img.complete && img.naturalWidth === 0)) return;
		var badge = img.nextElementSibling;
		img.remove();
		if (badge) badge.hidden = false;
	});

	// The bookmarklet's address needs this site's origin, which only the
	// browser knows for sure (proxies, ports). Clicking it here would be
	// blocked by our own CSP, so a click just explains what to do.
	document.querySelectorAll("[data-later-bookmarklet]").forEach(function (a) {
		a.href = "javascript:(function(){window.open('" + location.origin +
			"/later/save?url='+encodeURIComponent(location.href),'onlater','popup,width=480,height=360');})();";
		a.addEventListener("click", function (e) {
			e.preventDefault();
			a.title = "Drag this to your bookmarks bar";
		});
	});
	document.querySelectorAll("[data-later-close]").forEach(function (b) {
		b.addEventListener("click", function () { window.close(); });
	});
	if (document.querySelector("[data-later-autoclose]")) {
		setTimeout(function () { window.close(); }, 1500);
	}

	// confirmThen asks message in the app's dialog and calls onOK on OK.
	function confirmThen(message, onOK) {
		var dialog = document.getElementById("later-confirm-dialog");
		document.getElementById("later-confirm-message").textContent = message;

		// Listeners are tied to this one opening of the dialog, so a cancelled
		// confirmation can never fire later (the bug reader.js documents).
		var controller = new AbortController();
		dialog.addEventListener("close", function () { controller.abort(); }, { once: true });
		document.getElementById("later-confirm-ok").addEventListener("click", function () {
			dialog.close();
			onOK();
		}, { signal: controller.signal });
		document.getElementById("later-confirm-cancel").addEventListener("click", function () {
			dialog.close();
		}, { signal: controller.signal });
		dialog.showModal();
	}

	document.addEventListener("submit", function (e) {
		var form = e.target;
		if (!(form instanceof HTMLFormElement) || !form.dataset.laterConfirm) return;
		if (form.dataset.laterConfirmed === "1") return;

		var dialog = document.getElementById("later-confirm-dialog");
		if (!dialog || typeof dialog.showModal !== "function") return;

		e.preventDefault();
		confirmThen(form.dataset.laterConfirm, function () {
			form.dataset.laterConfirmed = "1";
			form.requestSubmit();
		});
	});

	// hx-confirm questions use the same dialog as data-later-confirm forms
	// (the pattern reader.js uses for its own dialog).
	document.addEventListener("htmx:confirm", function (e) {
		if (!e.detail.question) return;
		var dialog = document.getElementById("later-confirm-dialog");
		if (!dialog || typeof dialog.showModal !== "function") return; // htmx falls back to window.confirm
		e.preventDefault();
		confirmThen(e.detail.question, function () { e.detail.issueRequest(true); });
	});

	// Aa settings: apply at once, save in the background. Without JS the
	// forms post and redirect back.
	function updatePrefButtons(reader) {
		var cls = Array.from(reader.classList);
		function current(field) {
			var prefix = "later-" + field + "-";
			var c = cls.find(function (x) { return x.indexOf(prefix) === 0; });
			return c ? c.slice(prefix.length) : "";
		}
		reader.querySelectorAll("form[data-later-pref]").forEach(function (f) {
			var button = f.querySelector("button");
			["font", "width"].forEach(function (field) {
				var input = f.querySelector('input[name="' + field + '"]');
				if (input && button) button.setAttribute("aria-pressed", String(input.value === current(field)));
			});
			var size = f.querySelector('input[name="size"]');
			if (size && button) {
				var n = parseInt(current("size"), 10);
				var up = button.dataset.laterSize === "up";
				var target = up ? n + 1 : n - 1;
				var ok = target >= 1 && target <= 5;
				size.value = ok ? String(target) : "";
				button.disabled = !ok;
			}
		});
	}

	document.addEventListener("submit", function (e) {
		var form = e.target;
		if (!(form instanceof HTMLFormElement) || !form.hasAttribute("data-later-pref")) return;
		var reader = document.getElementById("later-reader");
		if (!reader) return;
		e.preventDefault();
		var data = new FormData(form);
		var body = new URLSearchParams(data);
		fetch(form.action, { method: "POST", body: body, headers: { "X-Later-Async": "1" }, credentials: "same-origin" })
			.then(function (res) { if (!res.ok) throw new Error(String(res.status)); })
			.catch(function () { form.submit(); }); // fall back to the plain post
		["font", "size", "width"].forEach(function (field) {
			var v = data.get(field);
			if (v === null) return;
			Array.from(reader.classList).forEach(function (c) {
				if (c.indexOf("later-" + field + "-") === 0) reader.classList.remove(c);
			});
			reader.classList.add("later-" + field + "-" + v);
		});
		updatePrefButtons(reader);
	});

	// A highlight link in the Notes panel scrolls to the passage; on a
	// narrow screen the panel would cover it, so close the panel too.
	document.addEventListener("click", function (e) {
		var link = e.target instanceof Element && e.target.closest(".later-notes-quote[href]");
		if (!link || !window.matchMedia("(max-width: 640px)").matches) return;
		var open = document.getElementById("later-notes-open");
		if (open) open.checked = false;
	});

	// "Saved" shouldn't linger over text that has changed since.
	document.addEventListener("input", function (e) {
		var t = e.target;
		if (!(t instanceof HTMLTextAreaElement)) return;
		if (t.id !== "later-note-panel" && t.id !== "later-note-end") return;
		var status = document.getElementById("later-note-status-" + t.id.slice("later-note-".length));
		if (status) status.textContent = "";
	});
})();

	// Reading progress: where you are, saved quietly, restored on return.
	(function () {
		var reader = document.getElementById("later-reader");
		if (!reader || !reader.hasAttribute("data-progress")) return;
		var id = reader.dataset.articleId;
		var minutes = parseInt(reader.dataset.minutes, 10) || 1;
		var bar = document.querySelector("[data-later-progress]");
		var left = document.querySelector("[data-later-left]");
		var saved = parseFloat(reader.dataset.progress) || 0;
		var timer = null;
		var touched = false; // the reader has scrolled by hand

		// The CSRF token HTMX sends is also the one a form field accepts.
		function csrfToken() {
			try {
				return JSON.parse(document.body.getAttribute("hx-headers"))["X-CSRF-Token"] || "";
			} catch (e) {
				return "";
			}
		}
		function maxScroll() {
			return document.documentElement.scrollHeight - window.innerHeight;
		}
		function current() {
			var max = maxScroll();
			return max > 0 ? Math.min(1, Math.max(0, window.scrollY / max)) : 1;
		}
		function show(p) {
			if (bar) bar.value = p;
			if (left) left.textContent = p >= 0.98 ? "Finished" : Math.max(1, Math.ceil(minutes * (1 - p))) + " min left";
		}
		function body(p) {
			var b = new URLSearchParams();
			b.set("csrf_token", csrfToken());
			b.set("progress", p.toFixed(4));
			return b;
		}
		function save() {
			// A restore scroll is not the reader moving; never save before they have.
			if (!touched) return;
			var p = current();
			if (Math.abs(p - saved) < 0.01) return;
			saved = p;
			fetch("/later/a/" + id + "/progress", { method: "POST", body: body(p), credentials: "same-origin", keepalive: true })
				.catch(function () {}); // the next scroll or pagehide retries
		}
		function restore() {
			if (saved > 0.02 && saved < 0.98) window.scrollTo(0, saved * maxScroll());
			show(current());
		}

		// Restore now, and again once images have loaded and moved things,
		// unless the reader has already taken over the scrolling.
		restore();
		window.addEventListener("load", function () { if (!touched) restore(); });
		["wheel", "touchstart", "keydown", "mousedown", "pointerdown"].forEach(function (name) {
			window.addEventListener(name, function () { touched = true; }, { passive: true });
		});

		window.addEventListener("scroll", function () {
			show(current());
			clearTimeout(timer);
			timer = setTimeout(save, 2000);
		}, { passive: true });
		function flush() {
			var p = current();
			if (touched && Math.abs(p - saved) >= 0.01) navigator.sendBeacon("/later/a/" + id + "/progress", body(p));
		}
		window.addEventListener("pagehide", flush);
		document.addEventListener("visibilitychange", function () {
			if (document.visibilityState === "hidden") flush();
		});
	})();


	// Margin comments (spec: "comments in the right margin on wide
	// screens"): when the window has room beside the column, each comment
	// sits level with its highlight, pushed down if the one above runs long.
	// Otherwise CSS hides the margin and the 💬 marker shows instead.
	(function () {
		var reader = document.getElementById("later-reader");
		var article = reader && reader.querySelector(".later-article");
		if (!article) return;
		function layout() {
			var margin = document.getElementById("later-margin");
			if (!margin) return;
			var rem = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
			var room = document.documentElement.clientWidth - article.getBoundingClientRect().right;
			var fits = room >= 17 * rem; // 14rem notes + 2rem gap + 1rem edge
			reader.classList.toggle("later-has-margin", fits);
			if (!fits) return;
			var top0 = article.getBoundingClientRect().top;
			var next = 0;
			margin.querySelectorAll(".later-margin-note").forEach(function (n) {
				var mark = document.getElementById("later-h-" + n.dataset.laterHlOpen);
				n.hidden = !mark;
				if (!mark) return;
				var top = Math.max(mark.getBoundingClientRect().top - top0, next);
				n.style.top = top + "px";
				next = top + n.offsetHeight + 8;
			});
		}
		layout();
		window.addEventListener("load", layout);
		window.addEventListener("resize", layout);
		document.body.addEventListener("htmx:afterSettle", layout);
		// Aa changes and late images reflow the column.
		if (typeof ResizeObserver === "function") new ResizeObserver(layout).observe(article);
	})();
