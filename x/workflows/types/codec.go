package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers scaffold message types on the provided codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgPublishWorkflow{}, "lumera/workflows/MsgPublishWorkflow", nil)
	cdc.RegisterConcrete(&MsgUpgradeWorkflow{}, "lumera/workflows/MsgUpgradeWorkflow", nil)
	cdc.RegisterConcrete(&MsgDeactivateWorkflow{}, "lumera/workflows/MsgDeactivateWorkflow", nil)
	cdc.RegisterConcrete(&MsgTopUpAuthorBond{}, "lumera/workflows/MsgTopUpAuthorBond", nil)
	cdc.RegisterConcrete(&MsgWithdrawBond{}, "lumera/workflows/MsgWithdrawBond", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "lumera/workflows/MsgUpdateParams", nil)
}

// RegisterInterfaces registers the author-facing workflow messages and the Msg
// service. The gogoproto type/file descriptors are registered by the generated
// .pb.go init(). (MsgUpdateParams stays a dormant hand-written type — params are
// genesis-configured, with no live tx service.)
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgPublishWorkflow{},
		&MsgUpgradeWorkflow{},
		&MsgDeactivateWorkflow{},
		&MsgTopUpAuthorBond{},
		&MsgWithdrawBond{},
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
	RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
