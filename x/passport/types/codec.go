package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/types/tx"
)

// RegisterLegacyAminoCodec registers the passport module's types on the LegacyAmino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	legacy.RegisterAminoMsg(cdc, &MsgRegisterPassport{}, "lumera/passport/MsgRegisterPassport")
	legacy.RegisterAminoMsg(cdc, &MsgSuspendPassport{}, "lumera/passport/MsgSuspendPassport")
	legacy.RegisterAminoMsg(cdc, &MsgRevokePassport{}, "lumera/passport/MsgRevokePassport")
	legacy.RegisterAminoMsg(cdc, &MsgReactivatePassport{}, "lumera/passport/MsgReactivatePassport")
	legacy.RegisterAminoMsg(cdc, &MsgSlashStake{}, "lumera/passport/MsgSlashStake")
	legacy.RegisterAminoMsg(cdc, &MsgTopUpStake{}, "lumera/passport/MsgTopUpStake")
	legacy.RegisterAminoMsg(cdc, &MsgUnregisterPassport{}, "lumera/passport/MsgUnregisterPassport")
}

// RegisterInterfaces registers interface types.
//
// The gogoproto-generated code self-registers its file and message
// descriptors in its own init(), so no manual proto registration is needed
// here.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgRegisterPassport{},
		&MsgSuspendPassport{},
		&MsgRevokePassport{},
		&MsgReactivatePassport{},
		&MsgSlashStake{},
		&MsgTopUpStake{},
		&MsgUnregisterPassport{},
	)

	registry.RegisterImplementations((*tx.MsgResponse)(nil),
		&MsgRegisterPassportResponse{},
		&MsgSuspendPassportResponse{},
		&MsgRevokePassportResponse{},
		&MsgReactivatePassportResponse{},
		&MsgSlashStakeResponse{},
		&MsgTopUpStakeResponse{},
		&MsgUnregisterPassportResponse{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &Msg_serviceDesc)
}

var (
	Amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
)

func init() {
	RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
