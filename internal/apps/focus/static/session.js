// ON Focus's running-session engine (spec: "The runner (focus.js)" —
// "State and timing"). Pure state and time maths, shared by the running
// page (focus.js) and the home page's resume banner (home.js); nothing here
// touches the page.
//
// Time left is always derived from Date.now() and the stored timestamps,
// never counted down, so a throttled background tab never drifts. Every
// function takes `now` (ms since the epoch) rather than reading the clock,
// which keeps the maths checkable by hand.
"use strict";

(function () {
	var KEY = "onsuite.focus.session";
	var VERSION = 2; // 2: sessions carry the owner's userId; v1 ones have none and are discarded

	function current(s) { return s.phases[s.phaseIndex]; }
	function upcoming(s) { return s.phases[s.phaseIndex + 1]; }
	function isLast(s) { return s.phaseIndex === s.phases.length - 1; }

	function newID() {
		if (window.crypto && window.crypto.randomUUID) return window.crypto.randomUUID();
		// randomUUID needs a secure context; a self-hosted suite on plain
		// http on the LAN doesn't have one.
		return Date.now().toString(36) + "-" + Math.random().toString(36).slice(2);
	}

	// create starts a session of the page's timer. Settings are copied in
	// here, so editing the timer mid-session doesn't change this one.
	function create(config, now) {
		return {
			v: VERSION,
			userId: config.userId,
			clientId: newID(),
			timerId: config.id,
			timerName: config.name,
			color: config.color,
			chime: config.chime,
			autoAdvance: config.autoAdvance,
			keepHistory: config.keepHistory,
			rounds: config.rounds,
			phases: config.phases,
			startedAt: now,
			endedAt: null,
			phaseIndex: 0,
			phaseStartedAt: now,
			pausedAt: null,
			pausedTotalInPhase: 0,
			focusSecondsBanked: 0,
			roundsDone: 0,
			waiting: false,
			finished: false,
			completed: false
		};
	}

	// elapsed is how much of the current phase has run, in ms, pauses
	// excluded. A phase waiting at its end or a finished session counts as
	// fully run.
	function elapsed(s, now) {
		var length = current(s).seconds * 1000;
		if (s.waiting || s.finished) return length;
		var until = s.pausedAt !== null ? s.pausedAt : now;
		return Math.min(length, Math.max(0, until - s.phaseStartedAt - s.pausedTotalInPhase));
	}

	function remaining(s, now) { return current(s).seconds * 1000 - elapsed(s, now); }

	function progress(s, now) { return elapsed(s, now) / (current(s).seconds * 1000); }

	// advance walks s forward to now: every phase whose time has run out
	// ends, in order, as it would have with the tab open. It stops after the
	// last phase, at the first boundary when auto-advance is off, and while
	// paused. It returns how many phases ended, so the caller chimes once
	// however many were missed (spec: "Phase end").
	function advance(s, now) {
		var ended = 0;
		while (!s.finished && !s.waiting && s.pausedAt === null) {
			var p = current(s);
			var end = s.phaseStartedAt + s.pausedTotalInPhase + p.seconds * 1000;
			if (now < end) break;
			ended++;
			if (p.kind === "focus") {
				s.focusSecondsBanked += p.seconds;
				s.roundsDone++;
			}
			if (isLast(s)) {
				s.finished = true;
				s.completed = true;
				s.endedAt = end;
			} else if (!s.autoAdvance) {
				s.waiting = true;
			} else {
				s.phaseIndex++;
				s.phaseStartedAt = end;
				s.pausedTotalInPhase = 0;
			}
		}
		return ended;
	}

	function startPhase(s, index, now) {
		s.phaseIndex = index;
		s.phaseStartedAt = now;
		s.pausedTotalInPhase = 0;
		s.pausedAt = null;
		s.waiting = false;
	}

	// bankCurrent counts the focus time done so far in the current phase:
	// Skip and Restart keep time actually focused (spec: "Controls").
	function bankCurrent(s, now) {
		if (current(s).kind === "focus" && !s.waiting && !s.finished) {
			s.focusSecondsBanked += Math.floor(elapsed(s, now) / 1000);
		}
	}

	function pause(s, now) {
		if (s.finished || s.waiting || s.pausedAt !== null) return;
		s.pausedAt = now;
	}

	function resume(s, now) {
		if (s.pausedAt === null) return;
		s.pausedTotalInPhase += now - s.pausedAt;
		s.pausedAt = null;
	}

	// next starts the phase after a boundary the session is waiting at
	// ("Start break", "Start round 3").
	function next(s, now) {
		if (!s.waiting) return;
		startPhase(s, s.phaseIndex + 1, now);
	}

	// skip ends the current phase now and starts the next one straight
	// away. Skipping the last phase ends the session, not completed — it
	// didn't run to its end. A skipped focus round isn't counted as done.
	function skip(s, now) {
		if (s.finished) return;
		if (s.waiting) {
			next(s, now);
			return;
		}
		bankCurrent(s, now);
		if (isLast(s)) {
			s.finished = true;
			s.completed = false;
			s.endedAt = now;
			return;
		}
		startPhase(s, s.phaseIndex + 1, now);
	}

	// restart runs the current phase again from its start, unpaused.
	function restart(s, now) {
		if (s.finished || s.waiting) return;
		bankCurrent(s, now);
		startPhase(s, s.phaseIndex, now);
	}

	// end stops the session now, as Exit and the home banner's End do:
	// focus time done so far counts, and it didn't run to its end. Callers
	// advance(s, now) first, so a phase that ran out still counts as done.
	function end(s, now) {
		if (s.finished) return;
		bankCurrent(s, now);
		s.finished = true;
		s.completed = false;
		s.waiting = false;
		s.endedAt = now;
	}

	// focusSeconds is the focus time done so far, pauses and breaks
	// excluded.
	function focusSeconds(s, now) {
		var n = s.focusSecondsBanked;
		if (current(s).kind === "focus" && !s.waiting && !s.finished) {
			n += Math.floor(elapsed(s, now) / 1000);
		}
		return n;
	}

	// startLabel names the button at a boundary. Only called while waiting,
	// and advance() never waits after the last phase, so upcoming(s) exists.
	function startLabel(s) {
		var p = upcoming(s);
		if (p.kind === "break") return "Start break";
		if (p.kind === "long_break") return "Start long break";
		return "Start round " + p.round;
	}

	// notice is the phase-end notification: what's happening now.
	function notice(s) {
		if (s.finished) return s.timerName + " — done";
		var p = s.waiting ? upcoming(s) : current(s);
		if (p.kind === "focus") return s.timerName + " — round " + p.round;
		return s.timerName + " — break time";
	}

	function pad(n) { return (n < 10 ? "0" : "") + n; }

	// clock renders ms left as the countdown shows it, rounding up so
	// 00:00 only shows at the very end. Go's Clock must agree.
	function clock(ms) {
		var total = Math.ceil(ms / 1000);
		var h = Math.floor(total / 3600);
		var m = Math.floor((total % 3600) / 60);
		var sec = total % 60;
		return h > 0 ? h + ":" + pad(m) + ":" + pad(sec) : pad(m) + ":" + pad(sec);
	}

	// focused renders focus time for the Done screen: "3h 20m", "50m".
	function focused(seconds) {
		var m = Math.floor(seconds / 60);
		if (m < 1) return "less than a minute";
		var h = Math.floor(m / 60);
		if (h === 0) return m + "m";
		return m % 60 ? h + "h " + (m % 60) + "m" : h + "h";
	}

	function valid(s) {
		return !!s && s.v === VERSION && typeof s.userId === "number" &&
			typeof s.clientId === "string" && s.clientId !== "" &&
			typeof s.timerId === "number" && typeof s.timerName === "string" &&
			typeof s.color === "string" && /^[a-z]+$/.test(s.color) &&
			Array.isArray(s.phases) && s.phases.length > 0 &&
			s.phases.every(function (p) {
				return p && typeof p.kind === "string" && typeof p.seconds === "number" && p.seconds > 0;
			}) &&
			typeof s.phaseIndex === "number" && s.phaseIndex >= 0 && s.phaseIndex < s.phases.length &&
			typeof s.startedAt === "number" && (s.endedAt === null || typeof s.endedAt === "number") &&
			typeof s.phaseStartedAt === "number" && typeof s.pausedTotalInPhase === "number" &&
			(s.pausedAt === null || typeof s.pausedAt === "number") &&
			typeof s.focusSecondsBanked === "number" && typeof s.roundsDone === "number" &&
			typeof s.waiting === "boolean" && typeof s.finished === "boolean";
	}

	// load returns this user's stored session, or null. Anything unreadable
	// is discarded with a warning, as if nothing were running (spec:
	// "Errors"). Another account's session is ignored but left in place:
	// accounts sharing a browser mustn't see or record each other's
	// sessions, and it is only replaced when this user starts a timer
	// (#494).
	function load(userId) {
		var raw;
		try {
			raw = window.localStorage.getItem(KEY);
		} catch (e) {
			return null;
		}
		if (!raw) return null;
		try {
			var s = JSON.parse(raw);
			if (valid(s)) return s.userId === userId ? s : null;
		} catch (e) {
			// fall through to discard it
		}
		console.warn("ON Focus: discarding an unreadable running session");
		clear();
		return null;
	}

	function save(s) {
		try {
			window.localStorage.setItem(KEY, JSON.stringify(s));
		} catch (e) {
			console.warn("ON Focus: couldn't save the running session", e);
		}
	}

	function clear() {
		try {
			window.localStorage.removeItem(KEY);
		} catch (e) {
			// nothing stored, nothing to clear
		}
	}

	window.OnFocus = window.OnFocus || {};
	window.OnFocus.session = {
		create: create, advance: advance,
		pause: pause, resume: resume, skip: skip, restart: restart, next: next, end: end,
		current: current, upcoming: upcoming,
		elapsed: elapsed, remaining: remaining, progress: progress, focusSeconds: focusSeconds,
		startLabel: startLabel, notice: notice, clock: clock, focused: focused,
		load: load, save: save, clear: clear
	};
})();
