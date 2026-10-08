// ON Focus's phase-end chimes (spec: "Chimes"), synthesised with the Web
// Audio API so there are no audio files. Browsers keep audio locked until
// the person clicks or presses a key on the page: unlock() is called from
// such a gesture, and locked() lets the running page say sound is off.
"use strict";

(function () {
	var AudioCtx = window.AudioContext || window.webkitAudioContext;
	var ctx = null;

	function context() {
		if (!ctx && AudioCtx) ctx = new AudioCtx();
		return ctx;
	}

	// locked reports whether a chime would be silent right now. Without Web
	// Audio there's nothing to unlock, so that isn't "locked".
	function locked() {
		var c = context();
		return !!c && c.state !== "running";
	}

	// unlock asks the audio context to start, from a click or key. It
	// returns a promise that settles once it has tried; locked() then says
	// whether it worked (a touch pointerdown may not count as a gesture).
	function unlock() {
		var c = context();
		if (!c || c.state === "running") return Promise.resolve(); // "suspended", or iOS's "interrupted"
		try {
			return Promise.resolve(c.resume()).catch(function () {});
		} catch (e) {
			return Promise.resolve();
		}
	}

	// tone plays one sine partial: a quick rise, then an exponential fade.
	function tone(c, out, freq, start, gain, attack, decay) {
		var osc = c.createOscillator();
		var g = c.createGain();
		osc.type = "sine";
		osc.frequency.value = freq;
		g.gain.setValueAtTime(0.0001, start);
		g.gain.exponentialRampToValueAtTime(gain, start + attack);
		g.gain.exponentialRampToValueAtTime(0.0001, start + attack + decay);
		osc.connect(g);
		g.connect(out);
		osc.start(start);
		osc.stop(start + attack + decay + 0.05);
	}

	var sounds = {
		// Two strikes of a small bell: inharmonic partials, fast decay.
		bell: function (c, out, t) {
			[0, 0.6].forEach(function (d) {
				tone(c, out, 660, t + d, 0.5, 0.005, 2.2);
				tone(c, out, 660 * 2.76, t + d, 0.18, 0.005, 1.2);
				tone(c, out, 660 * 5.4, t + d, 0.06, 0.005, 0.6);
			});
		},
		// A low singing bowl: slow swell, long ring; the detuned pair beats.
		bowl: function (c, out, t) {
			tone(c, out, 220, t, 0.45, 0.08, 5);
			tone(c, out, 221.6, t, 0.3, 0.08, 5);
			tone(c, out, 220 * 2.71, t, 0.1, 0.08, 3);
		},
		// Two quiet rising notes.
		soft: function (c, out, t) {
			tone(c, out, 523.25, t, 0.25, 0.03, 1.2);
			tone(c, out, 659.25, t + 0.35, 0.25, 0.03, 1.4);
		}
	};

	function play(name) {
		var make = sounds[name];
		var c = context();
		if (!make || !c) return; // "silent", or no Web Audio
		// A chime is a nicety: a closed or broken audio context must never
		// throw into the timer that called it.
		try {
			if (c.state === "suspended") {
				var resumed = c.resume();
				if (resumed && resumed.catch) resumed.catch(function () {});
			}
			var out = c.createGain();
			out.gain.value = 0.6;
			out.connect(c.destination);
			make(c, out, c.currentTime + 0.02);
		} catch (e) {
			console.warn("ON Focus: couldn't play the chime", e);
		}
	}

	window.OnFocus = window.OnFocus || {};
	window.OnFocus.chimes = { play: play, locked: locked, unlock: unlock };

	// The timer form's ▶ plays the chime picked in the select it names. It
	// ships hidden because it needs this script.
	document.querySelectorAll("[data-focus-chime-preview]").forEach(function (button) {
		button.hidden = false;
		button.addEventListener("click", function () {
			var select = document.getElementById(button.dataset.focusChimePreview);
			if (!select) return;
			unlock();
			play(select.value);
		});
	});
})();
