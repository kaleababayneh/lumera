//go:build cosmos

package types

import (
	"sync"

	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/types/tx"
	gogoproto "github.com/cosmos/gogoproto/proto"
)

var (
	// Amino defines the legacy amino codec for the NFT module.
	Amino = codec.NewLegacyAmino()
	// ModuleCdc encodes module messages using the protobuf interface registry.
	ModuleCdc = codec.NewProtoCodec(cdctypes.NewInterfaceRegistry())
)

// RegisterLegacyAminoCodec registers the NFT module's types on the LegacyAmino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgMintToolpack{}, "lumera/nft/MsgMintToolpack", nil)
	cdc.RegisterConcrete(&MsgUpdateToolpack{}, "lumera/nft/MsgUpdateToolpack", nil)
	cdc.RegisterConcrete(&MsgDeactivateToolpack{}, "lumera/nft/MsgDeactivateToolpack", nil)
	cdc.RegisterConcrete(&MsgRecordRoyaltyPayout{}, "lumera/nft/MsgRecordRoyaltyPayout", nil)
}

// RegisterInterfaces registers the NFT module message interfaces with the global registry.
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registerGogoDescriptors()

	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgMintToolpack{},
		&MsgUpdateToolpack{},
		&MsgDeactivateToolpack{},
		&MsgRecordRoyaltyPayout{},
	)

	registry.RegisterImplementations((*tx.MsgResponse)(nil),
		&MsgMintToolpackResponse{},
		&MsgUpdateToolpackResponse{},
		&MsgDeactivateToolpackResponse{},
		&MsgRecordRoyaltyPayoutResponse{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &Msg_ServiceDesc)
}

var registerOnce sync.Once

func registerGogoDescriptors() {
	registerOnce.Do(func() {
		gogoproto.RegisterFile("lumera/nft/v1/tx.proto", file_lumera_nft_v1_tx_proto_rawDescGZIP())
		gogoproto.RegisterFile("lumera/nft/v1/toolpack.proto", file_lumera_nft_v1_toolpack_proto_rawDescGZIP())

		gogoproto.RegisterType((*MsgMintToolpack)(nil), "lumera.nft.v1.MsgMintToolpack")
		gogoproto.RegisterType((*MsgMintToolpackResponse)(nil), "lumera.nft.v1.MsgMintToolpackResponse")
		gogoproto.RegisterType((*MsgUpdateToolpack)(nil), "lumera.nft.v1.MsgUpdateToolpack")
		gogoproto.RegisterType((*MsgUpdateToolpackResponse)(nil), "lumera.nft.v1.MsgUpdateToolpackResponse")
		gogoproto.RegisterType((*MsgDeactivateToolpack)(nil), "lumera.nft.v1.MsgDeactivateToolpack")
		gogoproto.RegisterType((*MsgDeactivateToolpackResponse)(nil), "lumera.nft.v1.MsgDeactivateToolpackResponse")
		gogoproto.RegisterType((*MsgRecordRoyaltyPayout)(nil), "lumera.nft.v1.MsgRecordRoyaltyPayout")
		gogoproto.RegisterType((*MsgRecordRoyaltyPayoutResponse)(nil), "lumera.nft.v1.MsgRecordRoyaltyPayoutResponse")
	})
}

func init() {
	registerGogoDescriptors()
	RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
