// ON Focus's home-page script. Forms marked data-focus-confirm ask first,
// in the app's own dialog (later.js's pattern); without JavaScript the form
// simply submits. Drag-to-reorder is added in Task 9.
"use strict";

(function () {
	// confirmThen asks message in the app's dialog and calls onOK on OK.
	function confirmThen(message, onOK) {
		var dialog = document.getElementById("focus-confirm-dialog");
		document.getElementById("focus-confirm-message").textContent = message;

		// Listeners are tied to this one opening of the dialog, so a
		// cancelled confirmation can never fire later (the bug reader.js
		// documents).
		var controller = new AbortController();
		dialog.addEventListener("close", function () { controller.abort(); }, { once: true });
		document.getElementById("focus-confirm-ok").addEventListener("click", function () {
			dialog.close();
			onOK();
		}, { signal: controller.signal });
		document.getElementById("focus-confirm-cancel").addEventListener("click", function () {
			dialog.close();
		}, { signal: controller.signal });
		dialog.showModal();
	}

	document.addEventListener("submit", function (e) {
		var form = e.target;
		if (!(form instanceof HTMLFormElement) || !form.dataset.focusConfirm) return;
		if (form.dataset.focusConfirmed === "1") return;

		var dialog = document.getElementById("focus-confirm-dialog");
		if (!dialog || typeof dialog.showModal !== "function") return;

		e.preventDefault();
		confirmThen(form.dataset.focusConfirm, function () {
			form.dataset.focusConfirmed = "1";
			form.requestSubmit();
		});
	});
})();
