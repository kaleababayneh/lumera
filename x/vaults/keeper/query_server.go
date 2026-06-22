package keeper

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/vaults/types"
)

// queryServer implements types.QueryServer.
type queryServer struct {
	types.UnimplementedQueryServer
	Keeper *Keeper
}

// NewQueryServer constructs a query server.
func NewQueryServer(keeper *Keeper) types.QueryServer {
	return &queryServer{Keeper: keeper}
}

func (q *queryServer) requireKeeper() (*Keeper, error) {
	if q == nil || q.Keeper == nil {
		return nil, fmt.Errorf("vaults keeper not initialized")
	}
	return q.Keeper, nil
}

// maxVaultQueryLimit caps results returned by list queries to prevent DoS.
const maxVaultQueryLimit = 1000

func validateQueryVaultID(id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("vault id is required")
	}
	if trimmed != id {
		return fmt.Errorf("vault id must not contain leading or trailing whitespace")
	}
	if len(id) > types.MaxVaultIDLen {
		return fmt.Errorf("vault id exceeds %d-byte cap (got %d)", types.MaxVaultIDLen, len(id))
	}
	return nil
}

// Vault returns a single vault by id.
func (q *queryServer) Vault(ctx context.Context, req *types.QueryVaultRequest) (*types.QueryVaultResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}
	if err := validateQueryVaultID(req.Id); err != nil {
		return nil, err
	}
	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}
	vault, found, err := keeper.GetVault(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if !found {
		return &types.QueryVaultResponse{}, nil
	}
	return &types.QueryVaultResponse{Vault: cloneVault(vault)}, nil
}

// Vaults lists vaults for an owner (if provided) or all vaults.
func (q *queryServer) Vaults(ctx context.Context, req *types.QueryVaultsRequest) (*types.QueryVaultsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}

	var owner string
	if req.Owner != "" {
		if _, err := sdk.AccAddressFromBech32(req.Owner); err != nil {
			return nil, types.ErrInvalidOwner
		}
		owner = req.Owner
	}

	keeper, err := q.requireKeeper()
	if err != nil {
		return nil, err
	}

	response := &types.QueryVaultsResponse{}

	if owner != "" {
		count := 0
		err := keeper.IterateVaultsByOwner(ctx, owner, func(v *types.Vault) bool {
			response.Vaults = append(response.Vaults, cloneVault(v))
			count++
			return count >= maxVaultQueryLimit
		})
		if err != nil {
			return nil, err
		}
		return response, nil
	}

	count := 0
	err = keeper.vaults.Walk(ctx, nil, func(_ string, vault *types.Vault) (bool, error) {
		if vault == nil {
			return count >= maxVaultQueryLimit, nil
		}

		hydrated, err := keeper.hydrateVault(ctx, vault)
		if err != nil {
			return false, err
		}
		if hydrated != nil {
			response.Vaults = append(response.Vaults, cloneVault(hydrated))
			count++
		}
		return count >= maxVaultQueryLimit, nil
	})
	if err != nil {
		return nil, err
	}

	return response, nil
}

func cloneVault(vault *types.Vault) *types.Vault {
	return deepCopyVault(vault)
}
