//go:build cosmos

package types

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// tsPtr returns a pointer to t, matching the gogoproto *time.Time fields.
func tsPtr(t time.Time) *time.Time { return &t }

func TestDefaultGenesis(t *testing.T) {
	gs := DefaultGenesis()
	if gs == nil {
		t.Fatal("DefaultGenesis returned nil")
	}
	if gs.Params == nil {
		t.Fatal("Params is nil")
	}
	if gs.Policies == nil {
		t.Fatal("Policies is nil")
	}
	if len(gs.Policies) != 0 {
		t.Errorf("expected 0 policies, got %d", len(gs.Policies))
	}
	if gs.PolicyUpdateCounter != 0 {
		t.Errorf("PolicyUpdateCounter = %d, want 0", gs.PolicyUpdateCounter)
	}
}

func TestDefaultParams(t *testing.T) {
	p := DefaultParams()
	if p == nil {
		t.Fatal("DefaultParams returned nil")
	}
	if p.MinPolicyDeposit != "1000000" {
		t.Errorf("MinPolicyDeposit = %q, want %q", p.MinPolicyDeposit, "1000000")
	}
	if p.MaxPolicyVersionHistory != 100 {
		t.Errorf("MaxPolicyVersionHistory = %d, want 100", p.MaxPolicyVersionHistory)
	}
	if p.DefaultMigrationWindowSeconds != 604800 {
		t.Errorf("DefaultMigrationWindowSeconds = %d, want 604800", p.DefaultMigrationWindowSeconds)
	}
	if p.MaxInheritanceDepth != 5 {
		t.Errorf("MaxInheritanceDepth = %d, want 5", p.MaxInheritanceDepth)
	}
	if p.DefaultAuditRetentionDays != 365 {
		t.Errorf("DefaultAuditRetentionDays = %d, want 365", p.DefaultAuditRetentionDays)
	}
}

func TestParamsValidate(t *testing.T) {
	tests := []struct {
		name    string
		params  *Params
		wantErr bool
	}{
		{
			name:    "default params are valid",
			params:  DefaultParams(),
			wantErr: false,
		},
		{
			name: "zero max_policy_version_history",
			params: &Params{
				MaxPolicyVersionHistory: 0,
				MaxInheritanceDepth:     5,
			},
			wantErr: true,
		},
		{
			name: "zero max_inheritance_depth",
			params: &Params{
				MaxPolicyVersionHistory: 100,
				MaxInheritanceDepth:     0,
			},
			wantErr: true,
		},
		{
			name: "max_inheritance_depth too large",
			params: &Params{
				MaxPolicyVersionHistory: 100,
				MaxInheritanceDepth:     11,
			},
			wantErr: true,
		},
		{
			name: "max_inheritance_depth at boundary",
			params: &Params{
				MinPolicyDeposit:              "1",
				MaxPolicyVersionHistory:       100,
				DefaultMigrationWindowSeconds: 1,
				MaxInheritanceDepth:           10,
				DefaultAuditRetentionDays:     1,
			},
			wantErr: false,
		},
		{
			name: "minimal valid params",
			params: &Params{
				MinPolicyDeposit:              "1",
				MaxPolicyVersionHistory:       1,
				DefaultMigrationWindowSeconds: 1,
				MaxInheritanceDepth:           1,
				DefaultAuditRetentionDays:     1,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParamsValidateRejectsInvalidDepositAndRetention(t *testing.T) {
	tests := []struct {
		name   string
		params *Params
	}{
		{
			name: "invalid deposit",
			params: &Params{
				MinPolicyDeposit:              "abc",
				MaxPolicyVersionHistory:       100,
				DefaultMigrationWindowSeconds: 60,
				MaxInheritanceDepth:           5,
				DefaultAuditRetentionDays:     30,
			},
		},
		{
			name: "zero migration window",
			params: &Params{
				MinPolicyDeposit:              "1",
				MaxPolicyVersionHistory:       100,
				DefaultMigrationWindowSeconds: 0,
				MaxInheritanceDepth:           5,
				DefaultAuditRetentionDays:     30,
			},
		},
		{
			name: "zero audit retention",
			params: &Params{
				MinPolicyDeposit:              "1",
				MaxPolicyVersionHistory:       100,
				DefaultMigrationWindowSeconds: 60,
				MaxInheritanceDepth:           5,
				DefaultAuditRetentionDays:     0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, tt.params.Validate())
		})
	}
}

func TestParamsValidateRejectsNonCanonicalMinPolicyDeposit(t *testing.T) {
	tests := map[string]string{
		"leading space": " 1000000",
		"trailing tab":  "1000000\t",
		"leading tab":   "\t1000000",
	}

	for name, deposit := range tests {
		t.Run(name, func(t *testing.T) {
			params := DefaultParams()
			params.MinPolicyDeposit = deposit

			err := params.Validate()
			require.Error(t, err)
			require.ErrorContains(t, err, "min_policy_deposit")
		})
	}
}

func TestValidateBudgetControlsRejectsUnsupportedAndInvalidFields(t *testing.T) {
	tests := []struct {
		name    string
		budgets *BudgetControls
	}{
		{
			name: "unsupported burst allowance",
			budgets: &BudgetControls{
				BurstAllowance: &BudgetLimit{SoftLimit: "10", HardLimit: "20"},
			},
		},
		{
			name: "unsupported reserve allocation",
			budgets: &BudgetControls{
				ReserveAllocation: &ReserveConfig{Tier: "gold", CommitmentPeriodSeconds: 60, PrepaidAmount: "10"},
			},
		},
		{
			name: "approval threshold requires approvers",
			budgets: &BudgetControls{
				PerHour: &BudgetLimit{SoftLimit: "10", HardLimit: "20", ApprovalRequiredAbove: "15"},
			},
		},
		{
			name: "approval threshold must parse",
			budgets: &BudgetControls{
				PerWeek: &BudgetLimit{SoftLimit: "10", HardLimit: "20", ApprovalRequiredAbove: "fifteen", Approvers: []string{"lumera1approver"}},
			},
		},
		{
			name: "hard action must be supported",
			budgets: &BudgetControls{
				PerMonth: &BudgetLimit{SoftLimit: "10", HardLimit: "20", ActionOnHard: BudgetActionType_BUDGET_ACTION_TYPE_APPROVE},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, ValidateBudgetControls(tt.budgets))
		})
	}
}

func TestValidateBudgetLimitRejectsNonCanonicalUintStrings(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*BudgetLimit)
		wantField string
	}{
		{
			name: "soft_limit leading space",
			mutate: func(limit *BudgetLimit) {
				limit.SoftLimit = " 10"
			},
			wantField: "soft_limit",
		},
		{
			name: "hard_limit trailing space",
			mutate: func(limit *BudgetLimit) {
				limit.HardLimit = "20 "
			},
			wantField: "hard_limit",
		},
		{
			name: "approval_required_above leading tab",
			mutate: func(limit *BudgetLimit) {
				limit.ApprovalRequiredAbove = "\t15"
				limit.Approvers = []string{"lumera1approver"}
			},
			wantField: "approval_required_above",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit := &BudgetLimit{
				SoftLimit: "10",
				HardLimit: "20",
			}
			tt.mutate(limit)

			err := ValidateBudgetLimit(limit, "per_call")
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantField)
		})
	}
}

func TestGenesisStateValidate(t *testing.T) {
	validPolicy := &PolicyProfile{
		PolicyId: "test-policy",
		Version:  "1.0.0",
		Metadata: &PolicyMetadata{Name: "Test Policy"},
		Lifecycle: &PolicyLifecycle{
			State: PolicyState_POLICY_STATE_DRAFT,
		},
	}
	validPolicyV2 := &PolicyProfile{
		PolicyId: "test-policy",
		Version:  "2.0.0",
		Metadata: &PolicyMetadata{Name: "Test Policy v2"},
		Lifecycle: &PolicyLifecycle{
			State: PolicyState_POLICY_STATE_ACTIVE,
		},
	}

	tests := []struct {
		name    string
		gs      *GenesisState
		wantErr bool
	}{
		{
			name:    "nil genesis state",
			gs:      nil,
			wantErr: true,
		},
		{
			name:    "nil params",
			gs:      &GenesisState{},
			wantErr: true,
		},
		{
			name:    "default genesis is valid",
			gs:      DefaultGenesis(),
			wantErr: false,
		},
		{
			name: "valid with one policy",
			gs: &GenesisState{
				Params:   DefaultParams(),
				Policies: []*PolicyProfile{validPolicy},
			},
			wantErr: false,
		},
		{
			name: "nil policy in list",
			gs: &GenesisState{
				Params:   DefaultParams(),
				Policies: []*PolicyProfile{nil},
			},
			wantErr: true,
		},
		{
			name: "duplicate policy",
			gs: &GenesisState{
				Params:   DefaultParams(),
				Policies: []*PolicyProfile{validPolicy, validPolicy},
			},
			wantErr: true,
		},
		{
			name: "invalid params",
			gs: &GenesisState{
				Params: &Params{
					MaxPolicyVersionHistory: 0,
					MaxInheritanceDepth:     5,
				},
				Policies: []*PolicyProfile{},
			},
			wantErr: true,
		},
		{
			name: "invalid policy in list",
			gs: &GenesisState{
				Params: DefaultParams(),
				Policies: []*PolicyProfile{
					{PolicyId: "", Version: "1.0.0"}, // missing policy_id
				},
			},
			wantErr: true,
		},
		{
			name: "valid policy update history",
			gs: &GenesisState{
				Params:   DefaultParams(),
				Policies: []*PolicyProfile{validPolicy, validPolicyV2},
				PolicyUpdates: []*PolicyUpdate{
					{
						PolicyId:        "test-policy",
						Version:         "2.0.0",
						PreviousVersion: "1.0.0",
						Updater:         "lumera1owner",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "nil policy update",
			gs: &GenesisState{
				Params:        DefaultParams(),
				Policies:      []*PolicyProfile{validPolicy},
				PolicyUpdates: []*PolicyUpdate{nil},
			},
			wantErr: true,
		},
		{
			name: "policy update missing policy id",
			gs: &GenesisState{
				Params:   DefaultParams(),
				Policies: []*PolicyProfile{validPolicy},
				PolicyUpdates: []*PolicyUpdate{
					{Version: "1.0.0", PreviousVersion: "0.9.0", Updater: "lumera1owner"},
				},
			},
			wantErr: true,
		},
		{
			name: "policy update missing version",
			gs: &GenesisState{
				Params:   DefaultParams(),
				Policies: []*PolicyProfile{validPolicy},
				PolicyUpdates: []*PolicyUpdate{
					{PolicyId: "test-policy", PreviousVersion: "0.9.0", Updater: "lumera1owner"},
				},
			},
			wantErr: true,
		},
		{
			name: "policy update missing previous version",
			gs: &GenesisState{
				Params:   DefaultParams(),
				Policies: []*PolicyProfile{validPolicy},
				PolicyUpdates: []*PolicyUpdate{
					{PolicyId: "test-policy", Version: "1.0.0", Updater: "lumera1owner"},
				},
			},
			wantErr: true,
		},
		{
			name: "policy update missing updater",
			gs: &GenesisState{
				Params:   DefaultParams(),
				Policies: []*PolicyProfile{validPolicy},
				PolicyUpdates: []*PolicyUpdate{
					{PolicyId: "test-policy", Version: "1.0.0", PreviousVersion: "0.9.0"},
				},
			},
			wantErr: true,
		},
		{
			name: "policy update references missing policy version",
			gs: &GenesisState{
				Params:   DefaultParams(),
				Policies: []*PolicyProfile{validPolicy},
				PolicyUpdates: []*PolicyUpdate{
					{PolicyId: "test-policy", Version: "2.0.0", PreviousVersion: "1.0.0", Updater: "lumera1owner"},
				},
			},
			wantErr: true,
		},
		{
			name: "policy update references missing previous version",
			gs: &GenesisState{
				Params:   DefaultParams(),
				Policies: []*PolicyProfile{validPolicyV2},
				PolicyUpdates: []*PolicyUpdate{
					{PolicyId: "test-policy", Version: "2.0.0", PreviousVersion: "1.0.0", Updater: "lumera1owner"},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.gs.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGenesisStateValidateRejectsMalformedPolicyUpdateTimestamp(t *testing.T) {
	validPolicy := func(version string) *PolicyProfile {
		return &PolicyProfile{
			PolicyId: "test-policy",
			Version:  version,
			Metadata: &PolicyMetadata{Name: "Test Policy"},
			Lifecycle: &PolicyLifecycle{
				State: PolicyState_POLICY_STATE_ACTIVE,
			},
		}
	}

	gs := &GenesisState{
		Params:   DefaultParams(),
		Policies: []*PolicyProfile{validPolicy("1.0.0"), validPolicy("2.0.0")},
		PolicyUpdates: []*PolicyUpdate{
			{
				PolicyId:        "test-policy",
				Version:         "2.0.0",
				PreviousVersion: "1.0.0",
				UpdatedAt:       malformedTimestamp(),
				Updater:         "lumera1owner",
			},
		},
	}

	err := gs.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "updated_at is invalid")
}

func TestGenesisStateValidateRejectsUncanonicalPolicyIdentityFields(t *testing.T) {
	tests := []struct {
		name      string
		set       func(*PolicyProfile)
		wantField string
	}{
		{
			name: "policy_id leading space",
			set: func(policy *PolicyProfile) {
				policy.PolicyId = " test-policy"
			},
			wantField: "policy_id must not contain leading or trailing whitespace",
		},
		{
			name: "policy_id trailing space",
			set: func(policy *PolicyProfile) {
				policy.PolicyId = "test-policy "
			},
			wantField: "policy_id must not contain leading or trailing whitespace",
		},
		{
			name: "version leading space",
			set: func(policy *PolicyProfile) {
				policy.Version = " 1.0.0"
			},
			wantField: "version must not contain leading or trailing whitespace",
		},
		{
			name: "version trailing space",
			set: func(policy *PolicyProfile) {
				policy.Version = "1.0.0 "
			},
			wantField: "version must not contain leading or trailing whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := validPolicyProfile()
			tt.set(policy)

			err := (&GenesisState{
				Params:   DefaultParams(),
				Policies: []*PolicyProfile{policy},
			}).Validate()

			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantField)
		})
	}
}

func TestGenesisStateValidateRejectsUncanonicalPolicyUpdateFields(t *testing.T) {
	validPolicy := func(version string) *PolicyProfile {
		policy := validPolicyProfile()
		policy.Version = version
		return policy
	}

	tests := []struct {
		name      string
		set       func(*PolicyUpdate)
		wantField string
	}{
		{
			name: "policy_id",
			set: func(update *PolicyUpdate) {
				update.PolicyId = " test-policy"
			},
			wantField: "non-canonical policy_id",
		},
		{
			name: "version",
			set: func(update *PolicyUpdate) {
				update.Version = "2.0.0 "
			},
			wantField: "non-canonical version",
		},
		{
			name: "previous_version",
			set: func(update *PolicyUpdate) {
				update.PreviousVersion = " 1.0.0"
			},
			wantField: "non-canonical previous_version",
		},
		{
			name: "updater",
			set: func(update *PolicyUpdate) {
				update.Updater = " lumera1owner"
			},
			wantField: "non-canonical updater",
		},
		{
			name: "update_reason leading space",
			set: func(update *PolicyUpdate) {
				update.UpdateReason = " governance update"
			},
			wantField: "update_reason must not contain leading or trailing whitespace",
		},
		{
			name: "update_reason trailing space",
			set: func(update *PolicyUpdate) {
				update.UpdateReason = "governance update "
			},
			wantField: "update_reason must not contain leading or trailing whitespace",
		},
		{
			name: "update_reason oversized",
			set: func(update *PolicyUpdate) {
				update.UpdateReason = strings.Repeat("a", MaxPolicyUpdateReasonLen+1)
			},
			wantField: "update_reason exceeds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			update := &PolicyUpdate{
				PolicyId:        "test-policy",
				Version:         "2.0.0",
				PreviousVersion: "1.0.0",
				Updater:         "lumera1owner",
			}
			tt.set(update)

			err := (&GenesisState{
				Params:        DefaultParams(),
				Policies:      []*PolicyProfile{validPolicy("1.0.0"), validPolicy("2.0.0")},
				PolicyUpdates: []*PolicyUpdate{update},
			}).Validate()

			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantField)
		})
	}
}

func TestValidatePolicy(t *testing.T) {
	tests := []struct {
		name    string
		policy  *PolicyProfile
		wantErr bool
	}{
		{
			name: "valid policy",
			policy: &PolicyProfile{
				PolicyId: "test-policy",
				Version:  "1.0.0",
				Metadata: &PolicyMetadata{Name: "Test Policy"},
				Lifecycle: &PolicyLifecycle{
					State: PolicyState_POLICY_STATE_DRAFT,
				},
			},
			wantErr: false,
		},
		{
			name: "missing policy_id",
			policy: &PolicyProfile{
				PolicyId: "",
				Version:  "1.0.0",
				Metadata: &PolicyMetadata{Name: "Test Policy"},
				Lifecycle: &PolicyLifecycle{
					State: PolicyState_POLICY_STATE_DRAFT,
				},
			},
			wantErr: true,
		},
		{
			name: "missing version",
			policy: &PolicyProfile{
				PolicyId: "test-policy",
				Version:  "",
				Metadata: &PolicyMetadata{Name: "Test Policy"},
				Lifecycle: &PolicyLifecycle{
					State: PolicyState_POLICY_STATE_DRAFT,
				},
			},
			wantErr: true,
		},
		{
			name: "missing metadata",
			policy: &PolicyProfile{
				PolicyId:  "test-policy",
				Version:   "1.0.0",
				Metadata:  nil,
				Lifecycle: &PolicyLifecycle{},
			},
			wantErr: true,
		},
		{
			name: "empty metadata name",
			policy: &PolicyProfile{
				PolicyId:  "test-policy",
				Version:   "1.0.0",
				Metadata:  &PolicyMetadata{Name: ""},
				Lifecycle: &PolicyLifecycle{},
			},
			wantErr: true,
		},
		{
			name: "missing lifecycle",
			policy: &PolicyProfile{
				PolicyId:  "test-policy",
				Version:   "1.0.0",
				Metadata:  &PolicyMetadata{Name: "Test"},
				Lifecycle: nil,
			},
			wantErr: true,
		},
		{
			name: "valid with budgets",
			policy: &PolicyProfile{
				PolicyId: "budget-policy",
				Version:  "1.0.0",
				Metadata: &PolicyMetadata{Name: "Budget Policy"},
				Lifecycle: &PolicyLifecycle{
					State: PolicyState_POLICY_STATE_ACTIVE,
				},
				Budgets: &BudgetControls{
					PerCall: &BudgetLimit{
						SoftLimit: "1000000",
						HardLimit: "5000000",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid lifecycle successor",
			policy: &PolicyProfile{
				PolicyId: "successor-policy",
				Version:  "1.0.0",
				Metadata: &PolicyMetadata{
					Name: "Successor Policy",
				},
				Lifecycle: &PolicyLifecycle{
					State:             PolicyState_POLICY_STATE_DEPRECATED,
					SuccessorPolicyId: "successor-policy-v2",
				},
			},
			wantErr: false,
		},
		{
			name: "padded lifecycle successor",
			policy: &PolicyProfile{
				PolicyId: "successor-policy",
				Version:  "1.0.0",
				Metadata: &PolicyMetadata{
					Name: "Successor Policy",
				},
				Lifecycle: &PolicyLifecycle{
					State:             PolicyState_POLICY_STATE_DEPRECATED,
					SuccessorPolicyId: " successor-policy-v2",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid budgets",
			policy: &PolicyProfile{
				PolicyId: "budget-policy",
				Version:  "1.0.0",
				Metadata: &PolicyMetadata{Name: "Budget Policy"},
				Lifecycle: &PolicyLifecycle{
					State: PolicyState_POLICY_STATE_ACTIVE,
				},
				Budgets: &BudgetControls{
					PerCall: &BudgetLimit{
						SoftLimit: "",
						HardLimit: "5.0",
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePolicy(tt.policy)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePolicy() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePolicyRejectsMalformedLifecycleTimestamps(t *testing.T) {
	tests := []struct {
		name      string
		set       func(*PolicyLifecycle)
		wantField string
	}{
		{
			name: "created_at",
			set: func(lifecycle *PolicyLifecycle) {
				lifecycle.CreatedAt = malformedTimestamp()
			},
			wantField: "lifecycle.created_at is invalid",
		},
		{
			name: "activated_at",
			set: func(lifecycle *PolicyLifecycle) {
				lifecycle.ActivatedAt = malformedTimestamp()
			},
			wantField: "lifecycle.activated_at is invalid",
		},
		{
			name: "deprecated_at",
			set: func(lifecycle *PolicyLifecycle) {
				lifecycle.DeprecatedAt = malformedTimestamp()
			},
			wantField: "lifecycle.deprecated_at is invalid",
		},
		{
			name: "archived_at",
			set: func(lifecycle *PolicyLifecycle) {
				lifecycle.ArchivedAt = malformedTimestamp()
			},
			wantField: "lifecycle.archived_at is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := validPolicyProfile()
			tt.set(policy.Lifecycle)

			err := ValidatePolicy(policy)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantField)
		})
	}
}

func TestValidatePolicyRejectsOutOfOrderLifecycleTimestamps(t *testing.T) {
	base := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		set       func(*PolicyLifecycle)
		wantField string
	}{
		{
			name: "activated before created",
			set: func(lifecycle *PolicyLifecycle) {
				lifecycle.ActivatedAt = tsPtr(base.Add(-time.Second))
			},
			wantField: "lifecycle.activated_at cannot be before lifecycle.created_at",
		},
		{
			name: "deprecated before created",
			set: func(lifecycle *PolicyLifecycle) {
				lifecycle.DeprecatedAt = tsPtr(base.Add(-time.Second))
			},
			wantField: "lifecycle.deprecated_at cannot be before lifecycle.created_at",
		},
		{
			name: "deprecated before activated",
			set: func(lifecycle *PolicyLifecycle) {
				lifecycle.DeprecatedAt = tsPtr(base.Add(30 * time.Minute))
			},
			wantField: "lifecycle.deprecated_at cannot be before lifecycle.activated_at",
		},
		{
			name: "archived before created",
			set: func(lifecycle *PolicyLifecycle) {
				lifecycle.ArchivedAt = tsPtr(base.Add(-time.Second))
			},
			wantField: "lifecycle.archived_at cannot be before lifecycle.created_at",
		},
		{
			name: "archived before deprecated",
			set: func(lifecycle *PolicyLifecycle) {
				lifecycle.ArchivedAt = tsPtr(base.Add(90 * time.Minute))
			},
			wantField: "lifecycle.archived_at cannot be before lifecycle.deprecated_at",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := validPolicyProfile()
			policy.Lifecycle.CreatedAt = tsPtr(base)
			policy.Lifecycle.ActivatedAt = tsPtr(base.Add(time.Hour))
			policy.Lifecycle.DeprecatedAt = tsPtr(base.Add(2 * time.Hour))
			policy.Lifecycle.ArchivedAt = tsPtr(base.Add(3 * time.Hour))
			tt.set(policy.Lifecycle)

			err := ValidatePolicy(policy)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantField)
		})
	}
}

func TestValidatePolicyRejectsInvalidLifecycleStates(t *testing.T) {
	tests := []struct {
		name      string
		state     PolicyState
		wantField string
	}{
		{
			name:      "unspecified",
			state:     PolicyState_POLICY_STATE_UNSPECIFIED,
			wantField: "lifecycle.state is required",
		},
		{
			name:      "unknown",
			state:     PolicyState(99),
			wantField: "lifecycle.state has invalid value 99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := validPolicyProfile()
			policy.Lifecycle.State = tt.state

			err := ValidatePolicy(policy)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantField)
		})
	}
}

func TestValidatePolicyAllowsEqualLifecycleTimestamps(t *testing.T) {
	instant := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	policy := validPolicyProfile()
	policy.Lifecycle.CreatedAt = tsPtr(instant)
	policy.Lifecycle.ActivatedAt = tsPtr(instant)
	policy.Lifecycle.DeprecatedAt = tsPtr(instant)
	policy.Lifecycle.ArchivedAt = tsPtr(instant)

	require.NoError(t, ValidatePolicy(policy))
}

func TestValidatePolicyRejectsMalformedSignatureTimestamp(t *testing.T) {
	policy := validPolicyProfile()
	policy.Signatures = []*PolicySignature{
		{
			Signer:    "lumera1signer",
			Signature: []byte("signature"),
			SignedAt:  malformedTimestamp(),
			Algorithm: "ed25519",
		},
	}

	err := ValidatePolicy(policy)
	require.Error(t, err)
	require.ErrorContains(t, err, "signed_at is invalid")
}

func TestValidateBudgetControls(t *testing.T) {
	tests := []struct {
		name    string
		budgets *BudgetControls
		wantErr bool
	}{
		{
			name:    "nil budgets",
			budgets: &BudgetControls{},
			wantErr: false,
		},
		{
			name: "valid per_call only",
			budgets: &BudgetControls{
				PerCall: &BudgetLimit{SoftLimit: "1000000", HardLimit: "5000000"},
			},
			wantErr: false,
		},
		{
			name: "valid per_session only",
			budgets: &BudgetControls{
				PerSession: &BudgetLimit{SoftLimit: "10000000", HardLimit: "50000000"},
			},
			wantErr: false,
		},
		{
			name: "valid per_day only",
			budgets: &BudgetControls{
				PerDay: &BudgetLimit{SoftLimit: "100000000", HardLimit: "500000000"},
			},
			wantErr: false,
		},
		{
			name: "all limits set",
			budgets: &BudgetControls{
				PerCall:    &BudgetLimit{SoftLimit: "1000000", HardLimit: "5000000"},
				PerSession: &BudgetLimit{SoftLimit: "10000000", HardLimit: "50000000"},
				PerDay:     &BudgetLimit{SoftLimit: "100000000", HardLimit: "500000000"},
			},
			wantErr: false,
		},
		{
			name: "per_call missing soft_limit",
			budgets: &BudgetControls{
				PerCall: &BudgetLimit{SoftLimit: "", HardLimit: "5000000"},
			},
			wantErr: true,
		},
		{
			name: "per_session missing hard_limit",
			budgets: &BudgetControls{
				PerSession: &BudgetLimit{SoftLimit: "10000000", HardLimit: ""},
			},
			wantErr: true,
		},
		{
			name: "per_day missing both limits",
			budgets: &BudgetControls{
				PerDay: &BudgetLimit{SoftLimit: "", HardLimit: ""},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBudgetControls(tt.budgets)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBudgetControls() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func validPolicyProfile() *PolicyProfile {
	return &PolicyProfile{
		PolicyId: "test-policy",
		Version:  "1.0.0",
		Metadata: &PolicyMetadata{Name: "Test Policy"},
		Lifecycle: &PolicyLifecycle{
			State: PolicyState_POLICY_STATE_ACTIVE,
		},
	}
}

// malformedTimestamp returns a *time.Time whose year is out of the valid
// 1..9999 range, which the validators reject as "invalid". (The protobuf-go
// original used an out-of-range Nanos value; gogo stores a stdlib time.Time,
// so an out-of-range year is the equivalent malformed value.)
func malformedTimestamp() *time.Time {
	t := time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(-1, 0, 0)
	return &t
}

func TestValidateBudgetLimit(t *testing.T) {
	tests := []struct {
		name    string
		limit   *BudgetLimit
		label   string
		wantErr bool
	}{
		{
			name:    "valid",
			limit:   &BudgetLimit{SoftLimit: "1000000", HardLimit: "5000000"},
			label:   "test",
			wantErr: false,
		},
		{
			name:    "missing soft_limit",
			limit:   &BudgetLimit{SoftLimit: "", HardLimit: "5000000"},
			label:   "per_call",
			wantErr: true,
		},
		{
			name:    "missing hard_limit",
			limit:   &BudgetLimit{SoftLimit: "1000000", HardLimit: ""},
			label:   "per_session",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBudgetLimit(tt.limit, tt.label)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBudgetLimit() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
