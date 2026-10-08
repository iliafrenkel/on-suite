// ON Focus's running page (spec: "The runner (focus.js)", "Running page").
// The server embeds the timer and its phase list as JSON; this script runs
// it with session.js's state and time maths, keeps the state in
// localStorage on every change, draws the ring, dots and controls, and records
// the session when it ends (record.js).
"use strict";

(function () {
	var root = document.getElementById("focus-runner");
	if (!root) return;
	var S = window.OnFocus.session;
	var chimes = window.OnFocus.chimes;
	var R = window.OnFocus.record;
	var config = JSON.parse(document.getElementById("focus-run-config").textContent);

	var el = {
		main: root.querySelector("[data-focus-main]"),
		bar: root.querySelector("[data-focus-bar]"),
		digits: root.querySelector("[data-focus-digits]"),
		phase: root.querySelector("[data-focus-phase]"), // null for a single timer
		dots: root.querySelectorAll(".focus-dot"),
		controls: root.querySelector("[data-focus-controls]"),
		pause: root.querySelector("[data-focus-pause]"),
		skip: root.querySelector("[data-focus-skip]"), // null for a single timer
		restart: root.querySelector("[data-focus-restart]"),
		waiting: root.querySelector("[data-focus-waiting]"),
		next: root.querySelector("[data-focus-next]"),
		soundHint: root.querySelector("[data-focus-sound-hint]"),
		done: root.querySelector("[data-focus-done]"),
		doneText: root.querySelector("[data-focus-done-text]"),
		doneStatus: root.querySelector("[data-focus-done-status]"),
		retry: root.querySelector("[data-focus-retry]"),
		exit: root.querySelector("[data-focus-exit]"),
		fullscreen: root.querySelector("[data-focus-fullscreen]")
	};

	var s = null; // the running session, once there is one
	var ticker = 0;

	// A page restored from the back/forward cache still holds the session
	// as it was when the person left, and would write that stale copy over
	// whatever is in localStorage now. Reload so it starts from storage.
	window.addEventListener("pageshow", function (e) {
		if (e.persisted) window.location.reload();
	});

	// ---- Drawing ----------------------------------------------------------

	function render(now) {
		var p = S.current(s);
		var isBreak = p.kind !== "focus";
		var paused = s.pausedAt !== null;
		var left = S.clock(S.remaining(s, now));

		root.classList.toggle("focus-runner-break", isBreak);
		root.classList.toggle("focus-runner-paused", paused);
		root.classList.toggle("focus-runner-waiting", s.waiting);

		el.digits.textContent = left;
		el.bar.setAttribute("stroke-dashoffset", String(Math.round(1000 * (1 - S.progress(s, now)))));
		if (el.phase) {
			if (s.waiting) el.phase.textContent = isBreak ? "Break over" : "Round " + p.round + " done";
			else if (paused) el.phase.textContent = "Paused";
			else el.phase.textContent = p.label;
		}

		// Dots: filled = rounds behind us, ringed = the round in progress
		// (spec: "Dots"). A break's round is the one just finished.
		var inFocus = p.kind === "focus" && !s.waiting;
		var behind = inFocus ? p.round - 1 : p.round;
		for (var i = 0; i < el.dots.length; i++) {
			el.dots[i].classList.toggle("focus-dot-done", i < behind);
			el.dots[i].classList.toggle("focus-dot-now", inFocus && i === p.round - 1);
		}

		el.controls.hidden = s.waiting;
		el.waiting.hidden = !s.waiting;
		if (s.waiting) el.next.textContent = S.startLabel(s);
		el.pause.textContent = paused ? "Resume" : "Pause";

		if (paused) document.title = "Paused · " + s.timerName;
		else if (s.waiting) document.title = "Ready · " + s.timerName;
		else document.title = left + " · " + (isBreak ? "Break" : s.timerName);
	}

	// finish shows the Done screen and records the session (spec:
	// "Recording a session"). leaving is Exit: once the server has it, go
	// home. Exit on a session too short to keep goes home at once.
	function finish(now, leaving) {
		window.clearInterval(ticker);
		if (leaving && !R.eligible(s)) {
			S.clear();
			window.location.assign("/focus/");
			return;
		}
		el.main.hidden = true;
		el.done.hidden = false;
		el.doneText.textContent = "Done — " + S.focused(S.focusSeconds(s, now)) + " focused";
		document.title = "Done · " + s.timerName;
		record(leaving);
	}

	// record sends the ended session. It stays in localStorage until the
	// server has it, so Retry here — or the home page's banner later — can
	// try again.
	function record(leaving) {
		el.retry.hidden = true;
		if (!R.eligible(s)) {
			S.clear();
			el.doneStatus.textContent = s.keepHistory ? "Under a minute of focus — not added to your history." : "";
			return;
		}
		el.doneStatus.textContent = "Saving…";
		R.send(s).then(function (result) {
			if (result === "failed") {
				el.doneStatus.textContent = "Couldn't save this session.";
				el.retry.hidden = false;
				return;
			}
			S.clear();
			if (leaving) {
				window.location.assign("/focus/");
				return;
			}
			el.doneStatus.textContent = result === "saved" ? "Saved to your history." : "Couldn't add this session to your history.";
		});
	}
	el.retry.addEventListener("click", function () { record(false); });

	// ---- Time passing -----------------------------------------------------

	// notify shows a browser notification, only while the tab is hidden and
	// only if the person allowed it (spec: "Phase end").
	function notify(text) {
		if (!document.hidden || !("Notification" in window) || Notification.permission !== "granted") return;
		try {
			new Notification(text, { tag: "on-focus" });
		} catch (e) {
			// Some mobile browsers only notify from a service worker; chimes still play.
		}
	}

	// tick walks the session forward to now and redraws: one chime and at
	// most one notification however many phases ended since the last tick.
	function tick() {
		var now = Date.now();
		if (S.advance(s, now) > 0) {
			S.save(s);
			chimes.play(s.chime);
			notify(S.notice(s));
		}
		if (s.finished) finish(now);
		else render(now);
	}

	function run() {
		tick();
		if (s.finished) return;
		ticker = window.setInterval(tick, 500);
		showSoundHint();
	}

	function begin() {
		s = S.create(config, Date.now());
		S.save(s);
		run();
	}

	// ---- Controls ---------------------------------------------------------

	// act runs one control: catch up to now first (so a phase that just
	// ended chimes and counts), then change the session, save and redraw.
	function act(change) {
		if (!s || s.finished) return;
		tick();
		if (s.finished) return;
		var now = Date.now();
		change(now);
		S.save(s);
		if (s.finished) finish(now);
		else render(now);
	}

	function togglePause() {
		act(function (now) {
			if (s.waiting) S.next(s, now);
			else if (s.pausedAt !== null) S.resume(s, now);
			else S.pause(s, now);
		});
	}
	function skip() { act(function (now) { S.skip(s, now); }); }
	function restart() { act(function (now) { S.restart(s, now); }); }

	// askToNotify asks for notification permission from a click or key
	// (spec: "Notification permission"), if the home page's ▶ didn't.
	function askToNotify() {
		if ("Notification" in window && Notification.permission === "default") Notification.requestPermission();
	}

	// A mouse click leaves focus on the button, and Space would then press
	// it again instead of pausing. Dropping focus after a pointer click
	// (detail > 0) keeps Space meaning Pause; keyboard presses keep focus.
	function control(button, handler) {
		if (!button) return;
		button.addEventListener("click", function (e) {
			askToNotify();
			handler();
			if (e.detail > 0) button.blur();
		});
	}
	control(el.pause, togglePause);
	control(el.next, togglePause);
	control(el.skip, skip);
	control(el.restart, restart);

	function toggleFullscreen() {
		if (!document.fullscreenEnabled) return;
		if (document.fullscreenElement) document.exitFullscreen();
		else document.documentElement.requestFullscreen().catch(function () {});
	}
	if (document.fullscreenEnabled) {
		el.fullscreen.hidden = false;
		control(el.fullscreen, toggleFullscreen);
		document.addEventListener("fullscreenchange", function () {
			el.fullscreen.textContent = document.fullscreenElement ? "Exit full screen" : "⛶ Full screen";
		});
	}

	// confirmThen asks in the page's dialog. onCancel runs on Cancel and on
	// Esc. Listeners belong to this one opening (home.js's pattern), so a
	// dismissed question can never fire later.
	function confirmThen(message, okLabel, cancelLabel, onOK, onCancel) {
		var dialog = document.getElementById("focus-run-dialog");
		var ok = document.getElementById("focus-run-dialog-ok");
		var cancel = document.getElementById("focus-run-dialog-cancel");
		document.getElementById("focus-run-dialog-message").textContent = message;
		ok.textContent = okLabel;
		cancel.textContent = cancelLabel;
		var chosen = false;
		var controller = new AbortController();
		dialog.addEventListener("close", function () {
			controller.abort();
			if (!chosen && onCancel) onCancel();
		}, { once: true });
		ok.addEventListener("click", function () {
			chosen = true;
			dialog.close();
			onOK();
		}, { signal: controller.signal });
		cancel.addEventListener("click", function () { dialog.close(); }, { signal: controller.signal });
		dialog.showModal();
	}

	// exit asks first while a session is running, then ends and records it
	// (spec: "Controls": Exit).
	function exit() {
		if (!s || s.finished) {
			window.location.assign("/focus/");
			return;
		}
		confirmThen("End this session?", "End session", "Keep going", function () {
			// It may have run out, and been recorded, while the dialog was open.
			if (s.finished) {
				window.location.assign("/focus/");
				return;
			}
			var now = Date.now();
			S.advance(s, now); // a phase that ran out while the dialog was open still counts
			S.end(s, now);
			S.save(s);
			finish(now, true);
		});
	}
	el.exit.addEventListener("click", function (e) {
		e.preventDefault();
		exit();
	});

	document.addEventListener("keydown", function (e) {
		if (e.repeat || e.metaKey || e.ctrlKey || e.altKey) return;
		if (document.querySelector("dialog[open]")) return; // the dialog's own keys
		switch (e.key) {
		case " ":
			// Space on a focused control presses that control, natively.
			if (e.target.closest && e.target.closest("button, a")) return;
			e.preventDefault();
			askToNotify();
			togglePause();
			break;
		case "s":
		case "S":
			if (el.skip) skip();
			break;
		case "r":
		case "R":
			restart();
			break;
		case "f":
		case "F":
			toggleFullscreen();
			break;
		case "Escape":
			// In real full screen the browser takes Esc to leave it.
			if (document.fullscreenElement) return;
			e.preventDefault();
			exit();
			break;
		}
	});

	// A tab coming back catches up at once rather than on the next tick.
	document.addEventListener("visibilitychange", function () {
		if (s && !s.finished && !document.hidden) tick();
	});

	// Browsers keep audio locked until the person clicks or presses a key
	// on this page, and ▶ was a click on the home page. Say so, and unlock
	// on the first gesture (F2 plan: "Sound unlock").
	function showSoundHint() {
		if (s.chime === "silent" || !chimes.locked()) return;
		el.soundHint.hidden = false;
		function unlock() {
			chimes.unlock();
			el.soundHint.hidden = true;
			document.removeEventListener("pointerdown", unlock, true);
			document.removeEventListener("keydown", unlock, true);
		}
		document.addEventListener("pointerdown", unlock, true);
		document.addEventListener("keydown", unlock, true);
	}

	// recordThenBegin records a session being replaced before this timer's
	// takes its place: there is one session per browser. If it can't be
	// saved it is dropped with a warning (F3 plan: being offline at exactly
	// this moment is rare).
	function recordThenBegin(old) {
		if (!R.eligible(old)) {
			begin();
			return;
		}
		R.send(old).then(function (result) {
			if (result === "failed") console.warn("ON Focus: couldn't save " + old.timerName + " before replacing it");
			begin();
		});
	}

	// ---- Start, resume or replace (spec: "Resume") -------------------------

	var stored = S.load(config.userId);
	if (!stored) {
		begin();
	} else if (stored.timerId === config.id) {
		// tick() catches up, so phases that ended while away chime once.
		s = stored;
		run();
	} else {
		S.advance(stored, Date.now());
		if (stored.finished) {
			// Another timer's session already ran out: record it, then
			// start this one.
			recordThenBegin(stored);
		} else {
			confirmThen(
				"End " + stored.timerName + " and start " + config.name + "?",
				"Start " + config.name, "Back to " + stored.timerName,
				function () {
					var now = Date.now();
					S.advance(stored, now); // a phase that ran out while the dialog was open still counts
					S.end(stored, now);
					recordThenBegin(stored);
				},
				function () {
					var timerId = Number(stored.timerId);
					if (!Number.isFinite(timerId) || Math.floor(timerId) !== timerId) return;
					window.location.assign("/focus/run/" + encodeURIComponent(String(timerId)));
				}
			);
		}
	}
})();
