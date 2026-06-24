package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the payment_rails module's types on the amino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgCreateDeposit{}, "payment_rails/CreateDeposit", nil)
	cdc.RegisterConcrete(&MsgRequestWithdraw{}, "payment_rails/RequestWithdraw", nil)
	cdc.RegisterConcrete(&MsgFinalizeWithdraw{}, "payment_rails/FinalizeWithdraw", nil)
	cdc.RegisterConcrete(&MsgRefundDeposit{}, "payment_rails/RefundDeposit", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "payment_rails/UpdateParams", nil)
}

// RegisterInterfaces registers the module's interface types.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgCreateDeposit{},
		&MsgRequestWithdraw{},
		&MsgFinalizeWithdraw{},
		&MsgRefundDeposit{},
		&MsgUpdateParams{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

var (
	Amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
)

func init() {
	// gogoproto type/file descriptors are registered by the generated .pb.go
	// init(); only the amino codec needs explicit setup here.
	RegisterLegacyAminoCodec(Amino)
	sdk.RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
