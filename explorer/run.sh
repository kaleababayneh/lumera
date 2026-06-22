#!/usr/bin/env bash
# Build + run the Lumera on-chain explorer against a local node.
#
#   ./explorer/run.sh                       # node tcp://localhost:26657, UI :8090
#   LISTEN=:9000 ./explorer/run.sh          # custom UI port
#   LUMERA_NODE=tcp://host:26657 ./explorer/run.sh
#
# Open http://localhost:8090
set -uo pipefail
cd "$(dirname "$0")/.."

NODE=${LUMERA_NODE:-tcp://localhost:26657}
LISTEN=${LISTEN:-:8090}
DB=${EXPLORER_DB:-/tmp/lumera-explorer.db}
BIN=${BIN:-/tmp/lumera-explorer}

echo "building explorer…"
go build -o "$BIN" ./explorer || { echo "build failed"; exit 1; }

pkill -f "lumera-explorer" 2>/dev/null || true; sleep 1
echo "explorer → $LISTEN   (node $NODE, db $DB)"
exec "$BIN" --node "$NODE" --listen "$LISTEN" --db "$DB"
