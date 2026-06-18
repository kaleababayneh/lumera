//go:build cosmos

package types

import (
	"sync"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/types/tx"
	gogoproto "github.com/cosmos/gogoproto/proto"
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
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registerGogoDescriptors()

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

	msgservice.RegisterMsgServiceDesc(registry, &Msg_ServiceDesc)
}

var (
	Amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
)

var registerOnce sync.Once

func registerGogoDescriptors() {
	registerOnce.Do(func() {
		gogoproto.RegisterFile("lumera/passport/v1/passport.proto", file_lumera_passport_v1_passport_proto_rawDescGZIP())

		gogoproto.RegisterType((*MsgRegisterPassport)(nil), "lumera.passport.v1.MsgRegisterPassport")
		gogoproto.RegisterType((*MsgRegisterPassportResponse)(nil), "lumera.passport.v1.MsgRegisterPassportResponse")
		gogoproto.RegisterType((*MsgSuspendPassport)(nil), "lumera.passport.v1.MsgSuspendPassport")
		gogoproto.RegisterType((*MsgSuspendPassportResponse)(nil), "lumera.passport.v1.MsgSuspendPassportResponse")
		gogoproto.RegisterType((*MsgRevokePassport)(nil), "lumera.passport.v1.MsgRevokePassport")
		gogoproto.RegisterType((*MsgRevokePassportResponse)(nil), "lumera.passport.v1.MsgRevokePassportResponse")
		gogoproto.RegisterType((*MsgReactivatePassport)(nil), "lumera.passport.v1.MsgReactivatePassport")
		gogoproto.RegisterType((*MsgReactivatePassportResponse)(nil), "lumera.passport.v1.MsgReactivatePassportResponse")
		gogoproto.RegisterType((*MsgSlashStake)(nil), "lumera.passport.v1.MsgSlashStake")
		gogoproto.RegisterType((*MsgSlashStakeResponse)(nil), "lumera.passport.v1.MsgSlashStakeResponse")
		gogoproto.RegisterType((*MsgTopUpStake)(nil), "lumera.passport.v1.MsgTopUpStake")
		gogoproto.RegisterType((*MsgTopUpStakeResponse)(nil), "lumera.passport.v1.MsgTopUpStakeResponse")
		gogoproto.RegisterType((*MsgUnregisterPassport)(nil), "lumera.passport.v1.MsgUnregisterPassport")
		gogoproto.RegisterType((*MsgUnregisterPassportResponse)(nil), "lumera.passport.v1.MsgUnregisterPassportResponse")
	})
}

func init() {
	registerGogoDescriptors()
	RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
