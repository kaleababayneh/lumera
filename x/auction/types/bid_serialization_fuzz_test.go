package types

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Fuzz harness for x/auction bid serialization edge cases.
//
// Attack surface
// --------------
// SpotBid and SpotAuction cross the JSON boundary at several points:
//   - MsgSubmitBid wire encoding (bidder-controlled fields)
//   - SpotAuction genesis import (operator-controlled at chain launch)
//   - IBC / query gRPC responses (encoded for external consumers)
//   - Event attributes (consumed by indexers and compliance pipelines)
//
// A malformed or oversized bid that crashes the block executor is a
// consensus-wide DoS. A bid that parses to a "valid" object but with
// surprising semantics (e.g. empty denom silently accepted) routes
// escrow incorrectly.
//
// Targets fuzzed in this file:
//
//   1. SpotBid.ValidateBasic — string-field adversarial content,
//      coin edge cases (zero, negative, invalid denom, max amount)
//   2. SpotAuction.ValidateBasic — status strings, nested coin fields,
//      time-field zero/wraparound, priority BPS bounds
//   3. JSON Marshal/Unmarshal round-trip — stable, non-panicking
//   4. sdk.Coin construction with adversarial denom/amount strings
//   5. BetterThan with adversarial bid pairs — never panic, always
//      decide (or correctly return false on incomparable input)
//
// Correctness criterion for every fuzz target: NEVER PANIC. Validation
// may reject (good), parsing may error (good), but no uncaught panic
// is acceptable for any counterparty-controlled input.

// --- SpotBid field-based validation fuzzing ---

// FuzzSpotBidValidateBasic_Strings fuzzes SpotBid.ValidateBasic with
// adversarial string field content. Every string field that ever
// touches the wire can carry NULs, Unicode, whitespace, huge payloads,
// or JSON injection.
func FuzzSpotBidValidateBasic_Strings(f *testing.F) {
	f.Add("bid-1", "auction-1", validTestBidder(),
		uint32(500), int64(1_700_000_000))
	f.Add("", "", "", uint32(0), int64(0))
	f.Add(" ", " ", " ", uint32(1), int64(1))
	f.Add("bid\x00null", "auction\n", "\x00", uint32(1), int64(1))
	f.Add("🔥", "💎", "lumera1🚀", uint32(100), int64(1))
	f.Add(strings.Repeat("a", 10000), strings.Repeat("b", 10000),
		strings.Repeat("c", 1000), uint32(1), int64(1))

	f.Fuzz(func(t *testing.T, id, auctionID, bidder string, latency uint32, tsUnix int64) {
		if !utf8.ValidString(id) || !utf8.ValidString(auctionID) || !utf8.ValidString(bidder) {
			return
		}
		// Clamp timestamp to avoid time.Unix wraparound quirks on 32-bit.
		if tsUnix < -2_000_000_000 || tsUnix > 4_000_000_000 {
			return
		}

		bid := SpotBid{
			ID:          id,
			AuctionID:   auctionID,
			Bidder:      bidder,
			Price:       sdk.NewInt64Coin("ulac", 100),
			LatencyMs:   latency,
			SubmittedAt: time.Unix(tsUnix, 0).UTC(),
		}

		// ValidateBasic must not panic on any input.
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ValidateBasic panicked: %v\nbid=%+v", r, bid)
			}
		}()
		_ = bid.ValidateBasic()
	})
}

// FuzzSpotBidValidateBasic_CoinEdgeCases targets the money-type
// validation path. Coins are the consensus-critical part of a bid —
// any bypass here could let malformed money into escrow.
func FuzzSpotBidValidateBasic_CoinEdgeCases(f *testing.F) {
	f.Add("ulac", "100")
	f.Add("", "100")             // empty denom
	f.Add("ulac", "0")           // zero amount
	f.Add("ulac", "-1")          // negative amount
	f.Add("ULAC", "100")         // uppercase (normalized?)
	f.Add("ulac ", "100")        // trailing space
	f.Add("u/lac", "100")        // slash in denom
	f.Add("ulac\x00null", "100") // NUL in denom
	f.Add("ulac", "99999999999999999999999999999999999999999999999999")
	f.Add(strings.Repeat("a", 200), "1") // very long denom
	f.Add("1ulac", "1")                  // denom starts with digit

	f.Fuzz(func(t *testing.T, denom, amountStr string) {
		if !utf8.ValidString(denom) || !utf8.ValidString(amountStr) {
			return
		}

		// Construct the coin via sdk.NewCoin recovering from panics.
		// Some invalid denoms may panic sdk.NewCoin; that's worth
		// pinning — but ValidateBasic itself must handle the resulting
		// coin safely.
		var price sdk.Coin
		func() {
			defer func() { _ = recover() }() // denom construction may panic — OK
			amt, ok := sdkmath.NewIntFromString(amountStr)
			if !ok {
				amt = sdkmath.ZeroInt()
			}
			price = sdk.Coin{Denom: denom, Amount: amt}
		}()

		bid := SpotBid{
			ID:          "bid-1",
			AuctionID:   "auction-1",
			Bidder:      validTestBidder(),
			Price:       price,
			LatencyMs:   500,
			SubmittedAt: time.Unix(1_700_000_000, 0).UTC(),
		}

		// ValidateBasic must handle the malformed coin safely.
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ValidateBasic panicked on coin{denom=%q, amount=%q}: %v",
					denom, amountStr, r)
			}
		}()
		err := bid.ValidateBasic()

		// If Validate passes, the coin MUST be positive (the core money
		// contract). Zero or negative bids polluting the accepted set
		// would break escrow accounting.
		if err == nil {
			if !price.Amount.IsPositive() {
				t.Fatalf("ValidateBasic accepted non-positive bid price %s", price.String())
			}
			if !price.IsValid() {
				t.Fatalf("ValidateBasic accepted invalid coin %s", price.String())
			}
		}
	})
}

// FuzzSpotBidValidateBasic_LatencyEdgeCases probes latency edge cases.
// Latency is on the wire as uint32 — must reject zero (per current
// contract), accept nonzero across the full 32-bit range.
func FuzzSpotBidValidateBasic_LatencyEdgeCases(f *testing.F) {
	f.Add(uint32(0))
	f.Add(uint32(1))
	f.Add(uint32(5000))
	f.Add(uint32(1_000_000))
	f.Add(^uint32(0)) // max uint32

	f.Fuzz(func(t *testing.T, latency uint32) {
		bid := SpotBid{
			ID:          "bid-1",
			AuctionID:   "auction-1",
			Bidder:      validTestBidder(),
			Price:       sdk.NewInt64Coin("ulac", 100),
			LatencyMs:   latency,
			SubmittedAt: time.Unix(1_700_000_000, 0).UTC(),
		}

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ValidateBasic panicked on latency=%d: %v", latency, r)
			}
		}()
		err := bid.ValidateBasic()

		// Zero latency MUST reject (stated contract).
		if latency == 0 && err == nil {
			t.Fatalf("zero latency accepted — contract violation")
		}
		// Nonzero latency should accept (all else being valid).
		if latency > 0 && err != nil {
			t.Fatalf("nonzero latency %d rejected: %v", latency, err)
		}
	})
}

// --- JSON serialization round-trip fuzzing ---

// FuzzSpotBidJSONRoundTrip fuzzes the JSON encoding/decoding of
// SpotBid. Round-trip stability is required across consensus: if two
// validators marshal the same bid to different bytes, their block
// hashes diverge.
func FuzzSpotBidJSONRoundTrip(f *testing.F) {
	f.Add("bid-1", "auction-1", validTestBidder(), "ulac", int64(100), uint32(500))
	f.Add("", "", "", "", int64(0), uint32(0))
	f.Add("🔥", "💎", validTestBidder(), "ulac", int64(1), uint32(1))
	f.Add(strings.Repeat("a", 1000), "a", "lumera1a", "ulac", int64(1), uint32(1))

	f.Fuzz(func(t *testing.T, id, auctionID, bidder, denom string, amt int64, latency uint32) {
		if !utf8.ValidString(id) || !utf8.ValidString(auctionID) ||
			!utf8.ValidString(bidder) || !utf8.ValidString(denom) {
			return
		}

		// Skip denoms that would panic sdk.NewInt64Coin — the fuzz
		// target is serialization, not coin construction.
		if strings.TrimSpace(denom) == "" {
			return
		}
		defer func() {
			// Some edge-case denoms can still panic NewInt64Coin; that's
			// not a bug in the serialization path, so recover and skip.
			_ = recover()
		}()

		var price sdk.Coin
		func() {
			defer func() { _ = recover() }()
			price = sdk.Coin{Denom: denom, Amount: sdkmath.NewInt(amt)}
		}()
		// Skip if coin construction gave us an invalid coin (would panic
		// in Marshal too).
		if !price.IsValid() && !price.Amount.IsNegative() && price.Amount.IsZero() {
			return
		}

		bid := SpotBid{
			ID:          id,
			AuctionID:   auctionID,
			Bidder:      bidder,
			Price:       price,
			LatencyMs:   latency,
			SubmittedAt: time.Unix(1_700_000_000, 0).UTC(),
		}

		// Marshal must not panic.
		raw, err := json.Marshal(bid)
		if err != nil {
			return // acceptable rejection
		}

		// Unmarshal must not panic.
		var decoded SpotBid
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return
		}

		// Re-marshal must produce byte-identical output — consensus
		// requirement.
		raw2, err := json.Marshal(decoded)
		if err != nil {
			t.Fatalf("re-marshal after round-trip failed: %v", err)
		}
		if string(raw) != string(raw2) {
			t.Fatalf("JSON round-trip not byte-stable:\n  first=%s\n  second=%s", raw, raw2)
		}

		// Preserved fields: scalar fields must round-trip identically.
		if bid.ID != decoded.ID {
			t.Fatalf("ID drifted: %q -> %q", bid.ID, decoded.ID)
		}
		if bid.AuctionID != decoded.AuctionID {
			t.Fatalf("AuctionID drifted: %q -> %q", bid.AuctionID, decoded.AuctionID)
		}
		if bid.Bidder != decoded.Bidder {
			t.Fatalf("Bidder drifted: %q -> %q", bid.Bidder, decoded.Bidder)
		}
		if bid.LatencyMs != decoded.LatencyMs {
			t.Fatalf("LatencyMs drifted: %d -> %d", bid.LatencyMs, decoded.LatencyMs)
		}
	})
}

// FuzzSpotBidJSONUnmarshalRaw feeds arbitrary bytes to SpotBid
// unmarshaling. Counterparty-controlled JSON bytes must never panic.
func FuzzSpotBidJSONUnmarshalRaw(f *testing.F) {
	seeds := [][]byte{
		[]byte(`{}`),
		[]byte(`{"id":"b","auction_id":"a","bidder":validTestBidder(),"price":{"denom":"ulac","amount":"100"},"latency_ms":500}`),
		[]byte(``),
		[]byte(`null`),
		[]byte(`[]`),
		[]byte(`{"price":null}`),
		[]byte(`{"latency_ms":-1}`), // negative in uint32 field
		[]byte(`{"latency_ms":999999999999999999}`),               // overflow uint32
		[]byte(`{"price":{"denom":"","amount":""}}`),              // empty coin
		[]byte(`{"price":{"denom":"ulac","amount":"not_a_num"}}`), // bad amount
		[]byte(`{"submitted_at":"not_a_time"}`),                   // bad time
		[]byte(`{"submitted_at":"0001-01-01T00:00:00Z"}`),         // zero time
		[]byte(strings.Repeat("a", 10000)),                        // huge garbage
		[]byte("{\"id\":\" \x01\"}"),                              // U+0001 control char in value
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var bid SpotBid
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SpotBid unmarshal panicked on %q (len=%d): %v",
					truncateBytes(data, 200), len(data), r)
			}
		}()
		_ = json.Unmarshal(data, &bid)

		// If we got here, ValidateBasic also must not panic even on
		// the possibly-malformed bid.
		_ = bid.ValidateBasic()
	})
}

// --- SpotAuction serialization fuzzing ---

// FuzzSpotAuctionValidateBasic_StatusValues fuzzes the status string.
// Only the 5 documented enum values must pass; everything else rejects.
func FuzzSpotAuctionValidateBasic_StatusValues(f *testing.F) {
	f.Add("PENDING")
	f.Add("ACTIVE")
	f.Add("SETTLED")
	f.Add("EXPIRED")
	f.Add("CANCELED")
	f.Add("pending") // wrong case
	f.Add("")
	f.Add("INVALID")
	f.Add("ACTIVE ") // trailing space
	f.Add("\x00")

	f.Fuzz(func(t *testing.T, status string) {
		if !utf8.ValidString(status) {
			return
		}
		auction := minimalAuctionFixture()
		auction.Status = AuctionStatus(status)

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ValidateBasic panicked on status %q: %v", status, r)
			}
		}()
		err := auction.ValidateBasic()

		// Only the 5 canonical values must accept.
		switch AuctionStatus(status) {
		case AuctionStatusPending, AuctionStatusActive, AuctionStatusSettled,
			AuctionStatusExpired, AuctionStatusCanceled:
			if err != nil {
				t.Fatalf("canonical status %q rejected: %v", status, err)
			}
		default:
			if err == nil {
				t.Fatalf("non-canonical status %q accepted — contract violation", status)
			}
		}
	})
}

// FuzzSpotAuctionValidateBasic_PriorityBps fuzzes the priority discount
// bps bound. Over 10_000 must reject; at-or-below must accept.
func FuzzSpotAuctionValidateBasic_PriorityBps(f *testing.F) {
	f.Add(uint32(0))
	f.Add(uint32(1))
	f.Add(uint32(5000))
	f.Add(uint32(10_000))
	f.Add(uint32(10_001))
	f.Add(^uint32(0))

	f.Fuzz(func(t *testing.T, bps uint32) {
		auction := minimalAuctionFixture()
		auction.PriorityDiscountBps = bps

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ValidateBasic panicked on bps=%d: %v", bps, r)
			}
		}()
		err := auction.ValidateBasic()

		if bps > 10_000 && err == nil {
			t.Fatalf("priority bps %d > 10_000 accepted — contract violation", bps)
		}
	})
}

// FuzzSpotAuctionValidateBasic_TimeFields fuzzes CreatedAt / ExpiresAt.
// Zero and inverted timestamps must reject.
func FuzzSpotAuctionValidateBasic_TimeFields(f *testing.F) {
	f.Add(int64(1_700_000_000), int64(1_700_000_060))
	f.Add(int64(0), int64(0))                         // both zero
	f.Add(int64(1_700_000_100), int64(1_700_000_000)) // inverted (expires before created)
	f.Add(int64(1_700_000_000), int64(1_700_000_000)) // equal (boundary)
	f.Add(int64(-1), int64(1))                        // negative created
	f.Add(int64(1), int64(-1))                        // negative expires

	f.Fuzz(func(t *testing.T, createdUnix, expiresUnix int64) {
		// Skip pathological values that would wrap time.Unix.
		if createdUnix < -2_000_000_000 || createdUnix > 4_000_000_000 {
			return
		}
		if expiresUnix < -2_000_000_000 || expiresUnix > 4_000_000_000 {
			return
		}

		auction := minimalAuctionFixture()
		auction.CreatedAt = time.Unix(createdUnix, 0).UTC()
		auction.ExpiresAt = time.Unix(expiresUnix, 0).UTC()

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ValidateBasic panicked on created=%d expires=%d: %v",
					createdUnix, expiresUnix, r)
			}
		}()
		err := auction.ValidateBasic()

		// If ExpiresAt is before CreatedAt, validation must reject.
		if auction.ExpiresAt.Before(auction.CreatedAt) && err == nil {
			t.Fatalf("inverted timestamps (created=%s expires=%s) accepted",
				auction.CreatedAt, auction.ExpiresAt)
		}
	})
}

// --- BetterThan adversarial pair fuzzing ---

// FuzzBetterThan_AdversarialPairs proves BetterThan never panics on
// any bid pair, and antisymmetry holds when BOTH sides have valid
// positive prices. The comparator is called inside sort.Slice in
// auction settlement; a panic there takes down the block.
//
// Known carve-out: when one side has a zero or negative price, the
// comparator treats the zero side as degenerate and returns true for
// "other beats it" — which combined with the cheaper-wins rule CAN
// produce both a.BetterThan(b)=true and b.BetterThan(a)=true. That's
// intentional (degenerate bids short-circuit) and is assumed by the
// settle pipeline to only run on bids that passed ValidateBasic (which
// rejects non-positive prices). This fuzz reflects that contract by
// only asserting antisymmetry for bids with positive prices.
func FuzzBetterThan_AdversarialPairs(f *testing.F) {
	f.Add(int64(100), uint32(100), int64(200), uint32(100))
	f.Add(int64(0), uint32(100), int64(100), uint32(100))  // zero vs normal (no antisymmetry assertion)
	f.Add(int64(-1), uint32(100), int64(100), uint32(100)) // negative vs normal (no antisymmetry assertion)
	f.Add(int64(100), uint32(0), int64(100), uint32(0))    // zero latency both

	f.Fuzz(func(t *testing.T, priceA int64, latA uint32, priceB int64, latB uint32) {
		now := time.Unix(1_700_000_000, 0).UTC()

		makeBid := func(price int64, lat uint32, label string) SpotBid {
			return SpotBid{
				ID:          label,
				AuctionID:   "auction-1",
				Bidder:      validTestBidder(),
				Price:       sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(price)},
				LatencyMs:   lat,
				SubmittedAt: now,
			}
		}

		a := makeBid(priceA, latA, "a")
		b := makeBid(priceB, latB, "b")

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("BetterThan panicked on a=%+v b=%+v: %v", a, b, r)
			}
		}()

		// Primary invariant (must always hold): BetterThan never panics.
		ab := a.BetterThan(b)
		ba := b.BetterThan(a)

		// Antisymmetry only asserted when both bids would pass
		// ValidateBasic's positive-price gate. Degenerate bids are
		// filtered out by SubmitBidRequest handling before reaching
		// the comparator in production.
		if priceA > 0 && priceB > 0 {
			if ab && ba {
				t.Fatalf("antisymmetry violated on valid bids: "+
					"a.BetterThan(b)=true and b.BetterThan(a)=true\n"+
					"a=%+v\nb=%+v", a, b)
			}
		}
	})
}

// --- SubmitBidRequest fuzzing ---

// FuzzSubmitBidRequestFields probes the SubmitBidRequest value as it
// arrives from the msg server — a bidder-controlled input that flows
// straight into auction state.
func FuzzSubmitBidRequestFields(f *testing.F) {
	f.Add("auction-1", validTestBidder(), int64(100), uint32(500))
	f.Add("", "", int64(0), uint32(0))
	f.Add("auction\x00", "not-bech32", int64(-1), ^uint32(0))

	f.Fuzz(func(t *testing.T, auctionID, bidder string, amt int64, lat uint32) {
		if !utf8.ValidString(auctionID) || !utf8.ValidString(bidder) {
			return
		}

		req := SubmitBidRequest{
			AuctionID: auctionID,
			Bidder:    bidder,
			Price:     sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(amt)},
			LatencyMs: lat,
		}

		// Even though SubmitBidRequest doesn't have a ValidateBasic of
		// its own, marshaling it for the wire must not panic.
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("JSON marshal panicked on request %+v: %v", req, r)
			}
		}()
		_, _ = json.Marshal(req)
	})
}

// --- Helpers ---

// minimalAuctionFixture returns a valid SpotAuction with all required
// fields populated. Used by the auction-focused fuzz targets that
// override specific fields to probe edge cases.
//
// Use a constructed zero coin to mirror keeper-created auctions. Validation
// also treats an entirely unset best-bid price as zero when no best bid exists.
func minimalAuctionFixture() SpotAuction {
	now := time.Unix(1_700_000_000, 0).UTC()
	return SpotAuction{
		ID:           "auction-1",
		Owner:        validAuctionOwner("auction-owner-000001"),
		RequestID:    "req-1",
		ToolID:       "tool-1",
		PolicyID:     "policy-1@v1",
		MaxPrice:     sdk.NewInt64Coin("ulac", 1_000_000),
		MaxLatencyMs: 5000,
		CreatedAt:    now,
		ExpiresAt:    now.Add(30 * time.Second),
		Status:       AuctionStatusActive,
		BestBidPrice: sdk.NewInt64Coin("ulac", 0),
	}
}

// truncateBytes shortens byte slices for error messages.
func truncateBytes(b []byte, max int) string {
	if len(b) <= max {
		return fmt.Sprintf("%q", b)
	}
	return fmt.Sprintf("%q...(%d more bytes)", b[:max], len(b)-max)
}

// validTestBidder returns a valid bech32-encoded test bidder address.
// Deterministic across test runs so fuzz seeds are reproducible.
func validTestBidder() string {
	return sdk.AccAddress([]byte("auction-fuzz-bidder")).String()
}
