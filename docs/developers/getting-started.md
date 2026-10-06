# Getting started

*For a developer setting up ON Suite locally for the first time. About ten
minutes, start to finish.*

## Requirements

- **Go 1.26.6 or newer** — the `go` line in `go.mod`. With an older Go 1.21+
  toolchain, `go` downloads the right version for you.
- Nothing else. No Docker, no Node, no database server: SQLite is the pure-Go
  `modernc.org/sqlite`, so there is no C toolchain to install either.

## Build and run

Clone and build:

```bash
git clone https://github.com/iliafrenkel/on-suite.git
cd on-suite
go build ./cmd/onsuite
```

That produces a single `./onsuite` binary with every template, stylesheet and
script embedded in it.

Create the first account. It prompts for a password twice, with echo off
(at least 12 characters):

```bash
./onsuite user add ilia --admin --data-dir ./data
```

Passwords are never accepted as a flag, because flags end up in `ps` output
and shell history. For scripted setup, pipe the password on stdin instead:
`./onsuite user add ilia --admin --data-dir ./data < password.txt`.

Start the server:

```bash
./onsuite serve --data-dir ./data
```

It listens on `:8080`, logs JSON to stdout and applies any pending database
migrations on startup. Everything it stores is under `./data` — `onsuite.db`,
and `backups/` once the first snapshot runs — which `.gitignore` already
excludes.

Check it's up:

```bash
curl -s localhost:8080/healthz
# {"status":"ok","version":"dev"}
```

Then open <http://localhost:8080/>. You'll be redirected to `/login`; sign in
and you land on the dashboard with all five apps in the sidebar. Because the
account is an admin, the sidebar also has an **Admin** entry, which leads to
the admin dashboard (`/admin/`), user management (`/admin/users`) and jobs
(`/admin/jobs`).

To start again from nothing, stop the server and delete `./data`.

## Commands and configuration

`./onsuite help` lists every command:

```text
onsuite serve [flags]     run the server
onsuite user add <name>   create an account
onsuite user reset-password <name>
                          set a new password and sign the account out
onsuite export <name>     write a user's data as JSON
onsuite backup            write a database snapshot
onsuite version           print the build version
onsuite help              show this message
```

Every `serve` flag has an `ONSUITE_*` environment variable. A flag given on
the command line wins over the variable. Run `./onsuite serve -h` for the
descriptions.

| Flag | Environment variable | Default |
| --- | --- | --- |
| `-addr` | `ONSUITE_ADDR` | `:8080` |
| `-data-dir` | `ONSUITE_DATA_DIR` | `./data` |
| `-log-level` | `ONSUITE_LOG_LEVEL` | `info` |
| `-disable-apps` | `ONSUITE_DISABLE_APPS` | *(none)* |
| `-backup-interval` | `ONSUITE_BACKUP_INTERVAL` | `24h` |
| `-backup-keep` | `ONSUITE_BACKUP_KEEP` | `7` |
| `-secure-cookies` | `ONSUITE_SECURE_COOKIES` | `false` |
| `-tls-domain` | `ONSUITE_TLS_DOMAIN` | *(none)* |
| `-tls-http-addr` | `ONSUITE_TLS_HTTP_ADDR` | `:80` |

`user`, `export` and `backup` take their own `--data-dir` flag and read
`ONSUITE_DATA_DIR` too. Leave `-secure-cookies` and `-tls-domain` off
locally: a `Secure` cookie is never sent over plain HTTP, so signing in would
silently stop working.

Two flags are handy while developing: `-log-level debug`, and
`-disable-apps notes,reader` to run with only the apps you're working on.

## Cross-compiling

There is no CGO, so any machine can build for any target:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o onsuite ./cmd/onsuite
```

Release builds (via GoReleaser) also stamp the version with
`-ldflags "-X main.version=1.2.3"` — the tag without its `v`; see
[Releasing](releasing.md).

## The full check

Run this before every commit. It must stay green:

```bash
go build ./... && go vet ./... && go test ./... -race
```

The whole suite takes under a minute. CI runs a little more —
`gofmt`, `staticcheck` and a `go mod tidy` diff — and [Testing](testing.md)
has the exact commands so you can run them locally too.

## Your first change

Templates, CSS and scripts are embedded into the binary with `go:embed`, so
a change only shows up after a rebuild and restart. A quick loop to see that
for yourself:

1. Open `internal/ui/templates/home.html` — the dashboard — and change the
   heading `<h1>ON Suite</h1>` to `<h1>ON Suite, built by me</h1>`.
2. Stop the server (Ctrl-C) and run it straight from source:

   ```bash
   go run ./cmd/onsuite serve --data-dir ./data
   ```

3. Reload <http://localhost:8080/>. The new heading is there.
4. Run the full check, then revert the change:

   ```bash
   go build ./... && go vet ./... && go test ./... -race
   git checkout internal/ui/templates/home.html
   ```

From here, a real change usually starts in an app's `handlers.go` and its
`templates/` directory — [Repository layout](repository-layout.md) has a
"where do I look for…" table.

**Next:** [Architecture](architecture.md)
