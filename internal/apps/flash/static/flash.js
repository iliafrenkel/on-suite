// internal/apps/flash/static/flash.js
//
// ON Flash's only script, loaded by every Flash page. Each feature is a
// small, independent piece keyed off elements that only exist on the page
// that needs them, so it is a no-op everywhere else (UI overhaul spec §1.6).
// Everything here is progressive enhancement: every page works without it.
//
// Review keyboard shortcuts: Space reveals the answer, 1-4 grade it
// (Again/Hard/Good/Easy), U undoes the last grade. Mirrors
// internal/apps/reader/static/reader.js's press()/keydown pattern.
// Card viewer shortcuts (U2): Space flips, ← → previous/next, E edits.
(function () {
	"use strict";

	function isTyping(el) {
		if (!el) return false;
		var tag = el.tagName;
		return tag === "INPUT" || tag === "TEXTAREA" || el.isContentEditable;
	}

	// press clicks the first element matching selector and reports whether
	// there was one, so a shortcut only swallows its key where it applies.
	function press(selector) {
		var el = document.querySelector(selector);
		if (!el) return false;
		el.click();
		return true;
	}

	function reveal() {
		var answer = document.querySelector(".flash-review-answer");
		if (!answer) return false;
		answer.hidden = false;
		return true;
	}

	// toggleFlip turns the opened card over. The flip itself is CSS on the
	// checkbox's :checked state; this only changes the state.
	function toggleFlip() {
		var flip = document.querySelector(".flash-viewer .flash-flip");
		if (!flip) return false;
		flip.checked = !flip.checked;
		return true;
	}

	document.addEventListener("keydown", function (e) {
		if (e.metaKey || e.ctrlKey || e.altKey) return;
		// The flip checkbox is an <input>, but Space on it should still flip
		// — natively, which is why Space returns here before isTyping and
		// before our own Space handling (doing both would flip twice). Only
		// Space is special-cased: any other key (e.g. ArrowLeft/ArrowRight/E)
		// must still reach the switch below even while focus sits on the
		// checkbox, or those shortcuts would stop working after a click.
		if (e.key === " " && e.target.classList && e.target.classList.contains("flash-flip")) return;
		if (isTyping(e.target)) return;

		var handled = false;
		switch (e.key) {
			case " ":
				handled = toggleFlip() || reveal();
				break;
			case "1":
				handled = press(".flash-grade-again");
				break;
			case "2":
				handled = press(".flash-grade-hard");
				break;
			case "3":
				handled = press(".flash-grade-good");
				break;
			case "4":
				handled = press(".flash-grade-easy");
				break;
			case "u":
			case "U":
				handled = press(".flash-undo-btn");
				break;
			case "ArrowLeft":
				handled = press(".flash-card-prev");
				break;
			case "ArrowRight":
				handled = press(".flash-card-next");
				break;
			case "e":
			case "E":
				handled = press(".flash-card-edit");
				break;
		}
		if (handled) e.preventDefault();
	});

	document.addEventListener("click", function (e) {
		if (e.target.closest && e.target.closest(".flash-reveal-btn")) reveal();
	});
})();
