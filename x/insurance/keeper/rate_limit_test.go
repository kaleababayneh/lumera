
package keeper_test

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	creditstypes "github.com/LumeraProtocol/lumera/x/credits/types"
	"github.com/LumeraProtocol/lumera/x/insurance/keeper"
	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// ============================================================
// RateLimiter — CheckClaimRate
// ============================================================

func TestRateLimiter_CheckClaimRate_NoClaim(t *testing.T) {
	fixture := setupKeeperTest(t)

	k := fixture.keeper
	rl := keeper.NewRateLimiter(&k)

	// No claims filed yet — should pass
	err := rl.CheckClaimRate(fixture.ctx, "receipt-never-claimed")
	require.NoError(t, err)
}

func TestRateLimiter_CheckClaimRate_ExistingClaim(t *testing.T) {
	fixture := setupKeeperTest(t)

	k := fixture.keeper
	rl := keeper.NewRateLimiter(&k)

	// Record contribution and file a claim for this receipt
	recordContributionForTests(t, fixture, "receipt-claimed", "tool-1", "pub-1", 100)

	_, err := k.FileClaim(fixture.ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant",
		ReceiptId:     "receipt-claimed",
		ToolId:        "tool-1",
		PublisherId:   "pub-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(50)},
		Reason:        "test",
	})
	require.NoError(t, err)

	// Rate limit should block duplicate claim for same receipt
	err = rl.CheckClaimRate(fixture.ctx, "receipt-claimed")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claim already exists")
	require.ErrorIs(t, err, types.ErrDuplicateClaim)
}

func TestRateLimiter_CheckClaimRate_DifferentReceipt(t *testing.T) {
	fixture := setupKeeperTest(t)

	k := fixture.keeper
	rl := keeper.NewRateLimiter(&k)

	// File claim for receipt-A
	recordContributionForTests(t, fixture, "receipt-A", "tool-1", "pub-1", 100)
	_, err := k.FileClaim(fixture.ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant",
		ReceiptId:     "receipt-A",
		ToolId:        "tool-1",
		PublisherId:   "pub-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(50)},
		Reason:        "test",
	})
	require.NoError(t, err)

	// Different receipt should still pass
	err = rl.CheckClaimRate(fixture.ctx, "receipt-B")
	require.NoError(t, err)
}

// ============================================================
// RateLimiter — CheckContributionRate
// ============================================================

func TestRateLimiter_CheckContributionRate_NoContribution(t *testing.T) {
	fixture := setupKeeperTest(t)

	k := fixture.keeper
	rl := keeper.NewRateLimiter(&k)

	// No contribution yet — should pass
	err := rl.CheckContributionRate(fixture.ctx, "receipt-no-contrib")
	require.NoError(t, err)
}

func TestRateLimiter_CheckContributionRate_ExistingContribution(t *testing.T) {
	fixture := setupKeeperTest(t)

	k := fixture.keeper
	rl := keeper.NewRateLimiter(&k)

	// Record a contribution
	recordContributionForTests(t, fixture, "receipt-contrib-dup", "tool-1", "pub-1", 100)

	// Rate limit should block duplicate contribution for same receipt
	err := rl.CheckContributionRate(fixture.ctx, "receipt-contrib-dup")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contribution already recorded")
	require.ErrorIs(t, err, types.ErrContributionRateLimitExceeded)
}

func TestRateLimiter_CheckContributionRate_DifferentReceipt(t *testing.T) {
	fixture := setupKeeperTest(t)

	k := fixture.keeper
	rl := keeper.NewRateLimiter(&k)

	// Record contribution for one receipt
	recordContributionForTests(t, fixture, "receipt-contrib-X", "tool-1", "pub-1", 100)

	// Different receipt should still pass
	err := rl.CheckContributionRate(fixture.ctx, "receipt-contrib-Y")
	require.NoError(t, err)
}

// ============================================================
// RateLimiter — CheckGlobalClaimRate
// ============================================================

func TestRateLimiter_CheckGlobalClaimRate_AlwaysPass(t *testing.T) {
	fixture := setupKeeperTest(t)

	k := fixture.keeper
	rl := keeper.NewRateLimiter(&k)

	// MVP: always passes
	err := rl.CheckGlobalClaimRate(fixture.ctx)
	require.NoError(t, err)
}

// ============================================================
// FileClaim — no coverage (no contribution recorded)
// ============================================================

func TestFileClaim_NoCoverage(t *testing.T) {
	fixture := setupKeeperTest(t)

	// File a claim without any contribution recorded for this receipt
	_, err := fixture.keeper.FileClaim(fixture.ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant",
		ReceiptId:     "receipt-no-coverage",
		ToolId:        "tool-1",
		PublisherId:   "pub-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
		Reason:        "no coverage exists",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no insurance coverage found")
}

// TestFileClaim_RejectsSpoofedPublisher pins the fix for a
// cross-publisher griefing attack. MsgFileClaim accepts a PublisherId
// field; prior to this gate, that field was copied verbatim into
// claim.PublisherId and later used as the key into PublisherRisks when
// the payout path runs (keeper.go publisherRiskKey = "pub:tool"). A
// claimant with a legitimate receipt could file a tiny auto-approve
// claim pointing at any victim publisher and silently inflate the
// victim's ClaimCount — eventually triggering recidivist payout
// reductions (up to 50%) on the victim's own legitimate claims.
//
// The fix resolves the authoritative publisher from the contribution
// (MsgProcessContribution is authority-gated so the publisher stored
// there cannot be spoofed). A mismatched msg.PublisherId is rejected;
// an empty one is filled in from the contribution.
func TestFileClaim_RejectsSpoofedPublisher(t *testing.T) {
	fixture := setupKeeperTest(t)

	// Contribution recorded by the real publisher "pub-authentic".
	recordContributionForTests(t, fixture, "receipt-grief", "tool-1", "pub-authentic", 100)

	// Attacker submits a claim on this receipt but names a VICTIM publisher.
	_, err := fixture.keeper.FileClaim(fixture.ctx, &types.MsgFileClaim{
		Claimant:      "cosmos1claimant",
		ReceiptId:     "receipt-grief",
		ToolId:        "tool-1",
		PublisherId:   "pub-VICTIM",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(5)},
		Reason:        "griefing attempt",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrUnauthorized)
	assert.Contains(t, err.Error(), "does not match")
	assert.Contains(t, err.Error(), "pub-authentic")
}

// TestFileClaim_FillsEmptyPublisherFromContribution verifies the
// normalization side of the gate: clients that omit PublisherId get it
// populated from the authoritative contribution record.
func TestFileClaim_FillsEmptyPublisherFromContribution(t *testing.T) {
	fixture := setupKeeperTest(t)

	recordContributionForTests(t, fixture, "receipt-empty-pub", "tool-1", "pub-real", 100)

	claimID, err := fixture.keeper.FileClaim(fixture.ctx, &types.MsgFileClaim{
		Claimant:  "cosmos1claimant",
		ReceiptId: "receipt-empty-pub",
		ToolId:    "tool-1",
		// PublisherId omitted — should be filled in from contribution.
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(5)},
		Reason:        "omitted publisher",
	})
	require.NoError(t, err)

	claim, err := fixture.keeper.GetClaim(fixture.ctx, claimID)
	require.NoError(t, err)
	assert.Equal(t, "pub-real", claim.PublisherId,
		"claim.PublisherId must be filled from the authoritative contribution")
}

// TestFileClaim_PublisherIsolation_EndToEnd is an invariant test for the
// PublisherId spoofing fix. It runs the full pipeline
//
//	record contribution → file claim → process claim (approve) → payout
//
// and asserts that (a) the legitimate publisher's PublisherRisks entry
// records exactly one ClaimCount increment, AND (b) the victim publisher
// — who has NO contribution on this receipt — has no PublisherRisks entry
// at all, confirming no cross-publisher state leaked via msg.PublisherId.
//
// Pre-fix, an attacker could have bypassed this invariant by setting
// msg.PublisherId=victim; after the fix the claim is either rejected
// (mismatch) or normalized (empty → authoritative). This test pins the
// end-to-end behavior so a regression anywhere between FileClaim,
// ProcessClaim, and processPayout would fail visibly.
func TestFileClaim_PublisherIsolation_EndToEnd(t *testing.T) {
	fixture := setupKeeperTest(t)

	fundPoolForTests(t, fixture, 10000)
	// Only the legitimate publisher has a contribution on this receipt.
	recordContributionForTests(t, fixture, "receipt-isolation", "tool-1", "pub-legitimate", 500)

	claimantAddr := sdk.AccAddress([]byte("claimant_isolation_"))
	claimantAccount := fixture.accountKeeper.NewAccountWithAddress(fixture.ctx, claimantAddr)
	fixture.accountKeeper.SetAccount(fixture.ctx, claimantAccount)

	// File with empty PublisherId — fix fills it from the contribution.
	claimID, err := fixture.keeper.FileClaim(fixture.ctx, &types.MsgFileClaim{
		Claimant:      claimantAddr.String(),
		ReceiptId:     "receipt-isolation",
		ToolId:        "tool-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(5)},
		Reason:        "invariant check",
	})
	require.NoError(t, err)

	authority := authtypes.NewModuleAddress("gov").String()
	require.NoError(t, fixture.keeper.ProcessClaim(fixture.ctx, &types.MsgProcessClaim{
		Authority:      authority,
		ClaimId:        claimID,
		Resolution:     "approve",
		ApprovedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(5)},
	}))

	msgServer := keeper.NewMsgServerImpl(fixture.keeper)
	_, err = msgServer.ProcessPayout(fixture.ctx, &types.MsgProcessPayout{
		Authority: authority,
		ClaimId:   claimID,
		Recipient: claimantAddr.String(),
	})
	require.NoError(t, err)

	// Assertion 1: the legitimate publisher's risk entry exists with
	// exactly one claim count.
	hooks := keeper.NewHooks(fixture.keeper)
	legitRisk, err := hooks.GetPublisherRisk(fixture.ctx, "pub-legitimate", "tool-1")
	require.NoError(t, err, "pub-legitimate must have a PublisherRisks entry after payout")
	require.NotNil(t, legitRisk)
	assert.Equal(t, uint32(1), legitRisk.ClaimCount,
		"pub-legitimate should have exactly 1 ClaimCount after one approved+paid claim")

	// Assertion 2: the victim publisher has NO PublisherRisks entry —
	// nothing was written under pub-VICTIM:tool-1 despite the fact that
	// a pre-fix attacker could have named that publisher in msg.PublisherId.
	_, err = hooks.GetPublisherRisk(fixture.ctx, "pub-VICTIM", "tool-1")
	require.Error(t, err, "pub-VICTIM must have no PublisherRisks entry — "+
		"otherwise the msg.PublisherId spoofing bug has regressed")
}

// ============================================================
// ProcessPayout — already paid
// ============================================================

func TestProcessPayout_AlreadyPaid(t *testing.T) {
	fixture := setupKeeperTest(t)

	fundPoolForTests(t, fixture, 10000)
	recordContributionForTests(t, fixture, "receipt-double-pay", "tool-1", "pub-1", 500)

	// Create claimant account
	claimantAddr := sdk.AccAddress([]byte("claimant_doublepay_"))
	claimantAccount := fixture.accountKeeper.NewAccountWithAddress(fixture.ctx, claimantAddr)
	fixture.accountKeeper.SetAccount(fixture.ctx, claimantAccount)

	// File and approve claim
	claimID, err := fixture.keeper.FileClaim(fixture.ctx, &types.MsgFileClaim{
		Claimant:      claimantAddr.String(),
		ReceiptId:     "receipt-double-pay",
		ToolId:        "tool-1",
		PublisherId:   "pub-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(500)},
		Reason:        "test",
	})
	require.NoError(t, err)

	authority := authtypes.NewModuleAddress("gov").String()
	require.NoError(t, fixture.keeper.ProcessClaim(fixture.ctx, &types.MsgProcessClaim{
		Authority:      authority,
		ClaimId:        claimID,
		Resolution:     "approve",
		ApprovedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(500)},
	}))

	// Pay once
	msgServer := keeper.NewMsgServerImpl(fixture.keeper)
	_, err = msgServer.ProcessPayout(fixture.ctx, &types.MsgProcessPayout{
		Authority: authority,
		ClaimId:   claimID,
		Recipient: claimantAddr.String(),
	})
	require.NoError(t, err)

	// Pay again — should fail
	_, err = msgServer.ProcessPayout(fixture.ctx, &types.MsgProcessPayout{
		Authority: authority,
		ClaimId:   claimID,
		Recipient: claimantAddr.String(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already paid")
}

// ============================================================
// ProcessPayout — claim not approved
// ============================================================

func TestProcessPayout_ClaimNotApproved(t *testing.T) {
	fixture := setupKeeperTest(t)

	fundPoolForTests(t, fixture, 10000)
	recordContributionForTests(t, fixture, "receipt-not-approved", "tool-1", "pub-1", 100)

	// Create claimant
	claimantAddr := sdk.AccAddress([]byte("claimant_notapprvd_"))
	claimantAccount := fixture.accountKeeper.NewAccountWithAddress(fixture.ctx, claimantAddr)
	fixture.accountKeeper.SetAccount(fixture.ctx, claimantAccount)

	// File claim but don't approve
	claimID, err := fixture.keeper.FileClaim(fixture.ctx, &types.MsgFileClaim{
		Claimant:      claimantAddr.String(),
		ReceiptId:     "receipt-not-approved",
		ToolId:        "tool-1",
		PublisherId:   "pub-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
		Reason:        "test",
	})
	require.NoError(t, err)

	// Try to pay without approval
	authority := authtypes.NewModuleAddress("gov").String()
	msgServer := keeper.NewMsgServerImpl(fixture.keeper)
	_, err = msgServer.ProcessPayout(fixture.ctx, &types.MsgProcessPayout{
		Authority: authority,
		ClaimId:   claimID,
		Recipient: claimantAddr.String(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not approved")
}

// ============================================================
// EndBlocker — disabled module
// ============================================================

func TestEndBlocker_DisabledModule(t *testing.T) {
	fixture := setupKeeperTest(t)

	// Ensure params are disabled (default)
	params := types.DefaultParams()
	params.Enabled = false
	require.NoError(t, fixture.keeper.SetParams(fixture.ctx, params))

	// Should complete without error and do nothing
	err := fixture.keeper.EndBlocker(fixture.ctx)
	require.NoError(t, err)
}

// ============================================================
// BeginBlocker
// ============================================================

func TestBeginBlocker_Noop(t *testing.T) {
	fixture := setupKeeperTest(t)
	err := fixture.keeper.BeginBlocker(fixture.ctx)
	require.NoError(t, err)
}

// ============================================================
// ContributeToPool — duplicate receipt
// ============================================================

func TestContributeToPool_DuplicateReceipt(t *testing.T) {
	fixture := setupKeeperTest(t)

	// Ensure module accounts
	creditsAccount := fixture.accountKeeper.GetModuleAccount(fixture.ctx, creditstypes.ModuleName)
	require.NotNil(t, creditsAccount)
	_ = fixture.accountKeeper.GetModuleAccount(fixture.ctx, types.ModuleAccountName)

	// Mint enough for two contributions
	coins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 2000))
	require.NoError(t, fixture.bankKeeper.MintCoins(fixture.ctx, creditstypes.ModuleName, coins))

	// First contribution
	contrib := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100))
	err := fixture.keeper.ContributeToPool(fixture.ctx, "receipt-dup-contrib", "tool-1", "pub-1", "v1", "", contrib)
	require.NoError(t, err)

	// Second contribution with same receipt — should fail
	err = fixture.keeper.ContributeToPool(fixture.ctx, "receipt-dup-contrib", "tool-1", "pub-1", "v1", "", contrib)
	require.Error(t, err)
}

// ============================================================
// Accessor helpers
// ============================================================

func TestKeeper_Authority(t *testing.T) {
	fixture := setupKeeperTest(t)
	authority := fixture.keeper.Authority()
	require.Equal(t, authtypes.NewModuleAddress("gov").String(), authority)
}

func TestKeeper_AccountKeeper(t *testing.T) {
	fixture := setupKeeperTest(t)
	ak := fixture.keeper.AccountKeeper()
	require.NotNil(t, ak)
}

func TestKeeper_BankKeeper(t *testing.T) {
	fixture := setupKeeperTest(t)
	bk := fixture.keeper.BankKeeper()
	require.NotNil(t, bk)
}

func TestKeeper_WithAuthority(t *testing.T) {
	fixture := setupKeeperTest(t)
	k2 := fixture.keeper.WithAuthority("custom-authority")
	require.Equal(t, "custom-authority", k2.Authority())
	// Original should be unchanged
	require.Equal(t, authtypes.NewModuleAddress("gov").String(), fixture.keeper.Authority())
}

// ============================================================
// Genesis edge cases
// ============================================================

func TestInitGenesis_NilGenesis(t *testing.T) {
	fixture := setupKeeperTest(t)
	// Should not panic — uses defaults
	fixture.keeper.InitGenesis(fixture.ctx, nil)

	params := fixture.keeper.GetParams(fixture.ctx)
	require.NotNil(t, params)
}

func TestInitGenesis_NilParams(t *testing.T) {
	fixture := setupKeeperTest(t)
	// Genesis with nil params should use defaults
	fixture.keeper.InitGenesis(fixture.ctx, &types.GenesisState{Params: nil})

	params := fixture.keeper.GetParams(fixture.ctx)
	require.NotNil(t, params)
}

func TestInitGenesis_RejectsInvalidGenesis(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(*types.GenesisState)
		panicMsg  string
		assertion func(*testing.T, keeper.Keeper, sdk.Context)
	}{
		{
			name: "nil claim",
			mutate: func(genesis *types.GenesisState) {
				genesis.Claims = []*types.Claim{nil}
			},
			panicMsg: "failed to validate insurance genesis: claim entry at index 0 cannot be nil",
		},
		{
			name: "nil contribution",
			mutate: func(genesis *types.GenesisState) {
				genesis.Contributions = []*types.Contribution{nil}
			},
			panicMsg: "failed to validate insurance genesis: contribution entry at index 0 cannot be nil",
			assertion: func(t *testing.T, k keeper.Keeper, ctx sdk.Context) {
				exported := k.ExportGenesis(ctx)
				require.Empty(t, exported.Contributions)
			},
		},
		{
			name: "nil publisher risk",
			mutate: func(genesis *types.GenesisState) {
				genesis.PublisherRisks = []*types.PublisherRisk{nil}
			},
			panicMsg: "failed to validate insurance genesis: publisher risk entry at index 0 cannot be nil",
			assertion: func(t *testing.T, k keeper.Keeper, ctx sdk.Context) {
				exported := k.ExportGenesis(ctx)
				require.Empty(t, exported.PublisherRisks)
			},
		},
		{
			name: "nil payout",
			mutate: func(genesis *types.GenesisState) {
				genesis.Payouts = []*types.Payout{nil}
			},
			panicMsg: "failed to validate insurance genesis: payout entry at index 0 cannot be nil",
			assertion: func(t *testing.T, k keeper.Keeper, ctx sdk.Context) {
				exported := k.ExportGenesis(ctx)
				require.Empty(t, exported.Payouts)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := setupKeeperTest(t)
			genesis := types.DefaultGenesis()
			tc.mutate(genesis)

			require.PanicsWithError(t, tc.panicMsg, func() {
				fixture.keeper.InitGenesis(fixture.ctx, genesis)
			})
			if tc.assertion != nil {
				tc.assertion(t, fixture.keeper, fixture.ctx)
			}
		})
	}
}

func TestInitGenesis_RejectsInvalidContributionTimestamp(t *testing.T) {
	t.Skip("not ported: Contribution.Timestamp is now *time.Time (gogoproto stdtime), which is always nil or a well-formed value; an \"out-of-range nanos\" invalid timestamp can no longer be constructed, so genesis validation no longer rejects it")
}

func TestInitGenesis_WithPoolAndMetrics(t *testing.T) {
	fixture := setupKeeperTest(t)

	genesis := &types.GenesisState{
		Params: types.DefaultParams(),
		Pool: &types.PoolState{
			TotalFunds:         "5000",
			AvailableFunds:     "4000",
			ReservedFunds:      "1000",
			TotalContributions: "5000",
			TotalPayouts:       "0",
			TargetUtilization:  "0.2",
			CurrentUtilization: "0.2",
			Status:             types.PoolStatus_POOL_STATUS_HEALTHY,
		},
		Metrics: &types.PoolMetrics{
			PendingClaims:          3,
			TotalContributions_24H: "5000",
			TotalPayouts_24H:       "0",
			ClaimApprovalRate:      "0.8",
			CoverageRatio:          "4.0",
			Samples:                10,
		},
	}
	fixture.keeper.InitGenesis(fixture.ctx, genesis)

	exported := fixture.keeper.ExportGenesis(fixture.ctx)
	require.NotNil(t, exported.Pool)
	require.Equal(t, "5000", exported.Pool.TotalFunds)
	require.Equal(t, "1000", exported.Pool.ReservedFunds)
	require.NotNil(t, exported.Metrics)
	require.EqualValues(t, 3, exported.Metrics.PendingClaims)
}

// ============================================================
// ProcessContribution via MsgServer — invalid amount
// ============================================================

func TestMsgServer_ProcessContribution_NegativeAmount(t *testing.T) {
	fixture := setupKeeperTest(t)
	msgServer := keeper.NewMsgServerImpl(fixture.keeper)
	authority := authtypes.NewModuleAddress("gov").String()

	resp, err := msgServer.ProcessContribution(fixture.ctx, &types.MsgProcessContribution{
		Authority:   authority,
		ReceiptId:   "receipt-neg",
		ToolId:      "tool-neg",
		PublisherId: "pub-neg",
		Amount:      sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(-100)},
	})
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestMsgServer_ProcessContribution_DuplicateReceiptRateLimited(t *testing.T) {
	fixture := setupKeeperTest(t)
	msgServer := keeper.NewMsgServerImpl(fixture.keeper)
	authority := authtypes.NewModuleAddress("gov").String()

	creditsAccount := fixture.accountKeeper.GetModuleAccount(fixture.ctx, creditstypes.ModuleName)
	require.NotNil(t, creditsAccount)
	_ = fixture.accountKeeper.GetModuleAccount(fixture.ctx, types.ModuleAccountName)

	coins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 2000))
	require.NoError(t, fixture.bankKeeper.MintCoins(fixture.ctx, creditstypes.ModuleName, coins))

	_, err := msgServer.ProcessContribution(fixture.ctx, &types.MsgProcessContribution{
		Authority:   authority,
		ReceiptId:   "receipt-msg-dup",
		ToolId:      "tool-1",
		PublisherId: "pub-1",
		Amount:      sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
	})
	require.NoError(t, err)

	_, err = msgServer.ProcessContribution(fixture.ctx, &types.MsgProcessContribution{
		Authority:   authority,
		ReceiptId:   "receipt-msg-dup",
		ToolId:      "tool-1",
		PublisherId: "pub-1",
		Amount:      sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(100)},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrContributionRateLimitExceeded)
	assert.Contains(t, err.Error(), "contribution already recorded")
}

// ============================================================
// ValidateAuthority — empty authority keeper
// ============================================================

func TestValidateAuthority_EmptyAuthority(t *testing.T) {
	fixture := setupKeeperTest(t)
	// Create a keeper with empty authority
	k := fixture.keeper.WithAuthority("")
	// Empty authority means no check — should pass for any caller
	err := k.ValidateAuthority("anyone")
	require.NoError(t, err)
}

// ============================================================
// Pool balance — missing module account
// ============================================================

func TestGetPoolBalance_WithFundedPool(t *testing.T) {
	fixture := setupKeeperTest(t)

	// Ensure module accounts
	creditsAccount := fixture.accountKeeper.GetModuleAccount(fixture.ctx, creditstypes.ModuleName)
	require.NotNil(t, creditsAccount)
	insuranceAccount := fixture.accountKeeper.GetModuleAccount(fixture.ctx, types.ModuleAccountName)
	require.NotNil(t, insuranceAccount)

	// Mint and contribute
	coins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 5000))
	require.NoError(t, fixture.bankKeeper.MintCoins(fixture.ctx, creditstypes.ModuleName, coins))
	require.NoError(t, fixture.keeper.ContributeToPool(fixture.ctx, "receipt-balance-check", "tool-1", "pub-1", "v1", "",
		sdk.NewCoins(sdk.NewInt64Coin("ulac", 3000))))

	balance, err := fixture.keeper.GetPoolBalance(fixture.ctx)
	require.NoError(t, err)
	require.Equal(t, sdkmath.NewInt(3000), balance.AmountOf("ulac"))
}
