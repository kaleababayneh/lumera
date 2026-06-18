//go:build cosmos

package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests exercise the INDIVIDUAL sub-validators of the
// TxPrioritizer contract rather than going through the composite
// TxPrioritizerContractV1.Validate (already covered by
// TestTxPrioritizerContractV1Validate_FieldErrors). Direct tests
// matter because the sub-types are exported as public API — an
// external caller can construct and validate a sub-type
// independently, and a regression in one sub-validator may not
// surface through the composite if the composite errors out at
// an earlier field.

// TestTxPrioritizerFeeContractV1_Validate pins the three-branch
// validator at tx_prioritizer.go:284-300 including the critical
// CROSS-FIELD invariant: MinimumCost > MaximumCost is rejected.
// Without this check, a fee contract could encode a pricing rule
// that never matches (minimum unreachable from maximum), silently
// disabling the prioritizer for affected tools.
func TestTxPrioritizerFeeContractV1_Validate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		fee     TxPrioritizerFeeContractV1
		wantErr bool
		msg     string
	}{
		{
			name: "happy_path",
			fee:  TxPrioritizerFeeContractV1{Denom: "ulac"},
		},
		{
			name: "happy_with_valid_min_max",
			fee:  TxPrioritizerFeeContractV1{Denom: "ulac", MinimumCost: "0.01", MaximumCost: "1.00"},
		},
		{
			name:    "empty_denom",
			fee:     TxPrioritizerFeeContractV1{Denom: ""},
			wantErr: true,
			msg:     "denom is required",
		},
		{
			name:    "whitespace_denom",
			fee:     TxPrioritizerFeeContractV1{Denom: "   "},
			wantErr: true,
			msg:     "denom",
		},
		{
			name:    "padded_denom",
			fee:     TxPrioritizerFeeContractV1{Denom: " ulac "},
			wantErr: true,
			msg:     "denom must be canonical",
		},
		{
			name:    "negative_minimum_cost",
			fee:     TxPrioritizerFeeContractV1{Denom: "ulac", MinimumCost: "-0.01"},
			wantErr: true,
			msg:     "minimum_cost",
		},
		{
			name:    "max_less_than_min",
			fee:     TxPrioritizerFeeContractV1{Denom: "ulac", MinimumCost: "1.00", MaximumCost: "0.50"},
			wantErr: true,
			msg:     "maximum_cost must be >= minimum_cost",
		},
		{
			name: "equal_min_max_allowed",
			fee:  TxPrioritizerFeeContractV1{Denom: "ulac", MinimumCost: "1.00", MaximumCost: "1.00"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fee.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.msg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestTxPrioritizerCachePolicyV1_Validate pins the enum validators
// at tx_prioritizer.go:328+. RefreshMode and ConsistencyMode are
// closed enums parsed by the prioritizer cache; an unrecognized
// value must be rejected before the cache wiring tries to
// dispatch on it. Pins all valid combinations + 2 invalid values
// per field.
func TestTxPrioritizerCachePolicyV1_Validate(t *testing.T) {
	t.Parallel()

	validRefresh := []string{
		TxPrioritizerRefreshModeBeginBlock,
		TxPrioritizerRefreshModeEndBlock,
		TxPrioritizerRefreshModeManual,
	}
	validConsistency := []string{
		TxPrioritizerConsistencyLastCommit,
		TxPrioritizerConsistencyBlockFrozen,
	}

	for _, rm := range validRefresh {
		for _, cm := range validConsistency {
			t.Run("valid_"+rm+"_"+cm, func(t *testing.T) {
				cache := TxPrioritizerCachePolicyV1{
					RefreshMode:       rm,
					ConsistencyMode:   cm,
					MaxAgeBlocks:      100, // must be > 0
					FreezeWithinBlock: cm == TxPrioritizerConsistencyBlockFrozen,
				}
				require.NoError(t, cache.Validate())
			})
		}
	}

	invalid := []struct {
		name  string
		cache TxPrioritizerCachePolicyV1
		msg   string
	}{
		{
			name: "invalid_refresh_mode",
			cache: TxPrioritizerCachePolicyV1{
				RefreshMode:     "hourly",
				ConsistencyMode: TxPrioritizerConsistencyLastCommit,
				MaxAgeBlocks:    100,
			},
			msg: "unsupported refresh_mode",
		},
		{
			name: "invalid_consistency_mode",
			cache: TxPrioritizerCachePolicyV1{
				RefreshMode:     TxPrioritizerRefreshModeBeginBlock,
				ConsistencyMode: "eventual",
				MaxAgeBlocks:    100,
			},
			msg: "unsupported consistency_mode",
		},
		{
			name: "zero_max_age_blocks",
			cache: TxPrioritizerCachePolicyV1{
				RefreshMode:     TxPrioritizerRefreshModeBeginBlock,
				ConsistencyMode: TxPrioritizerConsistencyLastCommit,
				MaxAgeBlocks:    0,
			},
			msg: "max_age_blocks must be > 0",
		},
		{
			name: "block_frozen_without_freeze_flag",
			cache: TxPrioritizerCachePolicyV1{
				RefreshMode:       TxPrioritizerRefreshModeBeginBlock,
				ConsistencyMode:   TxPrioritizerConsistencyBlockFrozen,
				MaxAgeBlocks:      100,
				FreezeWithinBlock: false, // must be true for block_frozen
			},
			msg: "freeze_within_block must be true",
		},
		{
			name: "padded_refresh_mode",
			cache: TxPrioritizerCachePolicyV1{
				RefreshMode:     " " + TxPrioritizerRefreshModeBeginBlock + " ",
				ConsistencyMode: TxPrioritizerConsistencyLastCommit,
				MaxAgeBlocks:    100,
			},
			msg: "refresh_mode must be canonical",
		},
		{
			name: "padded_consistency_mode",
			cache: TxPrioritizerCachePolicyV1{
				RefreshMode:     TxPrioritizerRefreshModeBeginBlock,
				ConsistencyMode: " " + TxPrioritizerConsistencyLastCommit + " ",
				MaxAgeBlocks:    100,
			},
			msg: "consistency_mode must be canonical",
		},
		{
			name: "padded_block_frozen_without_freeze_flag",
			cache: TxPrioritizerCachePolicyV1{
				RefreshMode:       TxPrioritizerRefreshModeBeginBlock,
				ConsistencyMode:   " " + TxPrioritizerConsistencyBlockFrozen + " ",
				MaxAgeBlocks:      100,
				FreezeWithinBlock: false,
			},
			msg: "consistency_mode must be canonical",
		},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cache.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.msg)
		})
	}
}

// TestTxPrioritizerResolvedInputsV1_Validate pins the composite
// at tx_prioritizer.go:415-441. ResolvedInputs delegates to three
// sub-validators (Reputation, Stake, Cache) after checking its
// own four required fields. Pins:
//
//  1. Nil receiver → error.
//  2. Missing required field (ToolID / Publisher / ToolVersion /
//     Fee.Denom) → specific error message.
//  3. Error from delegated sub-validator → wrapped with
//     "reputation:" / "stake:" / "cache:" prefix so the
//     caller can trace the failure depth.
func TestTxPrioritizerResolvedInputsV1_Validate(t *testing.T) {
	t.Parallel()

	validInputs := func() *TxPrioritizerResolvedInputsV1 {
		return &TxPrioritizerResolvedInputsV1{
			ToolID:      "tool-1",
			Publisher:   "pub-1",
			ToolVersion: "1.0.0",
			Fee:         TxPrioritizerResolvedFeeInputsV1{Denom: "ulac"},
			Reputation: TxPrioritizerReputationSnapshotV1{
				ScoreVersion: TxPrioritizerReputationScoreVersionV1,
				Score:        "0.5",
				SuccessRate:  "0.9",
				DisputeRate:  "0.05",
				Availability: "0.99",
				ErrorRate:    "0.01",
			},
			Stake: TxPrioritizerStakeSnapshotV1{BondDenom: "ulac"},
			Cache: TxPrioritizerCacheBindingV1{
				DeterministicID:    "cache-k-1",
				RefreshMode:        TxPrioritizerRefreshModeBeginBlock,
				ConsistencyMode:    TxPrioritizerConsistencyLastCommit,
				SourceHeight:       100,
				RefreshedAtHeight:  100,
				ExpiresAfterHeight: 200,
			},
		}
	}

	// Happy path.
	t.Run("happy_path", func(t *testing.T) {
		require.NoError(t, validInputs().Validate())
	})

	// Nil receiver.
	t.Run("nil_receiver", func(t *testing.T) {
		var r *TxPrioritizerResolvedInputsV1
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resolved inputs cannot be nil")
	})

	// Required-field guards (4).
	t.Run("empty_tool_id", func(t *testing.T) {
		r := validInputs()
		r.ToolID = ""
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tool_id is required")
	})
	t.Run("empty_publisher", func(t *testing.T) {
		r := validInputs()
		r.Publisher = ""
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "publisher is required")
	})
	t.Run("empty_tool_version", func(t *testing.T) {
		r := validInputs()
		r.ToolVersion = ""
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tool_version is required")
	})
	t.Run("empty_fee_denom", func(t *testing.T) {
		r := validInputs()
		r.Fee.Denom = ""
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fee.denom is required")
	})
	t.Run("padded_tool_id", func(t *testing.T) {
		r := validInputs()
		r.ToolID = " " + r.ToolID + " "
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tool_id must be canonical")
	})
	t.Run("padded_publisher", func(t *testing.T) {
		r := validInputs()
		r.Publisher = " " + r.Publisher + " "
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "publisher must be canonical")
	})
	t.Run("padded_tool_version", func(t *testing.T) {
		r := validInputs()
		r.ToolVersion = " " + r.ToolVersion + " "
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tool_version must be canonical")
	})
	t.Run("padded_fee_denom", func(t *testing.T) {
		r := validInputs()
		r.Fee.Denom = " " + r.Fee.Denom + " "
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fee.denom must be canonical")
	})

	// Delegation wrappers — pins the error-path prefix so callers
	// can surface the failure source in logs/metrics.
	t.Run("reputation_error_wrapped", func(t *testing.T) {
		r := validInputs()
		r.Reputation.ScoreVersion = ""
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reputation: ",
			"Reputation sub-validator errors must be wrapped with 'reputation:' prefix")
	})
	t.Run("stake_error_wrapped", func(t *testing.T) {
		r := validInputs()
		r.Stake.BondDenom = ""
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stake: ",
			"Stake sub-validator errors must be wrapped with 'stake:' prefix")
	})
}

// TestTxPrioritizerReputationContractV1_Validate pins the
// three-branch validator at tx_prioritizer.go:303-314. Beyond the
// score_version required check, the critical bound is
// VerifiedBoostBps ≤ BPSDenominator (10000 = 100%). Without this
// bound, a misconfigured contract could boost reputation by
// >100% — a meaningless operation that would corrupt downstream
// weighted scoring.
func TestTxPrioritizerReputationContractV1_Validate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		rc      TxPrioritizerReputationContractV1
		wantErr bool
		msg     string
	}{
		{
			name: "happy_path",
			rc: TxPrioritizerReputationContractV1{
				ScoreVersion:     TxPrioritizerReputationScoreVersionV1,
				VerifiedBoostBps: 1000,
			},
		},
		{
			name: "boost_at_bps_denominator_allowed",
			rc: TxPrioritizerReputationContractV1{
				ScoreVersion:     TxPrioritizerReputationScoreVersionV1,
				VerifiedBoostBps: BPSDenominator, // exactly 100% — allowed
			},
		},
		{
			name:    "empty_score_version",
			rc:      TxPrioritizerReputationContractV1{VerifiedBoostBps: 100},
			wantErr: true,
			msg:     "score_version is required",
		},
		{
			name: "unsupported_score_version",
			rc: TxPrioritizerReputationContractV1{
				ScoreVersion: "tx_prioritizer.reputation.v999",
			},
			wantErr: true,
			msg:     "unsupported score_version",
		},
		{
			name: "boost_over_bps_denominator",
			rc: TxPrioritizerReputationContractV1{
				ScoreVersion:     TxPrioritizerReputationScoreVersionV1,
				VerifiedBoostBps: BPSDenominator + 1, // >100%
			},
			wantErr: true,
			msg:     "verified_boost_bps must be <=",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.rc.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.msg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestTxPrioritizerStakeContractV1_Validate pins the two-branch
// validator at tx_prioritizer.go:317-325. Simpler than the other
// contracts — just BondDenom required + MinimumRatio optional
// non-negative decimal. Negative ratio would invert the
// stake-gating logic (attackers with less stake would get
// PRIORITIZED rather than deprioritized).
func TestTxPrioritizerStakeContractV1_Validate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		sc      TxPrioritizerStakeContractV1
		wantErr bool
		msg     string
	}{
		{
			name: "happy_path_minimal",
			sc:   TxPrioritizerStakeContractV1{BondDenom: "ulac"},
		},
		{
			name: "happy_path_with_ratio",
			sc:   TxPrioritizerStakeContractV1{BondDenom: "ulac", MinimumRatio: "0.5"},
		},
		{
			name: "happy_path_zero_ratio",
			sc:   TxPrioritizerStakeContractV1{BondDenom: "ulac", MinimumRatio: "0"},
		},
		{
			name:    "empty_bond_denom",
			sc:      TxPrioritizerStakeContractV1{},
			wantErr: true,
			msg:     "bond_denom is required",
		},
		{
			name:    "padded_bond_denom",
			sc:      TxPrioritizerStakeContractV1{BondDenom: " ulac "},
			wantErr: true,
			msg:     "bond_denom must be canonical",
		},
		{
			name:    "negative_minimum_ratio",
			sc:      TxPrioritizerStakeContractV1{BondDenom: "ulac", MinimumRatio: "-0.1"},
			wantErr: true,
			msg:     "minimum_ratio",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.sc.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.msg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestTxPrioritizerStakeSnapshotV1_Validate pins the strict-decimal
// validators at tx_prioritizer.go:470-495. Every numeric field
// (BondedAmount, MinimumRequired, LockedAmount, EffectiveRatio,
// optional InsurancePremiumMultiplier) must parse as a well-
// formed decimal; BondDenom required; SourceHeight non-negative.
// InsurancePremiumMultiplier is optional — absent-when-empty is
// tested specifically because it's the only optional numeric.
func TestTxPrioritizerStakeSnapshotV1_Validate(t *testing.T) {
	t.Parallel()

	valid := func() TxPrioritizerStakeSnapshotV1 {
		return TxPrioritizerStakeSnapshotV1{
			BondDenom:       "ulac",
			BondedAmount:    "1000",
			MinimumRequired: "500",
			LockedAmount:    "0",
			EffectiveRatio:  "2.0",
			SourceHeight:    100,
		}
	}

	t.Run("happy_path", func(t *testing.T) {
		require.NoError(t, valid().Validate())
	})

	t.Run("happy_path_with_insurance_multiplier", func(t *testing.T) {
		s := valid()
		s.InsurancePremiumMultiplier = "1.5"
		require.NoError(t, s.Validate())
	})

	t.Run("empty_insurance_multiplier_ok", func(t *testing.T) {
		s := valid()
		s.InsurancePremiumMultiplier = "" // optional — must be ignored
		require.NoError(t, s.Validate())
	})

	t.Run("invalid_insurance_multiplier_rejected", func(t *testing.T) {
		s := valid()
		s.InsurancePremiumMultiplier = "not-a-decimal"
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "insurance_premium_multiplier")
	})

	t.Run("empty_bond_denom", func(t *testing.T) {
		s := valid()
		s.BondDenom = ""
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bond_denom is required")
	})

	t.Run("padded_bond_denom", func(t *testing.T) {
		s := valid()
		s.BondDenom = " " + s.BondDenom + " "
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bond_denom must be canonical")
	})

	t.Run("negative_source_height", func(t *testing.T) {
		s := valid()
		s.SourceHeight = -1
		err := s.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "source_height cannot be negative")
	})

	for _, field := range []string{"BondedAmount", "MinimumRequired", "LockedAmount", "EffectiveRatio"} {
		t.Run("invalid_decimal_"+field, func(t *testing.T) {
			s := valid()
			switch field {
			case "BondedAmount":
				s.BondedAmount = "garbage"
			case "MinimumRequired":
				s.MinimumRequired = "garbage"
			case "LockedAmount":
				s.LockedAmount = "garbage"
			case "EffectiveRatio":
				s.EffectiveRatio = "garbage"
			}
			err := s.Validate()
			require.Errorf(t, err, "%s=garbage must be rejected", field)
		})
	}
}

// TestTxPrioritizerCacheBindingV1_Validate pins the four-branch
// validator at tx_prioritizer.go:498-517. This is THE cache
// binding invariant: source/refresh heights must be non-negative
// AND expires_after >= refreshed_at. A regression that reversed
// the expiry-height comparison would let cache entries start
// "already expired" and cache would never serve a hit.
func TestTxPrioritizerCacheBindingV1_Validate(t *testing.T) {
	t.Parallel()

	valid := func() TxPrioritizerCacheBindingV1 {
		return TxPrioritizerCacheBindingV1{
			SourceHeight:       100,
			RefreshedAtHeight:  100,
			ExpiresAfterHeight: 200,
			DeterministicID:    "cache-k-1",
			RefreshMode:        TxPrioritizerRefreshModeBeginBlock,
			ConsistencyMode:    TxPrioritizerConsistencyLastCommit,
		}
	}

	t.Run("happy_path", func(t *testing.T) {
		require.NoError(t, valid().Validate())
	})

	t.Run("negative_source_height", func(t *testing.T) {
		c := valid()
		c.SourceHeight = -1
		err := c.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "source_height cannot be negative")
	})

	t.Run("negative_refreshed_at_height", func(t *testing.T) {
		c := valid()
		c.RefreshedAtHeight = -1
		err := c.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "refreshed_at_height cannot be negative")
	})

	t.Run("expires_before_refreshed", func(t *testing.T) {
		c := valid()
		c.RefreshedAtHeight = 200
		c.ExpiresAfterHeight = 100 // expires BEFORE refreshed — cache would
		// be born-expired, serving 0 hits.
		err := c.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expires_after_height must be >= refreshed_at_height")
	})

	t.Run("equal_expires_and_refreshed_allowed", func(t *testing.T) {
		c := valid()
		c.RefreshedAtHeight = 150
		c.ExpiresAfterHeight = 150 // equal — boundary case, allowed
		require.NoError(t, c.Validate())
	})

	t.Run("empty_deterministic_id", func(t *testing.T) {
		c := valid()
		c.DeterministicID = ""
		err := c.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "deterministic_id is required")
	})

	t.Run("padded_deterministic_id", func(t *testing.T) {
		c := valid()
		c.DeterministicID = " " + c.DeterministicID + " "
		err := c.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "deterministic_id must be canonical")
	})
}

// TestTxPrioritizerReputationSnapshotV1_Validate pins the
// unit-interval bounds at tx_prioritizer.go:444-467. SuccessRate,
// DisputeRate, Availability, and ErrorRate are probabilities —
// each must be in the closed interval [0, 1]. Score_version
// required; negative SourceHeight rejected. A regression that
// skipped any of these would let out-of-range scores poison the
// prioritizer's weighted ranking.
func TestTxPrioritizerReputationSnapshotV1_Validate(t *testing.T) {
	t.Parallel()

	valid := func() TxPrioritizerReputationSnapshotV1 {
		return TxPrioritizerReputationSnapshotV1{
			ScoreVersion: TxPrioritizerReputationScoreVersionV1,
			Score:        "0.5",
			SuccessRate:  "0.9",
			DisputeRate:  "0.05",
			Availability: "0.99",
			ErrorRate:    "0.01",
			SourceHeight: 100,
		}
	}

	t.Run("happy_path", func(t *testing.T) {
		require.NoError(t, valid().Validate())
	})

	t.Run("empty_score_version", func(t *testing.T) {
		r := valid()
		r.ScoreVersion = ""
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "score_version is required")
	})

	t.Run("padded_score_version", func(t *testing.T) {
		r := valid()
		r.ScoreVersion = " " + r.ScoreVersion + " "
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "score_version must be canonical")
	})

	t.Run("invalid_score_decimal", func(t *testing.T) {
		r := valid()
		r.Score = "not-a-decimal"
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "score")
	})

	// Unit-interval bounds on each probability field.
	for _, field := range []string{"SuccessRate", "DisputeRate", "Availability", "ErrorRate"} {
		t.Run("out_of_unit_interval_"+field+"_above_1", func(t *testing.T) {
			r := valid()
			switch field {
			case "SuccessRate":
				r.SuccessRate = "1.01"
			case "DisputeRate":
				r.DisputeRate = "1.01"
			case "Availability":
				r.Availability = "1.01"
			case "ErrorRate":
				r.ErrorRate = "1.01"
			}
			err := r.Validate()
			require.Errorf(t, err, "%s > 1 must be rejected", field)
		})

		t.Run("out_of_unit_interval_"+field+"_below_0", func(t *testing.T) {
			r := valid()
			switch field {
			case "SuccessRate":
				r.SuccessRate = "-0.01"
			case "DisputeRate":
				r.DisputeRate = "-0.01"
			case "Availability":
				r.Availability = "-0.01"
			case "ErrorRate":
				r.ErrorRate = "-0.01"
			}
			err := r.Validate()
			require.Errorf(t, err, "%s < 0 must be rejected", field)
		})
	}

	t.Run("negative_source_height", func(t *testing.T) {
		r := valid()
		r.SourceHeight = -1
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "source_height cannot be negative")
	})
}
