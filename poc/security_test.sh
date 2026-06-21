#!/usr/bin/env bash
set -uo pipefail
LD=${LUMERAD:-/tmp/lumerad}; HM=/tmp/lnode_sec; NODE="tcp://localhost:26657"
pkill -f "lumerad start --home /tmp/lnode" 2>/dev/null || true; sleep 2; rm -rf "$HM"
KR=(--keyring-backend test --home "$HM")
C=(--home "$HM" --node "$NODE" --chain-id lumera-local-1 --keyring-backend test --gas 700000 --fees 200000ulume -y)
"$LD" init val --chain-id lumera-local-1 --home "$HM" >/dev/null 2>&1
for k in val pub chl; do "$LD" keys add "$k" "${KR[@]}" --algo eth_secp256k1 >/dev/null 2>&1; done
VAL=$("$LD" keys show val -a "${KR[@]}"); PUB=$("$LD" keys show pub -a "${KR[@]}"); CHL=$("$LD" keys show chl -a "${KR[@]}")
VALOPER=$("$LD" keys show val --bech val -a "${KR[@]}")
"$LD" genesis add-genesis-account "$VAL" 100000000000000ulume "${KR[@]}" >/dev/null 2>&1
"$LD" genesis gentx val 1000000000ulume --chain-id lumera-local-1 "${KR[@]}" >/dev/null 2>&1
"$LD" genesis collect-gentxs --home "$HM" >/dev/null 2>&1
nohup "$LD" start --home "$HM" --minimum-gas-prices=0ulume --log_level error > "$HM/node.log" 2>&1 &
for _ in $(seq 1 40); do "$LD" status --node "$NODE" >/dev/null 2>&1 && break; sleep 0.7; done
grep -qiE "panic:|CONSENSUS FAILURE" "$HM/node.log" && { echo "NODE PANIC"; tail "$HM/node.log"; exit 1; }
py(){ python3 -c "$1"; }
height(){ "$LD" status --node "$NODE" 2>/dev/null | py "import sys,json;d=json.load(sys.stdin);si=d.get('sync_info',d.get('SyncInfo',{}));print(si.get('latest_block_height','0'))" 2>/dev/null||echo 0; }
for _ in $(seq 1 60); do h=$(height); case "$h" in ''|*[!0-9]*) h=0;; esac; [ "$h" -ge 2 ] && break; sleep 0.7; done
echo "node up @h$(height)"
hash(){ py "import sys,json;print(json.load(sys.stdin).get('txhash',''))"; }
wt(){ for _ in $(seq 1 40); do o=$("$LD" query tx "$1" --node "$NODE" -o json 2>/dev/null||true); [ -n "$o" ] && { echo "$o"; return; }; sleep 0.7; done; echo '{}'; }
code(){ py "import sys,json;print(json.load(sys.stdin).get('code'))"; }
log(){ py "import sys,json;print(json.load(sys.stdin).get('raw_log','')[:95])"; }
ev(){ py "import sys,json;d=json.load(sys.stdin);print(next((a['value'] for e in d.get('events',[]) for a in e.get('attributes',[]) if e.get('type')=='$1' and a.get('key')=='$2'),''))"; }
EXPECT_FAIL(){ [ "$1" != "0" ] && [ -n "$1" ] && echo "BLOCKED ✓ (code=$1)" || echo "!!! NOT BLOCKED (code=$1)"; }

wt "$("$LD" tx supernode register-supernode "$VALOPER" 127.0.0.1 "$VAL" --from val "${C[@]}" -o json|hash)" >/dev/null
wt "$("$LD" tx bank send "$VAL" "$PUB" 6000000ulume --from val "${C[@]}" -o json|hash)" >/dev/null
wt "$("$LD" tx bank send "$VAL" "$CHL" 3000000ulume --from val "${C[@]}" -o json|hash)" >/dev/null
wt "$("$LD" tx registry register-tool pubtool --bond 2000000ulume --from pub "${C[@]}" -o json|hash)" >/dev/null
wt "$("$LD" tx credits swap-lume-to-lac --amount 5000000ulume --min-lac-out 1 --from val "${C[@]}" -o json|hash)" >/dev/null
L1=$(wt "$("$LD" tx credits lock-credits --amount 1000000ulac --session-id s1 --tool-id pubtool --quote-id q1 --policy-version policy-v1 --intent-hash i1 --from val "${C[@]}" -o json|hash)")
LID1=$(echo "$L1"|py "import sys,json;d=json.load(sys.stdin);print(next((a['value'] for e in d.get('events',[]) for a in e.get('attributes',[]) if a.get('key')=='lock_id'),''))")
RS=$(wt "$("$LD" tx registry submit-receipt pubtool --model m --input good --result ok --lock-id "$LID1" --from val "${C[@]}" -o json|hash)")
RID=$(echo "$RS"|ev receipt_submitted receipt_id)
echo "setup: lock1=$LID1 receipt=$RID  (publisher=pub supernode/agent=val challenger=chl)"

echo; echo "### FIX #6 — receipt is bound to its lock (cross-lock settle must be BLOCKED)"
L2=$(wt "$("$LD" tx credits lock-credits --amount 1000000ulac --session-id s2 --tool-id pubtool --quote-id q2 --policy-version policy-v1 --intent-hash i2 --from val "${C[@]}" -o json|hash)")
LID2=$(echo "$L2"|py "import sys,json;d=json.load(sys.stdin);print(next((a['value'] for e in d.get('events',[]) for a in e.get('attributes',[]) if a.get('key')=='lock_id'),''))")
X=$(wt "$("$LD" tx credits settle-credits --lock-id "$LID2" --actual-cost 800000ulac --publisher "$PUB" --receipt-id "$RID" --tool-id pubtool --from val "${C[@]}" -o json|hash)")
echo "   settle lock2 with lock1's receipt: $(EXPECT_FAIL "$(echo "$X"|code)")  log=$(echo "$X"|log)"

echo; echo "### FIX #4 — minimum challenger stake (1ulume challenge must be BLOCKED)"
X=$(wt "$("$LD" tx registry challenge-receipt "$RID" 1ulume --from chl "${C[@]}" -o json|hash)")
echo "   challenge with 1ulume: $(EXPECT_FAIL "$(echo "$X"|code)")  log=$(echo "$X"|log)"

echo; echo "### FIX (disjoint) — publisher cannot challenge its own tool's receipt"
X=$(wt "$("$LD" tx registry challenge-receipt "$RID" 500000ulume --from pub "${C[@]}" -o json|hash)")
echo "   publisher self-challenge: $(EXPECT_FAIL "$(echo "$X"|code)")  log=$(echo "$X"|log)"

echo; echo "### FIX #1/#5 — self-adjudication (challenger == adjudicator) must be BLOCKED"
# val (the supernode) files a challenge, then tries to uphold it itself.
CR=$(wt "$("$LD" tx registry challenge-receipt "$RID" 500000ulume --from val "${C[@]}" -o json|hash)")
echo "   val opens a challenge (as challenger): code=$(echo "$CR"|code)"
X=$(wt "$("$LD" tx registry resolve-dispute "$RID" --from val "${C[@]}" -o json|hash)")
echo "   val self-upholds (as adjudicator): $(EXPECT_FAIL "$(echo "$X"|code)")  log=$(echo "$X"|log)"
echo "   bond intact (not stolen): bonded=$("$LD" query registry get-bond pubtool --node "$NODE" -o json 2>/dev/null|py "import sys,json;b=json.load(sys.stdin).get('bond',{});print(next((c['amount'] for c in b.get('bonded_amount',[]) if c['denom']=='ulume'),'0'))")ulume (expect 2000000)"
echo "DONE"
pkill -f "lumerad start --home /tmp/lnode" 2>/dev/null || true