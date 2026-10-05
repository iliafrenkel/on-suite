// ON Later's highlight unit, browser side (with highlight.go and
// highlight_render.go). It turns a selection inside #later-body into
// code-point offsets: a Range from the body's start to the selection
// counts exactly the text nodes ContentText concatenates, and Array.from
// counts code points the way Go's []rune does (spec: "Offsets are code
// points").
"use strict";

(function () {
	var form = document.getElementById("later-hl-new");
	if (!form) return;
	var commentBox = form.querySelector("[data-later-hl-comment]");
	var comment = form.querySelector('textarea[name="comment"]');
	var submit = form.querySelector("[data-later-hl-submit]");
	var withComment = form.querySelector("[data-later-hl-with-comment]");
	var error = form.querySelector("[data-later-hl-error]");
	var pressedAt = 0; // last pointerdown inside the popover
	var timer = null;

	function body() { return document.getElementById("later-body"); }

	function codePoints(root, node, offset) {
		var r = document.createRange();
		r.setStart(root, 0);
		r.setEnd(node, offset);
		return Array.from(r.toString()).length;
	}

	// selection is the current selection as {start, end, quote, rect} when
	// it lies wholly inside the article body, trimmed of white space at
	// either end; otherwise null.
	function selection() {
		var sel = window.getSelection();
		if (!sel || sel.rangeCount === 0 || sel.isCollapsed) return null;
		var range = sel.getRangeAt(0);
		var root = body();
		if (!root || !root.contains(range.startContainer) || !root.contains(range.endContainer)) return null;
		var chars = Array.from(range.toString());
		var lead = 0, trail = 0;
		while (lead < chars.length && /\s/.test(chars[lead])) lead++;
		while (trail < chars.length - lead && /\s/.test(chars[chars.length - 1 - trail])) trail++;
		if (lead + trail >= chars.length) return null;
		var start = codePoints(root, range.startContainer, range.startOffset) + lead;
		return {
			start: start,
			end: start + chars.length - lead - trail,
			quote: chars.slice(lead, chars.length - trail).join(""),
			rect: range.getBoundingClientRect()
		};
	}

	// place puts a popover just under rect (viewport coordinates), kept
	// inside the window.
	function place(el, rect) {
		var width = document.documentElement.clientWidth;
		el.style.top = (rect.bottom + window.scrollY + 8) + "px";
		el.style.left = (window.scrollX + Math.max(8, Math.min(rect.left, width - el.offsetWidth - 8))) + "px";
	}

	function open(s) {
		form.elements.start.value = String(s.start);
		form.elements.end.value = String(s.end);
		form.elements.quote.value = s.quote;
		comment.value = "";
		commentBox.hidden = true;
		withComment.hidden = false;
		submit.textContent = "Highlight";
		error.hidden = true;
		form.hidden = false;
		place(form, s.rect);
	}
	function close() { form.hidden = true; }

	document.addEventListener("selectionchange", function () {
		clearTimeout(timer);
		timer = setTimeout(function () {
			if (form.contains(document.activeElement) || Date.now() - pressedAt < 500) return;
			var s = selection();
			if (s) open(s); else close();
		}, 250);
	});
	form.addEventListener("pointerdown", function () { pressedAt = Date.now(); });
	// Keep the selection while the popover's buttons are pressed.
	form.addEventListener("mousedown", function (e) {
		if (!(e.target instanceof HTMLTextAreaElement)) e.preventDefault();
	});
	withComment.addEventListener("click", function () {
		commentBox.hidden = false;
		withComment.hidden = true;
		submit.textContent = "Save highlight";
		comment.focus();
	});
	document.addEventListener("keydown", function (e) {
		if (e.key === "Escape" && !form.hidden) close();
	});

	// After the post: close on success (the body has been redrawn), else say
	// why next to the buttons. A network failure has no 422 text.
	document.body.addEventListener("htmx:afterRequest", function (e) {
		if (e.detail.elt !== form) return;
		if (e.detail.successful) {
			close();
			var sel = window.getSelection();
			if (sel) sel.removeAllRanges();
			return;
		}
		var xhr = e.detail.xhr;
		error.textContent = (xhr && xhr.status === 422 && xhr.responseText) || "Couldn't save the highlight. Try again.";
		error.hidden = false;
	});
})();
