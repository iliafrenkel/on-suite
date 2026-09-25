// internal/apps/flash/static/flash.js
//
// ON Flash's only script, loaded by every Flash page. Each feature is a
// small, independent piece keyed off elements that only exist on the page
// that needs them, so it is a no-op everywhere else (UI overhaul spec §1.6).
// Everything here is progressive enhancement: every page works without it.
//
// Review shortcuts: Space shows the answer, 1-4 grade it (Forgot/Hard/Got it/Easy) once it shows, U undoes the last grade, Esc stops.
// Card viewer shortcuts (U2): Space flips, ← → previous/next, E edits.
// Card editor (U3): Make blank, tag pills, and drag-and-drop for media.
// Import (U6): Copy the prompt.
(function () {
	"use strict";

	function isTyping(el) {
		if (!el) return false;
		var tag = el.tagName;
		// Exempting every checkbox/radio (not just .flash-flip) is deliberate
		// but broader than strictly needed: it's only correct because the
		// flip checkbox is the sole checkbox/radio ever coexisting with the
		// review/viewer shortcut keys in the DOM (bb757f2, #337). If a future
		// feature adds another checkbox or radio alongside those shortcuts,
		// narrow this to el.classList.contains("flash-flip") instead.
		if (tag === "INPUT" && (el.type === "checkbox" || el.type === "radio")) return false;
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

	// toggleFlip turns the opened card over. The flip itself is CSS on the
	// checkbox's :checked state; this only changes the state.
	function toggleFlip() {
		var flip = document.querySelector(".flash-viewer .flash-flip");
		if (!flip) return false;
		flip.checked = !flip.checked;
		return true;
	}

	// flipReview shows the review card's answer. It never flips back: once
	// the answer is showing, Space has nothing more to do.
	function flipReview() {
		var flip = document.querySelector("#review-card .flash-flip");
		if (!flip) return false;
		flip.checked = true;
		return true;
	}

	// grade presses a grade button, but only once the answer is showing —
	// the buttons are hidden until then, and a stray key must not grade a
	// card nobody has looked at.
	function grade(selector) {
		var flip = document.querySelector("#review-card .flash-flip");
		if (!flip || !flip.checked) return false;
		return press(selector);
	}

	// ---- Card editor (U3) -------------------------------------------------

	// nextClozeNumber is one more than the highest {{cN::…}} already in text.
	function nextClozeNumber(text) {
		var max = 0;
		var re = /\{\{c(\d+)::/g;
		var m;
		while ((m = re.exec(text)) !== null) {
			var n = parseInt(m[1], 10);
			if (n > max) max = n;
		}
		return max + 1;
	}

	// makeBlank wraps the textarea's selection (or a placeholder word) in
	// the next cloze marker and selects the word, ready to overtype.
	function makeBlank(btn) {
		var ta = document.getElementById(btn.getAttribute("data-target"));
		if (!ta) return;
		var start = ta.selectionStart, end = ta.selectionEnd;
		var word = ta.value.slice(start, end) || "answer";
		var open = "{{c" + nextClozeNumber(ta.value) + "::";
		ta.value = ta.value.slice(0, start) + open + word + "}}" + ta.value.slice(end);
		ta.focus();
		ta.setSelectionRange(start + open.length, start + open.length + word.length);
		ta.dispatchEvent(new Event("input", { bubbles: true }));
	}

	// initTagInput turns the comma-separated tags field into pills. The
	// original input stays in the form (as type=hidden) and keeps the same
	// comma-separated value the server already parses.
	function initTagInput(input) {
		var names = input.value.split(",").map(function (s) { return s.trim(); }).filter(Boolean);
		var box = document.createElement("div");
		box.className = "flash-tag-editor";
		var entry = document.createElement("input");
		entry.type = "text";
		entry.className = "flash-tag-entry";
		entry.placeholder = names.length ? "" : (input.placeholder || "Add a tag");
		entry.setAttribute("aria-label", "Add a tag");
		if (input.id) {
			// Keep the field's <label for=…> pointing at something focusable.
			entry.id = input.id;
			input.id = input.id + "-value";
		}

		function sync() {
			input.value = names.join(", ");
		}
		function render() {
			while (box.firstChild !== entry && box.firstChild) box.removeChild(box.firstChild);
			names.forEach(function (name, i) {
				var pill = document.createElement("span");
				pill.className = "flash-pill flash-tag-pill";
				pill.textContent = name;
				var x = document.createElement("button");
				x.type = "button";
				x.className = "flash-tag-remove";
				x.setAttribute("aria-label", "Remove tag " + name);
				x.textContent = "×";
				// A mousedown on this button moves focus off entry before the
				// click fires, which would otherwise run entry's blur handler
				// first and commit any leftover typed text as a new tag — the
				// removal click then lands on a re-rendered pill list and does
				// nothing (#326). preventDefault() keeps focus on entry so
				// blur never fires, while a real click (mouse or, for
				// keyboard, Enter/Space) still reaches the listener below.
				x.addEventListener("mousedown", function (e) {
					e.preventDefault();
				});
				x.addEventListener("click", function () {
					names.splice(i, 1);
					sync();
					render();
					entry.focus();
				});
				pill.appendChild(x);
				box.insertBefore(pill, entry);
			});
		}
		function add(raw) {
			raw.split(",").forEach(function (part) {
				var name = part.trim();
				if (name && names.indexOf(name) === -1) names.push(name);
			});
			entry.value = "";
			sync();
			render();
		}

		entry.addEventListener("keydown", function (e) {
			if (e.key === "Enter" || e.key === ",") {
				e.preventDefault();
				add(entry.value);
			} else if (e.key === "Backspace" && entry.value === "" && names.length) {
				names.pop();
				sync();
				render();
			}
		});
		entry.addEventListener("blur", function () {
			if (entry.value.trim()) add(entry.value);
		});
		if (input.form) {
			input.form.addEventListener("submit", function () {
				if (entry.value.trim()) add(entry.value);
			});
		}

		input.type = "hidden";
		input.parentNode.insertBefore(box, input);
		box.appendChild(entry);
		render();
	}

	// initDropZone lets a file be dropped onto the zone's label, and shows
	// the chosen file's name (the CSP's img-src 'self' rules out a blob:
	// thumbnail). A newly chosen file always wins over that kind's Remove
	// checkbox on the server (#328), so unticking it here too keeps what
	// the checkbox shows in sync with what will actually happen — a
	// convenience only, never relied on for correctness (the server enforces
	// the rule regardless of JS).
	function initDropZone(zone) {
		var input = zone.querySelector("input[type=file]");
		var label = zone.querySelector(".flash-drop-file");
		var field = zone.closest(".flash-drop-field");
		var removeBox = field && field.querySelector(".flash-drop-remove input[type=checkbox]");
		if (!input) return;
		function show() {
			if (label) label.textContent = input.files.length ? input.files[0].name : "";
			zone.classList.toggle("flash-drop-chosen", input.files.length > 0);
			if (removeBox && input.files.length) removeBox.checked = false;
		}
		zone.addEventListener("dragover", function (e) {
			e.preventDefault();
			zone.classList.add("flash-drop-over");
		});
		zone.addEventListener("dragleave", function () {
			zone.classList.remove("flash-drop-over");
		});
		zone.addEventListener("drop", function (e) {
			e.preventDefault();
			zone.classList.remove("flash-drop-over");
			if (e.dataTransfer && e.dataTransfer.files.length) {
				input.files = e.dataTransfer.files;
				show();
			}
		});
		input.addEventListener("change", show);

		// Mirror of the handler above (#328's other direction, per user
		// decision): ticking Remove clears any file already chosen for this
		// same kind, so the drop zone can't show a chosen file the server is
		// about to ignore in favour of the removal (a new file always wins
		// over Remove server-side — see saveCardUploads — so once Remove is
		// ticked with no file chosen, that stays true). A convenience only,
		// scoped to this drop zone via closest(".flash-drop-field") so it
		// never reaches the other kind's checkbox or input.
		if (removeBox) {
			removeBox.addEventListener("change", function () {
				if (!removeBox.checked) return;
				input.value = "";
				show();
			});
		}
	}

	// enhance wires every not-yet-enhanced editor element under root. It is
	// idempotent, so running it on each htmx:load is safe.
	function enhance(root) {
		function each(selector, fn) {
			var list = root.querySelectorAll(selector);
			for (var i = 0; i < list.length; i++) {
				if (list[i].getAttribute("data-enhanced")) continue;
				list[i].setAttribute("data-enhanced", "1");
				fn(list[i]);
			}
		}
		each(".flash-make-blank", function (btn) { btn.hidden = false; });
		each("input[data-tag-input]", initTagInput);
		each("label[data-drop]", initDropZone);
		each(".flash-copy-prompt", function (btn) { btn.hidden = false; });
	}

	document.addEventListener("click", function (e) {
		var btn = e.target.closest && e.target.closest(".flash-make-blank");
		if (btn) makeBlank(btn);
	});

	// Copy the import prompt to the clipboard, confirming on the button.
	// navigator.clipboard needs a secure context (https or localhost); where
	// it is missing, open the prompt box and select its text instead.
	document.addEventListener("click", function (e) {
		var btn = e.target.closest && e.target.closest(".flash-copy-prompt");
		if (!btn) return;
		var box = document.getElementById(btn.getAttribute("data-copy-target"));
		if (!box) return;
		var label = btn.querySelector(".flash-copy-label") || btn;
		function done(text) {
			label.textContent = text;
			// Rapid repeated clicks on the same button would otherwise start
			// overlapping timeouts, so an earlier one could revert the label
			// mid-flicker after a later click already changed it (#353).
			// Stash the timeout id on the button itself, scoped per button.
			if (btn._flashCopyRevert) clearTimeout(btn._flashCopyRevert);
			btn._flashCopyRevert = setTimeout(function () { label.textContent = "Copy the prompt"; }, 2000);
		}
		function selectIt() {
			var details = box.closest("details");
			if (details) details.open = true;
			box.focus();
			box.select();
			done("Press Ctrl+C to copy");
		}
		if (navigator.clipboard && navigator.clipboard.writeText) {
			navigator.clipboard.writeText(box.value).then(function () { done("Copied!"); }, selectIt);
		} else {
			selectIt();
		}
	});
	document.addEventListener("htmx:load", function (e) { enhance(e.target); });
	enhance(document);

	var mediaKeys = new Set([" ", "Enter", "ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End"]);
	document.addEventListener("keydown", function (e) {
		if (e.metaKey || e.ctrlKey || e.altKey) return;
		// A focused <audio>/<video> element's own controls (Space/Enter for
		// play/pause, arrows and Home/End to seek) must win over our
		// shortcuts, or Space both reveals the review answer and
		// preventDefault()s the player's native toggle. Other keys (grades,
		// U to undo) still work while the player has focus.
		if (e.target.closest && e.target.closest("audio, video") && mediaKeys.has(e.key)) return;
		// The flip checkbox is an <input>, but Space on it should still flip
		// — natively, which is why Space returns here before isTyping and
		// before our own Space handling (doing both would flip twice). Only
		// Space is special-cased: any other key (e.g. ArrowLeft/ArrowRight/E)
		// must still reach the switch below even while focus sits on the
		// checkbox, or those shortcuts would stop working after a click.
		if (e.key === " " && e.target.classList && e.target.classList.contains("flash-flip")) {
			if (e.target.checked) e.preventDefault();
			return;
		}
		if (isTyping(e.target)) return;

		var handled = false;
		switch (e.key) {
			case " ":
				handled = flipReview() || toggleFlip();
				break;
			case "1":
				handled = grade(".flash-grade-again");
				break;
			case "2":
				handled = grade(".flash-grade-hard");
				break;
			case "3":
				handled = grade(".flash-grade-good");
				break;
			case "4":
				handled = grade(".flash-grade-easy");
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
			case "Escape":
				handled = press(".flash-review-stop");
				break;
		}
		if (handled) e.preventDefault();
	});

})();
