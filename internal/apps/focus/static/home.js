// ON Focus's home and History page script. Forms marked data-focus-confirm ask first,
// in the app's own dialog (later.js's pattern); without JavaScript the form
// simply submits. Tiles can also be dragged to reorder. It also shows the
// resume banner (and records a session that has ended) and asks for notification permission on ▶.
"use strict";

(function () {
	// confirmThen asks message in the app's dialog, with okLabel on the OK
	// button, and calls onOK on OK.
	function confirmThen(message, okLabel, onOK) {
		var dialog = document.getElementById("focus-confirm-dialog");
		document.getElementById("focus-confirm-message").textContent = message;
		var ok = document.getElementById("focus-confirm-ok");
		ok.textContent = okLabel;

		// Listeners are tied to this one opening of the dialog, so a
		// cancelled confirmation can never fire later (the bug reader.js
		// documents). OK and Cancel let go of every listener at once rather
		// than waiting for "close", which can be held back (in a hidden tab,
		// say) and would otherwise reach the next opening's listeners.
		// "close" itself only covers Esc, and ignores one that arrives while
		// the dialog is open again (it belongs to an earlier opening).
		var controller = new AbortController();
		dialog.addEventListener("close", function () {
			if (dialog.open) return;
			controller.abort();
		}, { signal: controller.signal });
		ok.addEventListener("click", function () {
			controller.abort();
			dialog.close();
			onOK();
		}, { signal: controller.signal });
		document.getElementById("focus-confirm-cancel").addEventListener("click", function () {
			controller.abort();
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
		confirmThen(form.dataset.focusConfirm, form.dataset.focusConfirmOk || "Delete", function () {
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

	// Ask for notification permission on the first ▶ (spec: "Notification
	// permission": on a Start click, never on page load). The prompt would
	// vanish if the page navigated away under it, so wait for the answer.
	document.addEventListener("click", function (e) {
		if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
		var play = e.target.closest && e.target.closest(".focus-play");
		if (!play || !("Notification" in window) || Notification.permission !== "default") return;
		e.preventDefault();
		function go() { window.location.assign(play.href); }
		Notification.requestPermission().then(go, go);
	});

	// A page restored from the back/forward cache still holds the session
	// the banner was built from, and would resume or clear that stale copy
	// over whatever is in localStorage now. Reload so it starts from storage.
	window.addEventListener("pageshow", function (e) {
		if (e.persisted) window.location.reload();
	});

	// The resume banner (spec: "Resume"): a session stored in this browser,
	// read from the state the running page keeps. While it runs: a link
	// back to it, and End (#546 — it also clears a session whose timer was
	// deleted). Once it has ended: recorded on the spot, with Retry if that
	// fails.
	var S = window.OnFocus && window.OnFocus.session;
	var R = window.OnFocus && window.OnFocus.record;
	var banner = document.querySelector("[data-focus-resume]");
	var stored = S && R && banner ? S.load(Number(banner.dataset.userId)) : null;
	var bannerTicker = 0;
	var text, link, endButton, retryButton;
	if (stored) {
		var timerId = Number(stored.timerId);
		if (!Number.isFinite(timerId) || timerId <= 0 || Math.floor(timerId) !== timerId) {
			S.clear();
			stored = null;
		} else {
			text = banner.querySelector("[data-focus-resume-text]");
			endButton = banner.querySelector("[data-focus-resume-end]");
			retryButton = banner.querySelector("[data-focus-resume-retry]");
			link = document.createElement("a");
			link.href = "/focus/run/" + timerId;
			banner.classList.add("swatch-c-" + stored.color);
			banner.hidden = false;
			endButton.addEventListener("click", function () {
				confirmThen("End " + stored.timerName + "?", "End session", function () {
					// The session ran out while the dialog was open and the
					// ticker has already recorded it: the banner shows that.
					if (stored.finished) return;
					var now = Date.now();
					S.advance(stored, now); // a phase that ran out still counts
					S.end(stored, now);
					S.save(stored);
					updateBanner();
				});
			});
			retryButton.addEventListener("click", recordStored);
			bannerTicker = window.setInterval(updateBanner, 1000);
			updateBanner();
		}
	}

	function updateBanner() {
		var now = Date.now();
		S.advance(stored, now);
		if (stored.finished) {
			window.clearInterval(bannerTicker);
			endButton.hidden = true;
			recordStored();
			return;
		}
		endButton.hidden = false;
		if (link.parentNode !== text) {
			text.textContent = "";
			text.appendChild(link);
		}
		if (stored.waiting) {
			var up = S.upcoming(stored);
			link.textContent = stored.timerName + " — ready for " + (up.kind === "focus" ? "round " + up.round : "a break");
			return;
		}
		var left = S.clock(S.remaining(stored, now)) + " left";
		link.textContent = "Resume " + stored.timerName + " — " + (stored.pausedAt !== null ? "paused, " + left : left);
	}

	// recordStored records the ended session (spec: "Resume": "recorded on
	// the spot"), then refreshes the today strip so it includes it.
	function recordStored() {
		retryButton.hidden = true;
		var summary = stored.timerName + (stored.completed ? " finished" : " ended") + " — " +
			S.focused(S.focusSeconds(stored, stored.endedAt)) + " focused.";
		if (!R.eligible(stored)) {
			S.clear();
			text.textContent = summary;
			return;
		}
		text.textContent = summary + " Saving…";
		R.send(stored).then(function (result) {
			if (result === "failed") {
				text.textContent = summary + " Couldn't save this session.";
				retryButton.hidden = false;
				return;
			}
			S.clear();
			text.textContent = result === "saved" ? summary + " Saved to your history." : summary;
			if (result === "saved" && window.htmx) {
				htmx.ajax("GET", "/focus/today", { target: "[data-focus-today]", swap: "innerHTML" });
			}
		});
	}
})();
