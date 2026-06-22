package main

// decoder.go builds a fully-wired Cosmos codec / TxConfig OUTSIDE the running
// node, so the explorer can decode every module's transactions in-process —
// no per-module decode code, no shelling out to the lumerad CLI. The recipe
// mirrors cmd/lumera/cmd/root.go: inject a client.Context out of the app's
// depinject graph, then manually register the IBC, EVM and CosmWasm module
// interfaces (those modules are not depinject-wired) so Any-typed messages from
// them unpack too.

import (
	"os"
	"strings"

	"cosmossdk.io/depinject"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtxconfig "github.com/cosmos/cosmos-sdk/x/auth/tx/config"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	evmhd "github.com/cosmos/evm/crypto/hd"
	"github.com/spf13/viper"

	"github.com/LumeraProtocol/lumera/app"
	appevm "github.com/LumeraProtocol/lumera/app/evm"
)

// newClientCtx returns a client.Context whose Codec + TxConfig can decode any
// message type registered by any module compiled into lumerad.
func newClientCtx() (client.Context, error) {
	var appOpts servertypes.AppOptions = viper.New()
	var clientCtx client.Context
	if err := depinject.Inject(
		depinject.Configs(
			app.AppConfig(appOpts),
			depinject.Supply(log.NewNopLogger()),
			depinject.Provide(ProvideClientContext),
		),
		&clientCtx,
	); err != nil {
		return client.Context{}, err
	}

	// IBC, EVM and CosmWasm modules are manually wired in app.go and are NOT in
	// the depinject graph, so their interfaces must be registered explicitly for
	// Any decoding (this is exactly what root.go does for the CLI).
	app.RegisterIBC(clientCtx.Codec)
	appevm.RegisterModules(clientCtx.Codec)
	app.RegisterWasm(clientCtx.Codec)

	return clientCtx, nil
}

// ProvideClientContext is a trimmed copy of cmd.ProvideClientContext — just
// enough to build a decoding-capable client.Context for a non-CLI process.
func ProvideClientContext(
	appCodec codec.Codec,
	interfaceRegistry codectypes.InterfaceRegistry,
	txConfigOpts tx.ConfigOptions,
	legacyAmino *codec.LegacyAmino,
) client.Context {
	clientCtx := client.Context{}.
		WithCodec(appCodec).
		WithInterfaceRegistry(interfaceRegistry).
		WithLegacyAmino(legacyAmino).
		WithInput(os.Stdin).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithHomeDir(app.DefaultNodeHome).
		WithViper(app.Name).
		WithKeyringOptions(evmhd.EthSecp256k1Option()).
		WithLedgerHasProtobuf(true)

	clientCtx, _ = config.ReadFromClientConfig(clientCtx)

	txConfigOpts.TextualCoinMetadataQueryFn = authtxconfig.NewGRPCCoinMetadataQueryFn(clientCtx)
	txConfig, err := tx.NewTxConfigWithOptions(clientCtx.Codec, txConfigOpts)
	if err != nil {
		panic(err)
	}
	clientCtx = clientCtx.WithTxConfig(txConfig)

	return clientCtx
}

// ---------------------------------------------------------------------------
// Module / label attribution
// ---------------------------------------------------------------------------

// moduleOf maps a proto type URL to the owning module name, e.g.
//
//	/lumera.registry.v1.MsgRegisterTool   -> registry
//	/lumera.claim.MsgClaim                -> claim
//	/cosmos.bank.v1beta1.MsgSend          -> bank
//	/cosmos.evm.vm.v1.MsgEthereumTx       -> vm
//	/ibc.applications.transfer.v1.Msg...  -> ibc
//	/cosmwasm.wasm.v1.MsgExecuteContract  -> wasm
func moduleOf(typeURL string) string {
	s := strings.TrimPrefix(typeURL, "/")
	p := strings.Split(s, ".")
	switch {
	case strings.HasPrefix(s, "lumera.") && len(p) > 1:
		return p[1]
	case strings.HasPrefix(s, "cosmos.evm."):
		// collapse vm/erc20/feemarket/precisebank under the single "vm" card
		return "vm"
	case strings.HasPrefix(s, "cosmos.") && len(p) > 1:
		return p[1]
	case strings.HasPrefix(s, "ibc."):
		return "ibc"
	case strings.HasPrefix(s, "cosmwasm."):
		return "wasm"
	default:
		if len(p) > 1 {
			return p[1]
		}
		return "chain"
	}
}

// groupOf buckets a module into a high-level family used for colour coding.
func groupOf(typeURL string) string {
	s := strings.TrimPrefix(typeURL, "/")
	switch {
	case strings.HasPrefix(s, "lumera."):
		return "lumera"
	case strings.HasPrefix(s, "cosmos.evm."):
		return "evm"
	case strings.HasPrefix(s, "ibc."):
		return "ibc"
	case strings.HasPrefix(s, "cosmwasm."):
		return "wasm"
	default:
		return "cosmos"
	}
}

// labelOverride handles message names whose lowercase connector words ("to")
// can't be recovered from camel case alone (no capital marks the boundary).
var labelOverride = map[string]string{
	"MsgSwapLUMEtoLAC": "Swap LUME to LAC",
	"MsgSwapLACtoLUME": "Swap LAC to LUME",
}

// labelOf turns the trailing message name into a human label:
// "/lumera.registry.v1.MsgRegisterTool" -> "Register Tool".
func labelOf(typeURL string) string {
	p := strings.Split(strings.TrimPrefix(typeURL, "/"), ".")
	name := p[len(p)-1]
	if o, ok := labelOverride[name]; ok {
		return o
	}
	return splitCamel(strings.TrimPrefix(name, "Msg"))
}

// splitCamel inserts word breaks for CamelCase, keeping acronym runs together
// but splitting where an acronym run ends and a new capitalised word begins
// (e.g. "RegisterTool" -> "Register Tool", "SLOProbe" -> "SLO Probe",
// "SLATemplate" -> "SLA Template", "ERC20" stays "ERC20").
func splitCamel(s string) string {
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := rs[i-1]
			upperPrev := prev >= 'A' && prev <= 'Z'
			digitPrev := prev >= '0' && prev <= '9'
			switch {
			case !upperPrev && !digitPrev:
				// lower/other -> Upper: a normal word boundary
				b.WriteByte(' ')
			case upperPrev && i+1 < len(rs) && rs[i+1] >= 'a' && rs[i+1] <= 'z':
				// last capital of an acronym run that starts a new word
				b.WriteByte(' ')
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// eventModule attributes a (string-keyed) ABCI event type to its owning module.
// Custom Lumera modules emit only string events, so the explorer keys on the
// event-type string. Explicit names first, then prefix rules, then the standard
// Cosmos event set, else "chain".
func eventModule(t string, attrs []EventAttr) string {
	// "slash" is emitted by BOTH the SDK slashing module and registry bond
	// slashing; the two are indistinguishable by type alone, so disambiguate by
	// the registry-only tool_id attribute (SDK slash carries address/power).
	if t == "slash" {
		for _, a := range attrs {
			if a.Key == "tool_id" {
				return "registry"
			}
		}
		return "cosmos"
	}
	if m, ok := eventModuleExact[t]; ok {
		return m
	}
	switch {
	case strings.HasPrefix(t, "storage_truth_"):
		return "audit"
	case strings.HasPrefix(t, "badge_"):
		return "incentives"
	case strings.HasPrefix(t, "insurance_"):
		return "insurance"
	case strings.HasPrefix(t, "toolpack_"):
		return "nft"
	case strings.HasPrefix(t, "oracle_"):
		return "oracle"
	case strings.HasPrefix(t, "supernode_"):
		return "supernode"
	case strings.HasPrefix(t, "cache_"):
		return "cac"
	case strings.HasPrefix(t, "policy_"):
		return "policies"
	case strings.HasPrefix(t, "receipt_mirror_"), strings.HasPrefix(t, "evidence_bundle_mirror_"),
		strings.HasPrefix(t, "slo_probe_"), strings.HasPrefix(t, "bond_"):
		return "registry"
	}
	if _, ok := cosmosEvents[t]; ok {
		return "cosmos"
	}
	return "chain"
}

// eventModuleExact maps specific custom event-type strings to their module.
var eventModuleExact = map[string]string{
	// action
	"action_registered": "action", "action_finalized": "action", "action_approved": "action",
	"action_failed": "action", "action_expired": "action", "action_finalization_rejected": "action",
	"svc_verification_passed": "action", "svc_verification_failed_evidence": "action",
	// cac
	"decay_tick": "cac", "tier_promotion": "cac", "royalty_distributed": "cac",
	// claim
	"claim_processed": "claim", "delayed_claim_processed": "claim",
	"claim_period_end": "claim", "burn_unclaimed_tokens": "claim",
	// credits
	"lume_lac_swap": "credits", "credit_lock": "credits", "credit_unlock": "credits",
	"settlement": "credits", "settlement_dispute": "credits", "lac_burn": "credits",
	"revenue_distribute": "credits", "receipt_verified": "credits",
	"cac_royalty_distribution": "credits", "adaptive_burn_rate_evaluated": "credits",
	"adaptive_burn_rate_reason": "credits",
	// evmigration
	"claim_legacy_account": "evmigration", "migrate_validator": "evmigration",
	// registry ("slash" is handled by attribute in eventModule, not here)
	"tool_registered": "registry", "tool_updated": "registry", "tool_delisted": "registry",
	"receipt_submitted": "registry", "receipt_settled": "registry", "receipt_failed": "registry",
	"receipt_queued": "registry", "receipt_challenged": "registry", "challenge_resolved": "registry",
	"watcher_registered": "registry", "watcher_unregistered": "registry",
	"watcher_slashed": "registry", "quality_score": "registry", "quality_rebate": "registry",
	"verified_status_changed": "registry", "origin_routing_config_set": "registry",
	"origin_share_routed": "registry", "governance_param_change": "registry",
	"finalize_block_settlement_receipt": "registry",
	// supernode
	"reward_distribution": "supernode",
	// evm
	"ethereum_tx": "vm", "tx_log": "vm", "block_bloom": "vm",
}

// cosmosEvents is the standard SDK / staking / bank / gov event set — used to
// attribute generic events to "cosmos" rather than "chain".
var cosmosEvents = map[string]struct{}{
	"message": {}, "tx": {}, "coin_received": {}, "coin_spent": {}, "transfer": {},
	"coinbase": {}, "mint": {}, "burn": {}, "withdraw_rewards": {}, "withdraw_commission": {},
	"delegate": {}, "unbond": {}, "redelegate": {}, "create_validator": {}, "edit_validator": {},
	"proposal_vote": {}, "proposal_deposit": {}, "submit_proposal": {}, "active_proposal": {},
	"inactive_proposal": {}, "commission": {}, "rewards": {}, "slash": {}, "liveness": {},
	"fungible_token_packet": {}, "ibc_transfer": {}, "send_packet": {}, "recv_packet": {},
	"acknowledge_packet": {}, "timeout_packet": {}, "update_client": {},
	"use_feegrant": {}, "set_feegrant": {},
}
