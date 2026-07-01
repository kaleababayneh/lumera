// Command lumera-mcp-router is the off-chain agent layer for the Lumera AI
// economy: an MCP (Model Context Protocol) server that exposes on-chain tools to
// any MCP-compatible AI agent. When an agent calls a tool, the router runs the
// full economic + verification loop against a local lumera node:
//
//	lock credits -> execute the tool (off-chain) -> submit a SuperNode
//	Proof-of-Service receipt (BLAKE3(input,model,output)) -> settle (pay the
//	publisher) -> return the result + its on-chain proof.
//
// This is the "router" pivot from the integration plan: its real form is this
// off-chain daemon, not a node module. It speaks JSON-RPC 2.0 over stdio (the
// MCP stdio transport): stdout carries protocol messages, stderr carries logs.
//
// It is a PoC: it drives the chain by shelling `lumerad` with the test keyring,
// and tool "execution" is a placeholder transform — real tools plug in at
// executeTool(). The novel part (meter + prove + settle every call) is real.
package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"lukechampine.com/blake3"
)

// ---- config -----------------------------------------------------------------

type config struct {
	Bin, Home, Node, ChainID string
	Agent, Supernode         string // key names: the paying agent/router, and the attesting supernode
	Fees, Gas                string
	LockAmount, ActualCost   string // ulac
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

var cfg = config{
	Bin:        envOr("LUMERAD", "/tmp/lumerad"),
	Home:       envOr("LUMERA_HOME", "/tmp/lumera-web"),
	Node:       envOr("LUMERA_NODE", "tcp://localhost:26657"),
	ChainID:    envOr("LUMERA_CHAIN_ID", "lumera-local-1"),
	Agent:      envOr("LUMERA_AGENT", "val"),
	Supernode:  envOr("LUMERA_SUPERNODE", "val"),
	Fees:       envOr("LUMERA_FEES", "200000ulume"),
	Gas:        envOr("LUMERA_GAS", "700000"),
	LockAmount: envOr("LUMERA_LOCK", "1000000ulac"),
	ActualCost: envOr("LUMERA_COST", "800000ulac"),
}

func logf(format string, a ...any) { fmt.Fprintf(os.Stderr, "[mcp-router] "+format+"\n", a...) }

// ---- chain helpers (shell lumerad) ------------------------------------------

func run(args ...string) (string, error) {
	cmd := exec.Command(cfg.Bin, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%v: %s", err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

func query(args ...string) (map[string]any, error) {
	full := append([]string{"query"}, args...)
	full = append(full, "--node", cfg.Node, "--home", cfg.Home, "-o", "json")
	out, err := run(full...)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return m, nil
}

// queryRetry wraps query for reads that must succeed (tool discovery, balances):
// the node can be briefly busy right after a burst of txs, so a single transient
// failure should not be mistaken for "no data". It does NOT mask a node that is
// truly down — after the attempts it returns the last error.
func queryRetry(args ...string) (map[string]any, error) {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		m, err := query(args...)
		if err == nil {
			return m, nil
		}
		lastErr = err
		time.Sleep(time.Duration(300+200*attempt) * time.Millisecond)
	}
	return nil, lastErr
}

// broadcast runs a tx from key `from`, waits for inclusion, and returns
// (code, events, rawlog). It retries only failures that are provably *not*
// committed (a CheckTx rejection or a tx that never left the client), so a
// non-idempotent message (lock/settle) is never silently double-submitted.
func broadcast(from string, txArgs ...string) (int, []map[string]any, string, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		code, events, rawlog, retryable, err := broadcastOnce(from, txArgs...)
		if err == nil {
			return code, events, rawlog, nil
		}
		if !retryable {
			return code, events, rawlog, err
		}
		lastErr = err
		logf("transient broadcast failure (attempt %d): %v — retrying", attempt+1, err)
		time.Sleep(time.Duration(1200+600*attempt) * time.Millisecond)
	}
	return -1, nil, "", fmt.Errorf("after retries: %w", lastErr)
}

// broadcastOnce performs a single tx submission. retryable is true only when the
// tx definitely did not make it into a block (the CLI errored before broadcast,
// or CheckTx rejected it for a recoverable reason such as a sequence mismatch).
func broadcastOnce(from string, txArgs ...string) (code int, events []map[string]any, rawlog string, retryable bool, err error) {
	args := append([]string{"tx"}, txArgs...)
	args = append(args, "--from", from, "--home", cfg.Home, "--node", cfg.Node,
		"--chain-id", cfg.ChainID, "--keyring-backend", "test", "--gas", cfg.Gas,
		"--fees", cfg.Fees, "-y", "-o", "json")
	out, runErr := run(args...)
	if runErr != nil {
		// The CLI failed before/at broadcast — nothing entered the mempool.
		return -1, nil, "", isTransient(runErr.Error()), runErr
	}
	var bc map[string]any
	if jerr := json.Unmarshal([]byte(out), &bc); jerr != nil {
		return -1, nil, "", false, fmt.Errorf("decode broadcast: %w", jerr)
	}
	hash, _ := bc["txhash"].(string)
	if hash == "" {
		return -1, nil, "", false, fmt.Errorf("no txhash in broadcast response")
	}
	// CheckTx verdict carried in the sync broadcast response: a non-zero code
	// here means the tx was rejected and will never be indexed — surface it now
	// instead of polling for 20s. Sequence mismatches are recoverable.
	if c, ok := bc["code"].(float64); ok && c != 0 {
		rl, _ := bc["raw_log"].(string)
		return int(c), nil, rl, isTransient(rl), fmt.Errorf("tx rejected at checktx (code %d): %s", int(c), rl)
	}
	// Accepted into the mempool — poll for the committed result.
	for i := 0; i < 40; i++ {
		m, qerr := query("tx", hash)
		if qerr == nil && m != nil {
			cd := 0
			if c, ok := m["code"].(float64); ok {
				cd = int(c)
			}
			rl, _ := m["raw_log"].(string)
			var evs []map[string]any
			if raw, ok := m["events"].([]any); ok {
				for _, e := range raw {
					if em, ok := e.(map[string]any); ok {
						evs = append(evs, em)
					}
				}
			}
			return cd, evs, rl, false, nil
		}
		time.Sleep(700 * time.Millisecond)
	}
	// Accepted but not seen within the window (e.g. the node restarted mid-poll).
	// Do NOT retry — the tx may yet be committed; re-sending could double-spend.
	return -1, nil, "", false, fmt.Errorf("tx %s accepted but not committed within ~28s (node may have restarted)", hash)
}

// isTransient reports whether a failure message describes a recoverable
// condition where the tx is known not to have committed.
func isTransient(msg string) bool {
	m := strings.ToLower(msg)
	for _, s := range []string{
		"account sequence mismatch",
		"connection refused",
		"timed out",
		"timeout",
		"context deadline exceeded",
		"i/o timeout",
		"eof",
	} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

func eventAttr(events []map[string]any, etype, key string) string {
	for _, e := range events {
		if t, _ := e["type"].(string); t != etype {
			continue
		}
		attrs, _ := e["attributes"].([]any)
		for _, a := range attrs {
			am, _ := a.(map[string]any)
			if k, _ := am["key"].(string); k == key {
				v, _ := am["value"].(string)
				return v
			}
		}
	}
	return ""
}

func keyAddr(name string) string {
	out, _ := run("keys", "show", name, "-a", "--home", cfg.Home, "--keyring-backend", "test")
	return strings.TrimSpace(out)
}

func balance(addr, denom string) int64 {
	m, err := query("bank", "balances", addr)
	if err != nil {
		return 0
	}
	bals, _ := m["balances"].([]any)
	for _, b := range bals {
		bm, _ := b.(map[string]any)
		if d, _ := bm["denom"].(string); d == denom {
			amt, _ := bm["amount"].(string)
			var n int64
			_, _ = fmt.Sscan(amt, &n)
			return n
		}
	}
	return 0
}

// ---- tool registry (on-chain discovery) -------------------------------------

type tool struct {
	ID    string
	Owner string
	Badge string
}

func listTools() ([]tool, error) {
	m, err := queryRetry("registry", "list-tools")
	if err != nil {
		// Distinguish "node unreachable" from "no tools" so a caller never
		// reports a real tool as missing just because a query blipped.
		return nil, fmt.Errorf("discover tools (registry list-tools): %w", err)
	}
	var tools []tool
	raw, _ := m["tools"].([]any)
	for _, t := range raw {
		tm, _ := t.(map[string]any)
		id, _ := tm["tool_id"].(string)
		owner, _ := tm["owner"].(string)
		if id == "" {
			continue
		}
		badge := ""
		if bm, err := query("incentives", "badge", id); err == nil {
			if b, ok := bm["badge"].(map[string]any); ok {
				badge, _ = b["tier"].(string)
			}
		}
		tools = append(tools, tool{ID: id, Owner: owner, Badge: badge})
	}
	return tools, nil
}

// ---- the call loop: meter -> execute -> prove -> settle ---------------------

type callResult struct {
	Output     string `json:"output"`
	ReceiptID  string `json:"receipt_id"`
	CostUlac   string `json:"cost_ulac"`
	Publisher  string `json:"publisher"`
	Settled    bool   `json:"settled"`
	CacheHit   bool   `json:"cache_hit"`
	Verifiable string `json:"verifiable"`
}

// blake3Sum returns the BLAKE3-256 digest of s.
func blake3Sum(s string) []byte { h := blake3.Sum256([]byte(s)); return h[:] }

// cacRequestHash binds (tool, input) to a deterministic content-cache key.
func cacRequestHash(toolID, input string) string {
	return "req-" + hex.EncodeToString(blake3Sum(toolID + ":" + input))[:40]
}

// cacheLookup checks the content-addressed cache for a prior identical call.
// Returns (output, originTool, true) only when a hit's content decodes cleanly.
// originTool is the tool that stored the entry — used to route a CAC royalty
// when a DIFFERENT tool serves it (the credits module rejects a self-royalty).
func cacheLookup(requestHash string) (string, string, bool) {
	m, err := query("cac", "lookup", requestHash)
	if err != nil {
		return "", "", false
	}
	entries, ok := m["entries"].([]any)
	if !ok || len(entries) == 0 {
		return "", "", false
	}
	e, _ := entries[0].(map[string]any)
	c, _ := e["content"].(string)
	if c == "" {
		return "", "", false
	}
	raw, err := base64.StdEncoding.DecodeString(c)
	if err != nil {
		return "", "", false
	}
	origin, _ := e["tool_id"].(string)
	return strings.TrimRight(string(raw), "\n"), origin, true
}

// cacheStore populates the cache after a miss so the next identical call is a
// hit. Best-effort: a failure here never fails the tool call.
func cacheStore(toolID, requestHash, output string) {
	f, err := os.CreateTemp("", "mcp-cac-*.json")
	if err != nil {
		return
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, _ = f.WriteString(output)
	_ = f.Close()
	code, _, rawlog, err := broadcast(cfg.Supernode, "cac", "cache-store", f.Name(),
		"--tool-id", toolID, "--request-hash", requestHash, "--deterministic", "--royalty-eligible")
	if err != nil || code != 0 {
		logf("cache-store failed (non-fatal): %v %s", err, rawlog)
	}
}

// executeTool runs the off-chain work. One tool — oracle-feed — does GENUINE
// verifiable execution by fetching a live price from a real API (no key), so the
// Proof-of-Service receipt anchors a real external value. Other tools use a
// deterministic placeholder (real LLM/API/compute tools plug in the same way).
func executeTool(toolID, input string) (model, output string) {
	// A per-call model tag keeps each receipt content-unique so repeated identical
	// calls never collide with a prior receipt bound to a different lock.
	model = fmt.Sprintf("%s-c%d", toolID, time.Now().UnixNano()%1000000)
	if toolID == "oracle-feed" {
		if p, ok := fetchSpotPrice(input); ok {
			return model, p
		}
	}
	output = strings.ToUpper(strings.TrimSpace(input))
	if output == "" {
		output = "(empty)"
	}
	return model, output
}

// fetchSpotPrice queries Coinbase's public spot-price endpoint for a pair like
// BTC-USD (real, live, no API key). Falls back gracefully on any error.
func fetchSpotPrice(pair string) (string, bool) {
	clean := ""
	for _, c := range strings.ToUpper(strings.TrimSpace(pair)) {
		if c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' {
			clean += string(c)
		}
	}
	if !strings.Contains(clean, "-") {
		clean = "BTC-USD"
	}
	cl := &http.Client{Timeout: 4 * time.Second}
	resp, err := cl.Get("https://api.coinbase.com/v2/prices/" + clean + "/spot")
	if err != nil {
		return "", false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return "", false
	}
	var m struct {
		Data struct{ Amount, Base, Currency string } `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&m) != nil || m.Data.Amount == "" {
		return "", false
	}
	return fmt.Sprintf("%s/%s spot = $%s (Coinbase, live)", m.Data.Base, m.Data.Currency, m.Data.Amount), true
}

func ensureCredits(agent string, need int64) error {
	if balance(keyAddr(agent), "ulac") >= need {
		return nil
	}
	logf("agent low on LAC, swapping LUME->LAC")
	code, _, rawlog, err := broadcast(agent, "credits", "swap-lume-to-lac", "--amount", "5000000ulume", "--min-lac-out", "1")
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("swap failed: %s", rawlog)
	}
	return nil
}

func callTool(toolID, input string) (*callResult, error) {
	tools, err := listTools()
	if err != nil {
		return nil, err
	}
	t := tool{ID: toolID}
	for _, x := range tools {
		if x.ID == toolID {
			t = x
			break
		}
	}
	if t.Owner == "" {
		return nil, fmt.Errorf("tool %q not found on-chain", toolID)
	}

	if err := ensureCredits(cfg.Agent, 1_000_000); err != nil {
		return nil, err
	}

	// Content-addressed cache: a deterministic request hash binds (tool, input).
	// On a hit we serve the stored output (skipping execution) and flag the lock
	// as a cache hit so the credits module routes a royalty to the origin
	// publisher; on a miss we execute and store the result for next time.
	requestHash := cacRequestHash(toolID, input)
	cachedOutput, originTool, cacheHit := cacheLookup(requestHash)
	// A CAC royalty only applies when a DIFFERENT tool serves the cached content.
	crossToolHit := cacheHit && originTool != "" && originTool != toolID

	// 1. Lock credits for the call.
	sid := fmt.Sprintf("mcp-%d", time.Now().UnixNano()%1_000_000)
	code, events, rawlog, err := broadcast(cfg.Agent, "credits", "lock-credits",
		"--amount", cfg.LockAmount, "--session-id", sid, "--tool-id", toolID,
		"--quote-id", "q-"+sid, "--policy-version", "policy-v1", "--intent-hash", "i-"+sid)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("lock failed: %s", rawlog)
	}
	lockID := eventAttr(events, "credit_lock", "lock_id")
	if lockID == "" {
		// fall back: scan any event for lock_id
		for _, e := range events {
			if attrs, ok := e["attributes"].([]any); ok {
				for _, a := range attrs {
					am, _ := a.(map[string]any)
					if k, _ := am["key"].(string); k == "lock_id" {
						lockID, _ = am["value"].(string)
					}
				}
			}
		}
	}
	if lockID == "" {
		return nil, fmt.Errorf("no lock_id from lock-credits")
	}

	// 2. Execute the tool off-chain — or serve the stored output on a cache hit.
	var model, output string
	if cacheHit {
		model, output = "cache", cachedOutput
	} else {
		model, output = executeTool(toolID, input)
	}

	// 3. Submit a SuperNode Proof-of-Service receipt (BLAKE3(input,model,output)).
	code, events, rawlog, err = broadcast(cfg.Supernode, "registry", "submit-receipt", toolID,
		"--model", model, "--input", input, "--result", output, "--session-id", sid, "--lock-id", lockID)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("submit-receipt failed: %s", rawlog)
	}
	receiptID := eventAttr(events, "receipt_submitted", "receipt_id")
	if receiptID == "" {
		// derive deterministically as a fallback (matches the chain's rule)
		rh := blake3.Sum256([]byte(input))
		oh := blake3.Sum256([]byte(output))
		tr := blake3.Sum256(append(append(append([]byte{}, rh[:]...), []byte(model)...), oh[:]...))
		receiptID = "pos1" + hex.EncodeToString(tr[:])
	}

	// 4. Settle — gated on the verified receipt; pays the publisher. On a cache
	// hit, flag the settlement so the credits module routes a CAC royalty to the
	// origin publisher (the tool whose stored output we served).
	settleArgs := []string{"credits", "settle-credits",
		"--lock-id", lockID, "--actual-cost", cfg.ActualCost, "--publisher", t.Owner,
		"--receipt-id", receiptID, "--tool-id", toolID}
	if crossToolHit {
		settleArgs = append(settleArgs, "--cache-hit", "--origin-tool-id", originTool)
	}
	code, _, rawlog, err = broadcast(cfg.Agent, settleArgs...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("settle failed: %s", rawlog)
	}

	// 5. On a miss, populate the cache so the next identical call is a hit.
	if !cacheHit {
		cacheStore(toolID, requestHash, output)
	}

	return &callResult{
		Output: output, ReceiptID: receiptID, CostUlac: cfg.ActualCost,
		Publisher: t.Owner, Settled: true, CacheHit: cacheHit,
		Verifiable: "BLAKE3(input,model,output) anchored on-chain; settlement gated on it",
	}, nil
}

// ---- MCP (JSON-RPC 2.0 over stdio) ------------------------------------------

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

var out = bufio.NewWriter(os.Stdout)

func send(resp rpcResp) {
	resp.JSONRPC = "2.0"
	b, _ := json.Marshal(resp)
	_, _ = out.Write(b)
	_ = out.WriteByte('\n')
	_ = out.Flush()
}

func mcpTools() []map[string]any {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{"type": "string", "description": "input passed to the tool"},
		},
		"required": []string{"input"},
	}
	var tools []map[string]any
	onchain, err := listTools()
	if err != nil {
		logf("tools/list discovery failed: %v", err)
		return []map[string]any{}
	}
	for _, t := range onchain {
		desc := fmt.Sprintf("Lumera on-chain tool %q (publisher %s). Each call is metered, executed, "+
			"proven with a SuperNode Proof-of-Service receipt, and settled on-chain.", t.ID, short(t.Owner))
		if t.Badge != "" && t.Badge != "BADGE_TIER_NONE" {
			desc += " Reputation: " + strings.TrimPrefix(t.Badge, "BADGE_TIER_") + "."
		}
		tools = append(tools, map[string]any{"name": t.ID, "description": desc, "inputSchema": schema})
	}
	if tools == nil {
		tools = []map[string]any{}
	}
	return tools
}

func short(a string) string {
	if len(a) > 16 {
		return a[:12] + "…" + a[len(a)-4:]
	}
	return a
}

func handle(req rpcReq) {
	switch req.Method {
	case "initialize":
		send(rpcResp{ID: req.ID, Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "lumera-mcp-router", "version": "0.1.0"},
		}})
	case "notifications/initialized":
		// notification, no response
	case "ping":
		send(rpcResp{ID: req.ID, Result: map[string]any{}})
	case "tools/list":
		send(rpcResp{ID: req.ID, Result: map[string]any{"tools": mcpTools()}})
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)
		input, _ := p.Arguments["input"].(string)
		logf("tools/call %s input=%q", p.Name, input)
		res, err := callTool(p.Name, input)
		if err != nil {
			send(rpcResp{ID: req.ID, Result: map[string]any{
				"isError": true,
				"content": []map[string]any{{"type": "text", "text": "call failed: " + err.Error()}},
			}})
			return
		}
		served := "executed"
		if res.CacheHit {
			served = "served from content-cache (royalty to origin publisher)"
		}
		summary := fmt.Sprintf("%s\n\n— %s, proven on-chain —\nreceipt: %s\npaid publisher %s: %s\n%s",
			res.Output, served, res.ReceiptID, short(res.Publisher), res.CostUlac, res.Verifiable)
		structured, _ := json.Marshal(res)
		send(rpcResp{ID: req.ID, Result: map[string]any{
			"content":           []map[string]any{{"type": "text", "text": summary}},
			"structuredContent": json.RawMessage(structured),
		}})
	default:
		if len(req.ID) > 0 {
			send(rpcResp{ID: req.ID, Error: &rpcErr{Code: -32601, Message: "method not found: " + req.Method}})
		}
	}
}

func main() {
	logf("starting: node=%s home=%s agent=%s supernode=%s", cfg.Node, cfg.Home, cfg.Agent, cfg.Supernode)
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req rpcReq
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			logf("bad json: %v", err)
			continue
		}
		handle(req)
	}
}
