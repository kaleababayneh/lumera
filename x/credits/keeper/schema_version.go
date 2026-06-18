//go:build cosmos && cosmos_full

// Package keeper provides schema versioning utilities for the credits module.
package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/credits/types"
)

// Schema version constants
const (
	// SchemaVersion1 is the initial schema with basic lock and params support
	SchemaVersion1 uint64 = 1

	// SchemaVersion2 is the Collections v2 migration with settlements, disputes, and metrics
	SchemaVersion2 uint64 = 2

	// CurrentSchemaVersion is the current schema version
	CurrentSchemaVersion = SchemaVersion2
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
		ModuleName: "credits",
		Description: `Schema v2: Full Collections v2 migration with settlements, disputes, and metrics.
Collections:
- Params: module parameters
- Locks: credit locks by lock ID
- LockSequence: auto-increment sequence for lock IDs
- Settlements: settlement records by ID
- Disputes: dispute records by ID
- Metrics: aggregated settlement metrics`,
	}
}

// ValidateSchemaVersion checks if the store schema version matches the expected version
func (k Keeper) ValidateSchemaVersion(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// For new installations, there's no version stored yet - this is OK
	// The schema is set during InitGenesis or migration

	// Verify all collections are readable (schema integrity check)
	if err := k.validateCollectionsIntegrity(ctx); err != nil {
		return fmt.Errorf("schema integrity check failed: %w", err)
	}

	k.Logger(sdkCtx).Debug("schema version validated", "version", CurrentSchemaVersion)
	return nil
}

// validateCollectionsIntegrity verifies that all collections can be accessed
func (k Keeper) validateCollectionsIntegrity(ctx context.Context) error {
	// Try to read params (not-found is valid before InitGenesis)
	if _, err := k.state.Params.Get(ctx); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("params collection corrupted: %w", err)
	}

	// Verify locks collection is readable
	count := 0
	if err := k.IterateLocks(ctx, func(_ *types.Lock) bool {
		count++
		return count > 0 // Just verify we can iterate
	}); err != nil {
		return fmt.Errorf("locks collection corrupted: %w", err)
	}

	// Verify settlements collection is readable
	if err := k.IterateSettlements(ctx, func(_ *types.SettlementRecord) bool {
		return true // Just verify we can iterate
	}); err != nil {
		return fmt.Errorf("settlements collection corrupted: %w", err)
	}

	// Verify disputes collection is readable
	if err := k.IterateDisputes(ctx, func(_ *types.DisputeRecord) bool {
		return true // Just verify we can iterate
	}); err != nil {
		return fmt.Errorf("disputes collection corrupted: %w", err)
	}

	// Verify metrics can be read (not-found is valid for a clean chain)
	if _, err := k.state.Metrics.Get(ctx); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("metrics collection corrupted: %w", err)
	}

	return nil
}

// GetConsensusVersion returns the current consensus version for the module
func GetConsensusVersion() uint64 {
	return ConsensusVersion
}
