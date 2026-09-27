#!/usr/bin/env bash
# Build for the Pi, copy the binary over, and restart the service.
set -euo pipefail

HOST=${HOST:-pi.local}
BIN=onsuite

GOOS=linux GOARCH=arm64 go build -o "dist/$BIN" ./cmd/onsuite
rsync -az --progress "dist/$BIN" "$HOST:/opt/onsuite/$BIN.new"

ssh "$HOST" <<'EOF'
  set -e
  sudo mv /opt/onsuite/onsuite.new /opt/onsuite/onsuite
  sudo systemctl restart onsuite
  systemctl --no-pager status onsuite | head -5
EOF

echo "Deployed to $HOST"
