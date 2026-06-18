//go:build cosmos

package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/types/tx"
	gogoproto "github.com/cosmos/gogoproto/proto"
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

	msgservice.RegisterMsgServiceDesc(registry, &Msg_ServiceDesc)
}

var (
	// Amino is the legacy codec instance used for backwards compatibility.
	Amino = codec.NewLegacyAmino()
	// ModuleCdc is the module-wide Proto codec for binary encoding.
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
)

func init() {
	gogoproto.RegisterFile("lumera/registry/v1/tx.proto", file_lumera_registry_v1_tx_proto_rawDescGZIP())
	gogoproto.RegisterFile("lumera/registry/v1/query.proto", file_lumera_registry_v1_query_proto_rawDescGZIP())
	gogoproto.RegisterFile("lumera/registry/v1/types.proto", file_lumera_registry_v1_types_proto_rawDescGZIP())
	gogoproto.RegisterType((*MsgRegisterTool)(nil), "lumera.registry.v1.MsgRegisterTool")
	gogoproto.RegisterType((*MsgRegisterToolResponse)(nil), "lumera.registry.v1.MsgRegisterToolResponse")
	gogoproto.RegisterType((*MsgUpdateTool)(nil), "lumera.registry.v1.MsgUpdateTool")
	gogoproto.RegisterType((*MsgUpdateToolResponse)(nil), "lumera.registry.v1.MsgUpdateToolResponse")
	gogoproto.RegisterType((*MsgDelistTool)(nil), "lumera.registry.v1.MsgDelistTool")
	gogoproto.RegisterType((*MsgDelistToolResponse)(nil), "lumera.registry.v1.MsgDelistToolResponse")
	gogoproto.RegisterType((*MsgSubmitReceipt)(nil), "lumera.registry.v1.MsgSubmitReceipt")
	gogoproto.RegisterType((*MsgSubmitReceiptResponse)(nil), "lumera.registry.v1.MsgSubmitReceiptResponse")
	gogoproto.RegisterType((*MsgAnchorBundle)(nil), "lumera.registry.v1.MsgAnchorBundle")
	gogoproto.RegisterType((*MsgAnchorBundleResponse)(nil), "lumera.registry.v1.MsgAnchorBundleResponse")
	gogoproto.RegisterType((*MsgChallengeReceipt)(nil), "lumera.registry.v1.MsgChallengeReceipt")
	gogoproto.RegisterType((*MsgChallengeReceiptResponse)(nil), "lumera.registry.v1.MsgChallengeReceiptResponse")
	gogoproto.RegisterType((*MsgSettleReceipt)(nil), "lumera.registry.v1.MsgSettleReceipt")
	gogoproto.RegisterType((*MsgSettleReceiptResponse)(nil), "lumera.registry.v1.MsgSettleReceiptResponse")
	gogoproto.RegisterType((*MsgUpdateParams)(nil), "lumera.registry.v1.MsgUpdateParams")
	gogoproto.RegisterType((*MsgUpdateParamsResponse)(nil), "lumera.registry.v1.MsgUpdateParamsResponse")
	gogoproto.RegisterType((*MsgSetSLATemplate)(nil), "lumera.registry.v1.MsgSetSLATemplate")
	gogoproto.RegisterType((*MsgSetSLATemplateResponse)(nil), "lumera.registry.v1.MsgSetSLATemplateResponse")
	gogoproto.RegisterType((*MsgSetDisputeTerms)(nil), "lumera.registry.v1.MsgSetDisputeTerms")
	gogoproto.RegisterType((*MsgSetDisputeTermsResponse)(nil), "lumera.registry.v1.MsgSetDisputeTermsResponse")
	gogoproto.RegisterType((*MsgCreateBond)(nil), "lumera.registry.v1.MsgCreateBond")
	gogoproto.RegisterType((*MsgCreateBondResponse)(nil), "lumera.registry.v1.MsgCreateBondResponse")
	gogoproto.RegisterType((*MsgWithdrawBond)(nil), "lumera.registry.v1.MsgWithdrawBond")
	gogoproto.RegisterType((*MsgWithdrawBondResponse)(nil), "lumera.registry.v1.MsgWithdrawBondResponse")
	gogoproto.RegisterType((*MsgSetLaneRegistryEntry)(nil), "lumera.registry.v1.MsgSetLaneRegistryEntry")
	gogoproto.RegisterType((*MsgSetLaneRegistryEntryResponse)(nil), "lumera.registry.v1.MsgSetLaneRegistryEntryResponse")
	gogoproto.RegisterType((*MsgSetToolCapsule)(nil), "lumera.registry.v1.MsgSetToolCapsule")
	gogoproto.RegisterType((*MsgSetToolCapsuleResponse)(nil), "lumera.registry.v1.MsgSetToolCapsuleResponse")
	gogoproto.RegisterType((*MsgRegisterWatcher)(nil), "lumera.registry.v1.MsgRegisterWatcher")
	gogoproto.RegisterType((*MsgRegisterWatcherResponse)(nil), "lumera.registry.v1.MsgRegisterWatcherResponse")
	gogoproto.RegisterType((*MsgUnregisterWatcher)(nil), "lumera.registry.v1.MsgUnregisterWatcher")
	gogoproto.RegisterType((*MsgUnregisterWatcherResponse)(nil), "lumera.registry.v1.MsgUnregisterWatcherResponse")
	gogoproto.RegisterType((*MsgSubmitSLOProbeReceipt)(nil), "lumera.registry.v1.MsgSubmitSLOProbeReceipt")
	gogoproto.RegisterType((*MsgSubmitSLOProbeReceiptResponse)(nil), "lumera.registry.v1.MsgSubmitSLOProbeReceiptResponse")
	gogoproto.RegisterType((*MsgSetOriginRoutingConfig)(nil), "lumera.registry.v1.MsgSetOriginRoutingConfig")
	gogoproto.RegisterType((*MsgSetOriginRoutingConfigResponse)(nil), "lumera.registry.v1.MsgSetOriginRoutingConfigResponse")
	RegisterLegacyAminoCodec(Amino)
	sdk.RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}

// ProtoMessage implementations are generated in the .pb.go files
