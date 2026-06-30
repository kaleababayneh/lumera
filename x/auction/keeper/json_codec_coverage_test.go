package keeper

import (
	"testing"

	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log"
	"cosmossdk.io/store/metrics"
	"cosmossdk.io/store/rootmulti"
	storetypes "cosmossdk.io/store/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/auction/types"
	"time"
)

// jsonValueCodec's EncodeJSON / DecodeJSON methods are part of the
// cosmossdk.io/collections ValueCodec interface. They are invoked by the
// framework during genesis export/import. Prior coverage never
// exercised them because test harnesses use Set/Get (which use
// Encode/Decode), not the JSON-variant accessors. A silent regression
// that returned "" from EncodeJSON or panicked on DecodeJSON would
// surface only on chain upgrade or genesis migration — too late.

// TestJSONValueCodec_EncodeJSONRoundtrip pins that EncodeJSON produces
// bytes that Decode can consume (i.e. the JSON-variant produces the same
// wire format as the binary variant — json for this codec).
func TestJSONValueCodec_EncodeJSONRoundtrip(t *testing.T) {
	codec := newJSONCodec[types.Params]()
	original := types.DefaultParams()

	bz, err := codec.EncodeJSON(original)
	require.NoError(t, err, "EncodeJSON must succeed on a well-formed Params")
	require.NotEmpty(t, bz, "EncodeJSON must produce non-empty bytes for non-zero Params")

	decoded, err := codec.Decode(bz)
	require.NoError(t, err, "EncodeJSON output must be Decode-compatible")
	require.Equal(t, original.MaxActiveAuctions, decoded.MaxActiveAuctions,
		"roundtripped MaxActiveAuctions must match — pins JSON field-tag stability")
}

// TestJSONValueCodec_DecodeJSONDelegatesToDecode pins the current
// invariant that DecodeJSON is a pure alias for Decode. If the two
// diverge (e.g. DecodeJSON grows a wrapper header), genesis import would
// produce different values than runtime Get, creating consensus hazards.
func TestJSONValueCodec_DecodeJSONDelegatesToDecode(t *testing.T) {
	codec := newJSONCodec[types.Params]()
	original := types.DefaultParams()

	bz, err := codec.EncodeJSON(original)
	require.NoError(t, err)

	fromJSON, errJSON := codec.DecodeJSON(bz)
	fromBinary, errBin := codec.Decode(bz)
	require.NoError(t, errJSON)
	require.NoError(t, errBin)
	require.Equal(t, fromBinary, fromJSON,
		"DecodeJSON must match Decode byte-for-byte — divergence would split consensus on genesis replay")
}

// TestJSONValueCodec_DecodeJSON_EmptyBytesYieldsZeroValue pins that
// DecodeJSON of empty input returns the type's zero value without an
// error, matching Decode semantics. collections.Map.Get on an unset key
// never returns empty bytes (it returns ErrNotFound), but the codec
// contract requires the empty-input branch to be stable for forward
// compatibility.
func TestJSONValueCodec_DecodeJSON_EmptyBytesYieldsZeroValue(t *testing.T) {
	codec := newJSONCodec[types.Params]()

	got, err := codec.DecodeJSON(nil)
	require.NoError(t, err, "DecodeJSON(nil) must not error — contract with collections framework")
	require.Equal(t, types.Params{}, got,
		"empty input must decode to zero-value Params (not an arbitrary default)")

	got2, err := codec.DecodeJSON([]byte{})
	require.NoError(t, err)
	require.Equal(t, types.Params{}, got2,
		"[]byte{} must decode identically to nil — both represent absent-value")
}

// GetParams's NotFound-fallback branch was previously unreached because
// setupAuctionKeeper always calls SetParams. The fallback is load-bearing
// at chain init (before genesis is applied). Pinning it closes a silent
// regression mode where a removed guard would make GetParams return
// (nil, ErrNotFound) to a caller expecting defaults.

// TestGetParams_ReturnsDefaultsWhenUnset builds a fresh keeper without
// SetParams and asserts GetParams yields DefaultParams with nil error.
func TestGetParams_ReturnsDefaultsWhenUnset(t *testing.T) {
	// Minimal keeper setup without SetParams — to hit the NotFound branch.
	auctionKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	logger := log.NewNopLogger()
	cms := rootmulti.NewStore(db, logger, metrics.NewNoOpMetrics())
	cms.MountStoreWithDB(auctionKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, cms.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	keeper := NewKeeper(
		cdc,
		runtime.NewKVStoreService(auctionKey),
		authtypes.NewModuleAddress("gov").String(),
		logger,
	)

	header := tmproto.Header{Height: 1, Time: time.Unix(1_700_000_000, 0).UTC()}
	ctx := sdk.NewContext(cms, header, false, logger)

	got, err := keeper.GetParams(ctx)
	require.NoError(t, err, "GetParams must not error when no params stored")
	require.NotNil(t, got, "GetParams must never return nil")
	want := types.DefaultParams()
	require.Equal(t, want.MaxActiveAuctions, got.MaxActiveAuctions,
		"unset params must fall back to DefaultParams fields, not zero-value")
}
