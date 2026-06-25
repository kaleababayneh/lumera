// Package keeper implements replay protection for tool invocations per Phase 2.11.7.2
package keeper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/router/types"
)

// Replay protection configuration constants
const (
	// MaxTimestampSkew is the maximum allowed difference between request timestamp and block time
	MaxTimestampSkew = 5 * time.Minute

	// NonceTTL is how long nonce records are kept in state before cleanup
	NonceTTL = 24 * time.Hour

	// GasPerReplayCheck is the gas cost for replay protection check
	GasPerReplayCheck = 1000

	// MaxCleanupPerBlock is the maximum number of expired nonces to clean per block
	MaxCleanupPerBlock = 100

	// MaxDuplicateCheckIterations is the maximum number of nonce records to scan when checking for duplicate invocations
	// This prevents DoS attacks via unbounded iteration (Security Finding MEDIUM-2, 2025-09-30)
	MaxDuplicateCheckIterations = 1000

	// MaxInvokeArgs caps the number of entries in InvokeRequest.Args
	// before computeInvocationHash iterates them. Without this bound,
	// an attacker submitting max-size txs with hundreds of thousands of
	// tiny-entry Args maps consumes asymmetric validator compute (sort
	// + sha256 hashing of every key+value), since the cosmos tx-size
	// cap (~10MB) is ~3 orders of magnitude looser than what any
	// realistic tool invocation needs. 128 is a comfortable ceiling
	// that admits every plausible legitimate tool args while rejecting
	// the DoS amplification surface (lumera_ai-o5xc1).
	MaxInvokeArgs = 128
)

// CheckAndRecordInvocation verifies that an invocation is not a replay,
// and records it if valid. Returns error if replay detected.
// Uses Collections API for deterministic on-chain state.
func (k Keeper) CheckAndRecordInvocation(
	ctx context.Context,
	req *types.InvokeRequest,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Consume gas for replay check
	sdkCtx.GasMeter().ConsumeGas(GasPerReplayCheck, "router/replay-check")

	if req == nil {
		return fmt.Errorf("invocation request cannot be nil")
	}

	// Reject oversized args BEFORE computeInvocationHash sorts +
	// sha256s every entry. Counterparty-controlled Args is the
	// asymmetric-compute surface flagged in lumera_ai-o5xc1.
	if len(req.Args) > MaxInvokeArgs {
		return fmt.Errorf("invocation args count %d exceeds maximum %d",
			len(req.Args), MaxInvokeArgs)
	}

	// Validate nonce presence
	if req.Nonce == "" {
		return fmt.Errorf("nonce is required for replay protection")
	}

	// Validate timestamp presence
	if req.Timestamp.IsZero() {
		return fmt.Errorf("timestamp is required for replay protection")
	}

	reqTimestamp := req.Timestamp

	// Use deterministic block time for timestamp validation
	blockTime := sdkCtx.BlockTime()

	// Check timestamp is not too far in the future or past
	timeDiff := blockTime.Sub(reqTimestamp)
	if timeDiff < -MaxTimestampSkew || timeDiff > MaxTimestampSkew {
		return fmt.Errorf(
			"timestamp skew too large: %v (max: %v)",
			timeDiff,
			MaxTimestampSkew,
		)
	}

	// Create unique key from nonce + tool_id + session_id
	// This prevents reuse of same nonce across different invocations
	key := makeNonceKey(req.Nonce, req.ToolId, req.SessionId)

	// Compute invocation hash for detecting replay of equivalent requests
	invocationHash := computeInvocationHash(req)

	// Check if this nonce+context has been seen before
	exists, err := k.state.ProcessedNonces.Has(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to check nonce existence: %w", err)
	}

	if exists {
		// Get the existing record for detailed error
		record, err := k.state.ProcessedNonces.Get(ctx, key)
		if err != nil {
			return fmt.Errorf("failed to retrieve existing nonce record: %w", err)
		}

		return fmt.Errorf(
			"replay attack detected: nonce '%s' already used for tool '%s' at %v",
			req.Nonce,
			req.ToolId,
			record.FirstSeen,
		)
	}

	// Check if an equivalent invocation hash exists
	// (catches replays with different nonces but identical content)
	if err := k.checkDuplicateInvocation(ctx, invocationHash, key); err != nil {
		return err
	}

	// Record the invocation using Collections API
	record := types.NewNonceRecord(req.Nonce, req.ToolId, req.SessionId)
	record.Timestamp = req.Timestamp
	record.SetFirstSeen(blockTime)
	record.InvocationHash = invocationHash

	if err := k.state.ProcessedNonces.Set(ctx, key, record); err != nil {
		return fmt.Errorf("failed to record nonce: %w", err)
	}

	return nil
}

// makeNonceKey creates a collision-free cache key from (nonce, toolID,
// sessionID) using a length-prefix encoding.
//
// The prior "%s:%s:%s" format was a latent collision class: the nonce
// is caller-supplied with no charset restriction (publisher SDKs and
// relayers may set arbitrary bytes), so crafted tuples can produce
// identical keys even though each field individually passes validation.
// Example collision:
//
//	("a:b:c", "t1", "d") → "a:b:c:t1:d"   (stored by user A)
//	("a",     "b",  "c:t1:d") → "a:b:c:t1:d" (crafted by user B)
//
// — both produce the same key. Tool IDs are restricted to [a-z0-9-._]
// by x/registry/types/validations.go:validateToolID (no ':' allowed),
// so the attacker needs TWO registered tools to exploit this as a
// cross-user DoS; but once a future validation change loosens tool or
// session charset, the replay-protection key space silently collapses.
// The length-prefix form eliminates the whole class by making the
// encoding injective regardless of which characters the inputs contain.
//
// Matches the pattern used by x/policies/keeper/enforce.go:budgetUsageKey
// and x/challenges/keeper/dispute.go:disputeSubmissionKey — each part is
// emitted as "{len}:{value}|" so the parser can always recover the
// original tuple from the composed string.
func makeNonceKey(nonce, toolID, sessionID string) string {
	var b strings.Builder
	b.Grow(len(nonce) + len(toolID) + len(sessionID) + 16)
	b.WriteString(strconv.Itoa(len(nonce)))
	b.WriteByte(':')
	b.WriteString(nonce)
	b.WriteByte('|')
	b.WriteString(strconv.Itoa(len(toolID)))
	b.WriteByte(':')
	b.WriteString(toolID)
	b.WriteByte('|')
	b.WriteString(strconv.Itoa(len(sessionID)))
	b.WriteByte(':')
	b.WriteString(sessionID)
	b.WriteByte('|')
	return b.String()
}

// computeInvocationHash creates a deterministic hash of the invocation
// to detect equivalent requests with different nonces
func computeInvocationHash(req *types.InvokeRequest) string {
	h := sha256.New()
	writeString := func(value string) {
		if value == "" {
			return
		}
		h.Write([]byte(value))
		h.Write([]byte{0})
	}

	// Include stable fields that define request semantics (excluding nonce/timestamp).
	writeString(req.ToolId)
	writeString(req.SessionId)
	writeString(req.MaxCost)
	writeString(req.QuoteId)
	if req.AcceptCached {
		writeString(strconv.FormatBool(req.AcceptCached))
	}

	if len(req.Args) > 0 {
		keys := make([]string, 0, len(req.Args))
		for k := range req.Args {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			h.Write([]byte(key))
			h.Write([]byte{0})
			h.Write([]byte(req.Args[key]))
			h.Write([]byte{0})
		}
	}

	return hex.EncodeToString(h.Sum(nil))
}

// checkDuplicateInvocation scans for duplicate invocation hashes using the secondary index
// This replaces the O(N) scan with an O(1) index lookup
func (k Keeper) checkDuplicateInvocation(ctx context.Context, invocationHash, skipKey string) error {
	// Use secondary index for duplicate check
	iter, err := k.state.ProcessedNonces.Indexes.InvocationHash.MatchExact(ctx, invocationHash)
	if err != nil {
		return fmt.Errorf("failed to query invocation hash index: %w", err)
	}
	defer func() {
		_ = iter.Close()
	}()

	for ; iter.Valid(); iter.Next() {
		key, err := iter.PrimaryKey()
		if err != nil {
			return fmt.Errorf("failed to get primary key: %w", err)
		}

		// Skip the key we're about to insert
		if key == skipKey {
			continue
		}

		// Found a duplicate
		return fmt.Errorf(
			"duplicate invocation detected: equivalent request already processed (key: %s)",
			key,
		)
	}

	return nil
}

// CleanupExpiredNonces should be called in BeginBlocker to prevent state bloat
// Removes nonce records older than NonceTTL using a cursor for bounded iteration
func (k Keeper) CleanupExpiredNonces(ctx sdk.Context) error {
	cutoffTime := ctx.BlockTime().Add(-NonceTTL)
	cleaned := 0

	// Resume from last processed nonce
	startNonceKey, err := k.state.LastProcessedNonce.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}

	var rng collections.Ranger[string]
	if startNonceKey != "" {
		rng = new(collections.Range[string]).StartExclusive(startNonceKey)
	}

	iter, err := k.state.ProcessedNonces.Iterate(ctx, rng)
	if err != nil {
		return fmt.Errorf("failed to iterate nonces for cleanup: %w", err)
	}
	defer func() {
		_ = iter.Close()
	}()

	// Collect keys to delete (can't delete while iterating)
	var keysToDelete []string
	scanned := 0
	// Limit scanning to avoid O(N) DoS on large datasets
	const maxScanned = 1000
	var lastKey string

	for ; iter.Valid() && cleaned < MaxCleanupPerBlock && scanned < maxScanned; iter.Next() {
		scanned++
		record, err := iter.Value()
		if err != nil {
			return fmt.Errorf("failed to get nonce record: %w", err)
		}

		key, err := iter.Key()
		if err != nil {
			return fmt.Errorf("failed to get key: %w", err)
		}
		lastKey = key

		if record.FirstSeen.Before(cutoffTime) {
			keysToDelete = append(keysToDelete, key)
			cleaned++
		}
	}

	// If we hit the end of the collection, reset the cursor. Persist
	// (or clear) errors are logged rather than silently swallowed:
	// a silent Set failure would stall the nonce sweep on the same
	// cursor every block, never visiting nonces past the broken
	// position until the underlying store fault clears. Same bug
	// class as ce940f443 (x/incentives ProcessExpiredBadges).
	if !iter.Valid() {
		if err := k.state.LastProcessedNonce.Remove(ctx); err != nil {
			k.logger.Error("failed to clear nonce-sweep cursor after exhausting nonces — next sweep will restart from stale cursor",
				"error", err)
		}
	} else if lastKey != "" {
		if err := k.state.LastProcessedNonce.Set(ctx, lastKey); err != nil {
			k.logger.Error("failed to advance nonce-sweep cursor — sweep will re-process same nonces on next tick until store fault clears",
				"last_key", lastKey, "error", err)
		}
	}

	// Delete expired records
	for _, key := range keysToDelete {
		if err := k.state.ProcessedNonces.Remove(ctx, key); err != nil {
			k.logger.Error("failed to remove expired nonce", "key", key, "error", err)
			// Continue cleanup even if one delete fails
		}
	}

	if cleaned > 0 {
		k.logger.Debug("cleaned up expired nonces", "count", cleaned)
	}

	// Consume gas for cleanup work
	ctx.GasMeter().ConsumeGas(uint64(cleaned)*100, "router/nonce-cleanup")

	return nil
}

// GetNonceStats returns statistics about nonce records for monitoring
func (k Keeper) GetNonceStats(ctx context.Context) (map[string]interface{}, error) {
	totalCount := 0

	iter, err := k.state.ProcessedNonces.Iterate(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to iterate nonces: %w", err)
	}
	defer func() {
		_ = iter.Close()
	}()

	for ; iter.Valid(); iter.Next() {
		totalCount++
	}

	return map[string]interface{}{
		"total_nonces":          totalCount,
		"ttl_hours":             NonceTTL.Hours(),
		"max_timestamp_skew":    MaxTimestampSkew.String(),
		"max_cleanup_per_block": MaxCleanupPerBlock,
	}, nil
}

// CheckInvocationReplay validates an invocation request against replay protection
// This is the main entry point used by the Invoke handler
func (k Keeper) CheckInvocationReplay(ctx context.Context, req *types.InvokeRequest) error {
	return k.CheckAndRecordInvocation(ctx, req)
}

// CleanupReplayCache should be called in BeginBlocker to prevent state bloat
// This is an alias for CleanupExpiredNonces for backward compatibility
func (k Keeper) CleanupReplayCache(ctx sdk.Context) error {
	return k.CleanupExpiredNonces(ctx)
}
