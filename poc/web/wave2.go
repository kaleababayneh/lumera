// wave2.go adds native PoC support for the second wave of integrated modules:
// vaults (prepaid capacity), passport (agent identity/stake), cac (cache),
// challenges (tournaments), payment_rails (on-ramp), and router (telemetry).
//
// Every endpoint here is a REAL on-chain call shelled through `lumerad` — the
// same server-side-signing pattern as the core loop in main.go. No simulation.
package main

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ---- shared helpers ---------------------------------------------------------

// knownKeys is the full set of keyring names the PoC server can sign as.
func knownKeys() []string {
	return append([]string{cfg.Agent, cfg.Publisher, cfg.Challenger}, selectableAccounts...)
}

// keyNameForAddr reverse-maps an on-chain address back to the keyring name that
// owns it (used so router activation records are signed by the tool's owner,
// which the router keeper requires).
func keyNameForAddr(addr string) string {
	if addr == "" {
		return ""
	}
	seen := map[string]bool{}
	for _, n := range knownKeys() {
		if seen[n] {
			continue
		}
		seen[n] = true
		if a, err := keyAddr(n); err == nil && a == addr {
			return n
		}
	}
	return ""
}

// agentPubkeyFor derives a deterministic passport agent pubkey per account so a
// browser acting as a given account always maps to the same on-chain passport.
func agentPubkeyFor(name string) string { return "agent-" + name }

// ensureLac swaps LUME→LAC for the given account if its LAC balance is below
// need, so capacity/prize/vault flows never fail for lack of credits. Mirrors
// the inline swap in the /api/call loop.
func ensureLac(name, addr string, need int64) error {
	if have, _ := strconv.ParseInt(balance(addr, cfg.CreditDenom), 10, 64); have >= need {
		return nil
	}
	_, err := broadcast(name, "credits", "swap-lume-to-lac", "--amount", "5000000ulume", "--min-lac-out", "1")
	return err
}

// uniqueRef returns a short unique reference (for deposit tx-hash / request-id).
func uniqueRef(prefix string) string {
	h := hex.EncodeToString(sha256Bytes(prefix + strconv.FormatInt(time.Now().UnixNano(), 10)))
	return prefix + "-" + h[:16]
}

// firstString returns the first non-empty value plucked from a nested map path.
func mapStr(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// ---- state enrichment (merged into /api/state every poll) -------------------

func enrichWave2State(st map[string]any, agentName, agentAddr string) {
	// passport — the acting agent's on-chain identity + stake.
	if m, err := query("passport", "by-agent", agentPubkeyFor(agentName)); err == nil {
		if p, ok := m["passport"].(map[string]any); ok {
			st["passportId"] = mapStr(p, "passport_id")
			st["passportStatus"] = mapStr(p, "status")
			if stk, ok := p["stake"].(map[string]any); ok {
				st["passportStake"] = mapStr(stk, "amount")
			}
		}
	}
	// vaults — count + total remaining prepaid capacity for the acting agent.
	if m, err := query("vaults", "vaults", "--owner", agentAddr); err == nil {
		if vs, ok := m["vaults"].([]any); ok {
			st["vaultCount"] = len(vs)
			var rem int64
			for _, v := range vs {
				if vm, ok := v.(map[string]any); ok {
					if ra, ok := vm["remaining_amount"].(map[string]any); ok {
						n, _ := strconv.ParseInt(mapStr(ra, "amount"), 10, 64)
						rem += n
					}
				}
			}
			st["vaultRemaining"] = rem
		}
	}
	// cac — global cache stats.
	if m, err := query("cac", "stats"); err == nil {
		if s, ok := m["stats"].(map[string]any); ok {
			st["cacEntries"] = mapStr(s, "total_entries")
			st["cacHits"] = mapStr(s, "hit_count")
			st["cacMisses"] = mapStr(s, "miss_count")
			st["cacHitRate"] = mapStr(s, "hit_rate")
		}
	}
	// challenges — live tournament count.
	if m, err := query("challenges", "challenges"); err == nil {
		if cs, ok := m["challenges"].([]any); ok {
			st["tournamentCount"] = len(cs)
		}
	}
	// payment_rails — the acting agent's deposit count + total LAC minted.
	if m, err := query("payment_rails", "deposits", "--user", agentAddr); err == nil {
		if ds, ok := m["deposits"].([]any); ok {
			st["depositCount"] = len(ds)
		}
	}
	// router — routing telemetry. Active tools is the per-tool signal populated
	// directly by RecordActivation (owner-signable); global aggregates only roll
	// up via the authority-gated AggregateMetrics, so they are best-effort.
	if m, err := query("router", "active-tools"); err == nil {
		if ids, ok := m["tool_ids"].([]any); ok {
			st["routerActiveTools"] = len(ids)
		}
	}
	if m, err := query("router", "global-metrics"); err == nil {
		if gm, ok := m["metrics"].(map[string]any); ok {
			st["routerInvocations"] = mapStr(gm, "total_invocations")
			st["routerCacheHits"] = mapStr(gm, "total_cache_hits")
		}
	}
	// module-account escrow balances (capacity/prize/stake at rest).
	st["vaultsEscrow"] = balance(moduleAddr("vaults"), cfg.CreditDenom)
	st["challengesEscrow"] = balance(moduleAddr("challenges"), cfg.CreditDenom)
	st["passportStaked"] = balance(moduleAddr("passport"), cfg.LumeDenom)
	st["railsUsdc"] = balance(agentAddr, "usdc")
}

// ---- handlers ---------------------------------------------------------------

func registerWave2APIs(mux *http.ServeMux) {
	// -------- vaults: prepaid capacity --------
	mux.HandleFunc("/api/vault-create", func(w http.ResponseWriter, r *http.Request) {
		name := agentKey(r)
		addr, _ := keyAddr(name)
		tier := strings.TrimSpace(r.URL.Query().Get("tier"))
		if tier == "" {
			tier = "bronze"
		}
		amount := strings.TrimSpace(r.URL.Query().Get("amount"))
		if amount == "" {
			amount = "1000000" + cfg.CreditDenom
		}
		policy := strings.TrimSpace(r.URL.Query().Get("policy"))
		if policy == "" {
			policy = "p1"
		}
		if err := ensureLac(name, addr, 1500000); err != nil {
			fail(w, err)
			return
		}
		res, err := broadcast(name, "vaults", "create", "--policy-id", policy, "--tier", tier, "--amount", amount)
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			recordActivity(activityEntry{Type: "vault", Title: "Prepaid vault created", Sub: tier + " · " + amount, Account: name, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "vault-create", "tx": res,
			"note": "Prepaid " + amount + " into a " + tier + " vault (discounted capacity drawn down per call)."})
	})
	mux.HandleFunc("/api/vaults", func(w http.ResponseWriter, r *http.Request) {
		addr, _ := keyAddr(agentKey(r))
		m, err := query("vaults", "vaults", "--owner", addr)
		if err != nil {
			writeJSON(w, map[string]any{"ok": true, "vaults": []any{}})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "vaults": m["vaults"]})
	})

	// -------- passport: agent identity / stake --------
	mux.HandleFunc("/api/passport-register", func(w http.ResponseWriter, r *http.Request) {
		name := agentKey(r)
		stake := strings.TrimSpace(r.URL.Query().Get("stake"))
		if stake == "" {
			stake = "10000000" + cfg.LumeDenom // 10 LUME (genesis min_stake is lowered for the demo)
		}
		res, err := broadcast(name, "passport", "register", agentPubkeyFor(name), stake)
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			recordActivity(activityEntry{Type: "passport", Title: "Agent passport registered", Sub: agentPubkeyFor(name) + " · staked " + stake, Account: name, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "passport-register", "tx": res,
			"note": "Registered a stake-backed agent identity for " + name + " (slashable on disputes)."})
	})
	mux.HandleFunc("/api/passport-topup", func(w http.ResponseWriter, r *http.Request) {
		name := agentKey(r)
		amount := strings.TrimSpace(r.URL.Query().Get("amount"))
		if amount == "" {
			amount = "5000000" + cfg.LumeDenom
		}
		pid := strings.TrimSpace(r.URL.Query().Get("id"))
		if pid == "" {
			if m, err := query("passport", "by-agent", agentPubkeyFor(name)); err == nil {
				if p, ok := m["passport"].(map[string]any); ok {
					pid = mapStr(p, "passport_id")
				}
			}
		}
		if pid == "" {
			fail(w, fmt.Errorf("no passport registered for %s yet — register first", name))
			return
		}
		res, err := broadcast(name, "passport", "top-up", pid, amount)
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			recordActivity(activityEntry{Type: "passport", Title: "Passport stake topped up", Sub: pid + " · +" + amount, Account: name, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "passport-topup", "tx": res, "note": "Increased agent stake for " + pid + "."})
	})
	mux.HandleFunc("/api/passport", func(w http.ResponseWriter, r *http.Request) {
		name := agentKey(r)
		m, err := query("passport", "by-agent", agentPubkeyFor(name))
		if err != nil {
			writeJSON(w, map[string]any{"ok": true, "passport": nil})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "passport": m["passport"]})
	})

	// -------- cac: content-addressed cache --------
	mux.HandleFunc("/api/cac-stats", func(w http.ResponseWriter, r *http.Request) {
		m, err := query("cac", "stats")
		if err != nil {
			writeJSON(w, map[string]any{"ok": true, "stats": nil})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "stats": m["stats"]})
	})
	mux.HandleFunc("/api/cac-lookup", func(w http.ResponseWriter, r *http.Request) {
		req := strings.TrimSpace(r.URL.Query().Get("request"))
		if req == "" {
			fail(w, fmt.Errorf("request hash required"))
			return
		}
		m, err := query("cac", "lookup", req)
		if err != nil {
			writeJSON(w, map[string]any{"ok": true, "entries": []any{}, "note": "no cache entry for that request hash"})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "entries": m["entries"]})
	})

	// -------- challenges: grand-challenge tournaments --------
	mux.HandleFunc("/api/tournament-create", func(w http.ResponseWriter, r *http.Request) {
		name := agentKey(r)
		addr, _ := keyAddr(name)
		title := strings.TrimSpace(r.URL.Query().Get("title"))
		if title == "" {
			title = "Latency Cup"
		}
		prize := strings.TrimSpace(r.URL.Query().Get("prize"))
		if prize == "" {
			prize = "2000000" + cfg.CreditDenom
		}
		entry := strings.TrimSpace(r.URL.Query().Get("entry"))
		if entry == "" {
			entry = "1000" + cfg.CreditDenom
		}
		if err := ensureLac(name, addr, 2500000); err != nil {
			fail(w, err)
			return
		}
		now := time.Now().UTC()
		res, err := broadcast(name, "challenges", "create-challenge",
			"--title", title, "--type", "performance",
			"--prize-pool", prize, "--entry-fee", entry,
			"--starts-at", now.Add(-1*time.Minute).Format(time.RFC3339),
			"--ends-at", now.Add(24*time.Hour).Format(time.RFC3339))
		if err != nil {
			fail(w, err)
			return
		}
		var cid string
		for _, ev := range res.Events {
			if ev.Key == "challenge_id" {
				cid = ev.Val
			}
		}
		if res.OK {
			recordActivity(activityEntry{Type: "tournament", Title: "Tournament created", Sub: title + " · prize " + prize, Account: name, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "tournament-create", "tx": res, "challengeId": cid,
			"note": "Created tournament " + cid + " with an escrowed prize pool of " + prize + "."})
	})
	mux.HandleFunc("/api/tournament-join", func(w http.ResponseWriter, r *http.Request) {
		name := agentKey(r)
		addr, _ := keyAddr(name)
		cid := strings.TrimSpace(r.URL.Query().Get("id"))
		tool := toolID(r)
		if cid == "" {
			fail(w, fmt.Errorf("challenge id required"))
			return
		}
		if err := ensureLac(name, addr, 100000); err != nil {
			fail(w, err)
			return
		}
		res, err := broadcast(name, "challenges", "join-challenge", "--challenge-id", cid, "--tool-id", tool)
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			recordActivity(activityEntry{Type: "tournament", Title: "Joined tournament", Sub: tool + " → " + cid, Account: name, Tool: tool, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "tournament-join", "tx": res, "note": tool + " entered tournament " + cid + "."})
	})
	mux.HandleFunc("/api/tournaments", func(w http.ResponseWriter, r *http.Request) {
		m, err := query("challenges", "challenges")
		if err != nil {
			writeJSON(w, map[string]any{"ok": true, "challenges": []any{}})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "challenges": m["challenges"]})
	})
	mux.HandleFunc("/api/tournament-leaderboard", func(w http.ResponseWriter, r *http.Request) {
		cid := strings.TrimSpace(r.URL.Query().Get("id"))
		if cid == "" {
			fail(w, fmt.Errorf("challenge id required"))
			return
		}
		m, err := query("challenges", "leaderboard", cid)
		if err != nil {
			writeJSON(w, map[string]any{"ok": true, "rankings": []any{}})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "rankings": m["rankings"]})
	})

	// -------- payment_rails: bridged-asset on-ramp --------
	mux.HandleFunc("/api/rails-deposit", func(w http.ResponseWriter, r *http.Request) {
		name := agentKey(r)
		amount := strings.TrimSpace(r.URL.Query().Get("amount"))
		if amount == "" {
			amount = "1000000usdc"
		}
		res, err := broadcast(name, "payment_rails", "create-deposit", amount,
			"--tx-hash", uniqueRef("0xrail"), "--request-id", uniqueRef("req"), "--confirmations", "3")
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			recordActivity(activityEntry{Type: "rails", Title: "On-ramp deposit → LAC", Sub: amount + " bridged", Account: name, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "rails-deposit", "tx": res,
			"note": "Deposited " + amount + " → oracle-priced LAC minted (minus the acquisition fee)."})
	})
	mux.HandleFunc("/api/rails-deposits", func(w http.ResponseWriter, r *http.Request) {
		addr, _ := keyAddr(agentKey(r))
		m, err := query("payment_rails", "deposits", "--user", addr)
		if err != nil {
			writeJSON(w, map[string]any{"ok": true, "deposits": []any{}})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "deposits": m["deposits"]})
	})

	// -------- router: routing telemetry --------
	mux.HandleFunc("/api/router-metrics", func(w http.ResponseWriter, r *http.Request) {
		out := map[string]any{"ok": true}
		if m, err := query("router", "global-metrics"); err == nil {
			out["global"] = m["metrics"]
		}
		if m, err := query("router", "active-tools"); err == nil {
			out["activeTools"] = m["tool_ids"]
		}
		writeJSON(w, out)
	})
	// record-activation: signed by the tool's OWNER (the router keeper requires
	// the module authority or the tool owner). We resolve the owner from the
	// registry and sign as them, so the activation telemetry is genuine.
	mux.HandleFunc("/api/router-activate", func(w http.ResponseWriter, r *http.Request) {
		tool := toolID(r)
		ownerAddr := ""
		if m, err := query("registry", "get-tool", tool); err == nil {
			if t, ok := m["tool"].(map[string]any); ok {
				ownerAddr = mapStr(t, "owner")
			}
		}
		ownerKey := keyNameForAddr(ownerAddr)
		if ownerKey == "" {
			fail(w, fmt.Errorf("cannot record activation: owner of %s is not a local key", tool))
			return
		}
		activated := r.URL.Query().Get("off") == ""
		session := uniqueRef("sess")
		res, err := broadcast(ownerKey, "router", "record-activation", tool, strconv.FormatBool(activated), "--session-id", session)
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			recordActivity(activityEntry{Type: "router", Title: "Routing telemetry recorded", Sub: tool + " activation", Account: ownerKey, Tool: tool, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "router-activate", "tx": res,
			"note": "Recorded a routing activation for " + tool + " (feeds the router's per-tool + global metrics)."})
	})
}
