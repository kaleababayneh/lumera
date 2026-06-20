
package keeper_test

import (
	"context"
	"crypto/sha256"
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	creditstypes "github.com/LumeraProtocol/lumera/x/credits/types"
	"github.com/LumeraProtocol/lumera/x/insurance/keeper"
	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

func newMsgSrv(t *testing.T) (*keeperFixture, types.MsgServer) {
	t.Helper()
	f := setupKeeperTest(t)
	return f, keeper.NewMsgServerImpl(f.keeper)
}

func insuranceMsgAddress(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return sdk.AccAddress(sum[:20]).String()
}

func TestMsgServer_ValidatesStatelessFieldsBeforeKeeperAccess(t *testing.T) {
	srv := keeper.NewMsgServerImpl(keeper.Keeper{})
	authority := insuranceMsgAddress("authority")
	recipient := insuranceMsgAddress("recipient")

	tests := []struct {
		name string
		call func() error
		want string
	}{
		{
			name: "contribution invalid authority",
			call: func() error {
				_, err := srv.ProcessContribution(context.Background(), &types.MsgProcessContribution{
					Authority:   "not-a-valid-address",
					ReceiptId:   "receipt-1",
					ToolId:      "tool-1",
					PublisherId: "publisher-1",
					Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
				})
				return err
			},
			want: "invalid authority address",
		},
		{
			name: "payout padded claim id",
			call: func() error {
				_, err := srv.ProcessPayout(context.Background(), &types.MsgProcessPayout{
					Authority: authority,
					ClaimId:   " claim-1",
					Recipient: recipient,
					Amount:    &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
				})
				return err
			},
			want: "claim_id must be canonical",
		},
		{
			name: "publisher risk invalid premium tier",
			call: func() error {
				_, err := srv.UpdatePublisherRisk(context.Background(), &types.MsgUpdatePublisherRisk{
					Authority:    authority,
					PublisherId:  "publisher-1",
					ToolId:       "tool-1",
					RiskScoreBps: 500,
					PremiumTier:  "critical",
				})
				return err
			},
			want: "invalid premium_tier",
		},
		{
			name: "params nil",
			call: func() error {
				_, err := srv.UpdateParams(context.Background(), &types.MsgUpdateParams{
					Authority: authority,
					Params:    nil,
				})
				return err
			},
			want: "params",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			require.NotPanics(t, func() {
				err = tt.call()
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
			require.NotContains(t, err.Error(), "keeper not initialized")
		})
	}
}

func TestMsgServer_RejectsUninitializedKeeperBeforeStateAccess(t *testing.T) {
	srv := keeper.NewMsgServerImpl(keeper.Keeper{})
	authority := insuranceMsgAddress("authority")
	claimant := insuranceMsgAddress("claimant")
	recipient := insuranceMsgAddress("recipient")

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "process contribution",
			call: func() error {
				_, err := srv.ProcessContribution(context.Background(), &types.MsgProcessContribution{
					Authority:   authority,
					ReceiptId:   "receipt-1",
					ToolId:      "tool-1",
					PublisherId: "publisher-1",
					Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
				})
				return err
			},
		},
		{
			name: "file claim",
			call: func() error {
				_, err := srv.FileClaim(context.Background(), &types.MsgFileClaim{
					Claimant:      claimant,
					ReceiptId:     "receipt-1",
					ToolId:        "tool-1",
					PublisherId:   "publisher-1",
					ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
					Reason:        "covered failure",
				})
				return err
			},
		},
		{
			name: "process claim",
			call: func() error {
				_, err := srv.ProcessClaim(context.Background(), &types.MsgProcessClaim{
					Authority:  authority,
					ClaimId:    "claim-1",
					Resolution: "approve",
				})
				return err
			},
		},
		{
			name: "process payout",
			call: func() error {
				_, err := srv.ProcessPayout(context.Background(), &types.MsgProcessPayout{
					Authority: authority,
					ClaimId:   "claim-1",
					Recipient: recipient,
					Amount:    &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
				})
				return err
			},
		},
		{
			name: "update publisher risk",
			call: func() error {
				_, err := srv.UpdatePublisherRisk(context.Background(), &types.MsgUpdatePublisherRisk{
					Authority:    authority,
					PublisherId:  "publisher-1",
					ToolId:       "tool-1",
					RiskScoreBps: 500,
					PremiumTier:  "standard",
				})
				return err
			},
		},
		{
			name: "update params",
			call: func() error {
				_, err := srv.UpdateParams(context.Background(), &types.MsgUpdateParams{
					Authority: authority,
					Params:    types.DefaultParams(),
				})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			require.NotPanics(t, func() {
				err = tt.call()
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), "insurance keeper not initialized")
		})
	}
}

// --- ProcessContribution ---

func TestMsgServer_ProcessContribution_NilMsg(t *testing.T) {
	_, srv := newMsgSrv(t)

	resp, err := srv.ProcessContribution(sdk.Context{}, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestMsgServer_ProcessContribution_WrongAuthority(t *testing.T) {
	_, srv := newMsgSrv(t)
	wrongAuthority := insuranceMsgAddress("wrong-authority")

	resp, err := srv.ProcessContribution(sdk.Context{}, &types.MsgProcessContribution{
		Authority:   wrongAuthority,
		ReceiptId:   "receipt-1",
		ToolId:      "tool-1",
		PublisherId: "pub-1",
		Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestMsgServer_ProcessContribution_MissingReceiptID(t *testing.T) {
	_, srv := newMsgSrv(t)
	authority := authtypes.NewModuleAddress("gov").String()

	resp, err := srv.ProcessContribution(sdk.Context{}, &types.MsgProcessContribution{
		Authority: authority,
		ReceiptId: "",
		Amount:    &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "receipt_id")
}

func TestMsgServer_ProcessContribution_NilAmount(t *testing.T) {
	_, srv := newMsgSrv(t)
	authority := authtypes.NewModuleAddress("gov").String()

	resp, err := srv.ProcessContribution(sdk.Context{}, &types.MsgProcessContribution{
		Authority: authority,
		ReceiptId: "receipt-1",
		Amount:    nil,
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "amount")
}

func TestMsgServer_ProcessContribution_InvalidAmount(t *testing.T) {
	_, srv := newMsgSrv(t)
	authority := authtypes.NewModuleAddress("gov").String()

	resp, err := srv.ProcessContribution(sdk.Context{}, &types.MsgProcessContribution{
		Authority:   authority,
		ReceiptId:   "receipt-1",
		ToolId:      "tool-1",
		PublisherId: "pub-1",
		Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "not-a-number"},
	})
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestMsgServer_ProcessContribution_ZeroAmount(t *testing.T) {
	_, srv := newMsgSrv(t)
	authority := authtypes.NewModuleAddress("gov").String()

	resp, err := srv.ProcessContribution(sdk.Context{}, &types.MsgProcessContribution{
		Authority:   authority,
		ReceiptId:   "receipt-1",
		ToolId:      "tool-1",
		PublisherId: "pub-1",
		Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "0"},
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "positive")
}

func TestMsgServer_ProcessContribution_RejectsValidateBasicFailures(t *testing.T) {
	_, srv := newMsgSrv(t)
	authority := authtypes.NewModuleAddress("gov").String()

	tests := []struct {
		name   string
		mutate func(*types.MsgProcessContribution)
		want   string
	}{
		{
			name: "padded receipt id",
			mutate: func(msg *types.MsgProcessContribution) {
				msg.ReceiptId = " receipt-1"
			},
			want: "receipt_id must be canonical",
		},
		{
			name: "empty tool id",
			mutate: func(msg *types.MsgProcessContribution) {
				msg.ToolId = ""
			},
			want: "tool_id is required",
		},
		{
			name: "padded tool id",
			mutate: func(msg *types.MsgProcessContribution) {
				msg.ToolId = "tool-1 "
			},
			want: "tool_id must be canonical",
		},
		{
			name: "empty publisher id",
			mutate: func(msg *types.MsgProcessContribution) {
				msg.PublisherId = ""
			},
			want: "publisher_id is required",
		},
		{
			name: "padded publisher id",
			mutate: func(msg *types.MsgProcessContribution) {
				msg.PublisherId = "\tpub-1"
			},
			want: "publisher_id must be canonical",
		},
		{
			name: "unsafe amount exponent",
			mutate: func(msg *types.MsgProcessContribution) {
				msg.Amount = &basev1beta1.Coin{Denom: "ulac", Amount: "1e11100100"}
			},
			want: "magnitude out of range",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &types.MsgProcessContribution{
				Authority:   authority,
				ReceiptId:   "receipt-1",
				ToolId:      "tool-1",
				PublisherId: "pub-1",
				Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
			}
			tt.mutate(msg)

			resp, err := srv.ProcessContribution(sdk.Context{}, msg)
			require.Error(t, err)
			require.Nil(t, resp)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestMsgServer_ProcessContribution_HappyPath(t *testing.T) {
	f, srv := newMsgSrv(t)
	authority := authtypes.NewModuleAddress("gov").String()

	// Fund module accounts
	creditsAccount := f.accountKeeper.GetModuleAccount(f.ctx, creditstypes.ModuleName)
	require.NotNil(t, creditsAccount)
	_ = f.accountKeeper.GetModuleAccount(f.ctx, types.ModuleAccountName)

	initialCoins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 1_000_000))
	require.NoError(t, f.bankKeeper.MintCoins(f.ctx, creditstypes.ModuleName, initialCoins))

	resp, err := srv.ProcessContribution(f.ctx, &types.MsgProcessContribution{
		Authority:   authority,
		ReceiptId:   "receipt-happy",
		ToolId:      "tool-1",
		PublisherId: "pub-1",
		Amount:      &basev1beta1.Coin{Denom: "ulac", Amount: "200"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// --- FileClaim ---

func TestMsgServer_FileClaim_NilMsg(t *testing.T) {
	_, srv := newMsgSrv(t)

	resp, err := srv.FileClaim(sdk.Context{}, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestMsgServer_FileClaim_MissingReceiptID(t *testing.T) {
	_, srv := newMsgSrv(t)

	resp, err := srv.FileClaim(sdk.Context{}, &types.MsgFileClaim{
		ReceiptId:     "",
		Claimant:      "lumera1user",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "50"},
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "receipt_id")
}

func TestMsgServer_FileClaim_MissingClaimant(t *testing.T) {
	_, srv := newMsgSrv(t)

	resp, err := srv.FileClaim(sdk.Context{}, &types.MsgFileClaim{
		ReceiptId:     "receipt-1",
		Claimant:      "",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "50"},
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "claimant")
}

func TestMsgServer_FileClaim_NilClaimedAmount(t *testing.T) {
	_, srv := newMsgSrv(t)

	resp, err := srv.FileClaim(sdk.Context{}, &types.MsgFileClaim{
		ReceiptId:     "receipt-1",
		Claimant:      "lumera1user",
		ClaimedAmount: nil,
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "claimed_amount")
}

func TestMsgServer_FileClaim_RejectsValidateBasicFailures(t *testing.T) {
	_, srv := newMsgSrv(t)
	claimant := sdk.AccAddress([]byte("claimant_msg_server_")).String()

	tests := map[string]struct {
		mutate func(*types.MsgFileClaim)
		want   string
	}{
		"invalid claimant": {
			mutate: func(msg *types.MsgFileClaim) {
				msg.Claimant = "not-a-valid-address"
			},
			want: "invalid claimant address",
		},
		"padded receipt id": {
			mutate: func(msg *types.MsgFileClaim) {
				msg.ReceiptId = " receipt-fc"
			},
			want: "receipt_id must be canonical",
		},
		"empty tool id": {
			mutate: func(msg *types.MsgFileClaim) {
				msg.ToolId = ""
			},
			want: "tool_id is required",
		},
		"empty reason": {
			mutate: func(msg *types.MsgFileClaim) {
				msg.Reason = " \t"
			},
			want: "reason is required",
		},
		"padded reason": {
			mutate: func(msg *types.MsgFileClaim) {
				msg.Reason = " service failure "
			},
			want: "reason must be canonical",
		},
		"unsafe amount exponent": {
			mutate: func(msg *types.MsgFileClaim) {
				msg.ClaimedAmount = &basev1beta1.Coin{Denom: "ulac", Amount: "1e11100100"}
			},
			want: "claimed_amount amount magnitude out of range",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			msg := &types.MsgFileClaim{
				ReceiptId:     "receipt-fc",
				Claimant:      claimant,
				ToolId:        "tool-1",
				ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "50"},
				Reason:        "service failure",
			}
			tc.mutate(msg)

			resp, err := srv.FileClaim(sdk.Context{}, msg)
			require.Error(t, err)
			require.Nil(t, resp)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestMsgServer_FileClaim_HappyPath(t *testing.T) {
	f, srv := newMsgSrv(t)

	// Fund and contribute first
	creditsAccount := f.accountKeeper.GetModuleAccount(f.ctx, creditstypes.ModuleName)
	require.NotNil(t, creditsAccount)
	_ = f.accountKeeper.GetModuleAccount(f.ctx, types.ModuleAccountName)

	initialCoins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 1_000_000))
	require.NoError(t, f.bankKeeper.MintCoins(f.ctx, creditstypes.ModuleName, initialCoins))

	contribution := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100))
	require.NoError(t, f.keeper.ContributeToPool(f.ctx, "receipt-fc", "tool-1", "pub-1", "v1", "", contribution))

	resp, err := srv.FileClaim(f.ctx, &types.MsgFileClaim{
		ReceiptId:     "receipt-fc",
		Claimant:      sdk.AccAddress([]byte("claimant_msg_server_")).String(),
		ToolId:        "tool-1",
		PublisherId:   "pub-1",
		ClaimedAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "50"},
		Reason:        "service failure",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotEmpty(t, resp.ClaimId)
}

// --- ProcessClaim ---

func TestMsgServer_ProcessClaim_NilMsg(t *testing.T) {
	_, srv := newMsgSrv(t)

	resp, err := srv.ProcessClaim(sdk.Context{}, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestMsgServer_ProcessClaim_RejectsValidateBasicFailures(t *testing.T) {
	_, srv := newMsgSrv(t)
	authority := authtypes.NewModuleAddress("gov").String()

	tests := map[string]struct {
		msg  *types.MsgProcessClaim
		want string
	}{
		"padded claim id": {
			msg: &types.MsgProcessClaim{
				Authority:  authority,
				ClaimId:    " claim-1",
				Resolution: "approve",
			},
			want: "claim_id must be canonical",
		},
		"padded resolution": {
			msg: &types.MsgProcessClaim{
				Authority:  authority,
				ClaimId:    "claim-1",
				Resolution: " approve ",
			},
			want: "resolution must be approve, reject, or partial",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			resp, err := srv.ProcessClaim(sdk.Context{}, tc.msg)
			require.Error(t, err)
			require.Nil(t, resp)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

// --- ProcessPayout ---

func TestMsgServer_ProcessPayout_NilMsg(t *testing.T) {
	_, srv := newMsgSrv(t)

	resp, err := srv.ProcessPayout(sdk.Context{}, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestMsgServer_ProcessPayout_WrongAuthority(t *testing.T) {
	_, srv := newMsgSrv(t)
	wrongAuthority := insuranceMsgAddress("wrong-authority")
	recipient := insuranceMsgAddress("recipient")

	resp, err := srv.ProcessPayout(sdk.Context{}, &types.MsgProcessPayout{
		Authority: wrongAuthority,
		ClaimId:   "claim-1",
		Recipient: recipient,
		Amount:    &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestMsgServer_ProcessPayout_MissingClaimID(t *testing.T) {
	_, srv := newMsgSrv(t)
	authority := authtypes.NewModuleAddress("gov").String()

	resp, err := srv.ProcessPayout(sdk.Context{}, &types.MsgProcessPayout{
		Authority: authority,
		ClaimId:   "",
		Recipient: "lumera1recipient",
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "claim_id")
}

func TestMsgServer_ProcessPayout_MissingRecipient(t *testing.T) {
	_, srv := newMsgSrv(t)
	authority := authtypes.NewModuleAddress("gov").String()

	resp, err := srv.ProcessPayout(sdk.Context{}, &types.MsgProcessPayout{
		Authority: authority,
		ClaimId:   "claim-1",
		Recipient: "",
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "recipient")
}

func TestMsgServer_ProcessPayout_InvalidRecipient(t *testing.T) {
	_, srv := newMsgSrv(t)
	authority := authtypes.NewModuleAddress("gov").String()

	resp, err := srv.ProcessPayout(sdk.Context{}, &types.MsgProcessPayout{
		Authority: authority,
		ClaimId:   "claim-1",
		Recipient: "not-a-valid-bech32",
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "invalid recipient")
}

func TestMsgServer_ProcessPayout_RejectsPaddedClaimIDBeforeSDKContext(t *testing.T) {
	_, srv := newMsgSrv(t)
	authority := authtypes.NewModuleAddress("gov").String()
	recipient := sdk.AccAddress([]byte("insurance_recipient_")).String()

	resp, err := srv.ProcessPayout(context.Background(), &types.MsgProcessPayout{
		Authority: authority,
		ClaimId:   " claim-1",
		Recipient: recipient,
		Amount:    &basev1beta1.Coin{Denom: "ulac", Amount: "10"},
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "claim_id must be canonical")
}

// --- UpdateParams ---

func TestMsgServer_UpdateParams_NilMsg(t *testing.T) {
	_, srv := newMsgSrv(t)

	resp, err := srv.UpdateParams(sdk.Context{}, nil)
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "message cannot be nil")
}

func TestMsgServer_UpdateParams_WrongAuthority(t *testing.T) {
	_, srv := newMsgSrv(t)
	wrongAuthority := insuranceMsgAddress("wrong-authority")

	resp, err := srv.UpdateParams(sdk.Context{}, &types.MsgUpdateParams{
		Authority: wrongAuthority,
		Params:    types.DefaultParams(),
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestMsgServer_UpdateParams_NilParams(t *testing.T) {
	_, srv := newMsgSrv(t)
	authority := authtypes.NewModuleAddress("gov").String()

	resp, err := srv.UpdateParams(sdk.Context{}, &types.MsgUpdateParams{
		Authority: authority,
		Params:    nil,
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "params")
}

func TestMsgServer_UpdateParams_PersistsParamsAndEmitsEvent(t *testing.T) {
	f, srv := newMsgSrv(t)
	authority := authtypes.NewModuleAddress("gov").String()
	params := types.DefaultParams()
	params.InsurancePoolBps = 725
	params.MaxClaimsPerBlock = 17

	resp, err := srv.UpdateParams(f.ctx, &types.MsgUpdateParams{
		Authority: authority,
		Params:    params,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	persisted := f.keeper.GetParams(f.ctx)
	require.Equal(t, uint32(725), persisted.InsurancePoolBps)
	require.Equal(t, uint32(17), persisted.MaxClaimsPerBlock)

	var found bool
	for _, evt := range f.ctx.EventManager().Events() {
		if evt.Type != types.EventTypeParamsUpdated {
			continue
		}
		for _, attr := range evt.Attributes {
			if attr.Key == types.AttributeKeyAuthority && attr.Value == authority {
				found = true
			}
		}
	}
	require.True(t, found, "expected params update event with authority attribute")
}

// --- UpdatePublisherRisk ---

func TestMsgServer_UpdatePublisherRisk_NilMsg(t *testing.T) {
	_, srv := newMsgSrv(t)

	resp, err := srv.UpdatePublisherRisk(sdk.Context{}, nil)
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "message cannot be nil")
}

func TestMsgServer_UpdatePublisherRisk_WrongAuthority(t *testing.T) {
	f, srv := newMsgSrv(t)
	wrongAuthority := insuranceMsgAddress("wrong-authority")

	resp, err := srv.UpdatePublisherRisk(f.ctx, &types.MsgUpdatePublisherRisk{
		Authority:    wrongAuthority,
		PublisherId:  "publisher-risk",
		ToolId:       "tool-risk",
		RiskScoreBps: 500,
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestMsgServer_UpdatePublisherRisk_InvalidPremiumTier(t *testing.T) {
	f, srv := newMsgSrv(t)
	authority := authtypes.NewModuleAddress("gov").String()

	resp, err := srv.UpdatePublisherRisk(f.ctx, &types.MsgUpdatePublisherRisk{
		Authority:    authority,
		PublisherId:  "publisher-risk",
		ToolId:       "tool-risk",
		RiskScoreBps: 500,
		PremiumTier:  "critical",
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "invalid premium_tier")
}

func TestMsgServer_UpdatePublisherRisk_PersistsRiskAndEmitsEvent(t *testing.T) {
	f, srv := newMsgSrv(t)
	authority := authtypes.NewModuleAddress("gov").String()

	resp, err := srv.UpdatePublisherRisk(f.ctx, &types.MsgUpdatePublisherRisk{
		Authority:    authority,
		PublisherId:  "publisher-risk",
		ToolId:       "tool-risk",
		RiskScoreBps: 7_250,
		PremiumTier:  "high",
		Notes:        "manual review",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	risk, err := keeper.NewHooks(f.keeper).GetPublisherRisk(f.ctx, "publisher-risk", "tool-risk")
	require.NoError(t, err)
	require.Equal(t, "publisher-risk", risk.PublisherId)
	require.Equal(t, "tool-risk", risk.ToolId)
	require.Equal(t, "72.50", risk.RiskScore)
	require.Equal(t, types.PremiumTier_PREMIUM_TIER_HIGH, risk.PremiumTier)
	require.Equal(t, "1.5", risk.PremiumMultiplier)
	require.Equal(t, "1.0", risk.SuccessRate)
	require.NotNil(t, risk.LastEvaluated)

	var found bool
	for _, evt := range f.ctx.EventManager().Events() {
		if evt.Type != types.EventTypePublisherRiskUpdated {
			continue
		}
		attrs := map[string]string{}
		for _, attr := range evt.Attributes {
			attrs[attr.Key] = attr.Value
		}
		if attrs[types.AttributeKeyAuthority] == authority &&
			attrs["publisher_id"] == "publisher-risk" &&
			attrs[types.AttributeKeyToolID] == "tool-risk" &&
			attrs[types.AttributeKeyRiskScore] == "72.50" &&
			attrs[types.AttributeKeyPremiumTier] == types.PremiumTier_PREMIUM_TIER_HIGH.String() &&
			attrs["notes"] == "manual review" {
			found = true
		}
	}
	require.True(t, found, "expected publisher risk update event with risk metadata")
}

func TestMsgServer_UpdatePublisherRisk_DerivesTierFromRiskScore(t *testing.T) {
	f, srv := newMsgSrv(t)
	authority := authtypes.NewModuleAddress("gov").String()

	resp, err := srv.UpdatePublisherRisk(f.ctx, &types.MsgUpdatePublisherRisk{
		Authority:    authority,
		PublisherId:  "publisher-derived",
		ToolId:       "tool-derived",
		RiskScoreBps: 8_500,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	risk, err := keeper.NewHooks(f.keeper).GetPublisherRisk(f.ctx, "publisher-derived", "tool-derived")
	require.NoError(t, err)
	require.Equal(t, types.PremiumTier_PREMIUM_TIER_EXTREME, risk.PremiumTier)
	require.Equal(t, "2.0", risk.PremiumMultiplier)
}
