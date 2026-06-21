#!/usr/bin/env bash
# Boots a fresh local lumera node prepared for the web PoC: creates the agent
# (val, also router+supernode) and publisher (pub) keys, registers val as an
# active SuperNode, and funds the publisher so it can post a tool bond. Leaves
# the node running. The web server (main.go) talks to this node + keyring.
set -uo pipefail
LD=${LUMERAD:-/tmp/lumerad}
HM=${LUMERA_HOME:-/tmp/lumera-web}
NODE=${LUMERA_NODE:-tcp://localhost:26657}
CHAIN=${LUMERA_CHAIN_ID:-lumera-local-1}

pkill -f "lumerad start --home $HM" 2>/dev/null || true; sleep 2
rm -rf "$HM"
KR=(--keyring-backend test --home "$HM")
C=(--home "$HM" --node "$NODE" --chain-id "$CHAIN" --keyring-backend test --gas 700000 --fees 200000ulume -y)
"$LD" init val --chain-id "$CHAIN" --home "$HM" >/dev/null 2>&1
for k in val pub chl; do "$LD" keys add "$k" "${KR[@]}" --algo eth_secp256k1 >/dev/null 2>&1; done
VAL=$("$LD" keys show val -a "${KR[@]}"); PUB=$("$LD" keys show pub -a "${KR[@]}"); CHL=$("$LD" keys show chl -a "${KR[@]}")
VALOPER=$("$LD" keys show val --bech val -a "${KR[@]}")
# Seed a high-quality metric snapshot for the demo tool so the publisher can earn
# a reputation badge (incentives). In production these metrics come from the
# Proof-of-Service receipts + dispute outcomes; here we seed them for the demo.
python3 - "$HM/config/genesis.json" <<'PY'
import json,sys
p=sys.argv[1]; d=json.load(open(p)); inc=d['app_state']['incentives']
# Tune so the demo's reputation arc is observable: a single Proof-of-Service
# receipt is enough to score (the on-chain usage feed overwrites total_invocations
# with the real receipt count), and a downgrade after a dispute takes effect in
# one block rather than the ~50-minute production grace period.
inc['params']['min_invocations_for_scoring']=1
inc['params']['grace_period_blocks']=1
inc['metric_snapshots']=[{
  "tool_id":"pubtool","block_height":"1","timestamp":"2026-06-21T00:00:00Z",
  "uptime_bps":10000,"success_rate_bps":10000,"p95_latency_ms":40,"latency_variance":0,
  "quote_deviation_bps":0,"receipt_validity_bps":10000,"settlement_accuracy_bps":10000,
  "sbom_age_hours":1,"slsa_level":3,"critical_vulnerabilities":0,"high_vulnerabilities":0,
  "requests_per_second":200,"declared_capacity":200,"cache_hit_rate_bps":8000,
  "dispute_rate_bps":0,"governance_participation_bps":10000,
  "total_invocations":"1000","successful_invocations":"1000","failed_invocations":"0"}]
json.dump(d,open(p,'w'),indent=1)
PY
"$LD" genesis add-genesis-account "$VAL" 100000000000000ulume "${KR[@]}" >/dev/null 2>&1
"$LD" genesis gentx val 1000000000ulume --chain-id "$CHAIN" "${KR[@]}" >/dev/null 2>&1
"$LD" genesis collect-gentxs --home "$HM" >/dev/null 2>&1
nohup "$LD" start --home "$HM" --minimum-gas-prices=0ulume --log_level error > "$HM/node.log" 2>&1 &
for _ in $(seq 1 40); do "$LD" status --node "$NODE" >/dev/null 2>&1 && break; sleep 0.7; done
height(){ "$LD" status --node "$NODE" 2>/dev/null | python3 -c "import sys,json;d=json.load(sys.stdin);si=d.get('sync_info',d.get('SyncInfo',{}));print(si.get('latest_block_height','0'))" 2>/dev/null || echo 0; }
for _ in $(seq 1 60); do h=$(height); case "$h" in ''|*[!0-9]*) h=0;; esac; [ "$h" -ge 2 ] && break; sleep 0.7; done
hash(){ python3 -c "import sys,json;print(json.load(sys.stdin).get('txhash',''))"; }
waittx(){ for _ in $(seq 1 40); do o=$("$LD" query tx "$1" --node "$NODE" -o json 2>/dev/null||true); [ -n "$o" ] && return 0; sleep 0.7; done; }
# Register val as an active SuperNode (so it can attest Proof-of-Service receipts).
waittx "$("$LD" tx supernode register-supernode "$VALOPER" 127.0.0.1 "$VAL" --from val "${C[@]}" -o json | hash)"
# Fund the publisher (escrows a tool bond) and the challenger (disputes a bad receipt).
waittx "$("$LD" tx bank send "$VAL" "$PUB" 6000000ulume --from val "${C[@]}" -o json | hash)"
waittx "$("$LD" tx bank send "$VAL" "$CHL" 3000000ulume --from val "${C[@]}" -o json | hash)"
echo "node ready @h$(height)"
echo "  agent/router/supernode (val): $VAL"
echo "  publisher (pub):              $PUB"
echo "  challenger (chl):             $CHL"
echo
echo "Now start the web PoC:   LUMERA_HOME=$HM go run ./poc/web"
echo "Then open:               http://localhost:8787"