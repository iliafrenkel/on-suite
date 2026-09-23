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
(function () {
	"use strict";

	function isTyping(el) {
		if (!el) return false;
		var tag = el.tagName;
		return tag === "INPUT" || tag === "TEXTAREA" || el.isContentEditable;
	}

	function press(selector) {
		var el = document.querySelector(selector);
		if (el) el.click();
	}

	function reveal() {
		var answer = document.querySelector(".flash-review-answer");
		if (answer) answer.hidden = false;
	}

	document.addEventListener("keydown", function (e) {
		if (e.metaKey || e.ctrlKey || e.altKey) return;
		if (isTyping(e.target)) return;

		switch (e.key) {
			case " ":
				reveal();
				break;
			case "1":
				press(".flash-grade-again");
				break;
			case "2":
				press(".flash-grade-hard");
				break;
			case "3":
				press(".flash-grade-good");
				break;
			case "4":
				press(".flash-grade-easy");
				break;
			case "u":
			case "U":
				press(".flash-undo-btn");
				break;
			default:
				return;
		}
		e.preventDefault();
	});

	document.addEventListener("click", function (e) {
		if (e.target.closest(".flash-reveal-btn")) reveal();
	});
})();
