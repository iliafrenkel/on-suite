# Deploying ON Suite

ON Suite is one static binary plus one data directory. The data directory holds
the database, the backups and, if you use built-in TLS, the certificates —
copying it is a complete backup of the system.

## Build

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
  -trimpath -ldflags "-s -w -X main.version=$(git describe --tags --always)" \
  -o onsuite ./cmd/onsuite
```

`CGO_ENABLED=0` works because the SQLite driver is pure Go. The result has no
dynamic library dependencies and runs on any kernel of the right architecture.

## Install

```bash
scp onsuite server:/tmp/onsuite
ssh server 'sudo install -m 0755 /tmp/onsuite /usr/local/bin/onsuite'
scp docs/deploy/onsuite.service server:/tmp/
ssh server 'sudo install -m 0644 /tmp/onsuite.service /etc/systemd/system/ && sudo systemctl daemon-reload'
```

Create the first account before starting the service, so there is never a
window in which the suite is running with no accounts:

```bash
sudo -u root onsuite user add ilia --admin --data-dir /var/lib/onsuite
sudo systemctl enable --now onsuite
```

The command prompts for a password without echoing it. Note that with
`DynamicUser=yes` systemd owns `/var/lib/onsuite`; if you create the account
before the first start, `chown` the directory to match or simply run
`onsuite user add` again afterwards with the service stopped.

## Choose a TLS story

**Behind a reverse proxy (recommended).** The service listens on
`127.0.0.1:8080` and the proxy terminates TLS. Because the process itself only
sees plain HTTP, pass `--secure-cookies` so session cookies are still marked
`Secure`. A minimal Caddy configuration:

```
on.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

**On its own.** Drop the proxy and let the binary obtain its own certificate:

```bash
onsuite serve --data-dir /var/lib/onsuite --tls-domain on.example.com
```

The listen address defaults to `:443`, and a plain-HTTP listener on `:80`
answers ACME challenges and redirects to HTTPS. If port 80 is unavailable, pass
`--tls-http-addr ""`; certificates are then obtained over TLS-ALPN-01 on 443
alone. Both ports are privileged, so the unit needs
`AmbientCapabilities=CAP_NET_BIND_SERVICE`.

## Backups

The server snapshots itself every 24 hours by default, keeping 7 snapshots in
`<data-dir>/backups`:

```bash
onsuite serve --backup-interval 24h --backup-keep 7 ...
```

Set `--backup-interval 0` to disable that and drive it externally instead:

```bash
onsuite backup --data-dir /var/lib/onsuite --keep 30
```

Snapshots use SQLite's `VACUUM INTO`, so they are consistent without taking the
database offline. **Copying `onsuite.db` with `cp` while the server runs is not
safe** — use the command.

To restore: stop the service, replace `onsuite.db` with a snapshot, delete any
`onsuite.db-wal` and `onsuite.db-shm` beside it, start the service.

```bash
sudo systemctl stop onsuite
sudo cp /var/lib/onsuite/backups/onsuite-20260818T030000Z.db /var/lib/onsuite/onsuite.db
sudo rm -f /var/lib/onsuite/onsuite.db-wal /var/lib/onsuite/onsuite.db-shm
sudo systemctl start onsuite
```

Snapshots are not encrypted and contain everything every user has written. Send
them somewhere private.

## Upgrades

Replace the binary and restart. Migrations are forward-only and run
automatically at startup; there is no separate migration step.

```bash
sudo install -m 0755 /tmp/onsuite /usr/local/bin/onsuite
sudo systemctl restart onsuite
```

**Take a snapshot first.** There are no down migrations, so rolling back a
schema change means restoring a backup.

## Adding people

```bash
onsuite user add sasha --data-dir /var/lib/onsuite
```

There is no public sign-up page, by design. Omit `--admin` for an ordinary
account.

## Exporting your data

```bash
onsuite export ilia --data-dir /var/lib/onsuite --out ilia.json
```

Plain JSON, readable without this software. Share links are deliberately
excluded, because a share link is a credential; use a snapshot if you need a
restorable copy.

## Checking on it

```bash
curl -s localhost:8080/healthz          # version and a database ping
journalctl -u onsuite -f                # structured JSON logs
```

## Docker, if you prefer

```bash
docker build --build-arg VERSION=$(git describe --tags --always) -t onsuite .
docker run -d --name onsuite -p 8080:8080 -v onsuite-data:/data onsuite
docker exec -it onsuite /onsuite user add ilia --admin --data-dir /data
```
