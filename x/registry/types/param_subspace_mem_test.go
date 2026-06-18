
package types

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

type invalidParamSet struct{}

func (invalidParamSet) ParamSetPairs() ParamSetPairs {
	bad := "not-a-pointer"
	return ParamSetPairs{{
		Key:   []byte("bad_key"),
		Value: bad,
	}}
}

func TestInMemoryParamSubspaceWithKeyTable(t *testing.T) {
	t.Parallel()

	subspace := NewInMemoryParamSubspace()
	require.NotNil(t, subspace)
	require.False(t, subspace.HasKeyTable())

	returned := subspace.WithKeyTable(ParamKeyTable())
	require.NotNil(t, returned)
	require.True(t, subspace.HasKeyTable())
}

func TestInMemoryParamSubspaceSetGetHasClone(t *testing.T) {
	t.Parallel()

	subspace := NewInMemoryParamSubspace()
	ctx := sdk.Context{}
	key := []byte("map_key")

	original := map[string]string{"a": "1"}
	subspace.Set(ctx, key, original)
	require.True(t, subspace.Has(ctx, key))

	original["a"] = "2"

	var got map[string]string
	subspace.Get(ctx, key, &got)
	require.Equal(t, "1", got["a"], "stored value must be cloned on Set")

	got["a"] = "3"
	var gotAgain map[string]string
	subspace.Get(ctx, key, &gotAgain)
	require.Equal(t, "1", gotAgain["a"], "returned value must be cloned on Get")
}

func TestInMemoryParamSubspaceSetParamSetAndGetParamSet(t *testing.T) {
	t.Parallel()

	subspace := NewInMemoryParamSubspace()
	subspace.WithKeyTable(ParamKeyTable())
	ctx := sdk.Context{}

	params := DefaultParams()
	params.MinBondAmount = "1234"
	params.MaxActiveTools = 321

	subspace.SetParamSet(ctx, params)
	require.True(t, subspace.Has(ctx, KeyMinBondAmount))
	require.True(t, subspace.Has(ctx, KeyMaxActiveTools))

	params.MinBondAmount = "9999"
	params.MaxActiveTools = 1

	loaded := DefaultParams()
	loaded.MinBondAmount = "0"
	loaded.MaxActiveTools = 0
	subspace.GetParamSet(ctx, loaded)

	require.Equal(t, "1234", loaded.MinBondAmount)
	require.EqualValues(t, 321, loaded.MaxActiveTools)
}

func TestInMemoryParamSubspaceSetParamSetValidatorPanic(t *testing.T) {
	t.Parallel()

	subspace := NewInMemoryParamSubspace()
	subspace.WithKeyTable(ParamKeyTable())
	ctx := sdk.Context{}

	params := DefaultParams()
	params.MinReputation = 2.0 // invalid, must be <= 1

	require.Panics(t, func() {
		subspace.SetParamSet(ctx, params)
	})
}

func TestInMemoryParamSubspaceGetRequiresPointer(t *testing.T) {
	t.Parallel()

	subspace := NewInMemoryParamSubspace()
	ctx := sdk.Context{}
	key := []byte("string_key")
	subspace.Set(ctx, key, "value")

	require.Panics(t, func() {
		var notPointer string
		subspace.Get(ctx, key, notPointer)
	})
}

func TestInMemoryParamSubspaceSetParamSetRequiresPointerValues(t *testing.T) {
	t.Parallel()

	subspace := NewInMemoryParamSubspace()
	ctx := sdk.Context{}

	require.Panics(t, func() {
		subspace.SetParamSet(ctx, invalidParamSet{})
	})
}
