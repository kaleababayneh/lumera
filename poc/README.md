# Lumera AI — proof-of-concept (the agentic economy, end-to-end)

This directory demonstrates the complete Lumera AI vision running on a local
node: AI agents **discover** tools, the chain **meters** + **executes** +
**proves** (SuperNode Proof-of-Service) + **settles** every call, and a
self-reinforcing **trust graph** (bonds, disputes, reputation) governs it.

## One-command demo

```sh
bash poc/demo.sh
```

Boots a fresh local node and narrates the whole flywheel:

1. Publisher lists a tool and escrows a **bond** (skin-in-the-game).
2. Agent buys LAC credits and **locks** payment for a call.
3. Settlement is **gated on proof** — without a receipt it is rejected.
4. A **SuperNode Proof-of-Service receipt** (`BLAKE3(input,model,output)`) is
   anchored; settlement then **pays the publisher**.
5. **Reputation is earned** from the real receipts (a verification badge).
6. A bad call is **disputed and upheld** → the publisher's **bond is slashed**
   (restitution-routed: 5% burn / 85% insurance / 10% treasury).
7. **Reputation erodes** after the dispute (grace period, then downgrade).
8. An **AI agent calls the tool over MCP** — discover → meter → prove → settle.

## Web dashboard (drive the whole flywheel in the browser)

`demo.sh` runs once and exits. The web dashboard (`poc/web/`) leaves a node
running and lets you trigger every step from the browser instead.

**Start from scratch** (from the repo root):

```sh
go build -o /tmp/lumerad ./cmd/lumera             # build the chain binary (if not already)
bash poc/web/run-localnet.sh                      # boot a FRESH node (wipes /tmp/lumera-web, seeds the marketplace, builds the router)
go build -o /tmp/lumera-poc-web ./poc/web         # build the dashboard server
LUMERA_HOME=/tmp/lumera-web /tmp/lumera-poc-web   # serve on :8787 (Ctrl-C to stop)
# then open http://localhost:8787
```

**Stop everything:**

```sh
pkill -f lumera-poc-web        # the dashboard server (:8787)
pkill -f lumera-mcp-router     # the MCP router, if running
pkill -f "lumerad start"       # all local lumera nodes
```

`run-localnet.sh` re-wipes `/tmp/lumera-web` on every run, so it always boots
clean — but it only kills the node, **not** the `:8787` server. Kill
`lumera-poc-web` yourself before restarting, or the old server keeps talking to
the now-dead node.

## Components

| Path | What it is |
|---|---|
| `poc/demo.sh` | the narrated full-vision walkthrough + regression (above) |
| `poc/web/` | a browser dashboard that drives the **whole** flywheel over HTTP — swap → register+bond → lock → prove → settle → reputation → **dispute → slash → reputation erosion** — step-by-step or via one "Run the whole flywheel" button. `run-localnet.sh` boots a prepared node (agent/supernode `val`, publisher `pub`, challenger `chl`); `go run ./poc/web` serves it on :8787 |
| `poc/mcp-router/` | the **MCP server** — the off-chain "router". Any MCP-compatible AI agent (Claude Desktop, an Agent SDK app) discovers + calls on-chain tools through the standard protocol; each call runs lock → execute → Proof-of-Service → settle. See its README for a Claude-Desktop config. |

## What's real vs. PoC

**Real + on-chain (verified e2e):** tool registry + discovery, publisher bonds,
quote/lock/settle, SuperNode Proof-of-Service receipts gating settlement,
receipt disputes (uphold→slash + restitution, reject-on-expiry), the reputation
badge engine, and the trust-graph self-feed (receipts raise reputation, upheld
disputes erode it).

**PoC shortcuts:** tool *execution* is a placeholder transform (real tools plug
into `mcp-router/executeTool`); the daemons drive the chain by shelling
`lumerad` with the test keyring (not a production key-management pattern); the
reputation demo seeds the off-chain-reported metric dimensions (uptime, latency,
SBOM…) in genesis while the on-chain usage dimensions self-feed.

See `docs/LUMERA_AI_INTEGRATION_PLAN.md` and `CLAUDE.md` for the full status.
