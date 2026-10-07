// ON Focus's home-page script. Forms marked data-focus-confirm ask first,
// in the app's own dialog (later.js's pattern); without JavaScript the form
// simply submits. Tiles can also be dragged to reorder.
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

	// Drag a tile to reorder. The new order is saved in the background; if
	// that fails, a notice says so (htmx-notices.js, PATTERNS.md).
	var tiles = document.querySelector("[data-focus-tiles]");
	if (tiles && window.htmx) {
		var dragged = null;
		var before = "";

		function order() {
			return Array.prototype.map.call(tiles.querySelectorAll(".focus-tile"), function (t) {
				return t.dataset.id;
			}).join(",");
		}

		tiles.addEventListener("dragstart", function (e) {
			var tile = e.target.closest && e.target.closest(".focus-tile");
			if (!tile) return;
			dragged = tile;
			before = order();
			tile.classList.add("focus-tile-dragging");
			e.dataTransfer.effectAllowed = "move";
			e.dataTransfer.setData("text/plain", tile.dataset.id);
		});

		tiles.addEventListener("dragover", function (e) {
			if (!dragged) return;
			e.preventDefault();
			var over = e.target.closest && e.target.closest(".focus-tile");
			if (!over || over === dragged) return;
			var box = over.getBoundingClientRect();
			var after = e.clientX - box.left > box.width / 2;
			tiles.insertBefore(dragged, after ? over.nextSibling : over);
		});

		tiles.addEventListener("drop", function (e) {
			if (dragged) e.preventDefault();
		});

		tiles.addEventListener("dragend", function () {
			if (!dragged) return;
			dragged.classList.remove("focus-tile-dragging");
			dragged = null;
			var now = order();
			if (now === before) return;
			htmx.ajax("POST", "/focus/timers/order", {
				source: document.body,
				swap: "none",
				values: { ids: now }
			});
		});

		function isOrder(evt) {
			var info = evt.detail && evt.detail.pathInfo;
			return info && info.requestPath === "/focus/timers/order";
		}
		document.body.addEventListener("htmx:responseError", function (evt) {
			if (!isOrder(evt)) return;
			OnSuite.notices.show(tiles, "focus-order-error", "Couldn't save the new order. Reload the page and try again.");
		});
		document.body.addEventListener("htmx:sendError", function (evt) {
			if (!isOrder(evt)) return;
			OnSuite.notices.show(tiles, "focus-order-error", "Couldn't save the new order — you seem to be offline.");
		});
		document.body.addEventListener("htmx:afterRequest", function (evt) {
			if (isOrder(evt) && evt.detail.successful) OnSuite.notices.clear("focus-order-error");
		});
	}
})();
