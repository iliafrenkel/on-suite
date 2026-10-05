// ON Later's only script. Forms marked data-later-confirm ask first, in the
// app's own dialog. Without JavaScript the form simply submits.
"use strict";

(function () {
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
})();
