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
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	Home:       envOr("LUMERA_HOME", "/tmp/lnode_web"),
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

// broadcast runs a tx from key `from`, waits for inclusion, returns (code, events).
func broadcast(from string, txArgs ...string) (int, []map[string]any, string, error) {
	args := append([]string{"tx"}, txArgs...)
	args = append(args, "--from", from, "--home", cfg.Home, "--node", cfg.Node,
		"--chain-id", cfg.ChainID, "--keyring-backend", "test", "--gas", cfg.Gas,
		"--fees", cfg.Fees, "-y", "-o", "json")
	out, err := run(args...)
	if err != nil {
		return -1, nil, "", err
	}
	var bc map[string]any
	if err := json.Unmarshal([]byte(out), &bc); err != nil {
		return -1, nil, "", fmt.Errorf("decode broadcast: %w", err)
	}
	hash, _ := bc["txhash"].(string)
	if hash == "" {
		return -1, nil, "", fmt.Errorf("no txhash")
	}
	for i := 0; i < 30; i++ {
		m, qerr := query("tx", hash)
		if qerr == nil && m != nil {
			code := 0
			if c, ok := m["code"].(float64); ok {
				code = int(c)
			}
			rawlog, _ := m["raw_log"].(string)
			var events []map[string]any
			if evs, ok := m["events"].([]any); ok {
				for _, e := range evs {
					if em, ok := e.(map[string]any); ok {
						events = append(events, em)
					}
				}
			}
			return code, events, rawlog, nil
		}
		time.Sleep(700 * time.Millisecond)
	}
	return -1, nil, "", fmt.Errorf("tx %s not indexed", hash)
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
			fmt.Sscan(amt, &n)
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

func listTools() []tool {
	m, err := query("registry", "list-tools")
	if err != nil {
		logf("list-tools failed: %v", err)
		return nil
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
	return tools
}

// ---- the call loop: meter -> execute -> prove -> settle ---------------------

type callResult struct {
	Output     string `json:"output"`
	ReceiptID  string `json:"receipt_id"`
	CostUlac   string `json:"cost_ulac"`
	Publisher  string `json:"publisher"`
	Settled    bool   `json:"settled"`
	Verifiable string `json:"verifiable"`
}

// executeTool is the placeholder for off-chain tool execution. Real tools (LLM
// inference, APIs, compute) plug in here; the PoC returns a deterministic
// transform so the input/model/output digest is meaningful.
func executeTool(toolID, input string) (model, output string) {
	model = toolID
	output = strings.ToUpper(strings.TrimSpace(input))
	if output == "" {
		output = "(empty)"
	}
	return model, output
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
	t := tool{ID: toolID}
	for _, x := range listTools() {
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
	lockID := eventAttr(events, "credits.lock_created", "lock_id")
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

	// 2. Execute the tool off-chain.
	model, output := executeTool(toolID, input)

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

	// 4. Settle — gated on the verified receipt; pays the publisher.
	code, _, rawlog, err = broadcast(cfg.Agent, "credits", "settle-credits",
		"--lock-id", lockID, "--actual-cost", cfg.ActualCost, "--publisher", t.Owner,
		"--receipt-id", receiptID, "--tool-id", toolID)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("settle failed: %s", rawlog)
	}

	return &callResult{
		Output: output, ReceiptID: receiptID, CostUlac: cfg.ActualCost,
		Publisher: t.Owner, Settled: true,
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
	out.Write(b)
	out.WriteByte('\n')
	out.Flush()
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
	for _, t := range listTools() {
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
		summary := fmt.Sprintf("%s\n\n— proven on-chain —\nreceipt: %s\npaid publisher %s: %s\n%s",
			res.Output, res.ReceiptID, short(res.Publisher), res.CostUlac, res.Verifiable)
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
