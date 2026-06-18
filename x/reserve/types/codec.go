
// Package types provides codec registration utilities for the reserve module.
package types

import (
	"sync"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/types/tx"
	gogoproto "github.com/cosmos/gogoproto/proto"
)

// RegisterLegacyAminoCodec registers concrete reserve messages.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgCreateCommitment{}, "lumera/reserve/MsgCreateCommitment", nil)
	cdc.RegisterConcrete(&MsgReleaseExpired{}, "lumera/reserve/MsgReleaseExpired", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "lumera/reserve/MsgUpdateParams", nil)
}

// RegisterInterfaces registers reserve messages and Msg service descriptors.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registerGogoDescriptors()

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

	msgservice.RegisterMsgServiceDesc(registry, &Msg_ServiceDesc)
}

var (
	// Amino exposes the module's legacy amino codec.
	Amino = codec.NewLegacyAmino()
	// ModuleCdc encodes reserve module messages and state using protobuf.
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
)

var registerOnce sync.Once

func registerGogoDescriptors() {
	registerOnce.Do(func() {
		gogoproto.RegisterFile("lumera/reserve/v1/reserve.proto", file_lumera_reserve_v1_reserve_proto_rawDescGZIP())
		gogoproto.RegisterFile("lumera/reserve/v1/tx.proto", file_lumera_reserve_v1_tx_proto_rawDescGZIP())
		gogoproto.RegisterType((*ReserveTierConfig)(nil), "lumera.reserve.v1.ReserveTierConfig")
		gogoproto.RegisterType((*ReserveParams)(nil), "lumera.reserve.v1.ReserveParams")
		gogoproto.RegisterType((*ReserveCommitmentSummary)(nil), "lumera.reserve.v1.ReserveCommitmentSummary")
		gogoproto.RegisterType((*MsgCreateCommitment)(nil), "lumera.reserve.v1.MsgCreateCommitment")
		gogoproto.RegisterType((*MsgCreateCommitmentResponse)(nil), "lumera.reserve.v1.MsgCreateCommitmentResponse")
		gogoproto.RegisterType((*MsgReleaseExpired)(nil), "lumera.reserve.v1.MsgReleaseExpired")
		gogoproto.RegisterType((*MsgReleaseExpiredResponse)(nil), "lumera.reserve.v1.MsgReleaseExpiredResponse")
		gogoproto.RegisterType((*MsgUpdateParams)(nil), "lumera.reserve.v1.MsgUpdateParams")
		gogoproto.RegisterType((*MsgUpdateParamsResponse)(nil), "lumera.reserve.v1.MsgUpdateParamsResponse")
	})
}

func init() {
	registerGogoDescriptors()
	RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
