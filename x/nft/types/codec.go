
package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/types/tx"
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
// The gogoproto-generated code self-registers its file and message descriptors
// in its own init(), so no manual proto registration is needed here.
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
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

	msgservice.RegisterMsgServiceDesc(registry, &Msg_serviceDesc)
}

func init() {
	RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
