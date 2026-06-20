
package keeper

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/policies/types"
)

// safeAddUint64 adds two uint64 values with overflow protection.
// Returns math.MaxUint64 if overflow would occur.
func safeAddUint64(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}

// validateBudgetInvariants enforces monotonic-nesting relationships on
// the caller-supplied period accumulators. Any hour-bucket spend is
// temporally contained in the current day, which is contained in the
// current week, which is contained in the current month. Therefore:
//
//	HourCostMicroLAC <= DayCostMicroLAC <= WeekCostMicroLAC <= MonthCostMicroLAC
//
// These invariants MUST hold across bucket rollovers too: immediately
// after an hour flip, HourCostMicroLAC resets to 0 but the previous
// hour's spend is still counted in DayCost/WeekCost/MonthCost, so the
// chain remains monotonic (0 <= X <= X <= X).
//
// Per lumera_ai-9oh42: InvocationRequest accumulators are caller-supplied
// compatibility hints, while EvaluatePolicy uses keeper-side persisted counters
// as the authoritative source. This guard still catches an important class of
// malformed requests — any caller whose values violate monotonicity is provably
// wrong and the request must be rejected rather than audited as valid input.
//
// Session is INTENTIONALLY excluded from the chain: a session can span
// multiple hours/days, so SessionCostMicroLAC has no ordering relation
// with the time-bucket accumulators.
func validateBudgetInvariants(req *InvocationRequest) string {
	if req.HourCostMicroLAC > req.DayCostMicroLAC {
		return fmt.Sprintf(
			"inconsistent accumulators: hour_cost=%d > day_cost=%d violates temporal nesting",
			req.HourCostMicroLAC, req.DayCostMicroLAC,
		)
	}
	if req.DayCostMicroLAC > req.WeekCostMicroLAC {
		return fmt.Sprintf(
			"inconsistent accumulators: day_cost=%d > week_cost=%d violates temporal nesting",
			req.DayCostMicroLAC, req.WeekCostMicroLAC,
		)
	}
	if req.WeekCostMicroLAC > req.MonthCostMicroLAC {
		return fmt.Sprintf(
			"inconsistent accumulators: week_cost=%d > month_cost=%d violates temporal nesting",
			req.WeekCostMicroLAC, req.MonthCostMicroLAC,
		)
	}
	return ""
}

// ToolContext describes a tool invocation for policy evaluation.
type ToolContext struct {
	ToolID         string
	Category       string
	Capabilities   []string
	Certifications []string
	// VerificationTier is the tool's current verification tier.
	VerificationTier types.VerificationTier
	// DisputeRateBps is the tool's dispute rate in basis points.
	DisputeRateBps uint32
	// UptimeBps is the tool's uptime in basis points.
	UptimeBps uint32
	// SandboxProfile is the sandbox profile the tool runs in.
	SandboxProfile string
	// IsolationLevel is the isolation level of the tool execution.
	IsolationLevel types.IsolationLevel
	// Region is the geographic region of the tool execution.
	Region string
}

// InvocationRequest captures the full context of a tool invocation to evaluate.
type InvocationRequest struct {
	Tool      ToolContext
	UserID    string
	SessionID string
	// CostMicroLAC is the estimated cost of this invocation in micro-LAC units.
	CostMicroLAC uint64
	// SessionCostMicroLAC is the accumulated cost in the current session.
	SessionCostMicroLAC uint64
	// HourCostMicroLAC is the accumulated cost for the current hour.
	HourCostMicroLAC uint64
	// DayCostMicroLAC is the accumulated cost for the current day.
	DayCostMicroLAC uint64
	// WeekCostMicroLAC is the accumulated cost for the current week.
	WeekCostMicroLAC uint64
	// MonthCostMicroLAC is the accumulated cost for the current month.
	MonthCostMicroLAC uint64
}

// PolicyDecision captures the result of a policy evaluation.
type PolicyDecision struct {
	Allowed      bool
	DenialReason string
	// Warnings contains non-blocking issues (e.g. soft limit approaching).
	Warnings []string
	// PolicyID identifies which policy was evaluated.
	PolicyID string
	// PolicyVersion is the version that was evaluated.
	PolicyVersion string
}

type budgetUsageUpdate struct {
	key   string
	total uint64
}

// EvaluatePolicy loads the latest active policy by ID and evaluates the
// invocation request against all applicable controls. It records an audit
// entry for the decision.
func (k Keeper) EvaluatePolicy(ctx context.Context, policyID string, req *InvocationRequest) (*PolicyDecision, error) {
	if req == nil {
		return nil, fmt.Errorf("invocation request is required")
	}

	policy, err := k.GetPolicy(ctx, policyID, "")
	if err != nil {
		return nil, err
	}

	if policy.Lifecycle == nil || policy.Lifecycle.State != types.PolicyState_POLICY_STATE_ACTIVE {
		return nil, types.ErrPolicyNotActive.Wrapf("policy %s is not active", policyID)
	}

	decision := &PolicyDecision{
		Allowed:       true,
		PolicyID:      policy.PolicyId,
		PolicyVersion: policy.Version,
	}
	var budgetUpdates []budgetUsageUpdate

	// Validate caller-supplied accumulator invariants BEFORE any budget
	// check consumes them. An inconsistent set of accumulators (e.g.
	// hour_cost > day_cost) is provably wrong — rejecting here prevents
	// the budget checks from silently evaluating against falsified data.
	// Legacy callers may still send accumulator hints. They are no longer
	// authoritative for keeper-side enforcement, but impossible nesting remains
	// useful input validation and catches malformed requests before auditing.
	if reason := validateBudgetInvariants(req); reason != "" {
		decision.Allowed = false
		decision.DenialReason = reason
	}

	// Check tool filters.
	if decision.Allowed {
		if reason := checkToolFilters(policy.ToolFilters, &req.Tool); reason != "" {
			decision.Allowed = false
			decision.DenialReason = reason
		}
	}

	// Check budget limits (only if tool filters passed).
	if decision.Allowed {
		reason, warnings, updates, err := k.checkBudgetLimits(ctx, policy, req)
		if err != nil {
			return nil, err
		}
		decision.Warnings = append(decision.Warnings, warnings...)
		budgetUpdates = updates
		if reason != "" {
			decision.Allowed = false
			decision.DenialReason = reason
		}
	}

	// Check security controls.
	if decision.Allowed {
		if reason := checkSecurityControls(policy.Security, &req.Tool); reason != "" {
			decision.Allowed = false
			decision.DenialReason = reason
		}
	}

	// Check privacy controls.
	if decision.Allowed {
		if reason := checkPrivacyControls(policy.Privacy, &req.Tool); reason != "" {
			decision.Allowed = false
			decision.DenialReason = reason
		}
	}

	if decision.Allowed {
		if err := k.commitBudgetUsage(ctx, budgetUpdates); err != nil {
			return nil, err
		}
	}

	// Record audit entry. Include a monotonic sequence so multiple evaluations
	// of the same (policy, tool, user) tuple within a single block do not
	// collide on auditID — otherwise later entries silently overwrite earlier
	// ones and the audit trail loses policy violations.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	seq, err := k.state.PolicyAuditCounter.Next(ctx)
	if err != nil {
		return nil, fmt.Errorf("allocate audit sequence: %w", err)
	}
	auditID := fmt.Sprintf("%s:%s:%s:%d:%d", policyID, req.Tool.ToolID, req.UserID, sdkCtx.BlockHeight(), seq)
	entry := &types.PolicyAuditEntry{
		AuditId:       auditID,
		PolicyId:      policyID,
		PolicyVersion: policy.Version,
		UserId:        req.UserID,
		ToolId:        req.Tool.ToolID,
		Action:        "evaluate",
		Allowed:       decision.Allowed,
		DenialReason:  decision.DenialReason,
		BlockHeight:   uint64(sdkCtx.BlockHeight()), //#nosec G115 -- block heights always non-negative
	}
	// Previously this call was log-and-continue on the theory that a
	// lost audit row was tolerable since the decision had already been
	// computed. That's wrong for the same reason cac_royalties commit
	// 42d16798e was wrong: when decision.Allowed, commitBudgetUsage
	// above has already persisted budget deltas in the tx-scoped ctx;
	// returning nil here tells the caller the evaluation committed
	// cleanly, and the budget write commits with the msg. Swallowing
	// the audit-save error then leaves the chain with a
	// budget-consumed-but-no-audit-row state that breaks downstream
	// compliance / dispute-resolution paths. Returning the error
	// instead lets msg-scoped revert roll back the budget write
	// atomically with the failed audit, preserving
	// "audit row iff budget moved" as an invariant. For denied
	// evaluations no budget writes happened, so the revert is a
	// no-op beyond undoing the audit-counter advance — equally safe.
	if err := k.RecordAuditEntry(ctx, entry); err != nil {
		return nil, fmt.Errorf("record policy audit entry: %w", err)
	}

	return decision, nil
}

func (k Keeper) checkBudgetLimits(ctx context.Context, policy *types.PolicyProfile, req *InvocationRequest) (string, []string, []budgetUsageUpdate, error) {
	if policy == nil || policy.Budgets == nil {
		return "", nil, nil, nil
	}
	if req == nil {
		return "invocation request is required", nil, nil, nil
	}

	budgets := policy.Budgets
	var warnings []string
	var updates []budgetUsageUpdate

	if budgets.PerCall != nil {
		if reason, warn := evaluateBudgetLimit(budgets.PerCall, req.CostMicroLAC, "per-call"); reason != "" {
			return reason, warnings, nil, nil
		} else if warn != "" {
			warnings = append(warnings, warn)
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	periods := budgetUsagePeriods(sdkCtx)
	checks := []struct {
		limit    *types.BudgetLimit
		scope    string
		periodID string
	}{
		{limit: budgets.PerSession, scope: "per-session", periodID: budgetSessionPeriod(req.SessionID)},
		{limit: budgets.PerHour, scope: "per-hour", periodID: periods.hour},
		{limit: budgets.PerDay, scope: "per-day", periodID: periods.day},
		{limit: budgets.PerWeek, scope: "per-week", periodID: periods.week},
		{limit: budgets.PerMonth, scope: "per-month", periodID: periods.month},
	}

	for _, check := range checks {
		if check.limit == nil {
			continue
		}
		key := budgetUsageKey(policy.PolicyId, req.UserID, check.scope, check.periodID)
		current, err := k.getBudgetUsage(ctx, key)
		if err != nil {
			return "", warnings, nil, err
		}
		total := safeAddUint64(current, req.CostMicroLAC)
		if reason, warn := evaluateBudgetLimit(check.limit, total, check.scope); reason != "" {
			return reason, warnings, nil, nil
		} else if warn != "" {
			warnings = append(warnings, warn)
		}
		updates = append(updates, budgetUsageUpdate{key: key, total: total})
	}

	if budgets.PerTool != nil {
		if limit, ok := budgets.PerTool[req.Tool.ToolID]; ok {
			key := budgetUsageKey(policy.PolicyId, req.UserID, "per-tool", req.Tool.ToolID)
			current, err := k.getBudgetUsage(ctx, key)
			if err != nil {
				return "", warnings, nil, err
			}
			total := safeAddUint64(current, req.CostMicroLAC)
			if reason, warn := evaluateBudgetLimit(limit, total, "per-tool"); reason != "" {
				return reason, warnings, nil, nil
			} else if warn != "" {
				warnings = append(warnings, warn)
			}
			updates = append(updates, budgetUsageUpdate{key: key, total: total})
		}
	}

	if budgets.PerCategory != nil && req.Tool.Category != "" {
		if limit, ok := budgets.PerCategory[req.Tool.Category]; ok {
			key := budgetUsageKey(policy.PolicyId, req.UserID, "per-category", req.Tool.Category)
			current, err := k.getBudgetUsage(ctx, key)
			if err != nil {
				return "", warnings, nil, err
			}
			total := safeAddUint64(current, req.CostMicroLAC)
			if reason, warn := evaluateBudgetLimit(limit, total, "per-category"); reason != "" {
				return reason, warnings, nil, nil
			} else if warn != "" {
				warnings = append(warnings, warn)
			}
			updates = append(updates, budgetUsageUpdate{key: key, total: total})
		}
	}

	return "", warnings, updates, nil
}

func (k Keeper) getBudgetUsage(ctx context.Context, key string) (uint64, error) {
	usage, err := k.state.BudgetUsage.Get(ctx, key)
	if errors.Is(err, collections.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get budget usage %q: %w", key, err)
	}
	return usage, nil
}

func (k Keeper) commitBudgetUsage(ctx context.Context, updates []budgetUsageUpdate) error {
	for _, update := range updates {
		if err := k.state.BudgetUsage.Set(ctx, update.key, update.total); err != nil {
			return fmt.Errorf("set budget usage %q: %w", update.key, err)
		}
	}
	return nil
}

type budgetPeriods struct {
	hour  string
	day   string
	week  string
	month string
}

func budgetUsagePeriods(ctx sdk.Context) budgetPeriods {
	now := ctx.BlockTime().UTC()
	isoYear, isoWeek := now.ISOWeek()

	return budgetPeriods{
		hour:  now.Format("2006010215"),
		day:   now.Format("20060102"),
		week:  fmt.Sprintf("%04d-W%02d", isoYear, isoWeek),
		month: now.Format("200601"),
	}
}

func budgetSessionPeriod(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return "default"
	}
	return sessionID
}

// budgetUsageKey composes the BudgetUsage index key as a
// length-prefix-encoded concatenation of its parts. The encoding is
// injective — no combination of parts can alias another combination —
// which is the guarantee we need for state-keyed counters (the same
// reason disputeSubmissionKey uses the same form at
// x/challenges/keeper/dispute.go:312).
//
// DESIGN NOTE on policy.Version participation in the key
// (lumera_ai-veuki):
//
// Every caller composes the key as
//
//	budgetUsageKey(policy.PolicyId, userID, scope, periodID)
//
// at enforce.go:273. The OMISSION of policy.Version means that
// UpdatePolicy (which bumps the version) PRESERVES every user's
// accumulated budget for every period that is still live. The
// next call after an UpdatePolicy reads the existing usage from the
// version-agnostic key.
//
// This is intentional for safety: administrators who use UpdatePolicy
// for routine cosmetic changes (metadata tweaks, adding a single tool
// to an allowlist) will not silently grant every user a fresh budget
// for the remainder of the current period. Pin test
// TestEvaluatePolicy_VersionUpgradeResetsBudgetCounter_BudgetBypassRisk
// in enforce_test.go exercises this path end-to-end to ensure the
// budget bypass risk remains mitigated.
//
// If a future governance model wants per-policy-version counters that
// reset on bumps, policy.Version MUST be explicitly added back to the
// callers at enforce.go:273. Do NOT alter budgetUsageKey itself — the
// key composition is a single injective function and the parts list
// should be fully controlled by the caller.
func budgetUsageKey(parts ...string) string {
	var builder strings.Builder
	for _, part := range parts {
		builder.WriteString(strconv.Itoa(len(part)))
		builder.WriteByte(':')
		builder.WriteString(part)
		builder.WriteByte('|')
	}
	return builder.String()
}

// checkToolFilters evaluates tool filters against the tool context.
// Returns an empty string if the tool is allowed, or a denial reason.
func checkToolFilters(filters *types.ToolFilters, tool *ToolContext) string {
	if filters == nil {
		return ""
	}

	// Denied tools take precedence.
	for _, denied := range filters.DeniedTools {
		if denied == tool.ToolID {
			return fmt.Sprintf("tool %s is explicitly denied", tool.ToolID)
		}
	}

	// Denied categories.
	if tool.Category != "" {
		for _, denied := range filters.DeniedCategories {
			if denied == tool.Category {
				return fmt.Sprintf("category %s is denied", tool.Category)
			}
		}
	}

	// Allowed tools allowlist (if specified, only listed tools pass).
	if len(filters.AllowedTools) > 0 {
		found := false
		for _, allowed := range filters.AllowedTools {
			if allowed == tool.ToolID {
				found = true
				break
			}
		}
		if !found {
			return fmt.Sprintf("tool %s is not in the allowed list", tool.ToolID)
		}
	}

	// Allowed categories allowlist.
	if len(filters.AllowedCategories) > 0 {
		found := false
		for _, allowed := range filters.AllowedCategories {
			if allowed == tool.Category {
				found = true
				break
			}
		}
		if !found {
			return fmt.Sprintf("category %s is not in the allowed list", tool.Category)
		}
	}

	// Required capabilities.
	if len(filters.RequiredCapabilities) > 0 {
		capSet := make(map[string]struct{}, len(tool.Capabilities))
		for _, c := range tool.Capabilities {
			capSet[c] = struct{}{}
		}
		for _, req := range filters.RequiredCapabilities {
			if _, ok := capSet[req]; !ok {
				return fmt.Sprintf("missing required capability: %s", req)
			}
		}
	}

	// Forbidden capabilities.
	for _, forbidden := range filters.ForbiddenCapabilities {
		for _, cap := range tool.Capabilities {
			if cap == forbidden {
				return fmt.Sprintf("tool has forbidden capability: %s", forbidden)
			}
		}
	}

	// Minimum verification tier.
	if filters.MinVerificationTier > types.VerificationTier_VERIFICATION_TIER_UNSPECIFIED {
		if tool.VerificationTier < filters.MinVerificationTier {
			return fmt.Sprintf("tool verification tier %s below minimum %s",
				tool.VerificationTier.String(), filters.MinVerificationTier.String())
		}
	}

	// Maximum dispute rate.
	if filters.MaxDisputeRateBps > 0 && tool.DisputeRateBps > filters.MaxDisputeRateBps {
		return fmt.Sprintf("dispute rate %d bps exceeds maximum %d bps",
			tool.DisputeRateBps, filters.MaxDisputeRateBps)
	}

	// Minimum uptime.
	if filters.MinUptimeBps > 0 && tool.UptimeBps < filters.MinUptimeBps {
		return fmt.Sprintf("uptime %d bps below minimum %d bps",
			tool.UptimeBps, filters.MinUptimeBps)
	}

	// Required certifications.
	if len(filters.RequiredCertifications) > 0 {
		certSet := make(map[string]struct{}, len(tool.Certifications))
		for _, c := range tool.Certifications {
			certSet[c] = struct{}{}
		}
		for _, req := range filters.RequiredCertifications {
			if _, ok := certSet[req]; !ok {
				return fmt.Sprintf("missing required certification: %s", req)
			}
		}
	}

	return ""
}

// checkBudgetLimits evaluates budget controls against the invocation cost.
// Returns (denialReason, warnings). An empty denial reason means allowed.
func checkBudgetLimits(budgets *types.BudgetControls, req *InvocationRequest) (string, []string) {
	if budgets == nil {
		return "", nil
	}
	if req == nil {
		return "invocation request is required", nil
	}

	var warnings []string

	// Check per-call limit.
	if budgets.PerCall != nil {
		if reason, warn := evaluateBudgetLimit(budgets.PerCall, req.CostMicroLAC, "per-call"); reason != "" {
			return reason, warnings
		} else if warn != "" {
			warnings = append(warnings, warn)
		}
	}

	// Check per-session limit.
	if budgets.PerSession != nil {
		totalSession := safeAddUint64(req.SessionCostMicroLAC, req.CostMicroLAC)
		if reason, warn := evaluateBudgetLimit(budgets.PerSession, totalSession, "per-session"); reason != "" {
			return reason, warnings
		} else if warn != "" {
			warnings = append(warnings, warn)
		}
	}

	// Check per-hour limit.
	if budgets.PerHour != nil {
		totalHour := safeAddUint64(req.HourCostMicroLAC, req.CostMicroLAC)
		if reason, warn := evaluateBudgetLimit(budgets.PerHour, totalHour, "per-hour"); reason != "" {
			return reason, warnings
		} else if warn != "" {
			warnings = append(warnings, warn)
		}
	}

	// Check per-day limit.
	if budgets.PerDay != nil {
		totalDay := safeAddUint64(req.DayCostMicroLAC, req.CostMicroLAC)
		if reason, warn := evaluateBudgetLimit(budgets.PerDay, totalDay, "per-day"); reason != "" {
			return reason, warnings
		} else if warn != "" {
			warnings = append(warnings, warn)
		}
	}

	// Check per-week limit.
	if budgets.PerWeek != nil {
		totalWeek := safeAddUint64(req.WeekCostMicroLAC, req.CostMicroLAC)
		if reason, warn := evaluateBudgetLimit(budgets.PerWeek, totalWeek, "per-week"); reason != "" {
			return reason, warnings
		} else if warn != "" {
			warnings = append(warnings, warn)
		}
	}

	// Check per-month limit.
	if budgets.PerMonth != nil {
		totalMonth := safeAddUint64(req.MonthCostMicroLAC, req.CostMicroLAC)
		if reason, warn := evaluateBudgetLimit(budgets.PerMonth, totalMonth, "per-month"); reason != "" {
			return reason, warnings
		} else if warn != "" {
			warnings = append(warnings, warn)
		}
	}

	// Check per-tool limit.
	if budgets.PerTool != nil {
		if limit, ok := budgets.PerTool[req.Tool.ToolID]; ok {
			if reason, warn := evaluateBudgetLimit(limit, req.CostMicroLAC, "per-tool"); reason != "" {
				return reason, warnings
			} else if warn != "" {
				warnings = append(warnings, warn)
			}
		}
	}

	// Check per-category limit.
	if budgets.PerCategory != nil && req.Tool.Category != "" {
		if limit, ok := budgets.PerCategory[req.Tool.Category]; ok {
			if reason, warn := evaluateBudgetLimit(limit, req.CostMicroLAC, "per-category"); reason != "" {
				return reason, warnings
			} else if warn != "" {
				warnings = append(warnings, warn)
			}
		}
	}

	return "", warnings
}

// evaluateBudgetLimit checks a single BudgetLimit against an amount.
// Returns (denialReason, warning).
func evaluateBudgetLimit(limit *types.BudgetLimit, amountMicroLAC uint64, scope string) (string, string) {
	if strings.TrimSpace(limit.HardLimit) != "" {
		hardLimit, hardOK := parseMicroLAC(limit.HardLimit)
		if !hardOK {
			return fmt.Sprintf("%s budget hard limit unparseable: %q", scope, limit.HardLimit), ""
		}
		if amountMicroLAC > hardLimit {
			action := limit.ActionOnHard
			if action == types.BudgetActionType_BUDGET_ACTION_TYPE_DENY ||
				action == types.BudgetActionType_BUDGET_ACTION_TYPE_UNSPECIFIED {
				return fmt.Sprintf("%s budget hard limit exceeded: %d > %d", scope, amountMicroLAC, hardLimit), ""
			}
			if action == types.BudgetActionType_BUDGET_ACTION_TYPE_QUEUE ||
				action == types.BudgetActionType_BUDGET_ACTION_TYPE_ESCALATE {
				return fmt.Sprintf("%s budget hard limit exceeded (requires approval): %d > %d", scope, amountMicroLAC, hardLimit), ""
			}
			if action == types.BudgetActionType_BUDGET_ACTION_TYPE_WARN {
				return "", fmt.Sprintf("%s budget hard limit exceeded (warning): %d > %d", scope, amountMicroLAC, hardLimit)
			}
		}
	}

	if strings.TrimSpace(limit.ApprovalRequiredAbove) != "" {
		approvalThreshold, approvalOK := parseMicroLAC(limit.ApprovalRequiredAbove)
		if !approvalOK {
			return fmt.Sprintf("%s budget approval threshold unparseable: %q", scope, limit.ApprovalRequiredAbove), ""
		}
		if amountMicroLAC > approvalThreshold {
			return fmt.Sprintf("%s budget requires approval: %d > %d", scope, amountMicroLAC, approvalThreshold), ""
		}
	}

	if strings.TrimSpace(limit.SoftLimit) != "" {
		softLimit, softOK := parseMicroLAC(limit.SoftLimit)
		if softOK && amountMicroLAC > softLimit {
			return "", fmt.Sprintf("%s budget soft limit approached: %d > %d", scope, amountMicroLAC, softLimit)
		}
	}

	return "", ""
}

// parseMicroLAC parses a string amount to uint64. Returns (value, true) on
// success or empty input, (0, false) when the non-empty string is malformed.
func parseMicroLAC(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, true
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// checkSecurityControls evaluates security policy against tool context.
func checkSecurityControls(security *types.SecurityControls, tool *ToolContext) string {
	if security == nil {
		return ""
	}

	// Check isolation level requirement.
	if security.IsolationLevel > types.IsolationLevel_ISOLATION_LEVEL_UNSPECIFIED {
		if tool.IsolationLevel < security.IsolationLevel {
			return fmt.Sprintf("isolation level %s below required %s",
				tool.IsolationLevel.String(), security.IsolationLevel.String())
		}
	}

	// Check required sandbox profiles.
	if len(security.RequiredSandboxProfiles) > 0 {
		found := false
		for _, required := range security.RequiredSandboxProfiles {
			if required == tool.SandboxProfile {
				found = true
				break
			}
		}
		if !found {
			return fmt.Sprintf("sandbox profile %s not in required profiles", tool.SandboxProfile)
		}
	}

	// Check forbidden sandbox profiles.
	for _, forbidden := range security.ForbiddenSandboxProfiles {
		if forbidden == tool.SandboxProfile {
			return fmt.Sprintf("sandbox profile %s is forbidden", tool.SandboxProfile)
		}
	}

	// Check geo-blocking.
	if len(security.GeoBlocking) > 0 && tool.Region != "" {
		for _, blocked := range security.GeoBlocking {
			if blocked == tool.Region {
				return fmt.Sprintf("region %s is geo-blocked", tool.Region)
			}
		}
	}

	return ""
}

// checkPrivacyControls evaluates privacy policy against tool context.
// It checks data residency, data classification handling policies,
// encryption requirements, enclave requirements, and allowed providers.
func checkPrivacyControls(privacy *types.PrivacyControls, tool *ToolContext) string {
	if privacy == nil {
		return ""
	}

	// Check data residency.
	if privacy.DataResidency != nil {
		if reason := checkDataResidency(privacy.DataResidency, tool); reason != "" {
			return reason
		}
	}

	// Check data classification handling policies.
	if privacy.DataClassification != nil {
		if reason := checkDataClassification(privacy.DataClassification, tool); reason != "" {
			return reason
		}
	}

	// Check encryption requirements against tool capabilities.
	if privacy.EncryptionRequirements != nil {
		if reason := checkEncryptionRequirements(privacy.EncryptionRequirements, tool); reason != "" {
			return reason
		}
	}

	// Check enclave requirement for the tool's category.
	if privacy.EnclaveRequirements != nil {
		if reason := checkEnclaveRequirements(privacy.EnclaveRequirements, tool); reason != "" {
			return reason
		}
	}

	return ""
}

// checkDataResidency enforces geographic data handling constraints.
func checkDataResidency(dr *types.DataResidency, tool *ToolContext) string {
	if len(dr.AllowedRegions) > 0 {
		if tool.Region == "" {
			return "tool region is unspecified but data residency policy requires one of the allowed regions"
		}
		found := false
		for _, region := range dr.AllowedRegions {
			if region == tool.Region {
				found = true
				break
			}
		}
		if !found {
			return fmt.Sprintf("region %s not in allowed data residency regions", tool.Region)
		}
	}

	// Check denied regions.
	if len(dr.DeniedRegions) > 0 && tool.Region == "" {
		return "tool region is unspecified but data residency policy requires verification against denied regions"
	}
	for _, denied := range dr.DeniedRegions {
		if denied == tool.Region {
			return fmt.Sprintf("region %s is denied by data residency policy", tool.Region)
		}
	}

	return ""
}

// checkDataClassification verifies that the tool's isolation level is
// sufficient for the configured data handling policies. When a policy
// requires DENY handling for a data class, the tool is blocked. When it
// requires ENCRYPT handling, the tool must support at least container
// isolation.
func checkDataClassification(dc *types.DataClassification, tool *ToolContext) string {
	checks := []struct {
		policy types.DataHandlingPolicy
		label  string
	}{
		{dc.PiiHandling, "PII"},
		{dc.PhiHandling, "PHI"},
		{dc.PciHandling, "PCI"},
		{dc.ProprietaryHandling, "proprietary"},
	}

	for _, check := range checks {
		switch check.policy {
		case types.DataHandlingPolicy_DATA_HANDLING_POLICY_DENY:
			// If the policy denies a data class and the tool processes that
			// class (indicated by matching certifications), block the tool.
			if hasCertification(tool, strings.ToLower(check.label)+"_handler") {
				return fmt.Sprintf("policy denies %s data handling but tool has %s_handler certification",
					check.label, strings.ToLower(check.label))
			}
		case types.DataHandlingPolicy_DATA_HANDLING_POLICY_ENCRYPT:
			// Encryption handling requires at least container-level isolation.
			if hasCertification(tool, strings.ToLower(check.label)+"_handler") &&
				tool.IsolationLevel < types.IsolationLevel_ISOLATION_LEVEL_CONTAINER {
				return fmt.Sprintf("%s data requires encryption handling but tool isolation level %s is insufficient (need container+)",
					check.label, tool.IsolationLevel.String())
			}
		case types.DataHandlingPolicy_DATA_HANDLING_POLICY_REDACT:
			// Redaction requires the tool to have redaction capability.
			if hasCertification(tool, strings.ToLower(check.label)+"_handler") &&
				!hasCapability(tool, "redaction") {
				return fmt.Sprintf("%s data requires redaction but tool lacks redaction capability", check.label)
			}
		}
	}

	return ""
}

// checkEncryptionRequirements validates that the tool's isolation level is
// sufficient for the configured encryption level.
func checkEncryptionRequirements(enc *types.EncryptionRequirements, tool *ToolContext) string {
	// Higher encryption levels require higher isolation.
	switch enc.AtRest {
	case types.EncryptionLevel_ENCRYPTION_LEVEL_QUANTUM_RESISTANT:
		if tool.IsolationLevel < types.IsolationLevel_ISOLATION_LEVEL_ENCLAVE {
			return fmt.Sprintf("quantum-resistant encryption requires enclave isolation but tool has %s",
				tool.IsolationLevel.String())
		}
	case types.EncryptionLevel_ENCRYPTION_LEVEL_AES512:
		if tool.IsolationLevel < types.IsolationLevel_ISOLATION_LEVEL_VM {
			return fmt.Sprintf("AES-512 encryption requires VM+ isolation but tool has %s",
				tool.IsolationLevel.String())
		}
	}

	// HSM or enclave key management requires enclave isolation.
	if enc.KeyManagement == "hsm" || enc.KeyManagement == "enclave" {
		if tool.IsolationLevel < types.IsolationLevel_ISOLATION_LEVEL_ENCLAVE {
			return fmt.Sprintf("key management %q requires enclave isolation but tool has %s",
				enc.KeyManagement, tool.IsolationLevel.String())
		}
	}

	return ""
}

// checkEnclaveRequirements validates TEE requirements including category
// enforcement and allowed provider constraints.
func checkEnclaveRequirements(req *types.EnclaveRequirements, tool *ToolContext) string {
	if tool.Category == "" {
		return ""
	}

	categoryRequiresEnclave := false
	for _, cat := range req.RequiredFor {
		if cat == tool.Category {
			categoryRequiresEnclave = true
			break
		}
	}

	if categoryRequiresEnclave {
		if tool.IsolationLevel < types.IsolationLevel_ISOLATION_LEVEL_ENCLAVE {
			return fmt.Sprintf("enclave execution required for category %s", tool.Category)
		}

		// If allowed providers are specified, a declared provider identity is
		// required and must match the policy allowlist.
		if len(req.AllowedProviders) > 0 {
			if strings.TrimSpace(tool.SandboxProfile) == "" {
				return fmt.Sprintf("enclave provider is required for category %s", tool.Category)
			}
			found := false
			for _, provider := range req.AllowedProviders {
				if provider == tool.SandboxProfile {
					found = true
					break
				}
			}
			if !found {
				return fmt.Sprintf("enclave provider %s not in allowed providers for category %s",
					tool.SandboxProfile, tool.Category)
			}
		}
	}

	return ""
}

// hasCertification checks if the tool has a specific certification.
func hasCertification(tool *ToolContext, cert string) bool {
	for _, c := range tool.Certifications {
		if c == cert {
			return true
		}
	}
	return false
}

// hasCapability checks if the tool has a specific capability.
func hasCapability(tool *ToolContext, cap string) bool {
	for _, c := range tool.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}
