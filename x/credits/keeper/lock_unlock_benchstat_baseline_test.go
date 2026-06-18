
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

// This file extends tick 70's bench-anchored golden with a
// BENCHSTAT-STYLE STATISTICAL BASELINE. Where tick 70 captured
// a single bench result per anchor (susceptible to b.N-driven
// variance for stateful benchmarks), this captures 10
// iterations per anchor and freezes the MEDIAN + range.
//
// Pattern:
//   for each hot path:
//     run testing.Benchmark 10 times
//     collect all (allocs, bytes, ns) tuples
//     compute median, p25, p75 — capture as the baseline
//     assert future runs' median falls within [p25-tol, p75+tol]
//
// Why median instead of mean: median is robust to outliers
// (CI hiccups, GC pause artifacts). Why p25/p75 band: catches
// regressions that shift the DISTRIBUTION (not just the mean),
// e.g. a refactor that adds a rare-but-large allocation path.
//
// Companion to tick 70 — both files run side-by-side. Tick 70
// catches DETERMINISTIC regressions (every-call alloc deltas);
// this catches DISTRIBUTIONAL regressions (variance shifts,
// occasional-spike regressions).

// --------------------------------------------------------------
// Benchstat baseline structure
// --------------------------------------------------------------

type benchStatBaseline struct {
	BenchName string `json:"bench_name"`
	NumRuns   int    `json:"num_runs"`

	// Allocation distribution across runs.
	AllocsP25    int64 `json:"allocs_p25"`
	AllocsMedian int64 `json:"allocs_median"`
	AllocsP75    int64 `json:"allocs_p75"`

	// Bytes distribution.
	BytesP25    int64 `json:"bytes_p25"`
	BytesMedian int64 `json:"bytes_median"`
	BytesP75    int64 `json:"bytes_p75"`

	// Ns distribution (informational only).
	NsMedian int64 `json:"ns_median"`
}

type benchStatGolden struct {
	Suite         string              `json:"suite"`
	RunsPerBench  int                 `json:"runs_per_bench"`
	Baselines     []benchStatBaseline `json:"baselines"`
}

// runMultiBench runs the bench function `runs` times and returns
// the collected (allocs, bytes, ns) tuples.
func runMultiBench(runs int, fn func(b *testing.B)) (allocs, bytesAlloc, ns []int64) {
	allocs = make([]int64, runs)
	bytesAlloc = make([]int64, runs)
	ns = make([]int64, runs)
	for i := 0; i < runs; i++ {
		result := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			fn(b)
		})
		allocs[i] = result.AllocsPerOp()
		bytesAlloc[i] = result.AllocedBytesPerOp()
		ns[i] = result.NsPerOp()
	}
	return allocs, bytesAlloc, ns
}

// percentile returns the value at the given percentile (0-100).
// Uses linear interpolation; copies+sorts the input so caller
// retains original.
func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := p / 100 * float64(len(sorted)-1)
	lower := int(rank)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	weight := rank - float64(lower)
	low := float64(sorted[lower])
	high := float64(sorted[upper])
	return int64(low + weight*(high-low))
}

func computeBaseline(name string, runs int, allocs, bytesAlloc, ns []int64) benchStatBaseline {
	allocsCopy := append([]int64{}, allocs...)
	sort.Slice(allocsCopy, func(i, j int) bool { return allocsCopy[i] < allocsCopy[j] })
	bytesCopy := append([]int64{}, bytesAlloc...)
	sort.Slice(bytesCopy, func(i, j int) bool { return bytesCopy[i] < bytesCopy[j] })
	nsCopy := append([]int64{}, ns...)
	sort.Slice(nsCopy, func(i, j int) bool { return nsCopy[i] < nsCopy[j] })

	return benchStatBaseline{
		BenchName:    name,
		NumRuns:      runs,
		AllocsP25:    percentile(allocsCopy, 25),
		AllocsMedian: percentile(allocsCopy, 50),
		AllocsP75:    percentile(allocsCopy, 75),
		BytesP25:     percentile(bytesCopy, 25),
		BytesMedian:  percentile(bytesCopy, 50),
		BytesP75:     percentile(bytesCopy, 75),
		NsMedian:     percentile(nsCopy, 50),
	}
}

// assertBenchStatGolden compares a captured baseline distribution
// against the golden. Asserts that the current MEDIAN falls
// within [golden_p25 - tol, golden_p75 + tol] for both allocs
// and bytes — wider band than tick 70's point comparison,
// reflecting the distributional view.
func assertBenchStatGolden(t *testing.T, got benchStatGolden, goldenFile string) {
	t.Helper()
	gotBytes, err := json.MarshalIndent(got, "", "  ")
	require.NoError(t, err)

	path := filepath.Join("testdata", goldenFile)
	if os.Getenv("UPDATE_GOLDENS") == "1" {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, gotBytes, 0o644))
		t.Logf("[GOLDEN] wrote %s (%d bytes) — REVIEW the captured "+
			"distribution percentiles before committing",
			path, len(gotBytes))
		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err,
		"read golden %s — run with UPDATE_GOLDENS=1 to capture",
		path)

	var wantParsed benchStatGolden
	require.NoError(t, json.Unmarshal(want, &wantParsed))

	require.Equal(t, wantParsed.Suite, got.Suite)
	require.Equal(t, wantParsed.RunsPerBench, got.RunsPerBench)
	require.Equal(t, len(wantParsed.Baselines), len(got.Baselines))

	wantByName := map[string]benchStatBaseline{}
	for _, b := range wantParsed.Baselines {
		wantByName[b.BenchName] = b
	}

	for _, gotBL := range got.Baselines {
		wantBL, ok := wantByName[gotBL.BenchName]
		require.True(t, ok,
			"benchmark %q not in golden baseline", gotBL.BenchName)

		// Allocs band: gotMedian must fall within [wantP25-tol, wantP75+tol].
		// allocTol absolute = max(2, ceil(0.05 × wantMedian)) — 5%
		// of the median or 2, whichever is larger.
		allocTol := wantBL.AllocsMedian / 20 // 5%
		if allocTol < 2 {
			allocTol = 2
		}
		allocLower := wantBL.AllocsP25 - allocTol
		allocUpper := wantBL.AllocsP75 + allocTol
		require.GreaterOrEqual(t, gotBL.AllocsMedian, allocLower,
			"BENCHSTAT REGRESSION %q: got_median_allocs=%d below band "+
				"[%d, %d] (golden p25=%d p75=%d, tol=%d). Distribution "+
				"shifted DOWN — possibly an allocation got ELIDED, "+
				"which may indicate a refactor that needs review.",
			gotBL.BenchName, gotBL.AllocsMedian, allocLower, allocUpper,
			wantBL.AllocsP25, wantBL.AllocsP75, allocTol)
		require.LessOrEqual(t, gotBL.AllocsMedian, allocUpper,
			"BENCHSTAT REGRESSION %q: got_median_allocs=%d above band "+
				"[%d, %d] (golden p25=%d p75=%d, tol=%d). Distribution "+
				"shifted UP — a regression added allocation(s).",
			gotBL.BenchName, gotBL.AllocsMedian, allocLower, allocUpper,
			wantBL.AllocsP25, wantBL.AllocsP75, allocTol)

		// Bytes band: 10% tolerance.
		bytesTol := wantBL.BytesMedian / 10
		if bytesTol < 16 {
			bytesTol = 16
		}
		bytesLower := wantBL.BytesP25 - bytesTol
		bytesUpper := wantBL.BytesP75 + bytesTol
		require.GreaterOrEqual(t, gotBL.BytesMedian, bytesLower,
			"BENCHSTAT REGRESSION %q: bytes_median=%d below band "+
				"[%d, %d]", gotBL.BenchName, gotBL.BytesMedian,
			bytesLower, bytesUpper)
		require.LessOrEqual(t, gotBL.BytesMedian, bytesUpper,
			"BENCHSTAT REGRESSION %q: bytes_median=%d above band "+
				"[%d, %d]", gotBL.BenchName, gotBL.BytesMedian,
			bytesLower, bytesUpper)

		t.Logf("[benchstat] %s: allocs_median=%d (golden p25=%d p75=%d), "+
			"bytes_median=%d (golden p25=%d p75=%d), ns_median≈%d",
			gotBL.BenchName,
			gotBL.AllocsMedian, wantBL.AllocsP25, wantBL.AllocsP75,
			gotBL.BytesMedian, wantBL.BytesP25, wantBL.BytesP75,
			gotBL.NsMedian)
	}
}

// --------------------------------------------------------------
// THE 10-RUN BENCHSTAT BASELINE SUITE
// --------------------------------------------------------------

// TestCreditsLockUnlockHotPath_BenchStatBaselineGolden runs each
// hot path 10 times via testing.Benchmark, captures the
// distribution (p25/median/p75), and asserts the current median
// stays within the golden's [p25, p75] band ± tolerance.
//
// Complement to tick 70's single-run anchor: that pins
// deterministic per-call cost; this pins the DISTRIBUTION
// shape, catching variance regressions (e.g. occasional GC-
// triggered allocation spikes) that a single run would miss.
func TestCreditsLockUnlockHotPath_BenchStatBaselineGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("benchstat baseline skipped in -short mode")
	}
	t.Parallel()

	const runsPerBench = 10

	cases := []struct {
		name string
		fn   func(b *testing.B)
	}{
		{
			name: "LockCredits_freshLock",
			fn: func(b *testing.B) {
				ctx, k, bank, _, accKeeper := setupCreditsKeeper(adapt(b))
				const fund int64 = 100_000_000_000
				router := makeFundedRouter(adapt(b), bank, accKeeper, fund)
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					quoteID := fmt.Sprintf("bs-q-%d", i)
					_, err := k.LockCredits(ctx, router.String(),
						"bs-sess", sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000),
						"bs-tool", quoteID, "v1", "bs-intent")
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
					quoteID := fmt.Sprintf("bs-u-q-%d", i)
					id, err := k.LockCredits(ctx, router.String(),
						"bs-sess", sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000),
						"bs-tool", quoteID, "v1", "bs-intent")
					if err != nil {
						b.Fatalf("setup lock[%d]: %v", i, err)
					}
					lockIDs[i] = id
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := k.UnlockCredits(ctx, lockIDs[i], "bs-unlock"); err != nil {
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
					"bs-sess", sdk.NewInt64Coin(types.DefaultCreditDenom, 1_000),
					"bs-tool", "bs-getlock-q", "v1", "bs-intent")
				if err != nil {
					b.Fatalf("setup lock: %v", err)
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, _ = k.GetLock(ctx, lockID)
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

	baselines := make([]benchStatBaseline, 0, len(cases))
	for _, c := range cases {
		allocs, bytesAlloc, ns := runMultiBench(runsPerBench, c.fn)
		baselines = append(baselines, computeBaseline(c.name, runsPerBench,
			allocs, bytesAlloc, ns))
	}
	sort.Slice(baselines, func(i, j int) bool {
		return baselines[i].BenchName < baselines[j].BenchName
	})

	got := benchStatGolden{
		Suite:        "credits_lock_unlock_benchstat",
		RunsPerBench: runsPerBench,
		Baselines:    baselines,
	}
	assertBenchStatGolden(t, got, "benchstat_credits_lock_unlock_baseline.golden.json")
}

// --------------------------------------------------------------
// Distribution-shape invariant: p75-p25 spread should be
// SMALL relative to median. Catches regressions that introduce
// high-variance behavior (e.g., GC-triggered allocation paths
// firing intermittently).
// --------------------------------------------------------------

// TestCreditsHotPath_BenchStat_DistributionSpreadBounded asserts
// that for every captured baseline, the (p75 - p25) interquartile
// range is at most 30%% of the median. A wider IQR indicates
// high run-to-run variance — typically a sign that the path
// has a rare-but-expensive code branch firing intermittently.
func TestCreditsHotPath_BenchStat_DistributionSpreadBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("benchstat invariants skipped in -short mode")
	}
	t.Parallel()

	bz, err := os.ReadFile(filepath.Join("testdata",
		"benchstat_credits_lock_unlock_baseline.golden.json"))
	require.NoError(t, err,
		"baseline golden missing — run TestCreditsLockUnlockHotPath_BenchStatBaselineGolden "+
			"with UPDATE_GOLDENS=1 first")

	var g benchStatGolden
	require.NoError(t, json.Unmarshal(bz, &g))

	for _, bl := range g.Baselines {
		// Allocations IQR vs median. Tight floor: 30% of median
		// or 4 absolute, whichever is larger (small medians
		// naturally have wide percentage spread).
		allocsIQR := bl.AllocsP75 - bl.AllocsP25
		allocsThreshold := bl.AllocsMedian * 30 / 100
		if allocsThreshold < 4 {
			allocsThreshold = 4
		}
		require.LessOrEqual(t, allocsIQR, allocsThreshold,
			"BENCHSTAT INVARIANT %q: allocs IQR=%d > threshold %d "+
				"(median=%d). High run-to-run alloc variance suggests "+
				"a code branch fires intermittently — investigate "+
				"the path for rare-but-expensive operations",
			bl.BenchName, allocsIQR, allocsThreshold, bl.AllocsMedian)

		// Bytes IQR vs median. 30% bound; 64-byte absolute floor.
		bytesIQR := bl.BytesP75 - bl.BytesP25
		bytesThreshold := bl.BytesMedian * 30 / 100
		if bytesThreshold < 64 {
			bytesThreshold = 64
		}
		require.LessOrEqual(t, bytesIQR, bytesThreshold,
			"BENCHSTAT INVARIANT %q: bytes IQR=%d > threshold %d "+
				"(median=%d). High byte variance suggests an "+
				"intermittent allocation path",
			bl.BenchName, bytesIQR, bytesThreshold, bl.BytesMedian)
	}
}
