//go:build cosmos

package types

import (
	"strings"
	"time"
)

// ToolCardHasVerifiedBadge returns true when a tool card carries the canonical verified marker.
func ToolCardHasVerifiedBadge(tool *ToolCard) bool {
	return ToolCardHasVerifiedBadgeAtTime(tool, time.Now().UTC())
}

// ToolCardHasVerifiedBadgeAtTime returns true when a tool card is currently verified.
func ToolCardHasVerifiedBadgeAtTime(tool *ToolCard, now time.Time) bool {
	if tool == nil {
		return false
	}
	switch tool.GetVerifiedBadge() {
	case ToolVerifiedBadge_TOOL_VERIFIED_BADGE_BRONZE,
		ToolVerifiedBadge_TOOL_VERIFIED_BADGE_SILVER,
		ToolVerifiedBadge_TOOL_VERIFIED_BADGE_GOLD:
		return true
	}
	if ToolCardHasActiveBadgeGraceAtTime(tool, now) {
		return true
	}
	if !toolCardVerificationMigrated(tool) {
		if tool.Metadata != nil {
			if val, ok := tool.Metadata["verified"]; ok && strings.EqualFold(strings.TrimSpace(val), "true") {
				return true
			}
		}
		for _, tag := range tool.Tags {
			if strings.EqualFold(strings.TrimSpace(tag), "verified") {
				return true
			}
		}
	}
	return false
}

// ToolCardVerifiedBadgeTier returns the canonical badge tier label for a tool card.
func ToolCardVerifiedBadgeTier(tool *ToolCard) string {
	return ToolCardVerifiedBadgeTierAtTime(tool, time.Now().UTC())
}

// ToolCardVerifiedBadgeTierAtTime returns the effective badge tier label for a tool card.
func ToolCardVerifiedBadgeTierAtTime(tool *ToolCard, now time.Time) string {
	if tool == nil {
		return ""
	}
	switch tool.GetVerifiedBadge() {
	case ToolVerifiedBadge_TOOL_VERIFIED_BADGE_BRONZE:
		return "bronze"
	case ToolVerifiedBadge_TOOL_VERIFIED_BADGE_SILVER:
		return "silver"
	case ToolVerifiedBadge_TOOL_VERIFIED_BADGE_GOLD:
		return "gold"
	default:
		if tier, ok := toolCardGraceTierAtTime(tool, now); ok {
			return tier
		}
		if !toolCardVerificationMigrated(tool) && legacyVerifiedMarker(tool) {
			return "bronze"
		}
		return ""
	}
}

// ToolCardHasActiveBadgeGraceAtTime reports whether a migrated tool is still within its badge grace window.
func ToolCardHasActiveBadgeGraceAtTime(tool *ToolCard, now time.Time) bool {
	_, ok := toolCardGraceTierAtTime(tool, now)
	return ok
}

func toolCardGraceTierAtTime(tool *ToolCard, now time.Time) (string, bool) {
	if tool == nil || tool.Metadata == nil {
		return "", false
	}
	rawDeadline, ok := tool.Metadata[MetadataKeyVerifiedBadgeGraceExpires]
	if !ok {
		return "", false
	}
	deadline, err := time.Parse(time.RFC3339, strings.TrimSpace(rawDeadline))
	if err != nil || now.After(deadline) {
		return "", false
	}
	tier := strings.ToLower(strings.TrimSpace(tool.Metadata[MetadataKeyVerifiedBadgeGraceTier]))
	switch tier {
	case "", "bronze":
		return "bronze", true
	case "silver":
		return "silver", true
	case "gold":
		return "gold", true
	default:
		return "", false
	}
}

func toolCardVerificationMigrated(tool *ToolCard) bool {
	if tool == nil || tool.Metadata == nil {
		return false
	}
	_, ok := tool.Metadata[MetadataKeyVerifiedBadgeMigratedAt]
	return ok
}

func legacyVerifiedMarker(tool *ToolCard) bool {
	if tool == nil {
		return false
	}
	if tool.Metadata != nil {
		if val, ok := tool.Metadata["verified"]; ok && strings.EqualFold(strings.TrimSpace(val), "true") {
			return true
		}
	}
	for _, tag := range tool.Tags {
		if strings.EqualFold(strings.TrimSpace(tag), "verified") {
			return true
		}
	}
	return false
}
