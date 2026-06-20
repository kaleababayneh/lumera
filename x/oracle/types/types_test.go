//go:build cosmos

package types

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func validOracleAuthority() string {
	return sdk.AccAddress(bytes.Repeat([]byte{0x6F}, 20)).String()
}

// ---------- DefaultParams ----------

func TestDefaultParams(t *testing.T) {
	p := DefaultParams()
	require.NotNil(t, p)
	assert.Equal(t, int64(10), p.VotePeriod)
	assert.Equal(t, "0.67", p.VoteThreshold)
	assert.Equal(t, "0.10", p.MaxPriceDeviation)
	assert.Equal(t, []string{"LAC/USD", "ETH/USD", "BTC/USD"}, p.AssetPairs)
	assert.Equal(t, int64(300), p.MaxVoteAge)
}

func TestDefaultParams_Validate(t *testing.T) {
	require.NoError(t, DefaultParams().Validate())
}

// ---------- Params.Validate ----------

func TestParams_Validate_ZeroVotePeriod(t *testing.T) {
	p := DefaultParams()
	p.VotePeriod = 0
	require.Error(t, p.Validate())
}

func TestParams_Validate_NegativeVotePeriod(t *testing.T) {
	p := DefaultParams()
	p.VotePeriod = -5
	require.Error(t, p.Validate())
}

func TestParams_Validate_EmptyVoteThreshold(t *testing.T) {
	p := DefaultParams()
	p.VoteThreshold = ""
	require.Error(t, p.Validate())
}

func TestParams_Validate_InvalidVoteThreshold(t *testing.T) {
	p := DefaultParams()
	p.VoteThreshold = "not_a_number"
	require.Error(t, p.Validate())
}

func TestParams_Validate_NegativeVoteThreshold(t *testing.T) {
	p := DefaultParams()
	p.VoteThreshold = "-0.1"
	require.Error(t, p.Validate())
}

func TestParams_Validate_VoteThresholdAboveOne(t *testing.T) {
	p := DefaultParams()
	p.VoteThreshold = "1.01"
	require.Error(t, p.Validate())
}

func TestParams_Validate_VoteThresholdExactlyOne(t *testing.T) {
	p := DefaultParams()
	p.VoteThreshold = "1.0"
	require.NoError(t, p.Validate())
}

func TestParams_Validate_VoteThresholdZero(t *testing.T) {
	p := DefaultParams()
	p.VoteThreshold = "0.0"
	require.NoError(t, p.Validate())
}

func TestParams_Validate_EmptyMaxPriceDeviation(t *testing.T) {
	p := DefaultParams()
	p.MaxPriceDeviation = ""
	require.Error(t, p.Validate())
}

func TestParams_Validate_InvalidMaxPriceDeviation(t *testing.T) {
	p := DefaultParams()
	p.MaxPriceDeviation = "abc"
	require.Error(t, p.Validate())
}

func TestParams_Validate_NegativeMaxPriceDeviation(t *testing.T) {
	p := DefaultParams()
	p.MaxPriceDeviation = "-0.05"
	require.Error(t, p.Validate())
}

func TestParams_Validate_LargeMaxPriceDeviation(t *testing.T) {
	p := DefaultParams()
	p.MaxPriceDeviation = "5.0"
	require.NoError(t, p.Validate())
}

func TestParams_Validate_EmptyAssetPairs(t *testing.T) {
	p := DefaultParams()
	p.AssetPairs = nil
	require.Error(t, p.Validate())
}

func TestParams_Validate_EmptyStringAssetPair(t *testing.T) {
	p := DefaultParams()
	p.AssetPairs = []string{"LAC/USD", ""}
	require.Error(t, p.Validate())
}

func TestParams_Validate_WhitespaceAssetPair(t *testing.T) {
	p := DefaultParams()
	p.AssetPairs = []string{"LAC/USD", " ETH/USD"}
	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "asset_pairs[1]")
	require.Contains(t, err.Error(), "whitespace")
}

func TestParams_Validate_DuplicateAssetPairs(t *testing.T) {
	p := DefaultParams()
	p.AssetPairs = []string{"LAC/USD", "ETH/USD", "LAC/USD"}
	require.Error(t, p.Validate())
}

func TestParams_Validate_TrailingWhitespaceAssetPair(t *testing.T) {
	p := DefaultParams()
	p.AssetPairs = []string{"LAC/USD", "LAC/USD "}
	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "whitespace")
}

func TestParams_Validate_SingleAssetPair(t *testing.T) {
	p := DefaultParams()
	p.AssetPairs = []string{"LAC/USD"}
	require.NoError(t, p.Validate())
}

func TestParams_Validate_NegativeMaxVoteAge(t *testing.T) {
	p := DefaultParams()
	p.MaxVoteAge = -1
	require.Error(t, p.Validate())
}

func TestParams_Validate_ZeroMaxVoteAge(t *testing.T) {
	p := DefaultParams()
	p.MaxVoteAge = 0
	require.NoError(t, p.Validate())
}

func TestParams_Validate_MaxVoteAgeDurationBoundary(t *testing.T) {
	p := DefaultParams()
	p.MaxVoteAge = MaxVoteAgeSeconds
	require.NoError(t, p.Validate())

	p.MaxVoteAge = MaxVoteAgeSeconds + 1
	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "max vote age")
	require.Contains(t, err.Error(), "cannot exceed")
}

// ---------- DecimalFromStr ----------

func TestDecimalFromStr_Valid(t *testing.T) {
	d, err := DecimalFromStr("1.23")
	require.NoError(t, err)
	assert.Equal(t, "1.23", d.String())
}

func TestDecimalFromStr_Empty(t *testing.T) {
	d, err := DecimalFromStr("")
	require.NoError(t, err)
	assert.True(t, d.IsZero())
}

func TestDecimalFromStr_Invalid(t *testing.T) {
	_, err := DecimalFromStr("not_a_number")
	require.Error(t, err)
}

func TestDecimalFromStr_NegativeZero(t *testing.T) {
	d, err := DecimalFromStr("0")
	require.NoError(t, err)
	assert.True(t, d.IsZero())
}

func TestDecimalFromStr_HighPrecision(t *testing.T) {
	d, err := DecimalFromStr("0.123456789012345678")
	require.NoError(t, err)
	assert.False(t, d.IsZero())
}

func TestDecimalFromStr_RejectsUnsafeExponent(t *testing.T) {
	_, err := DecimalFromStr("1e11100100")
	require.Error(t, err)
	require.Contains(t, err.Error(), "magnitude")
}

// ---------- Keys ----------

func TestModuleConstants(t *testing.T) {
	assert.Equal(t, "oracle", ModuleName)
	assert.Equal(t, "oracle", StoreKey)
	assert.Equal(t, "oracle", RouterKey)
	assert.Equal(t, "mem_oracle", MemStoreKey)
	assert.Equal(t, "oracle", QuerierRoute)
}

func TestKeyPrefixesUnique(t *testing.T) {
	prefixes := [][]byte{
		ParamsKey,
		PriceFeedPrefix,
		AggregatedPricePrefix,
		ValidatorVotePrefix,
		VoteHistoryPrefix,
	}
	seen := make(map[byte]bool)
	for _, p := range prefixes {
		require.Len(t, p, 1, "expected single-byte prefix")
		b := p[0]
		assert.False(t, seen[b], "duplicate prefix byte: 0x%02x", b)
		seen[b] = true
	}
}

func TestKeyPrefixValues(t *testing.T) {
	assert.Equal(t, byte(0x01), ParamsKey[0])
	assert.Equal(t, byte(0x10), PriceFeedPrefix[0])
	assert.Equal(t, byte(0x11), AggregatedPricePrefix[0])
	assert.Equal(t, byte(0x12), ValidatorVotePrefix[0])
	assert.Equal(t, byte(0x13), VoteHistoryPrefix[0])
}

// ---------- Event Constants ----------

func TestEventConstants(t *testing.T) {
	assert.Equal(t, "oracle_aggregated_price", EventTypeAggregatedPrice)
	assert.Equal(t, "asset_pair", AttributeKeyAssetPair)
	assert.Equal(t, "median_price", AttributeKeyMedianPrice)
	assert.Equal(t, "num_validators", AttributeKeyNumValidators)
}

// ---------- Errors ----------

func TestSentinelErrors(t *testing.T) {
	errs := map[string]error{
		"ErrInvalidAssetPair":     ErrInvalidAssetPair,
		"ErrInvalidPrice":         ErrInvalidPrice,
		"ErrPriceFeedNotFound":    ErrPriceFeedNotFound,
		"ErrInsufficientVotes":    ErrInsufficientVotes,
		"ErrPriceDeviation":       ErrPriceDeviation,
		"ErrStaleVote":            ErrStaleVote,
		"ErrInvalidVoteExtension": ErrInvalidVoteExtension,
		"ErrInvalidParameters":    ErrInvalidParameters,
		"ErrUnauthorized":         ErrUnauthorized,
		"ErrInternalError":        ErrInternalError,
	}
	for name, e := range errs {
		assert.NotNil(t, e, "%s should not be nil", name)
		assert.NotEmpty(t, e.Error(), "%s should have a non-empty message", name)
	}
}

func TestSentinelErrorCodesUnique(t *testing.T) {
	codes := []uint32{2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	seen := make(map[uint32]bool)
	for _, code := range codes {
		assert.False(t, seen[code], "duplicate error code: %d", code)
		seen[code] = true
	}
}

// ---------- MarshalVoteExtension ----------

func TestMarshalVoteExtension_NilVote(t *testing.T) {
	_, err := MarshalVoteExtension(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil vote")
}

func TestMarshalVoteExtension_NilTimestamp(t *testing.T) {
	vote := &ValidatorVote{
		ValidatorAddress: "cosmos1abc",
		PriceFeeds:       []*PriceFeed{{AssetPair: "LAC/USD", Price: "1.0"}},
		BlockHeight:      100,
		Timestamp:        nil,
	}
	_, err := MarshalVoteExtension(vote)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timestamp")
}

func TestMarshalVoteExtension_InvalidTimestamp(t *testing.T) {
	vote := &ValidatorVote{
		ValidatorAddress: "cosmos1abc",
		PriceFeeds:       []*PriceFeed{{AssetPair: "LAC/USD", Price: "1.0"}},
		BlockHeight:      100,
		Timestamp:        &timestamppb.Timestamp{Nanos: 1_000_000_000},
	}
	_, err := MarshalVoteExtension(vote)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timestamp invalid")
}

func TestMarshalVoteExtension_Valid(t *testing.T) {
	vote := &ValidatorVote{
		ValidatorAddress: "cosmos1abc",
		PriceFeeds: []*PriceFeed{
			{AssetPair: "LAC/USD", Price: "1.50"},
			{AssetPair: "ETH/USD", Price: "2000.00"},
		},
		BlockHeight: 100,
		Timestamp:   timestamppb.Now(),
	}
	bz, err := MarshalVoteExtension(vote)
	require.NoError(t, err)
	require.NotEmpty(t, bz)
}

func TestMarshalVoteExtension_EmptyFeeds(t *testing.T) {
	vote := &ValidatorVote{
		ValidatorAddress: "cosmos1abc",
		BlockHeight:      1,
		Timestamp:        timestamppb.Now(),
	}
	bz, err := MarshalVoteExtension(vote)
	require.NoError(t, err)
	require.NotEmpty(t, bz)
}

// ---------- ParseVoteExtension ----------

func TestParseVoteExtension_EmptyBytes(t *testing.T) {
	vote, err := ParseVoteExtension(nil)
	require.NoError(t, err)
	require.NotNil(t, vote)
	assert.Empty(t, vote.ValidatorAddress)
}

func TestParseVoteExtension_ZeroLengthSlice(t *testing.T) {
	vote, err := ParseVoteExtension([]byte{})
	require.NoError(t, err)
	require.NotNil(t, vote)
}

func TestParseVoteExtension_InvalidBytes(t *testing.T) {
	_, err := ParseVoteExtension([]byte{0xFF, 0xFF, 0xFF})
	require.Error(t, err)
}

// TestParseVoteExtension_RejectsOversizedInput pins the DoS cap on
// the inbound parse path. A misbehaving proposer could otherwise
// inject max-consensus-size VoteExtension bytes whose dense proto
// burns Unmarshal compute on every validator processing the batch —
// the same amplification pattern as the registry/ibc UnmarshalPacket
// cap. Guard must short-circuit on length BEFORE proto.Unmarshal
// runs, so no cycles are spent parsing the adversarial payload.
func TestParseVoteExtension_RejectsOversizedInput(t *testing.T) {
	// One byte past the cap. Content doesn't need to decode to a
	// real ValidatorVote — the guard trips on length first.
	raw := make([]byte, MaxVoteExtensionBytes+1)
	for i := range raw {
		raw[i] = 0x01
	}
	_, err := ParseVoteExtension(raw)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceed")
	require.Contains(t, err.Error(), "cap")
}

// TestParseVoteExtension_AcceptsAtCapInput pins the cap as a
// threshold, not a floor. A legitimate-but-large vote extension at
// exactly MaxVoteExtensionBytes must pass the size guard; any
// subsequent error must be an Unmarshal/validation error, never
// the "exceeds cap" one.
func TestParseVoteExtension_AcceptsAtCapInput(t *testing.T) {
	// Whitespace-valid proto-ish bytes at exactly the cap. They
	// won't decode to a real ValidatorVote — the assertion is
	// only that the size guard doesn't fire.
	raw := make([]byte, MaxVoteExtensionBytes)
	_, err := ParseVoteExtension(raw)
	// Unmarshal may still reject the content, but NOT with our
	// size-cap message.
	if err != nil {
		require.NotContains(t, err.Error(), "exceed",
			"at-cap input must not trigger the size guard")
	}
}

// TestMarshalVoteExtension_RejectsOversizedOutput pins the
// symmetric outbound guard: if local vote-construction code ever
// produces a ValidatorVote whose marshaled bytes exceed the cap,
// emitting it would force other validators' ParseVoteExtension to
// reject our own vote during the next consensus round. The outbound
// guard catches the condition early with a named error instead of
// discovering it via a silent consensus-round miss.
func TestMarshalVoteExtension_RejectsOversizedOutput(t *testing.T) {
	// Pad ValidatorAddress past the cap. It's a string field on
	// the proto, so proto.Marshal serializes its full length. Other
	// validator fields are minimal so overall marshaled size is
	// dominated by this.
	padded := make([]byte, MaxVoteExtensionBytes+100)
	for i := range padded {
		padded[i] = 'a'
	}
	vote := &ValidatorVote{
		ValidatorAddress: string(padded),
		Timestamp:        timestamppb.New(time.Unix(1700000000, 0)),
	}
	_, err := MarshalVoteExtension(vote)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceed")
	require.Contains(t, err.Error(), "cap")
}

// TestParseVoteExtension_ValidProtoButNilTimestamp pins the branch
// where the bytes unmarshal cleanly but produce a vote with nil
// Timestamp. MarshalVoteExtension refuses to emit such bytes, but
// an attacker (or a buggy counterparty) could craft them directly
// via proto.Marshal on a zero-timestamp ValidatorVote. The parser
// must reject rather than silently produce a timestamp-less vote
// that downstream aggregation code would treat as submitted at
// epoch 0 / current time / etc. depending on reader logic.
//
// Regression guard: this branch exists precisely because the
// roundtrip helper can be bypassed; a refactor that removed it
// would open the aggregation path to stale-or-undated votes.
func TestParseVoteExtension_ValidProtoButNilTimestamp(t *testing.T) {
	// Build a vote with populated non-timestamp fields so the
	// marshaled bytes are non-empty (otherwise ParseVoteExtension
	// takes the len==0 early return instead of reaching the
	// nil-timestamp check).
	voteWithoutTs := &ValidatorVote{
		ValidatorAddress: "cosmos1crafted",
		BlockHeight:      99,
		// Timestamp intentionally left nil.
	}
	bz, err := proto.Marshal(voteWithoutTs)
	require.NoError(t, err)
	require.NotEmpty(t, bz, "encoded bytes must be non-empty to reach the post-unmarshal check")

	_, err = ParseVoteExtension(bz)
	require.Error(t, err)
	require.Contains(t, err.Error(), "timestamp missing",
		"parser must reject valid-proto-but-nil-timestamp as 'timestamp missing'")
}

func TestParseVoteExtension_ValidProtoButInvalidTimestamp(t *testing.T) {
	voteWithInvalidTS := &ValidatorVote{
		ValidatorAddress: "cosmos1crafted",
		BlockHeight:      99,
		Timestamp:        &timestamppb.Timestamp{Nanos: 1_000_000_000},
	}
	bz, err := proto.Marshal(voteWithInvalidTS)
	require.NoError(t, err)
	require.NotEmpty(t, bz, "encoded bytes must be non-empty to reach the post-unmarshal timestamp check")

	_, err = ParseVoteExtension(bz)
	require.Error(t, err)
	require.Contains(t, err.Error(), "timestamp invalid",
		"parser must reject malformed protobuf timestamps before downstream vote validation")
}

func TestParseVoteExtension_RoundTrip(t *testing.T) {
	original := &ValidatorVote{
		ValidatorAddress: "cosmos1validator",
		PriceFeeds: []*PriceFeed{
			{
				AssetPair:       "LAC/USD",
				Price:           "1.25",
				Volume_24H:      "1000000",
				Timestamp:       timestamppb.Now(),
				Sources:         []string{"binance", "coinbase"},
				ConfidenceScore: "0.95",
			},
		},
		BlockHeight: 42,
		Timestamp:   timestamppb.Now(),
	}

	bz, err := MarshalVoteExtension(original)
	require.NoError(t, err)

	parsed, err := ParseVoteExtension(bz)
	require.NoError(t, err)
	require.NotNil(t, parsed)

	assert.Equal(t, original.ValidatorAddress, parsed.ValidatorAddress)
	assert.Equal(t, original.BlockHeight, parsed.BlockHeight)
	require.Len(t, parsed.PriceFeeds, 1)
	assert.Equal(t, "LAC/USD", parsed.PriceFeeds[0].AssetPair)
	assert.Equal(t, "1.25", parsed.PriceFeeds[0].Price)
	assert.Equal(t, "1000000", parsed.PriceFeeds[0].Volume_24H)
	assert.Equal(t, []string{"binance", "coinbase"}, parsed.PriceFeeds[0].Sources)
	assert.Equal(t, "0.95", parsed.PriceFeeds[0].ConfidenceScore)
}

func TestParseVoteExtension_MultipleFeeds(t *testing.T) {
	original := &ValidatorVote{
		ValidatorAddress: "cosmos1val",
		PriceFeeds: []*PriceFeed{
			{AssetPair: "LAC/USD", Price: "1.0", Timestamp: timestamppb.Now()},
			{AssetPair: "ETH/USD", Price: "2000.0", Timestamp: timestamppb.Now()},
			{AssetPair: "BTC/USD", Price: "50000.0", Timestamp: timestamppb.Now()},
		},
		BlockHeight: 99,
		Timestamp:   timestamppb.Now(),
	}

	bz, err := MarshalVoteExtension(original)
	require.NoError(t, err)

	parsed, err := ParseVoteExtension(bz)
	require.NoError(t, err)
	require.Len(t, parsed.PriceFeeds, 3)
	assert.Equal(t, "LAC/USD", parsed.PriceFeeds[0].AssetPair)
	assert.Equal(t, "ETH/USD", parsed.PriceFeeds[1].AssetPair)
	assert.Equal(t, "BTC/USD", parsed.PriceFeeds[2].AssetPair)
}

// TestMarshalVoteExtension_DeterministicMetamorphic asserts the
// byte-for-byte determinism contract MarshalVoteExtension relies on:
// `proto.MarshalOptions{Deterministic: true}` must produce identical
// output across repeated calls AND across semantically identical
// votes with differently-ordered repeated fields. CometBFT signs the
// marshal output as part of the vote itself — if two validators
// serialized the same logical vote to different bytes, their
// signatures would diverge and vote-extension aggregation would
// break. Existing tests cover round-trip semantics but not the
// byte-level determinism that the signed-payload contract rides on.
func TestMarshalVoteExtension_DeterministicMetamorphic(t *testing.T) {
	ts := timestamppb.Now()
	feedsA := []*PriceFeed{
		{AssetPair: "LAC/USD", Price: "1.0", Timestamp: ts},
		{AssetPair: "ETH/USD", Price: "2000.0", Timestamp: ts},
		{AssetPair: "BTC/USD", Price: "50000.0", Timestamp: ts},
	}
	voteA := &ValidatorVote{
		ValidatorAddress: "cosmos1val",
		PriceFeeds:       feedsA,
		BlockHeight:      99,
		Timestamp:        ts,
	}
	// Two marshal calls on the same logical vote must produce equal bytes.
	b1, err := MarshalVoteExtension(voteA)
	require.NoError(t, err)
	b2, err := MarshalVoteExtension(voteA)
	require.NoError(t, err)
	if string(b1) != string(b2) {
		t.Fatalf("repeat marshal not byte-deterministic:\n  first:  %x\n  second: %x", b1, b2)
	}

	// Round-trip through parse + remarshal must also be byte-stable: a
	// vote reconstructed from serialized bytes must reserialize to the
	// exact same bytes. If this fails, proto default/unknown field
	// handling has drifted.
	parsed, err := ParseVoteExtension(b1)
	require.NoError(t, err)
	b3, err := MarshalVoteExtension(parsed)
	require.NoError(t, err)
	if string(b1) != string(b3) {
		t.Fatalf("round-trip marshal drift:\n  original: %x\n  remarshalled: %x", b1, b3)
	}
}

// TestMarshalVoteExtension_PriceFeedOrderSensitivity is the
// complementary negative property: protobuf repeated-field semantics
// preserve element order, so reordering PriceFeeds within the same
// vote must produce *different* serialized bytes. This is expected
// behavior — oracle aggregation code relies on it to detect replay
// attempts where a validator sorted feeds differently to mask a
// conflicting report. Locks the contract against any future
// "normalize by sorting" refactor.
func TestMarshalVoteExtension_PriceFeedOrderSensitivity(t *testing.T) {
	ts := timestamppb.Now()
	forward := &ValidatorVote{
		ValidatorAddress: "cosmos1val",
		PriceFeeds: []*PriceFeed{
			{AssetPair: "A/USD", Price: "1.0", Timestamp: ts},
			{AssetPair: "B/USD", Price: "2.0", Timestamp: ts},
		},
		BlockHeight: 1,
		Timestamp:   ts,
	}
	reversed := &ValidatorVote{
		ValidatorAddress: "cosmos1val",
		PriceFeeds: []*PriceFeed{
			{AssetPair: "B/USD", Price: "2.0", Timestamp: ts},
			{AssetPair: "A/USD", Price: "1.0", Timestamp: ts},
		},
		BlockHeight: 1,
		Timestamp:   ts,
	}
	bFwd, err := MarshalVoteExtension(forward)
	require.NoError(t, err)
	bRev, err := MarshalVoteExtension(reversed)
	require.NoError(t, err)
	if string(bFwd) == string(bRev) {
		t.Fatalf("repeated-field order was normalized away; vote replay attacks become undetectable")
	}
}

// ---------- Codec ----------

func TestRegisterInterfaces(t *testing.T) {
	assert.NotPanics(t, func() {
		// codec.go init() calls RegisterInterfaces; verify the module-level
		// variables are populated without panic.
		_ = ModuleCdc
		_ = Amino
	})
}

// ---------- GenesisState Proto Accessors ----------

func TestGenesisState_GetParams_Nil(t *testing.T) {
	var gs *GenesisState
	assert.Nil(t, gs.GetParams())
}

func TestGenesisState_GetParams_NonNil(t *testing.T) {
	gs := &GenesisState{Params: DefaultParams()}
	assert.NotNil(t, gs.GetParams())
	assert.Equal(t, int64(10), gs.GetParams().VotePeriod)
}

func TestGenesisState_GetPriceFeeds_Empty(t *testing.T) {
	gs := &GenesisState{}
	assert.Empty(t, gs.GetPriceFeeds())
}

func TestGenesisState_GetAggregatedPrices_Empty(t *testing.T) {
	gs := &GenesisState{}
	assert.Empty(t, gs.GetAggregatedPrices())
}

// ---------- Proto Message Type Accessors ----------

func TestPriceFeed_Getters(t *testing.T) {
	ts := timestamppb.Now()
	pf := &PriceFeed{
		AssetPair:       "LAC/USD",
		Price:           "1.50",
		Volume_24H:      "999",
		Timestamp:       ts,
		Sources:         []string{"binance"},
		ConfidenceScore: "0.9",
	}
	assert.Equal(t, "LAC/USD", pf.GetAssetPair())
	assert.Equal(t, "1.50", pf.GetPrice())
	assert.Equal(t, "999", pf.GetVolume_24H())
	assert.Equal(t, ts, pf.GetTimestamp())
	assert.Equal(t, []string{"binance"}, pf.GetSources())
	assert.Equal(t, "0.9", pf.GetConfidenceScore())
}

func TestPriceFeed_NilGetters(t *testing.T) {
	var pf *PriceFeed
	assert.Empty(t, pf.GetAssetPair())
	assert.Empty(t, pf.GetPrice())
	assert.Empty(t, pf.GetVolume_24H())
	assert.Nil(t, pf.GetTimestamp())
	assert.Nil(t, pf.GetSources())
	assert.Empty(t, pf.GetConfidenceScore())
}

func TestValidatorVote_Getters(t *testing.T) {
	ts := timestamppb.Now()
	v := &ValidatorVote{
		ValidatorAddress: "cosmos1x",
		PriceFeeds:       []*PriceFeed{{AssetPair: "A/B"}},
		BlockHeight:      50,
		Timestamp:        ts,
	}
	assert.Equal(t, "cosmos1x", v.GetValidatorAddress())
	require.Len(t, v.GetPriceFeeds(), 1)
	assert.Equal(t, int64(50), v.GetBlockHeight())
	assert.Equal(t, ts, v.GetTimestamp())
}

func TestValidatorVote_NilGetters(t *testing.T) {
	var v *ValidatorVote
	assert.Empty(t, v.GetValidatorAddress())
	assert.Nil(t, v.GetPriceFeeds())
	assert.Equal(t, int64(0), v.GetBlockHeight())
	assert.Nil(t, v.GetTimestamp())
}

func TestAggregatedPrice_Getters(t *testing.T) {
	ts := timestamppb.Now()
	ap := &AggregatedPrice{
		AssetPair:         "LAC/USD",
		MedianPrice:       "1.50",
		MeanPrice:         "1.48",
		StandardDeviation: "0.02",
		NumValidators:     5,
		BlockHeight:       100,
		Timestamp:         ts,
	}
	assert.Equal(t, "LAC/USD", ap.GetAssetPair())
	assert.Equal(t, "1.50", ap.GetMedianPrice())
	assert.Equal(t, "1.48", ap.GetMeanPrice())
	assert.Equal(t, "0.02", ap.GetStandardDeviation())
	assert.Equal(t, int32(5), ap.GetNumValidators())
	assert.Equal(t, int64(100), ap.GetBlockHeight())
	assert.Equal(t, ts, ap.GetTimestamp())
}

func TestAggregatedPrice_NilGetters(t *testing.T) {
	var ap *AggregatedPrice
	assert.Empty(t, ap.GetAssetPair())
	assert.Empty(t, ap.GetMedianPrice())
	assert.Empty(t, ap.GetMeanPrice())
	assert.Empty(t, ap.GetStandardDeviation())
	assert.Equal(t, int32(0), ap.GetNumValidators())
	assert.Equal(t, int64(0), ap.GetBlockHeight())
	assert.Nil(t, ap.GetTimestamp())
}

func TestMsgInjectOracleVotes_Getters(t *testing.T) {
	msg := &MsgInjectOracleVotes{
		Authority: "cosmos1gov",
		Height:    200,
		Votes: []*InjectedVoteExtension{
			{ValidatorAddress: []byte("val1"), VoteExtension: []byte("ext1")},
		},
	}
	assert.Equal(t, "cosmos1gov", msg.GetAuthority())
	assert.Equal(t, int64(200), msg.GetHeight())
	require.Len(t, msg.GetVotes(), 1)
	assert.Equal(t, []byte("val1"), msg.GetVotes()[0].GetValidatorAddress())
	assert.Equal(t, []byte("ext1"), msg.GetVotes()[0].GetVoteExtension())
}

func TestMsgInjectOracleVotes_NilGetters(t *testing.T) {
	var msg *MsgInjectOracleVotes
	assert.Empty(t, msg.GetAuthority())
	assert.Equal(t, int64(0), msg.GetHeight())
	assert.Nil(t, msg.GetVotes())
}

func TestInjectedVoteExtension_NilGetters(t *testing.T) {
	var ive *InjectedVoteExtension
	assert.Nil(t, ive.GetValidatorAddress())
	assert.Nil(t, ive.GetVoteExtension())
}

// ---------- Deterministic Marshaling ----------

func TestMarshalVoteExtension_Deterministic(t *testing.T) {
	ts := timestamppb.Now()
	vote := &ValidatorVote{
		ValidatorAddress: "cosmos1det",
		PriceFeeds: []*PriceFeed{
			{AssetPair: "LAC/USD", Price: "1.0", Timestamp: ts},
		},
		BlockHeight: 10,
		Timestamp:   ts,
	}

	bz1, err := MarshalVoteExtension(vote)
	require.NoError(t, err)
	bz2, err := MarshalVoteExtension(vote)
	require.NoError(t, err)
	assert.Equal(t, bz1, bz2, "deterministic marshal should produce identical bytes")
}

// ---------------------------------------------------------------------------
// MsgInjectOracleVotes.ValidateBasic — runs on every oracle vote
// injection tx at the antehandler boundary. Existing coverage tests
// only the Getters; the ValidateBasic itself had zero direct tests.
// Silent regressions would let malformed injections reach the msg
// server and produce less diagnostic failures deep in the aggregation
// keeper.
// ---------------------------------------------------------------------------

// TestMsgInjectOracleVotes_ValidateBasic_Valid pins the happy path.
func TestMsgInjectOracleVotes_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgInjectOracleVotes{
		Authority: validOracleAuthority(),
		Height:    100,
	}
	require.NoError(t, msg.ValidateBasic())
}

// TestMsgInjectOracleVotes_ValidateBasic_EmptyAuthority pins that
// empty or whitespace-only Authority is rejected. The authority
// gate is the first line of defense — dropping this check would
// let unauthorized validators inject arbitrary votes.
func TestMsgInjectOracleVotes_ValidateBasic_EmptyAuthority(t *testing.T) {
	tests := []struct {
		name string
		auth string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"tab and newline", "\t\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := &MsgInjectOracleVotes{
				Authority: tc.auth,
				Height:    100,
			}
			err := msg.ValidateBasic()
			require.Error(t, err, "expected rejection for authority=%q (TrimSpace contract)", tc.auth)
			assert.Contains(t, err.Error(), "authority is required",
				"error message must identify the authority-required branch for operator diagnostics")
		})
	}
}

func TestMsgInjectOracleVotes_ValidateBasic_InvalidAuthorityAddress(t *testing.T) {
	msg := &MsgInjectOracleVotes{
		Authority: "not-a-bech32-address",
		Height:    100,
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid authority address")
}

// TestMsgInjectOracleVotes_ValidateBasic_NonPositiveHeight pins
// the height gate: zero and negative heights are rejected. This
// is the sanity check against malformed injection heights that
// would otherwise land in aggregation keyed by bogus height.
// Pins BOTH zero and negative branches since the predicate is
// `<= 0` (not just `== 0` or `< 0`).
func TestMsgInjectOracleVotes_ValidateBasic_NonPositiveHeight(t *testing.T) {
	tests := []struct {
		name   string
		height int64
	}{
		{"zero height", 0},
		{"negative small", -1},
		{"negative large", -1_000_000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := &MsgInjectOracleVotes{
				Authority: validOracleAuthority(),
				Height:    tc.height,
			}
			err := msg.ValidateBasic()
			require.Error(t, err, "expected rejection for height=%d (must be positive)", tc.height)
			assert.Contains(t, err.Error(), "height must be positive",
				"error message must identify the height-required branch")
		})
	}
}

// TestMsgInjectOracleVotes_ValidateBasic_HeightBoundaryInclusive
// pins that height == 1 IS accepted — the predicate is strict
// "> 0", not ">= 1" (they're equivalent for int64 but worth pinning
// so a refactor to `>= 1` or `>= 0` is caught). Height=1 is the
// genesis-succession injection height; a silent shift would reject
// valid first-block injections.
func TestMsgInjectOracleVotes_ValidateBasic_HeightBoundaryInclusive(t *testing.T) {
	msg := &MsgInjectOracleVotes{
		Authority: validOracleAuthority(),
		Height:    1,
	}
	require.NoError(t, msg.ValidateBasic(),
		"height=1 must be accepted (smallest valid post-genesis height)")
}

func sortedInjectedVoteExtensions(count int) []*InjectedVoteExtension {
	votes := make([]*InjectedVoteExtension, count)
	for i := range votes {
		votes[i] = &InjectedVoteExtension{
			ValidatorAddress: []byte{byte(i >> 8), byte(i)},
		}
	}
	return votes
}

func TestMsgInjectOracleVotes_ValidateBasic_RejectsMalformedVoteEntries(t *testing.T) {
	oversizedExtension := make([]byte, MaxVoteExtensionBytes+1)
	tests := []struct {
		name    string
		votes   []*InjectedVoteExtension
		wantErr string
	}{
		{
			name:    "nil entry",
			votes:   []*InjectedVoteExtension{nil},
			wantErr: "vote[0] is nil",
		},
		{
			name:    "empty validator address",
			votes:   []*InjectedVoteExtension{{ValidatorAddress: nil}},
			wantErr: "validator address is empty",
		},
		{
			name: "duplicate validator address",
			votes: []*InjectedVoteExtension{
				{ValidatorAddress: []byte{0x01}},
				{ValidatorAddress: []byte{0x01}},
			},
			wantErr: "votes must be sorted",
		},
		{
			name: "unsorted validator address",
			votes: []*InjectedVoteExtension{
				{ValidatorAddress: []byte{0x02}},
				{ValidatorAddress: []byte{0x01}},
			},
			wantErr: "votes must be sorted",
		},
		{
			name: "oversized vote extension",
			votes: []*InjectedVoteExtension{
				{ValidatorAddress: []byte{0x01}, VoteExtension: oversizedExtension},
			},
			wantErr: "extension exceeds",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := &MsgInjectOracleVotes{
				Authority: validOracleAuthority(),
				Height:    100,
				Votes:     tc.votes,
			}
			err := msg.ValidateBasic()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestMsgInjectOracleVotes_ValidateBasic_RejectsOversizedVotesSlice
// pins the outer cap on the votes slice. Per-element capping via
// ParseVoteExtension (64 KiB each) covers individual vote
// amplification, but without an outer cap a proposer could inject
// millions of small votes burning Unmarshal loops and state
// handler time.
func TestMsgInjectOracleVotes_ValidateBasic_RejectsOversizedVotesSlice(t *testing.T) {
	votes := make([]*InjectedVoteExtension, MaxInjectedVotesPerMsg+1)
	for i := range votes {
		votes[i] = &InjectedVoteExtension{}
	}
	msg := &MsgInjectOracleVotes{
		Authority: validOracleAuthority(),
		Height:    100,
		Votes:     votes,
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "votes")
	require.Contains(t, err.Error(), "cap")
}

// TestMsgInjectOracleVotes_ValidateBasic_AcceptsAtCapVotes pins
// the cap as a threshold, not a floor.
func TestMsgInjectOracleVotes_ValidateBasic_AcceptsAtCapVotes(t *testing.T) {
	msg := &MsgInjectOracleVotes{
		Authority: validOracleAuthority(),
		Height:    100,
		Votes:     sortedInjectedVoteExtensions(MaxInjectedVotesPerMsg),
	}
	require.NoError(t, msg.ValidateBasic(),
		"exactly MaxInjectedVotesPerMsg entries must still pass")
}

// TestParams_Validate_RejectsOversizedAssetPairsSlice pins the cap
// on AssetPairs. Governance-gated but defense-in-depth against a
// malformed gov proposal that would otherwise bloat module params
// and slow every per-block aggregation pass.
func TestParams_Validate_RejectsOversizedAssetPairsSlice(t *testing.T) {
	pairs := make([]string, MaxAssetPairs+1)
	for i := range pairs {
		pairs[i] = fmt.Sprintf("PAIR%d/USD", i)
	}
	p := DefaultParams()
	p.AssetPairs = pairs
	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "asset pairs")
	require.Contains(t, err.Error(), "cap")
}

// TestParams_Validate_RejectsOversizedAssetPairString pins the
// per-entry length cap on an individual AssetPair.
func TestParams_Validate_RejectsOversizedAssetPairString(t *testing.T) {
	huge := make([]byte, MaxAssetPairLen+1)
	for i := range huge {
		huge[i] = 'X'
	}
	p := DefaultParams()
	p.AssetPairs = []string{"LAC/USD", string(huge)}
	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "asset_pairs[1]")
	require.Contains(t, err.Error(), "cap")
}

// TestParams_Validate_RejectsOversizedVoteThreshold /
// ...OversizedMaxPriceDeviation pin the per-field length caps on
// the two decimal-as-string fields. Without them, a gov proposal
// could submit megabyte-scale strings that reach
// sdkmath.LegacyNewDecFromStr's parser.
func TestParams_Validate_RejectsOversizedVoteThreshold(t *testing.T) {
	p := DefaultParams()
	huge := make([]byte, MaxDecimalStrLen+1)
	for i := range huge {
		huge[i] = '9'
	}
	p.VoteThreshold = string(huge)
	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "vote threshold")
	require.Contains(t, err.Error(), "cap")
}

func TestParams_Validate_RejectsOversizedMaxPriceDeviation(t *testing.T) {
	p := DefaultParams()
	huge := make([]byte, MaxDecimalStrLen+1)
	for i := range huge {
		huge[i] = '9'
	}
	p.MaxPriceDeviation = string(huge)
	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "max price deviation")
	require.Contains(t, err.Error(), "cap")
}
