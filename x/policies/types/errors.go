
package types

import (
	"cosmossdk.io/errors"
)

// x/policies module sentinel errors
var (
	// ErrPolicyNotFound is returned when a policy cannot be found.
	ErrPolicyNotFound = errors.Register(ModuleName, 2, "policy not found")
	// ErrPolicyAlreadyExists is returned when attempting to create a duplicate policy.
	ErrPolicyAlreadyExists = errors.Register(ModuleName, 3, "policy already exists")
	// ErrInvalidPolicyState is returned for invalid policy state transitions.
	ErrInvalidPolicyState = errors.Register(ModuleName, 4, "invalid policy state")
	// ErrUnauthorized is returned when the signer lacks permission.
	ErrUnauthorized = errors.Register(ModuleName, 5, "unauthorized")
	// ErrInvalidPolicyID is returned for malformed policy IDs.
	ErrInvalidPolicyID = errors.Register(ModuleName, 6, "invalid policy ID")
	// ErrInvalidPolicyVersion is returned for malformed versions.
	ErrInvalidPolicyVersion = errors.Register(ModuleName, 7, "invalid policy version")
	// ErrPolicyNotActive is returned when a policy is not in active state.
	ErrPolicyNotActive = errors.Register(ModuleName, 8, "policy not active")
	// ErrPolicyDeprecated is returned when using a deprecated policy.
	ErrPolicyDeprecated = errors.Register(ModuleName, 9, "policy is deprecated")
	// ErrBudgetExceeded is returned when a budget limit is exceeded.
	ErrBudgetExceeded = errors.Register(ModuleName, 10, "budget limit exceeded")
	// ErrToolNotAllowed is returned when a tool is filtered out by policy.
	ErrToolNotAllowed = errors.Register(ModuleName, 11, "tool not allowed by policy")
	// ErrJurisdictionViolation is returned for geographic compliance failures.
	ErrJurisdictionViolation = errors.Register(ModuleName, 12, "jurisdiction violation")
	// ErrPrivacyViolation is returned for privacy constraint failures.
	ErrPrivacyViolation = errors.Register(ModuleName, 13, "privacy violation")
	// ErrInvalidSignature is returned when policy signature verification fails.
	ErrInvalidSignature = errors.Register(ModuleName, 14, "invalid policy signature")
	// ErrInheritanceCycle is returned when policy inheritance creates a cycle.
	ErrInheritanceCycle = errors.Register(ModuleName, 15, "policy inheritance cycle detected")
	// ErrMaxInheritanceDepth is returned when inheritance chain is too deep.
	ErrMaxInheritanceDepth = errors.Register(ModuleName, 16, "max inheritance depth exceeded")
	// ErrInvalidBudgetConfig is returned for invalid budget configuration.
	ErrInvalidBudgetConfig = errors.Register(ModuleName, 17, "invalid budget configuration")
	// ErrRateLimitExceeded is returned when rate limits are exceeded.
	ErrRateLimitExceeded = errors.Register(ModuleName, 18, "rate limit exceeded")
	// ErrEgressDenied is returned when egress policy blocks a request.
	ErrEgressDenied = errors.Register(ModuleName, 19, "egress denied by policy")
	// ErrApprovalRequired is returned when manual approval is needed.
	ErrApprovalRequired = errors.Register(ModuleName, 20, "approval required")
	// ErrInvalidParams is returned when module parameters are invalid.
	ErrInvalidParams = errors.Register(ModuleName, 21, "invalid module parameters")
)
