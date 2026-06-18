
package types

import "time"

// GlobalMetrics represents aggregate metrics across all tools
type GlobalMetrics struct {
	TotalTools        int64     `json:"total_tools"`
	TotalInvocations  int64     `json:"total_invocations"`
	TotalSuccesses    int64     `json:"total_successes"`
	TotalFailures     int64     `json:"total_failures"`
	TotalDisputes     int64     `json:"total_disputes"`
	TotalCacheHits    int64     `json:"total_cache_hits"`
	TotalRevenue      string    `json:"total_revenue"`
	TotalCacheRoyalty string    `json:"total_cache_royalty"`
	LastUpdated       time.Time `json:"last_updated"`
}

