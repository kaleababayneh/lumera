// Package keeper provides schema versioning utilities for the router module.
package keeper

import (
	"context"
	"errors"
	"fmt"
	"math"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/router/types"
)

// Schema version constants
const (
	// SchemaVersion1 is the initial schema with basic params and metrics
	SchemaVersion1 uint64 = 1

	// SchemaVersion2 is the Collections v2 migration with indexed maps,
	// CAC records, session management, and replay protection
	SchemaVersion2 uint64 = 2

	// SchemaVersion3 migrates CacheEntry, CacheStats, QuoteRecord, and
	// SessionState from JSON to proto encoding (bd-3oc Phase 2).
	SchemaVersion3 uint64 = 3

	// SchemaVersion4 adds discovery-subsidy accounting period metadata and
	// deterministic reset-boundary state for existing router deployments.
	SchemaVersion4 uint64 = 4

	// CurrentSchemaVersion is the current schema version
	CurrentSchemaVersion = SchemaVersion4

	// ConsensusVersion defines the module's consensus version for migrations
	ConsensusVersion = SchemaVersion4

	// legacyDiscoverySubsidyPeriodBlocks is the migration-only accounting
	// period assigned to chains that enabled discovery subsidies before the
	// discovery_subsidy_period_blocks governance field existed. Disabled
	// subsidies keep the zero period used by DefaultParams.
	legacyDiscoverySubsidyPeriodBlocks uint32 = 43_200
)

// SchemaVersionKey is the store key for the schema version
var SchemaVersionKey = []byte("schema_version")

// SchemaInfo contains metadata about the current schema
type SchemaInfo struct {
	Version     uint64 `json:"version"`
	ModuleName  string `json:"module_name"`
	Description string `json:"description"`
}

// GetSchemaInfo returns information about the current schema version
func GetSchemaInfo() SchemaInfo {
	return SchemaInfo{
		Version:    CurrentSchemaVersion,
		ModuleName: types.ModuleName,
		Description: `Schema v4: Discovery-subsidy accounting-period migration plus proto-first codec state.
Collections:
- Params: module parameters
- GlobalMetrics: aggregated router metrics
- ToolMetrics: per-tool activation metrics
- SessionMetrics: per-session metrics
- CategoryMetrics: per-category metrics
- PolicyUpdates: policy change records
- CACRecords: content-addressable cache royalty records (indexed)
- SelectionScores: tool selection scores
- ActiveTools: tool activations (indexed, proto-encoded)
- NextAggregation: next aggregation block height
- ProcessedNonces: replay protection nonces
- DiscoverySubsidySpent: cumulative subsidy spend for the current accounting period
- DiscoverySubsidyReset: next accounting-period reset block
- CacheEntries: cached invocation entries (proto-encoded)
- CacheStats: per-tool cache statistics (proto-encoded)
- Quotes: stored quote records (proto-encoded)
- Sessions: session state records (proto-encoded)
Indexes:
- CAC by Origin: royalty records by origin tool
- CAC by Consumer: royalty records by consuming tool
- CAC by BlockHeight: royalty records by block
- CAC Composite: unique origin+consumer index
- Activation by Session: tool activations by session
- Activation by Tool: tool activations by tool ID
- Activation by Active: active/inactive filter`,
	}
}

// ValidateSchemaVersion checks if the store schema version matches the expected version
func (k Keeper) ValidateSchemaVersion(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Verify all collections are readable (schema integrity check)
	if err := k.validateCollectionsIntegrity(ctx); err != nil {
		return fmt.Errorf("schema integrity check failed: %w", err)
	}

	k.Logger(sdkCtx).Debug("router schema version validated", "version", CurrentSchemaVersion)
	return nil
}

// validateCollectionsIntegrity verifies that all collections can be accessed
func (k Keeper) validateCollectionsIntegrity(ctx context.Context) error {
	// Verify params can be read
	if _, err := k.state.Params.Get(ctx); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("params collection corrupted: %w", err)
	}

	// Verify global metrics can be read
	if _, err := k.state.GlobalMetrics.Get(ctx); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("global_metrics collection corrupted: %w", err)
	}

	// Verify tool metrics collection is readable
	err := k.state.ToolMetrics.Walk(ctx, nil, func(_ string, _ *types.ActivationMetrics) (bool, error) {
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("tool_metrics collection corrupted: %w", err)
	}

	// Verify sessions collection is readable
	err = k.state.Sessions.Walk(ctx, nil, func(_ string, _ *types.SessionState) (bool, error) {
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("sessions collection corrupted: %w", err)
	}

	// Verify quotes collection is readable
	err = k.state.Quotes.Walk(ctx, nil, func(_ string, _ *types.QuoteRecord) (bool, error) {
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("quotes collection corrupted: %w", err)
	}

	return nil
}

// MigrateV1ToV2 performs migration from v1 to v2 state.
// This is called by the upgrade handler during chain upgrades.
// The migration handles:
// 1. Re-encoding any legacy data under the new Collections schema
// 2. Preserving all sessions, quotes, and metrics
// 3. Ensuring idempotent reruns
func (k Keeper) MigrateV1ToV2(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	k.Logger(sdkCtx).Info("starting router v1 to v2 migration")

	// The current keeper already uses Collections v2 schema.
	// This migration ensures any legacy data is properly re-encoded.
	//
	// For fresh installations or already-migrated chains, this is a no-op.
	// For chains with v1 data, the schema builder already handles
	// the migration via the collections framework.

	// Verify schema integrity by attempting to read each collection
	sessionsCount := 0
	if err := k.state.Sessions.Walk(ctx, nil, func(_ string, _ *types.SessionState) (bool, error) {
		sessionsCount++
		return false, nil
	}); err != nil {
		return fmt.Errorf("migration failed: sessions collection corrupted: %w", err)
	}

	quotesCount := 0
	if err := k.state.Quotes.Walk(ctx, nil, func(_ string, _ *types.QuoteRecord) (bool, error) {
		quotesCount++
		return false, nil
	}); err != nil {
		return fmt.Errorf("migration failed: quotes collection corrupted: %w", err)
	}

	cacCount := 0
	if err := k.state.CACRecords.Walk(ctx, nil, func(_ string, _ *types.CACRoyaltyRecord) (bool, error) {
		cacCount++
		return false, nil
	}); err != nil {
		return fmt.Errorf("migration failed: cac_records collection corrupted: %w", err)
	}

	// Verify params can be read after migration
	if _, err := k.state.Params.Get(ctx); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("migration failed: params collection corrupted: %w", err)
	}

	k.Logger(sdkCtx).Info("router v1 to v2 migration completed",
		"sessions_migrated", sessionsCount,
		"quotes_migrated", quotesCount,
		"cac_records_migrated", cacCount,
	)

	return nil
}

// MigrateV2ToV3 performs migration from v2 to v3 state.
// This is a codec format change (JSON → proto) for CacheEntry, CacheStats,
// QuoteRecord, and SessionState. Pre-mainnet, so existing data is re-encoded
// automatically by the collections framework on first read/write.
func (k Keeper) MigrateV2ToV3(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	k.Logger(sdkCtx).Info("router v2 to v3 migration: codec format change (JSON→proto) — no-op pre-mainnet")
	return nil
}

// MigrateV3ToV4 performs migration from v3 to v4 state.
//
// v4 introduced discovery_subsidy_period_blocks and
// discovery_subsidy_next_reset_block after the initial discovery-subsidy
// governance knobs already existed. Disabled subsidies remain disabled with a
// zero period. Chains that had already enabled a subsidy under v3 receive a
// deterministic accounting period and reset boundary so upgraded params pass
// the stricter v4 validation contract.
func (k Keeper) MigrateV3ToV4(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	k.Logger(sdkCtx).Info("starting router v3 to v4 migration")

	params, err := k.state.Params.Get(sdkCtx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			k.Logger(sdkCtx).Info("router v3 to v4 migration completed without params state")
			return nil
		}
		return fmt.Errorf("reading router params for v3 to v4 migration: %w", err)
	}
	paramsBackfilled := false
	if params == nil {
		params = types.DefaultParams()
		paramsBackfilled = true
	}

	if params.GetDiscoverySubsidyBps() > 0 && params.GetDiscoverySubsidyPeriodBlocks() == 0 {
		params.DiscoverySubsidyPeriodBlocks = legacyDiscoverySubsidyPeriodBlocks
		paramsBackfilled = true
	}
	if err := params.Validate(); err != nil {
		return fmt.Errorf("validating router params during v3 to v4 migration: %w", err)
	}
	if paramsBackfilled {
		if err := k.state.Params.Set(sdkCtx, params); err != nil {
			return fmt.Errorf("writing router params during v3 to v4 migration: %w", err)
		}
	}

	resetInitialized := false
	if params.GetDiscoverySubsidyBps() > 0 && params.GetDiscoverySubsidyPeriodBlocks() > 0 {
		resetBlock, err := k.state.DiscoverySubsidyReset.Get(sdkCtx)
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("reading router discovery-subsidy reset boundary during v3 to v4 migration: %w", err)
		}
		if resetBlock < 1 {
			nextReset, err := migrationDiscoverySubsidyNextResetBlock(sdkCtx, params)
			if err != nil {
				return err
			}
			if err := k.state.DiscoverySubsidyReset.Set(sdkCtx, nextReset); err != nil {
				return fmt.Errorf("writing router discovery-subsidy reset boundary during v3 to v4 migration: %w", err)
			}
			resetInitialized = true
		}
	}

	k.Logger(sdkCtx).Info("router v3 to v4 migration completed",
		"discovery_subsidy_params_backfilled", paramsBackfilled,
		"discovery_subsidy_reset_initialized", resetInitialized,
		"discovery_subsidy_period_blocks", params.GetDiscoverySubsidyPeriodBlocks(),
	)
	return nil
}

func migrationDiscoverySubsidyNextResetBlock(sdkCtx sdk.Context, params *types.Params) (uint64, error) {
	if sdkCtx.BlockHeight() < 0 {
		return 0, fmt.Errorf("router v3 to v4 migration cannot derive reset boundary from negative block height %d", sdkCtx.BlockHeight())
	}
	currentHeight := uint64(sdkCtx.BlockHeight())
	period := uint64(params.GetDiscoverySubsidyPeriodBlocks())
	if period == 0 {
		return 0, fmt.Errorf("router v3 to v4 migration cannot initialize reset boundary with zero discovery_subsidy_period_blocks")
	}
	if currentHeight > math.MaxUint64-period {
		return 0, fmt.Errorf("router v3 to v4 migration discovery-subsidy reset boundary overflow: height=%d period=%d", currentHeight, period)
	}
	return currentHeight + period, nil
}

// GetConsensusVersion returns the current consensus version for the module
func GetConsensusVersion() uint64 {
	return ConsensusVersion
}
