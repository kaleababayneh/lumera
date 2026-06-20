
// Package simulation provides insurance module simulation operations.
package simulation

import (
	"context"
	"fmt"
	"math/rand"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	creditstypes "github.com/LumeraProtocol/lumera/x/credits/types"
	"github.com/LumeraProtocol/lumera/x/insurance/keeper"
	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// #nosec G101 -- these are simulation operation weight keys, not credentials
const (
	opWeightFloodClaims      = "op_weight_insurance_flood_claims"
	opWeightPublishAnomaly   = "op_weight_insurance_publish_anomaly"
	opWeightPoolExhaustion   = "op_weight_insurance_pool_exhaustion"
	defaultWeightFloodClaims = 40
	defaultWeightAnomaly     = 25
	defaultWeightExhaustion  = 20
	insuranceDenom           = "ulac"
)

// WeightedOperations wires the insurance simulation scenarios into the cosmos simulator.
func WeightedOperations(
	appParams simtypes.AppParams,
	cdc codec.JSONCodec,
	k keeper.Keeper,
	ak types.AccountKeeper,
	bk types.BankKeeper,
) simulation.WeightedOperations {
	_ = cdc // retained for future randomized param hooks

	var (
		weightFlood      int
		weightAnomaly    int
		weightExhaustion int
	)

	appParams.GetOrGenerate(opWeightFloodClaims, &weightFlood, nil,
		func(_ *rand.Rand) { weightFlood = defaultWeightFloodClaims })
	appParams.GetOrGenerate(opWeightPublishAnomaly, &weightAnomaly, nil,
		func(_ *rand.Rand) { weightAnomaly = defaultWeightAnomaly })
	appParams.GetOrGenerate(opWeightPoolExhaustion, &weightExhaustion, nil,
		func(_ *rand.Rand) { weightExhaustion = defaultWeightExhaustion })

	return simulation.WeightedOperations{
		simulation.NewWeightedOperation(weightFlood, simulateFloodClaims(k, ak, bk)),
		simulation.NewWeightedOperation(weightAnomaly, simulatePublishAnomaly(k)),
		simulation.NewWeightedOperation(weightExhaustion, simulatePoolExhaustion(k, ak, bk)),
	}
}

func simulateFloodClaims(k keeper.Keeper, ak types.AccountKeeper, bk types.BankKeeper) simtypes.Operation {
	_ = ak
	return func(
		r *rand.Rand,
		_ *baseapp.BaseApp,
		ctx sdk.Context,
		accs []simtypes.Account,
		_ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		if len(accs) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, "flood_claims", "no simulation accounts"), nil, nil
		}

		msgServer := keeper.NewMsgServerImpl(k)
		authority := k.Authority()

		minted := sdkmath.NewInt(r.Int63n(300_000) + 150_000)
		coins := sdk.NewCoins(sdk.NewCoin(insuranceDenom, minted))
		if err := mintModuleCoins(ctx, bk, creditstypes.ModuleName, coins); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "flood_claims", fmt.Sprintf("mint failed: %v", err)), nil, err
		}

		receiptID := fmt.Sprintf("sim-flood-contrib-%d-%d", ctx.BlockHeight(), r.Int63())
		contribMsg := &types.MsgProcessContribution{
			Authority:   authority,
			ReceiptId:   receiptID,
			ToolId:      fmt.Sprintf("tool-flood-%d", r.Intn(200)),
			PublisherId: fmt.Sprintf("publisher-flood-%d", r.Intn(50)),
			Amount:      sdk.NewCoin(insuranceDenom, minted),
		}
		if _, err := msgServer.ProcessContribution(ctx, contribMsg); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "flood_claims", fmt.Sprintf("contribution failed: %v", err)), nil, err
		}

		claimsToFile := r.Intn(6) + 5 // 5-10 claims
		approvedTotal := sdkmath.ZeroInt()
		approvedCount := 0
		for i := 0; i < claimsToFile; i++ {
			simAcc, _ := simtypes.RandomAcc(r, accs)
			claimAmt := sdkmath.NewInt(r.Int63n(35_000) + 5_000)
			claimMsg := &types.MsgFileClaim{
				Claimant:      simAcc.Address.String(),
				ReceiptId:     fmt.Sprintf("sim-flood-claim-%d-%d", ctx.BlockHeight(), r.Int63()),
				ToolId:        fmt.Sprintf("tool-%d", r.Intn(200)),
				PublisherId:   fmt.Sprintf("publisher-%d", r.Intn(50)),
				ClaimedAmount: sdk.NewCoin(insuranceDenom, claimAmt),
				Reason:        randomClaimReason(r),
			}

			resp, err := msgServer.FileClaim(ctx, claimMsg)
			if err != nil {
				// skip this claim but continue flood
				continue
			}

			resolveRoll := r.Float64()
			resolution := "approve"
			var approved sdkmath.Int
			switch {
			case resolveRoll < 0.25:
				resolution = "reject"
			case resolveRoll < 0.6:
				resolution = "partial"
				pct := int64(r.Intn(41) + 50) // 50% - 90%
				approved = claimAmt.MulRaw(pct).QuoRaw(100)
				if !approved.IsPositive() {
					approved = sdkmath.NewInt(1)
				}
			default:
				approved = claimAmt
			}

			var approvedCoin sdk.Coin
			if resolution != "reject" {
				approvedCoin = sdk.NewCoin(insuranceDenom, approved)
			}

			approveMsg := &types.MsgProcessClaim{
				Authority:      authority,
				ClaimId:        resp.ClaimId,
				Resolution:     resolution,
				ApprovedAmount: approvedCoin,
			}
			if _, err := msgServer.ProcessClaim(ctx, approveMsg); err != nil {
				continue
			}

			if resolution != "reject" {
				approvedTotal = approvedTotal.Add(approved)
				approvedCount++

				if r.Float64() < 0.45 {
					payoutMsg := &types.MsgProcessPayout{
						Authority: authority,
						ClaimId:   resp.ClaimId,
						Recipient: simAcc.Address.String(),
						// nil Amount lets keeper use approved total
					}
					if _, err := msgServer.ProcessPayout(ctx, payoutMsg); err != nil {
						// keep simulation moving even if payout fails
						continue
					}
				}
			}
		}

		comment := fmt.Sprintf("flooded %d claims, approved %d totalling %s %s", claimsToFile, approvedCount, approvedTotal.String(), insuranceDenom)
		return simtypes.NewOperationMsgBasic(types.ModuleName, "flood_claims", comment, true, nil), nil, nil
	}
}

func simulatePublishAnomaly(k keeper.Keeper) simtypes.Operation {
	severities := []keeper.AnomalySeverity{
		keeper.SeverityCritical,
		keeper.SeverityHigh,
		keeper.SeverityMedium,
		keeper.SeverityLow,
	}

	return func(
		r *rand.Rand,
		_ *baseapp.BaseApp,
		ctx sdk.Context,
		_ []simtypes.Account,
		_ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		hooks := keeper.NewHooks(k)
		severity := severities[r.Intn(len(severities))]
		report := keeper.AnomalyReport{
			Severity:      severity,
			PublisherID:   fmt.Sprintf("publisher-%d", r.Intn(80)),
			ToolID:        fmt.Sprintf("tool-%d", r.Intn(200)),
			Description:   randomAnomalyDescription(r, severity),
			Evidence:      []string{fmt.Sprintf("ipfs://evidence/%d", r.Int63())},
			ReportedBy:    "simulation-monitor",
			AutoRemediate: severity == keeper.SeverityCritical,
		}

		if err := hooks.PublishAnomaly(ctx, report); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "publish_anomaly", err.Error()), nil, err
		}

		comment := fmt.Sprintf("published %s anomaly for %s/%s", severity, report.PublisherID, report.ToolID)
		return simtypes.NewOperationMsgBasic(types.ModuleName, "publish_anomaly", comment, true, nil), nil, nil
	}
}

func simulatePoolExhaustion(k keeper.Keeper, ak types.AccountKeeper, bk types.BankKeeper) simtypes.Operation {
	_ = ak
	return func(
		r *rand.Rand,
		_ *baseapp.BaseApp,
		ctx sdk.Context,
		accs []simtypes.Account,
		_ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		if len(accs) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, "pool_exhaustion", "no simulation accounts"), nil, nil
		}

		msgServer := keeper.NewMsgServerImpl(k)
		authority := k.Authority()

		// Top up pool aggressively
		minted := sdkmath.NewInt(r.Int63n(400_000) + 250_000)
		coins := sdk.NewCoins(sdk.NewCoin(insuranceDenom, minted))
		if err := mintModuleCoins(ctx, bk, creditstypes.ModuleName, coins); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "pool_exhaustion", fmt.Sprintf("mint failed: %v", err)), nil, err
		}
		receiptID := fmt.Sprintf("sim-exhaustion-contrib-%d-%d", ctx.BlockHeight(), r.Int63())
		_, err := msgServer.ProcessContribution(ctx, &types.MsgProcessContribution{
			Authority:   authority,
			ReceiptId:   receiptID,
			ToolId:      fmt.Sprintf("tool-exhaustion-%d", r.Intn(25)),
			PublisherId: fmt.Sprintf("publisher-exhaustion-%d", r.Intn(10)),
			Amount:      sdk.NewCoin(insuranceDenom, minted),
		})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "pool_exhaustion", fmt.Sprintf("contribution failed: %v", err)), nil, err
		}

		poolBalance, err := k.GetPoolBalance(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "pool_exhaustion", fmt.Sprintf("pool balance lookup failed: %v", err)), nil, err
		}
		poolAmount := poolBalance.AmountOf(insuranceDenom)
		if poolAmount.IsZero() {
			return simtypes.NoOpMsg(types.ModuleName, "pool_exhaustion", "pool balance zero"), nil, nil
		}

		claimant, _ := simtypes.RandomAcc(r, accs)
		claimAmount := poolAmount.MulRaw(int64(r.Intn(26) + 60)).QuoRaw(100) // 60-85% of pool
		if !claimAmount.IsPositive() {
			claimAmount = sdkmath.NewInt(1)
		}

		claimMsg := &types.MsgFileClaim{
			Claimant:      claimant.Address.String(),
			ReceiptId:     fmt.Sprintf("sim-exhaustion-claim-%d-%d", ctx.BlockHeight(), r.Int63()),
			ToolId:        fmt.Sprintf("tool-hot-%d", r.Intn(25)),
			PublisherId:   fmt.Sprintf("publisher-hot-%d", r.Intn(10)),
			ClaimedAmount: sdk.NewCoin(insuranceDenom, claimAmount),
			Reason:        "extended outage under load",
		}
		claimResp, err := msgServer.FileClaim(ctx, claimMsg)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "pool_exhaustion", fmt.Sprintf("file claim failed: %v", err)), nil, err
		}

		approveMsg := &types.MsgProcessClaim{
			Authority:      authority,
			ClaimId:        claimResp.ClaimId,
			Resolution:     "approve",
			ApprovedAmount: sdk.NewCoin(insuranceDenom, claimAmount),
		}
		if _, err := msgServer.ProcessClaim(ctx, approveMsg); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, "pool_exhaustion", fmt.Sprintf("process claim failed: %v", err)), nil, err
		}

		payoutMsg := &types.MsgProcessPayout{
			Authority: authority,
			ClaimId:   claimResp.ClaimId,
			Recipient: claimant.Address.String(),
			// leave Amount nil to payout the approved total
		}
		payoutResp, err := msgServer.ProcessPayout(ctx, payoutMsg)
		if err != nil {
			// attempt a reduced payout to keep simulation progressing
			reduced := claimAmount.MulRaw(70).QuoRaw(100)
			if !reduced.IsPositive() {
				return simtypes.NoOpMsg(types.ModuleName, "pool_exhaustion", fmt.Sprintf("payout failed: %v", err)), nil, err
			}
			payoutMsg.Amount = sdk.NewCoin(insuranceDenom, reduced)
			payoutResp, err = msgServer.ProcessPayout(ctx, payoutMsg)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, "pool_exhaustion", fmt.Sprintf("reduced payout failed: %v", err)), nil, err
			}
		}

		comment := fmt.Sprintf("approved and paid claim %s exhausting ~%s %s", claimResp.ClaimId, claimAmount.String(), insuranceDenom)
		msgBytes := []byte{}
		if payoutResp != nil {
			msgBytes = nil // keep nil payload; summary captured in comment
		}
		return simtypes.NewOperationMsgBasic(types.ModuleName, "pool_exhaustion", comment, true, msgBytes), nil, nil
	}
}

func randomClaimReason(r *rand.Rand) string {
	reasons := []string{
		"timeout after 30s",
		"provider returned malformed payload",
		"response latency spike",
		"downstream SLA miss",
		"contract execution reverted",
	}
	return reasons[r.Intn(len(reasons))]
}

func randomAnomalyDescription(r *rand.Rand, severity keeper.AnomalySeverity) string {
	switch severity {
	case keeper.SeverityCritical:
		return "critical outage impacting majority of traffic"
	case keeper.SeverityHigh:
		return "multiple consecutive SLA violations detected"
	case keeper.SeverityMedium:
		return "elevated error rate observed in monitoring"
	default:
		samples := []string{
			"intermittent latency warnings from probes",
			"sporadic 429 responses from upstream provider",
			"automated canary detected minor regression",
		}
		return samples[r.Intn(len(samples))]
	}
}

type bankMinter interface {
	MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
}

func mintModuleCoins(ctx sdk.Context, bk types.BankKeeper, moduleName string, amt sdk.Coins) error {
	if minter, ok := any(bk).(bankMinter); ok {
		return minter.MintCoins(ctx, moduleName, amt)
	}
	return fmt.Errorf("bank keeper does not support MintCoins")
}
