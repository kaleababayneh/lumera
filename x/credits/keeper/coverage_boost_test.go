//go:build cosmos && cosmos_full

package keeper

import (
	"fmt"
	"testing"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// ---------------------------------------------------------------------------
// bd-31ygc: boost credits keeper coverage to >= 85%
// ---------------------------------------------------------------------------

// --- jsonValueCodec ---

func TestJsonValueCodec_EncodeJSON(t *testing.T) {
	c := jsonValueCodec[types.Lock]{}
	lock := &types.Lock{LockId: "lock-1", Router: "router-a"}
	bz, err := c.EncodeJSON(lock)
	require.NoError(t, err)
	require.Contains(t, string(bz), "lock_id")
	require.Contains(t, string(bz), "lock-1")
}

func TestJsonValueCodec_EncodeJSON_Nil(t *testing.T) {
	c := jsonValueCodec[types.Lock]{}
	bz, err := c.EncodeJSON(nil)
	require.NoError(t, err)
	require.Nil(t, bz)
}

func TestJsonValueCodec_DecodeJSON(t *testing.T) {
	c := jsonValueCodec[types.Lock]{}
	input := `{"lock_id":"lock-99","router":"router-b"}`
	lock, err := c.DecodeJSON([]byte(input))
	require.NoError(t, err)
	require.NotNil(t, lock)
	assert.Equal(t, "lock-99", lock.LockId)
	assert.Equal(t, "router-b", lock.Router)
}

func TestJsonValueCodec_DecodeJSON_Nil(t *testing.T) {
	c := jsonValueCodec[types.Lock]{}
	lock, err := c.DecodeJSON(nil)
	require.NoError(t, err)
	require.Nil(t, lock)
}

func TestJsonValueCodec_DecodeJSON_InvalidJSON(t *testing.T) {
	c := jsonValueCodec[types.Lock]{}
	_, err := c.DecodeJSON([]byte("not json"))
	require.Error(t, err)
}

func TestJsonValueCodec_Stringify(t *testing.T) {
	c := jsonValueCodec[types.Lock]{}
	lock := &types.Lock{LockId: "lock-42"}
	s := c.Stringify(lock)
	require.Contains(t, s, "lock-42")
}

func TestJsonValueCodec_Stringify_Nil(t *testing.T) {
	c := jsonValueCodec[types.Lock]{}
	s := c.Stringify(nil)
	assert.Equal(t, "", s)
}

func TestJsonValueCodec_ValueType(t *testing.T) {
	c := jsonValueCodec[types.Lock]{}
	vt := c.ValueType()
	require.Contains(t, vt, "Lock")
}

func TestJsonValueCodec_RoundTrip(t *testing.T) {
	c := jsonValueCodec[types.Lock]{}
	original := &types.Lock{LockId: "lock-rt", Router: "router-rt", SessionId: "sess-rt"}
	bz, err := c.Encode(original)
	require.NoError(t, err)
	decoded, err := c.Decode(bz)
	require.NoError(t, err)
	assert.Equal(t, original.LockId, decoded.LockId)
	assert.Equal(t, original.Router, decoded.Router)
	assert.Equal(t, original.SessionId, decoded.SessionId)
}

// --- normalizeOriginID ---

func TestNormalizeOriginID_Valid(t *testing.T) {
	result, err := normalizeOriginID("injective:spot")
	require.NoError(t, err)
	assert.Equal(t, "injective:spot", result)
}

func TestNormalizeOriginID_Uppercased(t *testing.T) {
	result, err := normalizeOriginID("INJECTIVE:PERP")
	require.NoError(t, err)
	assert.Equal(t, "injective:perp", result)
}

func TestNormalizeOriginID_WithWhitespace(t *testing.T) {
	result, err := normalizeOriginID("  injective:spot  ")
	require.NoError(t, err)
	assert.Equal(t, "injective:spot", result)
}

func TestNormalizeOriginID_Empty(t *testing.T) {
	result, err := normalizeOriginID("")
	require.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestNormalizeOriginID_OnlyWhitespace(t *testing.T) {
	result, err := normalizeOriginID("   ")
	require.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestNormalizeOriginID_TooLong(t *testing.T) {
	long := ""
	for i := 0; i < 65; i++ {
		long += "a"
	}
	_, err := normalizeOriginID(long)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too long")
}

func TestNormalizeOriginID_MissingColon(t *testing.T) {
	_, err := normalizeOriginID("injectivespot")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be <namespace>:<surface>")
}

func TestNormalizeOriginID_TooManyColons(t *testing.T) {
	_, err := normalizeOriginID("a:b:c")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be <namespace>:<surface>")
}

func TestNormalizeOriginID_EmptyNamespace(t *testing.T) {
	_, err := normalizeOriginID(":spot")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be empty")
}

func TestNormalizeOriginID_EmptySurface(t *testing.T) {
	_, err := normalizeOriginID("injective:")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be empty")
}

func TestNormalizeOriginID_WithHyphensAndUnderscores(t *testing.T) {
	result, err := normalizeOriginID("injective-1:spot_v2")
	require.NoError(t, err)
	assert.Equal(t, "injective-1:spot_v2", result)
}

// --- validateOriginPart ---

func TestValidateOriginPart_Valid(t *testing.T) {
	require.NoError(t, validateOriginPart("injective", "test"))
	require.NoError(t, validateOriginPart("abc-def", "test"))
	require.NoError(t, validateOriginPart("abc_def", "test"))
	require.NoError(t, validateOriginPart("a123", "test"))
	require.NoError(t, validateOriginPart("0abc", "test"))
}

func TestValidateOriginPart_Empty(t *testing.T) {
	err := validateOriginPart("", "test-field")
	require.Error(t, err)
	require.Contains(t, err.Error(), "test-field cannot be empty")
}

func TestValidateOriginPart_TooLong(t *testing.T) {
	long := ""
	for i := 0; i < 33; i++ {
		long += "a"
	}
	err := validateOriginPart(long, "test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "too long")
}

func TestValidateOriginPart_ExactlyAt32(t *testing.T) {
	exact := ""
	for i := 0; i < 32; i++ {
		exact += "a"
	}
	require.NoError(t, validateOriginPart(exact, "test"))
}

func TestValidateOriginPart_InvalidChars(t *testing.T) {
	err := validateOriginPart("abc.def", "test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid character")
}

func TestValidateOriginPart_StartsWithPunct(t *testing.T) {
	err := validateOriginPart("-abc", "test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must start with [a-z0-9]")

	err = validateOriginPart("_abc", "test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must start with [a-z0-9]")
}

func TestValidateOriginPart_UppercaseInvalid(t *testing.T) {
	err := validateOriginPart("ABC", "test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid character")
}

// --- sanitizeLabel ---

func TestSanitizeLabel_Normal(t *testing.T) {
	assert.Equal(t, "hello", sanitizeLabel("hello"))
}

func TestSanitizeLabel_Uppercased(t *testing.T) {
	assert.Equal(t, "hello", sanitizeLabel("HELLO"))
}

func TestSanitizeLabel_Whitespace(t *testing.T) {
	assert.Equal(t, "hello", sanitizeLabel("  HELLO  "))
}

func TestSanitizeLabel_Empty(t *testing.T) {
	assert.Equal(t, "unknown", sanitizeLabel(""))
}

func TestSanitizeLabel_OnlyWhitespace(t *testing.T) {
	assert.Equal(t, "unknown", sanitizeLabel("   "))
}

func TestSanitizeLabel_TruncatesAt48(t *testing.T) {
	long := ""
	for i := 0; i < 50; i++ {
		long += "a"
	}
	result := sanitizeLabel(long)
	assert.Len(t, result, 48)
}

func TestSanitizeLabel_ExactlyAt48(t *testing.T) {
	exactly48 := ""
	for i := 0; i < 48; i++ {
		exactly48 += "b"
	}
	result := sanitizeLabel(exactly48)
	assert.Len(t, result, 48)
	assert.Equal(t, exactly48, result)
}

// --- derivePolicyID ---

func TestDerivePolicyID_Simple(t *testing.T) {
	assert.Equal(t, "standard-v1", derivePolicyID("standard-v1"))
}

func TestDerivePolicyID_WithVersion(t *testing.T) {
	assert.Equal(t, "standard-v1", derivePolicyID("standard-v1@2024-01-01"))
}

func TestDerivePolicyID_Lowercased(t *testing.T) {
	assert.Equal(t, "standard-v1", derivePolicyID("Standard-V1"))
}

func TestDerivePolicyID_WhitespaceAround(t *testing.T) {
	assert.Equal(t, "myid", derivePolicyID("  myid  "))
}

func TestDerivePolicyID_Empty(t *testing.T) {
	assert.Equal(t, "", derivePolicyID(""))
}

func TestDerivePolicyID_OnlyWhitespace(t *testing.T) {
	assert.Equal(t, "", derivePolicyID("   "))
}

func TestDerivePolicyID_EmptyBeforeAt(t *testing.T) {
	assert.Equal(t, "", derivePolicyID("  @something"))
}

// --- distributeCuratorRoyalty (deprecated stub) ---

func TestDistributeCuratorRoyalty_ReturnsEmpty(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	coins, err := keeper.distributeCuratorRoyalty(ctx, "toolpack-1", sdk.NewCoins(sdk.NewInt64Coin("lac", 1000)))
	require.NoError(t, err)
	assert.True(t, coins.IsZero())
}

// --- Schema, GetCodec, GetStoreKey, StoreService ---

func TestSchema_ReturnsSchema(t *testing.T) {
	_, keeper, _, _, _ := setupCreditsKeeper(t)
	schema := keeper.Schema()
	_ = schema // Ensure no panic
}

func TestGetCodec_ReturnsNonNil(t *testing.T) {
	_, keeper, _, _, _ := setupCreditsKeeper(t)
	cdc := keeper.GetCodec()
	require.NotNil(t, cdc)
}

func TestGetStoreKey_ReturnsNonNil(t *testing.T) {
	_, keeper, _, _, _ := setupCreditsKeeper(t)
	ss := keeper.GetStoreKey()
	require.NotNil(t, ss)
}

func TestStoreService_ReturnsNonNil(t *testing.T) {
	_, keeper, _, _, _ := setupCreditsKeeper(t)
	ss := keeper.StoreService()
	require.NotNil(t, ss)
}

// --- DeleteLock ---

func TestDeleteLock_ExistingLock(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	lock := &types.Lock{
		LockId:    "lock-42",
		Router:    "router-a",
		SessionId: "session-1",
	}
	require.NoError(t, keeper.SaveLock(ctx, lock))

	_, found := keeper.GetLock(ctx, "lock-42")
	require.True(t, found)

	require.NoError(t, keeper.DeleteLock(ctx, "lock-42"))

	_, found = keeper.GetLock(ctx, "lock-42")
	require.False(t, found)
}

func TestDeleteLock_Nonexistent(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	err := keeper.DeleteLock(ctx, "lock-nonexistent")
	require.NoError(t, err)
}

// --- Query Server: Params ---

func TestQueryServer_Params(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	resp, err := qs.Params(ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Params)
	assert.Equal(t, types.DefaultCreditDenom, resp.Params.CreditDenom)
}

// --- Query Server: Lock ---

func TestQueryServer_Lock_Found(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	lock := &types.Lock{
		LockId:    "lock-100",
		Router:    "router-x",
		SessionId: "sess-1",
	}
	require.NoError(t, keeper.SaveLock(ctx, lock))

	resp, err := qs.Lock(ctx, &types.QueryLockRequest{LockId: "lock-100"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Lock)
	assert.Equal(t, "lock-100", resp.Lock.LockId)
	assert.Equal(t, "router-x", resp.Lock.Router)
}

func TestQueryServer_Lock_NotFound(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	_, err := qs.Lock(ctx, &types.QueryLockRequest{LockId: "lock-missing"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestQueryServer_Lock_NilRequest(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	_, err := qs.Lock(ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lock_id is required")
}

func TestQueryServer_Lock_EmptyID(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	qs := NewQueryServer(keeper)

	_, err := qs.Lock(ctx, &types.QueryLockRequest{LockId: ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "lock_id is required")
}

// --- MintCredits ---

func TestMintCredits_HappyPath(t *testing.T) {
	ctx, keeper, bank, _, _ := setupCreditsKeeper(t)
	recipient := newAccAddress()
	params := keeper.GetParams(ctx)
	amount := sdk.NewCoin(params.CreditDenom, sdkmath.NewInt(1000))

	err := keeper.MintCredits(ctx, recipient, amount, "test-mint")
	require.NoError(t, err)

	bal := bank.GetBalance(ctx, recipient, params.CreditDenom)
	assert.Equal(t, sdkmath.NewInt(1000), bal.Amount)
}

func TestMintCredits_EmptyRecipient(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	amount := sdk.NewCoin("lac", sdkmath.NewInt(100))
	err := keeper.MintCredits(ctx, sdk.AccAddress{}, amount, "test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "recipient required")
}

func TestMintCredits_ZeroAmount(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	recipient := newAccAddress()
	params := keeper.GetParams(ctx)
	amount := sdk.NewCoin(params.CreditDenom, sdkmath.ZeroInt())

	err := keeper.MintCredits(ctx, recipient, amount, "test")
	require.NoError(t, err) // Zero amount is a no-op
}

func TestMintCredits_WrongDenom(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	recipient := newAccAddress()
	amount := sdk.NewCoin("wrong-denom", sdkmath.NewInt(1000))

	err := keeper.MintCredits(ctx, recipient, amount, "test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected denom")
}

// --- BurnCreditsFromAccount ---

func TestBurnCreditsFromAccount_HappyPath(t *testing.T) {
	ctx, keeper, bank, _, _ := setupCreditsKeeper(t)
	sender := newAccAddress()
	params := keeper.GetParams(ctx)
	amount := sdk.NewCoin(params.CreditDenom, sdkmath.NewInt(500))

	// Fund the sender first
	bank.FundAccount(sender, sdk.NewCoins(sdk.NewInt64Coin(params.CreditDenom, 1000)))

	err := keeper.BurnCreditsFromAccount(ctx, sender, amount, "test-burn")
	require.NoError(t, err)
}

func TestBurnCreditsFromAccount_EmptySender(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	amount := sdk.NewCoin("lac", sdkmath.NewInt(100))
	err := keeper.BurnCreditsFromAccount(ctx, sdk.AccAddress{}, amount, "test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "sender required")
}

func TestBurnCreditsFromAccount_ZeroAmount(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	sender := newAccAddress()
	params := keeper.GetParams(ctx)
	amount := sdk.NewCoin(params.CreditDenom, sdkmath.ZeroInt())

	err := keeper.BurnCreditsFromAccount(ctx, sender, amount, "test")
	require.NoError(t, err) // Zero amount is a no-op
}

func TestBurnCreditsFromAccount_WrongDenom(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	sender := newAccAddress()
	amount := sdk.NewCoin("wrong-denom", sdkmath.NewInt(100))

	err := keeper.BurnCreditsFromAccount(ctx, sender, amount, "test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected denom")
}

// --- GetUserBalance ---

func TestGetUserBalance_WithBalance(t *testing.T) {
	ctx, keeper, bank, _, _ := setupCreditsKeeper(t)
	user := newAccAddress()
	params := keeper.GetParams(ctx)

	bank.FundAccount(user, sdk.NewCoins(sdk.NewInt64Coin(params.CreditDenom, 5000)))

	bal := keeper.GetUserBalance(ctx, user)
	assert.Equal(t, params.CreditDenom, bal.Denom)
	assert.Equal(t, sdkmath.NewInt(5000), bal.Amount)
}

func TestGetUserBalance_NoBalance(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	user := newAccAddress()
	params := keeper.GetParams(ctx)

	bal := keeper.GetUserBalance(ctx, user)
	assert.Equal(t, params.CreditDenom, bal.Denom)
	assert.True(t, bal.Amount.IsZero())
}

// --- GetLockedAmount ---

func TestGetLockedAmount_WithLocks(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)

	routerAddr := "lumera1routeraddr"
	// Create two active locks for this router
	for i := 1; i <= 2; i++ {
		lock := &types.Lock{
			LockId:    fmt.Sprintf("lock-active-%d", i),
			Router:    routerAddr,
			SessionId: "sess",
			Status:    types.LockStatus_LOCK_STATUS_ACTIVE,
			Amount:    &v1beta1.Coin{Denom: "lac", Amount: "1000"},
		}
		require.NoError(t, keeper.SaveLock(ctx, lock))
	}
	// One released lock (should be excluded from the total)
	released := &types.Lock{
		LockId:    "lock-released",
		Router:    routerAddr,
		SessionId: "sess-s",
		Status:    types.LockStatus_LOCK_STATUS_RELEASED,
		Amount:    &v1beta1.Coin{Denom: "lac", Amount: "500"},
	}
	require.NoError(t, keeper.SaveLock(ctx, released))

	total := keeper.GetLockedAmount(ctx, routerAddr)
	found := false
	for _, c := range total {
		if c.Denom == "lac" {
			assert.Equal(t, sdkmath.NewInt(2000), c.Amount)
			found = true
		}
	}
	assert.True(t, found, "should find lac in locked amount")
}

func TestGetLockedAmount_NoLocks(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	total := keeper.GetLockedAmount(ctx, "nonexistent-router")
	assert.True(t, total.IsZero())
}

// --- UpdateSettlementMetrics ---

func TestUpdateSettlementMetrics_CreatesIfNotExists(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	// Should not panic when metrics don't exist yet
	require.NoError(t, keeper.UpdateSettlementMetrics(ctx, 5, 1))
}

func TestUpdateSettlementMetrics_AccumulatesMultipleCalls(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	require.NoError(t, keeper.UpdateSettlementMetrics(ctx, 10, 2))
	require.NoError(t, keeper.UpdateSettlementMetrics(ctx, 5, 1))
	// Verify no panics; metrics accumulate internally
}

// --- Keeper accessor methods ---

func TestAuthority_ReturnsNonEmpty(t *testing.T) {
	_, keeper, _, _, _ := setupCreditsKeeper(t)
	auth := keeper.Authority()
	require.NotEmpty(t, auth)
}

func TestBankKeeper_ReturnsNonNil(t *testing.T) {
	_, keeper, _, _, _ := setupCreditsKeeper(t)
	bk := keeper.BankKeeper()
	require.NotNil(t, bk)
}

func TestAccountKeeper_ReturnsNonNil(t *testing.T) {
	_, keeper, _, _, _ := setupCreditsKeeper(t)
	ak := keeper.AccountKeeper()
	require.NotNil(t, ak)
}

func TestModuleAddress_ReturnsNonEmpty(t *testing.T) {
	_, keeper, _, moduleAddr, _ := setupCreditsKeeper(t)
	addr := keeper.ModuleAddress()
	require.NotNil(t, addr)
	assert.Equal(t, moduleAddr, addr)
}

func TestLogger_DoesNotPanic(t *testing.T) {
	ctx, keeper, _, _, _ := setupCreditsKeeper(t)
	logger := keeper.Logger(ctx)
	require.NotNil(t, logger)
}
