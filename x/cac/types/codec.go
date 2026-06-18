//go:build cosmos

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
	gogoproto "github.com/cosmos/gogoproto/proto"
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

	msgservice.RegisterMsgServiceDesc(registry, &Msg_ServiceDesc)
}

var (
	// Amino is the module-wide Amino codec.
	Amino = codec.NewLegacyAmino()
	// ModuleCdc references the global module codec.
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
)

func init() {
	gogoproto.RegisterFile("lumera/cac/v1/tx.proto", file_lumera_cac_v1_tx_proto_rawDescGZIP())
	gogoproto.RegisterType((*MsgCacheStore)(nil), "lumera.cac.v1.MsgCacheStore")
	gogoproto.RegisterType((*MsgCacheStoreResponse)(nil), "lumera.cac.v1.MsgCacheStoreResponse")
	gogoproto.RegisterType((*MsgCacheInvalidate)(nil), "lumera.cac.v1.MsgCacheInvalidate")
	gogoproto.RegisterType((*MsgCacheInvalidateResponse)(nil), "lumera.cac.v1.MsgCacheInvalidateResponse")
	gogoproto.RegisterType((*MsgRecordCacheHit)(nil), "lumera.cac.v1.MsgRecordCacheHit")
	gogoproto.RegisterType((*MsgRecordCacheHitResponse)(nil), "lumera.cac.v1.MsgRecordCacheHitResponse")
	gogoproto.RegisterType((*MsgTickDecay)(nil), "lumera.cac.v1.MsgTickDecay")
	gogoproto.RegisterType((*MsgTickDecayResponse)(nil), "lumera.cac.v1.MsgTickDecayResponse")
	gogoproto.RegisterType((*MsgPromoteTier)(nil), "lumera.cac.v1.MsgPromoteTier")
	gogoproto.RegisterType((*MsgPromoteTierResponse)(nil), "lumera.cac.v1.MsgPromoteTierResponse")
	gogoproto.RegisterType((*MsgUpdateParams)(nil), "lumera.cac.v1.MsgUpdateParams")
	gogoproto.RegisterType((*MsgUpdateParamsResponse)(nil), "lumera.cac.v1.MsgUpdateParamsResponse")
	RegisterLegacyAminoCodec(Amino)
	sdk.RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
