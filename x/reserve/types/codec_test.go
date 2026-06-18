//go:build cosmos

package types

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
)

func TestRegisterLegacyAminoCodec_NoPanic(t *testing.T) {
	t.Parallel()
	amino := codec.NewLegacyAmino()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RegisterLegacyAminoCodec panicked: %v", r)
		}
	}()

	RegisterLegacyAminoCodec(amino)
}

func TestRegisterInterfaces_NoPanic(t *testing.T) {
	t.Parallel()
	registry := codectypes.NewInterfaceRegistry()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RegisterInterfaces panicked: %v", r)
		}
	}()

	RegisterInterfaces(registry)
}
