# Lumera MCP Router — the agent-facing layer

`lumera-mcp-router` is the off-chain **router** that makes the on-chain Lumera AI
economy usable by real AI agents. It is an **MCP (Model Context Protocol)
server**: any MCP-compatible agent (Claude Desktop, an Agent SDK app, an IDE
assistant) connects to it, discovers the tools published on-chain, and calls
them. Every call is **metered, executed, proven, and settled on-chain**:

```
agent --(MCP: tools/call)--> router
        router: lock credits
              → execute the tool (off-chain)
              → submit SuperNode Proof-of-Service receipt = BLAKE3(input,model,output)
              → settle (gated on the receipt → pays the publisher)
        router --(result + on-chain proof)--> agent
```

This is the "router pivot" from `docs/LUMERA_AI_INTEGRATION_PLAN.md`: the on-chain
part of the router was only telemetry; its real form is this daemon.

## Run it

1. **Boot a prepared local node** (creates keys, registers a SuperNode, funds the
   publisher, seeds demo metrics):
   ```sh
   go build -o /tmp/lumerad ./cmd/lumera
   bash poc/web/run-localnet.sh
   ```
2. **Publish a tool** (the publisher escrows a bond):
   ```sh
   /tmp/lumerad tx registry register-tool pubtool --from pub \
     --home /tmp/lnode_web --node tcp://localhost:26657 --chain-id lumera-local-1 \
     --keyring-backend test --gas 700000 --fees 200000ulume -y
   ```
3. **Build + run the router** (it speaks JSON-RPC over stdio):
   ```sh
   go build -o /tmp/lumera-mcp-router ./poc/mcp-router
   LUMERA_HOME=/tmp/lnode_web /tmp/lumera-mcp-router
   ```

## Connect an AI agent (e.g. Claude Desktop)

Add to your MCP client config:
```json
{
  "mcpServers": {
    "lumera": {
      "command": "/tmp/lumera-mcp-router",
      "env": { "LUMERA_HOME": "/tmp/lnode_web", "LUMERA_AGENT": "val", "LUMERA_SUPERNODE": "val" }
    }
  }
}
```
The agent will see each on-chain tool, and calling one runs the full
lock → execute → Proof-of-Service → settle loop, returning the result plus its
on-chain proof (`receipt_id`, publisher paid, "settlement gated on the receipt").

## Configuration (env)

| var | default | meaning |
|---|---|---|
| `LUMERAD` | `/tmp/lumerad` | node binary |
| `LUMERA_HOME` | `/tmp/lnode_web` | node home (holds the test keyring) |
| `LUMERA_NODE` | `tcp://localhost:26657` | RPC endpoint |
| `LUMERA_CHAIN_ID` | `lumera-local-1` | chain id |
| `LUMERA_AGENT` | `val` | key that pays (locks + settles) |
| `LUMERA_SUPERNODE` | `val` | active-SuperNode key that attests the receipt |
| `LUMERA_LOCK` / `LUMERA_COST` | `1000000ulac` / `800000ulac` | quote / actual cost |

## PoC scope

- Tool *execution* (`executeTool`) is a placeholder transform — real tools (LLM
  inference, APIs, compute) plug in there. The novel part (meter + **prove** +
  settle every call) is real and on-chain.
- The router drives the chain by shelling `lumerad` with the **test keyring** —
  fine for a local PoC, not a production key-management pattern. A production
  router would hold the agent's session key / use a signing service, and stream
  results over MCP's HTTP+SSE transport for remote agents.
- Next: feed the incentives reputation engine from these receipts (Phase-2
  self-feed), and add `quote`/`activate` MCP methods + the content-addressed
  result cache.
