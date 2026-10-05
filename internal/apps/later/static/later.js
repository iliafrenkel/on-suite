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

	document.addEventListener("submit", function (e) {
		var form = e.target;
		if (!(form instanceof HTMLFormElement) || !form.dataset.laterConfirm) return;
		if (form.dataset.laterConfirmed === "1") return;

		var dialog = document.getElementById("later-confirm-dialog");
		if (!dialog || typeof dialog.showModal !== "function") return;

		e.preventDefault();
		document.getElementById("later-confirm-message").textContent = form.dataset.laterConfirm;

		// Listeners are tied to this one opening of the dialog, so a cancelled
		// confirmation can never fire later (the bug reader.js documents).
		var controller = new AbortController();
		dialog.addEventListener("close", function () { controller.abort(); }, { once: true });
		document.getElementById("later-confirm-ok").addEventListener("click", function () {
			form.dataset.laterConfirmed = "1";
			dialog.close();
			form.requestSubmit();
		}, { signal: controller.signal });
		document.getElementById("later-confirm-cancel").addEventListener("click", function () {
			dialog.close();
		}, { signal: controller.signal });
		dialog.showModal();
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
				var up = button.getAttribute("aria-label") === "Larger text";
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
})();
