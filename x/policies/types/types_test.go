//go:build cosmos

package types

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
)

// ---------------------------------------------------------------------------
// Keys & constants
// ---------------------------------------------------------------------------

func TestModuleConstants(t *testing.T) {
	t.Parallel()
	if ModuleName != "policies" {
		t.Errorf("ModuleName = %q, want %q", ModuleName, "policies")
	}
	if StoreKey != ModuleName {
		t.Errorf("StoreKey = %q, want %q", StoreKey, ModuleName)
	}
	if RouterKey != ModuleName {
		t.Errorf("RouterKey = %q, want %q", RouterKey, ModuleName)
	}
	if QuerierRoute != ModuleName {
		t.Errorf("QuerierRoute = %q, want %q", QuerierRoute, ModuleName)
	}
	if ModuleAccountName != ModuleName {
		t.Errorf("ModuleAccountName = %q, want %q", ModuleAccountName, ModuleName)
	}
}

func TestKeyPrefixBytesUnique(t *testing.T) {
	t.Parallel()
	bytes := []uint8{
		ParamsPrefixByte, PolicyPrefixByte, PolicyVersionPrefixByte,
		PolicyUpdatePrefixByte, PolicyUpdateCounterPrefixByte,
		PolicyAuditPrefixByte, PolicyByOwnerPrefixByte, PolicyByStatePrefixByte,
		PolicyAuditCounterPrefixByte,
	}
	seen := make(map[uint8]struct{})
	for _, b := range bytes {
		if _, ok := seen[b]; ok {
			t.Errorf("duplicate prefix byte 0x%02x", b)
		}
		seen[b] = struct{}{}
	}
}

func TestKeyPrefixSlicesMatchBytes(t *testing.T) {
	t.Parallel()
	pairs := []struct {
		name   string
		prefix []byte
		want   uint8
	}{
		{"ParamsPrefix", ParamsPrefix, ParamsPrefixByte},
		{"PolicyPrefix", PolicyPrefix, PolicyPrefixByte},
		{"PolicyVersionPrefix", PolicyVersionPrefix, PolicyVersionPrefixByte},
		{"PolicyUpdatePrefix", PolicyUpdatePrefix, PolicyUpdatePrefixByte},
		{"PolicyUpdateCounterPrefix", PolicyUpdateCounterPrefix, PolicyUpdateCounterPrefixByte},
		{"PolicyAuditPrefix", PolicyAuditPrefix, PolicyAuditPrefixByte},
		{"PolicyByOwnerPrefix", PolicyByOwnerPrefix, PolicyByOwnerPrefixByte},
		{"PolicyByStatePrefix", PolicyByStatePrefix, PolicyByStatePrefixByte},
		{"PolicyAuditCounterPrefix", PolicyAuditCounterPrefix, PolicyAuditCounterPrefixByte},
	}
	for _, tc := range pairs {
		if len(tc.prefix) != 1 {
			t.Errorf("%s: len=%d, want 1", tc.name, len(tc.prefix))
			continue
		}
		if !bytes.Equal([]byte{tc.prefix[0]}, []byte{tc.want}) {
			t.Errorf("%s: byte=0x%02x, want 0x%02x", tc.name, tc.prefix[0], tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

func TestSentinelErrors(t *testing.T) {
	t.Parallel()
	errs := []error{
		ErrPolicyNotFound, ErrPolicyAlreadyExists, ErrInvalidPolicyState,
		ErrUnauthorized, ErrInvalidPolicyID, ErrInvalidPolicyVersion,
		ErrPolicyNotActive, ErrPolicyDeprecated, ErrBudgetExceeded,
		ErrToolNotAllowed, ErrJurisdictionViolation, ErrPrivacyViolation,
		ErrInvalidSignature, ErrInheritanceCycle, ErrMaxInheritanceDepth,
		ErrInvalidBudgetConfig, ErrRateLimitExceeded, ErrEgressDenied,
		ErrApprovalRequired, ErrInvalidParams,
	}
	for _, e := range errs {
		if e == nil {
			t.Error("sentinel error should not be nil")
		}
	}

	type coder interface{ ABCICode() uint32 }
	codes := make(map[uint32]string)
	for _, e := range errs {
		c, ok := e.(coder)
		if !ok {
			continue
		}
		code := c.ABCICode()
		if prev, dup := codes[code]; dup {
			t.Errorf("duplicate error code %d: %q and %q", code, prev, e.Error())
		}
		codes[code] = e.Error()
	}
}

// ---------------------------------------------------------------------------
// Codec (smoke test)
// ---------------------------------------------------------------------------

func TestRegisterInterfaces_NoPanic(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RegisterInterfaces panicked: %v", r)
		}
	}()
	RegisterInterfaces(ModuleCdc.InterfaceRegistry())
}

func TestModuleCdc_NotNil(t *testing.T) {
	t.Parallel()
	if ModuleCdc == nil {
		t.Error("ModuleCdc should not be nil")
	}
	if Amino == nil {
		t.Error("Amino should not be nil")
	}
}

func TestRegisterLegacyAminoCodec_NoPanic(t *testing.T) {
	t.Parallel()
	amino := codec.NewLegacyAmino()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RegisterLegacyAminoCodec panicked: %v", r)
		}
	}()
	RegisterLegacyAminoCodec(amino)
}

// ---------------------------------------------------------------------------
// MsgCreatePolicy.ValidateBasic
// ---------------------------------------------------------------------------

func TestMsgCreatePolicy_ValidateBasic_Valid(t *testing.T) {
	t.Parallel()
	msg := &MsgCreatePolicy{
		Creator: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Policy: &PolicyProfile{
			PolicyId: "test",
			Version:  "1.0.0",
			Metadata: &PolicyMetadata{Name: "Test"},
			Lifecycle: &PolicyLifecycle{
				State: PolicyState_POLICY_STATE_DRAFT,
			},
		},
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMsgCreatePolicy_ValidateBasic_EmptyCreator(t *testing.T) {
	t.Parallel()
	msg := &MsgCreatePolicy{
		Creator: "",
		Policy:  &PolicyProfile{PolicyId: "test"},
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty creator")
	}
}

func TestMsgCreatePolicy_ValidateBasic_WhitespaceCreator(t *testing.T) {
	t.Parallel()
	msg := &MsgCreatePolicy{
		Creator: "   ",
		Policy:  &PolicyProfile{PolicyId: "test"},
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for whitespace-only creator")
	}
}

func TestMsgCreatePolicy_ValidateBasic_InvalidCreatorAddress(t *testing.T) {
	t.Parallel()
	msg := &MsgCreatePolicy{
		Creator: "not-a-bech32-address",
		Policy:  validBasePolicy(),
	}
	if err := msg.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "creator address") {
		t.Errorf("expected creator address error, got %v", err)
	}
}

func TestMsgCreatePolicy_ValidateBasic_NilPolicy(t *testing.T) {
	t.Parallel()
	msg := &MsgCreatePolicy{
		Creator: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Policy:  nil,
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for nil policy")
	}
}

// ---------------------------------------------------------------------------
// MsgUpdatePolicy.ValidateBasic
// ---------------------------------------------------------------------------

func TestMsgUpdatePolicy_ValidateBasic_Valid(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdatePolicy{
		Updater:  "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		PolicyId: "policy-1",
		Policy: &PolicyProfile{
			PolicyId: "policy-1",
			Version:  "2.0.0",
			Metadata: &PolicyMetadata{Name: "Updated"},
			Lifecycle: &PolicyLifecycle{
				State: PolicyState_POLICY_STATE_DRAFT,
			},
		},
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMsgUpdatePolicy_ValidateBasic_EmptyUpdater(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdatePolicy{
		Updater:  "",
		PolicyId: "policy-1",
		Policy:   &PolicyProfile{PolicyId: "policy-1"},
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty updater")
	}
}

func TestMsgUpdatePolicy_ValidateBasic_InvalidUpdaterAddress(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdatePolicy{
		Updater:  "not-a-bech32-address",
		PolicyId: "policy-1",
		Policy:   validBasePolicy(),
	}
	if err := msg.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "updater address") {
		t.Errorf("expected updater address error, got %v", err)
	}
}

func TestMsgUpdatePolicy_ValidateBasic_EmptyPolicyID(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdatePolicy{
		Updater:  "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		PolicyId: "",
		Policy:   &PolicyProfile{PolicyId: "policy-1"},
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty policy_id")
	}
}

func TestMsgUpdatePolicy_ValidateBasic_RejectsPaddedPolicyID(t *testing.T) {
	t.Parallel()
	p := validBasePolicy()
	p.PolicyId = "policy-1"
	msg := &MsgUpdatePolicy{
		Updater:  "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		PolicyId: " policy-1",
		Policy:   p,
	}
	if err := msg.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "policy_id") {
		t.Errorf("expected non-canonical policy_id error, got %v", err)
	}
}

func TestMsgUpdatePolicy_ValidateBasic_NilPolicy(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdatePolicy{
		Updater:  "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		PolicyId: "policy-1",
		Policy:   nil,
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for nil policy")
	}
}

func TestMsgUpdatePolicy_ValidateBasic_RejectsInvalidUpdateReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		reason string
	}{
		{
			name:   "leading space",
			reason: " policy update",
		},
		{
			name:   "trailing space",
			reason: "policy update ",
		},
		{
			name:   "oversized",
			reason: strings.Repeat("a", MaxPolicyUpdateReasonLen+1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			policy := validBasePolicy()
			policy.PolicyId = "policy-1"
			msg := &MsgUpdatePolicy{
				Updater:      "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
				PolicyId:     "policy-1",
				Policy:       policy,
				UpdateReason: tt.reason,
			}

			if err := msg.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "update_reason") {
				t.Errorf("expected update_reason error, got %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Regression coverage for lumera_ai-e427q: ValidateBasic must catch
// content-level policy errors that previously slipped through.
// ---------------------------------------------------------------------------

func validBasePolicy() *PolicyProfile {
	return &PolicyProfile{
		PolicyId:  "p1",
		Version:   "1.0.0",
		Metadata:  &PolicyMetadata{Name: "P1"},
		Lifecycle: &PolicyLifecycle{State: PolicyState_POLICY_STATE_DRAFT},
	}
}

func TestMsgCreatePolicy_ValidateBasic_RejectsToolAllowDenyOverlap(t *testing.T) {
	t.Parallel()
	p := validBasePolicy()
	p.ToolFilters = &ToolFilters{
		AllowedTools: []string{"a", "b"},
		DeniedTools:  []string{"b", "c"},
	}
	msg := &MsgCreatePolicy{Creator: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu", Policy: p}
	if err := msg.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "denied_tools") {
		t.Errorf("expected allowed/denied tool overlap error, got %v", err)
	}
}

func TestMsgCreatePolicy_ValidateBasic_RejectsCategoryAllowDenyOverlap(t *testing.T) {
	t.Parallel()
	p := validBasePolicy()
	p.ToolFilters = &ToolFilters{
		AllowedCategories: []string{"x"},
		DeniedCategories:  []string{"x"},
	}
	msg := &MsgCreatePolicy{Creator: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu", Policy: p}
	if err := msg.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "denied_categories") {
		t.Errorf("expected category overlap error, got %v", err)
	}
}

func TestMsgCreatePolicy_ValidateBasic_RejectsCapabilityOverlap(t *testing.T) {
	t.Parallel()
	p := validBasePolicy()
	p.ToolFilters = &ToolFilters{
		RequiredCapabilities:  []string{"http", "fs"},
		ForbiddenCapabilities: []string{"fs"},
	}
	msg := &MsgCreatePolicy{Creator: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu", Policy: p}
	if err := msg.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "forbidden_capabilities") {
		t.Errorf("expected capability overlap error, got %v", err)
	}
}

func TestMsgCreatePolicy_ValidateBasic_RejectsBPSOverflow(t *testing.T) {
	t.Parallel()
	p := validBasePolicy()
	p.ToolFilters = &ToolFilters{MaxDisputeRateBps: MaxPolicyBPS + 1}
	msg := &MsgCreatePolicy{Creator: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu", Policy: p}
	if err := msg.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "max_dispute_rate_bps") {
		t.Errorf("expected dispute_rate BPS overflow error, got %v", err)
	}

	p2 := validBasePolicy()
	p2.ToolFilters = &ToolFilters{MinUptimeBps: MaxPolicyBPS + 1}
	msg2 := &MsgCreatePolicy{Creator: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu", Policy: p2}
	if err := msg2.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "min_uptime_bps") {
		t.Errorf("expected uptime BPS overflow error, got %v", err)
	}
}

func TestMsgCreatePolicy_ValidateBasic_RejectsMissingMetadataName(t *testing.T) {
	t.Parallel()
	p := validBasePolicy()
	p.Metadata = &PolicyMetadata{Name: ""}
	msg := &MsgCreatePolicy{Creator: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu", Policy: p}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty metadata.name")
	}
}

func TestMsgCreatePolicy_ValidateBasic_RejectsInvalidLifecycleState(t *testing.T) {
	t.Parallel()
	p := validBasePolicy()
	p.Lifecycle.State = PolicyState(99)
	msg := &MsgCreatePolicy{Creator: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu", Policy: p}
	if err := msg.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "lifecycle.state") {
		t.Errorf("expected lifecycle state error, got %v", err)
	}
}

func TestMsgUpdatePolicy_ValidateBasic_RejectsMismatchedPolicyID(t *testing.T) {
	t.Parallel()
	p := validBasePolicy()
	p.PolicyId = "other"
	msg := &MsgUpdatePolicy{
		Updater:  "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		PolicyId: "policy-1",
		Policy:   p,
	}
	if err := msg.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("expected mismatched policy_id error, got %v", err)
	}
}

func TestMsgUpdatePolicy_ValidateBasic_RedactsMismatchedPolicyIDDiagnostics(t *testing.T) {
	t.Parallel()
	p := validBasePolicy()
	p.PolicyId = "policy.inner?client_secret=embedded-policy-value-64850"
	msg := &MsgUpdatePolicy{
		Updater:  "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		PolicyId: "policy.outer?api_key=outer-policy-value-64850",
		Policy:   p,
	}

	err := msg.ValidateBasic()
	if err == nil {
		t.Fatal("expected mismatched policy_id error")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected mismatched policy_id error, got %v", err)
	}
	for _, rawValue := range []string{"embedded-policy-value-64850", "outer-policy-value-64850"} {
		if strings.Contains(err.Error(), rawValue) {
			t.Fatalf("policy_id mismatch diagnostic leaked raw value %q: %v", rawValue, err)
		}
	}
	for _, redacted := range []string{"client_secret=[REDACTED]", "api_key=[REDACTED]"} {
		if !strings.Contains(err.Error(), redacted) {
			t.Fatalf("policy_id mismatch diagnostic missing redaction marker %q: %v", redacted, err)
		}
	}
}

// MsgUpdatePolicy.ValidateBasic must require the embedded policy to carry its
// own PolicyId so the persisted record is self-describing rather than relying
// on out-of-band msg metadata that is not stored alongside it.
func TestMsgUpdatePolicy_ValidateBasic_RequiresEmbeddedPolicyID(t *testing.T) {
	t.Parallel()
	p := validBasePolicy()
	p.PolicyId = ""
	msg := &MsgUpdatePolicy{
		Updater:  "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		PolicyId: "policy-1",
		Policy:   p,
	}
	if err := msg.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "policy_id") {
		t.Errorf("expected error requiring embedded policy_id, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// MsgActivatePolicy.ValidateBasic
// ---------------------------------------------------------------------------

func TestMsgActivatePolicy_ValidateBasic_Valid(t *testing.T) {
	t.Parallel()
	msg := &MsgActivatePolicy{
		Authority: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		PolicyId:  "policy-1",
		Version:   "1.0.0",
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMsgActivatePolicy_ValidateBasic_EmptyAuthority(t *testing.T) {
	t.Parallel()
	msg := &MsgActivatePolicy{
		Authority: "",
		PolicyId:  "policy-1",
		Version:   "1.0.0",
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty authority")
	}
}

func TestPolicyAuthorityMessages_ValidateBasic_InvalidAuthorityAddress(t *testing.T) {
	t.Parallel()
	tests := map[string]interface {
		ValidateBasic() error
	}{
		"activate": &MsgActivatePolicy{
			Authority: "not-a-bech32-address",
			PolicyId:  "policy-1",
			Version:   "1.0.0",
		},
		"deprecate": &MsgDeprecatePolicy{
			Authority: "not-a-bech32-address",
			PolicyId:  "policy-1",
			Version:   "1.0.0",
		},
		"archive": &MsgArchivePolicy{
			Authority: "not-a-bech32-address",
			PolicyId:  "policy-1",
			Version:   "1.0.0",
		},
		"update_params": &MsgUpdateParams{
			Authority: "not-a-bech32-address",
			Params:    DefaultParams(),
		},
	}

	for name, msg := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := msg.ValidateBasic()
			if err == nil || !strings.Contains(err.Error(), "authority address") {
				t.Errorf("expected authority address error, got %v", err)
			}
		})
	}
}

func TestMsgActivatePolicy_ValidateBasic_EmptyPolicyID(t *testing.T) {
	t.Parallel()
	msg := &MsgActivatePolicy{
		Authority: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		PolicyId:  "",
		Version:   "1.0.0",
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty policy_id")
	}
}

func TestMsgActivatePolicy_ValidateBasic_EmptyVersion(t *testing.T) {
	t.Parallel()
	msg := &MsgActivatePolicy{
		Authority: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		PolicyId:  "policy-1",
		Version:   "",
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty version")
	}
}

func TestPolicyLifecycleMessages_ValidateBasic_RejectPaddedPolicyReferences(t *testing.T) {
	t.Parallel()
	const authority = "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu"
	tests := map[string]struct {
		msg  interface{ ValidateBasic() error }
		want string
	}{
		"activate_policy_id": {
			msg:  &MsgActivatePolicy{Authority: authority, PolicyId: " policy-1", Version: "1.0.0"},
			want: "policy_id",
		},
		"activate_version": {
			msg:  &MsgActivatePolicy{Authority: authority, PolicyId: "policy-1", Version: "1.0.0 "},
			want: "version",
		},
		"deprecate_policy_id": {
			msg:  &MsgDeprecatePolicy{Authority: authority, PolicyId: "policy-1\t", Version: "1.0.0"},
			want: "policy_id",
		},
		"deprecate_version": {
			msg:  &MsgDeprecatePolicy{Authority: authority, PolicyId: "policy-1", Version: "\t1.0.0"},
			want: "version",
		},
		"deprecate_successor_policy_id": {
			msg:  &MsgDeprecatePolicy{Authority: authority, PolicyId: "policy-1", Version: "1.0.0", SuccessorPolicyId: " policy-2"},
			want: "successor_policy_id",
		},
		"archive_policy_id": {
			msg:  &MsgArchivePolicy{Authority: authority, PolicyId: "\npolicy-1", Version: "1.0.0"},
			want: "policy_id",
		},
		"archive_version": {
			msg:  &MsgArchivePolicy{Authority: authority, PolicyId: "policy-1", Version: " 1.0.0"},
			want: "version",
		},
	}

	for name, tc := range tests {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := tc.msg.ValidateBasic()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s error, got %v", tc.want, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MsgDeprecatePolicy.ValidateBasic
// ---------------------------------------------------------------------------

func TestMsgDeprecatePolicy_ValidateBasic_Valid(t *testing.T) {
	t.Parallel()
	msg := &MsgDeprecatePolicy{
		Authority:         "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		PolicyId:          "policy-1",
		Version:           "1.0.0",
		SuccessorPolicyId: "policy-2",
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMsgDeprecatePolicy_ValidateBasic_EmptyAuthority(t *testing.T) {
	t.Parallel()
	msg := &MsgDeprecatePolicy{
		Authority: "",
		PolicyId:  "policy-1",
		Version:   "1.0.0",
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty authority")
	}
}

func TestMsgDeprecatePolicy_ValidateBasic_EmptyPolicyID(t *testing.T) {
	t.Parallel()
	msg := &MsgDeprecatePolicy{
		Authority: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		PolicyId:  "",
		Version:   "1.0.0",
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty policy_id")
	}
}

func TestMsgDeprecatePolicy_ValidateBasic_EmptyVersion(t *testing.T) {
	t.Parallel()
	msg := &MsgDeprecatePolicy{
		Authority: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		PolicyId:  "policy-1",
		Version:   "",
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty version")
	}
}

// ---------------------------------------------------------------------------
// MsgArchivePolicy.ValidateBasic
// ---------------------------------------------------------------------------

func TestMsgArchivePolicy_ValidateBasic_Valid(t *testing.T) {
	t.Parallel()
	msg := &MsgArchivePolicy{
		Authority: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		PolicyId:  "policy-1",
		Version:   "1.0.0",
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMsgArchivePolicy_ValidateBasic_EmptyAuthority(t *testing.T) {
	t.Parallel()
	msg := &MsgArchivePolicy{
		Authority: "",
		PolicyId:  "policy-1",
		Version:   "1.0.0",
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty authority")
	}
}

func TestMsgArchivePolicy_ValidateBasic_EmptyPolicyID(t *testing.T) {
	t.Parallel()
	msg := &MsgArchivePolicy{
		Authority: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		PolicyId:  "",
		Version:   "1.0.0",
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty policy_id")
	}
}

func TestMsgArchivePolicy_ValidateBasic_EmptyVersion(t *testing.T) {
	t.Parallel()
	msg := &MsgArchivePolicy{
		Authority: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		PolicyId:  "policy-1",
		Version:   "",
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty version")
	}
}

// ---------------------------------------------------------------------------
// MsgUpdateParams.ValidateBasic
// ---------------------------------------------------------------------------

func TestMsgUpdateParams_ValidateBasic_Valid(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdateParams{
		Authority: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Params:    DefaultParams(),
	}
	if err := msg.ValidateBasic(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMsgUpdateParams_ValidateBasic_EmptyAuthority(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdateParams{
		Authority: "",
		Params:    DefaultParams(),
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for empty authority")
	}
}

func TestMsgUpdateParams_ValidateBasic_NilParams(t *testing.T) {
	t.Parallel()
	msg := &MsgUpdateParams{
		Authority: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
		Params:    nil,
	}
	if err := msg.ValidateBasic(); err == nil {
		t.Error("expected error for nil params")
	}
}

func TestMsgUpdateParams_ValidateBasic_InvalidParams(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		params *Params
	}{
		{
			name: "invalid inheritance depth",
			params: &Params{
				MinPolicyDeposit:              "1000000",
				MaxPolicyVersionHistory:       100,
				DefaultMigrationWindowSeconds: 604800,
				MaxInheritanceDepth:           0,
				DefaultAuditRetentionDays:     365,
			},
		},
		{
			name: "padded min policy deposit",
			params: &Params{
				MinPolicyDeposit:              " 1000000",
				MaxPolicyVersionHistory:       100,
				DefaultMigrationWindowSeconds: 604800,
				MaxInheritanceDepth:           5,
				DefaultAuditRetentionDays:     365,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			msg := &MsgUpdateParams{
				Authority: "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
				Params:    tt.params,
			}

			if err := msg.ValidateBasic(); err == nil {
				t.Fatal("expected error for invalid params")
			}
		})
	}
}

// TestFirstOverlap_OrderInAPreservedMetamorphic asserts the
// documented ordering contract: firstOverlap returns the first
// a-element that also appears in b, scanning a in its input order.
// Error messages emitted by ValidatePolicy interpolate this value;
// a refactor that switched to "first b-element in a" or "any match"
// would produce non-deterministic policy-validation error text that
// breaks downstream test assertions.
func TestFirstOverlap_OrderInAPreservedMetamorphic(t *testing.T) {
	// "apple" is the first a-element that also appears in b (b has
	// both "banana" and "apple", but a-order controls the result).
	a := []string{"mango", "apple", "banana"}
	b := []string{"banana", "apple"}
	if got := firstOverlap(a, b); got != "apple" {
		t.Errorf("a-order first-match failed: got %q want %q", got, "apple")
	}

	// Swap a-order: "banana" should now win because it's the first a
	// element to match. This is the metamorphic half — changing a-
	// order changes the answer.
	a2 := []string{"banana", "apple"}
	if got := firstOverlap(a2, b); got != "banana" {
		t.Errorf("swapped a-order did not flip winner: got %q want %q", got, "banana")
	}

	// a-order stable across two identical calls.
	if got := firstOverlap(a, b); got != "apple" {
		t.Errorf("non-deterministic result on repeat call: got %q", got)
	}
}

// TestFirstOverlap_EmptyAndDisjointCases pins the empty/disjoint
// contract: any empty input short-circuits to "", and disjoint
// non-empty sets also produce "". The function uses a linear scan
// because filter lists are small; a regression introducing a
// map-first optimisation could subtly change behavior for these
// corner cases.
func TestFirstOverlap_EmptyAndDisjointCases(t *testing.T) {
	if got := firstOverlap(nil, nil); got != "" {
		t.Errorf("nil,nil: got %q, want empty", got)
	}
	if got := firstOverlap(nil, []string{"x"}); got != "" {
		t.Errorf("nil,{x}: got %q, want empty", got)
	}
	if got := firstOverlap([]string{"x"}, nil); got != "" {
		t.Errorf("{x},nil: got %q, want empty", got)
	}
	if got := firstOverlap([]string{"a", "b"}, []string{"c", "d"}); got != "" {
		t.Errorf("disjoint: got %q, want empty", got)
	}
}

// TestFirstOverlap_CaseSensitivity pins case-sensitive matching:
// "ABC" and "abc" are distinct. Policy filter lists are case-sensitive
// by convention (a deny on "tool.UPPER" should not accidentally deny
// "tool.upper"); a future ToLower normalization would change this
// semantics.
func TestFirstOverlap_CaseSensitivity(t *testing.T) {
	if got := firstOverlap([]string{"ABC"}, []string{"abc"}); got != "" {
		t.Errorf("case-sensitivity broken: got %q, want empty", got)
	}
	if got := firstOverlap([]string{"abc"}, []string{"abc"}); got != "abc" {
		t.Errorf("exact-case match failed: got %q", got)
	}
}

// TestKeyPrefixBytes_ExactValues pins the exact byte value of
// each prefix constant. State-migration-critical: a silent
// renumbering would pass any existing uniqueness/slice-match
// tests while corrupting cross-version reads.
func TestKeyPrefixBytes_ExactValues(t *testing.T) {
	cases := []struct {
		name string
		got  uint8
		want uint8
	}{
		{"ParamsPrefixByte", ParamsPrefixByte, 0x01},
		{"PolicyPrefixByte", PolicyPrefixByte, 0x02},
		{"PolicyVersionPrefixByte", PolicyVersionPrefixByte, 0x03},
		{"PolicyUpdatePrefixByte", PolicyUpdatePrefixByte, 0x04},
		{"PolicyUpdateCounterPrefixByte", PolicyUpdateCounterPrefixByte, 0x05},
		{"PolicyAuditPrefixByte", PolicyAuditPrefixByte, 0x06},
		{"PolicyByOwnerPrefixByte", PolicyByOwnerPrefixByte, 0x07},
		{"PolicyByStatePrefixByte", PolicyByStatePrefixByte, 0x08},
		{"PolicyAuditCounterPrefixByte", PolicyAuditCounterPrefixByte, 0x09},
	}
	for _, c := range cases {
		if !bytes.Equal([]byte{c.got}, []byte{c.want}) {
			t.Errorf("%s = 0x%02x; want 0x%02x — any change requires a state migration",
				c.name, c.got, c.want)
		}
	}
}

// TestModuleIdentity_StableStrings pins the policies module
// identity strings. A rename is a chain-fork event.
func TestModuleIdentity_StableStrings(t *testing.T) {
	if ModuleName != "policies" {
		t.Errorf("ModuleName = %q; want 'policies'", ModuleName)
	}
	if StoreKey != "policies" {
		t.Errorf("StoreKey = %q; want 'policies'", StoreKey)
	}
	if RouterKey != "policies" {
		t.Errorf("RouterKey = %q; want 'policies'", RouterKey)
	}
	if QuerierRoute != "policies" {
		t.Errorf("QuerierRoute = %q; want 'policies'", QuerierRoute)
	}
	if ModuleAccountName != "policies" {
		t.Errorf("ModuleAccountName = %q; want 'policies'", ModuleAccountName)
	}
}
