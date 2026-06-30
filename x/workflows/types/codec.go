package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
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

// RegisterInterfaces is intentionally empty until tx/query protobuf services
// land with the storage bead. The scaffold still registers Amino types so
// genesis and keeper tests have stable type names.
func RegisterInterfaces(_ codectypes.InterfaceRegistry) {}

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
