package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cosmossdk.io/collections"
	"github.com/shopspring/decimal"

	"github.com/LumeraProtocol/lumera/x/router/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	defaultActivationReason   = "activation"
	defaultDeactivationReason = "deactivation"
)

// GetSession retrieves an existing session by ID, returning an error if not found.
func (k Keeper) GetSession(ctx context.Context, sessionID string) (*types.SessionState, error) {
	session, err := k.state.Sessions.Get(ctx, sessionID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, types.ErrSessionNotFound.Wrapf("session %s not found", sessionID)
		}
		return nil, fmt.Errorf("failed to load session: %w", err)
	}
	return k.refreshSession(ctx, session)
}

// GetOrCreateSession retrieves a session by ID, creating a new unpersisted session if not found.
func (k Keeper) GetOrCreateSession(ctx context.Context, sessionID string) (*types.SessionState, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	session, err := k.state.Sessions.Get(ctx, sessionID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return k.newSession(sdkCtx, sessionID, "", ""), nil
		}
		return nil, fmt.Errorf("failed to load session: %w", err)
	}
	return k.refreshSession(ctx, session)
}

// refreshSession handles TTL expiry, cooldown cleanup, and last-access updates for a loaded session.
func (k Keeper) refreshSession(ctx context.Context, session *types.SessionState) (*types.SessionState, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, err := k.paramsOrDefault(sdkCtx)
	if err != nil {
		return nil, err
	}
	sessionTTL := time.Duration(params.GetSessionTtlSeconds()) * time.Second

	// Determine whether the session has expired relative to TTL and block time.
	createdAt := session.CreatedAtTime()
	if sessionTTL > 0 && !createdAt.IsZero() {
		age := sdkCtx.BlockTime().Sub(createdAt)
		if age >= sessionTTL {
			// Purge the expired session before returning a fresh state to callers.
			if err := k.state.Sessions.Remove(ctx, session.GetSessionId()); err != nil && !errors.Is(err, collections.ErrNotFound) {
				return nil, fmt.Errorf("failed to remove expired session: %w", err)
			}
			fresh := k.newSession(sdkCtx, session.GetSessionId(), session.UserAddr, session.PolicyVersion)
			if err := k.SaveSession(ctx, fresh); err != nil {
				return nil, err
			}
			return fresh, nil
		}
	}

	// Update last accessed time
	session.SetLastAccessedAt(sdkCtx.BlockTime())
	session.EnsureMaps()

	// Drop any lingering cooldown entries that extend past the session TTL boundary.
	// Collect keys first, then delete — avoids mutating the map during iteration
	// and ensures deterministic behavior across validators.
	//
	// Also guard on !createdAt.IsZero(), mirroring the outer TTL-expiry
	// check (line ~68). Without that guard, a session record with a
	// missing/unset CreatedAt timestamp (legacy record, partial proto
	// deserialization) computes expiryCutoff = zero.Add(sessionTTL) ≈
	// year-1 + TTL, so every cooldown timestamp (any non-zero real
	// time) is After that cutoff and ALL cooldowns get silently
	// wiped on first session access. Same zero-timestamp-comparison
	// bug class as TimeWithinWindow (e82ddb06a) and
	// SLOEvidenceWindow.ContainsCompletedAt (54f51e0ca).
	if sessionTTL > 0 && !createdAt.IsZero() && len(session.CooldownUntil) > 0 {
		expiryCutoff := createdAt.Add(sessionTTL)
		var expiredCooldowns []string
		for toolID := range session.CooldownUntil {
			until, ok := session.CooldownUntilTime(toolID)
			if ok && until.After(expiryCutoff) {
				expiredCooldowns = append(expiredCooldowns, toolID)
			}
		}
		for _, toolID := range expiredCooldowns {
			session.DeleteCooldown(toolID)
		}
	}

	return session, nil
}

// SaveSession persists a session to storage
func (k Keeper) SaveSession(ctx context.Context, session *types.SessionState) error {
	if session == nil {
		return types.ErrInvalidParams.Wrap("session is nil")
	}
	if err := k.state.Sessions.Set(ctx, session.GetSessionId(), session); err != nil {
		return fmt.Errorf("failed to persist session: %w", err)
	}
	return nil
}

func (k Keeper) newSession(ctx sdk.Context, sessionID, userAddr, policyVersion string) *types.SessionState {
	return types.NewSessionState(sessionID, userAddr, policyVersion, ctx.BlockTime())
}

// AddToolToActiveSet adds a tool to the session's active set
func (k Keeper) AddToolToActiveSet(ctx context.Context, sessionID, toolID string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	trimmedSession := strings.TrimSpace(sessionID)
	trimmedTool := strings.TrimSpace(toolID)
	if trimmedSession == "" || trimmedTool == "" {
		return types.ErrInvalidParams.Wrap("session and tool ids must be non-empty")
	}

	session, err := k.GetOrCreateSession(ctx, trimmedSession)
	if err != nil {
		return err
	}

	params, err := k.paramsOrDefault(sdkCtx)
	if err != nil {
		return err
	}

	limit := int(params.GetActiveSetLimit())
	if limit <= 0 {
		limit = int(types.DefaultParams().GetActiveSetLimit())
	}

	// If already active, refresh timestamp and persist
	for _, activeToolID := range session.ActiveTools {
		if activeToolID == trimmedTool {
			session.SetActivatedAtTime(trimmedTool, sdkCtx.BlockTime())
			return k.SaveSession(ctx, session)
		}
	}

	if cooldownUntil, exists := session.CooldownUntilTime(trimmedTool); exists {
		if sdkCtx.BlockTime().Before(cooldownUntil) {
			return types.ErrCooldownActive.Wrapf("tool %s is in cooldown until %s", trimmedTool, cooldownUntil.UTC().Format(time.RFC3339))
		}
		session.DeleteCooldown(trimmedTool)
	}

	if limit > 0 && len(session.ActiveTools) >= limit {
		return types.ErrActiveSetFull.Wrapf("active set limit %d reached", limit)
	}

	if _, exists := k.registryKeeper.GetToolCard(sdkCtx, trimmedTool); !exists {
		return types.ErrToolNotFound.Wrap(trimmedTool)
	}

	minRep := params.MinReputationScoreDecimal()
	if minRep.GreaterThan(decimal.Zero) {
		score, scoreErr := k.state.SelectionScores.Get(sdkCtx, trimmedTool)
		switch {
		case scoreErr == nil:
			reputation := score.ReputationScoreDecimal()
			if reputation.LessThan(minRep) {
				return types.ErrInvalidScore.Wrapf("reputation %s below minimum %s", reputation.String(), minRep.String())
			}
		case errors.Is(scoreErr, collections.ErrNotFound):
			bootstrap := types.NewToolSelectionScore(trimmedTool)
			bootstrap.SetReputationScoreDecimal(minRep)
			bootstrap.SetPerformanceScoreDecimal(decimal.NewFromInt(50))
			bootstrap.SetReliabilityScoreDecimal(decimal.NewFromInt(50))
			bootstrap.SetCostEfficiencyScoreDecimal(decimal.NewFromInt(50))
			bootstrap.SetOverallScoreDecimal(minRep)
			bootstrap.SetLastCalculated(sdkCtx.BlockTime())
			if err := k.state.SelectionScores.Set(sdkCtx, trimmedTool, bootstrap); err != nil {
				return fmt.Errorf("bootstrap selection score: %w", err)
			}
			k.Logger(sdkCtx).Info(
				"bootstrap selection score for tool",
				"tool", trimmedTool,
				"min_reputation", minRep.String(),
			)
		default:
			return scoreErr
		}
	}

	session.ActiveTools = append(session.ActiveTools, trimmedTool)
	session.SetActivatedAtTime(trimmedTool, sdkCtx.BlockTime())

	if err := k.SaveSession(ctx, session); err != nil {
		return err
	}

	if err := k.RecordActivation(ctx, trimmedTool, trimmedSession, true, defaultActivationReason); err != nil && !errors.Is(err, types.ErrMetricsDisabled) {
		return err
	}

	return nil
}

// RemoveToolFromActiveSet removes a tool from the session's active set
func (k Keeper) RemoveToolFromActiveSet(ctx context.Context, sessionID, toolID string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	trimmedSession := strings.TrimSpace(sessionID)
	trimmedTool := strings.TrimSpace(toolID)
	if trimmedSession == "" || trimmedTool == "" {
		return types.ErrInvalidParams.Wrap("session and tool ids must be non-empty")
	}

	session, err := k.GetSession(ctx, trimmedSession)
	if err != nil {
		return err
	}

	params, err := k.paramsOrDefault(sdkCtx)
	if err != nil {
		return err
	}

	index := -1
	for i, activeToolID := range session.ActiveTools {
		if activeToolID == trimmedTool {
			index = i
			break
		}
	}

	if index == -1 {
		return types.ErrToolNotActive.Wrapf("tool %s not in active set", trimmedTool)
	}

	// Remove from active set
	session.ActiveTools = append(session.ActiveTools[:index], session.ActiveTools[index+1:]...)

	now := sdkCtx.BlockTime()
	session.SetDeactivatedAtTime(trimmedTool, now)

	cooldown := time.Duration(params.GetCooldownSeconds()) * time.Second
	sessionTTL := time.Duration(params.GetSessionTtlSeconds()) * time.Second
	if sessionTTL > 0 && cooldown > sessionTTL {
		cooldown = sessionTTL
	}
	if cooldown > 0 {
		expires := now.Add(cooldown)
		createdAt := session.CreatedAtTime()
		if sessionTTL > 0 && !createdAt.IsZero() {
			sessionExpiry := createdAt.Add(sessionTTL)
			if expires.After(sessionExpiry) {
				expires = sessionExpiry
			}
		}
		session.SetCooldownUntilTime(trimmedTool, expires)
	} else {
		session.DeleteCooldown(trimmedTool)
	}

	if err := k.SaveSession(ctx, session); err != nil {
		return err
	}

	if err := k.RecordActivation(ctx, trimmedTool, trimmedSession, false, defaultDeactivationReason); err != nil && !errors.Is(err, types.ErrMetricsDisabled) {
		return err
	}

	return nil
}

// GetActiveTools returns the list of active tools for a session
func (k Keeper) GetActiveTools(ctx context.Context, sessionID string) ([]string, error) {
	session, err := k.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return session.ActiveTools, nil
}

// CleanupExpiredSessions removes sessions that haven't been accessed in 24 hours
func (k Keeper) CleanupExpiredSessions(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, err := k.paramsOrDefault(sdkCtx)
	if err != nil {
		return err
	}
	sessionTTL := time.Duration(params.GetSessionTtlSeconds()) * time.Second
	if sessionTTL <= 0 {
		sessionTTL = 24 * time.Hour
	}
	expirationTime := sdkCtx.BlockTime().Add(-sessionTTL)
	keysToRemove := []string{}

	err = k.state.Sessions.Walk(ctx, nil, func(key string, session *types.SessionState) (bool, error) {
		if session.LastAccessedAtTime().Before(expirationTime) {
			keysToRemove = append(keysToRemove, key)
		}
		return false, nil
	})
	if err != nil {
		return err
	}

	// Delete expired sessions
	for _, key := range keysToRemove {
		if err := k.state.Sessions.Remove(ctx, key); err != nil && !errors.Is(err, collections.ErrNotFound) {
			k.Logger(sdkCtx).Error("failed to delete expired session", "session", key, "error", err)
		}
	}

	return nil
}
