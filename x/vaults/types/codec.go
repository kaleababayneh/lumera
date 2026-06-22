// Package types provides codec registration utilities for the vaults module.
package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/types/tx"
)

var (
	// Amino exposes the module's legacy amino codec.
	Amino = codec.NewLegacyAmino()
	// ModuleCdc encodes module messages and state using protobuf.
	ModuleCdc = codec.NewProtoCodec(cdctypes.NewInterfaceRegistry())
)

// RegisterLegacyAminoCodec registers the vaults module's types on the LegacyAmino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgCreateVault{}, "vaults/CreateVault", nil)
}

// RegisterInterfaces wires vault messages into the global interface registry.
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgCreateVault{},
	)
	registry.RegisterImplementations((*tx.MsgResponse)(nil),
		&MsgCreateVaultResponse{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &Msg_serviceDesc)
}

func init() {
	RegisterLegacyAminoCodec(Amino)
	sdk.RegisterLegacyAminoCodec(Amino)
	Amino.Seal()
}
