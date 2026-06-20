
// Package types holds shared types and helpers for the credits module.
//
//revive:disable:var-naming // Package name follows Cosmos SDK conventions.
package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/types/tx"
)

// RegisterLegacyAminoCodec registers concrete message types on the provided codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgLockCredits{}, "lumera/credits/MsgLockCredits", nil)
	cdc.RegisterConcrete(&MsgSettleCredits{}, "lumera/credits/MsgSettleCredits", nil)
	cdc.RegisterConcrete(&MsgSettleOverdraft{}, "lumera/credits/MsgSettleOverdraft", nil)
	cdc.RegisterConcrete(&MsgUnlockCredits{}, "lumera/credits/MsgUnlockCredits", nil)
	cdc.RegisterConcrete(&MsgSwapLUMEtoLAC{}, "lumera/credits/MsgSwapLUMEtoLAC", nil)
	cdc.RegisterConcrete(&MsgSwapLACtoLUME{}, "lumera/credits/MsgSwapLACtoLUME", nil)
}

// RegisterInterfaces registers interfaces types with the interface registry.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgLockCredits{},
		&MsgSettleCredits{},
		&MsgSettleOverdraft{},
		&MsgUnlockCredits{},
		&MsgSwapLUMEtoLAC{},
		&MsgSwapLACtoLUME{},
		&MsgUpdateParams{},
	)

	registry.RegisterImplementations((*tx.MsgResponse)(nil),
		&MsgSwapLUMEtoLACResponse{},
		&MsgSwapLACtoLUMEResponse{},
		&MsgLockCreditsResponse{},
		&MsgUnlockCreditsResponse{},
		&MsgSettleCreditsResponse{},
		&MsgSettleOverdraftResponse{},
		&MsgUpdateParamsResponse{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &Msg_serviceDesc)
}

var (
	// Amino is the module-wide Amino codec.
	Amino = codec.NewLegacyAmino()
	// ModuleCdc references the global module codec.
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
)

func init() {
	// The gogoproto-generated code self-registers its file + message descriptors
	// in its own init(); only amino registration remains hand-wired here.
	RegisterLegacyAminoCodec(Amino)
	sdk.RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
