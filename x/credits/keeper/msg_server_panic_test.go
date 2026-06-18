
package keeper

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

func TestMsgServerNilKeeperRejected(t *testing.T) {
	server := NewMsgServerImpl(nil)
	ctx := context.Background()
	router := newAccAddress().String()
	publisher := newAccAddress().String()

	tests := []struct {
		name string
		call func(context.Context, types.MsgServer) error
	}{
		{
			name: "lock credits",
			call: func(ctx context.Context, server types.MsgServer) error {
				resp, err := server.LockCredits(ctx, &types.MsgLockCredits{
					Router:    router,
					SessionId: "session-nil-keeper",
					ToolId:    "tool-1",
					Amount:    protoCoin("ulac", "100"),
				})
				require.Nil(t, resp)
				return err
			},
		},
		{
			name: "settle credits",
			call: func(ctx context.Context, server types.MsgServer) error {
				resp, err := server.SettleCredits(ctx, &types.MsgSettleCredits{
					Router:     router,
					LockId:     "lock-1",
					ReceiptId:  "receipt-1",
					ToolId:     "tool-1",
					Publisher:  publisher,
					ActualCost: protoCoin("ulac", "100"),
				})
				require.Nil(t, resp)
				return err
			},
		},
		{
			name: "unlock credits",
			call: func(ctx context.Context, server types.MsgServer) error {
				resp, err := server.UnlockCredits(ctx, &types.MsgUnlockCredits{
					Router: router,
					LockId: "lock-1",
				})
				require.Nil(t, resp)
				return err
			},
		},
		{
			name: "update params",
			call: func(ctx context.Context, server types.MsgServer) error {
				resp, err := server.UpdateParams(ctx, &types.MsgUpdateParams{
					Authority: router,
				})
				require.Nil(t, resp)
				return err
			},
		},
		{
			name: "swap LUME to LAC",
			call: func(ctx context.Context, server types.MsgServer) error {
				resp, err := server.SwapLUMEtoLAC(ctx, &types.MsgSwapLUMEtoLAC{
					Sender:     router,
					LumeAmount: protoCoin("ulume", "100"),
				})
				require.Nil(t, resp)
				return err
			},
		},
		{
			name: "swap LAC to LUME",
			call: func(ctx context.Context, server types.MsgServer) error {
				resp, err := server.SwapLACtoLUME(ctx, &types.MsgSwapLACtoLUME{
					Sender:    router,
					LacAmount: protoCoin("ulac", "100"),
				})
				require.Nil(t, resp)
				return err
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(ctx, server)
			require.Error(t, err)
			require.False(t, sdkerrors.ErrPanic.Is(err), "nil keeper should not rely on panic recovery: %v", err)
			require.Contains(t, err.Error(), "credits keeper not initialized")
		})
	}
}

func TestMsgServerLockCreditsValidatesBeforeSDKContext(t *testing.T) {
	server := &msgServer{}
	ctx := context.Background()

	_, err := server.LockCredits(ctx, &types.MsgLockCredits{
		Router:    "not-bech32",
		SessionId: "session-validation",
		ToolId:    "tool-1",
		Amount:    protoCoin("ulac", "100"),
	})
	require.Error(t, err)
	require.False(t, sdkerrors.ErrPanic.Is(err), "invalid messages should not reach SDK context unwrap: %v", err)
	require.Contains(t, err.Error(), "invalid router address")
}
