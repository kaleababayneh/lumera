//go:build cosmos

package keeper

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/oracle/types"
)

// validRewardBech32 builds a valid bech32 sdk.AccAddress string. The
// setRewardAddress guard calls sdk.AccAddressFromBech32 which requires a
// correct checksum; hand-typed strings from docs tend to be stale.
func validRewardBech32(seed string) string {
	b := make([]byte, 20)
	copy(b, seed)
	return sdk.AccAddress(b).String()
}

// setRewardAddress is the gate that stores an alternate payout address
// for an oracle validator. Three validation branches were previously
// unreached by tests (empty validator, empty reward address, invalid
// bech32). Each has real fail-closed significance:
//
//   - Empty validator: a nil key would corrupt the rewardAddresses map,
//     stranding rewards or (worse) letting subsequent lookups hit a
//     ghost entry.
//   - Empty reward address: treated as a no-op rather than an error so
//     governance can revoke alternate addresses by passing "".
//   - Invalid bech32: must be rejected before Set to avoid persisting
//     an address that SendCoinsFromModuleToAccount would later reject
//     on every reward distribution cycle.

// TestSetRewardAddress_RejectsEmptyValidator pins the fail-closed branch
// that prevents storing rewards under an empty key. A silent accept
// here would poison the rewardAddresses map.
func TestSetRewardAddress_RejectsEmptyValidator(t *testing.T) {
	ctx, k := setupOracleKeeper(t)

	err := k.setRewardAddress(ctx, "", validRewardBech32("reward_01__________"))
	require.Error(t, err, "empty validator address must be rejected")
	require.ErrorIs(t, err, types.ErrInvalidRewardAddress,
		"error must wrap ErrInvalidRewardAddress for typed caller branching")
	require.Contains(t, err.Error(), "validator address cannot be empty",
		"error message must identify the specific validation failure")
}

// TestSetRewardAddress_RejectsWhitespaceOnlyValidator pins the
// companion invariant: the empty-check uses TrimSpace, so a
// whitespace-only validator string must also be rejected rather than
// treated as a distinct key "   ".
func TestSetRewardAddress_RejectsWhitespaceOnlyValidator(t *testing.T) {
	ctx, k := setupOracleKeeper(t)

	err := k.setRewardAddress(ctx, "   \t\n", validRewardBech32("reward_01__________"))
	require.Error(t, err, "whitespace-only validator must be rejected")
	require.ErrorIs(t, err, types.ErrInvalidRewardAddress)
}

// TestSetRewardAddress_RejectsPaddedValidator prevents reward addresses
// from being stored under a key that later exact consensus-validator
// lookups cannot reach.
func TestSetRewardAddress_RejectsPaddedValidator(t *testing.T) {
	ctx, k := setupOracleKeeper(t)

	err := k.setRewardAddress(ctx, " lumeravaloper1test ", validRewardBech32("reward_01__________"))
	require.Error(t, err, "padded validator must be rejected")
	require.ErrorIs(t, err, types.ErrInvalidRewardAddress)
	require.Contains(t, err.Error(), "leading or trailing whitespace")

	_, found := k.getRewardAddress(ctx, " lumeravaloper1test ")
	require.False(t, found, "rejected padded validator must not be persisted")
	_, found = k.getRewardAddress(ctx, "lumeravaloper1test")
	require.False(t, found, "rejected padded validator must not be normalized and persisted")
}

// TestSetRewardAddress_EmptyRewardAddressIsNoOp pins the deliberate
// no-op branch: governance uses an empty reward address to clear a
// prior alternate (revert to default). This must NOT error — the
// intent is "use the default payout address" not "this is invalid".
func TestSetRewardAddress_EmptyRewardAddressIsNoOp(t *testing.T) {
	ctx, k := setupOracleKeeper(t)

	err := k.setRewardAddress(ctx, "lumeravaloper1test", "")
	require.NoError(t, err,
		"empty reward address must be a silent no-op — governance uses this to revert")

	// Verify no entry was written.
	addr, found := k.getRewardAddress(ctx, "lumeravaloper1test")
	require.False(t, found, "no-op must not persist any entry")
	require.Empty(t, addr)
}

// TestSetRewardAddress_EmptyRewardAddressDoesNotClearExisting pins a
// subtle invariant: passing "" does NOT delete an existing reward
// address — it is a no-op, not a remove. If governance wants to clear
// an entry, it must use a dedicated remove path (not present in the
// current keeper surface). A regression that turned this into a delete
// would silently revoke alternate payout addresses on every empty call.
func TestSetRewardAddress_EmptyRewardAddressDoesNotClearExisting(t *testing.T) {
	ctx, k := setupOracleKeeper(t)

	// Pre-populate a valid entry.
	const val = "lumeravaloper1existing"
	addr := validRewardBech32("reward_01__________")
	require.NoError(t, k.setRewardAddress(ctx, val, addr),
		"pre-condition: valid entry stored")

	// Now call with empty reward address.
	require.NoError(t, k.setRewardAddress(ctx, val, ""))

	// Entry must still be present.
	got, found := k.getRewardAddress(ctx, val)
	require.True(t, found, "existing entry must survive no-op call")
	require.Equal(t, addr, got,
		"existing reward address must be preserved — no-op must not clear")
}

// TestSetRewardAddress_RejectsInvalidBech32 pins the fail-closed branch
// that prevents persisting an address that cannot be decoded. Without
// this guard, reward distribution would fail every cycle after the
// malformed Set because SendCoinsFromModuleToAccount requires a valid
// sdk.AccAddress — the failure would be in a hot path, not at the Set
// site where it could be surfaced to the caller.
func TestSetRewardAddress_RejectsInvalidBech32(t *testing.T) {
	ctx, k := setupOracleKeeper(t)

	err := k.setRewardAddress(ctx, "lumeravaloper1test", "not-a-bech32-address")
	require.Error(t, err, "invalid bech32 must be rejected at the Set site")
	require.ErrorIs(t, err, types.ErrInvalidRewardAddress)
	require.Contains(t, err.Error(), "invalid reward address",
		"error must identify as a reward-address validation failure")

	// The malformed address must NOT have been persisted.
	_, found := k.getRewardAddress(ctx, "lumeravaloper1test")
	require.False(t, found,
		"rejected address must not leak into state — a Set that errors must not half-apply")
}

// TestSetRewardAddress_HappyPath_ComplementsRejections pins the success
// path as a polarity anchor. Without this, a regression that inverted
// the error return (e.g. returning nil on invalid bech32) would still
// pass the rejection tests if they only checked `err != nil` on invalid
// inputs. By pinning the valid path returns nil AND stores the entry,
// we lock down the semantic contract.
func TestSetRewardAddress_HappyPath_ComplementsRejections(t *testing.T) {
	ctx, k := setupOracleKeeper(t)

	const val = "lumeravaloper1happy"
	addr := validRewardBech32("reward_01__________")
	require.NoError(t, k.setRewardAddress(ctx, val, addr),
		"valid bech32 + non-empty validator must succeed")

	got, found := k.getRewardAddress(ctx, val)
	require.True(t, found)
	require.Equal(t, addr, got)
}
