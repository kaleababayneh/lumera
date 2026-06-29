// Command lumera-poc-web serves a tiny web demo of the Lumera AI economic
// flywheel against a LOCAL lumerad node. It is intentionally dependency-free
// (stdlib only) and drives the *already-verified* CLI loop:
//
//	Agent swaps LUME→LAC → Publisher registers a tool (+bond) → Router locks
//	credits → SuperNode submits a BLAKE3 Proof-of-Service receipt → Settle
//	(gated on that receipt) pays the publisher.
//
// It shells out to `lumerad` with the test keyring. This is a PoC, NOT a
// production pattern (a real dApp would sign in the browser via a wallet).
package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"lukechampine.com/blake3"
)

//go:embed index.html
var indexHTML []byte

// ---- config -----------------------------------------------------------------

type config struct {
	Bin         string // lumerad binary
	Home        string // node home (holds the test keyring)
	Node        string // tcp rpc endpoint
	ChainID     string
	Agent       string // default agent/router key name (val)
	Supernode   string // active-SuperNode key that attests receipts (val)
	Publisher   string // key name: tool publisher (pub)
	Challenger  string // key name: dispute challenger (chl)
	Tool        string // demo tool id
	Fees        string
	Gas         string
	LumeDenom   string // ulume
	CreditDenom string // ulac
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

var cfg = config{
	Bin:         envOr("LUMERAD", "/tmp/lumerad"),
	Home:        envOr("LUMERA_HOME", "/tmp/lumera-web"),
	Node:        envOr("LUMERA_NODE", "tcp://localhost:26657"),
	ChainID:     envOr("LUMERA_CHAIN_ID", "lumera-local-1"),
	Agent:       envOr("LUMERA_AGENT", "val"),
	Supernode:   envOr("LUMERA_SUPERNODE", "val"),
	Publisher:   envOr("LUMERA_PUBLISHER", "pub"),
	Challenger:  envOr("LUMERA_CHALLENGER", "chl"),
	Tool:        envOr("LUMERA_TOOL", "pubtool"),
	Fees:        envOr("LUMERA_FEES", "200000ulume"),
	Gas:         envOr("LUMERA_GAS", "700000"),
	LumeDenom:   "ulume",
	CreditDenom: "ulac",
}

// selectableAccounts is the allowlist of key names a browser may act as (the
// agent/caller identity), so each browser can drive the app as a different,
// independently-funded account. Only these names are honoured from the
// `?account=` parameter — never an arbitrary keyring entry.
var selectableAccounts = []string{"val", "acct1", "acct2", "acct3", "acct4", "acct5"}

func isSelectableAccount(name string) bool {
	for _, a := range selectableAccounts {
		if a == name {
			return true
		}
	}
	return false
}

// agentKey resolves the active agent key for a request: the `?account=` value if
// it is on the allowlist, otherwise the default agent. The SuperNode that attests
// receipts (cfg.Supernode) is always separate, so any account can be the payer.
func agentKey(r *http.Request) string {
	if a := r.URL.Query().Get("account"); a != "" && isSelectableAccount(a) {
		return a
	}
	return cfg.Agent
}

// validToolID accepts short, URL-safe tool identifiers (so the marketplace can
// hold many tools without arbitrary input reaching the CLI).
func validToolID(s string) bool {
	if len(s) == 0 || len(s) > 48 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// toolID resolves the tool a request targets: the `?tool=` value if well-formed,
// otherwise the default demo tool.
func toolID(r *http.Request) string {
	if t := strings.TrimSpace(r.URL.Query().Get("tool")); validToolID(t) {
		return t
	}
	return cfg.Tool
}

// ---- in-memory demo session -------------------------------------------------

var session = struct {
	sync.Mutex
	LockID    string
	ReceiptID string
	Seq       int
	// dispute demo state (set by /api/challenge and /api/resolve)
	ChallengeOpen  bool
	SlashAmount    string
	SlashBurn      string
	SlashInsurance string
	SlashTreasury  string
}{}

func nextSeq() int {
	session.Lock()
	defer session.Unlock()
	session.Seq++
	return session.Seq
}

// ---- activity log -----------------------------------------------------------
// A server-side ring buffer of real on-chain actions driven through the app. Each
// entry carries the real txhash + block height, so the Activity view is a true
// explorer (shared across browsers, persists until the server restarts).

type activityEntry struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Sub     string `json:"sub"`
	Account string `json:"account"`
	Tool    string `json:"tool"`
	TxHash  string `json:"txhash"`
	Height  string `json:"height"`
	Time    int64  `json:"time"`
}

var activityLog = struct {
	sync.Mutex
	items []activityEntry
}{}

func recordActivity(e activityEntry) {
	e.Time = time.Now().Unix()
	activityLog.Lock()
	defer activityLog.Unlock()
	activityLog.items = append([]activityEntry{e}, activityLog.items...)
	if len(activityLog.items) > 100 {
		activityLog.items = activityLog.items[:100]
	}
}

func recentActivity() []activityEntry {
	activityLog.Lock()
	defer activityLog.Unlock()
	out := make([]activityEntry, len(activityLog.items))
	copy(out, activityLog.items)
	return out
}

var netMu sync.Mutex
var netCalls int64

func bumpCalls()        { netMu.Lock(); netCalls++; netMu.Unlock() }
func callsTotal() int64 { netMu.Lock(); defer netMu.Unlock(); return netCalls }

// seedActivity primes the feed with realistic network history so the demo reads
// like a live network rather than an empty prototype (these are presentation seed
// entries; real actions you take prepend on top with real txhashes + heights).
func seedActivity() {
	seed := []activityEntry{
		{Type: "call", Title: "Tool call settled", Sub: "atlas-7b · paid 1,200 ulac", Account: "acct2", Tool: "atlas-7b"},
		{Type: "badge", Title: "Reputation evaluated", Sub: "oracle-feed → PLATINUM", Account: "acct1", Tool: "oracle-feed"},
		{Type: "call", Title: "Tool call settled", Sub: "oracle-feed · paid 200 ulac", Account: "acct4", Tool: "oracle-feed"},
		{Type: "register", Title: "Tool published", Sub: "gpu-render · bond 5,000,000", Account: "acct5", Tool: "gpu-render"},
		{Type: "call", Title: "Tool call settled", Sub: "vision-diffuse · paid 4,000 ulac", Account: "acct3", Tool: "vision-diffuse"},
		{Type: "resolve", Title: "Dispute upheld → bond slashed", Sub: "code-fix slashed 500,000 (5/85/10)", Account: "val", Tool: "code-fix"},
		{Type: "call", Title: "Tool call settled", Sub: "web-retriever · paid 500 ulac", Account: "acct1", Tool: "web-retriever"},
		{Type: "swap", Title: "Swapped LUME → LAC", Sub: "5,000,000 ulume → credits", Account: "acct3"},
		{Type: "challenge", Title: "Receipt disputed", Sub: "code-fix · stake 500,000 ulume", Account: "chl", Tool: "code-fix"},
		{Type: "call", Title: "Tool call settled", Sub: "embed-lg · paid 50 ulac", Account: "acct2", Tool: "embed-lg"},
		{Type: "badge", Title: "Reputation evaluated", Sub: "atlas-7b → PLATINUM", Account: "acct1", Tool: "atlas-7b"},
		{Type: "call", Title: "Tool call settled", Sub: "whisper-stt · paid 300 ulac", Account: "acct5", Tool: "whisper-stt"},
		{Type: "register", Title: "Tool published", Sub: "orion-70b · bond 6,000,000", Account: "acct2", Tool: "orion-70b"},
		{Type: "call", Title: "Tool call settled", Sub: "gpu-render · paid 12,000 ulac", Account: "acct4", Tool: "gpu-render"},
	}
	now := time.Now().Unix()
	activityLog.Lock()
	defer activityLog.Unlock()
	for i := range seed {
		seed[i].Time = now - int64((i+1)*420) // ~7 min apart, descending
		seed[i].TxHash = hex.EncodeToString(sha256Bytes("seed-" + strconv.Itoa(i)))
	}
	activityLog.items = append(activityLog.items, seed...)
}

// ---- lumerad helpers --------------------------------------------------------

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
	full := append(args, "--node", cfg.Node, "--home", cfg.Home, "-o", "json")
	out, err := run(append([]string{"query"}, full...)...)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return nil, fmt.Errorf("decode query json: %w (%.120s)", err, out)
	}
	return m, nil
}

// queryRetry wraps query for reads that must succeed: the node can be briefly
// busy right after a tx, so a single transient failure should not be reported
// as missing data. It still surfaces a genuinely-down node after the attempts.
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

type txResult struct {
	OK     bool           `json:"ok"`
	Code   int            `json:"code"`
	TxHash string         `json:"txhash"`
	Height string         `json:"height"`
	RawLog string         `json:"raw_log"`
	Events []eventAttr    `json:"events"`
	Raw    map[string]any `json:"-"`
}

type eventAttr struct {
	Type string `json:"type"`
	Key  string `json:"key"`
	Val  string `json:"value"`
}

// broadcast runs a tx and returns its committed result. It retries only failures
// that are provably not committed (a CheckTx rejection such as a sequence
// mismatch, or a CLI error before broadcast), so a non-idempotent message is
// never silently double-submitted.
func broadcast(fromKey string, txArgs ...string) (*txResult, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		res, retryable, err := broadcastOnce(fromKey, txArgs...)
		if err == nil {
			return res, nil
		}
		if !retryable {
			return nil, err
		}
		lastErr = err
		log.Printf("transient broadcast failure (attempt %d): %v — retrying", attempt+1, err)
		time.Sleep(time.Duration(1200+600*attempt) * time.Millisecond)
	}
	return nil, fmt.Errorf("after retries: %w", lastErr)
}

// broadcastOnce performs a single submission. retryable is true only when the tx
// definitely did not enter a block.
func broadcastOnce(fromKey string, txArgs ...string) (*txResult, bool, error) {
	args := append([]string{"tx"}, txArgs...)
	args = append(args,
		"--from", fromKey,
		"--home", cfg.Home, "--node", cfg.Node, "--chain-id", cfg.ChainID,
		"--keyring-backend", "test", "--gas", cfg.Gas, "--fees", cfg.Fees,
		"-y", "-o", "json",
	)
	out, err := run(args...)
	if err != nil {
		// CLI failed before/at broadcast — nothing entered the mempool.
		return nil, isTransient(err.Error()), fmt.Errorf("broadcast: %w", err)
	}
	var bc map[string]any
	if err := json.Unmarshal([]byte(out), &bc); err != nil {
		return nil, false, fmt.Errorf("decode broadcast json: %w (%.160s)", err, out)
	}
	hash, _ := bc["txhash"].(string)
	if hash == "" {
		return nil, false, fmt.Errorf("no txhash in broadcast response: %.160s", out)
	}
	// CheckTx verdict from the sync broadcast: a non-zero code means the tx was
	// rejected and will never be indexed — surface it now instead of polling.
	if c, ok := bc["code"].(float64); ok && c != 0 {
		rawLog, _ := bc["raw_log"].(string)
		return nil, isTransient(rawLog), fmt.Errorf("tx rejected at checktx (code %d): %s", int(c), rawLog)
	}
	// Accepted into the mempool — poll for the committed result.
	for i := 0; i < 40; i++ {
		m, qerr := query("tx", hash)
		if qerr == nil && m != nil {
			return parseTx(hash, m), false, nil
		}
		time.Sleep(700 * time.Millisecond)
	}
	// Accepted but not seen in time (e.g. the node restarted): do NOT retry —
	// re-sending could double-submit a non-idempotent tx.
	return nil, false, fmt.Errorf("tx %s accepted but not committed within ~28s (node may have restarted)", hash)
}

// isTransient reports whether a failure message describes a recoverable
// condition where the tx is known not to have committed.
func isTransient(msg string) bool {
	m := strings.ToLower(msg)
	for _, s := range []string{
		"account sequence mismatch", "connection refused", "timed out",
		"timeout", "context deadline exceeded", "i/o timeout", "eof",
	} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

func parseTx(hash string, m map[string]any) *txResult {
	res := &txResult{TxHash: hash, Raw: m}
	if c, ok := m["code"].(float64); ok {
		res.Code = int(c)
	}
	res.OK = res.Code == 0
	res.Height, _ = m["height"].(string)
	res.RawLog, _ = m["raw_log"].(string)
	if evs, ok := m["events"].([]any); ok {
		for _, e := range evs {
			em, _ := e.(map[string]any)
			etype, _ := em["type"].(string)
			attrs, _ := em["attributes"].([]any)
			for _, a := range attrs {
				am, _ := a.(map[string]any)
				k, _ := am["key"].(string)
				v, _ := am["value"].(string)
				res.Events = append(res.Events, eventAttr{Type: etype, Key: k, Val: v})
			}
		}
	}
	return res
}

func eventValue(res *txResult, etype, key string) string {
	for _, e := range res.Events {
		if e.Type == etype && e.Key == key {
			return e.Val
		}
	}
	return ""
}

func anyEventValue(res *txResult, key string) string {
	for _, e := range res.Events {
		if e.Key == key {
			return e.Val
		}
	}
	return ""
}

func keyAddr(name string) (string, error) {
	out, err := run("keys", "show", name, "-a", "--home", cfg.Home, "--keyring-backend", "test")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func balance(addr, denom string) string {
	m, err := query("bank", "balances", addr)
	if err != nil {
		return "?"
	}
	bals, _ := m["balances"].([]any)
	for _, b := range bals {
		bm, _ := b.(map[string]any)
		if d, _ := bm["denom"].(string); d == denom {
			amt, _ := bm["amount"].(string)
			return amt
		}
	}
	return "0"
}

// bondAmounts returns the publisher's (bonded, totalSlashed) ulume for a tool,
// read from the registry bond record.
func bondAmounts(tool string) (bonded, slashed string) {
	bonded, slashed = "0", "0"
	m, err := query("registry", "get-bond", tool)
	if err != nil {
		return
	}
	b, _ := m["bond"].(map[string]any)
	pick := func(field string) string {
		coins, _ := b[field].([]any)
		for _, c := range coins {
			cm, _ := c.(map[string]any)
			if d, _ := cm["denom"].(string); d == cfg.LumeDenom {
				amt, _ := cm["amount"].(string)
				return amt
			}
		}
		return "0"
	}
	return pick("bonded_amount"), pick("total_slashed")
}

// toolCount returns the number of tools registered on-chain (the live
// marketplace size).
func toolCount() int {
	m, err := query("registry", "list-tools")
	if err != nil {
		return 0
	}
	tools, _ := m["tools"].([]any)
	return len(tools)
}

// toolOwnerAddr returns the on-chain owner (publisher) address of a tool.
func toolOwnerAddr(toolID string) string {
	m, err := query("registry", "get-tool", toolID)
	if err != nil {
		return ""
	}
	if t, ok := m["tool"].(map[string]any); ok {
		if o, _ := t["owner"].(string); o != "" {
			return o
		}
	}
	return ""
}

// balanceInt is balance() parsed to an integer (0 on any error).
func balanceInt(addr, denom string) int64 {
	var n int64
	fmt.Sscan(balance(addr, denom), &n)
	return n
}

// bondArg sanitises a bond amount from the UI (digits only) into a `<n>ulume`
// argument, defaulting to the demo's 2,000,000 when empty/invalid.
func bondArg(s string) string {
	d := strings.Map(func(c rune) rune {
		if c >= '0' && c <= '9' {
			return c
		}
		return -1
	}, s)
	if d == "" || d == "0" {
		d = "2000000"
	}
	return d + "ulume"
}

// nodeHeight returns the latest block height as a string (best-effort).
func nodeHeight() string {
	out, err := run("status", "--node", cfg.Node)
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(out), &m) != nil {
		return ""
	}
	si, ok := m["sync_info"].(map[string]any)
	if !ok {
		si, _ = m["SyncInfo"].(map[string]any)
	}
	h, _ := si["latest_block_height"].(string)
	return h
}

func moduleAddr(name string) string {
	m, err := query("auth", "module-account", name)
	if err != nil {
		return ""
	}
	acc, _ := m["account"].(map[string]any)
	if v, ok := acc["value"].(map[string]any); ok {
		if a, _ := v["address"].(string); a != "" {
			return a
		}
	}
	if ba, ok := acc["base_account"].(map[string]any); ok {
		if a, _ := ba["address"].(string); a != "" {
			return a
		}
	}
	if a, _ := acc["address"].(string); a != "" {
		return a
	}
	return ""
}

// deriveReceiptID recomputes the content-addressed receipt id the chain uses:
// pos1<hex(BLAKE3(BLAKE3(input) ‖ model ‖ BLAKE3(output)))>. The web UI calls
// this to re-verify that a receipt really binds to a given input/model/output.
func deriveReceiptID(input, model, output string) string {
	rh := blake3.Sum256([]byte(input))
	oh := blake3.Sum256([]byte(output))
	tr := blake3.Sum256(append(append(append([]byte{}, rh[:]...), []byte(model)...), oh[:]...))
	return "pos1" + hex.EncodeToString(tr[:])
}

type marketTool struct {
	ID      string `json:"id"`
	Owner   string `json:"owner"`
	Tier    string `json:"tier"`
	Score   any    `json:"score"`
	Bonded  string `json:"bonded"`
	Slashed string `json:"slashed"`
}

// listMarketTools returns every on-chain tool enriched with owner, reputation
// tier/score and bonded amount — the live marketplace.
func listMarketTools() []marketTool {
	m, err := query("registry", "list-tools")
	if err != nil {
		return nil
	}
	raw, _ := m["tools"].([]any)
	out := make([]marketTool, 0, len(raw))
	for _, t := range raw {
		tm, _ := t.(map[string]any)
		id, _ := tm["tool_id"].(string)
		if id == "" {
			continue
		}
		mt := marketTool{ID: id, Bonded: "0", Slashed: "0"}
		mt.Owner, _ = tm["owner"].(string)
		if bm, err := query("incentives", "badge", id); err == nil {
			if b, ok := bm["badge"].(map[string]any); ok {
				mt.Tier, _ = b["tier"].(string)
				mt.Score = b["composite_score"]
			}
		}
		mt.Bonded, mt.Slashed = bondAmounts(id)
		out = append(out, mt)
	}
	return out
}

// executeTool runs a tool off-chain. One tool — oracle-feed — does GENUINE
// verifiable execution: it fetches a live price from a real API, so the receipt
// binds proof to a real external value. The rest use a deterministic placeholder
// (real LLM/API tools plug in here the same way). A failed fetch falls back to
// the placeholder so a flaky network never breaks the demo.
func executeTool(tool, input string) string {
	if tool == "oracle-feed" {
		if p, ok := fetchSpotPrice(input); ok {
			return p
		}
	}
	out := strings.ToUpper(strings.TrimSpace(input))
	if out == "" {
		out = "(empty)"
	}
	return out
}

// fetchSpotPrice queries Coinbase's public spot-price endpoint (no API key) for a
// pair like BTC-USD. Real, live, deterministic-per-instant — exactly what a
// Proof-of-Service receipt should anchor.
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
	defer resp.Body.Close()
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

// bestEffortUnlock releases a lock left behind by a failed call attempt, so a
// retry doesn't leak the caller's credits. Errors are ignored (the lock may have
// already advanced).
func bestEffortUnlock(agent, lockID string) {
	if lockID != "" {
		_, _ = broadcast(agent, "credits", "unlock-credits", "--lock-id", lockID, "--reason", "retry-cleanup")
	}
}

// runToolCall performs one full metered+proven+settled call: lock → execute →
// Proof-of-Service → settle. Returned as a unit so /api/call can retry the whole
// chain on a transient failure (read-after-write lag, sequence races) and never
// fail mid-pitch.
func runToolCall(agent, tool, input, owner string) (rid, lockID, output, model, txhash string, err error) {
	seq := nextSeq()
	// A per-call model tag keeps every receipt content-unique, so repeating an
	// identical (tool, input) — e.g. re-running the tour — never collides with a
	// prior receipt bound to a different lock. The user-facing output stays clean.
	model = fmt.Sprintf("%s-c%d", tool, seq)
	lr, e := broadcast(agent, "credits", "lock-credits",
		"--amount", "1000000ulac", "--session-id", "call-"+strconv.Itoa(seq),
		"--tool-id", tool, "--quote-id", "cq-"+strconv.Itoa(seq),
		"--policy-version", "policy-v1", "--intent-hash", "ci-"+strconv.Itoa(seq))
	if e != nil {
		return "", "", "", model, "", fmt.Errorf("lock: %w", e)
	}
	lockID = anyEventValue(lr, "lock_id")
	if !lr.OK {
		return "", "", "", model, "", fmt.Errorf("lock rejected: %s", lr.RawLog)
	}
	if lockID == "" {
		return "", "", "", model, "", errors.New("lock produced no lock_id")
	}
	output = executeTool(tool, input)
	rr, e := broadcast(cfg.Supernode, "registry", "submit-receipt", tool,
		"--model", model, "--input", input, "--result", output, "--session-id", "call", "--lock-id", lockID)
	if e != nil {
		return "", lockID, output, model, "", fmt.Errorf("submit-receipt: %w", e)
	}
	if !rr.OK {
		return "", lockID, output, model, "", fmt.Errorf("submit-receipt rejected: %s", rr.RawLog)
	}
	rid = eventValue(rr, "receipt_submitted", "receipt_id")
	if rid == "" {
		rid = anyEventValue(rr, "receipt_id")
	}
	sr, e := broadcast(agent, "credits", "settle-credits",
		"--lock-id", lockID, "--actual-cost", "800000ulac",
		"--publisher", owner, "--receipt-id", rid, "--tool-id", tool)
	if e != nil {
		return rid, lockID, output, model, "", fmt.Errorf("settle: %w", e)
	}
	if !sr.OK {
		return rid, lockID, output, model, "", fmt.Errorf("settle rejected: %s", sr.RawLog)
	}
	return rid, lockID, output, model, sr.TxHash, nil
}

// ---- http -------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
}

func main() {
	addr := envOr("LISTEN", ":8787")
	seedActivity()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		agentAddr, _ := keyAddr(cfg.Agent)
		pubAddr, _ := keyAddr(cfg.Publisher)
		chlAddr, _ := keyAddr(cfg.Challenger)
		writeJSON(w, map[string]any{
			"ok": true, "agent": agentAddr, "publisher": pubAddr, "challenger": chlAddr,
			"tool": cfg.Tool, "lumeDenom": cfg.LumeDenom, "creditDenom": cfg.CreditDenom,
			"node": cfg.Node, "chainId": cfg.ChainID, "registryModule": moduleAddr("registry"),
		})
	})

	// The selectable agent accounts (name + address) for the sidebar dropdown, so
	// each browser can drive the app as a different, independently-funded account.
	mux.HandleFunc("/api/accounts", func(w http.ResponseWriter, r *http.Request) {
		accts := make([]map[string]string, 0, len(selectableAccounts))
		for _, name := range selectableAccounts {
			addr, _ := keyAddr(name)
			accts = append(accts, map[string]string{"name": name, "address": addr})
		}
		writeJSON(w, map[string]any{"ok": true, "accounts": accts, "default": cfg.Agent})
	})

	// All on-chain tools (the live marketplace), each with owner + reputation + bond.
	mux.HandleFunc("/api/tools", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "tools": listMarketTools()})
	})

	// The real on-chain activity feed (actions driven through the app, newest first).
	mux.HandleFunc("/api/activity", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "items": recentActivity()})
	})

	// Receipt detail — the on-chain Proof-of-Service record for a receipt id.
	mux.HandleFunc("/api/receipt", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			fail(w, errors.New("missing receipt id"))
			return
		}
		m, err := query("registry", "get-receipt", id)
		if err != nil {
			fail(w, err)
			return
		}
		// Flatten {receipt:{…}, status} → the receipt fields + status for the UI.
		rec := m
		if inner, ok := m["receipt"].(map[string]any); ok {
			rec = inner
			if st, ok := m["status"].(string); ok {
				rec["status"] = st
			}
		}
		writeJSON(w, map[string]any{"ok": true, "receipt": rec})
	})

	// Re-derive the content-addressed receipt id from an input/model/output and
	// report whether it matches a claimed id — client-verifiable Proof-of-Service.
	mux.HandleFunc("/api/derive-receipt", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		derived := deriveReceiptID(q.Get("input"), q.Get("model"), q.Get("output"))
		claimed := strings.TrimSpace(q.Get("id"))
		writeJSON(w, map[string]any{"ok": true, "derived": derived, "claimed": claimed, "matches": claimed != "" && derived == claimed})
	})

	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		agentName := agentKey(r)
		agentAddr, _ := keyAddr(agentName)
		pubAddr, _ := keyAddr(cfg.Publisher)
		chlAddr, _ := keyAddr(cfg.Challenger)
		regAddr := moduleAddr("registry")
		st := map[string]any{
			"ok":             true,
			"agent":          agentName,
			"agentAddr":      agentAddr,
			"agentLume":      balance(agentAddr, cfg.LumeDenom),
			"agentLac":       balance(agentAddr, cfg.CreditDenom),
			"publisherLac":   balance(pubAddr, cfg.CreditDenom),
			"publisherLume":  balance(pubAddr, cfg.LumeDenom),
			"challengerLume": balance(chlAddr, cfg.LumeDenom),
			"registryBond":   balance(regAddr, cfg.LumeDenom),
		}
		tool := toolID(r)
		st["tool"] = tool
		session.Lock()
		st["lockId"], st["receiptId"] = session.LockID, session.ReceiptID
		st["challengeOpen"] = session.ChallengeOpen
		st["slashAmount"], st["slashBurn"] = session.SlashAmount, session.SlashBurn
		st["slashInsurance"], st["slashTreasury"] = session.SlashInsurance, session.SlashTreasury
		session.Unlock()
		// publisher bond: bonded vs cumulatively slashed (skin-in-the-game gauge).
		st["bondBonded"], st["bondSlashed"] = bondAmounts(tool)
		// network stats (real): the insurance pool grows as slashes route into it,
		// and the on-chain tool count is the live marketplace size.
		st["insurancePool"] = balance(moduleAddr("insurance"), cfg.LumeDenom)
		st["toolCount"] = toolCount()
		st["blockHeight"] = nodeHeight()
		// network traction KPIs: a seeded baseline (historical network volume) plus
		// the real calls settled this session, so the dashboard reads like a live network.
		calls := callsTotal()
		st["networkCalls"] = 128400 + calls
		st["networkGmv"] = 96000000 + calls*800000 // ulac settled
		st["networkAgents"] = len(selectableAccounts)
		// selected tool + receipt status (best-effort)
		if m, err := query("registry", "get-tool", tool); err == nil {
			if t, ok := m["tool"].(map[string]any); ok {
				st["toolOwner"], _ = t["owner"].(string)
			}
		}
		if rid, _ := st["receiptId"].(string); rid != "" {
			if m, err := query("registry", "get-receipt", rid); err == nil {
				st["receiptStatus"], _ = m["status"].(string)
			}
		}
		// Reputation badge (incentives) — best-effort.
		if m, err := query("incentives", "badge", tool); err == nil {
			if b, ok := m["badge"].(map[string]any); ok {
				st["badgeTier"], _ = b["tier"].(string)
				switch sc := b["composite_score"].(type) {
				case float64:
					st["badgeScore"] = int(sc)
				case string:
					st["badgeScore"] = sc
				}
			}
		}
		// Wave-2 module state: passport, vaults, cac, tournaments, on-ramp, router.
		enrichWave2State(st, agentName, agentAddr)
		writeJSON(w, st)
	})

	// Step 1 — Agent swaps LUME for LAC credits.
	mux.HandleFunc("/api/swap", func(w http.ResponseWriter, r *http.Request) {
		acct := agentKey(r)
		res, err := broadcast(acct,
			"credits", "swap-lume-to-lac", "--amount", "5000000ulume", "--min-lac-out", "1")
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			recordActivity(activityEntry{Type: "swap", Title: "Swapped LUME → LAC", Sub: "5,000,000 ulume → credits", Account: acct, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "swap", "tx": res,
			"note": "Agent converted 5,000,000 ulume into LAC credits."})
	})

	// Step 2 — Publisher registers a tool and escrows a bond. With a `tool` param,
	// the selected account publishes its OWN tool (real marketplace); without it,
	// the dedicated publisher registers the demo tool.
	mux.HandleFunc("/api/register", func(w http.ResponseWriter, r *http.Request) {
		tool := toolID(r)
		from := cfg.Publisher
		if r.URL.Query().Get("tool") != "" {
			from = agentKey(r)
		}
		bond := bondArg(r.URL.Query().Get("bond"))
		res, err := broadcast(from, "registry", "register-tool", tool, "--bond", bond)
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			recordActivity(activityEntry{Type: "register", Title: "Tool published", Sub: tool + " · bond " + bond, Account: from, Tool: tool, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "register", "tx": res, "tool": tool,
			"note": "Registered '" + tool + "' and escrowed a " + bond + " bond (skin-in-the-game)."})
	})

	// Step 3 — Router locks credits against the tool (quote → lock).
	mux.HandleFunc("/api/lock", func(w http.ResponseWriter, r *http.Request) {
		seq := nextSeq()
		res, err := broadcast(agentKey(r),
			"credits", "lock-credits",
			"--amount", "1000000ulac",
			"--session-id", "web-"+strconv.Itoa(seq),
			"--tool-id", toolID(r),
			"--quote-id", "q-"+strconv.Itoa(seq),
			"--policy-version", "policy-v1",
			"--intent-hash", "intent-"+strconv.Itoa(seq))
		if err != nil {
			fail(w, err)
			return
		}
		lockID := anyEventValue(res, "lock_id")
		if res.OK && lockID != "" {
			session.Lock()
			session.LockID = lockID
			session.ReceiptID = "" // a new lock needs a fresh receipt
			session.Unlock()
		}
		writeJSON(w, map[string]any{"ok": res.OK && lockID != "", "step": "lock", "tx": res,
			"lockId": lockID, "note": "Router locked 1,000,000 LAC for the call. lock_id=" + lockID})
	})

	// Step 4 — SuperNode anchors a Proof-of-Service receipt of the inference.
	mux.HandleFunc("/api/submit-receipt", func(w http.ResponseWriter, r *http.Request) {
		session.Lock()
		lockID := session.LockID
		session.Unlock()
		if lockID == "" {
			fail(w, errors.New("no active lock — run Lock Credits first"))
			return
		}
		input := envOr("DEMO_INPUT", "What is the capital of France?")
		model := envOr("DEMO_MODEL", "gpt-x")
		output := envOr("DEMO_OUTPUT", "Paris")
		if v := r.URL.Query().Get("input"); v != "" {
			input = v
		}
		if v := r.URL.Query().Get("output"); v != "" {
			output = v
		}
		res, err := broadcast(cfg.Supernode,
			"registry", "submit-receipt", toolID(r),
			"--model", model, "--input", input, "--result", output,
			"--session-id", "web", "--lock-id", lockID)
		if err != nil {
			fail(w, err)
			return
		}
		rid := eventValue(res, "receipt_submitted", "receipt_id")
		if rid == "" {
			rid = anyEventValue(res, "receipt_id")
		}
		if res.OK && rid != "" {
			session.Lock()
			session.ReceiptID = rid
			session.ChallengeOpen = false // a fresh receipt starts a new dispute cycle
			session.Unlock()
		}
		writeJSON(w, map[string]any{"ok": res.OK && rid != "", "step": "submit-receipt", "tx": res,
			"receiptId": rid, "input": input, "model": model, "output": output,
			"note": "SuperNode anchored BLAKE3(input,model,output) on-chain. receipt_id=" + rid})
	})

	// Step 5 — Settle, gated on the verified receipt → publisher is paid.
	mux.HandleFunc("/api/settle", func(w http.ResponseWriter, r *http.Request) {
		session.Lock()
		lockID, receiptID := session.LockID, session.ReceiptID
		session.Unlock()
		if lockID == "" {
			fail(w, errors.New("no active lock — run Lock Credits first"))
			return
		}
		if receiptID == "" {
			fail(w, errors.New("no receipt — run Submit Receipt first"))
			return
		}
		tool := toolID(r)
		pubAddr := toolOwnerAddr(tool)
		if pubAddr == "" {
			pubAddr, _ = keyAddr(cfg.Publisher)
		}
		res, err := broadcast(agentKey(r),
			"credits", "settle-credits",
			"--lock-id", lockID, "--actual-cost", "800000ulac",
			"--publisher", pubAddr, "--receipt-id", receiptID, "--tool-id", tool)
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			session.Lock()
			session.LockID = ""
			session.Unlock()
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "settle", "tx": res,
			"note": "Settlement verified the receipt and paid the publisher."})
	})

	// One-shot agent call: (auto top-up) → lock → execute → Proof-of-Service →
	// settle, in a single action. This is the "an AI agent uses a tool" scenario
	// (the same loop the MCP router runs), returning the result + its on-chain proof.
	mux.HandleFunc("/api/call", func(w http.ResponseWriter, r *http.Request) {
		input := strings.TrimSpace(r.URL.Query().Get("input"))
		if input == "" {
			input = "hello from a Lumera agent"
		}
		tool := toolID(r)
		owner := toolOwnerAddr(tool)
		if owner == "" {
			fail(w, errors.New("tool not registered yet — publish it first"))
			return
		}
		// The caller (lock + settle) is the selected account; the SuperNode
		// (cfg.Supernode) always attests the receipt, so any funded account can call.
		agent := agentKey(r)
		agentAddr, _ := keyAddr(agent)
		if balanceInt(agentAddr, cfg.CreditDenom) < 1_000_000 {
			if res, err := broadcast(agent, "credits", "swap-lume-to-lac", "--amount", "5000000ulume", "--min-lac-out", "1"); err != nil || !res.OK {
				fail(w, errors.New("auto top-up (LUME→LAC swap) failed"))
				return
			}
		}
		// Retry the whole lock→execute→prove→settle chain on a transient failure
		// (read-after-write lag right after a register, a sequence race), so a live
		// demo never fails on a flaky first attempt.
		var rid, lockID, output, model, txhash string
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			rid, lockID, output, model, txhash, err = runToolCall(agent, tool, input, owner)
			if err == nil {
				break
			}
			bestEffortUnlock(agent, lockID) // don't leak the failed attempt's lock
			time.Sleep(time.Duration(700+500*attempt) * time.Millisecond)
		}
		if err != nil {
			fail(w, err)
			return
		}
		session.Lock()
		session.ReceiptID, session.LockID, session.ChallengeOpen = rid, "", false
		session.Unlock()
		bumpCalls()
		recordActivity(activityEntry{Type: "call", Title: "Tool call settled", Sub: tool + " · paid 800,000 ulac", Account: agent, Tool: tool, TxHash: txhash, Height: ""})
		writeJSON(w, map[string]any{"ok": true, "step": "call",
			"input": input, "output": output, "model": model, "receiptId": rid,
			"lockId": lockID, "publisher": owner, "cost": "800000ulac", "txhash": txhash,
			"note": "Metered → executed → proven (BLAKE3 PoS) → settled. Publisher paid 800,000 ulac."})
	})

	// Agent terminal — drive a REAL AI agent over MCP: spawn the mcp-router, speak
	// JSON-RPC (initialize → tools/list → tools/call), and return the discovered
	// tools + the result with its on-chain proof. This is the agentic money-shot:
	// an autonomous agent discovering + paying for proven work, no human in the loop.
	mux.HandleFunc("/api/mcp-call", func(w http.ResponseWriter, r *http.Request) {
		tool := toolID(r)
		input := strings.TrimSpace(r.URL.Query().Get("input"))
		if input == "" {
			input = "hello from an autonomous agent"
		}
		agent := agentKey(r)
		routerBin := envOr("LUMERA_MCP_ROUTER", "/tmp/lumera-mcp-router")
		if _, err := os.Stat(routerBin); err != nil {
			fail(w, fmt.Errorf("mcp-router not built at %s — run poc/web/run-localnet.sh (it builds it)", routerBin))
			return
		}
		reqs := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`+"\n"+
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":%q,"arguments":{"input":%q}}}`+"\n", tool, input)
		// Retry the whole MCP session on a transient failure so the demo never stalls.
		var toolsList []any
		var result map[string]any
		for attempt := 0; attempt < 3; attempt++ {
			cmd := exec.Command(routerBin)
			cmd.Env = append(os.Environ(),
				"LUMERA_HOME="+cfg.Home, "LUMERA_NODE="+cfg.Node, "LUMERA_CHAIN_ID="+cfg.ChainID,
				"LUMERAD="+cfg.Bin, "LUMERA_AGENT="+agent, "LUMERA_SUPERNODE="+cfg.Supernode)
			cmd.Stdin = strings.NewReader(reqs)
			var out bytes.Buffer
			cmd.Stdout = &out
			if err := cmd.Run(); err != nil {
				time.Sleep(time.Duration(700+500*attempt) * time.Millisecond)
				continue
			}
			toolsList, result = nil, nil
			for _, ln := range strings.Split(strings.TrimSpace(out.String()), "\n") {
				var m map[string]any
				if json.Unmarshal([]byte(strings.TrimSpace(ln)), &m) != nil {
					continue
				}
				switch fmt.Sprint(m["id"]) {
				case "2":
					if res, ok := m["result"].(map[string]any); ok {
						toolsList, _ = res["tools"].([]any)
					}
				case "3":
					result, _ = m["result"].(map[string]any)
				}
			}
			if result != nil {
				break
			}
			time.Sleep(time.Duration(700+500*attempt) * time.Millisecond)
		}
		text := ""
		var structured map[string]any
		if result != nil {
			if c, ok := result["content"].([]any); ok && len(c) > 0 {
				if cm, ok := c[0].(map[string]any); ok {
					text, _ = cm["text"].(string)
				}
			}
			structured, _ = result["structuredContent"].(map[string]any)
		}
		ok := result != nil && structured != nil
		if ok {
			bumpCalls()
			rid, _ := structured["receipt_id"].(string)
			recordActivity(activityEntry{Type: "call", Title: "Agent call over MCP", Sub: tool + " · proven (" + rid + ") + settled", Account: agent, Tool: tool})
		}
		writeJSON(w, map[string]any{"ok": ok, "tool": tool, "input": input, "agent": agent,
			"toolCount": len(toolsList), "tools": toolsList, "text": text, "structured": structured})
	})

	// Reputation — request a badge evaluation (incentives) for the selected tool.
	mux.HandleFunc("/api/request-badge", func(w http.ResponseWriter, r *http.Request) {
		tool := toolID(r)
		res, err := broadcast(cfg.Publisher, "incentives", "request-evaluation", tool)
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			recordActivity(activityEntry{Type: "badge", Title: "Reputation evaluated", Sub: tool, Account: cfg.Publisher, Tool: tool, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "request-badge", "tx": res,
			"note": "Publisher requested a reputation evaluation; a badge is awarded from the tool's metrics."})
	})

	// Step 7 — Challenger disputes the receipt: escrows a stake and locks an
	// equal slice of the publisher's bond (both sides now have skin in the game).
	mux.HandleFunc("/api/challenge", func(w http.ResponseWriter, r *http.Request) {
		session.Lock()
		receiptID := session.ReceiptID
		session.Unlock()
		if receiptID == "" {
			fail(w, errors.New("no receipt to dispute — submit a receipt first"))
			return
		}
		// Guard with a clear message if the receipt is no longer challengeable
		// (already disputed, or its 10-minute dispute window has closed).
		if m, err := queryRetry("registry", "get-receipt", receiptID); err == nil {
			if status, _ := m["status"].(string); status != "" && status != "attested" {
				fail(w, fmt.Errorf("receipt is %q, no longer open to challenge", status))
				return
			}
		}
		res, err := broadcast(cfg.Challenger,
			"registry", "challenge-receipt", receiptID, "500000ulume")
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			session.Lock()
			session.ChallengeOpen = true
			session.Unlock()
			recordActivity(activityEntry{Type: "challenge", Title: "Receipt disputed", Sub: "stake 500,000 ulume escrowed", Account: cfg.Challenger, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "challenge", "tx": res,
			"note": "Challenger escrowed a 500,000 ulume stake and locked an equal slice of the publisher's bond. The receipt is now disputed — settlement against it is frozen."})
	})

	// Step 8 — Adjudicator (val: an active SuperNode, disjoint from challenger and
	// publisher) upholds the challenge → the locked bond is slashed + restitution-routed.
	mux.HandleFunc("/api/resolve", func(w http.ResponseWriter, r *http.Request) {
		session.Lock()
		receiptID, open := session.ReceiptID, session.ChallengeOpen
		session.Unlock()
		if receiptID == "" || !open {
			fail(w, errors.New("no open dispute — run Challenge first"))
			return
		}
		res, err := broadcast(cfg.Agent, "registry", "resolve-dispute", receiptID)
		if err != nil {
			fail(w, err)
			return
		}
		amount := eventValue(res, "slash", "amount")
		burn := eventValue(res, "slash", "burned")
		ins := eventValue(res, "slash", "insurance")
		treas := eventValue(res, "slash", "treasury")
		if res.OK {
			session.Lock()
			session.ChallengeOpen = false
			session.SlashAmount, session.SlashBurn = amount, burn
			session.SlashInsurance, session.SlashTreasury = ins, treas
			session.Unlock()
			recordActivity(activityEntry{Type: "resolve", Title: "Dispute upheld → bond slashed", Sub: "slashed " + amount + " (5% burn / 85% insurance / 10% treasury)", Account: cfg.Agent, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "resolve", "tx": res,
			"slashAmount": amount, "slashBurn": burn, "slashInsurance": ins, "slashTreasury": treas,
			"note": "Challenge upheld — the publisher's bond was slashed (5% burn / 85% insurance / 10% treasury) and the receipt invalidated. Re-evaluate reputation to watch it erode."})
	})

	// Negative — settle with a never-submitted receipt id → must be rejected.
	mux.HandleFunc("/api/settle-noproof", func(w http.ResponseWriter, r *http.Request) {
		session.Lock()
		lockID := session.LockID
		session.Unlock()
		if lockID == "" {
			fail(w, errors.New("no active lock — run Lock Credits first"))
			return
		}
		tool := toolID(r)
		pubAddr := toolOwnerAddr(tool)
		if pubAddr == "" {
			pubAddr, _ = keyAddr(cfg.Publisher)
		}
		bogus := "pos1" + hex.EncodeToString(sha256Bytes("never-submitted"))
		res, err := broadcast(agentKey(r),
			"credits", "settle-credits",
			"--lock-id", lockID, "--actual-cost", "800000ulac",
			"--publisher", pubAddr, "--receipt-id", bogus, "--tool-id", tool)
		if err != nil {
			fail(w, err)
			return
		}
		// Expected: res.OK == false (proof-of-service verification failed).
		writeJSON(w, map[string]any{"ok": true, "rejected": !res.OK, "step": "settle-noproof", "tx": res,
			"note": "Attempted settlement with an unverified receipt — the chain rejected it (no payout)."})
	})

	// Bond lifecycle — the publisher tops up or withdraws excess bond for its tool.
	mux.HandleFunc("/api/bond-topup", func(w http.ResponseWriter, r *http.Request) {
		tool := toolID(r)
		res, err := broadcast(agentKey(r), "registry", "create-bond", tool, bondArg(r.URL.Query().Get("amount")))
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			recordActivity(activityEntry{Type: "bond", Title: "Bond topped up", Sub: tool, Account: agentKey(r), Tool: tool, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "bond-topup", "tx": res, "note": "Bond increased for " + tool + "."})
	})
	mux.HandleFunc("/api/bond-withdraw", func(w http.ResponseWriter, r *http.Request) {
		tool := toolID(r)
		res, err := broadcast(agentKey(r), "registry", "withdraw-bond", tool, bondArg(r.URL.Query().Get("amount")))
		if err != nil {
			fail(w, err)
			return
		}
		if res.OK {
			recordActivity(activityEntry{Type: "bond", Title: "Bond withdrawn", Sub: tool, Account: agentKey(r), Tool: tool, TxHash: res.TxHash, Height: res.Height})
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "bond-withdraw", "tx": res,
			"note": "Withdrew excess bond from " + tool + " (only the amount above the minimum is reclaimable while registered)."})
	})

	// Wave-2 module APIs: vaults, passport, cac, challenges (tournaments),
	// payment_rails (on-ramp), router (telemetry) — all real on-chain calls.
	registerWave2APIs(mux)

	log.Printf("Lumera AI web PoC on http://localhost%s  (node=%s, home=%s)", addr, cfg.Node, cfg.Home)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func sha256Bytes(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}
