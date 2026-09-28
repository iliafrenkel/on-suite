package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

// browser is one headless Chrome with one page target.
type browser struct {
	cmd       *exec.Cmd
	conn      *websocket.Conn
	nextID    int
	tmp       string
	closeOnce sync.Once
}

// chromePath finds Chrome: $CHROME, then the usual macOS and Linux paths.
func chromePath() (string, error) {
	cands := []string{os.Getenv("CHROME"),
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser"}
	for _, c := range cands {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", errors.New("capture: Chrome not found; set $CHROME")
}

// devToolsOrigin is the Origin the DevTools WebSocket is dialled with, and
// the only one Chrome is told to accept (--remote-allow-origins compares it
// exactly, so no trailing slash).
const devToolsOrigin = "http://127.0.0.1"

// launch starts Chrome with a throwaway profile. The DevTools port is 0, so
// Chrome picks a free one and writes it to DevToolsActivePort in the
// profile — no fixed port to collide with another run or a desktop Chrome.
// started, if non-nil, sees the browser as soon as the process exists, so a
// signal handler can kill it even if launch is interrupted.
func launch(started func(*browser)) (*browser, error) {
	path, err := chromePath()
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "onsuite-capture-")
	if err != nil {
		return nil, err
	}
	profile := filepath.Join(tmp, "profile")
	cmd := exec.Command(path, "--headless=new", "--remote-debugging-port=0",
		"--remote-allow-origins="+devToolsOrigin, "--user-data-dir="+profile,
		"--hide-scrollbars", "--force-device-scale-factor=1", "--no-first-run",
		"--no-default-browser-check", "about:blank")
	if err := cmd.Start(); err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	b := &browser{cmd: cmd, tmp: tmp}
	if started != nil {
		started(b)
	}

	// Wait for the DevTools endpoint and find the page target.
	var targets []struct {
		Type, WebSocketDebuggerURL string
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		err = nil
		port, perr := devToolsPort(profile)
		if perr != nil {
			err = perr
		} else {
			var resp *http.Response
			resp, err = http.Get("http://127.0.0.1:" + port + "/json/list")
			if err == nil {
				err = json.NewDecoder(resp.Body).Decode(&targets)
				resp.Body.Close()
				if err == nil && len(targets) > 0 {
					break
				}
			}
		}
		if time.Now().After(deadline) {
			b.close()
			return nil, fmt.Errorf("capture: Chrome DevTools not reachable: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	for _, t := range targets {
		if t.Type == "page" {
			b.conn, err = websocket.Dial(t.WebSocketDebuggerURL, "", devToolsOrigin)
			if err != nil {
				b.close()
				return nil, err
			}
			return b, nil
		}
	}
	b.close()
	return nil, errors.New("capture: no page target")
}

// devToolsPort reads the port Chrome chose from its profile directory.
func devToolsPort(profile string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
	if err != nil {
		return "", err
	}
	port, _, _ := strings.Cut(string(raw), "\n")
	if port = strings.TrimSpace(port); port == "" {
		return "", errors.New("capture: DevToolsActivePort is empty")
	}
	return port, nil
}

// close kills Chrome and removes its profile. It is safe to call more than
// once and from the signal handler.
func (b *browser) close() {
	b.closeOnce.Do(func() {
		if b.conn != nil {
			b.conn.Close()
		}
		if b.cmd != nil && b.cmd.Process != nil {
			_ = b.cmd.Process.Kill()
			_ = b.cmd.Wait()
		}
		os.RemoveAll(b.tmp)
	})
}

// call sends one CDP command and returns its result, skipping events.
func (b *browser) call(method string, params any) (json.RawMessage, error) {
	b.nextID++
	id := b.nextID
	if err := websocket.JSON.Send(b.conn, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	return awaitResult(id, func() ([]byte, error) {
		var raw []byte
		err := websocket.Message.Receive(b.conn, &raw)
		return raw, err
	})
}

// expandURL replaces {{name}} with vars[name] — the share-* files seed
// writes next to demo-session, whose slugs are random per seed.
func expandURL(u string, vars map[string]string) string {
	for k, v := range vars {
		u = strings.ReplaceAll(u, "{{"+k+"}}", v)
	}
	return u
}

// awaitResult reads messages until the response for id arrives. Events
// (messages with a method and no id) are discarded.
func awaitResult(id int, next func() ([]byte, error)) (json.RawMessage, error) {
	for {
		raw, err := next()
		if err != nil {
			return nil, err
		}
		var m struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		if m.ID != id {
			continue
		}
		if m.Error != nil {
			return nil, errors.New("cdp: " + m.Error.Message)
		}
		return m.Result, nil
	}
}
