// ON Books' script: the delete confirmation, and (Task 8) resizable panes,
// keyboard shortcuts and the open book's row highlight. Vanilla and
// CSP-clean; the sections mirror reader.js's (apps never share app
// scripts).
(function () {
	"use strict";

	// --- Confirm dialog, replacing window.confirm for hx-confirm ----------
	//
	// Mirrors reader.js's, including the #551 fix: the OK listener belongs
	// to one opening through an AbortController and goes as soon as OK or
	// Cancel is pressed — not on "close", which Chrome can hold back in a
	// hidden tab, so a declined delete would otherwise fire with the next.
	var confirmController = null;
	document.addEventListener("htmx:confirm", function (e) {
		var panes = document.getElementById("books-panes");
		if (!panes || !e.target || !panes.contains(e.target)) return;
		var msg = e.target.getAttribute("hx-confirm");
		if (!msg) return;
		var dialog = document.getElementById("books-confirm-dialog");
		if (!dialog || typeof dialog.showModal !== "function") return; // htmx falls back to window.confirm

		e.preventDefault();
		document.getElementById("books-confirm-message").textContent = msg;
		var ok = document.getElementById("books-confirm-ok");
		var cancel = dialog.querySelector(".books-dialog-cancel");

		if (confirmController) confirmController.abort();
		var controller = confirmController = new AbortController();
		dialog.addEventListener("close", function () {
			if (dialog.open) return;
			controller.abort();
		}, { signal: controller.signal });
		cancel.addEventListener("click", function () {
			controller.abort();
			dialog.close();
		}, { signal: controller.signal });
		ok.addEventListener("click", function () {
			controller.abort();
			dialog.close();
			// true: skip the confirm gate, or htmx would ask again natively.
			e.detail.issueRequest(true);
		}, { signal: controller.signal });
		dialog.showModal();
	});
})();
