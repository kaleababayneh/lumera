//go:build cosmos

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
	gogoproto "github.com/cosmos/gogoproto/proto"
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

	msgservice.RegisterMsgServiceDesc(registry, &Msg_ServiceDesc)
}

var (
	// Amino is the module-wide Amino codec.
	Amino = codec.NewLegacyAmino()
	// ModuleCdc references the global module codec.
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
)

func init() {
	gogoproto.RegisterFile("lumera/credits/v1/tx.proto", file_lumera_credits_v1_tx_proto_rawDescGZIP())
	gogoproto.RegisterType((*MsgSwapLUMEtoLAC)(nil), "lumera.credits.v1.MsgSwapLUMEtoLAC")
	gogoproto.RegisterType((*MsgSwapLUMEtoLACResponse)(nil), "lumera.credits.v1.MsgSwapLUMEtoLACResponse")
	gogoproto.RegisterType((*MsgSwapLACtoLUME)(nil), "lumera.credits.v1.MsgSwapLACtoLUME")
	gogoproto.RegisterType((*MsgSwapLACtoLUMEResponse)(nil), "lumera.credits.v1.MsgSwapLACtoLUMEResponse")
	gogoproto.RegisterType((*MsgLockCredits)(nil), "lumera.credits.v1.MsgLockCredits")
	gogoproto.RegisterType((*MsgLockCreditsResponse)(nil), "lumera.credits.v1.MsgLockCreditsResponse")
	gogoproto.RegisterType((*MsgUnlockCredits)(nil), "lumera.credits.v1.MsgUnlockCredits")
	gogoproto.RegisterType((*MsgUnlockCreditsResponse)(nil), "lumera.credits.v1.MsgUnlockCreditsResponse")
	gogoproto.RegisterType((*MsgSettleCredits)(nil), "lumera.credits.v1.MsgSettleCredits")
	gogoproto.RegisterType((*MsgSettleCreditsResponse)(nil), "lumera.credits.v1.MsgSettleCreditsResponse")
	gogoproto.RegisterType((*OverdraftSettlementSplit)(nil), "lumera.credits.v1.OverdraftSettlementSplit")
	gogoproto.RegisterType((*OverdraftSettlementEntry)(nil), "lumera.credits.v1.OverdraftSettlementEntry")
	gogoproto.RegisterType((*MsgSettleOverdraft)(nil), "lumera.credits.v1.MsgSettleOverdraft")
	gogoproto.RegisterType((*MsgSettleOverdraftResponse)(nil), "lumera.credits.v1.MsgSettleOverdraftResponse")
	gogoproto.RegisterType((*MsgUpdateParams)(nil), "lumera.credits.v1.MsgUpdateParams")
	gogoproto.RegisterType((*MsgUpdateParamsResponse)(nil), "lumera.credits.v1.MsgUpdateParamsResponse")
	RegisterLegacyAminoCodec(Amino)
	sdk.RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
