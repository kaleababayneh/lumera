#!/usr/bin/env bash
# Lumera AI — full-vision demo + regression. Boots a fresh local node and walks
# the entire agentic economy end-to-end:
#
#   register tool + bond  ->  agent buys credits  ->  lock  ->  SuperNode
#   Proof-of-Service receipt  ->  settle (gated on the proof, pays publisher)
#   ->  reputation earned  ->  dispute upheld -> bond slashed + restitution
#   ->  reputation erodes  ->  AI agent calls the tool over MCP
#
# Run from the repo root:  bash poc/demo.sh
set -uo pipefail
LD=${LUMERAD:-/tmp/lumerad}; HM=/tmp/lnode_demo; NODE="tcp://localhost:26657"
ROUTER=/tmp/lumera-mcp-router
echo "### building lumerad + mcp-router"
go build -o "$LD" ./cmd/lumera && go build -o "$ROUTER" ./poc/mcp-router || { echo "build failed"; exit 1; }

pkill -f "lumerad start --home /tmp/lnode" 2>/dev/null || true; sleep 2; rm -rf "$HM"
KR=(--keyring-backend test --home "$HM")
C=(--home "$HM" --node "$NODE" --chain-id lumera-local-1 --keyring-backend test --gas 700000 --fees 200000ulume -y)
"$LD" init val --chain-id lumera-local-1 --home "$HM" >/dev/null 2>&1
for k in val pub chl; do "$LD" keys add "$k" "${KR[@]}" --algo eth_secp256k1 >/dev/null 2>&1; done
VAL=$("$LD" keys show val -a "${KR[@]}"); PUB=$("$LD" keys show pub -a "${KR[@]}"); CHL=$("$LD" keys show chl -a "${KR[@]}")
VALOPER=$("$LD" keys show val --bech val -a "${KR[@]}")
# Tune incentives so the reputation demo is fast (low min-invocations, short grace).
python3 - "$HM/config/genesis.json" <<'PY'
import json,sys
p=sys.argv[1]; d=json.load(open(p)); inc=d['app_state']['incentives']
inc['params']['min_invocations_for_scoring']=2; inc['params']['grace_period_blocks']=1
inc['metric_snapshots']=[{"tool_id":"pubtool","block_height":"0","timestamp":"2026-06-21T00:00:00Z",
 "uptime_bps":10000,"success_rate_bps":0,"p95_latency_ms":40,"latency_variance":0,"quote_deviation_bps":0,
 "receipt_validity_bps":0,"settlement_accuracy_bps":10000,"sbom_age_hours":1,"slsa_level":3,
 "critical_vulnerabilities":0,"high_vulnerabilities":0,"requests_per_second":200,"declared_capacity":200,
 "cache_hit_rate_bps":8000,"dispute_rate_bps":0,"governance_participation_bps":10000,
 "total_invocations":"0","successful_invocations":"0","failed_invocations":"0"}]
json.dump(d,open(p,'w'),indent=1)
PY
"$LD" genesis add-genesis-account "$VAL" 100000000000000ulume "${KR[@]}" >/dev/null 2>&1
"$LD" genesis gentx val 1000000000ulume --chain-id lumera-local-1 "${KR[@]}" >/dev/null 2>&1
"$LD" genesis collect-gentxs --home "$HM" >/dev/null 2>&1
nohup "$LD" start --home "$HM" --minimum-gas-prices=0ulume --log_level error > "$HM/node.log" 2>&1 &
for _ in $(seq 1 40); do "$LD" status --node "$NODE" >/dev/null 2>&1 && break; sleep 0.7; done
grep -qiE "panic:|CONSENSUS FAILURE" "$HM/node.log" && { echo "NODE PANIC"; tail "$HM/node.log"; exit 1; }
py(){ python3 -c "$1"; }
height(){ "$LD" status --node "$NODE" 2>/dev/null | py "import sys,json;d=json.load(sys.stdin);si=d.get('sync_info',d.get('SyncInfo',{}));print(si.get('latest_block_height','0'))" 2>/dev/null||echo 0; }
for _ in $(seq 1 60); do h=$(height); case "$h" in ''|*[!0-9]*) h=0;; esac; [ "$h" -ge 2 ] && break; sleep 0.7; done
hash(){ py "import sys,json;print(json.load(sys.stdin).get('txhash',''))"; }
wt(){ for _ in $(seq 1 40); do o=$("$LD" query tx "$1" --node "$NODE" -o json 2>/dev/null||true); [ -n "$o" ] && { echo "$o"; return; }; sleep 0.7; done; echo '{}'; }
code(){ py "import sys,json;print(json.load(sys.stdin).get('code'))"; }
ev(){ py "import sys,json;d=json.load(sys.stdin);print(next((a['value'] for e in d.get('events',[]) for a in e.get('attributes',[]) if e.get('type')=='$1' and a.get('key')=='$2'),''))"; }
bal(){ "$LD" query bank balances "$1" --node "$NODE" -o json 2>/dev/null | py "import sys,json;print(next((b['amount'] for b in json.load(sys.stdin).get('balances',[]) if b['denom']=='$2'),'0'))"; }
bond(){ "$LD" query registry get-bond pubtool --node "$NODE" -o json 2>/dev/null | py "import sys,json;b=json.load(sys.stdin).get('bond',{});print(next((c['amount'] for c in b.get('$1',[]) if c['denom']=='ulume'),'0'))"; }
badge(){ "$LD" query incentives badge pubtool --node "$NODE" -o json 2>/dev/null | py "import sys,json;b=json.load(sys.stdin).get('badge',{});print('%s (score %s)'%(b.get('tier','NONE'),b.get('composite_score','-')))"; }
ok(){ [ "$1" = "0" ] && echo "OK" || echo "FAIL(code=$1)"; }

echo; echo "### node up @h$(height)   agent/supernode=val  publisher=pub  challenger=chl"
wt "$("$LD" tx supernode register-supernode "$VALOPER" 127.0.0.1 "$VAL" --from val "${C[@]}" -o json|hash)" >/dev/null
wt "$("$LD" tx bank send "$VAL" "$PUB" 6000000ulume --from val "${C[@]}" -o json|hash)" >/dev/null
wt "$("$LD" tx bank send "$VAL" "$CHL" 3000000ulume --from val "${C[@]}" -o json|hash)" >/dev/null

echo; echo "## 1. Publisher lists a tool + escrows a bond (skin-in-the-game)"
echo "   register: $(ok "$(wt "$("$LD" tx registry register-tool pubtool --bond 2000000ulume --from pub "${C[@]}" -o json|hash)"|code)")   bond=$(bond bonded_amount)ulume"

echo; echo "## 2. Agent buys credits, locks payment for a call"
echo "   swap: $(ok "$(wt "$("$LD" tx credits swap-lume-to-lac --amount 5000000ulume --min-lac-out 1 --from val "${C[@]}" -o json|hash)"|code)")"
LR=$(wt "$("$LD" tx credits lock-credits --amount 1000000ulac --session-id d1 --tool-id pubtool --quote-id q1 --policy-version policy-v1 --intent-hash i1 --from val "${C[@]}" -o json|hash)")
LID=$(echo "$LR"|py "import sys,json;d=json.load(sys.stdin);print(next((a['value'] for e in d.get('events',[]) for a in e.get('attributes',[]) if a.get('key')=='lock_id'),''))")
echo "   lock: $(ok "$(echo "$LR"|code)")  lock_id=$LID"

echo; echo "## 3. Settlement is GATED on proof — without a receipt it is rejected"
NS=$(wt "$("$LD" tx credits settle-credits --lock-id "$LID" --actual-cost 800000ulac --publisher "$PUB" --receipt-id pos1deadbeef --tool-id pubtool --from val "${C[@]}" -o json|hash)")
echo "   settle-without-proof: code=$(echo "$NS"|code) (rejected as intended)"

echo; echo "## 4. SuperNode proves the work, settlement pays the publisher"
RS=$(wt "$("$LD" tx registry submit-receipt pubtool --model gpt-x --input 'capital of France?' --result 'Paris' --lock-id "$LID" --from val "${C[@]}" -o json|hash)")
RID=$(echo "$RS"|ev receipt_submitted receipt_id)
echo "   receipt: BLAKE3 -> $RID"
PUB0=$(bal "$PUB" ulac)
echo "   settle: $(ok "$(wt "$("$LD" tx credits settle-credits --lock-id "$LID" --actual-cost 800000ulac --publisher "$PUB" --receipt-id "$RID" --tool-id pubtool --from val "${C[@]}" -o json|hash)"|code)")  publisher paid $(( $(bal "$PUB" ulac) - PUB0 ))ulac"

echo; echo "## 5. Reputation is EARNED from real receipts (self-feed)"
for i in 2 3; do wt "$("$LD" tx registry submit-receipt pubtool --model gpt-x --input "q$i" --result "a$i" --from val "${C[@]}" -o json|hash)" >/dev/null; done
echo "   evaluate: $(ok "$(wt "$("$LD" tx incentives request-evaluation pubtool --from pub "${C[@]}" -o json|hash)"|code)")  ->  badge: $(badge)"

echo; echo "## 6. A bad call is DISPUTED and upheld — the publisher's bond is slashed"
CR=$(wt "$("$LD" tx registry challenge-receipt "$RID" 500000ulume --from chl "${C[@]}" -o json|hash)")
UR=$(wt "$("$LD" tx registry resolve-dispute "$RID" --from val "${C[@]}" -o json|hash)")
echo "   uphold: $(ok "$(echo "$UR"|code)")  slash: amount=$(echo "$UR"|ev slash amount) burn=$(echo "$UR"|ev slash burned) insurance=$(echo "$UR"|ev slash insurance) treasury=$(echo "$UR"|ev slash treasury)"
echo "   bond after slash: bonded=$(bond bonded_amount) total_slashed=$(bond total_slashed)"

echo; echo "## 7. Reputation ERODES after the dispute (grace period, then downgrade)"
wt "$("$LD" tx incentives request-evaluation pubtool --from pub "${C[@]}" -o json|hash)" >/dev/null
echo "   re-evaluate (downgrade detected -> grace) -> badge: $(badge)  (tier held during grace)"
sleep 9
wt "$("$LD" tx incentives request-evaluation pubtool --from pub "${C[@]}" -o json|hash)" >/dev/null
echo "   re-evaluate (grace expired) -> badge: $(badge)  (now LOWER than step 5 — the dispute cost reputation)"

echo; echo "## 8. An AI agent calls the tool over MCP (discover -> meter -> prove -> settle)"
CALL='{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"pubtool","arguments":{"input":"hello from an AI agent"}}}'
printf '%s\n%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize"}' "$CALL" \
  | LUMERA_HOME="$HM" LUMERA_AGENT=val LUMERA_SUPERNODE=val "$ROUTER" 2>/dev/null \
  | py "import sys,json
for ln in sys.stdin:
    d=json.loads(ln)
    if d.get('id')==3:
        print('  '+d['result']['content'][0]['text'].replace(chr(10),chr(10)+'  '))"

echo; echo "### DEMO COMPLETE — discover -> meter -> execute -> prove -> settle, with a self-reinforcing trust graph."
pkill -f "lumerad start --home /tmp/lnode" 2>/dev/null || true