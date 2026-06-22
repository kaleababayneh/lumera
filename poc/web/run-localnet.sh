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

pkill -f "lumerad start --home $HM" 2>/dev/null || true
pkill -f "lumera-explorer" 2>/dev/null || true
sleep 2
rm -rf "$HM"
KR=(--keyring-backend test --home "$HM")
C=(--home "$HM" --node "$NODE" --chain-id "$CHAIN" --keyring-backend test --gas 700000 --fees 200000ulume -y)
"$LD" init val --chain-id "$CHAIN" --home "$HM" >/dev/null 2>&1
# val = agent/router/supernode, pub = publisher, chl = challenger,
# acct1..acct5 = funded test accounts (so you can drive the app as different
# agents from different browsers — pick one in the sidebar account dropdown).
ACCTS=(acct1 acct2 acct3 acct4 acct5)
for k in val pub chl "${ACCTS[@]}"; do "$LD" keys add "$k" "${KR[@]}" --algo eth_secp256k1 >/dev/null 2>&1; done
VAL=$("$LD" keys show val -a "${KR[@]}"); PUB=$("$LD" keys show pub -a "${KR[@]}"); CHL=$("$LD" keys show chl -a "${KR[@]}")
VALOPER=$("$LD" keys show val --bech val -a "${KR[@]}")
# Seed a high-quality metric snapshot for the demo tool so the publisher can earn
# a reputation badge (incentives). In production these metrics come from the
# Proof-of-Service receipts + dispute outcomes; here we seed them for the demo.
python3 - "$HM/config/genesis.json" <<'PY'
import json,sys
p=sys.argv[1]; d=json.load(open(p)); inc=d['app_state']['incentives']
# Reputation arc is observable: a single receipt scores (the on-chain usage feed
# overwrites total_invocations with the real receipt count) and a post-dispute
# downgrade lands in one block rather than the ~50-minute production grace period.
inc['params']['min_invocations_for_scoring']=1
inc['params']['grace_period_blocks']=1
# Seed a fleet of metric snapshots at graded quality so the marketplace + leaderboard
# show a realistic reputation spread (thresholds: BRONZE 6000 / SILVER 7500 / GOLD 8500 / PLATINUM 9500).
def snap(tool,q):
    s=dict(tool_id=tool,block_height="1",timestamp="2026-06-21T00:00:00Z",
      uptime_bps=10000,success_rate_bps=10000,p95_latency_ms=40,latency_variance=0,
      quote_deviation_bps=0,receipt_validity_bps=10000,settlement_accuracy_bps=10000,
      sbom_age_hours=1,slsa_level=3,critical_vulnerabilities=0,high_vulnerabilities=0,
      requests_per_second=200,declared_capacity=200,cache_hit_rate_bps=8000,
      dispute_rate_bps=0,governance_participation_bps=10000,
      total_invocations="1000",successful_invocations="1000",failed_invocations="0")
    if q=='G': s.update(success_rate_bps=9850,uptime_bps=9820,p95_latency_ms=90,sbom_age_hours=18,slsa_level=2,high_vulnerabilities=2,cache_hit_rate_bps=7000)
    if q=='S': s.update(success_rate_bps=9550,uptime_bps=9500,p95_latency_ms=170,sbom_age_hours=72,slsa_level=1,high_vulnerabilities=5,cache_hit_rate_bps=5500,governance_participation_bps=6000)
    if q=='B': s.update(success_rate_bps=9050,uptime_bps=9000,p95_latency_ms=340,sbom_age_hours=260,slsa_level=0,critical_vulnerabilities=1,high_vulnerabilities=9,cache_hit_rate_bps=4000,governance_participation_bps=3000,dispute_rate_bps=400)
    return s
FLEET=[('pubtool','P'),('atlas-7b','P'),('orion-70b','P'),('oracle-feed','P'),('vision-diffuse','G'),('gpu-render','G'),('embed-lg','G'),('web-retriever','S'),('whisper-stt','S'),('code-fix','B')]
inc['metric_snapshots']=[snap(t,q) for t,q in FLEET]
# Keep the SuperNode active for the whole demo: by default it is POSTPONED after
# ~500 blocks (metrics_update_interval 400 + grace 100) unless it reports metrics,
# which makes submit-receipt fail with "supernode is not active". Widen the
# staleness window to effectively-never so a long demo session never trips it.
sn=d['app_state']['supernode']['params']
sn['metrics_update_interval_blocks']="1000000000"
sn['metrics_grace_period_blocks']="1000000000"
sn['metrics_freshness_max_blocks']="1000000000"
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
# Fund the 5 test accounts with LUME (each swaps to LAC + pays for tool calls).
for a in "${ACCTS[@]}"; do
  ADDR=$("$LD" keys show "$a" -a "${KR[@]}")
  waittx "$("$LD" tx bank send "$VAL" "$ADDR" 50000000ulume --from val "${C[@]}" -o json | hash)"
done

# Seed a living marketplace: register a fleet of tools (varied owners/bonds), then
# request a reputation evaluation for each (owner-gated) so the genesis-seeded
# metric snapshots crystallise into on-chain badges. Makes the demo feel like a
# network with traction rather than an empty prototype.
echo "seeding marketplace fleet…"
FLEET=("pubtool pub 2000000" "atlas-7b acct1 4000000" "orion-70b acct2 6000000"
       "oracle-feed acct1 3000000" "vision-diffuse acct3 3000000" "gpu-render acct5 5000000"
       "embed-lg acct3 2500000" "web-retriever acct4 2000000" "whisper-stt acct2 2000000" "code-fix acct4 2000000")
for entry in "${FLEET[@]}"; do set -- $entry
  waittx "$("$LD" tx registry register-tool "$1" --bond "${3}ulume" --from "$2" "${C[@]}" -o json | hash)"
done
for entry in "${FLEET[@]}"; do set -- $entry
  waittx "$("$LD" tx incentives request-evaluation "$1" --from "$2" "${C[@]}" -o json | hash)"
done
# Build the MCP router so the web "Agent terminal" can drive a real agent over MCP.
go build -o /tmp/lumera-mcp-router ./poc/mcp-router 2>/dev/null || true

# Build + launch the on-chain explorer. It indexes EVERY block/tx/event across
# ALL modules from this node into a local bbolt DB and serves a live UI on :8090.
# Rebuilt each boot so its in-process decoder matches the freshly-built node
# (any new module you add is decoded automatically). It backfills from genesis,
# so it captures everything even though it starts after the node.
echo "building + starting explorer…"
if go build -o /tmp/lumera-explorer ./explorer 2>/tmp/lumera-explorer.build.log; then
  nohup /tmp/lumera-explorer --node "$NODE" --listen :8090 \
    --db /tmp/lumera-explorer.db > /tmp/lumera-explorer.log 2>&1 &
  echo "  explorer pid $! → http://localhost:8090"
else
  echo "  explorer build FAILED (see /tmp/lumera-explorer.build.log) — skipping"
fi

echo "node ready @h$(height)"
echo "  agent/router/supernode (val): $VAL"
echo "  publisher (pub):              $PUB"
echo "  challenger (chl):             $CHL"
for a in "${ACCTS[@]}"; do echo "  $a (test account):            $("$LD" keys show "$a" -a "${KR[@]}")"; done
echo
echo "On-chain explorer (live, every module):  http://localhost:8090"
echo
echo "Now start the web PoC:   go build -o /tmp/lumera-poc-web ./poc/web && LUMERA_HOME=$HM /tmp/lumera-poc-web"
echo "Then open:               http://localhost:8787"