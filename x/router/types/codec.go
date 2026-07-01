// Package types provides codec registration helpers for the router module.
package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/types/tx"
)

// RegisterLegacyAminoCodec registers router message types on the provided LegacyAmino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgUpdateParams{}, "lumera/router/MsgUpdateParams", nil)
	cdc.RegisterConcrete(&MsgRecordActivation{}, "lumera/router/MsgRecordActivation", nil)
	cdc.RegisterConcrete(&MsgRecordInvocation{}, "lumera/router/MsgRecordInvocation", nil)
	cdc.RegisterConcrete(&MsgRecordPolicyUpdate{}, "lumera/router/MsgRecordPolicyUpdate", nil)
	cdc.RegisterConcrete(&MsgRecordCACHit{}, "lumera/router/MsgRecordCACHit", nil)
	cdc.RegisterConcrete(&MsgAggregateMetrics{}, "lumera/router/MsgAggregateMetrics", nil)
}

// RegisterInterfaces wires router message implementations into the Cosmos SDK interface registry.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgUpdateParams{},
		&MsgRecordActivation{},
		&MsgRecordInvocation{},
		&MsgRecordPolicyUpdate{},
		&MsgRecordCACHit{},
		&MsgAggregateMetrics{},
	)

	registry.RegisterImplementations(
		(*tx.MsgResponse)(nil),
		&MsgUpdateParamsResponse{},
		&MsgRecordActivationResponse{},
		&MsgRecordInvocationResponse{},
		&MsgRecordPolicyUpdateResponse{},
		&MsgRecordCACHitResponse{},
		&MsgAggregateMetricsResponse{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

var (
	// Amino is the module-wide Amino codec.
	Amino = codec.NewLegacyAmino()
	// ModuleCdc references the global module codec.
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
)

func init() {
	// gogoproto type/file descriptors are registered by the generated .pb.go init().
	RegisterLegacyAminoCodec(Amino)
	sdk.RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
