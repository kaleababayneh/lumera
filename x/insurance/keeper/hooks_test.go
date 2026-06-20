
package keeper_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	insurancekeeper "github.com/LumeraProtocol/lumera/x/insurance/keeper"
	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// TestPublishAnomaly_Critical tests critical severity anomaly handling
func TestPublishAnomaly_Critical(t *testing.T) {
	f := setupKeeperTest(t)
	ctx := f.ctx
	var goCtx context.Context = ctx

	hooks := insurancekeeper.NewHooks(f.keeper)

	// Publish critical anomaly
	report := insurancekeeper.AnomalyReport{
		Severity:      insurancekeeper.SeverityCritical,
		PublisherID:   "publisher-123",
		ToolID:        "tool-456",
		Description:   "Enclave attestation failure detected",
		ReportedBy:    "governance",
		AutoRemediate: true,
	}

	err := hooks.PublishAnomaly(goCtx, report)
	require.NoError(t, err)

	// Verify risk profile was created and updated
	risk, err := hooks.GetPublisherRisk(goCtx, "publisher-123", "tool-456")
	require.NoError(t, err)
	require.NotNil(t, risk)
	require.Equal(t, types.PremiumTier_PREMIUM_TIER_EXTREME, risk.PremiumTier)
	require.Equal(t, "5.0", risk.PremiumMultiplier)
	require.Equal(t, uint32(1), risk.DisputeCount)

	// Verify event was emitted
	events := ctx.EventManager().Events()
	found := false
	for _, ev := range events {
		if ev.Type == "insurance_anomaly_published" {
			found = true
			for _, attr := range ev.Attributes {
				if string(attr.Key) == "severity" {
					require.Equal(t, string(insurancekeeper.SeverityCritical), string(attr.Value))
				}
			}
		}
	}
	require.True(t, found, "anomaly event should be emitted")
}

// TestPublishAnomaly_High tests high severity anomaly handling
func TestPublishAnomaly_High(t *testing.T) {
	f := setupKeeperTest(t)
	ctx := f.ctx
	var goCtx context.Context = ctx

	hooks := insurancekeeper.NewHooks(f.keeper)

	// Publish high severity anomaly
	report := insurancekeeper.AnomalyReport{
		Severity:    insurancekeeper.SeverityHigh,
		PublisherID: "publisher-789",
		ToolID:      "tool-012",
		Description: "Repeated SLO breaches detected",
		ReportedBy:  "validator-node-1",
	}

	err := hooks.PublishAnomaly(goCtx, report)
	require.NoError(t, err)

	// Verify risk profile
	risk, err := hooks.GetPublisherRisk(goCtx, "publisher-789", "tool-012")
	require.NoError(t, err)
	require.Equal(t, types.PremiumTier_PREMIUM_TIER_HIGH, risk.PremiumTier)
	require.Equal(t, "2.5", risk.PremiumMultiplier)
	require.Equal(t, uint32(1), risk.DisputeCount)
}

// TestPublishAnomaly_Medium tests medium severity anomaly handling
func TestPublishAnomaly_Medium(t *testing.T) {
	f := setupKeeperTest(t)
	ctx := f.ctx
	var goCtx context.Context = ctx

	hooks := insurancekeeper.NewHooks(f.keeper)

	// Publish medium severity anomaly
	report := insurancekeeper.AnomalyReport{
		Severity:    insurancekeeper.SeverityMedium,
		PublisherID: "publisher-medium",
		ToolID:      "tool-medium",
		Description: "Elevated latency detected",
		ReportedBy:  "monitoring-system",
	}

	err := hooks.PublishAnomaly(goCtx, report)
	require.NoError(t, err)

	// Verify risk profile
	risk, err := hooks.GetPublisherRisk(goCtx, "publisher-medium", "tool-medium")
	require.NoError(t, err)
	require.Equal(t, types.PremiumTier_PREMIUM_TIER_STANDARD, risk.PremiumTier)
	require.Equal(t, "1.5", risk.PremiumMultiplier)
}

// TestPublishAnomaly_Low tests low severity anomaly handling
func TestPublishAnomaly_Low(t *testing.T) {
	f := setupKeeperTest(t)
	ctx := f.ctx
	var goCtx context.Context = ctx

	hooks := insurancekeeper.NewHooks(f.keeper)

	// Publish low severity anomaly
	report := insurancekeeper.AnomalyReport{
		Severity:    insurancekeeper.SeverityLow,
		PublisherID: "publisher-low",
		ToolID:      "tool-low",
		Description: "Minor policy violation - outdated SBOM",
		ReportedBy:  "compliance-checker",
	}

	err := hooks.PublishAnomaly(goCtx, report)
	require.NoError(t, err)

	// Verify risk profile (should not change tier for low severity)
	risk, err := hooks.GetPublisherRisk(goCtx, "publisher-low", "tool-low")
	require.NoError(t, err)
	require.Equal(t, uint32(1), risk.DisputeCount)
}

// TestPublishAnomaly_InvalidInput tests error handling for invalid inputs
func TestPublishAnomaly_InvalidInput(t *testing.T) {
	f := setupKeeperTest(t)
	ctx := f.ctx
	var goCtx context.Context = ctx

	hooks := insurancekeeper.NewHooks(f.keeper)

	tests := []struct {
		name    string
		report  insurancekeeper.AnomalyReport
		wantErr bool
	}{
		{
			name: "missing publisher ID",
			report: insurancekeeper.AnomalyReport{
				Severity:    insurancekeeper.SeverityHigh,
				ToolID:      "tool-123",
				Description: "Test",
				ReportedBy:  "test",
			},
			wantErr: true,
		},
		{
			name: "missing tool ID",
			report: insurancekeeper.AnomalyReport{
				Severity:    insurancekeeper.SeverityHigh,
				PublisherID: "publisher-123",
				Description: "Test",
				ReportedBy:  "test",
			},
			wantErr: true,
		},
		{
			name: "missing description",
			report: insurancekeeper.AnomalyReport{
				Severity:    insurancekeeper.SeverityHigh,
				PublisherID: "publisher-123",
				ToolID:      "tool-123",
				ReportedBy:  "test",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := hooks.PublishAnomaly(goCtx, tt.report)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestReportSecurityAnomaly tests the security anomaly convenience method
func TestReportSecurityAnomaly(t *testing.T) {
	f := setupKeeperTest(t)
	ctx := f.ctx
	var goCtx context.Context = ctx

	hooks := insurancekeeper.NewHooks(f.keeper)

	tests := []struct {
		name             string
		threatType       string
		expectedSeverity insurancekeeper.AnomalySeverity
		expectedTier     types.PremiumTier
	}{
		{
			name:             "enclave attestation failure",
			threatType:       "enclave_attestation_failure",
			expectedSeverity: insurancekeeper.SeverityCritical,
			expectedTier:     types.PremiumTier_PREMIUM_TIER_EXTREME,
		},
		{
			name:             "SLO breach",
			threatType:       "slo_breach",
			expectedSeverity: insurancekeeper.SeverityHigh,
			expectedTier:     types.PremiumTier_PREMIUM_TIER_HIGH,
		},
		{
			name:             "elevated latency",
			threatType:       "elevated_latency",
			expectedSeverity: insurancekeeper.SeverityMedium,
			expectedTier:     types.PremiumTier_PREMIUM_TIER_STANDARD,
		},
		{
			name:             "outdated SBOM",
			threatType:       "outdated_sbom",
			expectedSeverity: insurancekeeper.SeverityLow,
			expectedTier:     types.PremiumTier_PREMIUM_TIER_STANDARD,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publisherID := "publisher-" + tt.name
			toolID := "tool-" + tt.name

			err := hooks.ReportSecurityAnomaly(
				goCtx,
				publisherID,
				toolID,
				tt.threatType,
				"Test security anomaly",
				"security-monitor",
			)
			require.NoError(t, err)

			// Verify risk profile
			risk, err := hooks.GetPublisherRisk(goCtx, publisherID, toolID)
			require.NoError(t, err)
			require.Equal(t, tt.expectedTier, risk.PremiumTier)
		})
	}
}

// TestAdjustPremiumTier tests manual premium tier adjustment
func TestAdjustPremiumTier(t *testing.T) {
	f := setupKeeperTest(t)
	ctx := f.ctx
	var goCtx context.Context = ctx

	hooks := insurancekeeper.NewHooks(f.keeper)

	// Adjust premium tier
	err := hooks.AdjustPremiumTier(
		goCtx,
		"publisher-adjust",
		"tool-adjust",
		types.PremiumTier_PREMIUM_TIER_HIGH,
		"3.0",
		"Manual adjustment due to governance decision",
	)
	require.NoError(t, err)

	// Verify adjustment
	risk, err := hooks.GetPublisherRisk(goCtx, "publisher-adjust", "tool-adjust")
	require.NoError(t, err)
	require.Equal(t, types.PremiumTier_PREMIUM_TIER_HIGH, risk.PremiumTier)
	require.Equal(t, "3.0", risk.PremiumMultiplier)

	// Verify event was emitted
	events := ctx.EventManager().Events()
	found := false
	for _, ev := range events {
		if ev.Type == "insurance_premium_adjusted" {
			found = true
		}
	}
	require.True(t, found, "premium adjusted event should be emitted")
}

// TestTriggerAutoRefunds tests automatic refund triggering for critical anomalies
func TestTriggerAutoRefunds(t *testing.T) {
	f := setupKeeperTest(t)
	ctx := f.ctx
	var goCtx context.Context = ctx

	hooks := insurancekeeper.NewHooks(f.keeper)

	// Create a pending claim for the publisher/tool
	// (This would require creating a claim first, which is tested elsewhere)
	// For now, just verify the function executes without error
	err := hooks.PublishAnomaly(goCtx, insurancekeeper.AnomalyReport{
		Severity:      insurancekeeper.SeverityCritical,
		PublisherID:   "publisher-refund",
		ToolID:        "tool-refund",
		Description:   "Critical failure requiring auto-refunds",
		ReportedBy:    "governance",
		AutoRemediate: true,
	})
	require.NoError(t, err)

	// Verify risk profile was updated
	risk, err := hooks.GetPublisherRisk(goCtx, "publisher-refund", "tool-refund")
	require.NoError(t, err)
	require.Equal(t, types.PremiumTier_PREMIUM_TIER_EXTREME, risk.PremiumTier)
}

// TestMultipleAnomalies tests cumulative effect of multiple anomalies
func TestMultipleAnomalies(t *testing.T) {
	f := setupKeeperTest(t)
	ctx := f.ctx
	var goCtx context.Context = ctx

	hooks := insurancekeeper.NewHooks(f.keeper)

	publisherID := "publisher-multi"
	toolID := "tool-multi"

	// Report low severity anomaly first
	err := hooks.PublishAnomaly(goCtx, insurancekeeper.AnomalyReport{
		Severity:    insurancekeeper.SeverityLow,
		PublisherID: publisherID,
		ToolID:      toolID,
		Description: "First anomaly",
		ReportedBy:  "monitor",
	})
	require.NoError(t, err)

	risk1, err := hooks.GetPublisherRisk(goCtx, publisherID, toolID)
	require.NoError(t, err)
	require.Equal(t, uint32(1), risk1.DisputeCount)

	// Report medium severity anomaly
	err = hooks.PublishAnomaly(goCtx, insurancekeeper.AnomalyReport{
		Severity:    insurancekeeper.SeverityMedium,
		PublisherID: publisherID,
		ToolID:      toolID,
		Description: "Second anomaly",
		ReportedBy:  "monitor",
	})
	require.NoError(t, err)

	risk2, err := hooks.GetPublisherRisk(goCtx, publisherID, toolID)
	require.NoError(t, err)
	require.Equal(t, uint32(2), risk2.DisputeCount)
	require.Equal(t, types.PremiumTier_PREMIUM_TIER_STANDARD, risk2.PremiumTier)

	// Report high severity anomaly
	err = hooks.PublishAnomaly(goCtx, insurancekeeper.AnomalyReport{
		Severity:    insurancekeeper.SeverityHigh,
		PublisherID: publisherID,
		ToolID:      toolID,
		Description: "Third anomaly",
		ReportedBy:  "monitor",
	})
	require.NoError(t, err)

	risk3, err := hooks.GetPublisherRisk(goCtx, publisherID, toolID)
	require.NoError(t, err)
	require.Equal(t, uint32(3), risk3.DisputeCount)
	require.Equal(t, types.PremiumTier_PREMIUM_TIER_HIGH, risk3.PremiumTier)
}