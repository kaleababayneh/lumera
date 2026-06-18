//go:build cosmos

package types

import (
	"testing"
	"time"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	registrytypes "github.com/LumeraProtocol/lumera/x/registry/types"
)

const validAddr = "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"

func validCoin() *basev1beta1.Coin {
	return &basev1beta1.Coin{Denom: "ulac", Amount: "1000"}
}

// ---------- DefaultParams ----------

func TestDefaultParams(t *testing.T) {
	p := DefaultParams()
	require.NotNil(t, p)
	assert.Equal(t, DefaultCreditDenom, p.CreditDenom)
	assert.Equal(t, uint32(DefaultLockTTLSeconds), p.DefaultLockTtlSeconds)
	assert.Equal(t, uint32(DefaultMaxLockTTLSeconds), p.MaxLockTtlSeconds)
	assert.Equal(t, uint32(DefaultSettlementGraceSeconds), p.SettlementGracePeriodSeconds)
	assert.Equal(t, uint32(DefaultMaxSettlementsPerBlock), p.MaxSettlementsPerBlock)
	assert.Equal(t, uint32(DefaultMaxExpiredLocksPerBlock), p.MaxExpiredLocksPerBlock)
	assert.Equal(t, uint32(DefaultMaxPrunedSettlementsPerBlock), p.MaxPrunedSettlementsPerBlock)
	assert.Equal(t, DefaultTreasuryAddress, p.TreasuryAddress)
	assert.Equal(t, uint32(DefaultBurnRateSpendBps), p.BurnRateSpendBps)
	assert.Equal(t, uint32(DefaultBurnRateAcqBps), p.BurnRateAcqBps)
	assert.Equal(t, uint32(DefaultInsuranceBps), p.InsuranceBps)
	assert.Equal(t, uint32(DefaultDisputeWindowHours), p.DisputeWindowHours)
	assert.Equal(t, uint32(DefaultOverdraftMaxCreditLineToBondBps), p.OverdraftMaxCreditLineToBondBps)
	assert.Equal(t, uint32(DefaultOverdraftLiquidationThresholdBps), p.OverdraftLiquidationThresholdBps)
}

func TestDefaultParams_Validate(t *testing.T) {
	require.NoError(t, DefaultParams().Validate())
}

func TestDefaultParams_SharedEconomicsAlignWithRegistry(t *testing.T) {
	creditsDefaults := DefaultParams()
	registryDefaults := registrytypes.DefaultParams()

	require.Equal(t, creditsDefaults.BurnRateSpendBps, registryDefaults.BurnRateSpendBps)
	require.Equal(t, creditsDefaults.BurnRateAcqBps, registryDefaults.BurnRateAcqBps)
	require.Equal(t, int32(creditsDefaults.InsuranceBps), registryDefaults.InsuranceBps)
	require.Equal(t, creditsDefaults.MaxSettlementsPerBlock, registryDefaults.MaxSettlementsPerBlock)
	require.Equal(t, DefaultDisputeWindowDuration(), time.Duration(registryDefaults.DisputeWindowSeconds)*time.Second)
}

func TestNewParams(t *testing.T) {
	p := NewParams("ulac", 60, 300, 120, "", 50, 50, 50)
	require.NotNil(t, p)
	assert.Equal(t, "ulac", p.CreditDenom)
	assert.Equal(t, uint32(60), p.DefaultLockTtlSeconds)
	assert.Equal(t, uint32(300), p.MaxLockTtlSeconds)
	assert.Equal(t, uint32(DefaultBurnRateSpendBps), p.BurnRateSpendBps)
	assert.Equal(t, uint32(DefaultBurnRateAcqBps), p.BurnRateAcqBps)
	assert.Equal(t, uint32(DefaultInsuranceBps), p.InsuranceBps)
	assert.Equal(t, uint32(DefaultDisputeWindowHours), p.DisputeWindowHours)
}

// ---------- Params.Validate ----------

func TestParams_Validate_NilParams(t *testing.T) {
	var p *Params
	require.Error(t, p.Validate())
}

func TestParams_Validate_InvalidDenom(t *testing.T) {
	p := DefaultParams()
	p.CreditDenom = ""
	require.Error(t, p.Validate())
}

func TestParams_Validate_ZeroDefaultTTL(t *testing.T) {
	p := DefaultParams()
	p.DefaultLockTtlSeconds = 0
	require.Error(t, p.Validate())
}

func TestParams_Validate_ZeroMaxTTL(t *testing.T) {
	p := DefaultParams()
	p.MaxLockTtlSeconds = 0
	require.Error(t, p.Validate())
}

func TestParams_Validate_DefaultExceedsMax(t *testing.T) {
	p := DefaultParams()
	p.DefaultLockTtlSeconds = 5000
	p.MaxLockTtlSeconds = 1000
	require.Error(t, p.Validate())
}

func TestParams_Validate_ZeroGracePeriod(t *testing.T) {
	p := DefaultParams()
	p.SettlementGracePeriodSeconds = 0
	require.Error(t, p.Validate())
}

func TestParams_Validate_ZeroMaxSettlements(t *testing.T) {
	p := DefaultParams()
	p.MaxSettlementsPerBlock = 0
	require.Error(t, p.Validate())
}

func TestParams_Validate_ZeroMaxExpiredLocks(t *testing.T) {
	p := DefaultParams()
	p.MaxExpiredLocksPerBlock = 0
	require.Error(t, p.Validate())
}

func TestParams_Validate_ZeroMaxPrunedSettlements(t *testing.T) {
	p := DefaultParams()
	p.MaxPrunedSettlementsPerBlock = 0
	require.Error(t, p.Validate())
}

func TestParams_Validate_InvalidTreasuryAddr(t *testing.T) {
	p := DefaultParams()
	p.TreasuryAddress = "not_valid_address"
	require.Error(t, p.Validate())
}

func TestParams_Validate_ValidTreasuryAddr(t *testing.T) {
	p := DefaultParams()
	p.TreasuryAddress = validAddr
	require.NoError(t, p.Validate())
}

func TestParams_Validate_EmptyTreasuryOK(t *testing.T) {
	p := DefaultParams()
	p.TreasuryAddress = ""
	require.NoError(t, p.Validate())
}

func TestParams_Validate_ZeroDisputeWindowHoursUsesCanonicalDefault(t *testing.T) {
	p := DefaultParams()
	p.DisputeWindowHours = 0
	require.NoError(t, p.Validate())
	require.Equal(t, DefaultDisputeWindowDuration(), DisputeWindowDuration(p))
}

func TestParams_Validate_DisputeWindowHoursDurationBoundary(t *testing.T) {
	p := DefaultParams()
	p.DisputeWindowHours = maxDisputeWindowHours
	require.NoError(t, p.Validate())
	require.Equal(t, time.Duration(maxDisputeWindowHours)*time.Hour, DisputeWindowDuration(p))

	p.DisputeWindowHours = maxDisputeWindowHours + 1
	require.ErrorContains(t, p.Validate(), "dispute window hours")
	require.Equal(t, DefaultDisputeWindowDuration(), DisputeWindowDuration(p))
}

func TestParams_Validate_OverdraftGovernanceKnobs(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Params)
		wantErr string
	}{
		{
			name: "credit line ratio requires liquidation threshold",
			mutate: func(p *Params) {
				p.OverdraftMaxCreditLineToBondBps = 5000
			},
			wantErr: "overdraft liquidation threshold",
		},
		{
			name: "liquidation threshold requires credit line ratio",
			mutate: func(p *Params) {
				p.OverdraftLiquidationThresholdBps = 8000
			},
			wantErr: "overdraft max credit line",
		},
		{
			name: "credit line ratio rejects more than bond value",
			mutate: func(p *Params) {
				p.OverdraftMaxCreditLineToBondBps = MaxBasisPoints + 1
				p.OverdraftLiquidationThresholdBps = 8000
			},
			wantErr: "overdraft max credit line",
		},
		{
			name: "liquidation threshold rejects more than full utilization",
			mutate: func(p *Params) {
				p.OverdraftMaxCreditLineToBondBps = 5000
				p.OverdraftLiquidationThresholdBps = MaxBasisPoints + 1
			},
			wantErr: "overdraft liquidation threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := DefaultParams()
			tt.mutate(p)
			require.ErrorContains(t, p.Validate(), tt.wantErr)
		})
	}
}

func TestParams_Validate_OverdraftGovernanceKnobsValid(t *testing.T) {
	p := DefaultParams()
	p.OverdraftMaxCreditLineToBondBps = 5000
	p.OverdraftLiquidationThresholdBps = 8000
	require.NoError(t, p.Validate())
}

// TestDisputeWindowDuration_NilParamsFallsThroughToDefault pins the
// nil-safety branch: DisputeWindowDuration must not panic on nil
// Params and must return the canonical registry default. Callers
// include read paths that may encounter uninitialized params during
// module migration / genesis bootstrap.
func TestDisputeWindowDuration_NilParamsFallsThroughToDefault(t *testing.T) {
	got := DisputeWindowDuration(nil)
	require.Equal(t, DefaultDisputeWindowDuration(), got,
		"nil Params must fall through to canonical default, not panic")
}

// TestDisputeWindowDuration_OverrideHoursUsed pins the positive-
// override branch that the existing zero-override test complements.
// When p.DisputeWindowHours > 0, the helper returns that value in
// hours (NOT the registry default). Regression guard against a
// refactor that collapsed the branch to always use the default.
func TestDisputeWindowDuration_OverrideHoursUsed(t *testing.T) {
	p := DefaultParams()
	p.DisputeWindowHours = 48 // 48 hours override
	got := DisputeWindowDuration(p)
	require.Equal(t, 48*time.Hour, got,
		"non-zero DisputeWindowHours must override the canonical default")
	require.NotEqual(t, DefaultDisputeWindowDuration(), got,
		"override must actually differ from default (sanity check on the fixture)")
}

// ---------- Params.LockTTL ----------

func TestLockTTL_NilParams(t *testing.T) {
	var p *Params
	ttl := p.LockTTL(0)
	assert.Equal(t, time.Duration(DefaultLockTTLSeconds)*time.Second, ttl)
}

func TestLockTTL_ZeroRequested(t *testing.T) {
	p := DefaultParams()
	ttl := p.LockTTL(0)
	assert.Equal(t, time.Duration(DefaultLockTTLSeconds)*time.Second, ttl)
}

func TestLockTTL_NegativeRequested(t *testing.T) {
	p := DefaultParams()
	ttl := p.LockTTL(-1)
	assert.Equal(t, time.Duration(DefaultLockTTLSeconds)*time.Second, ttl)
}

func TestLockTTL_ExceedsMax(t *testing.T) {
	p := DefaultParams()
	ttl := p.LockTTL(2 * time.Hour)
	assert.Equal(t, time.Duration(p.MaxLockTtlSeconds)*time.Second, ttl)
}

func TestLockTTL_WithinLimits(t *testing.T) {
	p := DefaultParams()
	ttl := p.LockTTL(60 * time.Second)
	assert.Equal(t, 60*time.Second, ttl)
}

// TestLockTTL_ZeroMaxTTLFallsBackToDefaultCap pins the MaxLockTtlSeconds
// == 0 branch: when the param is zero (e.g. genesis without a
// cap set), the helper uses DefaultMaxLockTTLSeconds as the
// effective cap rather than silently accepting unbounded requests.
// Regression guard: without this fallback, a governance proposal
// that set MaxLockTtlSeconds=0 would let clients request arbitrary
// lock durations.
func TestLockTTL_ZeroMaxTTLFallsBackToDefaultCap(t *testing.T) {
	p := DefaultParams()
	p.MaxLockTtlSeconds = 0
	// Request twice the default cap — should clamp to DefaultMax.
	requested := 2 * time.Duration(DefaultMaxLockTTLSeconds) * time.Second
	ttl := p.LockTTL(requested)
	assert.Equal(t, time.Duration(DefaultMaxLockTTLSeconds)*time.Second, ttl,
		"zero MaxLockTtlSeconds must fall back to DefaultMaxLockTTLSeconds, not accept unbounded")
}

// TestLockTTL_ZeroDefaultTTLFallsBackToPackageDefault pins the
// double-fallback branch: if requested <= 0 AND p.DefaultLockTtlSeconds
// is also zero, the helper must fall back to the package-level
// DefaultLockTTLSeconds rather than returning 0 (which would produce
// an immediately-expired lock). This is the second "if requested <= 0"
// check in LockTTL — without it, a corrupt Params with zero Default
// would silently break every lock.
func TestLockTTL_ZeroDefaultTTLFallsBackToPackageDefault(t *testing.T) {
	p := DefaultParams()
	p.DefaultLockTtlSeconds = 0
	ttl := p.LockTTL(0)
	assert.Equal(t, time.Duration(DefaultLockTTLSeconds)*time.Second, ttl,
		"zero DefaultLockTtlSeconds + zero requested must fall back to package DefaultLockTTLSeconds, not 0")
}

// TestLockTTL_ExactlyAtMaxUnchanged pins the strict-greater-than
// boundary on the max-cap: a requested TTL exactly equal to the
// cap must pass through unchanged. Regression guard against a
// refactor flipping the comparison to `>=` which would silently
// clamp requests at the exact boundary.
func TestLockTTL_ExactlyAtMaxUnchanged(t *testing.T) {
	p := DefaultParams()
	maxDur := time.Duration(p.MaxLockTtlSeconds) * time.Second
	ttl := p.LockTTL(maxDur)
	assert.Equal(t, maxDur, ttl,
		"requested == max must pass through unchanged (strict GT boundary, not GTE)")
}

// ---------- Genesis ----------

func TestDefaultGenesis(t *testing.T) {
	gs := DefaultGenesis()
	require.NotNil(t, gs)
	require.NotNil(t, gs.Params)
}

func TestDefaultGenesis_Validate(t *testing.T) {
	require.NoError(t, DefaultGenesis().Validate())
}

func TestNewGenesisState_NilParams(t *testing.T) {
	gs := NewGenesisState(nil, nil, nil, nil, nil)
	require.NotNil(t, gs.Params)
}

func TestGenesis_Validate_NilState(t *testing.T) {
	var gs *GenesisState
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_NilParams(t *testing.T) {
	gs := DefaultGenesis()
	gs.Params = nil
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_InvalidParams(t *testing.T) {
	gs := DefaultGenesis()
	gs.Params.CreditDenom = ""
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_NilLock(t *testing.T) {
	gs := DefaultGenesis()
	gs.Locks = []*Lock{nil}
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_EmptyLockID(t *testing.T) {
	gs := DefaultGenesis()
	gs.Locks = []*Lock{{LockId: "", Amount: validCoin()}}
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_DuplicateLockID(t *testing.T) {
	gs := DefaultGenesis()
	gs.Locks = []*Lock{
		{LockId: "lock-1", Amount: validCoin()},
		{LockId: "lock-1", Amount: validCoin()},
	}
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_NilSettlement(t *testing.T) {
	gs := DefaultGenesis()
	gs.Settlements = []*SettlementRecord{nil}
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_EmptySettlementID(t *testing.T) {
	gs := DefaultGenesis()
	gs.Settlements = []*SettlementRecord{{Id: ""}}
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_DuplicateSettlementID(t *testing.T) {
	gs := DefaultGenesis()
	gs.Settlements = []*SettlementRecord{
		{Id: "s-1", Status: SettlementStatus_SETTLEMENT_STATUS_PENDING},
		{Id: "s-1", Status: SettlementStatus_SETTLEMENT_STATUS_PENDING},
	}
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_NilDispute(t *testing.T) {
	gs := DefaultGenesis()
	gs.Disputes = []*DisputeRecord{nil}
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_EmptyDisputeID(t *testing.T) {
	gs := DefaultGenesis()
	gs.Disputes = []*DisputeRecord{{Id: ""}}
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_DuplicateDisputeID(t *testing.T) {
	gs := DefaultGenesis()
	gs.Disputes = []*DisputeRecord{
		{Id: "d-1"},
		{Id: "d-1"},
	}
	require.Error(t, gs.Validate())
}

func TestGenesis_Validate_WithMetrics(t *testing.T) {
	gs := DefaultGenesis()
	gs.Metrics = &SettlementMetrics{}
	require.NoError(t, gs.Validate())
}

// ---------- Keys ----------

func TestModuleConstants(t *testing.T) {
	assert.Equal(t, "credits", ModuleName)
	assert.Equal(t, "credits", StoreKey)
	assert.Equal(t, "credits", RouterKey)
	assert.Equal(t, "credits", QuerierRoute)
	assert.Equal(t, "credits", ModuleAccountName)
}

func TestKeyPrefixBytesUnique(t *testing.T) {
	prefixes := []uint8{
		ParamsPrefixByte, LocksPrefixByte, LockSeqKeyPrefixByte,
		SettlementsPrefixByte, DisputesPrefixByte, MetricsPrefixByte,
		CACRoyaltyPrefixByte, CACStatsPrefixByte, LockExpiryPrefixByte,
	}
	seen := make(map[uint8]bool)
	for _, p := range prefixes {
		assert.False(t, seen[p], "duplicate prefix byte: 0x%02x", p)
		seen[p] = true
	}
}

func TestKeyPrefixSlicesMatchBytes(t *testing.T) {
	assert.Equal(t, []byte{ParamsPrefixByte}, ParamsPrefix)
	assert.Equal(t, []byte{LocksPrefixByte}, LocksPrefix)
	assert.Equal(t, []byte{LockExpiryPrefixByte}, LockExpiryPrefix)
	assert.Equal(t, []byte{LockSeqKeyPrefixByte}, LockSeqKeyPrefix)
	assert.Equal(t, []byte{SettlementsPrefixByte}, SettlementPrefix)
	assert.Equal(t, []byte{DisputesPrefixByte}, DisputePrefix)
	assert.Equal(t, []byte{MetricsPrefixByte}, MetricsPrefix)
	assert.Equal(t, []byte{CACRoyaltyPrefixByte}, CACRoyaltyPrefix)
	assert.Equal(t, []byte{CACStatsPrefixByte}, CACStatsPrefix)
}

// ---------- Errors ----------

func TestSentinelErrors(t *testing.T) {
	errs := []error{
		ErrInvalidParams, ErrInsufficientFunds, ErrLockNotFound,
		ErrLockExpired, ErrLockInactive, ErrSettlementFailed,
		ErrDisputeFailed, ErrReleaseFailed,
	}
	for _, e := range errs {
		assert.NotNil(t, e)
		assert.NotEmpty(t, e.Error())
	}
}

// ---------- Events ----------

func TestEventTypesUnique(t *testing.T) {
	events := []string{
		EventTypeSettlement, EventTypeBurn, EventTypeDistribute,
		EventTypeLock, EventTypeUnlock, EventTypeDispute, EventTypeSwap,
	}
	seen := make(map[string]bool)
	for _, e := range events {
		assert.NotEmpty(t, e)
		assert.False(t, seen[e], "duplicate event: %s", e)
		seen[e] = true
	}
}

func TestAttributeKeysUnique(t *testing.T) {
	attrs := []string{
		AttributeKeySettlementID, AttributeKeyToolID, AttributeKeyPublisher,
		AttributeKeyUser, AttributeKeyAmount, AttributeKeyBurnAmount,
		AttributeKeyStatus, AttributeKeyLockID, AttributeKeyDisputeID,
		AttributeKeySwapRate, AttributeKeyRouter, AttributeKeySessionID,
		AttributeKeyReason, AttributeKeyExpiresAt, AttributeKeyToolpackID,
	}
	seen := make(map[string]bool)
	for _, a := range attrs {
		assert.NotEmpty(t, a)
		assert.False(t, seen[a], "duplicate attr: %s", a)
		seen[a] = true
	}
}

// ---------- Message Types ----------

func TestMsgSwapLUMEtoLAC_RouteType(t *testing.T) {
	msg := &MsgSwapLUMEtoLAC{}
	assert.Equal(t, RouterKey, msg.Route())
	assert.Equal(t, TypeMsgSwapLUMEtoLAC, msg.Type())
}

func TestMsgSwapLUMEtoLAC_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgSwapLUMEtoLAC{Sender: validAddr, LumeAmount: validCoin(), MinLacOut: "1"}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgSwapLUMEtoLAC_ValidateBasic_EmptySender(t *testing.T) {
	msg := &MsgSwapLUMEtoLAC{Sender: "", LumeAmount: validCoin()}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgSwapLUMEtoLAC_ValidateBasic_NilAmount(t *testing.T) {
	msg := &MsgSwapLUMEtoLAC{Sender: validAddr, LumeAmount: nil}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgSwapLUMEtoLAC_ValidateBasic_InvalidMinLacOut(t *testing.T) {
	tests := []string{"0", "-1", "not-an-int"}
	for _, minOut := range tests {
		msg := &MsgSwapLUMEtoLAC{Sender: validAddr, LumeAmount: validCoin(), MinLacOut: minOut}
		require.Error(t, msg.ValidateBasic(), "min_lac_out=%q", minOut)
	}
}

func TestMsgUpdateParams_ValidateBasic_DisableOverdraftAlone(t *testing.T) {
	msg := &MsgUpdateParams{Authority: validAddr, DisableOverdraft: true}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgUpdateParams_ValidateBasic_DisableOverdraftConflictsWithValues(t *testing.T) {
	tests := []MsgUpdateParams{
		{Authority: validAddr, DisableOverdraft: true, OverdraftMaxCreditLineToBondBps: 5000},
		{Authority: validAddr, DisableOverdraft: true, OverdraftLiquidationThresholdBps: 8000},
		{Authority: validAddr, DisableOverdraft: true, OverdraftMaxCreditLineToBondBps: 5000, OverdraftLiquidationThresholdBps: 8000},
	}
	for i := range tests {
		err := tests[i].ValidateBasic()
		require.Error(t, err, "case %d", i)
		require.Contains(t, err.Error(), "disable_overdraft", "case %d", i)
	}
}

func TestMsgUpdateParams_ValidateBasic_ZeroStateFlags(t *testing.T) {
	valid := []MsgUpdateParams{
		{Authority: validAddr, DisableBurnRateAdjustment: true},
		{Authority: validAddr, ResetDisputeWindow: true},
		{Authority: validAddr, DisableBurnRateAdjustment: true, ResetDisputeWindow: true},
	}
	for i := range valid {
		require.NoError(t, valid[i].ValidateBasic(), "valid case %d", i)
	}

	conflicts := []struct {
		msg  MsgUpdateParams
		want string
	}{
		{MsgUpdateParams{Authority: validAddr, DisableBurnRateAdjustment: true, BurnRateAdjustmentEpoch: 100}, "disable_burn_rate_adjustment"},
		{MsgUpdateParams{Authority: validAddr, ResetDisputeWindow: true, DisputeWindowHours: 24}, "reset_dispute_window"},
	}
	for i := range conflicts {
		err := conflicts[i].msg.ValidateBasic()
		require.Error(t, err, "conflict case %d", i)
		require.Contains(t, err.Error(), conflicts[i].want, "conflict case %d", i)
	}
}

func TestMsgSwapLACtoLUME_RouteType(t *testing.T) {
	msg := &MsgSwapLACtoLUME{}
	assert.Equal(t, RouterKey, msg.Route())
	assert.Equal(t, TypeMsgSwapLACtoLUME, msg.Type())
}

func TestMsgSwapLACtoLUME_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgSwapLACtoLUME{Sender: validAddr, LacAmount: validCoin(), MinLumeOut: "1"}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgSwapLACtoLUME_ValidateBasic_EmptySender(t *testing.T) {
	msg := &MsgSwapLACtoLUME{Sender: "", LacAmount: validCoin()}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgSwapLACtoLUME_ValidateBasic_InvalidMinLumeOut(t *testing.T) {
	tests := []string{"0", "-1", "not-an-int"}
	for _, minOut := range tests {
		msg := &MsgSwapLACtoLUME{Sender: validAddr, LacAmount: validCoin(), MinLumeOut: minOut}
		require.Error(t, msg.ValidateBasic(), "min_lume_out=%q", minOut)
	}
}

func TestMsgLockCredits_RouteType(t *testing.T) {
	msg := &MsgLockCredits{}
	assert.Equal(t, RouterKey, msg.Route())
	assert.Equal(t, TypeMsgLockCredits, msg.Type())
}

func TestMsgLockCredits_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgLockCredits{
		Router:    validAddr,
		SessionId: "session-1",
		ToolId:    "tool-1",
		Amount:    validCoin(),
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgLockCredits_ValidateBasic_EmptyRouter(t *testing.T) {
	msg := &MsgLockCredits{Router: "", SessionId: "s", ToolId: "t", Amount: validCoin()}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgLockCredits_ValidateBasic_EmptySessionID(t *testing.T) {
	msg := &MsgLockCredits{Router: validAddr, SessionId: "", ToolId: "t", Amount: validCoin()}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgLockCredits_ValidateBasic_EmptyToolID(t *testing.T) {
	msg := &MsgLockCredits{Router: validAddr, SessionId: "s", ToolId: "", Amount: validCoin()}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgLockCredits_ValidateBasic_RejectsPaddedIDs(t *testing.T) {
	tests := []struct {
		name string
		msg  MsgLockCredits
	}{
		{
			name: "session id",
			msg:  MsgLockCredits{Router: validAddr, SessionId: " session-1", ToolId: "tool-1", Amount: validCoin()},
		},
		{
			name: "tool id",
			msg:  MsgLockCredits{Router: validAddr, SessionId: "session-1", ToolId: "tool-1 ", Amount: validCoin()},
		},
		{
			name: "toolpack id",
			msg:  MsgLockCredits{Router: validAddr, SessionId: "session-1", ToolId: "tool-1", ToolpackId: " toolpack-1 ", Amount: validCoin()},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			require.Error(t, err)
			require.Contains(t, err.Error(), "whitespace")
		})
	}
}

func TestMsgLockCredits_ValidateBasic_NilAmount(t *testing.T) {
	msg := &MsgLockCredits{Router: validAddr, SessionId: "s", ToolId: "t", Amount: nil}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgUnlockCredits_RouteType(t *testing.T) {
	msg := &MsgUnlockCredits{}
	assert.Equal(t, RouterKey, msg.Route())
	assert.Equal(t, TypeMsgUnlockCredits, msg.Type())
}

func TestMsgUnlockCredits_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgUnlockCredits{Router: validAddr, LockId: "lock-1"}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgUnlockCredits_ValidateBasic_EmptyRouter(t *testing.T) {
	msg := &MsgUnlockCredits{Router: "", LockId: "lock-1"}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgUnlockCredits_ValidateBasic_EmptyLockID(t *testing.T) {
	msg := &MsgUnlockCredits{Router: validAddr, LockId: ""}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgUnlockCredits_ValidateBasic_RejectsPaddedLockID(t *testing.T) {
	msg := &MsgUnlockCredits{Router: validAddr, LockId: "\tlock-1"}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "whitespace")
}

func TestMsgSettleCredits_RouteType(t *testing.T) {
	msg := &MsgSettleCredits{}
	assert.Equal(t, RouterKey, msg.Route())
	assert.Equal(t, TypeMsgSettleCredits, msg.Type())
}

func TestMsgSettleCredits_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgSettleCredits{
		Router:     validAddr,
		LockId:     "lock-1",
		ReceiptId:  "receipt-1",
		ToolId:     "tool-1",
		Publisher:  validAddr,
		ActualCost: validCoin(),
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgSettleCredits_ValidateBasic_ZeroActualCost(t *testing.T) {
	msg := &MsgSettleCredits{
		Router:     validAddr,
		LockId:     "lock-1",
		ReceiptId:  "receipt-1",
		ToolId:     "tool-1",
		Publisher:  validAddr,
		ActualCost: &basev1beta1.Coin{Denom: "ulac", Amount: "0"},
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgSettleCredits_ValidateBasic_EmptyRouter(t *testing.T) {
	msg := &MsgSettleCredits{
		Router: "", LockId: "l", ReceiptId: "r", ToolId: "t",
		Publisher: validAddr, ActualCost: validCoin(),
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgSettleCredits_ValidateBasic_EmptyLockID(t *testing.T) {
	msg := &MsgSettleCredits{
		Router: validAddr, LockId: "", ReceiptId: "r", ToolId: "t",
		Publisher: validAddr, ActualCost: validCoin(),
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgSettleCredits_ValidateBasic_EmptyReceiptID(t *testing.T) {
	msg := &MsgSettleCredits{
		Router: validAddr, LockId: "l", ReceiptId: "", ToolId: "t",
		Publisher: validAddr, ActualCost: validCoin(),
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgSettleCredits_ValidateBasic_EmptyToolID(t *testing.T) {
	msg := &MsgSettleCredits{
		Router: validAddr, LockId: "l", ReceiptId: "r", ToolId: "",
		Publisher: validAddr, ActualCost: validCoin(),
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgSettleCredits_ValidateBasic_RejectsPaddedIDs(t *testing.T) {
	base := func() MsgSettleCredits {
		return MsgSettleCredits{
			Router:     validAddr,
			LockId:     "lock-1",
			ReceiptId:  "receipt-1",
			ToolId:     "tool-1",
			Publisher:  validAddr,
			ActualCost: validCoin(),
		}
	}
	tests := []struct {
		name string
		edit func(*MsgSettleCredits)
	}{
		{name: "lock id", edit: func(m *MsgSettleCredits) { m.LockId = " lock-1" }},
		{name: "receipt id", edit: func(m *MsgSettleCredits) { m.ReceiptId = "receipt-1 " }},
		{name: "tool id", edit: func(m *MsgSettleCredits) { m.ToolId = "\ttool-1" }},
		{name: "toolpack id", edit: func(m *MsgSettleCredits) { m.ToolpackId = " toolpack-1 " }},
		{name: "origin tool id", edit: func(m *MsgSettleCredits) {
			m.CacheHit = true
			m.OriginToolId = " tool-origin"
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := base()
			tc.edit(&msg)
			err := msg.ValidateBasic()
			require.Error(t, err)
			require.Contains(t, err.Error(), "whitespace")
		})
	}
}

func TestMsgSettleCredits_ValidateBasic_InvalidPublisher(t *testing.T) {
	msg := &MsgSettleCredits{
		Router: validAddr, LockId: "l", ReceiptId: "r", ToolId: "t",
		Publisher: "bad_addr", ActualCost: validCoin(),
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgSettleCredits_ValidateBasic_NilActualCost(t *testing.T) {
	msg := &MsgSettleCredits{
		Router: validAddr, LockId: "l", ReceiptId: "r", ToolId: "t",
		Publisher: validAddr, ActualCost: nil,
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgSettleCredits_ValidateBasic_CacheHitMissingOrigin(t *testing.T) {
	msg := &MsgSettleCredits{
		Router: validAddr, LockId: "l", ReceiptId: "r", ToolId: "tool-1",
		Publisher: validAddr, ActualCost: validCoin(),
		CacheHit: true, OriginToolId: "",
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgSettleCredits_ValidateBasic_CacheHitSameToolID(t *testing.T) {
	msg := &MsgSettleCredits{
		Router: validAddr, LockId: "l", ReceiptId: "r", ToolId: "tool-1",
		Publisher: validAddr, ActualCost: validCoin(),
		CacheHit: true, OriginToolId: "tool-1",
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgSettleCredits_ValidateBasic_CacheHitValid(t *testing.T) {
	msg := &MsgSettleCredits{
		Router: validAddr, LockId: "l", ReceiptId: "r", ToolId: "tool-1",
		Publisher: validAddr, ActualCost: validCoin(),
		CacheHit: true, OriginToolId: "tool-original",
	}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgSettleCredits_ValidateBasic_InvalidReferrer(t *testing.T) {
	msg := &MsgSettleCredits{
		Router: validAddr, LockId: "l", ReceiptId: "r", ToolId: "t",
		Publisher: validAddr, ActualCost: validCoin(),
		Referrer: "bad_referrer",
	}
	require.Error(t, msg.ValidateBasic())
}

func TestMsgSettleCredits_ValidateBasic_ValidReferrer(t *testing.T) {
	msg := &MsgSettleCredits{
		Router: validAddr, LockId: "l", ReceiptId: "r", ToolId: "t",
		Publisher: validAddr, ActualCost: validCoin(),
		Referrer: validAddr,
	}
	require.NoError(t, msg.ValidateBasic())
}

func validOverdraftSettlementEntry() *OverdraftSettlementEntry {
	return &OverdraftSettlementEntry{
		RequestId:         "request-1",
		QuoteId:           "quote-1",
		ProvisionalLockId: "overdraft-lock-1",
		ReceiptId:         "receipt-1",
		ToolId:            "tool-1",
		QuotedCost:        validCoin(),
		ActualCost:        validCoin(),
		RefundAmount:      &basev1beta1.Coin{Denom: "ulac", Amount: "0"},
		InsuranceAmount:   &basev1beta1.Coin{Denom: "ulac", Amount: "0"},
		BurnAmount:        &basev1beta1.Coin{Denom: "ulac", Amount: "0"},
		Splits: []*OverdraftSettlementSplit{
			{Role: "publisher", Address: validAddr, Amount: validCoin()},
		},
		ToolpackId: "toolpack-1",
		Stage:      "finalized",
	}
}

func validSettleOverdraftMsg() *MsgSettleOverdraft {
	return &MsgSettleOverdraft{
		Router:                  validAddr,
		CreditLineId:            "credit-line-1",
		SettlementBatchId:       "batch-1",
		CreditLimit:             &basev1beta1.Coin{Denom: "ulac", Amount: "10000"},
		LiquidationThresholdBps: 8000,
		PolicyVersion:           "policy-v1",
		Entries:                 []*OverdraftSettlementEntry{validOverdraftSettlementEntry()},
	}
}

func TestMsgSettleOverdraft_RouteType(t *testing.T) {
	msg := &MsgSettleOverdraft{}
	assert.Equal(t, RouterKey, msg.Route())
	assert.Equal(t, TypeMsgSettleOverdraft, msg.Type())
}

func TestMsgSettleOverdraft_ValidateBasic_Valid(t *testing.T) {
	require.NoError(t, validSettleOverdraftMsg().ValidateBasic())
}

func TestMsgSettleOverdraft_ValidateBasic_RejectsMissingBatchFields(t *testing.T) {
	tests := []struct {
		name string
		edit func(*MsgSettleOverdraft)
		want string
	}{
		{name: "credit line", edit: func(m *MsgSettleOverdraft) { m.CreditLineId = "" }, want: "credit_line_id"},
		{name: "batch", edit: func(m *MsgSettleOverdraft) { m.SettlementBatchId = " batch-1" }, want: "whitespace"},
		{name: "policy", edit: func(m *MsgSettleOverdraft) { m.PolicyVersion = "" }, want: "policy_version"},
		{name: "entries", edit: func(m *MsgSettleOverdraft) { m.Entries = nil }, want: "entries"},
		{name: "threshold", edit: func(m *MsgSettleOverdraft) { m.LiquidationThresholdBps = 0 }, want: "liquidation_threshold_bps"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := validSettleOverdraftMsg()
			tc.edit(msg)
			err := msg.ValidateBasic()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestMsgSettleOverdraft_ValidateBasic_RejectsDuplicateRequestAndLock(t *testing.T) {
	msg := validSettleOverdraftMsg()
	second := validOverdraftSettlementEntry()
	second.QuoteId = "quote-2"
	second.ProvisionalLockId = "overdraft-lock-2"
	msg.Entries = append(msg.Entries, second)

	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate request_id")

	msg = validSettleOverdraftMsg()
	second = validOverdraftSettlementEntry()
	second.RequestId = "request-2"
	msg.Entries = append(msg.Entries, second)
	err = msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate provisional_lock_id")
}

func TestMsgSettleOverdraft_ValidateBasic_RejectsEntryDenomMismatch(t *testing.T) {
	msg := validSettleOverdraftMsg()
	msg.Entries[0].ActualCost = &basev1beta1.Coin{Denom: "uatom", Amount: "1000"}

	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "credit_limit denom")
}

func TestMsgSettleOverdraft_ValidateBasic_RejectsNegativeComponent(t *testing.T) {
	msg := validSettleOverdraftMsg()
	msg.Entries[0].RefundAmount = &basev1beta1.Coin{Denom: "ulac", Amount: "-1"}

	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-negative")
}

func TestMsgUpdateParams_RouteType(t *testing.T) {
	msg := &MsgUpdateParams{}
	assert.Equal(t, RouterKey, msg.Route())
	assert.Equal(t, TypeMsgUpdateParams, msg.Type())
}

func TestMsgUpdateParams_ValidateBasic_Valid(t *testing.T) {
	msg := &MsgUpdateParams{Authority: validAddr}
	require.NoError(t, msg.ValidateBasic())
}

func TestMsgUpdateParams_ValidateBasic_EmptyAuthority(t *testing.T) {
	msg := &MsgUpdateParams{Authority: ""}
	require.Error(t, msg.ValidateBasic())
}

// ---------- Proto Helpers ----------

func TestCoinToProto(t *testing.T) {
	c := sdk.NewInt64Coin("ulac", 500)
	proto := CoinToProto(c)
	require.NotNil(t, proto)
	assert.Equal(t, "ulac", proto.Denom)
	assert.Equal(t, "500", proto.Amount)
}

func TestCoinFromProto_Valid(t *testing.T) {
	p := &basev1beta1.Coin{Denom: "ulac", Amount: "123"}
	c := CoinFromProto(p)
	assert.Equal(t, "ulac", c.Denom)
	assert.True(t, c.Amount.Equal(math.NewInt(123)))
}

func TestCoinFromProto_Nil(t *testing.T) {
	c := CoinFromProto(nil)
	assert.True(t, c.IsZero())
}

func TestCoinsToProto_Empty(t *testing.T) {
	assert.Nil(t, CoinsToProto(nil))
	assert.Nil(t, CoinsToProto(sdk.Coins{}))
}

func TestCoinsToProto_Multiple(t *testing.T) {
	coins := sdk.NewCoins(sdk.NewInt64Coin("ulac", 100), sdk.NewInt64Coin("ulume", 200))
	protos := CoinsToProto(coins)
	require.Len(t, protos, 2)
}

func TestCoinsFromProto_Empty(t *testing.T) {
	c := CoinsFromProto(nil)
	assert.Empty(t, c)
}

func TestCoinsRoundTrip(t *testing.T) {
	original := sdk.NewCoins(sdk.NewInt64Coin("ulac", 42))
	proto := CoinsToProto(original)
	back := CoinsFromProto(proto)
	assert.True(t, original.Equal(back))
}

// ---------- Lock Helpers ----------

func TestLock_AmountCoin(t *testing.T) {
	lock := &Lock{Amount: validCoin()}
	c := lock.AmountCoin()
	assert.Equal(t, "ulac", c.Denom)
	assert.True(t, c.Amount.Equal(math.NewInt(1000)))
}

func TestLock_SetAmountCoin(t *testing.T) {
	lock := &Lock{}
	lock.SetAmountCoin(sdk.NewInt64Coin("ulac", 500))
	assert.Equal(t, "500", lock.Amount.Amount)
}

func TestLock_SetAmountCoin_NilLock(t *testing.T) {
	var lock *Lock
	lock.SetAmountCoin(sdk.NewInt64Coin("ulac", 1))
	// should not panic
}

func TestLock_CreatedAtTime(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	lock := &Lock{CreatedAt: timestamppb.New(now)}
	assert.Equal(t, now, lock.CreatedAtTime())
}

func TestLock_CreatedAtTime_Nil(t *testing.T) {
	var lock *Lock
	assert.True(t, lock.CreatedAtTime().IsZero())
}

func TestLock_SetCreatedAtTime(t *testing.T) {
	lock := &Lock{}
	now := time.Now().UTC()
	lock.SetCreatedAtTime(now)
	assert.NotNil(t, lock.CreatedAt)
}

func TestLock_ExpiresAtTime(t *testing.T) {
	future := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	lock := &Lock{ExpiresAt: timestamppb.New(future)}
	assert.Equal(t, future, lock.ExpiresAtTime())
}

func TestLock_ExpiresAtTime_Nil(t *testing.T) {
	lock := &Lock{}
	assert.True(t, lock.ExpiresAtTime().IsZero())
}

func TestLock_SetExpiresAtTime(t *testing.T) {
	lock := &Lock{}
	future := time.Now().Add(time.Hour).UTC()
	lock.SetExpiresAtTime(future)
	assert.NotNil(t, lock.ExpiresAt)
}

// ---------- Timestamp Helpers ----------

func TestProtoTimestampOrZero_Nil(t *testing.T) {
	assert.True(t, ProtoTimestampOrZero(nil).IsZero())
}

func TestProtoTimestampOrZero_Valid(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	ts := timestamppb.New(now)
	assert.Equal(t, now, ProtoTimestampOrZero(ts))
}

func TestTimeToProto_Zero(t *testing.T) {
	assert.Nil(t, TimeToProto(time.Time{}))
}

func TestTimeToProto_Valid(t *testing.T) {
	now := time.Now().UTC()
	ts := TimeToProto(now)
	require.NotNil(t, ts)
	assert.Equal(t, now.Unix(), ts.AsTime().Unix())
}

// ---------- Settlement Helpers ----------

func TestSettlement_AmountBurnedCoin(t *testing.T) {
	s := &Settlement{AmountBurned: &basev1beta1.Coin{Denom: "ulac", Amount: "750"}}
	c := s.AmountBurnedCoin()
	assert.Equal(t, "ulac", c.Denom)
	assert.True(t, c.Amount.Equal(math.NewInt(750)))
}

func TestSettlement_AmountBurnedCoin_Nil(t *testing.T) {
	s := &Settlement{}
	c := s.AmountBurnedCoin()
	assert.True(t, c.IsZero())
}

func TestSettlement_SetAmountBurnedCoin(t *testing.T) {
	s := &Settlement{}
	s.SetAmountBurnedCoin(sdk.NewInt64Coin("ulac", 300))
	require.NotNil(t, s.AmountBurned)
	assert.Equal(t, "300", s.AmountBurned.Amount)
}

func TestSettlement_SetAmountBurnedCoin_NilReceiver(t *testing.T) {
	var s *Settlement
	s.SetAmountBurnedCoin(sdk.NewInt64Coin("ulac", 1))
	// should not panic
}

func TestSettlement_RefundAmountCoin(t *testing.T) {
	s := &Settlement{RefundAmount: &basev1beta1.Coin{Denom: "ulac", Amount: "250"}}
	c := s.RefundAmountCoin()
	assert.Equal(t, "ulac", c.Denom)
	assert.True(t, c.Amount.Equal(math.NewInt(250)))
}

func TestSettlement_RefundAmountCoin_Nil(t *testing.T) {
	s := &Settlement{}
	c := s.RefundAmountCoin()
	assert.True(t, c.IsZero())
}

func TestSettlement_SetRefundAmountCoin(t *testing.T) {
	s := &Settlement{}
	s.SetRefundAmountCoin(sdk.NewInt64Coin("ulac", 150))
	require.NotNil(t, s.RefundAmount)
	assert.Equal(t, "150", s.RefundAmount.Amount)
}

func TestSettlement_SetRefundAmountCoin_NilReceiver(t *testing.T) {
	var s *Settlement
	s.SetRefundAmountCoin(sdk.NewInt64Coin("ulac", 1))
	// should not panic
}

func TestSettlement_SettledAtTime(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	s := &Settlement{SettledAt: timestamppb.New(now)}
	assert.Equal(t, now, s.SettledAtTime())
}

func TestSettlement_SettledAtTime_Nil(t *testing.T) {
	s := &Settlement{}
	assert.True(t, s.SettledAtTime().IsZero())
}

func TestSettlement_SettledAtTime_NilReceiver(t *testing.T) {
	var s *Settlement
	assert.True(t, s.SettledAtTime().IsZero())
}

func TestSettlement_SetSettledAtTime(t *testing.T) {
	s := &Settlement{}
	now := time.Now().UTC()
	s.SetSettledAtTime(now)
	require.NotNil(t, s.SettledAt)
}

func TestSettlement_SetSettledAtTime_NilReceiver(t *testing.T) {
	var s *Settlement
	s.SetSettledAtTime(time.Now())
	// should not panic
}

// ---------- SettlementRecord Helpers ----------

func TestSettlementRecord_TotalCostCoins(t *testing.T) {
	sr := &SettlementRecord{
		TotalCost: []*basev1beta1.Coin{{Denom: "ulac", Amount: "1000"}},
	}
	coins := sr.TotalCostCoins()
	require.Len(t, coins, 1)
	assert.True(t, coins.AmountOf("ulac").Equal(math.NewInt(1000)))
}

func TestSettlementRecord_TotalCostCoins_Empty(t *testing.T) {
	sr := &SettlementRecord{}
	coins := sr.TotalCostCoins()
	assert.Empty(t, coins)
}

func TestSettlementRecord_BurnAmountCoins(t *testing.T) {
	sr := &SettlementRecord{
		BurnAmount: []*basev1beta1.Coin{{Denom: "ulac", Amount: "500"}},
	}
	coins := sr.BurnAmountCoins()
	require.Len(t, coins, 1)
	assert.True(t, coins.AmountOf("ulac").Equal(math.NewInt(500)))
}

func TestSettlementRecord_NetAmountCoins(t *testing.T) {
	sr := &SettlementRecord{
		NetAmount: []*basev1beta1.Coin{{Denom: "ulac", Amount: "750"}},
	}
	coins := sr.NetAmountCoins()
	require.Len(t, coins, 1)
	assert.True(t, coins.AmountOf("ulac").Equal(math.NewInt(750)))
}

func TestSettlementRecord_SetTotalCostCoins(t *testing.T) {
	sr := &SettlementRecord{}
	sr.SetTotalCostCoins(sdk.NewCoins(sdk.NewInt64Coin("ulac", 2000)))
	require.Len(t, sr.TotalCost, 1)
	assert.Equal(t, "2000", sr.TotalCost[0].Amount)
}

func TestSettlementRecord_SetTotalCostCoins_NilReceiver(t *testing.T) {
	var sr *SettlementRecord
	sr.SetTotalCostCoins(sdk.NewCoins(sdk.NewInt64Coin("ulac", 1)))
	// should not panic
}

func TestSettlementRecord_SetBurnAmountCoins(t *testing.T) {
	sr := &SettlementRecord{}
	sr.SetBurnAmountCoins(sdk.NewCoins(sdk.NewInt64Coin("ulac", 100)))
	require.Len(t, sr.BurnAmount, 1)
	assert.Equal(t, "100", sr.BurnAmount[0].Amount)
}

func TestSettlementRecord_SetNetAmountCoins(t *testing.T) {
	sr := &SettlementRecord{}
	sr.SetNetAmountCoins(sdk.NewCoins(sdk.NewInt64Coin("ulac", 900)))
	require.Len(t, sr.NetAmount, 1)
	assert.Equal(t, "900", sr.NetAmount[0].Amount)
}

func TestSettlementRecord_TimestampTime(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	sr := &SettlementRecord{Timestamp: timestamppb.New(now)}
	assert.Equal(t, now, sr.TimestampTime())
}

func TestSettlementRecord_TimestampTime_Nil(t *testing.T) {
	sr := &SettlementRecord{}
	assert.True(t, sr.TimestampTime().IsZero())
}

func TestSettlementRecord_TimestampTime_NilReceiver(t *testing.T) {
	var sr *SettlementRecord
	assert.True(t, sr.TimestampTime().IsZero())
}

func TestSettlementRecord_SetTimestampTime(t *testing.T) {
	sr := &SettlementRecord{}
	now := time.Now().UTC()
	sr.SetTimestampTime(now)
	require.NotNil(t, sr.Timestamp)
}

func TestSettlementRecord_SetTimestampTime_NilReceiver(t *testing.T) {
	var sr *SettlementRecord
	sr.SetTimestampTime(time.Now())
	// should not panic
}

func TestSettlementRecord_CompletedAtTime(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	sr := &SettlementRecord{CompletedAt: timestamppb.New(now)}
	result := sr.CompletedAtTime()
	require.NotNil(t, result)
	assert.Equal(t, now, *result)
}

func TestSettlementRecord_CompletedAtTime_Nil(t *testing.T) {
	sr := &SettlementRecord{}
	assert.Nil(t, sr.CompletedAtTime())
}

func TestSettlementRecord_CompletedAtTime_NilReceiver(t *testing.T) {
	var sr *SettlementRecord
	assert.Nil(t, sr.CompletedAtTime())
}

func TestSettlementRecord_SetCompletedAtTime(t *testing.T) {
	sr := &SettlementRecord{}
	now := time.Now().UTC()
	sr.SetCompletedAtTime(&now)
	require.NotNil(t, sr.CompletedAt)
}

func TestSettlementRecord_SetCompletedAtTime_NilTime(t *testing.T) {
	now := time.Now().UTC()
	sr := &SettlementRecord{CompletedAt: timestamppb.New(now)}
	sr.SetCompletedAtTime(nil)
	assert.Nil(t, sr.CompletedAt)
}

func TestSettlementRecord_SetCompletedAtTime_NilReceiver(t *testing.T) {
	var sr *SettlementRecord
	now := time.Now()
	sr.SetCompletedAtTime(&now)
	// should not panic
}

// ---------- DisputeRecord Helpers ----------

func TestDisputeRecord_CreatedAtTime(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	dr := &DisputeRecord{CreatedAt: timestamppb.New(now)}
	assert.Equal(t, now, dr.CreatedAtTime())
}

func TestDisputeRecord_CreatedAtTime_Nil(t *testing.T) {
	dr := &DisputeRecord{}
	assert.True(t, dr.CreatedAtTime().IsZero())
}

func TestDisputeRecord_CreatedAtTime_NilReceiver(t *testing.T) {
	var dr *DisputeRecord
	assert.True(t, dr.CreatedAtTime().IsZero())
}

func TestDisputeRecord_SetCreatedAtTime(t *testing.T) {
	dr := &DisputeRecord{}
	now := time.Now().UTC()
	dr.SetCreatedAtTime(now)
	require.NotNil(t, dr.CreatedAt)
}

func TestDisputeRecord_SetCreatedAtTime_NilReceiver(t *testing.T) {
	var dr *DisputeRecord
	dr.SetCreatedAtTime(time.Now())
	// should not panic
}

func TestDisputeRecord_ResolvedAtTime(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	dr := &DisputeRecord{ResolvedAt: timestamppb.New(now)}
	result := dr.ResolvedAtTime()
	require.NotNil(t, result)
	assert.Equal(t, now, *result)
}

func TestDisputeRecord_ResolvedAtTime_Nil(t *testing.T) {
	dr := &DisputeRecord{}
	assert.Nil(t, dr.ResolvedAtTime())
}

func TestDisputeRecord_ResolvedAtTime_NilReceiver(t *testing.T) {
	var dr *DisputeRecord
	assert.Nil(t, dr.ResolvedAtTime())
}

func TestDisputeRecord_SetResolvedAtTime(t *testing.T) {
	dr := &DisputeRecord{}
	now := time.Now().UTC()
	dr.SetResolvedAtTime(&now)
	require.NotNil(t, dr.ResolvedAt)
}

func TestDisputeRecord_SetResolvedAtTime_NilTime(t *testing.T) {
	now := time.Now().UTC()
	dr := &DisputeRecord{ResolvedAt: timestamppb.New(now)}
	dr.SetResolvedAtTime(nil)
	assert.Nil(t, dr.ResolvedAt)
}

func TestDisputeRecord_SetResolvedAtTime_NilReceiver(t *testing.T) {
	var dr *DisputeRecord
	now := time.Now()
	dr.SetResolvedAtTime(&now)
	// should not panic
}
