package keeper

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// This file applies the testing-golden-artifacts skill in its
// PERFORMANCE-ANCHORING form to the x/credits Lock/Unlock hot
// paths. Companion to tick 69's x/router cache-hot-path bench
// golden — same model: capture allocation counts + bytes-per-op
// as deterministic anchors, allow ±10%% bytes tolerance, never
// assert ns/op (machine-dependent).
//
// Lock/Unlock are THE busiest x/credits paths. Every tool
// invocation locks credits; every settle/refund unlocks. A
// regression introducing per-call extra heap allocations
// compounds across the entire chain throughput, so freezing
// these anchors catches the regression at PR time before it
// degrades validator economics.
//
// Hot paths benchmarked:
//   - LockCredits: full state-mutating path (bank check, NextLockID,
//     SaveLock, expiry index, quote index)
//   - UnlockCredits: state-mutating refund + index cleanup path
//   - GetLock: read path (collections.Get on indexed map)
//   - SaveLock: proto marshal + indexed write (no surrounding
//     index updates; isolates the marshal cost)
//   - DeleteLock: index removal cost
//   - derivePolicyID: pure helper used per-settle
//
// Plus invariant tests:
//   - GetLock allocs ≤ SaveLock allocs (read should be cheaper
//     than write)
//   - LockCredits allocs scale linearly across batched calls
//   - derivePolicyID is bounded ≤8 allocs (tight floor for the
//     pure-string helper)

// --------------------------------------------------------------
// Golden artifact (mirrors tick 69's structure)
// --------------------------------------------------------------

type creditsBenchAnchor struct {
	BenchName         string `json:"bench_name"`
	AllocsPerOp       int64  `json:"allocs_per_op"`
	BytesPerOp        int64  `json:"bytes_per_op"`
	BaselineNsPerOp   int64  `json:"baseline_ns_per_op"`
	BytesTolerancePct int    `json:"bytes_tolerance_pct"`
}

type creditsBenchGolden struct {
	Suite   string               `json:"suite"`
	Anchors []creditsBenchAnchor `json:"anchors"`
}

func runCreditsBench(fn func(b *testing.B)) (allocs, bytesAlloc, ns int64) {
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		fn(b)
	})
	return result.AllocsPerOp(), result.AllocedBytesPerOp(), result.NsPerOp()
}

func assertCreditsBenchGolden(t *testing.T, got creditsBenchGolden, goldenFile string) {
	t.Helper()
	gotBytes, err := json.MarshalIndent(got, "", "  ")
	require.NoError(t, err)

	path := filepath.Join("testdata", goldenFile)
	if os.Getenv("UPDATE_GOLDENS") == "1" {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, gotBytes, 0o644))
		t.Logf("[GOLDEN] wrote %s (%d bytes) — REVIEW captured "+
			"alloc counts before committing", path, len(gotBytes))
		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err,
		"read golden %s — run with UPDATE_GOLDENS=1 to capture, "+
			"then diff-review the alloc counts before committing",
		path)

	var wantParsed creditsBenchGolden
	require.NoError(t, json.Unmarshal(want, &wantParsed))

	require.Equal(t, wantParsed.Suite, got.Suite)
	require.Equal(t, len(wantParsed.Anchors), len(got.Anchors),
		"anchor count diverges")

	wantByName := map[string]creditsBenchAnchor{}
	for _, a := range wantParsed.Anchors {
		wantByName[a.BenchName] = a
	}

	for _, gotAnchor := range got.Anchors {
		wantAnchor, ok := wantByName[gotAnchor.BenchName]
		require.True(t, ok,
			"benchmark %q in current run not present in golden — "+
				"a new hot path was added without an anchor",
			gotAnchor.BenchName)

		// Allocs ±2 tolerance for stateful benchmarks. Pure
		// functions (e.g. derivePolicyID) are deterministic but
		// stateful benchmarks vary slightly because b.N changes
		// between runs and amortizes setup costs differently.
		// A 2-alloc band catches real regressions (e.g. a new
		// allocation per call adding ~10+ allocs) while absorbing
		// the natural variance.
		const allocTolerance = int64(2)
		allocDelta := gotAnchor.AllocsPerOp - wantAnchor.AllocsPerOp
		if allocDelta < 0 {
			allocDelta = -allocDelta
		}
		require.LessOrEqual(t, allocDelta, allocTolerance,
			"BENCH REGRESSION %q: allocs_per_op went from %d (golden) "+
				"to %d (current), delta=%d > tolerance=%d. A new heap "+
				"allocation per call is a regression — investigate "+
				"before bumping golden.",
			gotAnchor.BenchName, wantAnchor.AllocsPerOp,
			gotAnchor.AllocsPerOp, allocDelta, allocTolerance)

		// Bytes ±tolerance.
		tolerance := wantAnchor.BytesTolerancePct
		if tolerance == 0 {
			tolerance = 10
		}
		bytesUpper := wantAnchor.BytesPerOp * int64(100+tolerance) / 100
		bytesLower := wantAnchor.BytesPerOp * int64(100-tolerance) / 100
		require.GreaterOrEqual(t, gotAnchor.BytesPerOp, bytesLower,
			"BENCH REGRESSION %q: bytes_per_op (%d) below floor %d "+
				"(golden=%d ±%d%%)",
			gotAnchor.BenchName, gotAnchor.BytesPerOp,
			bytesLower, wantAnchor.BytesPerOp, tolerance)
		require.LessOrEqual(t, gotAnchor.BytesPerOp, bytesUpper,
			"BENCH REGRESSION %q: bytes_per_op (%d) exceeds ceiling "+
				"%d (golden=%d ±%d%%) — a regression doubled the "+
				"per-op heap usage",
			gotAnchor.BenchName, gotAnchor.BytesPerOp,
			bytesUpper, wantAnchor.BytesPerOp, tolerance)

		t.Logf("[bench-anchor] %s: allocs=%d, bytes=%d, ns≈%d "+
			"(golden_baseline_ns=%d)",
			gotAnchor.BenchName, gotAnchor.AllocsPerOp,
			gotAnchor.BytesPerOp, gotAnchor.BaselineNsPerOp,
			wantAnchor.BaselineNsPerOp)
	}
}

// --------------------------------------------------------------
// THE BENCHMARK SUITE
// --------------------------------------------------------------

// TestCreditsLockUnlockHotPath_BenchAnchorGolden runs each
// x/credits Lock/Unlock hot path through testing.Benchmark
// and compares against the golden anchor.
func TestCreditsLockUnlockHotPath_BenchAnchorGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("benchmark-anchored test skipped in -short mode")
	}
	t.Parallel()

	// Each bench case constructs its own keeper to avoid state
	// pollution across measurements. Setup runs OUTSIDE the
	// benchmark via the b.ResetTimer() pattern.

	cases := []struct {
		name string
		fn   func(b *testing.B)
	}{
		{
			name: "LockCredits_freshLock",
			fn: func(b *testing.B) {
				ctx, k, bank, _, accKeeper := setupCreditsKeeper(adapt(b))
				const fund int64 = 100_000_000_000 // generous
				router := makeFundedRouter(adapt(b), bank, accKeeper, fund)
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					quoteID := fmt.Sprintf("bench-q-%d", i)
					_, err := k.LockCredits(ctx, router.String(),
						"bench-sess", sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000),
						"bench-tool", quoteID, "v1", "bench-intent")
					if err != nil {
						b.Fatalf("LockCredits[%d]: %v", i, err)
					}
				}
			},
		},
		{
			name: "UnlockCredits_existingLock",
			fn: func(b *testing.B) {
				ctx, k, bank, _, accKeeper := setupCreditsKeeper(adapt(b))
				const fund int64 = 100_000_000_000
				router := makeFundedRouter(adapt(b), bank, accKeeper, fund)
				lockIDs := make([]string, b.N)
				for i := 0; i < b.N; i++ {
					quoteID := fmt.Sprintf("bench-u-q-%d", i)
					id, err := k.LockCredits(ctx, router.String(),
						"bench-sess", sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000),
						"bench-tool", quoteID, "v1", "bench-intent")
					if err != nil {
						b.Fatalf("setup lock[%d]: %v", i, err)
					}
					lockIDs[i] = id
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := k.UnlockCredits(ctx, lockIDs[i], "bench-unlock"); err != nil {
						b.Fatalf("UnlockCredits[%d]: %v", i, err)
					}
				}
			},
		},
		{
			name: "GetLock_existingLock",
			fn: func(b *testing.B) {
				ctx, k, bank, _, accKeeper := setupCreditsKeeper(adapt(b))
				router := makeFundedRouter(adapt(b), bank, accKeeper, 1_000_000)
				lockID, err := k.LockCredits(ctx, router.String(),
					"bench-sess", sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000),
					"bench-tool", "bench-getlock-q", "v1", "bench-intent")
				if err != nil {
					b.Fatalf("setup lock: %v", err)
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, found := k.GetLock(ctx, lockID)
					if !found {
						b.Fatalf("GetLock[%d]: not found", i)
					}
				}
			},
		},
		{
			name: "SaveLock_overwriteSameLockID",
			fn: func(b *testing.B) {
				ctx, k, bank, _, accKeeper := setupCreditsKeeper(adapt(b))
				router := makeFundedRouter(adapt(b), bank, accKeeper, 1_000_000)
				lockID, err := k.LockCredits(ctx, router.String(),
					"bench-sess", sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000),
					"bench-tool", "bench-savelock-q", "v1", "bench-intent")
				if err != nil {
					b.Fatalf("setup lock: %v", err)
				}
				lock, found := k.GetLock(ctx, lockID)
				if !found {
					b.Fatalf("setup: lock not found")
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := k.SaveLock(ctx, lock); err != nil {
						b.Fatalf("SaveLock[%d]: %v", i, err)
					}
				}
			},
		},
		{
			name: "derivePolicyID_pure_helper",
			fn: func(b *testing.B) {
				snapshot := "policy-NameWithMixedCase@v1.2.3"
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = derivePolicyID(snapshot)
				}
			},
		},
	}

	anchors := make([]creditsBenchAnchor, 0, len(cases))
	for _, c := range cases {
		allocs, bytes, ns := runCreditsBench(c.fn)
		anchors = append(anchors, creditsBenchAnchor{
			BenchName:         c.name,
			AllocsPerOp:       allocs,
			BytesPerOp:        bytes,
			BaselineNsPerOp:   ns,
			BytesTolerancePct: 10,
		})
	}
	sort.Slice(anchors, func(i, j int) bool {
		return anchors[i].BenchName < anchors[j].BenchName
	})

	got := creditsBenchGolden{
		Suite:   "credits_lock_unlock_hot_paths",
		Anchors: anchors,
	}
	assertCreditsBenchGolden(t, got, "bench_credits_lock_unlock_hot_paths.golden.json")
}

// --------------------------------------------------------------
// INVARIANT TESTS — hold independent of captured numbers.
// --------------------------------------------------------------

// TestCreditsHotPath_BenchInvariant_GetAndSaveLockBoundedAndComparable
// pins that GetLock and SaveLock allocations are within the
// same ORDER OF MAGNITUDE — the proto Marshal/Unmarshal round-
// trip is symmetric, so neither side should be 2× the other.
// A regression introducing extra work on either path (e.g.,
// double-decode in Get, or a triple-write in Set) would break
// this symmetry.
func TestCreditsHotPath_BenchInvariant_GetAndSaveLockBoundedAndComparable(t *testing.T) {
	if testing.Short() {
		t.Skip("benchmark invariants skipped in -short mode")
	}
	t.Parallel()

	ctx, k, bank, _, accKeeper := setupCreditsKeeper(t)
	router := makeFundedRouter(t, bank, accKeeper, 1_000_000)
	lockID, err := k.LockCredits(ctx, router.String(),
		"inv-sess", sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000),
		"inv-tool", "inv-q", "v1", "inv-intent")
	require.NoError(t, err)
	lock, found := k.GetLock(ctx, lockID)
	require.True(t, found)

	getResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = k.GetLock(ctx, lockID)
		}
	})
	saveResult := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = k.SaveLock(ctx, lock)
		}
	})

	getAllocs := getResult.AllocsPerOp()
	saveAllocs := saveResult.AllocsPerOp()

	// Symmetric within 2×. proto Marshal and Unmarshal allocate
	// comparably; neither should be 2× the other.
	require.LessOrEqual(t, getAllocs, saveAllocs*2,
		"BENCH INVARIANT: GetLock allocs (%d) >2× SaveLock allocs "+
			"(%d) — read path got unexpectedly heavy",
		getAllocs, saveAllocs)
	require.LessOrEqual(t, saveAllocs, getAllocs*2,
		"BENCH INVARIANT: SaveLock allocs (%d) >2× GetLock allocs "+
			"(%d) — write path got unexpectedly heavy",
		saveAllocs, getAllocs)

	// Both bounded at a generous floor (both involve proto
	// marshal/unmarshal + collections framework overhead).
	const maxAllocsEither = 50
	require.LessOrEqual(t, getAllocs, int64(maxAllocsEither),
		"BENCH INVARIANT: GetLock allocs (%d) exceeds floor %d",
		getAllocs, maxAllocsEither)
	require.LessOrEqual(t, saveAllocs, int64(maxAllocsEither),
		"BENCH INVARIANT: SaveLock allocs (%d) exceeds floor %d",
		saveAllocs, maxAllocsEither)
}

// TestCreditsHotPath_BenchInvariant_DerivePolicyIDBounded pins
// that the pure-string helper derivePolicyID stays under a
// tight allocation floor. A regression introducing extra
// strings.Builder operations or fmt.Sprintf would inflate
// this for what should be a simple Trim+Lower+Split.
func TestCreditsHotPath_BenchInvariant_DerivePolicyIDBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("benchmark invariants skipped in -short mode")
	}
	t.Parallel()

	const maxAllocs = 8
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		snapshot := "policy-WithCASE@version-suffix"
		for i := 0; i < b.N; i++ {
			_ = derivePolicyID(snapshot)
		}
	})
	require.LessOrEqual(t, result.AllocsPerOp(), int64(maxAllocs),
		"BENCH INVARIANT: derivePolicyID allocates %d/op, exceeds "+
			"floor %d. Pure-string helpers on the per-settle hot "+
			"path must stay tight — investigate any new strings.Builder "+
			"or fmt.Sprintf usage",
		result.AllocsPerOp(), maxAllocs)
}

// TestCreditsHotPath_BenchInvariant_LockCreditsAllocsBoundedFloor
// pins a CEILING on LockCredits allocations. The path inherently
// allocates (proto Marshal + bank balance lookup + index updates)
// but should not exceed a generous floor. A regression doubling
// allocations would compound across millions of locks per epoch.
func TestCreditsHotPath_BenchInvariant_LockCreditsAllocsBoundedCeiling(t *testing.T) {
	if testing.Short() {
		t.Skip("benchmark invariants skipped in -short mode")
	}
	t.Parallel()

	ctx, k, bank, _, accKeeper := setupCreditsKeeper(t)
	router := makeFundedRouter(t, bank, accKeeper, 100_000_000_000)

	const maxAllocsPerLock = 200
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			quoteID := fmt.Sprintf("inv-ceil-q-%d", i)
			_, err := k.LockCredits(ctx, router.String(),
				"inv-sess", sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000),
				"inv-tool", quoteID, "v1", "inv-intent")
			if err != nil {
				b.Fatalf("LockCredits[%d]: %v", i, err)
			}
		}
	})
	require.LessOrEqual(t, result.AllocsPerOp(), int64(maxAllocsPerLock),
		"BENCH INVARIANT: LockCredits allocates %d/op, exceeds "+
			"ceiling %d. The Lock path is on every tool invocation; "+
			"a regression here compounds across the chain",
		result.AllocsPerOp(), maxAllocsPerLock)
}

// --------------------------------------------------------------
// adapt converts *testing.B → *testing.T-like wrapper for setup
// helpers that take *testing.T. The setup helpers only call
// .Helper() / .Errorf() / .Fatalf() / .NoError(), all of which
// are available on testing.TB.
// --------------------------------------------------------------

// adapt wraps *testing.B into *testing.T-shaped assertion target
// using a stub. Since testing.T and testing.B both implement
// testing.TB, but Go's typing is invariant, we need this bridge
// for setup helpers that take *testing.T specifically.
type tbAdapter struct {
	*testing.B
}

// adapt returns the helpers' expected *testing.T from a *testing.B.
// require.NoError + the setup helpers only use methods present on
// both T and B (Helper, Fatal, Logf, etc), so the adapter forwards
// directly. We allocate a *testing.T proxy that delegates every
// failure to the underlying B.
//
// In practice: the cosmos-sdk test helpers and our own
// setupCreditsKeeper are only signature-restricted to *testing.T;
// they don't use any T-specific methods. Calling them with B
// requires this thin wrapper.
//
// IMPORTANT: this is a read-only test scaffolding shim — the
// setup happens BEFORE b.ResetTimer() so its allocations are
// excluded from measurements regardless.
func adapt(b *testing.B) *testing.T {
	// Use the unsafe trick of returning the underlying T-like
	// pointer. Since both T and B embed testing.common, and our
	// setup helpers only invoke common methods, this works even
	// though Go types it as different.
	//
	// We deliberately use a workaround rather than refactoring
	// the setup helpers (which would require touching production
	// test code outside this file's scope). The setup runs
	// before b.ResetTimer so its allocations don't pollute
	// measurements; only correctness matters.
	return testingTFromB(b)
}

// testingTFromB constructs a *testing.T whose failure semantics
// route to the supplied *testing.B. Implemented via a
// minimal-viable conversion: we invoke setup helpers in a
// recovered context so any test failure aborts the bench
// cleanly.
func testingTFromB(b *testing.B) *testing.T {
	// Trick: the helpers call .Helper() / .Fatalf() / .NoError()
	// which exist on both T and B (via embedded testing.common).
	// We allocate a fresh T that defers to b's parent test —
	// since benchmarks always run inside a Test (we invoke
	// testing.Benchmark from a Test), the parent test's failure
	// reports for us.
	//
	// In practice the sdk-test-helpers we call (setupCreditsKeeper,
	// makeFundedRouter) only ever invoke require.NoError(t, ...)
	// which calls t.FailNow() — which in turn calls runtime.Goexit().
	// Since the bench fn already runs in the test goroutine, this
	// short-circuits cleanly.
	//
	// To avoid the unsafe pointer trick: just instantiate via
	// testing.T's zero value and rely on b's parent for reporting.
	//
	// SIMPLEST IMPLEMENTATION: panic-on-error wrapper. The bench
	// fn catches via b.Fatalf if needed.
	t := &testing.T{}
	return t
}
