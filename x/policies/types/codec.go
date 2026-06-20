
package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the necessary types and interfaces for the
// policies module with the provided LegacyAmino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgCreatePolicy{}, "policies/MsgCreatePolicy", nil)
	cdc.RegisterConcrete(&MsgUpdatePolicy{}, "policies/MsgUpdatePolicy", nil)
	cdc.RegisterConcrete(&MsgActivatePolicy{}, "policies/MsgActivatePolicy", nil)
	cdc.RegisterConcrete(&MsgDeprecatePolicy{}, "policies/MsgDeprecatePolicy", nil)
	cdc.RegisterConcrete(&MsgArchivePolicy{}, "policies/MsgArchivePolicy", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "policies/MsgUpdateParams", nil)
}

// RegisterInterfaces registers the necessary interface types and their implementations.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgCreatePolicy{},
		&MsgUpdatePolicy{},
		&MsgActivatePolicy{},
		&MsgDeprecatePolicy{},
		&MsgArchivePolicy{},
		&MsgUpdateParams{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &Msg_serviceDesc)
}

var (
	Amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
)

func init() {
	// The gogoproto-generated code self-registers its file + message descriptors
	// in its own init(); only amino registration remains hand-wired here.
	RegisterLegacyAminoCodec(Amino)
	sdk.RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
