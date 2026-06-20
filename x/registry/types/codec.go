package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/types/tx"
)

// RegisterLegacyAminoCodec registers the necessary x/registry interfaces and concrete types
// on the provided LegacyAmino codec. These types are used for Amino JSON serialization.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	// Register messages
	cdc.RegisterConcrete(&MsgRegisterTool{}, "registry/RegisterTool", nil)
	cdc.RegisterConcrete(&MsgUpdateTool{}, "registry/UpdateTool", nil)
	cdc.RegisterConcrete(&MsgDelistTool{}, "registry/DelistTool", nil)
	cdc.RegisterConcrete(&MsgSubmitReceipt{}, "registry/SubmitReceipt", nil)
	cdc.RegisterConcrete(&MsgAnchorBundle{}, "registry/AnchorBundle", nil)
	cdc.RegisterConcrete(&MsgChallengeReceipt{}, "registry/ChallengeReceipt", nil)
	cdc.RegisterConcrete(&MsgSettleReceipt{}, "registry/SettleReceipt", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "registry/UpdateParams", nil)
	cdc.RegisterConcrete(&MsgSetSLATemplate{}, "registry/SetSLATemplate", nil)
	cdc.RegisterConcrete(&MsgSetDisputeTerms{}, "registry/SetDisputeTerms", nil)
	cdc.RegisterConcrete(&MsgCreateBond{}, "registry/CreateBond", nil)
	cdc.RegisterConcrete(&MsgWithdrawBond{}, "registry/WithdrawBond", nil)
	cdc.RegisterConcrete(&MsgSetLaneRegistryEntry{}, "registry/SetLaneRegistryEntry", nil)
	cdc.RegisterConcrete(&MsgSetToolCapsule{}, "registry/SetToolCapsule", nil)
	cdc.RegisterConcrete(&MsgRegisterWatcher{}, "registry/RegisterWatcher", nil)
	cdc.RegisterConcrete(&MsgUnregisterWatcher{}, "registry/UnregisterWatcher", nil)
	cdc.RegisterConcrete(&MsgSubmitSLOProbeReceipt{}, "registry/SubmitSLOProbeReceipt", nil)
	cdc.RegisterConcrete(&MsgSetOriginRoutingConfig{}, "registry/SetOriginRoutingConfig", nil)
}

// RegisterInterfaces registers the x/registry interfaces types with the interface registry
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterTool{},
		&MsgUpdateTool{},
		&MsgDelistTool{},
		&MsgSubmitReceipt{},
		&MsgAnchorBundle{},
		&MsgChallengeReceipt{},
		&MsgSettleReceipt{},
		&MsgUpdateParams{},
		&MsgSetSLATemplate{},
		&MsgSetDisputeTerms{},
		&MsgCreateBond{},
		&MsgWithdrawBond{},
		&MsgSetLaneRegistryEntry{},
		&MsgSetToolCapsule{},
		&MsgRegisterWatcher{},
		&MsgUnregisterWatcher{},
		&MsgSubmitSLOProbeReceipt{},
		&MsgSetOriginRoutingConfig{},
	)

	registry.RegisterImplementations((*tx.MsgResponse)(nil),
		&MsgRegisterToolResponse{},
		&MsgUpdateToolResponse{},
		&MsgDelistToolResponse{},
		&MsgSubmitReceiptResponse{},
		&MsgAnchorBundleResponse{},
		&MsgChallengeReceiptResponse{},
		&MsgSettleReceiptResponse{},
		&MsgUpdateParamsResponse{},
		&MsgSetSLATemplateResponse{},
		&MsgSetDisputeTermsResponse{},
		&MsgCreateBondResponse{},
		&MsgWithdrawBondResponse{},
		&MsgSetLaneRegistryEntryResponse{},
		&MsgSetToolCapsuleResponse{},
		&MsgRegisterWatcherResponse{},
		&MsgUnregisterWatcherResponse{},
		&MsgSubmitSLOProbeReceiptResponse{},
		&MsgSetOriginRoutingConfigResponse{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &Msg_serviceDesc)
}

var (
	// Amino is the legacy codec instance used for backwards compatibility.
	Amino = codec.NewLegacyAmino()
	// ModuleCdc is the module-wide Proto codec for binary encoding.
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
)

func init() {
	RegisterLegacyAminoCodec(Amino)
	sdk.RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
