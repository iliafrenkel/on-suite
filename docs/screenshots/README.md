# Documentation screenshots

*For a developer refreshing the screenshots in the README and the user
guides, or adding a new one.*

Every screenshot is generated, not taken by hand, so they can all be redone
in a minute after a UI change. Two small Go programs do the work:

- **[`seed`](seed/)** creates a fresh data directory with demo accounts and
  realistic content — notes, pastes, feeds, flash card decks and focus
  timers, with a few weeks of focus history. It also writes a signed-in
  session for the `demo` account (`demo-session`) and the paths of two public
  share pages (`share-paste`, `share-notes`) next to the database.
- **[`capture`](capture/)** drives headless Google Chrome over the DevTools
  protocol. For every shot in [`capture/shots.go`](capture/shots.go) it sets
  the session and theme cookies, opens the page at a fixed viewport, runs
  any setup script and saves a PNG.

## Prerequisites

- Go, the version in [`go.mod`](../../go.mod).
- Google Chrome. `capture` looks in the usual macOS and Linux locations; set
  `$CHROME` to the browser binary if yours is elsewhere (Chromium works too).

## Demo accounts

| Username | Role  | Password                   |
|----------|-------|----------------------------|
| `demo`   | admin | `demo-password-not-secret` |
| `sam`    | user  | `demo-password-not-secret` |

The password is a published local demo value, not a real credential. Sign
in as `demo` in a normal browser if you want to look around the demo data
before writing a setup script.

## Refreshing every screenshot

From the repository root:

```bash
# The version shown in screenshot footers; bump it per release. No leading
# v: releases stamp the tag without it, so real footers read "2.0.0".
VERSION=2.0.0
SEED=$(mktemp -d)/demo
go run ./docs/screenshots/seed --data-dir $SEED
go build -ldflags "-X main.version=$VERSION" -o $SEED/onsuite ./cmd/onsuite
$SEED/onsuite serve --addr :8308 --data-dir $SEED &
SERVER=$!
until curl -fs http://localhost:8308/healthz >/dev/null; do sleep 0.2; done
go run ./docs/screenshots/capture --session-file $SEED/demo-session
kill $SERVER
```

`capture` prints `wrote <path>` for each PNG. Always seed a fresh
directory: the seed uses today's date, so due dates and "3 days ago"
labels look the same every time. Review the changed images with
`git diff --stat` and by eye before committing them.

`capture` flags:

- `--base` — the server's URL, default `http://localhost:8308`.
- `--session-file` — the `demo-session` file the seed wrote. The `share-*`
  files are read from the same directory.
- `--only` — a comma-separated list of file names to capture instead of
  all of them, e.g. `--only dashboard.png,notes-outline.png`.

A shot fails, rather than saving the wrong picture, if the page redirects to
the sign-in screen (a stale session or a `--base` that doesn't match the
server) or renders in the wrong theme. Chrome runs with a throwaway profile
and is always shut down, even on an error or `Ctrl`+`C`.

## Adding a screenshot

1. Append a `shot` to [`capture/shots.go`](capture/shots.go), next to the
   other shots for the same guide:

   ```go
   {Name: "docs/user/images/notes-outline.png", URL: "/notes/"},
   ```

   `Name` is the output path from the repository root. The other fields are
   optional: `Setup` is JavaScript run after the page loads (open a menu,
   flip a card; it may use `await`), `Theme` is `"light"` (default) or
   `"dark"`, `Width` and `Height` set the viewport (default 1280×800), and
   `Anon: true` captures signed out. A URL can use `{{share-paste}}` or
   `{{share-notes}}` for the public share pages, whose addresses change on
   every seed.
2. Reference it from the guide as `images/<name>.png`.
3. Capture just that shot with `--only <name>.png` and look at the result.
   Re-shoot if anything is cut off, empty or caught mid-transition.
4. Commit the image, the guide and `shots.go` together.

Keep images small: crop with a smaller viewport rather than capturing a
whole mostly-empty page.
