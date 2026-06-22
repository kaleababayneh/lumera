package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/vaults/types"
)

// msgServer implements types.MsgServer.
type msgServer struct {
	types.UnimplementedMsgServer
	Keeper *Keeper
}

// NewMsgServer returns a new msg server instance.
func NewMsgServer(keeper *Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

func recoverVaults(action string, err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s panic: %v", action, r)
	}
}

// CreateVault handles MsgCreateVault requests.
func (s *msgServer) CreateVault(goCtx context.Context, msg *types.MsgCreateVault) (resp *types.MsgCreateVaultResponse, err error) {
	defer recoverVaults("vaults/CreateVault", &err)
	if msg == nil {
		return nil, fmt.Errorf("empty request")
	}
	if err := msg.ValidateBasic(); err != nil {
		return nil, err
	}
	if s == nil || s.Keeper == nil {
		return nil, fmt.Errorf("vaults keeper not initialized")
	}
	sdkCtx := sdk.UnwrapSDKContext(goCtx)

	vault, err := s.Keeper.CreateVault(sdkCtx, msg)
	if err != nil {
		return nil, err
	}

	return &types.MsgCreateVaultResponse{Vault: vault}, nil
}
