
package types

import (
	"strings"
	"testing"

	v1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const validAddr = "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"

func validStakeCoin() *v1beta1.Coin {
	return &v1beta1.Coin{Denom: "ulume", Amount: "100000000"}
}

func requireEqualComparable[T comparable](t *testing.T, got, want T, name string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

func validGenesisPassport(id, agentPubkey string) *AgentPassport {
	return &AgentPassport{
		PassportId:   id,
		AgentPubkey:  agentPubkey,
		OwnerAddress: validAddr,
		Stake:        validStakeCoin(),
		Status:       PassportStatus_PASSPORT_STATUS_ACTIVE,
	}
}

// ---------------------------------------------------------------------------
// DefaultParams
// ---------------------------------------------------------------------------

func TestDefaultParams(t *testing.T) {
	t.Parallel()
	p := DefaultParams()
	if p == nil {
		t.Fatal("DefaultParams() returned nil")
	}
	if p.MinStake == nil {
		t.Fatal("MinStake should not be nil")
	}
	if p.MinStake.Denom != DefaultMinStakeDenom {
		t.Errorf("denom = %q, want %q", p.MinStake.Denom, DefaultMinStakeDenom)
	}
	if p.SlashRateBps != DefaultSlashRateBPS {
		t.Errorf("SlashRateBps = %d, want %d", p.SlashRateBps, DefaultSlashRateBPS)
	}
	if p.RevocationGraceSeconds != DefaultRevocationGraceSeconds {
		t.Errorf("RevocationGraceSeconds = %d, want %d", p.RevocationGraceSeconds, DefaultRevocationGraceSeconds)
	}
	if p.CollusionRiskThresholdBps != DefaultCollusionRiskThresholdBPS {
		t.Errorf("CollusionRiskThresholdBps = %d, want %d", p.CollusionRiskThresholdBps, DefaultCollusionRiskThresholdBPS)
	}
	requireEqualComparable(t, p.CollusionVerificationPenaltyBps, DefaultCollusionVerificationPenaltyBPS, "CollusionVerificationPenaltyBps")
	if p.CollusionMaxPayerShareBps != DefaultCollusionMaxPayerShareBPS {
		t.Errorf("CollusionMaxPayerShareBps = %d, want %d", p.CollusionMaxPayerShareBps, DefaultCollusionMaxPayerShareBPS)
	}
	if p.CollusionMaxPublisherShareBps != DefaultCollusionMaxPublisherShareBPS {
		t.Errorf("CollusionMaxPublisherShareBps = %d, want %d", p.CollusionMaxPublisherShareBps, DefaultCollusionMaxPublisherShareBPS)
	}
	if p.CollusionMaxToolShareBps != DefaultCollusionMaxToolShareBPS {
		t.Errorf("CollusionMaxToolShareBps = %d, want %d", p.CollusionMaxToolShareBps, DefaultCollusionMaxToolShareBPS)
	}
}

func TestDefaultParams_Validate(t *testing.T) {
	t.Parallel()
	if err := DefaultParams().Validate(); err != nil {
		t.Fatalf("DefaultParams().Validate() = %v", err)
	}
}

// ---------------------------------------------------------------------------
// Params.Validate
// ---------------------------------------------------------------------------

func TestParams_Validate_NilMinStake(t *testing.T) {
	t.Parallel()
	p := DefaultParams()
	p.MinStake = nil
	if err := p.Validate(); err == nil {
		t.Error("expected error for nil MinStake")
	}
}

func TestParams_Validate_EmptyDenom(t *testing.T) {
	t.Parallel()
	p := DefaultParams()
	p.MinStake.Denom = ""
	if err := p.Validate(); err == nil {
		t.Error("expected error for empty denom")
	}
}

func TestParams_Validate_SlashRateOverMax(t *testing.T) {
	t.Parallel()
	p := DefaultParams()
	p.SlashRateBps = 10001
	if err := p.Validate(); err == nil {
		t.Error("expected error for slash rate > 10000")
	}
}

func TestParams_Validate_SlashRateExactMax(t *testing.T) {
	t.Parallel()
	p := DefaultParams()
	p.SlashRateBps = 10000
	if err := p.Validate(); err != nil {
		t.Errorf("slash rate 10000 should be valid, got error: %v", err)
	}
}

func TestParams_Validate_ZeroSlashRate(t *testing.T) {
	t.Parallel()
	p := DefaultParams()
	p.SlashRateBps = 0
	if err := p.Validate(); err != nil {
		t.Errorf("slash rate 0 should be valid, got error: %v", err)
	}
}

func TestParams_Validate_CollusionGovernanceKnobs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Params)
		wantErr string
	}{
		{
			name: "zero risk threshold",
			mutate: func(p *Params) {
				p.CollusionRiskThresholdBps = 0
			},
			wantErr: "collusion_risk_threshold_bps must be positive",
		},
		{
			name: "risk threshold above max",
			mutate: func(p *Params) {
				p.CollusionRiskThresholdBps = 10001
			},
			wantErr: "collusion_risk_threshold_bps cannot exceed",
		},
		{
			name: "zero verification penalty",
			mutate: func(p *Params) {
				p.CollusionVerificationPenaltyBps = 0
			},
			wantErr: "collusion_verification_penalty_bps must be positive",
		},
		{
			name: "payer share above max",
			mutate: func(p *Params) {
				p.CollusionMaxPayerShareBps = 10001
			},
			wantErr: "collusion_max_payer_share_bps cannot exceed",
		},
		{
			name: "publisher share above max",
			mutate: func(p *Params) {
				p.CollusionMaxPublisherShareBps = 10001
			},
			wantErr: "collusion_max_publisher_share_bps cannot exceed",
		},
		{
			name: "tool share above max",
			mutate: func(p *Params) {
				p.CollusionMaxToolShareBps = 10001
			},
			wantErr: "collusion_max_tool_share_bps cannot exceed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := DefaultParams()
			tc.mutate(p)
			err := p.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestScoringConfigFromParams_CollusionGovernanceKnobs(t *testing.T) {
	t.Parallel()
	p := DefaultParams()
	p.CollusionRiskThresholdBps = 6500
	p.CollusionVerificationPenaltyBps = 4500
	p.CollusionMaxPayerShareBps = 5500
	p.CollusionMaxPublisherShareBps = 5600
	p.CollusionMaxToolShareBps = 7500

	cfg := ScoringConfigFromParams(p)
	if cfg.CollusionRiskThreshold != 0.65 {
		t.Fatalf("CollusionRiskThreshold = %v, want 0.65", cfg.CollusionRiskThreshold)
	}
	if cfg.CollusionVerificationPenalty != 0.45 {
		t.Fatalf("CollusionVerificationPenalty = %v, want 0.45", cfg.CollusionVerificationPenalty)
	}
	if cfg.CollusionMaxPayerShare != 0.55 {
		t.Fatalf("CollusionMaxPayerShare = %v, want 0.55", cfg.CollusionMaxPayerShare)
	}
	if cfg.CollusionMaxPublisherShare != 0.56 {
		t.Fatalf("CollusionMaxPublisherShare = %v, want 0.56", cfg.CollusionMaxPublisherShare)
	}
	if cfg.CollusionMaxToolShare != 0.75 {
		t.Fatalf("CollusionMaxToolShare = %v, want 0.75", cfg.CollusionMaxToolShare)
	}
}

func TestScoringConfigFromParams_LegacyZeroesFallBack(t *testing.T) {
	t.Parallel()
	cfg := ScoringConfigFromParams(&Params{})
	defaults := DefaultScoringConfig()
	if cfg.CollusionRiskThreshold != defaults.CollusionRiskThreshold {
		t.Fatalf("CollusionRiskThreshold = %v, want default %v", cfg.CollusionRiskThreshold, defaults.CollusionRiskThreshold)
	}
	requireEqualComparable(t, cfg.CollusionVerificationPenalty, defaults.CollusionVerificationPenalty, "CollusionVerificationPenalty")
	if cfg.CollusionMaxPayerShare != defaults.CollusionMaxPayerShare {
		t.Fatalf("CollusionMaxPayerShare = %v, want default %v", cfg.CollusionMaxPayerShare, defaults.CollusionMaxPayerShare)
	}
	if cfg.CollusionMaxPublisherShare != defaults.CollusionMaxPublisherShare {
		t.Fatalf("CollusionMaxPublisherShare = %v, want default %v", cfg.CollusionMaxPublisherShare, defaults.CollusionMaxPublisherShare)
	}
	if cfg.CollusionMaxToolShare != defaults.CollusionMaxToolShare {
		t.Fatalf("CollusionMaxToolShare = %v, want default %v", cfg.CollusionMaxToolShare, defaults.CollusionMaxToolShare)
	}
}

// ---------------------------------------------------------------------------
// Params.MinStakeCoin
// ---------------------------------------------------------------------------

func TestMinStakeCoin_Default(t *testing.T) {
	t.Parallel()
	p := DefaultParams()
	c := p.MinStakeCoin()
	if c.Denom != DefaultMinStakeDenom {
		t.Errorf("denom = %q, want %q", c.Denom, DefaultMinStakeDenom)
	}
	if !c.Amount.Equal(math.NewInt(DefaultMinStakeAmount)) {
		t.Errorf("amount = %s, want %d", c.Amount, DefaultMinStakeAmount)
	}
}

func TestMinStakeCoin_NilMinStake(t *testing.T) {
	t.Parallel()
	p := &Params{MinStake: nil}
	c := p.MinStakeCoin()
	if c.Denom != DefaultMinStakeDenom {
		t.Errorf("nil MinStake denom = %q, want %q", c.Denom, DefaultMinStakeDenom)
	}
}

// ---------------------------------------------------------------------------
// MsgRegisterPassport
// ---------------------------------------------------------------------------

func TestMsgRegisterPassport_ValidateBasic_Valid(t *testing.T) {
	t.Parallel()
	msg := &MsgRegisterPassport{
		Creator:     validAddr,
		AgentPubkey: "agent-pubkey-123",
		Stake:       validStakeCoin(),
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestMsgRegisterPassport_ValidateBasic_EmptyCreator(t *testing.T) {
	t.Parallel()
	msg := &MsgRegisterPassport{
		Creator:     "",
		AgentPubkey: "pubkey",
		Stake:       validStakeCoin(),
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty creator")
	}
}

func TestMsgRegisterPassport_ValidateBasic_InvalidCreator(t *testing.T) {
	t.Parallel()
	msg := &MsgRegisterPassport{
		Creator:     "not-a-bech32",
		AgentPubkey: "pubkey",
		Stake:       validStakeCoin(),
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for invalid creator address")
	}
}

func TestMsgRegisterPassport_ValidateBasic_EmptyPubkey(t *testing.T) {
	t.Parallel()
	msg := &MsgRegisterPassport{
		Creator:     validAddr,
		AgentPubkey: "",
		Stake:       validStakeCoin(),
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty agent pubkey")
	}
}

func TestMsgRegisterPassport_ValidateBasic_WhitespacePubkey(t *testing.T) {
	t.Parallel()
	msg := &MsgRegisterPassport{
		Creator:     validAddr,
		AgentPubkey: "   ",
		Stake:       validStakeCoin(),
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for whitespace-only pubkey")
	}
}

func TestMsgRegisterPassport_ValidateBasic_PaddedPubkey(t *testing.T) {
	t.Parallel()
	msg := &MsgRegisterPassport{
		Creator:     validAddr,
		AgentPubkey: " ed25519:agent-pubkey-123 ",
		Stake:       validStakeCoin(),
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for padded agent pubkey")
	}
}

func TestMsgRegisterPassport_ValidateBasic_NilStake(t *testing.T) {
	t.Parallel()
	msg := &MsgRegisterPassport{
		Creator:     validAddr,
		AgentPubkey: "pubkey",
		Stake:       nil,
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for nil stake")
	}
}

func TestMsgRegisterPassport_ValidateBasic_ZeroStake(t *testing.T) {
	t.Parallel()
	msg := &MsgRegisterPassport{
		Creator:     validAddr,
		AgentPubkey: "pubkey",
		Stake:       &v1beta1.Coin{Denom: "ulume", Amount: "0"},
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for zero stake")
	}
}

func TestMsgRegisterPassport_ValidateBasic_InvalidStakeCoin(t *testing.T) {
	t.Parallel()
	tests := map[string]*v1beta1.Coin{
		"invalid denom":  {Denom: "1bad", Amount: "100"},
		"invalid amount": {Denom: "ulume", Amount: "not-a-number"},
		"negative":       {Denom: "ulume", Amount: "-1"},
	}
	for name, stake := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			msg := &MsgRegisterPassport{
				Creator:     validAddr,
				AgentPubkey: "pubkey",
				Stake:       stake,
			}
			if err := msg.ValidateBasic(); err == nil {
				t.Fatal("expected error for invalid stake coin")
			}
		})
	}
}

func TestMsgRegisterPassport_GetSigners(t *testing.T) {
	t.Parallel()
	msg := &MsgRegisterPassport{Creator: validAddr}
	signers := msg.GetSigners()
	if len(signers) != 1 {
		t.Fatalf("expected 1 signer, got %d", len(signers))
	}
	expected, _ := sdk.AccAddressFromBech32(validAddr)
	if !signers[0].Equals(expected) {
		t.Errorf("signer = %s, want %s", signers[0], expected)
	}
}

func TestMsgUpdateParams_ValidateBasic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		msg     *MsgUpdateParams
		wantErr string
	}{
		{
			name: "valid",
			msg:  NewMsgUpdateParams(validAddr, DefaultParams()),
		},
		{
			name:    "invalid authority",
			msg:     NewMsgUpdateParams("not-an-address", DefaultParams()),
			wantErr: "invalid authority address",
		},
		{
			name:    "nil params",
			msg:     NewMsgUpdateParams(validAddr, nil),
			wantErr: "params cannot be nil",
		},
		{
			name: "invalid params",
			msg: func() *MsgUpdateParams {
				params := DefaultParams()
				params.CollusionRiskThresholdBps = 0
				return NewMsgUpdateParams(validAddr, params)
			}(),
			wantErr: "invalid params",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.msg.ValidateBasic()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateBasic() = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateBasic() error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestMsgUpdateParams_GetSigners(t *testing.T) {
	t.Parallel()
	msg := NewMsgUpdateParams(validAddr, DefaultParams())
	signers := msg.GetSigners()
	if len(signers) != 1 {
		t.Fatalf("signers len = %d, want 1", len(signers))
	}
	if signers[0].String() != validAddr {
		t.Fatalf("signer = %s, want %s", signers[0], validAddr)
	}
}

func TestNewMsgRegisterPassport(t *testing.T) {
	t.Parallel()
	coin := sdk.NewInt64Coin("ulume", 500)
	msg := NewMsgRegisterPassport(validAddr, "pub123", coin)
	if msg.Creator != validAddr {
		t.Errorf("Creator = %q, want %q", msg.Creator, validAddr)
	}
	if msg.AgentPubkey != "pub123" {
		t.Errorf("AgentPubkey = %q, want %q", msg.AgentPubkey, "pub123")
	}
	if msg.Stake == nil {
		t.Fatal("Stake should not be nil")
	}
}

type passportIDMsg interface {
	ValidateBasic() error
}

func passportIDMessages(passportID string) map[string]passportIDMsg {
	return map[string]passportIDMsg{
		"suspend":    &MsgSuspendPassport{Authority: validAddr, PassportId: passportID},
		"revoke":     &MsgRevokePassport{Authority: validAddr, PassportId: passportID},
		"reactivate": &MsgReactivatePassport{Owner: validAddr, PassportId: passportID},
		"slash":      &MsgSlashStake{Authority: validAddr, PassportId: passportID, Amount: validStakeCoin()},
		"top_up":     &MsgTopUpStake{Owner: validAddr, PassportId: passportID, Amount: validStakeCoin()},
		"unregister": &MsgUnregisterPassport{Owner: validAddr, PassportId: passportID},
	}
}

func TestPassportIDMessages_ValidateBasic_NonCanonicalPassportID(t *testing.T) {
	t.Parallel()
	for name, msg := range passportIDMessages(" passport-1 ") {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := msg.ValidateBasic(); err == nil {
				t.Fatal("expected non-canonical passport_id error")
			}
		})
	}
}

func TestPassportIDMessages_ValidateBasic_OversizedPassportID(t *testing.T) {
	t.Parallel()
	oversizedID := strings.Repeat("p", MaxPassportIDLen+1)
	for name, msg := range passportIDMessages(oversizedID) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := msg.ValidateBasic(); err == nil {
				t.Fatal("expected oversized passport_id error")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MsgSuspendPassport
// ---------------------------------------------------------------------------

func TestMsgSuspendPassport_ValidateBasic_Valid(t *testing.T) {
	t.Parallel()
	msg := &MsgSuspendPassport{
		Authority:  validAddr,
		PassportId: "passport-1",
		Reason:     "violation",
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestMsgSuspendPassport_ValidateBasic_EmptyAuthority(t *testing.T) {
	t.Parallel()
	msg := &MsgSuspendPassport{Authority: "", PassportId: "p-1"}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty authority")
	}
}

func TestMsgSuspendPassport_ValidateBasic_EmptyPassportID(t *testing.T) {
	t.Parallel()
	msg := &MsgSuspendPassport{Authority: validAddr, PassportId: ""}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty passport ID")
	}
}

func TestNewMsgSuspendPassport(t *testing.T) {
	t.Parallel()
	msg := NewMsgSuspendPassport(validAddr, "p-1", "bad behavior")
	if msg.Authority != validAddr {
		t.Errorf("Authority = %q, want %q", msg.Authority, validAddr)
	}
	if msg.PassportId != "p-1" {
		t.Errorf("PassportId = %q, want %q", msg.PassportId, "p-1")
	}
	if msg.Reason != "bad behavior" {
		t.Errorf("Reason = %q, want %q", msg.Reason, "bad behavior")
	}
}

// ---------------------------------------------------------------------------
// MsgRevokePassport
// ---------------------------------------------------------------------------

func TestMsgRevokePassport_ValidateBasic_Valid(t *testing.T) {
	t.Parallel()
	msg := &MsgRevokePassport{Authority: validAddr, PassportId: "p-1"}
	if err := msg.ValidateBasic(); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestMsgRevokePassport_ValidateBasic_InvalidAuthority(t *testing.T) {
	t.Parallel()
	msg := &MsgRevokePassport{Authority: "bad", PassportId: "p-1"}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for invalid authority")
	}
}

func TestMsgRevokePassport_ValidateBasic_EmptyPassportID(t *testing.T) {
	t.Parallel()
	msg := &MsgRevokePassport{Authority: validAddr, PassportId: "  "}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for whitespace passport ID")
	}
}

func TestNewMsgRevokePassport(t *testing.T) {
	t.Parallel()
	msg := NewMsgRevokePassport(validAddr, "p-2", "revoke reason")
	if msg.PassportId != "p-2" {
		t.Errorf("PassportId = %q, want %q", msg.PassportId, "p-2")
	}
}

// ---------------------------------------------------------------------------
// MsgReactivatePassport
// ---------------------------------------------------------------------------

func TestMsgReactivatePassport_ValidateBasic_Valid(t *testing.T) {
	t.Parallel()
	msg := &MsgReactivatePassport{Owner: validAddr, PassportId: "p-1"}
	if err := msg.ValidateBasic(); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestMsgReactivatePassport_ValidateBasic_InvalidOwner(t *testing.T) {
	t.Parallel()
	msg := &MsgReactivatePassport{Owner: "invalid", PassportId: "p-1"}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for invalid owner")
	}
}

func TestMsgReactivatePassport_ValidateBasic_EmptyPassportID(t *testing.T) {
	t.Parallel()
	msg := &MsgReactivatePassport{Owner: validAddr, PassportId: ""}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty passport ID")
	}
}

func TestNewMsgReactivatePassport(t *testing.T) {
	t.Parallel()
	msg := NewMsgReactivatePassport(validAddr, "p-3")
	if msg.Owner != validAddr {
		t.Errorf("Owner = %q, want %q", msg.Owner, validAddr)
	}
}

// ---------------------------------------------------------------------------
// MsgSlashStake
// ---------------------------------------------------------------------------

func TestMsgSlashStake_ValidateBasic_Valid(t *testing.T) {
	t.Parallel()
	msg := &MsgSlashStake{
		Authority:  validAddr,
		PassportId: "p-1",
		Amount:     validStakeCoin(),
		Reason:     "slash reason",
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestMsgSlashStake_ValidateBasic_InvalidAuthority(t *testing.T) {
	t.Parallel()
	msg := &MsgSlashStake{Authority: "", PassportId: "p-1", Amount: validStakeCoin()}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty authority")
	}
}

func TestMsgSlashStake_ValidateBasic_EmptyPassportID(t *testing.T) {
	t.Parallel()
	msg := &MsgSlashStake{Authority: validAddr, PassportId: "", Amount: validStakeCoin()}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty passport ID")
	}
}

func TestMsgSlashStake_ValidateBasic_NilAmount(t *testing.T) {
	t.Parallel()
	msg := &MsgSlashStake{Authority: validAddr, PassportId: "p-1", Amount: nil}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for nil amount")
	}
}

func TestMsgSlashStake_ValidateBasic_ZeroAmount(t *testing.T) {
	t.Parallel()
	msg := &MsgSlashStake{
		Authority:  validAddr,
		PassportId: "p-1",
		Amount:     &v1beta1.Coin{Denom: "ulume", Amount: "0"},
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for zero slash amount")
	}
}

func TestMsgSlashStake_ValidateBasic_InvalidAmountCoin(t *testing.T) {
	t.Parallel()
	tests := map[string]*v1beta1.Coin{
		"invalid denom":  {Denom: "1bad", Amount: "100"},
		"invalid amount": {Denom: "ulume", Amount: "not-a-number"},
		"negative":       {Denom: "ulume", Amount: "-1"},
	}
	for name, amount := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			msg := &MsgSlashStake{Authority: validAddr, PassportId: "p-1", Amount: amount}
			if err := msg.ValidateBasic(); err == nil {
				t.Fatal("expected error for invalid slash amount coin")
			}
		})
	}
}

func TestNewMsgSlashStake(t *testing.T) {
	t.Parallel()
	coin := sdk.NewInt64Coin("ulume", 1000)
	msg := NewMsgSlashStake(validAddr, "p-1", coin, "test slash")
	if msg.Authority != validAddr {
		t.Errorf("Authority = %q, want %q", msg.Authority, validAddr)
	}
	if msg.Reason != "test slash" {
		t.Errorf("Reason = %q, want %q", msg.Reason, "test slash")
	}
}

// ---------------------------------------------------------------------------
// Message type constants
// ---------------------------------------------------------------------------

func TestMessageTypeConstants(t *testing.T) {
	t.Parallel()
	types := map[string]string{
		"register":   TypeMsgRegisterPassport,
		"suspend":    TypeMsgSuspendPassport,
		"revoke":     TypeMsgRevokePassport,
		"reactivate": TypeMsgReactivatePassport,
		"slash":      TypeMsgSlashStake,
	}
	seen := make(map[string]bool)
	for name, typ := range types {
		if typ == "" {
			t.Errorf("%s type constant is empty", name)
		}
		if seen[typ] {
			t.Errorf("duplicate type constant: %s", typ)
		}
		seen[typ] = true
	}
}

// ---------------------------------------------------------------------------
// Proto helpers
// ---------------------------------------------------------------------------

func TestCoinFromProto_Nil(t *testing.T) {
	t.Parallel()
	c := CoinFromProto(nil)
	// CoinFromProto(nil) returns sdk.Coin{} (zero-value struct with nil Amount).
	// We cannot call c.IsZero() because that dereferences nil Amount.
	if c.Denom != "" {
		t.Errorf("nil proto denom = %q, want empty", c.Denom)
	}
}

func TestCoinFromProto_Valid(t *testing.T) {
	t.Parallel()
	p := &v1beta1.Coin{Denom: "ulume", Amount: "500"}
	c := CoinFromProto(p)
	if c.Denom != "ulume" {
		t.Errorf("denom = %q, want %q", c.Denom, "ulume")
	}
	if !c.Amount.Equal(math.NewInt(500)) {
		t.Errorf("amount = %s, want 500", c.Amount)
	}
}

func TestCoinFromProto_InvalidAmount(t *testing.T) {
	t.Parallel()
	p := &v1beta1.Coin{Denom: "ulume", Amount: "not_a_number"}
	c := CoinFromProto(p)
	if !c.Amount.IsZero() {
		t.Errorf("invalid amount should return zero, got %s", c.Amount)
	}
}

func TestCoinToProto(t *testing.T) {
	t.Parallel()
	c := sdk.NewInt64Coin("ulume", 42)
	p := CoinToProto(c)
	if p.Denom != "ulume" {
		t.Errorf("denom = %q, want %q", p.Denom, "ulume")
	}
	if p.Amount != "42" {
		t.Errorf("amount = %q, want %q", p.Amount, "42")
	}
}

func TestCoinRoundtrip(t *testing.T) {
	t.Parallel()
	original := sdk.NewInt64Coin("ulume", 999)
	proto := CoinToProto(original)
	back := CoinFromProto(proto)
	if !original.Equal(back) {
		t.Errorf("roundtrip failed: %s != %s", original, back)
	}
}

func TestCoinsFromProto_Nil(t *testing.T) {
	t.Parallel()
	c := CoinsFromProto(nil)
	if !c.Empty() {
		t.Errorf("nil input should return empty coins, got %s", c)
	}
}

func TestCoinsFromProto_WithNilEntry(t *testing.T) {
	t.Parallel()
	input := []*v1beta1.Coin{
		{Denom: "ulume", Amount: "100"},
		nil,
		{Denom: "ulac", Amount: "200"},
	}
	c := CoinsFromProto(input)
	if len(c) != 2 {
		t.Errorf("expected 2 coins, got %d", len(c))
	}
}

func TestCoinsToProto(t *testing.T) {
	t.Parallel()
	coins := sdk.NewCoins(sdk.NewInt64Coin("ulume", 50))
	protos := CoinsToProto(coins)
	if len(protos) != 1 {
		t.Fatalf("expected 1 proto coin, got %d", len(protos))
	}
	if protos[0].Denom != "ulume" {
		t.Errorf("denom = %q, want %q", protos[0].Denom, "ulume")
	}
}

// ---------------------------------------------------------------------------
// GenesisState.Validate
//
// Coverage was zero before this block — all nine error branches and the
// happy path were unpinned. GenesisState.Validate runs at chain boot and
// on every InitChain; a silent regression here (e.g., dropping the
// duplicate-pubkey check) would let a malformed genesis file bring a
// chain up in a self-inconsistent state.
// ---------------------------------------------------------------------------

// TestDefaultGenesis_ValidatesSuccessfully is the happy-path smoke.
// Pinning DefaultGenesis specifically (not a hand-built one) also
// catches the case where DefaultParams silently drifts to a value
// that no longer passes Validate.
func TestDefaultGenesis_ValidatesSuccessfully(t *testing.T) {
	t.Parallel()
	gs := DefaultGenesis()
	if gs == nil {
		t.Fatal("DefaultGenesis() returned nil")
	}
	if err := gs.Validate(); err != nil {
		t.Fatalf("DefaultGenesis().Validate() = %v", err)
	}
}

// TestGenesisState_Validate_NilParams pins the nil-Params branch.
// GenesisState has no nil-receiver guard on purpose (callers never
// hand a nil GenesisState to Validate — DefaultGenesis always
// returns non-nil, and deserializers guarantee non-nil) — but the
// inner Params pointer can legitimately be unset in a malformed
// file, so that branch must stay covered.
func TestGenesisState_Validate_NilParams(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{Params: nil}
	if err := gs.Validate(); err == nil {
		t.Fatal("expected error for nil Params")
	}
}

// TestGenesisState_Validate_InvalidParamsBubbled pins that a
// Params.Validate() failure propagates through GenesisState.Validate
// rather than being silently swallowed. Without this pin, a refactor
// that flipped the `if err := ...; err != nil` to a bare
// `_ = gs.Params.Validate()` would let malformed params land in state.
func TestGenesisState_Validate_InvalidParamsBubbled(t *testing.T) {
	t.Parallel()
	// Invalid MinStake denom is guaranteed to fail Params.Validate.
	badParams := DefaultParams()
	badParams.MinStake = &v1beta1.Coin{Denom: "", Amount: "100"}
	gs := &GenesisState{Params: badParams}
	if err := gs.Validate(); err == nil {
		t.Fatal("expected Params.Validate error to propagate")
	}
}

// TestGenesisState_Validate_NilPassportEntry pins the
// per-passport nil guard. A nil slice entry can appear from a
// half-zeroed protobuf deserialization and would otherwise nil-deref
// at passport.PassportId.
func TestGenesisState_Validate_NilPassportEntry(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Params:    DefaultParams(),
		Passports: []*AgentPassport{nil},
	}
	if err := gs.Validate(); err == nil {
		t.Fatal("expected error for nil passport entry")
	}
}

// TestGenesisState_Validate_EmptyPassportID pins the empty-ID
// branch. An empty PassportId would lead to key collisions in the
// keeper's collections map on import.
func TestGenesisState_Validate_EmptyPassportID(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Params: DefaultParams(),
		Passports: []*AgentPassport{
			{PassportId: "", AgentPubkey: "pk1", OwnerAddress: validAddr},
		},
	}
	if err := gs.Validate(); err == nil {
		t.Fatal("expected error for empty PassportId")
	}
}

func TestGenesisState_Validate_BlankPassportID(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Params: DefaultParams(),
		Passports: []*AgentPassport{
			{PassportId: " \t\n ", AgentPubkey: "pk1", OwnerAddress: validAddr},
		},
	}
	if err := gs.Validate(); err == nil {
		t.Fatal("expected error for blank PassportId")
	}
}

func TestGenesisState_Validate_PaddedPassportID(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Params: DefaultParams(),
		Passports: []*AgentPassport{
			validGenesisPassport(" pp-1 ", "pk1"),
		},
	}
	if err := gs.Validate(); err == nil {
		t.Fatal("expected error for padded PassportId")
	}
}

func TestGenesisState_Validate_OversizedPassportID(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Params: DefaultParams(),
		Passports: []*AgentPassport{
			{PassportId: strings.Repeat("p", MaxPassportIDLen+1), AgentPubkey: "pk1", OwnerAddress: validAddr},
		},
	}
	if err := gs.Validate(); err == nil {
		t.Fatal("expected error for oversized PassportId")
	}
}

// TestGenesisState_Validate_DuplicatePassportID pins the
// duplicate-ID branch. Duplicate IDs silently survive genesis import
// under a refactor that drops the `seen` map (the second Set
// simply overwrites the first), leaving two distinct passport
// records collapsed into one — a state-integrity bug that's nearly
// impossible to recover from post-launch.
func TestGenesisState_Validate_DuplicatePassportID(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Params: DefaultParams(),
		Passports: []*AgentPassport{
			{PassportId: "pp-1", AgentPubkey: "pk1", OwnerAddress: validAddr},
			{PassportId: "pp-1", AgentPubkey: "pk2", OwnerAddress: validAddr},
		},
	}
	if err := gs.Validate(); err == nil {
		t.Fatal("expected error for duplicate PassportId")
	}
}

// TestGenesisState_Validate_EmptyAgentPubkey pins the empty-pubkey
// branch. Empty pubkey would allow multiple passports to register
// under the same (empty) cryptographic identity.
func TestGenesisState_Validate_EmptyAgentPubkey(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Params: DefaultParams(),
		Passports: []*AgentPassport{
			{PassportId: "pp-1", AgentPubkey: "", OwnerAddress: validAddr},
		},
	}
	if err := gs.Validate(); err == nil {
		t.Fatal("expected error for empty AgentPubkey")
	}
}

// TestGenesisState_Validate_BlankAgentPubkey pins the keeper import
// contract: SavePassport canonicalizes AgentPubkey with trim+lowercase,
// so genesis validation must reject values that canonicalize to empty
// before InitGenesis reaches the import path.
func TestGenesisState_Validate_BlankAgentPubkey(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Params: DefaultParams(),
		Passports: []*AgentPassport{
			{PassportId: "pp-1", AgentPubkey: " \t\n ", OwnerAddress: validAddr},
		},
	}
	if err := gs.Validate(); err == nil {
		t.Fatal("expected error for blank AgentPubkey")
	}
}

func TestGenesisState_Validate_OversizedAgentPubkey(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Params: DefaultParams(),
		Passports: []*AgentPassport{
			{
				PassportId:   "pp-1",
				AgentPubkey:  strings.Repeat("a", MaxAgentPubkeyLen+1),
				OwnerAddress: validAddr,
			},
		},
	}
	if err := gs.Validate(); err == nil {
		t.Fatal("expected error for oversized AgentPubkey")
	}
}

// TestGenesisState_Validate_DuplicateAgentPubkey pins the
// agent-pubkey uniqueness branch. Two passports with distinct IDs
// but the same cryptographic identity would let one signer act
// under two reputations — breaking the pubkey-is-identity
// invariant keeper.save_passport enforces.
func TestGenesisState_Validate_DuplicateAgentPubkey(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Params: DefaultParams(),
		Passports: []*AgentPassport{
			{PassportId: "pp-1", AgentPubkey: "pk-shared", OwnerAddress: validAddr},
			{PassportId: "pp-2", AgentPubkey: "pk-shared", OwnerAddress: validAddr},
		},
	}
	if err := gs.Validate(); err == nil {
		t.Fatal("expected error for duplicate AgentPubkey")
	}
}

// TestGenesisState_Validate_DuplicateCanonicalAgentPubkey pins
// parity with keeper canonicalization. Hex public keys are
// case-insensitive and keeper import lowercases/trim-spaces
// AgentPubkey before indexing, so these two rows collide in state
// and must be rejected during genesis validation.
func TestGenesisState_Validate_DuplicateCanonicalAgentPubkey(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Params: DefaultParams(),
		Passports: []*AgentPassport{
			{PassportId: "pp-1", AgentPubkey: " ED25519:ABCDEF ", OwnerAddress: validAddr},
			{PassportId: "pp-2", AgentPubkey: "ed25519:abcdef", OwnerAddress: validAddr},
		},
	}
	if err := gs.Validate(); err == nil {
		t.Fatal("expected error for duplicate canonical AgentPubkey")
	}
}

// TestGenesisState_Validate_EmptyOwnerAddress pins that every
// imported passport must have an owner. Empty owner would mean no
// account can slash or rotate the passport's stake.
func TestGenesisState_Validate_EmptyOwnerAddress(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Params: DefaultParams(),
		Passports: []*AgentPassport{
			{PassportId: "pp-1", AgentPubkey: "pk1", OwnerAddress: ""},
		},
	}
	if err := gs.Validate(); err == nil {
		t.Fatal("expected error for empty OwnerAddress")
	}
}

// TestGenesisState_Validate_InvalidOwnerAddress pins parity with
// MsgRegisterPassport and owner-signed operations. Importing a
// non-Bech32 owner strands the passport because no valid account
// can top up, unregister, or otherwise act as that owner later.
func TestGenesisState_Validate_InvalidOwnerAddress(t *testing.T) {
	t.Parallel()
	gs := &GenesisState{
		Params: DefaultParams(),
		Passports: []*AgentPassport{
			{PassportId: "pp-1", AgentPubkey: "pk1", OwnerAddress: "owner-1"},
		},
	}
	if err := gs.Validate(); err == nil {
		t.Fatal("expected error for invalid OwnerAddress")
	}
}

// TestGenesisState_Validate_InvalidSummaryBubbled pins that a
// per-passport Summary.Validate failure is surfaced by the outer
// genesis validator. Without this, malformed summaries (e.g.,
// ToolDiversityIndex NaN, or SettledSpend with empty denom) would
// silently import and produce downstream runtime errors far from
// the genesis load site.
func TestGenesisState_Validate_InvalidSummaryBubbled(t *testing.T) {
	t.Parallel()
	passport := validGenesisPassport("pp-1", "pk1")
	passport.Summary = &PassportSummary{
		TotalSpend: &v1beta1.Coin{Denom: "", Amount: "100"}, // empty denom
	}
	gs := &GenesisState{
		Params:    DefaultParams(),
		Passports: []*AgentPassport{passport},
	}
	if err := gs.Validate(); err == nil {
		t.Fatal("expected error from Summary.Validate to propagate")
	}
}

func TestGenesisState_Validate_InvalidPassportLifecycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*AgentPassport)
		want   string
	}{
		{
			name: "unspecified status",
			mutate: func(passport *AgentPassport) {
				passport.Status = PassportStatus_PASSPORT_STATUS_UNSPECIFIED
			},
			want: "invalid status",
		},
		{
			name: "unknown status",
			mutate: func(passport *AgentPassport) {
				passport.Status = PassportStatus(99)
			},
			want: "invalid status",
		},
		{
			name: "nil stake",
			mutate: func(passport *AgentPassport) {
				passport.Stake = nil
			},
			want: "stake must be a positive coin",
		},
		{
			name: "zero stake",
			mutate: func(passport *AgentPassport) {
				passport.Stake = &v1beta1.Coin{Denom: "ulume", Amount: "0"}
			},
			want: "stake must be a positive coin",
		},
		{
			name: "invalid stake denom",
			mutate: func(passport *AgentPassport) {
				passport.Stake = &v1beta1.Coin{Denom: "bad denom", Amount: "100"}
			},
			want: "stake must be a positive coin",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			passport := validGenesisPassport("pp-1", "pk1")
			tc.mutate(passport)
			err := (&GenesisState{
				Params:    DefaultParams(),
				Passports: []*AgentPassport{passport},
			}).Validate()
			if err == nil {
				t.Fatalf("expected error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

// TestGenesisState_Validate_InvalidPassportTimestamps pins that
// imported protobuf timestamps are checked before keeper import.
// Runtime constructors use timestamppb.New, so malformed timestamp
// structs should not be accepted from genesis.
func TestGenesisState_Validate_InvalidPassportTimestamps(t *testing.T) {
	t.Parallel()

	invalidTimestamp := func() *timestamppb.Timestamp {
		return &timestamppb.Timestamp{Nanos: 1_000_000_000}
	}
	genesisWithPassport := func(mutator func(*AgentPassport)) *GenesisState {
		passport := validGenesisPassport("pp-1", "pk1")
		mutator(passport)
		return &GenesisState{
			Params:    DefaultParams(),
			Passports: []*AgentPassport{passport},
		}
	}

	tests := []struct {
		name   string
		mutate func(*AgentPassport)
		want   string
	}{
		{
			name: "reputation updated_at",
			mutate: func(passport *AgentPassport) {
				passport.Reputation = &ReputationVector{UpdatedAt: invalidTimestamp()}
			},
			want: "reputation.updated_at",
		},
		{
			name: "score breakdown updated_at",
			mutate: func(passport *AgentPassport) {
				passport.ScoreBreakdown = &PassportScoreBreakdown{UpdatedAt: invalidTimestamp()}
			},
			want: "score_breakdown.updated_at",
		},
		{
			name: "tier entered_at",
			mutate: func(passport *AgentPassport) {
				passport.TierState = &PassportTierState{TierEnteredAt: invalidTimestamp()}
			},
			want: "tier_state.tier_entered_at",
		},
		{
			name: "promotion started_at",
			mutate: func(passport *AgentPassport) {
				passport.TierState = &PassportTierState{PromotionStartedAt: invalidTimestamp()}
			},
			want: "tier_state.promotion_started_at",
		},
		{
			name: "lockup expires_at",
			mutate: func(passport *AgentPassport) {
				passport.TierState = &PassportTierState{LockupExpiresAt: invalidTimestamp()}
			},
			want: "tier_state.lockup_expires_at",
		},
		{
			name: "tier history score updated_at",
			mutate: func(passport *AgentPassport) {
				passport.TierHistory = []*PassportTierHistoryEntry{
					{ScoreBreakdown: &PassportScoreBreakdown{UpdatedAt: invalidTimestamp()}},
				}
			},
			want: "tier_history[0].score_breakdown.updated_at",
		},
		{
			name: "tier history nil entry",
			mutate: func(passport *AgentPassport) {
				passport.TierHistory = []*PassportTierHistoryEntry{nil}
			},
			want: "tier_history[0] cannot be nil",
		},
		{
			name: "tier history transitioned_at",
			mutate: func(passport *AgentPassport) {
				passport.TierHistory = []*PassportTierHistoryEntry{
					{TransitionedAt: invalidTimestamp()},
				}
			},
			want: "tier_history[0].transitioned_at",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := genesisWithPassport(tc.mutate).Validate()
			if err == nil {
				t.Fatalf("expected invalid timestamp error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

// TestGenesisState_Validate_ValidMultiPassport pins the happy path
// with multiple distinct passports. Complements the per-branch
// negative tests by proving the validator doesn't reject a
// well-formed multi-passport genesis.
func TestGenesisState_Validate_ValidMultiPassport(t *testing.T) {
	t.Parallel()
	validSummary := &PassportSummary{
		ToolDiversityIndex: 0.5,
		VerifiedSpendShare: 0.9,
		CollusionRiskScore: 0.1,
	}
	gs := &GenesisState{
		Params: DefaultParams(),
		Passports: []*AgentPassport{
			func() *AgentPassport {
				passport := validGenesisPassport("pp-1", "pk-a")
				passport.Summary = validSummary
				return passport
			}(),
			validGenesisPassport("pp-2", "pk-b"),
			validGenesisPassport("pp-3", "pk-c"),
		},
	}
	if err := gs.Validate(); err != nil {
		t.Fatalf("valid multi-passport genesis should pass: %v", err)
	}
}
