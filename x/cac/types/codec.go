
// Package types holds shared types and helpers for the CAC (Content-Addressed Cache) module.
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
	cdc.RegisterConcrete(&MsgCacheStore{}, "lumera/cac/MsgCacheStore", nil)
	cdc.RegisterConcrete(&MsgCacheInvalidate{}, "lumera/cac/MsgCacheInvalidate", nil)
	cdc.RegisterConcrete(&MsgRecordCacheHit{}, "lumera/cac/MsgRecordCacheHit", nil)
	cdc.RegisterConcrete(&MsgTickDecay{}, "lumera/cac/MsgTickDecay", nil)
	cdc.RegisterConcrete(&MsgPromoteTier{}, "lumera/cac/MsgPromoteTier", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "lumera/cac/MsgUpdateParams", nil)
}

// RegisterInterfaces registers interfaces types with the interface registry.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCacheStore{},
		&MsgCacheInvalidate{},
		&MsgRecordCacheHit{},
		&MsgTickDecay{},
		&MsgPromoteTier{},
		&MsgUpdateParams{},
	)

	registry.RegisterImplementations((*tx.MsgResponse)(nil),
		&MsgCacheStoreResponse{},
		&MsgCacheInvalidateResponse{},
		&MsgRecordCacheHitResponse{},
		&MsgTickDecayResponse{},
		&MsgPromoteTierResponse{},
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
	RegisterLegacyAminoCodec(Amino)
	sdk.RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
