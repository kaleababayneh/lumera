// Package types registers codec utilities for the challenges module.
package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/types/tx"
)

// RegisterLegacyAminoCodec registers module types for legacy amino encoding.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgCreateChallenge{}, "challenges/CreateChallenge", nil)
	cdc.RegisterConcrete(&MsgJoinChallenge{}, "challenges/JoinChallenge", nil)
	cdc.RegisterConcrete(&MsgSubmitResult{}, "challenges/SubmitResult", nil)
	cdc.RegisterConcrete(&MsgActivateChallenge{}, "challenges/ActivateChallenge", nil)
	cdc.RegisterConcrete(&MsgCancelChallenge{}, "challenges/CancelChallenge", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "challenges/UpdateParams", nil)
}

// RegisterInterfaces wires message implementations into the Cosmos SDK interface
// registry. The gogoproto type/file descriptors are registered automatically by
// the generated .pb.go init(), so no manual descriptor registration is needed.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateChallenge{},
		&MsgJoinChallenge{},
		&MsgSubmitResult{},
		&MsgActivateChallenge{},
		&MsgCancelChallenge{},
		&MsgUpdateParams{},
	)

	registry.RegisterImplementations((*tx.MsgResponse)(nil),
		&MsgCreateChallengeResponse{},
		&MsgJoinChallengeResponse{},
		&MsgSubmitResultResponse{},
		&MsgActivateChallengeResponse{},
		&MsgCancelChallengeResponse{},
		&MsgUpdateParamsResponse{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

// ModuleCdc encodes and decodes challenges module messages using protobuf.
var ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
