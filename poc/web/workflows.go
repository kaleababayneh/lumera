// workflows.go exposes the Composable Intelligence module (x/workflows) in the
// PoC: authors publish multi-step workflow cards, escrow an author bond (tracked
// in module state), upgrade/deactivate versions, and the dashboard lists the
// live on-chain workflow + bond state.
//
// Unlike priority/auction (genesis-only config), workflows has live Msg + Query
// services, so this panel drives real transactions and reads live state. There
// is no "list all" query upstream, so the PoC keeps an in-memory set of the
// workflow ids it has published/seeded and live-queries each one.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
)

// workflowAuthorPubkey mirrors the ed448 pubkey shape the module's static
// validator accepts ("ed448:" + 114 chars). The PoC uses a placeholder key; a
// real author signs their card off-chain with their own ed448 key.
var workflowAuthorPubkey = "ed448:" + strings.Repeat("a", 114)

// wfRef is a workflow id+version the PoC knows about (published or seeded).
type wfRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Author  string `json:"author"`
}

var pocWorkflows = struct {
	sync.Mutex
	items []wfRef
}{}

func rememberWorkflow(ref wfRef) {
	pocWorkflows.Lock()
	defer pocWorkflows.Unlock()
	for _, it := range pocWorkflows.items {
		if it.ID == ref.ID && it.Version == ref.Version {
			return
		}
	}
	pocWorkflows.items = append(pocWorkflows.items, ref)
}

func knownWorkflows() []wfRef {
	pocWorkflows.Lock()
	defer pocWorkflows.Unlock()
	out := make([]wfRef, len(pocWorkflows.items))
	copy(out, pocWorkflows.items)
	return out
}

// buildWorkflowCard writes a minimal-valid WorkflowCard (proto-JSON) for the
// given id/version authored by authorAddr, and returns the temp file path.
func buildWorkflowCard(id, version, authorAddr string) (string, error) {
	card := map[string]any{
		"workflow_id":   id,
		"version":       version,
		"display_name":  "Workflow " + id,
		"description":   "Single-step demo workflow published from the Lumera AI PoC.",
		"author_id":     "author-" + authorAddr,
		"author_pubkey": workflowAuthorPubkey,
		"categories":    []string{"agent-contracts"},
		"license_lane":  "byo_key",
		"dag": []map[string]any{{
			"step_id":                 "step-a",
			"tool_id":                 "tool.alpha",
			"tool_version_constraint": "1.0.0",
			"input_binding":           "$.inputs",
			"max_sub_cost":            map[string]any{"denom": cfg.CreditDenom, "amount": "1"},
			"sub_slo_p95_ms":          1000,
			"retry_policy":            map[string]any{"max_attempts": 1},
			"failure_action":          "FAILURE_ACTION_REVERT_BUNDLE",
			"side_effect":             "SIDE_EFFECT_REVERSIBLE",
		}},
		"input_schema":  `{"type":"object"}`,
		"output_schema": `{"type":"object"}`,
		"pricing": map[string]any{
			"pricing_model": "sum_steps_plus_margin",
			"min_bond":      map[string]any{"denom": cfg.CreditDenom, "amount": "1000000"},
		},
		"passport_requirements": map[string]any{"min_tier": "PASSPORT_TIER_BASIC"},
		"governance": map[string]any{
			"author_addresses": []string{authorAddr},
			"upgrade_policy":   "UPGRADE_POLICY_SEMVER_COMPATIBLE",
		},
		"safety_invariants": []map[string]any{{
			"invariant_id": "total_cost_bound",
			"expression":   "total_cost <= max_cost",
			"phase":        "INVARIANT_PHASE_LOCK",
			"severity":     "error",
			"error_code":   "workflow_cost_exceeded",
			"hint_message": "Keep the locked workflow cost within the signed quote budget.",
		}},
	}
	raw, err := json.Marshal(card)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "wfcard-*.json")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return path, nil
}

// workflowBondDenom/Amount come from the module params (queried lazily).
func workflowParams() map[string]any {
	m, err := query("workflows", "params")
	if err != nil {
		return map[string]any{}
	}
	return m
}

func registerWorkflowsAPIs(mux *http.ServeMux) {
	// Publish a new workflow card + escrow the author bond (recorded in state).
	mux.HandleFunc("/api/workflow-publish", func(w http.ResponseWriter, r *http.Request) {
		name := agentKey(r)
		addr, err := keyAddr(name)
		if err != nil {
			fail(w, err)
			return
		}
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			id = "wf." + name + "." + strings.TrimPrefix(uniqueRef("d"), "d-")[:10]
		}
		version := strings.TrimSpace(r.URL.Query().Get("version"))
		if version == "" {
			version = "1.0.0"
		}
		bond := strings.TrimSpace(r.URL.Query().Get("bond"))
		if bond == "" {
			bond = "1000000" + cfg.CreditDenom
		}
		path, err := buildWorkflowCard(id, version, addr)
		if err != nil {
			fail(w, err)
			return
		}
		defer func() { _ = os.Remove(path) }()
		res, err := broadcast(name, "workflows", "publish-workflow", path, "--bond", bond)
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			rememberWorkflow(wfRef{ID: id, Version: version, Author: addr})
			recordActivity(activityEntry{Type: "workflow", Title: "Workflow published", Sub: id + " v" + version + " · bond " + bond, Account: name, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "workflow-publish", "tx": res, "id": id, "version": version,
			"note": "Published a multi-step workflow card with a " + bond + " author bond (composable intelligence)."})
	})

	// Upgrade an existing workflow to a new semver-compatible version.
	mux.HandleFunc("/api/workflow-upgrade", func(w http.ResponseWriter, r *http.Request) {
		name := agentKey(r)
		addr, err := keyAddr(name)
		if err != nil {
			fail(w, err)
			return
		}
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		from := strings.TrimSpace(r.URL.Query().Get("from"))
		to := strings.TrimSpace(r.URL.Query().Get("to"))
		if id == "" || from == "" || to == "" {
			fail(w, fmt.Errorf("id, from, and to versions are required"))
			return
		}
		path, err := buildWorkflowCard(id, to, addr)
		if err != nil {
			fail(w, err)
			return
		}
		defer func() { _ = os.Remove(path) }()
		res, err := broadcast(name, "workflows", "upgrade-workflow", id, from, path)
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			rememberWorkflow(wfRef{ID: id, Version: to, Author: addr})
			recordActivity(activityEntry{Type: "workflow", Title: "Workflow upgraded", Sub: id + " " + from + " → " + to, Account: name, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "workflow-upgrade", "tx": res, "id": id, "version": to})
	})

	// Deactivate a workflow version (releases its bond lock).
	mux.HandleFunc("/api/workflow-deactivate", func(w http.ResponseWriter, r *http.Request) {
		name := agentKey(r)
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		version := strings.TrimSpace(r.URL.Query().Get("version"))
		if id == "" || version == "" {
			fail(w, fmt.Errorf("id and version are required"))
			return
		}
		reason := strings.TrimSpace(r.URL.Query().Get("reason"))
		if reason == "" {
			reason = "retired from PoC"
		}
		res, err := broadcast(name, "workflows", "deactivate-workflow", id, version, "--reason", reason)
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			recordActivity(activityEntry{Type: "workflow", Title: "Workflow deactivated", Sub: id + " v" + version, Account: name, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "workflow-deactivate", "tx": res})
	})

	// Top up the calling author's bond (module-state accounting).
	mux.HandleFunc("/api/workflow-bond-topup", func(w http.ResponseWriter, r *http.Request) {
		name := agentKey(r)
		amount := strings.TrimSpace(r.URL.Query().Get("amount"))
		if amount == "" {
			amount = "500000" + cfg.CreditDenom
		}
		res, err := broadcast(name, "workflows", "top-up-bond", amount)
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			recordActivity(activityEntry{Type: "workflow", Title: "Author bond topped up", Sub: "+" + amount, Account: name, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "workflow-bond-topup", "tx": res})
	})

	// List the PoC-known workflows (live-queried) + the agent's author bond + params.
	mux.HandleFunc("/api/workflows", func(w http.ResponseWriter, r *http.Request) {
		addr, _ := keyAddr(agentKey(r))
		var list []map[string]any
		for _, ref := range knownWorkflows() {
			m, err := query("workflows", "workflow", ref.ID, ref.Version)
			if err != nil {
				continue
			}
			card, _ := m["card"].(map[string]any)
			list = append(list, map[string]any{
				"id":          ref.ID,
				"version":     ref.Version,
				"status":      mapStr(m, "status"),
				"author":      mapStr(m, "author_address"),
				"displayName": mapStr(card, "display_name"),
				"steps":       workflowStepCount(card),
			})
		}
		out := map[string]any{"ok": true, "workflows": list, "params": workflowParams()}
		if addr != "" {
			if b, err := query("workflows", "author-bond", addr); err == nil {
				out["authorBond"] = b
			}
		}
		writeJSON(w, out)
	})
}

func workflowStepCount(card map[string]any) int {
	if card == nil {
		return 0
	}
	if dag, ok := card["dag"].([]any); ok {
		return len(dag)
	}
	return 0
}

// enrichWorkflowsState adds workflow + author-bond summary to the live state.
func enrichWorkflowsState(st map[string]any, agentAddr string) {
	known := knownWorkflows()
	active := 0
	for _, ref := range known {
		if m, err := query("workflows", "workflow", ref.ID, ref.Version); err == nil {
			if mapStr(m, "status") == "active" {
				active++
			}
		}
	}
	st["workflowsKnown"] = len(known)
	st["workflowsActive"] = active
	if agentAddr != "" {
		if b, err := query("workflows", "author-bond", agentAddr); err == nil {
			if bond, ok := b["bond"].(map[string]any); ok {
				st["workflowAuthorBond"] = mapStr(bond, "amount")
			}
		}
	}
}

// seedKnownWorkflows registers any workflow the localnet seeded so the panel
// shows it on first load. The seed id is a stable constant the run script
// publishes; if it was not seeded, the live query simply skips it.
func seedKnownWorkflows() {
	// Stable demo id published by poc/web/run-localnet.sh (best-effort).
	rememberWorkflow(wfRef{ID: "wf.lumera.echo", Version: "1.0.0"})
}
