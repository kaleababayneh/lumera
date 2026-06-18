
package types

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func TestEvaluateTier_StartsAndCompletesPromotionLockup(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	state := TierState{
		CurrentTier:   TierProbationary,
		TierEnteredAt: now.Add(-48 * time.Hour),
	}

	start := EvaluateTier(TierEvaluationInput{
		Reputation:   standardReputation(),
		ReceiptCount: 60,
		Now:          now,
		State:        state,
	})
	if start.CurrentTier != TierProbationary {
		t.Fatalf("current tier = %v, want probationary", start.CurrentTier)
	}
	if start.PromotionBlocker != TierBlockerPromotionLockup {
		t.Fatalf("promotion blocker = %q, want %q", start.PromotionBlocker, TierBlockerPromotionLockup)
	}
	if start.NextState.PromotionPendingTo != TierStandard {
		t.Fatalf("pending target = %v, want standard", start.NextState.PromotionPendingTo)
	}
	if !start.NextState.PromotionStartedAt.Equal(now) {
		t.Fatalf("promotion start = %s, want %s", start.NextState.PromotionStartedAt, now)
	}

	done := EvaluateTier(TierEvaluationInput{
		Reputation:   standardReputation(),
		ReceiptCount: 60,
		Now:          now.Add(24 * time.Hour),
		State:        start.NextState,
	})
	if !done.Promoted || done.CurrentTier != TierStandard {
		t.Fatalf("promotion result promoted=%t current=%v, want promoted standard", done.Promoted, done.CurrentTier)
	}
	if done.NextState.PromotionPendingTo != TierProbationary || !done.NextState.PromotionStartedAt.IsZero() {
		t.Fatalf("promotion state was not cleared: %+v", done.NextState)
	}
}

func TestEvaluateTier_LockupResetsWhenEligibilityDrops(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	state := TierState{
		CurrentTier:        TierStandard,
		TierEnteredAt:      now.Add(-10 * 24 * time.Hour),
		PromotionPendingTo: TierTrusted,
		PromotionStartedAt: now.Add(-6 * 24 * time.Hour),
	}
	regressed := trustedReputation()
	regressed.Safety = 0.79

	result := EvaluateTier(TierEvaluationInput{
		Reputation:   regressed,
		ReceiptCount: 60,
		Now:          now,
		State:        state,
	})
	if result.CurrentTier != TierStandard {
		t.Fatalf("current tier = %v, want standard", result.CurrentTier)
	}
	if result.PromotionBlocker != TierBlockerMinSafety {
		t.Fatalf("promotion blocker = %q, want %q", result.PromotionBlocker, TierBlockerMinSafety)
	}
	if result.NextState.PromotionPendingTo != TierProbationary || !result.NextState.PromotionStartedAt.IsZero() {
		t.Fatalf("lockup should reset on eligibility drop: %+v", result.NextState)
	}
}

func TestEvaluateTier_DemotesImmediatelyBelowCurrentFloor(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	state := TierState{
		CurrentTier:   TierTrusted,
		TierEnteredAt: now.Add(-20 * 24 * time.Hour),
	}

	result := EvaluateTier(TierEvaluationInput{
		Reputation:   standardReputation(),
		ReceiptCount: 60,
		Now:          now,
		State:        state,
	})
	if !result.Demoted || result.CurrentTier != TierStandard {
		t.Fatalf("demotion result demoted=%t current=%v, want demoted standard", result.Demoted, result.CurrentTier)
	}
	if result.PromotionBlocker != TierBlockerMinReliability {
		t.Fatalf("demotion reason = %q, want %q", result.PromotionBlocker, TierBlockerMinReliability)
	}
	if !result.NextState.TierEnteredAt.Equal(now) {
		t.Fatalf("tier_entered_at = %s, want %s", result.NextState.TierEnteredAt, now)
	}
}

func TestEvaluateTier_DisputeBreachDemotesWithGovernanceThreshold(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	defs := DefaultTierDefinitions()
	defs[TierTrusted].MaxDisputeRate = 0.05
	state := TierState{
		CurrentTier:   TierTrusted,
		TierEnteredAt: now.Add(-20 * 24 * time.Hour),
	}

	result := EvaluateTier(TierEvaluationInput{
		Reputation:     trustedReputation(),
		ReceiptCount:   60,
		DisputeRate30d: 0.10,
		Now:            now,
		State:          state,
		Definitions:    defs,
	})
	if !result.Demoted || result.CurrentTier != TierStandard {
		t.Fatalf("demotion result demoted=%t current=%v, want demoted standard", result.Demoted, result.CurrentTier)
	}
	if result.PromotionBlocker != TierBlockerDisputeRate {
		t.Fatalf("demotion reason = %q, want %q", result.PromotionBlocker, TierBlockerDisputeRate)
	}
}

func TestEvaluateTier_PromotesOneStepEvenWhenEligibleForPremium(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	result := EvaluateTier(TierEvaluationInput{
		Reputation:   premiumReputation(),
		ReceiptCount: 60,
		Now:          now,
		State: TierState{
			CurrentTier:   TierProbationary,
			TierEnteredAt: now.Add(-48 * time.Hour),
		},
	})
	if result.EligibleTier != TierPremium {
		t.Fatalf("eligible tier = %v, want premium", result.EligibleTier)
	}
	if result.CurrentTier != TierProbationary || result.NextState.PromotionPendingTo != TierStandard {
		t.Fatalf("promotion must advance one step: current=%v next=%+v", result.CurrentTier, result.NextState)
	}
}

func TestEvaluateTier_SlashDemotesOneTierWithoutLockup(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	result := EvaluateTier(TierEvaluationInput{
		Reputation:   premiumReputation(),
		ReceiptCount: 60,
		Slashed:      true,
		Now:          now,
		State: TierState{
			CurrentTier:        TierPremium,
			TierEnteredAt:      now.Add(-40 * 24 * time.Hour),
			PromotionPendingTo: TierPremium,
			PromotionStartedAt: now.Add(-24 * time.Hour),
		},
	})
	if !result.Demoted || result.CurrentTier != TierTrusted {
		t.Fatalf("slash demotion demoted=%t current=%v, want trusted", result.Demoted, result.CurrentTier)
	}
	if result.PromotionBlocker != TierBlockerSlash {
		t.Fatalf("slash blocker = %q, want %q", result.PromotionBlocker, TierBlockerSlash)
	}
	if result.NextState.PromotionPendingTo != TierProbationary || !result.NextState.PromotionStartedAt.IsZero() {
		t.Fatalf("slash should clear pending promotion: %+v", result.NextState)
	}
}

func TestEvaluateTier_MetamorphicReplayDeterministic(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()
	events := []struct {
		at         time.Time
		reputation *ReputationResult
		receipts   uint64
	}{
		{at: start, reputation: standardReputation(), receipts: 60},
		{at: start.Add(12 * time.Hour), reputation: standardReputation(), receipts: 60},
		{at: start.Add(24 * time.Hour), reputation: standardReputation(), receipts: 60},
		{at: start.Add(25 * time.Hour), reputation: trustedReputation(), receipts: 60},
		{at: start.Add(8*24*time.Hour + time.Hour), reputation: trustedReputation(), receipts: 60},
	}
	replay := func() []TierLevel {
		state := TierState{CurrentTier: TierProbationary, TierEnteredAt: start.Add(-24 * time.Hour)}
		history := make([]TierLevel, 0, len(events))
		for _, event := range events {
			out := EvaluateTier(TierEvaluationInput{
				Reputation:   event.reputation,
				ReceiptCount: event.receipts,
				Now:          event.at,
				State:        state,
			})
			history = append(history, out.CurrentTier)
			state = out.NextState
		}
		return history
	}

	first := replay()
	second := replay()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("replay histories differ: %v != %v", first, second)
	}
	want := []TierLevel{TierProbationary, TierProbationary, TierStandard, TierStandard, TierTrusted}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("replay history = %v, want %v", first, want)
	}
}

func TestPassportAuthor_ConformanceScore_ComputedFromReceipts(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	cfg := DefaultScoringConfig()
	outcomes := []WorkflowOutcomeInput{
		{WorkflowID: "wf-a", Outcome: "FINALIZED", SLOConformant: true, Timestamp: now, EconomicWeight: 9},
		{WorkflowID: "wf-b", Outcome: "REVERTED", SLOConformant: true, Timestamp: now, EconomicWeight: 4},
		{WorkflowID: "wf-c", Outcome: "FINALIZED", SLOConformant: false, Timestamp: now, EconomicWeight: 1},
	}

	got := ComputeWorkflowConformance(cfg, outcomes, now)
	want := math.Sqrt(9) / (math.Sqrt(9) + math.Sqrt(4) + math.Sqrt(1))
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("workflow conformance = %v, want %v", got, want)
	}
}

func TestPassportAuthor_TierDrop_RaisesMinBond(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	result := EvaluateWorkflowAuthorTier(WorkflowAuthorTierEvaluationInput{
		TierInput: TierEvaluationInput{
			Reputation:   premiumReputation(),
			ReceiptCount: 100,
			Now:          now,
			State:        TierState{CurrentTier: TierPremium, TierEnteredAt: now.Add(-60 * 24 * time.Hour)},
			Definitions:  DefaultTierDefinitions(),
		},
		Outcomes: []WorkflowOutcomeInput{
			{WorkflowID: "wf-a", Outcome: "REVERTED", SLOConformant: false, Timestamp: now, EconomicWeight: 10},
			{WorkflowID: "wf-b", Outcome: "PARTIAL_SKIP", SLOConformant: true, Timestamp: now, EconomicWeight: 10},
		},
		Policy: WorkflowAuthorPolicy{
			ConformanceWeightBPS: 5000,
			MinBondPenaltyBPS:    5000,
			DefaultConformance:   1,
		},
		BaseMinBondLAC: 1_000,
	})

	if !result.Tier.Demoted || result.Tier.CurrentTier != TierStandard {
		t.Fatalf("tier result demoted=%t current=%v, want demoted standard", result.Tier.Demoted, result.Tier.CurrentTier)
	}
	if result.MinBondMultiplierBPS != 15_000 {
		t.Fatalf("min-bond multiplier = %d, want 15000", result.MinBondMultiplierBPS)
	}
	if result.MinBondLAC != 1_500 {
		t.Fatalf("min bond = %d, want 1500", result.MinBondLAC)
	}
}

func TestPassportAuthor_DeterministicTransitions(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	input := WorkflowAuthorTierEvaluationInput{
		TierInput: TierEvaluationInput{
			Reputation:   trustedReputation(),
			ReceiptCount: 100,
			Now:          now,
			State:        TierState{CurrentTier: TierTrusted, TierEnteredAt: now.Add(-30 * 24 * time.Hour)},
			Definitions:  DefaultTierDefinitions(),
		},
		Outcomes: []WorkflowOutcomeInput{
			{WorkflowID: "wf-a", Outcome: "FINALIZED", SLOConformant: true, Timestamp: now.Add(-2 * time.Hour), EconomicWeight: 3},
			{WorkflowID: "wf-a", Outcome: "FINALIZED", SLOConformant: false, Timestamp: now.Add(-1 * time.Hour), EconomicWeight: 5},
			{WorkflowID: "wf-b", Outcome: "REVERTED", SLOConformant: false, Timestamp: now, EconomicWeight: 2},
		},
		Policy: WorkflowAuthorPolicy{
			ConformanceWeightBPS: 2500,
			MinBondPenaltyBPS:    4000,
			DefaultConformance:   1,
		},
		BaseMinBondLAC: 2_000,
	}

	first := EvaluateWorkflowAuthorTier(input)
	second := EvaluateWorkflowAuthorTier(input)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("workflow author tier evaluation is nondeterministic:\nfirst=%+v\nsecond=%+v", first, second)
	}
}

func FuzzEvaluateTier_DeterministicAndBounded(f *testing.F) {
	f.Add(uint8(0), uint8(0), uint64(0), 0.0, 0.5, 1.0, 1.0, 1.0, 1.0, 0.0, 0.0, false, int64(0), int64(0))
	f.Add(uint8(0), uint8(1), uint64(60), 0.0, 0.55, 0.65, 1.0, 1.0, 1.0, 0.15, 0.0, true, int64(1_700_000_000), int64(24))
	f.Add(uint8(3), uint8(3), uint64(60), 0.1, 0.9, 0.98, 1.0, 1.0, 1.0, 0.55, 1.0, true, int64(1_700_000_000), int64(720))

	f.Fuzz(func(
		t *testing.T,
		currentRaw uint8,
		pendingRaw uint8,
		receiptCount uint64,
		disputeRate float64,
		reliability float64,
		safety float64,
		latency float64,
		costDiscipline float64,
		dispute float64,
		longevity float64,
		privacy float64,
		eligible bool,
		nowSec int64,
		pendingAgeHours int64,
	) {
		now := time.Unix(nowSec%4_000_000_000, 0).UTC()
		if receiptCount > 1_000_000 {
			receiptCount %= 1_000_000
		}
		pendingAgeHours %= 24 * 365
		if pendingAgeHours < 0 {
			pendingAgeHours = -pendingAgeHours
		}

		state := TierState{
			CurrentTier:        TierLevel(currentRaw),
			TierEnteredAt:      now.Add(-24 * time.Hour),
			PromotionPendingTo: TierLevel(pendingRaw),
		}
		if pendingRaw%2 == 0 {
			state.PromotionStartedAt = now.Add(-time.Duration(pendingAgeHours) * time.Hour)
		}
		input := TierEvaluationInput{
			Reputation: &ReputationResult{
				Reliability:    reliability,
				Safety:         safety,
				Latency:        latency,
				CostDiscipline: costDiscipline,
				Dispute:        dispute,
				Longevity:      longevity,
				Privacy:        privacy,
				Eligible:       eligible,
				UpdatedAt:      now,
			},
			ReceiptCount:   receiptCount,
			DisputeRate30d: disputeRate,
			Now:            now,
			State:          state,
		}

		first := EvaluateTier(input)
		second := EvaluateTier(input)
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("EvaluateTier is nondeterministic:\nfirst=%+v\nsecond=%+v", first, second)
		}
		if first.CurrentTier > TierPremium {
			t.Fatalf("current tier out of bounds: %v", first.CurrentTier)
		}
		if first.EligibleTier > TierPremium {
			t.Fatalf("eligible tier out of bounds: %v", first.EligibleTier)
		}
		if first.Permissions.Tier > TierPremium {
			t.Fatalf("permissions tier out of bounds: %v", first.Permissions.Tier)
		}
		if first.Promoted && first.Demoted {
			t.Fatalf("result cannot both promote and demote: %+v", first)
		}
	})
}

func FuzzPassportAuthor_AdversarialHistory(f *testing.F) {
	f.Add(uint8(3), uint64(100), 1.0, 1.0, 1.0, "FINALIZED", true, "REVERTED", false, uint32(2500))
	f.Add(uint8(2), uint64(50), 0.5, 10.0, 0.1, "PARTIAL_SKIP", true, "FINALIZED", false, uint32(10_000))

	f.Fuzz(func(
		t *testing.T,
		currentRaw uint8,
		receiptCount uint64,
		weightA float64,
		weightB float64,
		weightC float64,
		outcomeA string,
		sloA bool,
		outcomeB string,
		sloB bool,
		weightBPS uint32,
	) {
		now := time.Unix(1_700_000_000, 0).UTC()
		if receiptCount > 1_000_000 {
			receiptCount %= 1_000_000
		}
		input := WorkflowAuthorTierEvaluationInput{
			TierInput: TierEvaluationInput{
				Reputation:   premiumReputation(),
				ReceiptCount: receiptCount,
				Now:          now,
				State:        TierState{CurrentTier: TierLevel(currentRaw), TierEnteredAt: now.Add(-30 * 24 * time.Hour)},
			},
			Outcomes: []WorkflowOutcomeInput{
				{WorkflowID: "wf-a", Outcome: outcomeA, SLOConformant: sloA, Timestamp: now, EconomicWeight: weightA},
				{WorkflowID: "wf-b", Outcome: outcomeB, SLOConformant: sloB, Timestamp: now.Add(-time.Hour), EconomicWeight: weightB},
				{WorkflowID: "wf-c", Outcome: "REVERTED", SLOConformant: false, Timestamp: now.Add(-2 * time.Hour), EconomicWeight: weightC},
			},
			Policy: WorkflowAuthorPolicy{
				ConformanceWeightBPS: weightBPS,
				MinBondPenaltyBPS:    5000,
				DefaultConformance:   1,
			},
			BaseMinBondLAC: 1_000,
		}

		first := EvaluateWorkflowAuthorTier(input)
		second := EvaluateWorkflowAuthorTier(input)
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("workflow author evaluation is nondeterministic:\nfirst=%+v\nsecond=%+v", first, second)
		}
		if math.IsNaN(first.WorkflowConformance) || first.WorkflowConformance < 0 || first.WorkflowConformance > 1 {
			t.Fatalf("workflow conformance out of bounds: %v", first.WorkflowConformance)
		}
		if math.IsNaN(first.Composite) || first.Composite < 0 || first.Composite > 1 {
			t.Fatalf("composite out of bounds: %v", first.Composite)
		}
		if first.Tier.CurrentTier > TierPremium || first.Tier.EligibleTier > TierPremium {
			t.Fatalf("tier out of bounds: %+v", first.Tier)
		}
	})
}

func standardReputation() *ReputationResult {
	return &ReputationResult{
		Reliability:    0.55,
		Safety:         0.65,
		Latency:        1.00,
		CostDiscipline: 1.00,
		Dispute:        1.00,
		Longevity:      0.15,
		Eligible:       true,
	}
}

func trustedReputation() *ReputationResult {
	return &ReputationResult{
		Reliability:    0.75,
		Safety:         0.85,
		Latency:        1.00,
		CostDiscipline: 1.00,
		Dispute:        1.00,
		Longevity:      0.35,
		Eligible:       true,
	}
}

func premiumReputation() *ReputationResult {
	return &ReputationResult{
		Reliability:    0.90,
		Safety:         0.98,
		Latency:        1.00,
		CostDiscipline: 1.00,
		Dispute:        1.00,
		Longevity:      0.55,
		Eligible:       true,
	}
}
