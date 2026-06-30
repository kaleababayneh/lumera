package keeper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudflare/circl/sign/ed448"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/workflows/types"
)

func TestQuoteWorkflow_SignatureVerify(t *testing.T) {
	ctx, k := setupKeeper(t)
	_, priv := deterministicQuoteKey()
	require.NoError(t, k.PublishWorkflow(ctx, quotePublishMsg("wf-quote", "1.0.0", "1.0.0", "2.0.0")))

	quote, err := k.QuoteWorkflow(ctx, quoteRequest(priv, "wf-quote", "1.0.0", "nonce-sig"))
	require.NoError(t, err)

	pubkey, err := types.RouterPubkeyFromPrivateKey(priv)
	require.NoError(t, err)
	require.NoError(t, types.VerifyBundleQuoteSignature(quote, pubkey))
	require.Equal(t, "wf-quote", quote.WorkflowID)
	require.Equal(t, "1.0.0", quote.Version)
	require.Equal(t, int64(1), quote.AnchoredHeight)
	require.Equal(t, "standard", quote.CallerPassportTier)
	require.Len(t, quote.StepQuotes, 2)
	require.Equal(t, "126500ulac", quote.TotalMaxCost.Amount+quote.TotalMaxCost.Denom)
	require.Equal(t, uint32(200), quote.TotalSloP95Ms)

	events := ctx.EventManager().Events()
	require.Equal(t, types.EventTypeBundleQuoted, events[len(events)-1].Type)
}

func TestQuoteWorkflowRejectsNonCanonicalRequestFields(t *testing.T) {
	ctx, k := setupKeeper(t)
	_, priv := deterministicQuoteKey()
	require.NoError(t, k.PublishWorkflow(ctx, quotePublishMsg("wf-quote-canonical", "1.0.0", "1.0.0")))

	tests := []struct {
		name   string
		mutate func(*types.QuoteWorkflowRequest)
		want   string
	}{
		{
			name: "workflow id padded",
			mutate: func(req *types.QuoteWorkflowRequest) {
				req.WorkflowID = " " + req.WorkflowID + " "
			},
			want: "quote workflow workflow_id must be canonical",
		},
		{
			name: "version padded",
			mutate: func(req *types.QuoteWorkflowRequest) {
				req.Version = " " + req.Version + " "
			},
			want: "quote workflow version must be canonical",
		},
		{
			name: "nonce padded",
			mutate: func(req *types.QuoteWorkflowRequest) {
				req.Nonce = " " + req.Nonce + " "
			},
			want: "quote workflow nonce must be canonical",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := quoteRequest(priv, "wf-quote-canonical", "1.0.0", "nonce-canonical-"+tc.name)
			tc.mutate(req)

			_, err := k.QuoteWorkflow(ctx, req)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestQuoteWorkflow_PricingMinimumCostFloorsTotal(t *testing.T) {
	ctx, k := setupKeeper(t)
	_, priv := deterministicQuoteKey()
	msg := quotePublishMsg("wf-min-cost", "1.0.0", "1.0.0")
	msg.WorkflowCard.Pricing.MinimumCost = coin("ulac", "150000")
	require.NoError(t, k.PublishWorkflow(ctx, msg))

	quote, err := k.QuoteWorkflow(ctx, quoteRequest(priv, "wf-min-cost", "1.0.0", "nonce-min-cost"))

	require.NoError(t, err)
	require.Equal(t, "150000", quote.TotalMaxCost.Amount)
	require.Equal(t, "ulac", quote.TotalMaxCost.Denom)
}

func TestQuoteWorkflow_PricingMaximumCostRejectsUnsafeQuote(t *testing.T) {
	ctx, k := setupKeeper(t)
	_, priv := deterministicQuoteKey()
	msg := quotePublishMsg("wf-max-cost", "1.0.0", "1.0.0")
	msg.WorkflowCard.Pricing.MaximumCost = coin("ulac", "104999")
	require.NoError(t, k.PublishWorkflow(ctx, msg))

	_, err := k.QuoteWorkflow(ctx, quoteRequest(priv, "wf-max-cost", "1.0.0", "nonce-max-cost"))

	require.ErrorContains(t, err, "maximum_cost")
}

func TestPublishWorkflow_PricingMinimumAboveMaximumRejects(t *testing.T) {
	ctx, k := setupKeeper(t)
	msg := quotePublishMsg("wf-min-max-cost", "1.0.0", "1.0.0")
	msg.WorkflowCard.Pricing.MinimumCost = coin("ulac", "150000")
	msg.WorkflowCard.Pricing.MaximumCost = coin("ulac", "120000")

	err := k.PublishWorkflow(ctx, msg)

	require.ErrorContains(t, err, "minimum_cost must be <= maximum_cost")
}

func TestQuoteWorkflow_PassportRequirementsRequireActivePassport(t *testing.T) {
	ctx, k := setupKeeper(t)
	_, priv := deterministicQuoteKey()
	msg := quotePublishMsg("wf-active-passport", "1.0.0", "1.0.0")
	msg.WorkflowCard.PassportRequirements.RequireActivePassport = true
	require.NoError(t, k.PublishWorkflow(ctx, msg))

	req := quoteRequest(priv, "wf-active-passport", "1.0.0", "nonce-inactive-passport")
	req.CallerPassportActive = false
	_, err := k.QuoteWorkflow(ctx, req)
	require.ErrorContains(t, err, "caller passport must be active")

	req = quoteRequest(priv, "wf-active-passport", "1.0.0", "nonce-active-passport")
	quote, err := k.QuoteWorkflow(ctx, req)
	require.NoError(t, err)
	require.True(t, quote.CallerPassportActive)
	require.Equal(t, uint32(700), quote.CallerReputationScore)
}

func TestQuoteWorkflow_PassportRequirementsEnforceReputationFloor(t *testing.T) {
	ctx, k := setupKeeper(t)
	_, priv := deterministicQuoteKey()
	msg := quotePublishMsg("wf-reputation-floor", "1.0.0", "1.0.0")
	msg.WorkflowCard.PassportRequirements.MinReputationScore = 800
	require.NoError(t, k.PublishWorkflow(ctx, msg))

	req := quoteRequest(priv, "wf-reputation-floor", "1.0.0", "nonce-low-reputation")
	req.CallerReputationScore = 799
	_, err := k.QuoteWorkflow(ctx, req)
	require.ErrorContains(t, err, "caller reputation score 799 below required 800")

	req = quoteRequest(priv, "wf-reputation-floor", "1.0.0", "nonce-high-reputation")
	req.CallerReputationScore = 800
	quote, err := k.QuoteWorkflow(ctx, req)
	require.NoError(t, err)
	require.Equal(t, uint32(800), quote.CallerReputationScore)
}

func TestQuoteWorkflow_PassportRequirementsEnforceRequiredBadges(t *testing.T) {
	ctx, k := setupKeeper(t)
	_, priv := deterministicQuoteKey()
	msg := quotePublishMsg("wf-required-badges", "1.0.0", "1.0.0")
	msg.WorkflowCard.PassportRequirements.RequiredBadges = []string{"verified-spend", "onchain-tx-simulation"}
	require.NoError(t, k.PublishWorkflow(ctx, msg))

	req := quoteRequest(priv, "wf-required-badges", "1.0.0", "nonce-missing-badge")
	req.CallerPassportBadges = []string{"verified-spend"}
	_, err := k.QuoteWorkflow(ctx, req)
	require.ErrorContains(t, err, "caller passport missing required badge onchain-tx-simulation")

	req = quoteRequest(priv, "wf-required-badges", "1.0.0", "nonce-has-badges")
	req.CallerPassportBadges = []string{"VERIFIED-SPEND", " onchain-tx-simulation ", "verified-spend"}
	quote, err := k.QuoteWorkflow(ctx, req)
	require.NoError(t, err)
	require.Equal(t, []string{"onchain-tx-simulation", "verified-spend"}, quote.CallerPassportBadges)
}

func TestQuoteWorkflow_BadSig_Reject(t *testing.T) {
	ctx, k := setupKeeper(t)
	_, priv := deterministicQuoteKey()
	require.NoError(t, k.PublishWorkflow(ctx, quotePublishMsg("wf-badsig", "1.0.0", "1.0.0")))
	quote, err := k.QuoteWorkflow(ctx, quoteRequest(priv, "wf-badsig", "1.0.0", "nonce-badsig"))
	require.NoError(t, err)

	quote.Signed = "ed448:" + strings.Repeat("00", ed448.SignatureSize)

	pubkey, err := types.RouterPubkeyFromPrivateKey(priv)
	require.NoError(t, err)
	require.ErrorContains(t, k.ValidateBundleQuote(ctx, quote, pubkey, time.Time{}), "signature does not match")
}

func TestQuoteWorkflow_Expired_Reject(t *testing.T) {
	ctx, k := setupKeeper(t)
	_, priv := deterministicQuoteKey()
	require.NoError(t, k.PublishWorkflow(ctx, quotePublishMsg("wf-expired", "1.0.0", "1.0.0")))
	req := quoteRequest(priv, "wf-expired", "1.0.0", "nonce-expired")
	req.Validity = time.Second
	quote, err := k.QuoteWorkflow(ctx, req)
	require.NoError(t, err)

	pubkey, err := types.RouterPubkeyFromPrivateKey(priv)
	require.NoError(t, err)
	require.ErrorContains(t, k.ValidateBundleQuote(ctx, quote, pubkey, ctx.BlockTime().Add(2*time.Second)), "expired")
}

func TestQuoteWorkflow_Replay_Reject(t *testing.T) {
	ctx, k := setupKeeper(t)
	_, priv := deterministicQuoteKey()
	require.NoError(t, k.PublishWorkflow(ctx, quotePublishMsg("wf-replay", "1.0.0", "1.0.0")))
	req := quoteRequest(priv, "wf-replay", "1.0.0", "nonce-replay")

	quote, err := k.QuoteWorkflow(ctx, req)
	require.NoError(t, err)
	_, err = k.QuoteWorkflow(ctx, req)
	require.ErrorContains(t, err, "bundle quote replay")

	pubkey, err := types.RouterPubkeyFromPrivateKey(priv)
	require.NoError(t, err)
	require.NoError(t, k.ConsumeBundleQuote(ctx, quote, pubkey, time.Time{}))
	require.ErrorContains(t, k.ConsumeBundleQuote(ctx, quote, pubkey, time.Time{}), "bundle quote replay")
}

func TestQuoteWorkflow_InFlightVersionBump_ValidUntilExpiry(t *testing.T) {
	ctx, k := setupKeeper(t)
	_, priv := deterministicQuoteKey()
	require.NoError(t, k.PublishWorkflow(ctx, quotePublishMsg("wf-inflight", "1.0.0", "1.0.0")))
	quote, err := k.QuoteWorkflow(ctx, quoteRequest(priv, "wf-inflight", "1.0.0", "nonce-inflight"))
	require.NoError(t, err)

	require.NoError(t, k.UpgradeWorkflow(ctx, &types.MsgUpgradeWorkflow{
		Author:       workflowTestAuthorAddress(),
		WorkflowID:   "wf-inflight",
		FromVersion:  "1.0.0",
		WorkflowCard: quoteWorkflowCard("wf-inflight", "2.0.0", "2.0.0"),
	}))

	pubkey, err := types.RouterPubkeyFromPrivateKey(priv)
	require.NoError(t, err)
	require.NoError(t, k.ValidateBundleQuote(ctx, quote, pubkey, ctx.BlockTime().Add(time.Minute)))
	require.Equal(t, "1.0.0", quote.Version)
	require.Equal(t, "1.0.0", quote.StepQuotes[0].ToolVersion)
}

func TestQuoteWorkflow_VersionPinning(t *testing.T) {
	ctx, k := setupKeeper(t)
	_, priv := deterministicQuoteKey()
	require.NoError(t, k.PublishWorkflow(ctx, quotePublishMsg("wf-pinning", "1.0.0", "1.0.0")))
	require.NoError(t, k.UpgradeWorkflow(ctx, &types.MsgUpgradeWorkflow{
		Author:       workflowTestAuthorAddress(),
		WorkflowID:   "wf-pinning",
		FromVersion:  "1.0.0",
		WorkflowCard: quoteWorkflowCard("wf-pinning", "2.0.0", "2.0.0"),
	}))

	v1Quote, err := k.QuoteWorkflow(ctx, quoteRequest(priv, "wf-pinning", "1.0.0", "nonce-v1"))
	require.NoError(t, err)
	latestReq := quoteRequest(priv, "wf-pinning", "", "nonce-latest")
	latestQuote, err := k.QuoteWorkflow(ctx, latestReq)
	require.NoError(t, err)

	require.Equal(t, "1.0.0", v1Quote.Version)
	require.Equal(t, "1.0.0", v1Quote.StepQuotes[0].ToolVersion)
	require.Equal(t, "2.0.0", latestQuote.Version)
	require.Equal(t, "2.0.0", latestQuote.StepQuotes[0].ToolVersion)
}

func TestQuoteWorkflow_MetamorphicFreshChainsSameHeightDeterministic(t *testing.T) {
	ctxA, keeperA := setupKeeper(t)
	ctxB, keeperB := setupKeeper(t)
	_, priv := deterministicQuoteKey()
	require.NoError(t, keeperA.PublishWorkflow(ctxA, quotePublishMsg("wf-deterministic-quote", "1.0.0", "1.0.0", "2.0.0")))
	require.NoError(t, keeperB.PublishWorkflow(ctxB, quotePublishMsg("wf-deterministic-quote", "1.0.0", "1.0.0", "2.0.0")))

	quoteA, err := keeperA.QuoteWorkflow(ctxA, quoteRequest(priv, "wf-deterministic-quote", "1.0.0", "nonce-deterministic"))
	require.NoError(t, err)
	quoteB, err := keeperB.QuoteWorkflow(ctxB, quoteRequest(priv, "wf-deterministic-quote", "1.0.0", "nonce-deterministic"))
	require.NoError(t, err)

	bytesA, err := quoteA.CanonicalBytes()
	require.NoError(t, err)
	bytesB, err := quoteB.CanonicalBytes()
	require.NoError(t, err)
	require.Equal(t, string(bytesA), string(bytesB))
	require.Equal(t, quoteA.Signed, quoteB.Signed)
}

func TestDAG_LinearChain_LatencyEqualsSum(t *testing.T) {
	card := quoteWorkflowCard("wf-linear", "1.0.0")
	card.Dag = []*types.Step{
		quoteDAGStep("step-a", "tool.a", 100),
		quoteDAGStep("step-b", "tool.b", 200, "step-a"),
		quoteDAGStep("step-c", "tool.c", 50, "step-b"),
	}

	stepQuotes, _, latency, err := buildBundleStepQuotes(card, "ulac")
	require.NoError(t, err)
	require.Equal(t, uint32(350), latency.computedP95MS)
	require.Equal(t, [][]string{{"step-a"}, {"step-b"}, {"step-c"}}, latency.topoLevels)
	require.Equal(t, []string{"step-a", "step-b", "step-c"}, latency.criticalPathSteps)
	require.Equal(t, []string{"step-a", "step-b", "step-c"}, quoteStepIDs(stepQuotes))
}

func TestDAG_Diamond_LatencyMax(t *testing.T) {
	card := quoteWorkflowCard("wf-diamond", "1.0.0")
	card.Dag = []*types.Step{
		quoteDAGStep("step-d", "tool.d", 7, "step-b", "step-c"),
		quoteDAGStep("step-c", "tool.c", 20, "step-a"),
		quoteDAGStep("step-b", "tool.b", 100, "step-a"),
		quoteDAGStep("step-a", "tool.a", 10),
	}

	_, _, latency, err := buildBundleStepQuotes(card, "ulac")
	require.NoError(t, err)
	require.Equal(t, uint32(117), latency.computedP95MS)
	require.Equal(t, [][]string{{"step-a"}, {"step-b", "step-c"}, {"step-d"}}, latency.topoLevels)
	require.Equal(t, []string{"step-a", "step-b", "step-d"}, latency.criticalPathSteps)
}

func TestDAG_FanOut_ParallelEqualsMax(t *testing.T) {
	card := quoteWorkflowCard("wf-fanout", "1.0.0")
	card.Dag = []*types.Step{
		quoteDAGStep("root", "tool.root", 5),
		quoteDAGStep("branch-a", "tool.a", 20, "root"),
		quoteDAGStep("branch-b", "tool.b", 90, "root"),
		quoteDAGStep("branch-c", "tool.c", 30, "root"),
	}

	_, _, latency, err := buildBundleStepQuotes(card, "ulac")
	require.NoError(t, err)
	require.Equal(t, uint32(95), latency.computedP95MS)
	require.Equal(t, [][]string{{"root"}, {"branch-a", "branch-b", "branch-c"}}, latency.topoLevels)
	require.Equal(t, []string{"root", "branch-b"}, latency.criticalPathSteps)
}

func TestDAG_Cycles_Rejected(t *testing.T) {
	card := quoteWorkflowCard("wf-cycle", "1.0.0")
	card.Dag = []*types.Step{
		quoteDAGStep("step-a", "tool.a", 10, "step-b"),
		quoteDAGStep("step-b", "tool.b", 20, "step-a"),
	}

	_, _, _, err := buildBundleStepQuotes(card, "ulac")
	require.ErrorContains(t, err, "workflow DAG contains cycle")
}

func TestDAG_RejectsNonCanonicalPinnedToolVersion(t *testing.T) {
	cases := []struct {
		name       string
		constraint string
	}{
		{
			name:       "padded exact version",
			constraint: " 1.0.0 ",
		},
		{
			name:       "double equals exact version",
			constraint: strings.Repeat("=", 2) + "1.0.0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := quoteWorkflowCard("wf-noncanonical-version", "1.0.0", tc.constraint)

			_, _, _, err := buildBundleStepQuotes(card, "ulac")

			require.ErrorContains(t, err, "tool_version_constraint must pin")
		})
	}
}

func TestDAG_RedactsToolVersionConstraintDiagnostics(t *testing.T) {
	marker := strings.Join([]string{"workflow", "version", "secret"}, "-")
	apiKeyField := strings.Join([]string{"api", "key"}, "_")
	clientSecretField := strings.Join([]string{"client", "secret"}, "_")
	constraint := " 1.0.0?" + apiKeyField + "=" + marker + "&" + clientSecretField + "=" + marker + " "

	card := quoteWorkflowCard("wf-redact-version", "1.0.0", constraint)
	_, _, _, err := buildBundleStepQuotes(card, "ulac")

	require.Error(t, err)
	require.Contains(t, err.Error(), "tool_version_constraint must pin")
	require.Contains(t, err.Error(), apiKeyField+"=[REDACTED]")
	require.Contains(t, err.Error(), clientSecretField+"=[REDACTED]")
	require.NotContains(t, err.Error(), marker)
}

func TestDAG_AcceptsSingleEqualsPinnedToolVersion(t *testing.T) {
	card := quoteWorkflowCard("wf-single-equals-version", "1.0.0", "=1.0.0")

	stepQuotes, _, _, err := buildBundleStepQuotes(card, "ulac")

	require.NoError(t, err)
	require.Len(t, stepQuotes, 1)
	require.Equal(t, "1.0.0", stepQuotes[0].ToolVersion)
}

func TestDAG_DeterministicOrdering(t *testing.T) {
	left := quoteWorkflowCard("wf-order", "1.0.0")
	left.Dag = []*types.Step{
		quoteDAGStep("join", "tool.join", 7, "branch-a", "branch-b"),
		quoteDAGStep("branch-b", "tool.b", 5, "root"),
		quoteDAGStep("root", "tool.root", 3),
		quoteDAGStep("branch-a", "tool.a", 11, "root"),
	}
	right := quoteWorkflowCard("wf-order", "1.0.0")
	right.Dag = []*types.Step{
		quoteDAGStep("branch-a", "tool.a", 11, "root"),
		quoteDAGStep("root", "tool.root", 3),
		quoteDAGStep("join", "tool.join", 7, "branch-b", "branch-a"),
		quoteDAGStep("branch-b", "tool.b", 5, "root"),
	}

	leftQuotes, _, leftLatency, err := buildBundleStepQuotes(left, "ulac")
	require.NoError(t, err)
	rightQuotes, _, rightLatency, err := buildBundleStepQuotes(right, "ulac")
	require.NoError(t, err)
	require.Equal(t, quoteStepIDs(leftQuotes), quoteStepIDs(rightQuotes))
	require.Equal(t, leftLatency.topoLevels, rightLatency.topoLevels)
	require.Equal(t, leftLatency.criticalPathSteps, rightLatency.criticalPathSteps)
}

func TestDAG_FallbackStepAddsSequentialRecoveryEdge(t *testing.T) {
	card := quoteWorkflowCard("wf-fallback", "1.0.0")
	primary := quoteDAGStep("primary_search", "tool.primary", 100)
	primary.FailureAction = types.FailureAction_FAILURE_ACTION_FALLBACK_STEP
	primary.FallbackStepId = "archive_search"
	archive := quoteDAGStep("archive_search", "tool.archive", 200)
	redact := quoteDAGStep("redact_result", "tool.redact", 50, "primary_search", "archive_search")
	card.Dag = []*types.Step{archive, redact, primary}

	stepQuotes, _, latency, err := buildBundleStepQuotes(card, "ulac")

	require.NoError(t, err)
	require.Equal(t, []string{"primary_search", "archive_search", "redact_result"}, quoteStepIDs(stepQuotes))
	require.Equal(t, [][]string{{"primary_search"}, {"archive_search"}, {"redact_result"}}, latency.topoLevels)
	require.Equal(t, []string{"primary_search", "archive_search", "redact_result"}, latency.criticalPathSteps)
	require.Equal(t, uint32(350), latency.computedP95MS)
}

func TestDAG_ConditionOutcomeAddsSequentialDependency(t *testing.T) {
	card := quoteWorkflowCard("wf-condition", "1.0.0")
	primary := quoteDAGStep("primary_search", "tool.primary", 100)
	archive := quoteDAGStep("archive_search", "tool.archive", 200)
	archive.Condition = "steps.primary_search.outcome == 'failed'"
	redact := quoteDAGStep("redact_result", "tool.redact", 50, "primary_search", "archive_search")
	card.Dag = []*types.Step{archive, redact, primary}

	stepQuotes, _, latency, err := buildBundleStepQuotes(card, "ulac")

	require.NoError(t, err)
	require.Equal(t, []string{"primary_search", "archive_search", "redact_result"}, quoteStepIDs(stepQuotes))
	require.Equal(t, [][]string{{"primary_search"}, {"archive_search"}, {"redact_result"}}, latency.topoLevels)
	require.Equal(t, []string{"primary_search", "archive_search", "redact_result"}, latency.criticalPathSteps)
	require.Equal(t, uint32(350), latency.computedP95MS)
}

func TestDAG_OutputClaimConditionAddsSequentialDependency(t *testing.T) {
	card := quoteWorkflowCard("wf-output-condition", "1.0.0")
	primary := quoteDAGStep("primary_search", "tool.primary", 100)
	archive := quoteDAGStep("archive_search", "tool.archive", 200)
	archive.Condition = "steps.primary_search.output.policy_allowed == true"
	redact := quoteDAGStep("redact_result", "tool.redact", 50, "primary_search", "archive_search")
	card.Dag = []*types.Step{archive, redact, primary}

	stepQuotes, _, latency, err := buildBundleStepQuotes(card, "ulac")

	require.NoError(t, err)
	require.Equal(t, []string{"primary_search", "archive_search", "redact_result"}, quoteStepIDs(stepQuotes))
	require.Equal(t, [][]string{{"primary_search"}, {"archive_search"}, {"redact_result"}}, latency.topoLevels)
	require.Equal(t, []string{"primary_search", "archive_search", "redact_result"}, latency.criticalPathSteps)
	require.Equal(t, uint32(350), latency.computedP95MS)
}

func TestDAG_InvalidConditionRejectsQuotePlan(t *testing.T) {
	card := quoteWorkflowCard("wf-invalid-condition", "1.0.0")
	step := quoteDAGStep("step-a", "tool.alpha", 100)
	step.Condition = "steps.step-b.output.policy_allowed == null"
	card.Dag = []*types.Step{step, quoteDAGStep("step-b", "tool.beta", 100)}

	_, _, _, err := buildBundleStepQuotes(card, "ulac")

	require.ErrorContains(t, err, "condition is invalid")
}

func TestPropDAG_LatencyBound(t *testing.T) {
	for width := 1; width <= 8; width++ {
		card := quoteWorkflowCard(fmt.Sprintf("wf-bound-%d", width), "1.0.0")
		card.Dag = make([]*types.Step, 0, 24)
		var maxSingleStep uint32
		for i := 0; i < 24; i++ {
			stepID := fmt.Sprintf("step-%02d", i)
			slo := uint32((i*37+width)%251 + 1)
			if slo > maxSingleStep {
				maxSingleStep = slo
			}
			if i < width {
				card.Dag = append(card.Dag, quoteDAGStep(stepID, fmt.Sprintf("tool.%02d", i), slo))
				continue
			}
			card.Dag = append(card.Dag, quoteDAGStep(stepID, fmt.Sprintf("tool.%02d", i), slo, fmt.Sprintf("step-%02d", i-width)))
		}

		_, _, latency, err := buildBundleStepQuotes(card, "ulac")
		require.NoError(t, err)
		require.GreaterOrEqual(t, latency.computedP95MS, maxSingleStep)
	}
}

func TestDAG_StressFanOutLatencyUnder1ms(t *testing.T) {
	steps := make(map[string]normalizedBundleStep, 111)
	steps["step-000"] = normalizedBundleStep{id: "step-000", subSLO: 1}
	for i := 1; i <= 110; i++ {
		dep := "step-000"
		if i > 10 {
			dep = fmt.Sprintf("step-%03d", i-10)
		}
		stepID := fmt.Sprintf("step-%03d", i)
		steps[stepID] = normalizedBundleStep{id: stepID, subSLO: 1, dependsOn: []string{dep}}
	}

	_, _, err := computeBundleLatencyPlan(steps)
	require.NoError(t, err)
	start := time.Now()
	_, latency, err := computeBundleLatencyPlan(steps)
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.Equal(t, uint32(12), latency.computedP95MS)
	require.Less(t, elapsed, time.Millisecond, "latency computation took %s", elapsed)
}

func FuzzDAG_RandomShapes(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4, 5})
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte{9, 8, 7, 6, 5, 4, 3})

	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) == 0 {
			raw = []byte{0}
		}
		stepCount := int(raw[0]%16) + 1
		card := quoteWorkflowCard("wf-fuzz-dag", "1.0.0")
		card.Dag = make([]*types.Step, 0, stepCount)
		for i := 0; i < stepCount; i++ {
			deps := make([]string, 0, 3)
			if i > 0 {
				depSlots := int(raw[(i+1)%len(raw)] % 4)
				for j := 0; j < depSlots; j++ {
					depIndex := int(raw[(i+j+2)%len(raw)]) % stepCount
					deps = append(deps, fmt.Sprintf("step-%02d", depIndex))
				}
			}
			slo := uint32(raw[(i+3)%len(raw)]%250) + 1
			card.Dag = append(card.Dag, quoteDAGStep(fmt.Sprintf("step-%02d", i), fmt.Sprintf("tool.%02d", i), slo, deps...))
		}
		_, _, _, _ = buildBundleStepQuotes(card, "ulac")
	})
}

func quoteRequest(priv ed448.PrivateKey, workflowID string, version string, nonce string) *types.QuoteWorkflowRequest {
	return &types.QuoteWorkflowRequest{
		WorkflowID:            workflowID,
		Version:               version,
		Inputs:                json.RawMessage(`{"asset":"ETH","side":"short"}`),
		CallerPassportTier:    "standard",
		CallerPassportActive:  true,
		CallerReputationScore: 700,
		Nonce:                 nonce,
		RouterPrivateKey:      priv,
	}
}

func quotePublishMsg(workflowID, workflowVersion string, toolVersions ...string) *types.MsgPublishWorkflow {
	return &types.MsgPublishWorkflow{
		Author:       workflowTestAuthorAddress(),
		WorkflowCard: quoteWorkflowCard(workflowID, workflowVersion, toolVersions...),
		Bond:         coin("ulac", "1000000"),
	}
}

func quoteWorkflowCard(workflowID, workflowVersion string, toolVersions ...string) *types.WorkflowCard {
	if len(toolVersions) == 0 {
		toolVersions = []string{"1.0.0"}
	}
	steps := make([]*types.Step, 0, len(toolVersions))
	for i, version := range toolVersions {
		stepID := "step-a"
		toolID := "tool.alpha"
		cost := "100000"
		slo := uint32(200)
		if i == 1 {
			stepID = "step-b"
			toolID = "tool.beta"
			cost = "10000"
			slo = 100
		}
		steps = append(steps, &types.Step{
			StepId:                stepID,
			ToolId:                toolID,
			ToolVersionConstraint: version,
			InputBinding:          fmt.Sprintf("$.inputs.step_%d", i),
			MaxSubCost:            coin("ulac", cost),
			SubSloP95Ms:           slo,
			RetryPolicy:           &types.RetryPolicy{MaxAttempts: 1},
			FailureAction:         types.FailureAction_FAILURE_ACTION_REVERT_BUNDLE,
			SideEffect:            types.SideEffect_SIDE_EFFECT_REVERSIBLE,
		})
	}
	return &types.WorkflowCard{
		WorkflowId:   workflowID,
		Version:      workflowVersion,
		DisplayName:  "Workflow " + workflowID,
		AuthorId:     "author-1",
		AuthorPubkey: workflowAuthorPubkey(),
		Categories:   []string{"agent-contracts"},
		LicenseLane:  "byo_key",
		Dag:          steps,
		InputSchema:  `{"type":"object","properties":{"step_0":{"type":"string"},"step_1":{"type":"string"}}}`,
		OutputSchema: `{"type":"object"}`,
		Pricing: &types.WorkflowPricing{
			PricingModel:    "sum_steps_plus_margin",
			MinBond:         coin("ulac", "1000000"),
			AuthorMarginBps: 200,
			InsuranceBps:    300,
		},
		PassportRequirements: &types.PassportRequirements{
			MinTier: types.PassportTier_PASSPORT_TIER_BASIC,
		},
		Governance:       quoteWorkflowGovernance(),
		SafetyInvariants: []*types.SafetyInvariant{workflowTestSafetyInvariant()},
	}
}

func quoteWorkflowGovernance() *types.Governance {
	return &types.Governance{
		AuthorAddresses: []string{workflowTestGovernanceAuthorAddress()},
		UpgradePolicy:   types.UpgradePolicy_UPGRADE_POLICY_SEMVER_COMPATIBLE,
	}
}

func quoteDAGStep(stepID string, toolID string, slo uint32, dependsOn ...string) *types.Step {
	return &types.Step{
		StepId:                stepID,
		ToolId:                toolID,
		ToolVersionConstraint: "1.0.0",
		MaxSubCost:            coin("ulac", "1"),
		SubSloP95Ms:           slo,
		RetryPolicy:           &types.RetryPolicy{MaxAttempts: 1},
		SideEffect:            types.SideEffect_SIDE_EFFECT_REVERSIBLE,
		DependsOn:             dependsOn,
	}
}

func quoteStepIDs(quotes []types.BundleStepQuote) []string {
	out := make([]string, 0, len(quotes))
	for _, quote := range quotes {
		out = append(out, quote.StepID)
	}
	return out
}

func deterministicQuoteKey() (ed448.PublicKey, ed448.PrivateKey) {
	seed := bytes.Repeat([]byte{0x42}, ed448.SeedSize)
	priv := ed448.NewKeyFromSeed(seed)
	return priv.Public().(ed448.PublicKey), priv
}
