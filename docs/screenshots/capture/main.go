// Command capture regenerates the documentation screenshots from a server
// running on seeded demo data. See docs/screenshots/README.md.
package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

type shot struct {
	Name   string // output path relative to the repo root, e.g. "docs/user/images/notes-outline.png"
	URL    string // path on the demo server, e.g. "/notes/"; may use {{share-paste}} / {{share-notes}}
	Setup  string // optional JS run after load, before the capture (open a menu, flip a card)
	Theme  string // "light" (default) or "dark"
	Width  int    // viewport, default 1280
	Height int    // viewport, default 800
	Anon   bool   // capture signed out
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	base := flag.String("base", "http://localhost:8308", "demo server base URL")
	sessionFile := flag.String("session-file", "", "file holding the demo session id (written by seed)")
	only := flag.String("only", "", "comma-separated shot names (base names) to capture; empty = all")
	flag.Parse()
	*base = strings.TrimRight(*base, "/")
	if *sessionFile == "" {
		return errors.New("capture: --session-file is required")
	}

	sid, err := os.ReadFile(*sessionFile)
	if err != nil {
		return err
	}
	vars := map[string]string{}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(*sessionFile), "share-*"))
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			return err
		}
		vars[filepath.Base(m)] = strings.TrimSpace(string(b))
	}

	want := map[string]bool{}
	for _, n := range strings.Split(*only, ",") {
		if n = strings.TrimSpace(n); n != "" {
			want[n] = true
		}
	}
	known := map[string]bool{}
	for _, s := range shots {
		known[filepath.Base(s.Name)] = true
	}
	for n := range want {
		if !known[n] {
			return fmt.Errorf("capture: --only %q matches no shot", n)
		}
	}

	// Ctrl-C (or a kill) must not leave a headless Chrome behind. The
	// handler goes in before launch so a signal mid-launch is covered too.
	var current atomic.Pointer[browser]
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		if b := current.Load(); b != nil {
			b.close()
		}
		os.Exit(130)
	}()

	b, err := launch(current.Store)
	if err != nil {
		return err
	}
	defer b.close()

	for _, m := range []string{"Page.enable", "Network.enable", "Runtime.enable"} {
		if _, err := b.call(m, map[string]any{}); err != nil {
			return err
		}
	}
	for _, s := range shots {
		if len(want) > 0 && !want[filepath.Base(s.Name)] {
			continue
		}
		s.URL = expandURL(s.URL, vars)
		if strings.Contains(s.URL, "{{") {
			return fmt.Errorf("%s: unexpanded placeholder in %q (missing share-* file next to the session file?)", s.Name, s.URL)
		}
		if err := capture(b, *base, strings.TrimSpace(string(sid)), s); err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}
		fmt.Println("wrote", s.Name)
	}
	return nil
}

func capture(b *browser, base, sid string, s shot) error {
	if s.Width == 0 {
		s.Width = 1280
	}
	if s.Height == 0 {
		s.Height = 800
	}
	if s.Theme == "" {
		s.Theme = "light"
	}
	if _, err := b.call("Network.clearBrowserCookies", map[string]any{}); err != nil {
		return err
	}
	// The server renders data-theme from this cookie (prefs.go ThemeFrom);
	// the CSS keys off data-theme alone, not prefers-color-scheme.
	cookies := []map[string]any{{"name": "onsuite_theme", "value": s.Theme, "url": base}}
	if !s.Anon {
		cookies = append(cookies, map[string]any{"name": "onsuite_session", "value": sid, "url": base})
	}
	if _, err := b.call("Network.setCookies", map[string]any{"cookies": cookies}); err != nil {
		return err
	}
	if _, err := b.call("Emulation.setDeviceMetricsOverride", map[string]any{
		"width": s.Width, "height": s.Height, "deviceScaleFactor": 1, "mobile": false,
	}); err != nil {
		return err
	}
	// Mark the outgoing document so waitReady can't mistake it (it is
	// already "complete") for the page being navigated to.
	if _, err := evaluate(b, "window.__captureOld = true", false); err != nil {
		return err
	}
	if _, err := b.call("Page.navigate", map[string]any{"url": base + s.URL}); err != nil {
		return err
	}
	if err := waitReady(b); err != nil {
		return err
	}
	if err := checkPage(b, s); err != nil {
		return err
	}
	if s.Setup != "" {
		if _, err := evaluate(b, "(async () => {"+s.Setup+"})()", true); err != nil {
			return fmt.Errorf("setup: %w", err)
		}
		time.Sleep(400 * time.Millisecond) // let htmx swaps and transitions settle
	}
	res, err := b.call("Page.captureScreenshot", map[string]any{"format": "png"})
	if err != nil {
		return err
	}
	var out struct{ Data string }
	if err := json.Unmarshal(res, &out); err != nil {
		return err
	}
	png, err := base64.StdEncoding.DecodeString(out.Data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Name), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.Name, png, 0o644)
}

// checkPage fails the shot if the cookies didn't take: a signed-in shot
// that landed on /login, or a page whose theme isn't the one asked for.
func checkPage(b *browser, s shot) error {
	v, err := evaluate(b, `JSON.stringify({path: location.pathname, theme: document.documentElement.getAttribute("data-theme") || ""})`, false)
	if err != nil {
		return err
	}
	var got struct{ Path, Theme string }
	if err := json.Unmarshal([]byte(v.(string)), &got); err != nil {
		return err
	}
	if !s.Anon && got.Path == "/login" && !strings.HasPrefix(s.URL, "/login") {
		return errors.New("redirected to /login: the session cookie was rejected (stale demo-session, or --base doesn't match the server?)")
	}
	if got.Theme != "" && got.Theme != s.Theme {
		return fmt.Errorf("page rendered theme %q, want %q", got.Theme, s.Theme)
	}
	return nil
}

// evaluate runs JS in the page and returns its value, turning a thrown
// exception into an error (CDP reports those in the result, not as a
// protocol error).
func evaluate(b *browser, expr string, await bool) (any, error) {
	res, err := b.call("Runtime.evaluate", map[string]any{
		"expression": expr, "returnByValue": true, "awaitPromise": await,
	})
	if err != nil {
		return nil, err
	}
	var r struct {
		Result           struct{ Value any }
		ExceptionDetails *struct {
			Text      string
			Exception struct{ Description string }
		}
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	if e := r.ExceptionDetails; e != nil {
		if e.Exception.Description != "" {
			return nil, errors.New(e.Exception.Description)
		}
		return nil, errors.New(e.Text)
	}
	return r.Result.Value, nil
}

// waitReady polls for the new document's readyState and font loading, then gives layout a
// moment to settle.
func waitReady(b *browser) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		v, err := evaluate(b, "!window.__captureOld && document.readyState === 'complete' && document.fonts.status === 'loaded'", false)
		if err != nil {
			return err
		}
		if ok, _ := v.(bool); ok {
			time.Sleep(300 * time.Millisecond)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("page not ready after 10s")
}
