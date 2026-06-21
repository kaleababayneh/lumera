package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/types/tx"
)

// RegisterLegacyAminoCodec registers the incentives module's messages on the
// LegacyAmino codec (Amino JSON serialization).
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgRecordMetrics{}, "incentives/RecordMetrics", nil)
	cdc.RegisterConcrete(&MsgRequestEvaluation{}, "incentives/RequestEvaluation", nil)
	cdc.RegisterConcrete(&MsgUpdateTierConfig{}, "incentives/UpdateTierConfig", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "incentives/UpdateParams", nil)
	cdc.RegisterConcrete(&MsgRevokeBadge{}, "incentives/RevokeBadge", nil)
}

// RegisterInterfaces wires the incentives message implementations into the SDK
// interface registry.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRecordMetrics{},
		&MsgRequestEvaluation{},
		&MsgUpdateTierConfig{},
		&MsgUpdateParams{},
		&MsgRevokeBadge{},
	)

	registry.RegisterImplementations((*tx.MsgResponse)(nil),
		&MsgRecordMetricsResponse{},
		&MsgRequestEvaluationResponse{},
		&MsgUpdateTierConfigResponse{},
		&MsgUpdateParamsResponse{},
		&MsgRevokeBadgeResponse{},
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
