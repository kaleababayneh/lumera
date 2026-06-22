package types

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GenesisState defines the initial state for the vaults module.
type GenesisState struct {
	Vaults []*Vault `json:"vaults"`
	NextID uint64   `json:"next_id"`
}

// DefaultGenesis returns the default genesis state for the module.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Vaults: []*Vault{},
		NextID: 0,
	}
}

// Validate performs basic genesis state validation checks.
func (gs *GenesisState) Validate() error {
	if gs == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}

	seen := make(map[string]struct{}, len(gs.Vaults))
	for i, vault := range gs.Vaults {
		if vault == nil {
			return fmt.Errorf("vault entry %d is nil", i)
		}
		if err := validateVault(vault); err != nil {
			return fmt.Errorf("invalid vault %d: %w", i, err)
		}
		if _, exists := seen[vault.Id]; exists {
			return fmt.Errorf("duplicate vault id %s in genesis", vault.Id)
		}
		seen[vault.Id] = struct{}{}
	}

	inferredNextID, err := inferNextVaultID(gs.Vaults)
	if err != nil {
		return err
	}
	if gs.NextID != 0 && gs.NextID < inferredNextID {
		return fmt.Errorf("next_id %d would reuse imported vault id; must be 0 or >= %d", gs.NextID, inferredNextID)
	}

	return nil
}

func inferNextVaultID(vaults []*Vault) (uint64, error) {
	var next uint64
	for _, vault := range vaults {
		if vault == nil {
			continue
		}
		suffix, ok := strings.CutPrefix(vault.Id, "vault-")
		if !ok {
			continue
		}
		seq, err := strconv.ParseUint(suffix, 10, 64)
		if err != nil {
			continue
		}
		if seq == math.MaxUint64 {
			return 0, fmt.Errorf("vault id %s leaves no allocatable next_id", vault.Id)
		}
		if seq >= next {
			next = seq + 1
		}
	}
	return next, nil
}

func validateVault(vault *Vault) error {
	if err := validateVaultIdentifier("vault_id", vault.Id, true, fmt.Errorf("vault id required")); err != nil {
		return err
	}
	if strings.TrimSpace(vault.Owner) == "" {
		return fmt.Errorf("owner required")
	}
	if _, err := sdk.AccAddressFromBech32(vault.Owner); err != nil {
		return fmt.Errorf("invalid owner: %w", err)
	}
	if err := validateVaultIdentifier("policy_id", vault.PolicyId, true, fmt.Errorf("policy id required")); err != nil {
		return err
	}
	if err := validateVaultIdentifier("tool_id", vault.ToolId, false, nil); err != nil {
		return err
	}
	if err := validateVaultIdentifier("commitment_id", vault.CommitmentId, false, nil); err != nil {
		return err
	}
	if err := validateVaultStringCap("tier", vault.Tier); err != nil {
		return err
	}
	trimmedTier := strings.TrimSpace(vault.Tier)
	if trimmedTier != "" && trimmedTier != vault.Tier {
		return fmt.Errorf("tier must not contain leading or trailing whitespace")
	}
	prepaidAmount, err := validateVaultCoin("prepaid amount", vault.PrepaidAmount, true)
	if err != nil {
		return err
	}
	if trimmedTier == "" {
		return fmt.Errorf("tier required")
	}
	if !vault.RemainingAmount.Amount.IsNil() {
		remainingAmount, err := validateVaultCoin("remaining amount", vault.RemainingAmount, false)
		if err != nil {
			return err
		}
		if vault.RemainingAmount.Denom != vault.PrepaidAmount.Denom {
			return fmt.Errorf("remaining amount denom %s does not match prepaid amount denom %s",
				vault.RemainingAmount.Denom, vault.PrepaidAmount.Denom)
		}
		if remainingAmount.GT(prepaidAmount) {
			return fmt.Errorf("remaining amount cannot exceed prepaid amount")
		}
	}
	if vault.DiscountBps > 10_000 {
		return fmt.Errorf("discount_bps exceeds 100%%")
	}
	if !vault.StartTime.IsZero() && !vault.ExpireTime.IsZero() && !vault.ExpireTime.After(vault.StartTime) {
		return fmt.Errorf("expire_time must be after start_time")
	}
	return nil
}

func validateVaultStringCap(field, value string) error {
	if len(value) > MaxVaultIDLen {
		return fmt.Errorf("%s exceeds %d-byte cap (got %d)", field, MaxVaultIDLen, len(value))
	}
	return nil
}

func validateVaultCoin(field string, coin sdk.Coin, requirePositive bool) (sdkmath.Int, error) {
	if err := sdk.ValidateDenom(coin.Denom); err != nil {
		return sdkmath.Int{}, fmt.Errorf("%s invalid denom: %w", field, err)
	}
	if coin.Amount.IsNil() {
		return sdkmath.Int{}, fmt.Errorf("%s amount required", field)
	}
	if coin.Amount.IsNegative() || (requirePositive && coin.Amount.IsZero()) {
		if requirePositive {
			return sdkmath.Int{}, fmt.Errorf("%s amount must be positive", field)
		}
		return sdkmath.Int{}, fmt.Errorf("%s amount must be non-negative", field)
	}
	return coin.Amount, nil
}
