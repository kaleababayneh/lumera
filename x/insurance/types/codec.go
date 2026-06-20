
// Package types registers codec utilities for the insurance module.
package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the necessary x/insurance interfaces and concrete types
// on the provided LegacyAmino codec. These types are used for Amino JSON serialization.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgProcessContribution{}, "insurance/ProcessContribution", nil)
	cdc.RegisterConcrete(&MsgFileClaim{}, "insurance/FileClaim", nil)
	cdc.RegisterConcrete(&MsgProcessClaim{}, "insurance/ProcessClaim", nil)
	cdc.RegisterConcrete(&MsgProcessPayout{}, "insurance/ProcessPayout", nil)
	cdc.RegisterConcrete(&MsgUpdatePublisherRisk{}, "insurance/UpdatePublisherRisk", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "insurance/UpdateParams", nil)
}

// RegisterInterfaces registers the x/insurance interfaces types with the interface registry
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgProcessContribution{},
		&MsgFileClaim{},
		&MsgProcessClaim{},
		&MsgProcessPayout{},
		&MsgUpdatePublisherRisk{},
		&MsgUpdateParams{},
	)

	// The gogoproto-generated code self-registers its file + message descriptors
	// in its own init(); only the msg-service descriptor wiring remains here.
	msgservice.RegisterMsgServiceDesc(registry, &Msg_serviceDesc)
}

// ModuleCdc encodes and decodes insurance module messages using protobuf.
var ModuleCdc = codec.NewProtoCodec(cdctypes.NewInterfaceRegistry())
