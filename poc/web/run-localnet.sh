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
# Passport: lower the minimum agent stake so the funded test accounts (50 LUME)
# can register an identity + top up within the demo. Production default is 100 LUME.
pp=d['app_state'].get('passport')
if pp is not None:
    pp['params']['min_stake']={'denom':'ulume','amount':'10000000'}  # 10 LUME
# Payment rails on-ramp: seed an oracle USDC/USD aggregated price (timestamp =
# genesis_time so it is fresh at boot) so create-deposit can mint LAC from a
# bridged usdc deposit. Without a price the deposit fails "pricing unavailable".
gt=d['genesis_time']
orc=d['app_state'].get('oracle')
if orc is not None:
    orc.setdefault('aggregated_prices',[])
    orc['aggregated_prices'].append({'asset_pair':'USDC/USD','median_price':'1.000000000000000000',
        'mean_price':'1.000000000000000000','standard_deviation':'0.000000000000000000',
        'num_validators':1,'block_height':'0','timestamp':gt})
# Widen the payment_rails oracle-staleness window so the single genesis-seeded
# USDC/USD price (no live validator vote-extension feed on a 1-node localnet)
# stays "fresh" for the whole demo. Same demo-shortcut as the supernode windows.
pr=d['app_state'].get('payment_rails')
if pr is not None:
    pr['params']['oracle_staleness_sec']="1000000000"
json.dump(d,open(p,'w'),indent=1)
PY
# val also holds usdc (the bridged on-ramp asset) so it can fund the test accounts.
"$LD" genesis add-genesis-account "$VAL" 100000000000000ulume,1000000000usdc "${KR[@]}" >/dev/null 2>&1
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
# Fund the 5 test accounts with LUME (each swaps to LAC + pays for tool calls)
# and usdc (the bridged on-ramp asset for the payment_rails deposit panel).
for a in "${ACCTS[@]}"; do
  ADDR=$("$LD" keys show "$a" -a "${KR[@]}")
  waittx "$("$LD" tx bank send "$VAL" "$ADDR" 50000000ulume --from val "${C[@]}" -o json | hash)"
  waittx "$("$LD" tx bank send "$VAL" "$ADDR" 50000000usdc --from val "${C[@]}" -o json | hash)"
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

# Seed the wave-2 module panels so they show live on-chain data on first load
# (every line below is a real tx — the same calls the web panels make).
echo "seeding wave-2 modules (passport · vaults · cac · tournaments · on-ramp · router)…"
# Agent identities (passport): a few accounts register a stake-backed identity.
for entry in "acct1 12000000" "acct2 15000000" "acct4 18000000"; do set -- $entry
  waittx "$("$LD" tx passport register "agent-$1" "${2}ulume" --from "$1" "${C[@]}" -o json | hash)"
done
# Prepaid capacity (vaults): acct1 swaps + opens a bronze vault.
waittx "$("$LD" tx credits swap-lume-to-lac --amount 5000000ulume --min-lac-out 1 --from acct1 "${C[@]}" -o json | hash)"
waittx "$("$LD" tx vaults create --policy-id p1 --tier bronze --amount 1000000ulac --from acct1 "${C[@]}" -o json | hash)"
# Content cache (cac): the supernode stores a cache entry for pubtool.
echo '{"answer":"42","model":"demo"}' > /tmp/lumera-cac-seed.json
waittx "$("$LD" tx cac cache-store /tmp/lumera-cac-seed.json --tool-id pubtool --request-hash seed-req-001 --ttl 3600 --deterministic --royalty-eligible --from val "${C[@]}" -o json | hash)"
# Tournament (challenges): acct2 swaps + opens a performance tournament, two tools join.
waittx "$("$LD" tx credits swap-lume-to-lac --amount 8000000ulume --min-lac-out 1 --from acct2 "${C[@]}" -o json | hash)"
NOW=$(python3 -c "import datetime;print(datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'))")
ENDS=$(python3 -c "import datetime;print((datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(days=1)).strftime('%Y-%m-%dT%H:%M:%SZ'))")
CC=$("$LD" tx challenges create-challenge --title "Latency Cup" --type performance --prize-pool 2000000ulac --entry-fee 1000ulac --starts-at "$NOW" --ends-at "$ENDS" --from acct2 "${C[@]}" -o json | hash)
waittx "$CC"
CID=$("$LD" query challenges challenges --node "$NODE" -o json 2>/dev/null | python3 -c "import sys,json;cs=json.load(sys.stdin).get('challenges',[]);print(cs[0]['challenge_id'] if cs else '')" 2>/dev/null)
if [ -n "$CID" ]; then
  waittx "$("$LD" tx challenges join-challenge --challenge-id "$CID" --tool-id atlas-7b --from acct2 "${C[@]}" -o json | hash)"
  waittx "$("$LD" tx challenges join-challenge --challenge-id "$CID" --tool-id orion-70b --from acct2 "${C[@]}" -o json | hash)"
fi
# On-ramp (payment_rails): acct3 deposits bridged usdc → oracle-priced LAC mint.
waittx "$("$LD" tx payment_rails create-deposit 2000000usdc --tx-hash 0xseedrail001 --request-id rail-seed-001 --confirmations 3 --from acct3 "${C[@]}" -o json | hash)"
# Routing telemetry (router): tool owners record activations (feeds global metrics).
waittx "$("$LD" tx router record-activation pubtool true --session-id seed-sess-1 --from pub "${C[@]}" -o json | hash)"
waittx "$("$LD" tx router record-activation atlas-7b true --session-id seed-sess-1 --from acct1 "${C[@]}" -o json | hash)"

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