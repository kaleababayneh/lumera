// Package types provides the core types for the x/policies module.
//
// The policies module manages policy profiles that define tool selection constraints,
// budget controls, privacy requirements, security settings, and compliance requirements
// for AI agent tool invocations via the Lumera router.
//
// Key concepts:
//   - PolicyProfile: Complete policy definition with budgets, privacy, security, compliance
//   - PolicyState: Lifecycle states (draft, review, active, deprecated, archived)
//   - BudgetControls: Multi-tier budget limits (per-call, per-session, per-day, etc.)
//   - ToolFilters: Tool selection constraints (categories, verification tiers, etc.)
//   - PrivacyControls: Data handling, encryption, residency requirements
//   - SecurityControls: Authentication, sandboxing, rate limiting
//
// The module supports policy inheritance, versioning, and cryptographic signing
// for tamper-proof policy enforcement.
package types
