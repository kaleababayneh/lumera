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
)

//go:embed index.html
var indexHTML []byte

// ---- config -----------------------------------------------------------------

type config struct {
	Bin         string // lumerad binary
	Home        string // node home (holds the test keyring)
	Node        string // tcp rpc endpoint
	ChainID     string
	Agent       string // key name: agent + router + supernode (val)
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
	Publisher:   envOr("LUMERA_PUBLISHER", "pub"),
	Challenger:  envOr("LUMERA_CHALLENGER", "chl"),
	Tool:        envOr("LUMERA_TOOL", "pubtool"),
	Fees:        envOr("LUMERA_FEES", "200000ulume"),
	Gas:         envOr("LUMERA_GAS", "700000"),
	LumeDenom:   "ulume",
	CreditDenom: "ulac",
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

	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		agentAddr, _ := keyAddr(cfg.Agent)
		pubAddr, _ := keyAddr(cfg.Publisher)
		chlAddr, _ := keyAddr(cfg.Challenger)
		regAddr := moduleAddr("registry")
		st := map[string]any{
			"ok":             true,
			"agentLume":      balance(agentAddr, cfg.LumeDenom),
			"agentLac":       balance(agentAddr, cfg.CreditDenom),
			"publisherLac":   balance(pubAddr, cfg.CreditDenom),
			"publisherLume":  balance(pubAddr, cfg.LumeDenom),
			"challengerLume": balance(chlAddr, cfg.LumeDenom),
			"registryBond":   balance(regAddr, cfg.LumeDenom),
		}
		session.Lock()
		st["lockId"], st["receiptId"] = session.LockID, session.ReceiptID
		st["challengeOpen"] = session.ChallengeOpen
		st["slashAmount"], st["slashBurn"] = session.SlashAmount, session.SlashBurn
		st["slashInsurance"], st["slashTreasury"] = session.SlashInsurance, session.SlashTreasury
		session.Unlock()
		// publisher bond: bonded vs cumulatively slashed (skin-in-the-game gauge).
		st["bondBonded"], st["bondSlashed"] = bondAmounts(cfg.Tool)
		// tool + receipt status (best-effort)
		if m, err := query("registry", "get-tool", cfg.Tool); err == nil {
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
		if m, err := query("incentives", "badge", cfg.Tool); err == nil {
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
		writeJSON(w, st)
	})

	// Step 1 — Agent swaps LUME for LAC credits.
	mux.HandleFunc("/api/swap", func(w http.ResponseWriter, r *http.Request) {
		res, err := broadcast(cfg.Agent,
			"credits", "swap-lume-to-lac", "--amount", "5000000ulume", "--min-lac-out", "1")
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "swap", "tx": res,
			"note": "Agent converted 5,000,000 ulume into LAC credits."})
	})

	// Step 2 — Publisher registers a tool and escrows a bond.
	mux.HandleFunc("/api/register", func(w http.ResponseWriter, r *http.Request) {
		res, err := broadcast(cfg.Publisher,
			"registry", "register-tool", cfg.Tool, "--bond", "2000000ulume")
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": res.OK, "step": "register", "tx": res,
			"note": "Publisher registered '" + cfg.Tool + "' and escrowed a 2,000,000 ulume bond (skin-in-the-game)."})
	})

	// Step 3 — Router locks credits against the tool (quote → lock).
	mux.HandleFunc("/api/lock", func(w http.ResponseWriter, r *http.Request) {
		seq := nextSeq()
		res, err := broadcast(cfg.Agent,
			"credits", "lock-credits",
			"--amount", "1000000ulac",
			"--session-id", "web-"+strconv.Itoa(seq),
			"--tool-id", cfg.Tool,
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
		res, err := broadcast(cfg.Agent,
			"registry", "submit-receipt", cfg.Tool,
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
		pubAddr, err := keyAddr(cfg.Publisher)
		if err != nil {
			fail(w, err)
			return
		}
		res, err := broadcast(cfg.Agent,
			"credits", "settle-credits",
			"--lock-id", lockID, "--actual-cost", "800000ulac",
			"--publisher", pubAddr, "--receipt-id", receiptID, "--tool-id", cfg.Tool)
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

	// Reputation — the publisher requests a badge evaluation (incentives).
	mux.HandleFunc("/api/request-badge", func(w http.ResponseWriter, r *http.Request) {
		res, err := broadcast(cfg.Publisher, "incentives", "request-evaluation", cfg.Tool)
		if err != nil {
			fail(w, err)
			return
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
		pubAddr, _ := keyAddr(cfg.Publisher)
		bogus := "pos1" + hex.EncodeToString(sha256Bytes("never-submitted"))
		res, err := broadcast(cfg.Agent,
			"credits", "settle-credits",
			"--lock-id", lockID, "--actual-cost", "800000ulac",
			"--publisher", pubAddr, "--receipt-id", bogus, "--tool-id", cfg.Tool)
		if err != nil {
			fail(w, err)
			return
		}
		// Expected: res.OK == false (proof-of-service verification failed).
		writeJSON(w, map[string]any{"ok": true, "rejected": !res.OK, "step": "settle-noproof", "tx": res,
			"note": "Attempted settlement with an unverified receipt — the chain rejected it (no payout)."})
	})

	log.Printf("Lumera AI web PoC on http://localhost%s  (node=%s, home=%s)", addr, cfg.Node, cfg.Home)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func sha256Bytes(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}
