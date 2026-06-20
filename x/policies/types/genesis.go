
package types

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const MaxPolicyUpdateReasonLen = 4 * 1024

// DefaultGenesis returns the default genesis state for the policies module.
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:              DefaultParams(),
		Policies:            []*PolicyProfile{},
		PolicyUpdates:       []*PolicyUpdate{},
		PolicyUpdateCounter: 0,
	}
}

// DefaultParams returns the default module parameters.
func DefaultParams() *Params {
	return &Params{
		MinPolicyDeposit:              "1000000", // 1 LAC in micro-units
		MaxPolicyVersionHistory:       100,       // Keep up to 100 versions per policy
		DefaultMigrationWindowSeconds: 604800,    // 7 days
		MaxInheritanceDepth:           5,         // Max 5 levels of policy inheritance
		DefaultAuditRetentionDays:     365,       // 1 year
	}
}

// Validate performs basic genesis state validation.
func (gs *GenesisState) Validate() error {
	if gs == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}
	if gs.Params == nil {
		return fmt.Errorf("params are required")
	}
	if err := gs.Params.Validate(); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}

	seenPolicies := make(map[string]bool)
	for i, policy := range gs.Policies {
		if policy == nil {
			return fmt.Errorf("policy at index %d is nil", i)
		}
		key := policyVersionKey(policy.PolicyId, policy.Version)
		if seenPolicies[key] {
			return fmt.Errorf("duplicate policy: %s version %s", policy.PolicyId, policy.Version)
		}
		seenPolicies[key] = true

		if err := ValidatePolicy(policy); err != nil {
			return fmt.Errorf("invalid policy %s: %w", policy.PolicyId, err)
		}
	}

	return validatePolicyUpdates(gs.PolicyUpdates, seenPolicies)
}

func validatePolicyUpdates(updates []*PolicyUpdate, policyKeys map[string]bool) error {
	for i, update := range updates {
		if update == nil {
			return fmt.Errorf("policy update at index %d is nil", i)
		}
		if strings.TrimSpace(update.PolicyId) == "" {
			return fmt.Errorf("policy update at index %d has empty policy_id", i)
		}
		if strings.TrimSpace(update.PolicyId) != update.PolicyId {
			return fmt.Errorf("policy update %q at index %d has non-canonical policy_id", update.PolicyId, i)
		}
		if strings.TrimSpace(update.Version) == "" {
			return fmt.Errorf("policy update %s at index %d has empty version", update.PolicyId, i)
		}
		if strings.TrimSpace(update.Version) != update.Version {
			return fmt.Errorf("policy update %s:%q at index %d has non-canonical version", update.PolicyId, update.Version, i)
		}
		if strings.TrimSpace(update.PreviousVersion) == "" {
			return fmt.Errorf("policy update %s:%s at index %d has empty previous_version", update.PolicyId, update.Version, i)
		}
		if strings.TrimSpace(update.PreviousVersion) != update.PreviousVersion {
			return fmt.Errorf("policy update %s:%s at index %d has non-canonical previous_version", update.PolicyId, update.Version, i)
		}
		if strings.TrimSpace(update.Updater) == "" {
			return fmt.Errorf("policy update %s:%s at index %d has empty updater", update.PolicyId, update.Version, i)
		}
		if strings.TrimSpace(update.Updater) != update.Updater {
			return fmt.Errorf("policy update %s:%s at index %d has non-canonical updater", update.PolicyId, update.Version, i)
		}
		if err := ValidatePolicyUpdateReason(update.UpdateReason); err != nil {
			return fmt.Errorf("policy update %s:%s at index %d has invalid update_reason: %w", update.PolicyId, update.Version, i, err)
		}
		if !policyKeys[policyVersionKey(update.PolicyId, update.Version)] {
			return fmt.Errorf("policy update %s:%s at index %d references unknown policy version", update.PolicyId, update.Version, i)
		}
		if !policyKeys[policyVersionKey(update.PolicyId, update.PreviousVersion)] {
			return fmt.Errorf("policy update %s:%s at index %d references unknown previous_version %s", update.PolicyId, update.Version, i, update.PreviousVersion)
		}
		if err := validatePolicyTimestamp(fmt.Sprintf("policy update %s:%s at index %d", update.PolicyId, update.Version, i), "updated_at", update.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

func policyVersionKey(policyID, version string) string {
	return policyID + ":" + version
}

// ValidatePolicyUpdateReason validates optional policy update audit text.
func ValidatePolicyUpdateReason(reason string) error {
	if strings.TrimSpace(reason) != reason {
		return fmt.Errorf("update_reason must not contain leading or trailing whitespace")
	}
	if len(reason) > MaxPolicyUpdateReasonLen {
		return fmt.Errorf("update_reason exceeds %d-byte cap (got %d)", MaxPolicyUpdateReasonLen, len(reason))
	}
	return nil
}

// Validate validates the module parameters.
func (p *Params) Validate() error {
	if p == nil {
		return fmt.Errorf("params are required")
	}
	if _, ok := parseUint64String(p.MinPolicyDeposit); !ok {
		return fmt.Errorf("min_policy_deposit must be a valid uint64 string")
	}
	if p.MaxPolicyVersionHistory == 0 {
		return fmt.Errorf("max_policy_version_history must be positive")
	}
	if p.DefaultMigrationWindowSeconds == 0 {
		return fmt.Errorf("default_migration_window_seconds must be positive")
	}
	if p.MaxInheritanceDepth == 0 || p.MaxInheritanceDepth > 10 {
		return fmt.Errorf("max_inheritance_depth must be between 1 and 10")
	}
	if p.DefaultAuditRetentionDays == 0 {
		return fmt.Errorf("default_audit_retention_days must be positive")
	}
	return nil
}

// ValidatePolicy validates a policy profile.
func ValidatePolicy(policy *PolicyProfile) error {
	if err := validateCanonicalRequiredString("policy_id", policy.PolicyId); err != nil {
		return err
	}
	if err := validateCanonicalRequiredString("version", policy.Version); err != nil {
		return err
	}
	if policy.Metadata == nil {
		return fmt.Errorf("metadata is required")
	}
	if policy.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if policy.Lifecycle == nil {
		return fmt.Errorf("lifecycle is required")
	}
	if err := validatePolicyLifecycleState(policy.Lifecycle.State); err != nil {
		return err
	}
	if err := validateOptionalCanonicalString("lifecycle.successor_policy_id", policy.Lifecycle.SuccessorPolicyId); err != nil {
		return err
	}
	if err := validatePolicyLifecycleTimestamps(policy); err != nil {
		return err
	}
	for i, signature := range policy.Signatures {
		if signature == nil {
			return fmt.Errorf("signature at index %d is nil", i)
		}
		if err := validatePolicyTimestamp(fmt.Sprintf("policy %s:%s signature[%d]", policy.PolicyId, policy.Version, i), "signed_at", signature.SignedAt); err != nil {
			return err
		}
	}

	// Validate budget limits if present
	if policy.Budgets != nil {
		if err := ValidateBudgetControls(policy.Budgets); err != nil {
			return fmt.Errorf("invalid budgets: %w", err)
		}
	}

	// Validate tool filter consistency. Overlapping allow/deny lists are
	// silently won by the deny side at evaluation time, hiding operator
	// intent and producing confusing audit trails; reject them up front.
	// BPS-valued fields must fit the [0, MaxPolicyBPS] range so the policy
	// cannot be created in a state that no real tool could ever satisfy.
	if filters := policy.ToolFilters; filters != nil {
		if dup := firstOverlap(filters.AllowedTools, filters.DeniedTools); dup != "" {
			return fmt.Errorf("tool_filters: %q appears in both allowed_tools and denied_tools", dup)
		}
		if dup := firstOverlap(filters.AllowedCategories, filters.DeniedCategories); dup != "" {
			return fmt.Errorf("tool_filters: %q appears in both allowed_categories and denied_categories", dup)
		}
		if dup := firstOverlap(filters.RequiredCapabilities, filters.ForbiddenCapabilities); dup != "" {
			return fmt.Errorf("tool_filters: %q appears in both required_capabilities and forbidden_capabilities", dup)
		}
		if filters.MaxDisputeRateBps > MaxPolicyBPS {
			return fmt.Errorf("tool_filters.max_dispute_rate_bps %d exceeds %d", filters.MaxDisputeRateBps, MaxPolicyBPS)
		}
		if filters.MinUptimeBps > MaxPolicyBPS {
			return fmt.Errorf("tool_filters.min_uptime_bps %d exceeds %d", filters.MinUptimeBps, MaxPolicyBPS)
		}
	}

	return nil
}

func validatePolicyLifecycleState(state PolicyState) error {
	switch state {
	case PolicyState_POLICY_STATE_DRAFT,
		PolicyState_POLICY_STATE_REVIEW,
		PolicyState_POLICY_STATE_ACTIVE,
		PolicyState_POLICY_STATE_DEPRECATED,
		PolicyState_POLICY_STATE_ARCHIVED:
		return nil
	case PolicyState_POLICY_STATE_UNSPECIFIED:
		return fmt.Errorf("lifecycle.state is required")
	default:
		return fmt.Errorf("lifecycle.state has invalid value %d", state)
	}
}

func validateCanonicalRequiredString(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	return nil
}

func validatePolicyLifecycleTimestamps(policy *PolicyProfile) error {
	owner := fmt.Sprintf("policy %s:%s", policy.PolicyId, policy.Version)
	for _, field := range []struct {
		name string
		ts   *time.Time
	}{
		{name: "lifecycle.created_at", ts: policy.Lifecycle.CreatedAt},
		{name: "lifecycle.activated_at", ts: policy.Lifecycle.ActivatedAt},
		{name: "lifecycle.deprecated_at", ts: policy.Lifecycle.DeprecatedAt},
		{name: "lifecycle.archived_at", ts: policy.Lifecycle.ArchivedAt},
	} {
		if err := validatePolicyTimestamp(owner, field.name, field.ts); err != nil {
			return err
		}
	}
	if err := validatePolicyTimestampNotBefore(owner, "lifecycle.activated_at", policy.Lifecycle.ActivatedAt, "lifecycle.created_at", policy.Lifecycle.CreatedAt); err != nil {
		return err
	}
	if err := validatePolicyTimestampNotBefore(owner, "lifecycle.deprecated_at", policy.Lifecycle.DeprecatedAt, "lifecycle.created_at", policy.Lifecycle.CreatedAt); err != nil {
		return err
	}
	if err := validatePolicyTimestampNotBefore(owner, "lifecycle.deprecated_at", policy.Lifecycle.DeprecatedAt, "lifecycle.activated_at", policy.Lifecycle.ActivatedAt); err != nil {
		return err
	}
	if err := validatePolicyTimestampNotBefore(owner, "lifecycle.archived_at", policy.Lifecycle.ArchivedAt, "lifecycle.created_at", policy.Lifecycle.CreatedAt); err != nil {
		return err
	}
	if err := validatePolicyTimestampNotBefore(owner, "lifecycle.archived_at", policy.Lifecycle.ArchivedAt, "lifecycle.deprecated_at", policy.Lifecycle.DeprecatedAt); err != nil {
		return err
	}
	return nil
}

func validatePolicyTimestamp(owner, field string, ts *time.Time) error {
	if ts == nil {
		return nil
	}
	if ts.Year() < 1 || ts.Year() > 9999 {
		return fmt.Errorf("%s %s is invalid: year out of range", owner, field)
	}
	return nil
}

func validatePolicyTimestampNotBefore(owner, laterField string, later *time.Time, earlierField string, earlier *time.Time) error {
	if later == nil || earlier == nil {
		return nil
	}
	if later.Before(*earlier) {
		return fmt.Errorf("%s %s cannot be before %s", owner, laterField, earlierField)
	}
	return nil
}

// MaxPolicyBPS is the upper bound for any basis-points-valued policy field.
// Values above 10000 (= 100%) are nonsense for percentages-of-something
// policy knobs; rejecting them up front prevents un-satisfiable filters
// from being persisted and saves agents from chasing impossible thresholds.
const MaxPolicyBPS = uint32(10000)

// firstOverlap returns the first element appearing in both a and b, or "" if
// they are disjoint. Order in a is preserved so error messages are stable.
// Both slices are expected to be small (filter lists), so the linear scan is
// cheaper than allocating a map.
func firstOverlap(a, b []string) string {
	if len(a) == 0 || len(b) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(b))
	for _, v := range b {
		seen[v] = struct{}{}
	}
	for _, v := range a {
		if _, ok := seen[v]; ok {
			return v
		}
	}
	return ""
}

// ValidateBudgetControls validates budget configuration.
func ValidateBudgetControls(budgets *BudgetControls) error {
	if budgets == nil {
		return nil
	}
	if budgets.PerCall != nil {
		if err := ValidateBudgetLimit(budgets.PerCall, "per_call"); err != nil {
			return err
		}
	}
	if budgets.PerSession != nil {
		if err := ValidateBudgetLimit(budgets.PerSession, "per_session"); err != nil {
			return err
		}
	}
	if budgets.PerHour != nil {
		if err := ValidateBudgetLimit(budgets.PerHour, "per_hour"); err != nil {
			return err
		}
	}
	if budgets.PerDay != nil {
		if err := ValidateBudgetLimit(budgets.PerDay, "per_day"); err != nil {
			return err
		}
	}
	if budgets.PerWeek != nil {
		if err := ValidateBudgetLimit(budgets.PerWeek, "per_week"); err != nil {
			return err
		}
	}
	if budgets.PerMonth != nil {
		if err := ValidateBudgetLimit(budgets.PerMonth, "per_month"); err != nil {
			return err
		}
	}
	if budgets.PerTool != nil {
		for toolID, limit := range budgets.PerTool {
			if err := ValidateBudgetLimit(limit, fmt.Sprintf("per_tool[%s]", toolID)); err != nil {
				return err
			}
		}
	}
	if budgets.PerCategory != nil {
		for category, limit := range budgets.PerCategory {
			if err := ValidateBudgetLimit(limit, fmt.Sprintf("per_category[%s]", category)); err != nil {
				return err
			}
		}
	}
	if budgets.BurstAllowance != nil {
		return fmt.Errorf("burst_allowance is not supported by keeper evaluation")
	}
	if budgets.ReserveAllocation != nil {
		return fmt.Errorf("reserve_allocation is not supported by keeper evaluation")
	}
	return nil
}

// ValidateBudgetLimit validates a single budget limit.
func ValidateBudgetLimit(limit *BudgetLimit, name string) error {
	if limit == nil {
		return fmt.Errorf("%s is required", name)
	}
	if limit.SoftLimit == "" {
		return fmt.Errorf("%s.soft_limit is required", name)
	}
	if limit.HardLimit == "" {
		return fmt.Errorf("%s.hard_limit is required", name)
	}

	soft, ok := parseUint64String(limit.SoftLimit)
	if !ok {
		return fmt.Errorf("%s.soft_limit must be a valid uint64 string", name)
	}
	hard, ok := parseUint64String(limit.HardLimit)
	if !ok {
		return fmt.Errorf("%s.hard_limit must be a valid uint64 string", name)
	}
	if soft > hard {
		return fmt.Errorf("%s.soft_limit (%d) must not exceed hard_limit (%d)", name, soft, hard)
	}

	if err := validateBudgetAction(limit.ActionOnSoft, name, "action_on_soft", map[BudgetActionType]struct{}{
		BudgetActionType_BUDGET_ACTION_TYPE_UNSPECIFIED: {},
		BudgetActionType_BUDGET_ACTION_TYPE_WARN:        {},
		BudgetActionType_BUDGET_ACTION_TYPE_THROTTLE:    {},
		BudgetActionType_BUDGET_ACTION_TYPE_APPROVE:     {},
	}); err != nil {
		return err
	}
	if err := validateBudgetAction(limit.ActionOnHard, name, "action_on_hard", map[BudgetActionType]struct{}{
		BudgetActionType_BUDGET_ACTION_TYPE_UNSPECIFIED: {},
		BudgetActionType_BUDGET_ACTION_TYPE_DENY:        {},
		BudgetActionType_BUDGET_ACTION_TYPE_QUEUE:       {},
		BudgetActionType_BUDGET_ACTION_TYPE_ESCALATE:    {},
	}); err != nil {
		return err
	}

	if limit.ApprovalRequiredAbove != "" {
		approvalRequiredAbove, ok := parseUint64String(limit.ApprovalRequiredAbove)
		if !ok {
			return fmt.Errorf("%s.approval_required_above must be a valid uint64 string", name)
		}
		if approvalRequiredAbove > hard {
			return fmt.Errorf("%s.approval_required_above (%d) must not exceed hard_limit (%d)", name, approvalRequiredAbove, hard)
		}
		if len(limit.Approvers) == 0 {
			return fmt.Errorf("%s.approvers is required when approval_required_above is set", name)
		}
	}

	return nil
}

func validateBudgetAction(action BudgetActionType, name, field string, allowed map[BudgetActionType]struct{}) error {
	if _, ok := allowed[action]; ok {
		return nil
	}
	return fmt.Errorf("%s.%s has unsupported action %s", name, field, action.String())
}

func parseUint64String(s string) (uint64, bool) {
	if strings.TrimSpace(s) != s {
		return 0, false
	}
	value, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}
