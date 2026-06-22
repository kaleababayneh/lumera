
package keeper_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	querytypes "github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	creditstypes "github.com/LumeraProtocol/lumera/x/credits/types"
	"github.com/LumeraProtocol/lumera/x/insurance/keeper"
	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// --- GetParams ---

func TestQueryServer_GetParams_HappyPath(t *testing.T) {
	f := setupKeeperTest(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.GetParams(f.ctx, &types.QueryGetParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp.Params)
}

func TestQueryServer_GetParams_CloneSafety(t *testing.T) {
	f := setupKeeperTest(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	params := types.DefaultParams()
	params.MaxClaimsPerBlock = 17
	params.Enabled = true
	require.NoError(t, f.keeper.SetParams(f.ctx, params))

	resp, err := qs.GetParams(f.ctx, &types.QueryGetParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp.Params)

	resp.Params.MaxClaimsPerBlock = 99
	resp.Params.Enabled = false

	fresh, err := qs.GetParams(f.ctx, &types.QueryGetParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, uint32(17), fresh.Params.MaxClaimsPerBlock)
	require.True(t, fresh.Params.Enabled)
}

func TestQueryServer_GetParams_NilRequest(t *testing.T) {
	f := setupKeeperTest(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.GetParams(f.ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "empty request")
}

// --- PoolStatus ---

func TestQueryServer_PoolStatus_Empty(t *testing.T) {
	f := setupKeeperTest(t)
	_ = f.accountKeeper.GetModuleAccount(f.ctx, types.ModuleAccountName)

	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.PoolStatus(f.ctx, &types.QueryPoolStatusRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	// Pool should have zero balance initially
	for _, coin := range resp.Balance {
		require.Equal(t, "0", coin.Amount.String())
	}
}

func TestQueryServer_PoolStatus_WithContribution(t *testing.T) {
	f := setupKeeperTest(t)

	// Fund credits module account and contribute to pool
	creditsAccount := f.accountKeeper.GetModuleAccount(f.ctx, creditstypes.ModuleName)
	require.NotNil(t, creditsAccount)
	insuranceAccount := f.accountKeeper.GetModuleAccount(f.ctx, types.ModuleAccountName)
	require.NotNil(t, insuranceAccount)

	initialCoins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 1_000_000))
	require.NoError(t, f.bankKeeper.MintCoins(f.ctx, creditstypes.ModuleName, initialCoins))

	contribution := sdk.NewCoins(sdk.NewInt64Coin("ulac", 500))
	require.NoError(t, f.keeper.ContributeToPool(f.ctx, "receipt-qs1", "tool-1", "pub-1", "v1", "", contribution))

	qs := keeper.NewQueryServerImpl(f.keeper)
	resp, err := qs.PoolStatus(f.ctx, &types.QueryPoolStatusRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Pool should have 500 ulac
	found := false
	for _, coin := range resp.Balance {
		if coin.Denom == "ulac" {
			require.Equal(t, "500", coin.Amount.String())
			found = true
		}
	}
	require.True(t, found, "expected ulac balance in pool")
}

func TestQueryServer_PoolStatus_CloneSafety(t *testing.T) {
	f := setupKeeperTest(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.PoolStatus(f.ctx, &types.QueryPoolStatusRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp.State)
	require.NotNil(t, resp.Metrics)

	resp.State.TotalFunds = "999"
	resp.State.Status = types.PoolStatus_POOL_STATUS_CRITICAL
	resp.Metrics.Samples = 99
	resp.Metrics.PoolHealthScore = "1"

	fresh, err := qs.PoolStatus(f.ctx, &types.QueryPoolStatusRequest{})
	require.NoError(t, err)
	require.Equal(t, "0", fresh.State.TotalFunds)
	require.Equal(t, types.PoolStatus_POOL_STATUS_HEALTHY, fresh.State.Status)
	require.Equal(t, uint64(0), fresh.Metrics.Samples)
	require.Equal(t, "100", fresh.Metrics.PoolHealthScore)
}

func TestQueryServer_PoolStatus_NilRequest(t *testing.T) {
	f := setupKeeperTest(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.PoolStatus(f.ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "empty request")
}

// --- GetClaim ---

func TestQueryServer_GetClaim_HappyPath(t *testing.T) {
	// FIXED: cloneClaim now deep-copies via a marshal/unmarshal round-trip
	// (deepCopyProto) instead of proto.Clone, so a claim with a populated sdk.Coin
	// amount no longer panics. Test re-enabled.
	f := setupKeeperTest(t)

	// Set up: fund module, contribute, then file claim
	creditsAccount := f.accountKeeper.GetModuleAccount(f.ctx, creditstypes.ModuleName)
	require.NotNil(t, creditsAccount)
	_ = f.accountKeeper.GetModuleAccount(f.ctx, types.ModuleAccountName)

	initialCoins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 1_000_000))
	require.NoError(t, f.bankKeeper.MintCoins(f.ctx, creditstypes.ModuleName, initialCoins))

	contribution := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100))
	require.NoError(t, f.keeper.ContributeToPool(f.ctx, "receipt-claim1", "tool-1", "pub-1", "v1", "", contribution))

	// File claim through keeper
	claimID, err := f.keeper.FileClaim(f.ctx, &types.MsgFileClaim{
		ReceiptId:     "receipt-claim1",
		Claimant:      "lumera1claimant",
		ToolId:        "tool-1",
		PublisherId:   "pub-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(50)},
		Reason:        "test claim",
	})
	require.NoError(t, err)
	require.NotEmpty(t, claimID)

	// Query the claim
	qs := keeper.NewQueryServerImpl(f.keeper)
	resp, err := qs.GetClaim(f.ctx, &types.QueryGetClaimRequest{ClaimId: claimID})
	require.NoError(t, err)
	require.NotNil(t, resp.Claim)
	require.Equal(t, claimID, resp.Claim.Id)
	require.Equal(t, "receipt-claim1", resp.Claim.ReceiptId)
	require.Equal(t, "lumera1claimant", resp.Claim.ClaimantId)
	require.Equal(t, types.ClaimStatus_CLAIM_STATUS_PENDING, resp.Claim.Status)
}

func TestQueryServer_GetClaim_CloneSafety(t *testing.T) {
	// FIXED: cloneClaim now deep-copies via a marshal/unmarshal round-trip
	// (deepCopyProto) instead of proto.Clone, so a claim with a populated sdk.Coin
	// amount no longer panics. Test re-enabled.
	f := setupKeeperTest(t)
	claimID := seedQueryClaim(t, f, "receipt-claim-clone", "lumera1claimant")

	qs := keeper.NewQueryServerImpl(f.keeper)
	resp, err := qs.GetClaim(f.ctx, &types.QueryGetClaimRequest{ClaimId: claimID})
	require.NoError(t, err)
	require.NotNil(t, resp.Claim)

	resp.Claim.Reason = "mutated"
	resp.Claim.Status = types.ClaimStatus_CLAIM_STATUS_PAID
	resp.Claim.ClaimedAmount.Amount = sdkmath.NewInt(999)

	fresh, err := qs.GetClaim(f.ctx, &types.QueryGetClaimRequest{ClaimId: claimID})
	require.NoError(t, err)
	require.Equal(t, "test", fresh.Claim.Reason)
	require.Equal(t, types.ClaimStatus_CLAIM_STATUS_PENDING, fresh.Claim.Status)
	require.Equal(t, "50", fresh.Claim.ClaimedAmount.Amount.String())
}

func TestQueryServer_GetClaim_NotFound(t *testing.T) {
	f := setupKeeperTest(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.GetClaim(f.ctx, &types.QueryGetClaimRequest{ClaimId: "nonexistent"})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "not found")
}

func TestQueryServer_GetClaim_EmptyID(t *testing.T) {
	f := setupKeeperTest(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.GetClaim(f.ctx, &types.QueryGetClaimRequest{ClaimId: ""})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "claim_id is required")
}

func TestQueryServer_GetClaim_NilRequest(t *testing.T) {
	f := setupKeeperTest(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.GetClaim(f.ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestQueryServer_GetClaim_RejectsInvalidClaimIDBeforeSDKContext(t *testing.T) {
	qs := keeper.NewQueryServerImpl(keeper.Keeper{})
	ctx := context.Background()

	tests := []struct {
		name    string
		claimID string
		want    string
	}{
		{
			name:    "blank",
			claimID: "\t\n",
			want:    "claim_id is required",
		},
		{
			name:    "padded",
			claimID: " claim-1 ",
			want:    "claim_id must be canonical",
		},
		{
			name:    "overlong",
			claimID: strings.Repeat("a", types.MaxInsuranceIDLen+1),
			want:    "claim_id exceeds 256-byte cap",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := qs.GetClaim(ctx, &types.QueryGetClaimRequest{ClaimId: tt.claimID})
			require.ErrorContains(t, err, tt.want)
			require.Nil(t, resp)
		})
	}
}

func TestQueryServer_ValidatesBeforeSDKContext(t *testing.T) {
	qs := keeper.NewQueryServerImpl(keeper.Keeper{})
	ctx := context.Background()

	tests := []struct {
		name     string
		call     func() error
		wantCode codes.Code
		want     string
	}{
		{
			name: "PoolStatus nil request",
			call: func() error {
				_, err := qs.PoolStatus(ctx, nil)
				return err
			},
			wantCode: codes.InvalidArgument,
			want:     "empty request",
		},
		{
			name: "PoolStatus zero keeper",
			call: func() error {
				_, err := qs.PoolStatus(ctx, &types.QueryPoolStatusRequest{})
				return err
			},
			wantCode: codes.Internal,
			want:     "insurance keeper not initialized",
		},
		{
			name: "GetClaim nil request",
			call: func() error {
				_, err := qs.GetClaim(ctx, nil)
				return err
			},
			wantCode: codes.InvalidArgument,
			want:     "empty request",
		},
		{
			name: "GetClaim zero keeper",
			call: func() error {
				_, err := qs.GetClaim(ctx, &types.QueryGetClaimRequest{ClaimId: "claim-direct"})
				return err
			},
			wantCode: codes.Internal,
			want:     "insurance keeper not initialized",
		},
		{
			name: "ListClaims nil request",
			call: func() error {
				_, err := qs.ListClaims(ctx, nil)
				return err
			},
			wantCode: codes.InvalidArgument,
			want:     "empty request",
		},
		{
			name: "ListClaims reverse pagination",
			call: func() error {
				_, err := qs.ListClaims(ctx, &types.QueryListClaimsRequest{
					Pagination: &querytypes.PageRequest{Reverse: true},
				})
				return err
			},
			wantCode: codes.Unimplemented,
			want:     "reverse pagination not supported",
		},
		{
			name: "ListClaims mixed key and offset",
			call: func() error {
				_, err := qs.ListClaims(ctx, &types.QueryListClaimsRequest{
					Pagination: &querytypes.PageRequest{Key: []byte("claim-direct"), Offset: 1},
				})
				return err
			},
			wantCode: codes.InvalidArgument,
			want:     "key and offset are mutually exclusive",
		},
		{
			name: "ListClaims padded claimant filter",
			call: func() error {
				_, err := qs.ListClaims(ctx, &types.QueryListClaimsRequest{ClaimantId: " lumera1claimant "})
				return err
			},
			wantCode: codes.InvalidArgument,
			want:     "claimant_id must be canonical",
		},
		{
			name: "ListClaims overlong publisher filter",
			call: func() error {
				_, err := qs.ListClaims(ctx, &types.QueryListClaimsRequest{
					PublisherId: strings.Repeat("p", types.MaxInsuranceIDLen+1),
				})
				return err
			},
			wantCode: codes.InvalidArgument,
			want:     "publisher_id exceeds 256-byte cap",
		},
		{
			name: "ListClaims padded status filter",
			call: func() error {
				_, err := qs.ListClaims(ctx, &types.QueryListClaimsRequest{Status: " CLAIM_STATUS_PENDING "})
				return err
			},
			wantCode: codes.InvalidArgument,
			want:     "status must be canonical",
		},
		{
			name: "ListClaims unknown status filter",
			call: func() error {
				_, err := qs.ListClaims(ctx, &types.QueryListClaimsRequest{Status: "CLAIM_STATUS_UNKNOWN"})
				return err
			},
			wantCode: codes.InvalidArgument,
			want:     "status has invalid value",
		},
		{
			name: "ListClaims zero keeper",
			call: func() error {
				_, err := qs.ListClaims(ctx, &types.QueryListClaimsRequest{})
				return err
			},
			wantCode: codes.Internal,
			want:     "insurance keeper not initialized",
		},
		{
			name: "GetParams nil request",
			call: func() error {
				_, err := qs.GetParams(ctx, nil)
				return err
			},
			wantCode: codes.InvalidArgument,
			want:     "empty request",
		},
		{
			name: "GetParams zero keeper",
			call: func() error {
				_, err := qs.GetParams(ctx, &types.QueryGetParamsRequest{})
				return err
			},
			wantCode: codes.Internal,
			want:     "insurance keeper not initialized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			require.NotPanics(t, func() {
				err = tt.call()
			})
			require.Error(t, err)
			require.Equal(t, tt.wantCode, status.Code(err))
			require.Contains(t, err.Error(), tt.want)
			if tt.want != "insurance keeper not initialized" {
				require.NotContains(t, err.Error(), "keeper not initialized")
			}
		})
	}
}

// --- ListClaims ---

func TestQueryServer_ListClaims_Empty(t *testing.T) {
	f := setupKeeperTest(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.ListClaims(f.ctx, &types.QueryListClaimsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Empty(t, resp.Claims)
}

func TestQueryServer_ListClaims_WithData(t *testing.T) {
	// FIXED: cloneClaim now deep-copies via a marshal/unmarshal round-trip
	// (deepCopyProto) instead of proto.Clone, so a claim with a populated sdk.Coin
	// amount no longer panics. Test re-enabled.
	f := setupKeeperTest(t)

	// Set up two claims on different receipts
	creditsAccount := f.accountKeeper.GetModuleAccount(f.ctx, creditstypes.ModuleName)
	require.NotNil(t, creditsAccount)
	_ = f.accountKeeper.GetModuleAccount(f.ctx, types.ModuleAccountName)

	initialCoins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 1_000_000))
	require.NoError(t, f.bankKeeper.MintCoins(f.ctx, creditstypes.ModuleName, initialCoins))

	for _, receiptID := range []string{"receipt-list1", "receipt-list2"} {
		contribution := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100))
		require.NoError(t, f.keeper.ContributeToPool(f.ctx, receiptID, "tool-1", "pub-1", "v1", "", contribution))

		_, err := f.keeper.FileClaim(f.ctx, &types.MsgFileClaim{
			ReceiptId:     receiptID,
			Claimant:      "lumera1claimant",
			ToolId:        "tool-1",
			PublisherId:   "pub-1",
			ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(50)},
			Reason:        "test",
		})
		require.NoError(t, err)
	}

	qs := keeper.NewQueryServerImpl(f.keeper)
	resp, err := qs.ListClaims(f.ctx, &types.QueryListClaimsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Claims, 2)
}

func TestQueryServer_ListClaims_CloneSafety(t *testing.T) {
	// FIXED: cloneClaim now deep-copies via a marshal/unmarshal round-trip
	// (deepCopyProto) instead of proto.Clone, so a claim with a populated sdk.Coin
	// amount no longer panics. Test re-enabled.
	f := setupKeeperTest(t)
	claimID := seedQueryClaim(t, f, "receipt-list-clone", "lumera1claimant")

	qs := keeper.NewQueryServerImpl(f.keeper)
	resp, err := qs.ListClaims(f.ctx, &types.QueryListClaimsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Claims, 1)

	resp.Claims[0].Reason = "mutated"
	resp.Claims[0].Status = types.ClaimStatus_CLAIM_STATUS_PAID
	resp.Claims[0].ClaimedAmount.Amount = sdkmath.NewInt(999)

	fresh, err := qs.GetClaim(f.ctx, &types.QueryGetClaimRequest{ClaimId: claimID})
	require.NoError(t, err)
	require.Equal(t, "test", fresh.Claim.Reason)
	require.Equal(t, types.ClaimStatus_CLAIM_STATUS_PENDING, fresh.Claim.Status)
	require.Equal(t, "50", fresh.Claim.ClaimedAmount.Amount.String())
}

func TestQueryServer_ListClaims_PaginationKey(t *testing.T) {
	// FIXED: cloneClaim now deep-copies via a marshal/unmarshal round-trip
	// (deepCopyProto) instead of proto.Clone, so a claim with a populated sdk.Coin
	// amount no longer panics. Test re-enabled.
	f := setupKeeperTest(t)
	claimIDs := make([]string, 0, 5)

	for _, receiptID := range []string{"receipt-page-1", "receipt-page-2", "receipt-page-3", "receipt-page-4", "receipt-page-5"} {
		claimIDs = append(claimIDs, seedQueryClaim(t, f, receiptID, "lumera1claimant"))
	}

	qs := keeper.NewQueryServerImpl(f.keeper)
	first, err := qs.ListClaims(f.ctx, &types.QueryListClaimsRequest{
		Pagination: &querytypes.PageRequest{Limit: 2, CountTotal: true},
	})
	require.NoError(t, err)
	require.Len(t, first.Claims, 2)
	require.Equal(t, uint64(5), first.Pagination.Total)
	require.Equal(t, []byte(claimIDs[2]), first.Pagination.NextKey)
	require.Equal(t, claimIDs[0], first.Claims[0].Id)
	require.Equal(t, claimIDs[1], first.Claims[1].Id)

	second, err := qs.ListClaims(f.ctx, &types.QueryListClaimsRequest{
		Pagination: &querytypes.PageRequest{Key: first.Pagination.NextKey, Limit: 2, CountTotal: true},
	})
	require.NoError(t, err)
	require.Len(t, second.Claims, 2)
	require.Equal(t, uint64(5), second.Pagination.Total)
	require.Equal(t, []byte(claimIDs[4]), second.Pagination.NextKey)
	require.Equal(t, claimIDs[2], second.Claims[0].Id)
	require.Equal(t, claimIDs[3], second.Claims[1].Id)

	final, err := qs.ListClaims(f.ctx, &types.QueryListClaimsRequest{
		Pagination: &querytypes.PageRequest{Key: second.Pagination.NextKey, Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, final.Claims, 1)
	require.Empty(t, final.Pagination.NextKey)
	require.Equal(t, claimIDs[4], final.Claims[0].Id)
}

func TestQueryServer_ListClaims_CapsResponseAndCountsTotal(t *testing.T) {
	// FIXED: cloneClaim now deep-copies via a marshal/unmarshal round-trip
	// (deepCopyProto) instead of proto.Clone, so a claim with a populated sdk.Coin
	// amount no longer panics. Test re-enabled.
	const queryLimit = 1000
	const totalClaims = queryLimit + 5

	f := setupKeeperTest(t)
	claimIDs := make([]string, 0, totalClaims)
	for i := 0; i < totalClaims; i++ {
		receiptID := fmt.Sprintf("receipt-list-cap-%04d", i)
		claimIDs = append(claimIDs, seedQueryClaim(t, f, receiptID, "lumera1claimant"))
	}
	sort.Strings(claimIDs)

	qs := keeper.NewQueryServerImpl(f.keeper)
	unpaginated, err := qs.ListClaims(f.ctx, &types.QueryListClaimsRequest{})
	require.NoError(t, err)
	require.Len(t, unpaginated.Claims, queryLimit)
	require.NotNil(t, unpaginated.Pagination)
	require.Equal(t, uint64(totalClaims), unpaginated.Pagination.Total)
	require.Equal(t, []byte(claimIDs[queryLimit]), unpaginated.Pagination.NextKey)

	firstPage, err := qs.ListClaims(f.ctx, &types.QueryListClaimsRequest{
		Pagination: &querytypes.PageRequest{Limit: totalClaims, CountTotal: true},
	})
	require.NoError(t, err)
	require.Len(t, firstPage.Claims, queryLimit)
	require.Equal(t, uint64(totalClaims), firstPage.Pagination.Total)
	require.Equal(t, []byte(claimIDs[queryLimit]), firstPage.Pagination.NextKey)

	secondPage, err := qs.ListClaims(f.ctx, &types.QueryListClaimsRequest{
		Pagination: &querytypes.PageRequest{Key: firstPage.Pagination.NextKey, CountTotal: true},
	})
	require.NoError(t, err)
	require.Len(t, secondPage.Claims, totalClaims-queryLimit)
	require.Equal(t, uint64(totalClaims), secondPage.Pagination.Total)
	require.Empty(t, secondPage.Pagination.NextKey)
}

func TestQueryServer_ListClaims_PaginationInvalidKey(t *testing.T) {
	f := setupKeeperTest(t)
	seedQueryClaim(t, f, "receipt-invalid-page-key", "lumera1claimant")

	qs := keeper.NewQueryServerImpl(f.keeper)
	resp, err := qs.ListClaims(f.ctx, &types.QueryListClaimsRequest{
		Pagination: &querytypes.PageRequest{Key: []byte("claim-does-not-exist"), Limit: 2},
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "invalid pagination key")
}

func TestQueryServer_ListClaims_RejectsMixedPaginationKeyAndOffset(t *testing.T) {
	f := setupKeeperTest(t)
	claimID := seedQueryClaim(t, f, "receipt-mixed-page", "lumera1claimant")

	qs := keeper.NewQueryServerImpl(f.keeper)
	resp, err := qs.ListClaims(f.ctx, &types.QueryListClaimsRequest{
		Pagination: &querytypes.PageRequest{Key: []byte(claimID), Offset: 1, Limit: 1},
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "key and offset are mutually exclusive")
}

func TestQueryServer_ListClaims_FilterByClaimant(t *testing.T) {
	// FIXED: cloneClaim now deep-copies via a marshal/unmarshal round-trip
	// (deepCopyProto) instead of proto.Clone, so a claim with a populated sdk.Coin
	// amount no longer panics. Test re-enabled.
	f := setupKeeperTest(t)

	creditsAccount := f.accountKeeper.GetModuleAccount(f.ctx, creditstypes.ModuleName)
	require.NotNil(t, creditsAccount)
	_ = f.accountKeeper.GetModuleAccount(f.ctx, types.ModuleAccountName)

	initialCoins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 1_000_000))
	require.NoError(t, f.bankKeeper.MintCoins(f.ctx, creditstypes.ModuleName, initialCoins))

	// Claim by claimant A
	contribution := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100))
	require.NoError(t, f.keeper.ContributeToPool(f.ctx, "receipt-a", "tool-1", "pub-1", "v1", "", contribution))
	_, err := f.keeper.FileClaim(f.ctx, &types.MsgFileClaim{
		ReceiptId:     "receipt-a",
		Claimant:      "lumera1alice",
		ToolId:        "tool-1",
		PublisherId:   "pub-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(50)},
		Reason:        "test",
	})
	require.NoError(t, err)

	// Claim by claimant B
	require.NoError(t, f.keeper.ContributeToPool(f.ctx, "receipt-b", "tool-1", "pub-1", "v1", "", contribution))
	_, err = f.keeper.FileClaim(f.ctx, &types.MsgFileClaim{
		ReceiptId:     "receipt-b",
		Claimant:      "lumera1bob",
		ToolId:        "tool-1",
		PublisherId:   "pub-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(50)},
		Reason:        "test",
	})
	require.NoError(t, err)

	qs := keeper.NewQueryServerImpl(f.keeper)
	resp, err := qs.ListClaims(f.ctx, &types.QueryListClaimsRequest{ClaimantId: "lumera1alice"})
	require.NoError(t, err)
	require.Len(t, resp.Claims, 1)
	require.Equal(t, "lumera1alice", resp.Claims[0].ClaimantId)
}

func TestQueryServer_ListClaims_NilRequest(t *testing.T) {
	f := setupKeeperTest(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.ListClaims(f.ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func seedQueryClaim(t *testing.T, f *keeperFixture, receiptID, claimant string) string {
	t.Helper()

	require.NotNil(t, f.accountKeeper.GetModuleAccount(f.ctx, creditstypes.ModuleName))
	require.NotNil(t, f.accountKeeper.GetModuleAccount(f.ctx, types.ModuleAccountName))

	contribution := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100))
	require.NoError(t, f.bankKeeper.MintCoins(f.ctx, creditstypes.ModuleName, contribution))
	require.NoError(t, f.keeper.ContributeToPool(f.ctx, receiptID, "tool-1", "pub-1", "v1", "", contribution))

	claimID, err := f.keeper.FileClaim(f.ctx, &types.MsgFileClaim{
		ReceiptId:     receiptID,
		Claimant:      claimant,
		ToolId:        "tool-1",
		PublisherId:   "pub-1",
		ClaimedAmount: sdk.Coin{Denom: "ulac", Amount: sdkmath.NewInt(50)},
		Reason:        "test",
	})
	require.NoError(t, err)
	return claimID
}
