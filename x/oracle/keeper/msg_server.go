
package keeper

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

type msgServer struct {
	types.UnimplementedMsgServer
	keeper *Keeper
}

// NewMsgServerImpl returns an implementation of the oracle Msg service.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return &msgServer{keeper: k}
}

func recoverOracle(action string, err *error) {
	if r := recover(); r != nil {
		switch v := r.(type) {
		case error:
			*err = sdkerrors.ErrPanic.Wrapf("%s panic: %v", action, v)
		default:
			*err = sdkerrors.ErrPanic.Wrapf("%s panic: %v", action, v)
		}
	}
}

func (s *msgServer) InjectOracleVotes(goCtx context.Context, msg *types.MsgInjectOracleVotes) (resp *types.MsgInjectOracleVotesResponse, err error) {
	defer recoverOracle("oracle/InjectOracleVotes", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	if s == nil || s.keeper == nil {
		return nil, fmt.Errorf("oracle keeper not initialized")
	}
	keeper := s.keeper

	sdkCtx := sdk.UnwrapSDKContext(goCtx)

	if err := keeper.ValidateAuthority(msg.Authority); err != nil {
		return nil, err
	}

	expectedHeight := sdkCtx.BlockHeight() - 1
	if expectedHeight > 0 && msg.Height != expectedHeight {
		return nil, fmt.Errorf("unexpected injected height %d (expected %d)", msg.Height, expectedHeight)
	}

	// Enforce deterministic ordering and uniqueness over validator addresses.
	var lastAddr []byte
	for idx, injected := range msg.Votes {
		if injected == nil {
			return nil, fmt.Errorf("vote[%d] is nil", idx)
		}
		if len(injected.ValidatorAddress) == 0 {
			return nil, fmt.Errorf("vote[%d] validator address is empty", idx)
		}
		if idx > 0 && bytes.Compare(injected.ValidatorAddress, lastAddr) <= 0 {
			return nil, fmt.Errorf("votes must be sorted by validator address (index %d)", idx)
		}
		lastAddr = injected.ValidatorAddress
	}

	for idx, injected := range msg.Votes {
		if injected == nil || len(injected.VoteExtension) == 0 {
			continue
		}

		vote, err := types.ParseVoteExtension(injected.VoteExtension)
		if err != nil {
			return nil, fmt.Errorf("vote[%d] parse: %w", idx, err)
		}

		expectedValidator := sdk.ConsAddress(injected.ValidatorAddress).String()
		rewardAddr := strings.TrimSpace(vote.ValidatorAddress)
		if rewardAddr != "" {
			if _, err := sdk.AccAddressFromBech32(rewardAddr); err == nil {
				if err := keeper.setRewardAddress(sdkCtx, expectedValidator, rewardAddr); err != nil {
					return nil, fmt.Errorf("vote[%d] reward address: %w", idx, err)
				}
			} else {
				keeper.Logger(sdkCtx).Warn("invalid oracle reward address; skipping",
					"validator", expectedValidator,
					"reward_address", rewardAddr,
					"error", err,
				)
			}
		}
		// The vote extension payload is non-authoritative for validator identity; tie
		// the stored vote to the consensus validator address included in the injected entry.
		vote.ValidatorAddress = expectedValidator

		if msg.Height > 0 && vote.GetBlockHeight() != msg.Height {
			return nil, fmt.Errorf("vote[%d] height mismatch (expected %d got %d)", idx, msg.Height, vote.GetBlockHeight())
		}

		if err := keeper.ValidateVote(sdkCtx, vote); err != nil {
			return nil, fmt.Errorf("vote[%d] invalid: %w", idx, err)
		}

		if err := keeper.SetValidatorVote(sdkCtx, vote); err != nil {
			return nil, fmt.Errorf("vote[%d] store: %w", idx, err)
		}
	}

	if err := keeper.AggregateVotes(sdkCtx); err != nil {
		return nil, err
	}

	resp = &types.MsgInjectOracleVotesResponse{}
	return resp, nil
}
