//go:build cosmos && generate_goldens

package types

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// TestGenerateRegistryGoldens is gated by the generate_goldens build tag.
// Run with: go test -tags='cosmos generate_goldens' -run TestGenerateRegistryGoldens ./x/registry/types/
func TestGenerateRegistryGoldens(t *testing.T) {
	emit := func(filename string, msg proto.Message) {
		opts := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false}
		raw, err := opts.Marshal(msg)
		require.NoError(t, err)
		var intermediate map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &intermediate))
		canonical, err := json.Marshal(intermediate)
		require.NoError(t, err)
		path := filepath.Join("testdata", filename)
		require.NoError(t, os.WriteFile(path, canonical, 0644))
		t.Logf("wrote %s (%d bytes)", path, len(canonical))
	}

	emit("tool_card_full.golden.json", fullToolCardFixture())

	emit("tool_card_minimal.golden.json", &ToolCard{
		ToolId:  "tool-minimal",
		Owner:   "lumera1owner",
		Version: "1.0.0",
	})

	emit("pricing_full.golden.json", &Pricing{
		Model:         "per_call",
		Unit:          "request",
		PricePerUnit:  "0.001",
		MinimumCost:   "0.0005",
		MaximumCost:   "10.0",
		QuoteEndpoint: "https://quote.example.com/tool-001",
	})

	emit("slo_full.golden.json", &SLO{
		P95LatencyMs: 500,
		Availability: "99.95",
		ErrorRateBps: 50,
		TimeoutMs:    5000,
	})

	emit("sandbox_profile_full.golden.json", &SandboxProfile{
		Profile:          "strict",
		EgressAllowlist:  []string{"api.example.com", "cdn.example.com"},
		MaxMemoryMb:      512,
		MaxCpuMillicores: 1000,
		MaxExecutionSec:  30,
		PiiHandling:      "encrypt_at_rest",
		RequiresEnclave:  true,
	})

	emit("cache_policy_full.golden.json", &CachePolicy{
		Enabled:         true,
		TtlSeconds:      3600,
		Deterministic:   true,
		RoyaltyShareBps: 2500,
		MaxSizeMb:       128,
	})

	emit("gas_profile_full.golden.json", &GasProfile{
		LockGas:             50000,
		SettleGas:           80000,
		InvocationGas:       30000,
		CacheHitDiscountBps: 7500,
	})

	emit("endpoint_full.golden.json", &Endpoint{
		Protocol: "mcp/1.0",
		Url:      "https://tool.example.com/mcp",
		Priority: 100,
		Region:   "us-east-1",
	})
}
