
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
	cdc.RegisterConcrete(&MsgInjectOracleVotes{}, "lumera/oracle/MsgInjectOracleVotes", nil)
}

// RegisterInterfaces registers interfaces types with the interface registry.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgInjectOracleVotes{},
	)

	registry.RegisterImplementations((*tx.MsgResponse)(nil),
		&MsgInjectOracleVotesResponse{},
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
