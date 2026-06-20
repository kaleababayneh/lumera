
// Package types provides codec registration utilities for the reserve module.
package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/types/tx"
)

// RegisterLegacyAminoCodec registers concrete reserve messages.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgCreateCommitment{}, "lumera/reserve/MsgCreateCommitment", nil)
	cdc.RegisterConcrete(&MsgReleaseExpired{}, "lumera/reserve/MsgReleaseExpired", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "lumera/reserve/MsgUpdateParams", nil)
}

// RegisterInterfaces registers reserve messages and Msg service descriptors.
// The gogoproto-generated code self-registers its file and message descriptors
// in its own init(), so no manual proto registration is needed here.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateCommitment{},
		&MsgReleaseExpired{},
		&MsgUpdateParams{},
	)

	registry.RegisterImplementations((*tx.MsgResponse)(nil),
		&MsgCreateCommitmentResponse{},
		&MsgReleaseExpiredResponse{},
		&MsgUpdateParamsResponse{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &Msg_serviceDesc)
}

var (
	// Amino exposes the module's legacy amino codec.
	Amino = codec.NewLegacyAmino()
	// ModuleCdc encodes reserve module messages and state using protobuf.
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
)

func init() {
	RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
