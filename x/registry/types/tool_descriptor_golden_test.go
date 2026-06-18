
package types

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Golden artifact tests for x/registry tool descriptor JSON wire format.
//
// Why this matters
// ----------------
// ToolCard is the public published descriptor that every off-chain
// consumer uses to discover, evaluate, and invoke registered tools:
//   - Discovery agents querying the registry for capability matches
//   - Compliance auditors verifying licensing/jurisdiction metadata
//   - SIEM rules filtering invocations by verified_badge status
//   - DEX/marketplace UIs displaying pricing and SLO for buyers
//   - External search indexers (data pipelines building capability graphs)
//
// None of these consumers share protobuf schema files with Lumera at
// runtime — they parse ToolCard JSON field-by-field against documented
// names. A silent rename (e.g. `tool_id` → `toolID`) or omit-empty flip
// on a nested message (e.g. pricing.minimum_cost emitted when empty)
// would break every downstream consumer.
//
// Nested types that form the descriptor contract:
//   - Pricing        — how much the tool charges
//   - SLO            — latency/availability guarantees
//   - SandboxProfile — execution constraints
//   - CachePolicy    — cache + royalty terms
//   - GasProfile     — on-chain gas costs
//   - Endpoint       — where to reach the tool (protocol/url/region)
//
// This file freezes the JSON byte format for ToolCard and its nested
// types via protojson → encoding/json canonicalization.

// fixedRegistrySnapshot is a deterministic timestamp used across goldens.
var fixedRegistrySnapshot = time.Date(2026, 4, 20, 15, 0, 0, 0, time.UTC)

// marshalCanonicalRegistryJSON produces byte-comparable JSON by round-
// tripping protojson output through encoding/json (sorts map keys
// lexicographically, strips protojson's intentional whitespace non-
// determinism).
func marshalCanonicalRegistryJSON(t *testing.T, msg proto.Message) []byte {
	t.Helper()

	opts := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}
	raw, err := opts.Marshal(msg)
	require.NoError(t, err)

	var intermediate map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &intermediate))

	canonical, err := json.Marshal(intermediate)
	require.NoError(t, err)
	return canonical
}

func loadRegistryGolden(t *testing.T, filename string) []byte {
	t.Helper()
	path := filepath.Join("testdata", filename)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read golden file %s", path)
	return data
}

func assertRegistryGolden(t *testing.T, msg proto.Message, goldenFile string) {
	t.Helper()

	got := marshalCanonicalRegistryJSON(t, msg)
	want := loadRegistryGolden(t, goldenFile)

	var gotObj, wantObj interface{}
	require.NoError(t, json.Unmarshal(got, &gotObj))
	require.NoError(t, json.Unmarshal(want, &wantObj))

	require.Equal(t, wantObj, gotObj,
		"registry descriptor wire format drift in %s — a change here "+
			"breaks every external discovery/compliance/indexer consumer. "+
			"Review the diff carefully before regenerating the golden.",
		goldenFile)
}

// TestToolCard_WireFormat_FullFields pins the canonical ToolCard JSON
// with every field populated including all nested sub-messages. This is
// the comprehensive schema contract that indexers validate against.
func TestToolCard_WireFormat_FullFields(t *testing.T) {
	t.Parallel()

	card := fullToolCardFixture()
	assertRegistryGolden(t, card, "tool_card_full.golden.json")
}

// TestToolCard_WireFormat_Minimal pins the minimal ToolCard shape (just
// identity fields). Catches regressions where omit-empty semantics
// regress (previously-omitted optional fields start being serialized).
func TestToolCard_WireFormat_Minimal(t *testing.T) {
	t.Parallel()

	card := &ToolCard{
		ToolId:  "tool-minimal",
		Owner:   "lumera1owner",
		Version: "1.0.0",
	}
	assertRegistryGolden(t, card, "tool_card_minimal.golden.json")
}

// TestPricing_WireFormat pins the Pricing sub-descriptor. Buyers and
// marketplace UIs depend on these exact field names to display costs.
func TestPricing_WireFormat(t *testing.T) {
	t.Parallel()

	pricing := &Pricing{
		Model:         "per_call",
		Unit:          "request",
		PricePerUnit:  "0.001",
		MinimumCost:   "0.0005",
		MaximumCost:   "10.0",
		QuoteEndpoint: "https://quote.example.com/tool-001",
	}
	assertRegistryGolden(t, pricing, "pricing_full.golden.json")
}

// TestSLO_WireFormat pins the SLO sub-descriptor.
func TestSLO_WireFormat(t *testing.T) {
	t.Parallel()

	slo := &SLO{
		P95LatencyMs: 500,
		Availability: "99.95",
		ErrorRateBps: 50,
		TimeoutMs:    5000,
	}
	assertRegistryGolden(t, slo, "slo_full.golden.json")
}

// TestSandboxProfile_WireFormat pins the SandboxProfile descriptor.
// Compliance/security scanners parse this to verify execution constraints.
func TestSandboxProfile_WireFormat(t *testing.T) {
	t.Parallel()

	sandbox := &SandboxProfile{
		Profile:          "strict",
		EgressAllowlist:  []string{"api.example.com", "cdn.example.com"},
		MaxMemoryMb:      512,
		MaxCpuMillicores: 1000,
		MaxExecutionSec:  30,
		PiiHandling:      "encrypt_at_rest",
		RequiresEnclave:  true,
	}
	assertRegistryGolden(t, sandbox, "sandbox_profile_full.golden.json")
}

// TestCachePolicy_WireFormat pins the CachePolicy descriptor. Caching
// and royalty splits depend on these exact field names.
func TestCachePolicy_WireFormat(t *testing.T) {
	t.Parallel()

	cache := &CachePolicy{
		Enabled:         true,
		TtlSeconds:      3600,
		Deterministic:   true,
		RoyaltyShareBps: 2500,
		MaxSizeMb:       128,
	}
	assertRegistryGolden(t, cache, "cache_policy_full.golden.json")
}

// TestGasProfile_WireFormat pins the GasProfile descriptor. On-chain
// gas calculators parse this.
func TestGasProfile_WireFormat(t *testing.T) {
	t.Parallel()

	gas := &GasProfile{
		LockGas:             50000,
		SettleGas:           80000,
		InvocationGas:       30000,
		CacheHitDiscountBps: 7500,
	}
	assertRegistryGolden(t, gas, "gas_profile_full.golden.json")
}

// TestEndpoint_WireFormat pins the Endpoint descriptor. Routers and
// clients parse this to dispatch invocations.
func TestEndpoint_WireFormat(t *testing.T) {
	t.Parallel()

	ep := &Endpoint{
		Protocol: "mcp/1.0",
		Url:      "https://tool.example.com/mcp",
		Priority: 100,
		Region:   "us-east-1",
	}
	assertRegistryGolden(t, ep, "endpoint_full.golden.json")
}

// TestToolCard_WireContract_FieldNames pins the JSON field names on
// ToolCard. Every rename is a wire-format break.
func TestToolCard_WireContract_FieldNames(t *testing.T) {
	t.Parallel()

	card := fullToolCardFixture()
	raw := marshalCanonicalRegistryJSON(t, card)
	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &obj))

	// Every field on ToolCard that external consumers filter, sort, or
	// display by. Renaming any breaks documented client code.
	requiredFields := []string{
		"tool_id",
		"owner",
		"version",
		"categories",
		"tags",
		"license_lane",
		"jurisdictions",
		"pricing",
		"slo",
		"sandbox",
		"cache",
		"schema_hash",
		"sbom_hash",
		"attestation_root",
		"policy_tag",
		"registered_at",
		"updated_at",
		"endpoints",
		"mcp_protocols",
		"description",
		"metadata",
		"gas_profile",
		"owner_pubkey",
		"input_schema",
		"output_schema",
		"error_schema",
		"verified_badge",
	}
	for _, f := range requiredFields {
		require.Contains(t, obj, f,
			"ToolCard field %q missing from wire — rename breaks every "+
				"external discovery/compliance consumer that filters by this key", f)
	}
}

// TestToolCard_WireContract_OmitEmpty pins omit-empty behavior for all
// optional top-level ToolCard fields. Flipping omit-empty semantics
// would cause consumers that distinguish "not-set" from "empty" to drop
// valid entries or crash on unexpected nulls.
func TestToolCard_WireContract_OmitEmpty(t *testing.T) {
	t.Parallel()

	// Minimal card: only required identity fields populated.
	card := &ToolCard{
		ToolId:  "tool-omit",
		Owner:   "lumera1owner",
		Version: "1.0.0",
	}

	raw := marshalCanonicalRegistryJSON(t, card)
	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &obj))

	// These optional fields must be absent from the wire when empty.
	// A refactor that switches omitempty → always-emit breaks consumers
	// that distinguish "field absent" from "field set to empty value".
	omittedFields := []string{
		"categories",
		"tags",
		"license_lane",
		"jurisdictions",
		"pricing",
		"slo",
		"sandbox",
		"cache",
		"schema_hash",
		"sbom_hash",
		"attestation_root",
		"policy_tag",
		"registered_at",
		"updated_at",
		"endpoints",
		"mcp_protocols",
		"description",
		"metadata",
		"gas_profile",
		"owner_pubkey",
		"input_schema",
		"output_schema",
		"error_schema",
	}
	for _, f := range omittedFields {
		_, present := obj[f]
		require.False(t, present,
			"empty optional ToolCard field %q serialized when it should "+
				"be omitted — changing omit-empty semantics is a wire "+
				"format change that breaks external consumers", f)
	}

	// verified_badge has a zero-value enum default. protojson omits zero
	// enum values by default when EmitUnpopulated=false; pin that so a
	// flip to explicit emission would fail this test.
	_, badgePresent := obj["verified_badge"]
	require.False(t, badgePresent,
		"zero-valued verified_badge serialized when it should be omitted "+
			"— UNVERIFIED sentinel vs missing distinction would flip for "+
			"existing tools")
}

// TestPricing_WireContract_FieldNames pins Pricing field names.
func TestPricing_WireContract_FieldNames(t *testing.T) {
	t.Parallel()

	pricing := &Pricing{
		Model:         "per_call",
		Unit:          "request",
		PricePerUnit:  "0.001",
		MinimumCost:   "0.0005",
		MaximumCost:   "10.0",
		QuoteEndpoint: "https://quote.example.com/tool-001",
	}
	raw := marshalCanonicalRegistryJSON(t, pricing)
	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &obj))

	required := []string{"model", "unit", "price_per_unit", "minimum_cost", "maximum_cost", "quote_endpoint"}
	for _, f := range required {
		require.Contains(t, obj, f,
			"Pricing field %q missing from wire — rename breaks buyers "+
				"parsing cost fields", f)
	}
}

// TestSandboxProfile_WireContract_FieldNames pins SandboxProfile field
// names. Security scanners depend on these to enforce execution
// constraints.
func TestSandboxProfile_WireContract_FieldNames(t *testing.T) {
	t.Parallel()

	sandbox := &SandboxProfile{
		Profile:          "strict",
		EgressAllowlist:  []string{"a.example.com"},
		MaxMemoryMb:      512,
		MaxCpuMillicores: 1000,
		MaxExecutionSec:  30,
		PiiHandling:      "encrypt_at_rest",
		RequiresEnclave:  true,
	}
	raw := marshalCanonicalRegistryJSON(t, sandbox)
	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &obj))

	required := []string{
		"profile",
		"egress_allowlist",
		"max_memory_mb",
		"max_cpu_millicores",
		"max_execution_sec",
		"pii_handling",
		"requires_enclave",
	}
	for _, f := range required {
		require.Contains(t, obj, f,
			"SandboxProfile field %q missing from wire — rename breaks "+
				"security scanners enforcing execution constraints", f)
	}
}

// TestEndpoint_WireContract_FieldNames pins Endpoint field names.
func TestEndpoint_WireContract_FieldNames(t *testing.T) {
	t.Parallel()

	ep := &Endpoint{
		Protocol: "mcp/1.0",
		Url:      "https://tool.example.com/mcp",
		Priority: 100,
		Region:   "us-east-1",
	}
	raw := marshalCanonicalRegistryJSON(t, ep)
	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &obj))

	required := []string{"protocol", "url", "priority", "region"}
	for _, f := range required {
		require.Contains(t, obj, f,
			"Endpoint field %q missing from wire — rename breaks routers "+
				"dispatching invocations to URL/protocol", f)
	}
}

// TestToolCard_WireContract_BytesFieldsEncoding pins the base64 encoding
// of bytes fields (schema_hash, sbom_hash, attestation_root). protojson
// encodes bytes as base64; if that ever changes, external verifiers
// that decode hashes would break.
func TestToolCard_WireContract_BytesFieldsEncoding(t *testing.T) {
	t.Parallel()

	card := &ToolCard{
		ToolId:          "tool-bytes-test",
		Owner:           "lumera1owner",
		Version:         "1.0.0",
		SchemaHash:      []byte{0x01, 0x02, 0x03, 0x04},
		SbomHash:        []byte{0xaa, 0xbb, 0xcc, 0xdd},
		AttestationRoot: []byte{0xff, 0xee, 0xdd, 0xcc},
	}
	raw := marshalCanonicalRegistryJSON(t, card)
	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &obj))

	// All three bytes fields must encode as strings (not arrays, not
	// hex) — consumers decode them as base64 per protojson conventions.
	for _, field := range []string{"schema_hash", "sbom_hash", "attestation_root"} {
		v, ok := obj[field]
		require.True(t, ok, "bytes field %q missing", field)
		_, isString := v.(string)
		require.True(t, isString,
			"bytes field %q is not string-encoded (got %T) — protojson "+
				"base64 contract broken, external verifiers would fail to "+
				"decode hashes", field, v)
	}

	// Specifically: schema_hash {01,02,03,04} base64 = "AQIDBA==".
	require.Equal(t, "AQIDBA==", obj["schema_hash"],
		"schema_hash base64 encoding drifted — verifiers decoding with "+
			"standard base64 would get wrong bytes")
}

// TestToolCard_WireContract_TimestampFormat pins the timestamp
// serialization format (RFC3339 string, not proto-internal seconds+nanos
// object). Consumers parsing with standard time.Parse would fail if
// protojson switched to an object representation.
func TestToolCard_WireContract_TimestampFormat(t *testing.T) {
	t.Parallel()

	card := &ToolCard{
		ToolId:       "tool-ts-test",
		Owner:        "lumera1owner",
		Version:      "1.0.0",
		RegisteredAt: timestamppb.New(fixedRegistrySnapshot),
		UpdatedAt:    timestamppb.New(fixedRegistrySnapshot.Add(time.Hour)),
	}
	raw := marshalCanonicalRegistryJSON(t, card)
	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &obj))

	// Both timestamps must encode as ISO 8601 strings.
	registered, ok := obj["registered_at"].(string)
	require.True(t, ok, "registered_at not string-encoded")
	require.Equal(t, "2026-04-20T15:00:00Z", registered,
		"RFC3339 timestamp encoding drifted — time.Parse consumers would break")

	updated, ok := obj["updated_at"].(string)
	require.True(t, ok, "updated_at not string-encoded")
	require.Equal(t, "2026-04-20T16:00:00Z", updated)
}

// TestToolCard_WireContract_MetadataMap pins the metadata map encoding.
// protojson encodes map<string,string> as a JSON object; consumers
// depend on that shape.
func TestToolCard_WireContract_MetadataMap(t *testing.T) {
	t.Parallel()

	card := &ToolCard{
		ToolId:  "tool-meta-test",
		Owner:   "lumera1owner",
		Version: "1.0.0",
		Metadata: map[string]string{
			"region":  "us-east",
			"team":    "platform",
			"project": "lumera",
		},
	}
	raw := marshalCanonicalRegistryJSON(t, card)
	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &obj))

	meta, ok := obj["metadata"].(map[string]interface{})
	require.True(t, ok, "metadata must be a JSON object (not array/string)")
	require.Equal(t, "us-east", meta["region"])
	require.Equal(t, "platform", meta["team"])
	require.Equal(t, "lumera", meta["project"])
}

// TestGoldenFiles_AllPresent enforces coverage: every golden file
// referenced above must exist on disk.
func TestGoldenFiles_AllPresent(t *testing.T) {
	t.Parallel()

	required := []string{
		"tool_card_full.golden.json",
		"tool_card_minimal.golden.json",
		"pricing_full.golden.json",
		"slo_full.golden.json",
		"sandbox_profile_full.golden.json",
		"cache_policy_full.golden.json",
		"gas_profile_full.golden.json",
		"endpoint_full.golden.json",
	}
	for _, f := range required {
		path := filepath.Join("testdata", f)
		_, err := os.Stat(path)
		require.NoError(t, err,
			"required registry golden %s missing", f)
	}
}

// fullToolCardFixture returns a deterministic ToolCard populated on
// every public field. Shared between TestToolCard_WireFormat_FullFields
// and the field-name contract test so both pin the same canonical shape.
func fullToolCardFixture() *ToolCard {
	return &ToolCard{
		ToolId:        "tool-001",
		Owner:         "lumera1ownerpublisher",
		Version:       "2.1.0",
		Categories:    []string{"nlp", "translation"},
		Tags:          []string{"en", "es", "fr"},
		LicenseLane:   "commercial-v1",
		Jurisdictions: []string{"US", "EU"},
		Pricing: &Pricing{
			Model:         "per_call",
			Unit:          "request",
			PricePerUnit:  "0.001",
			MinimumCost:   "0.0005",
			MaximumCost:   "10.0",
			QuoteEndpoint: "https://quote.example.com/tool-001",
		},
		Slo: &SLO{
			P95LatencyMs: 500,
			Availability: "99.95",
			ErrorRateBps: 50,
			TimeoutMs:    5000,
		},
		Sandbox: &SandboxProfile{
			Profile:          "strict",
			EgressAllowlist:  []string{"api.example.com"},
			MaxMemoryMb:      512,
			MaxCpuMillicores: 1000,
			MaxExecutionSec:  30,
			PiiHandling:      "encrypt_at_rest",
			RequiresEnclave:  true,
		},
		Cache: &CachePolicy{
			Enabled:         true,
			TtlSeconds:      3600,
			Deterministic:   true,
			RoyaltyShareBps: 2500,
			MaxSizeMb:       128,
		},
		SchemaHash:      []byte{0x01, 0x02, 0x03, 0x04},
		SbomHash:        []byte{0xaa, 0xbb, 0xcc, 0xdd},
		AttestationRoot: []byte{0xff, 0xee, 0xdd, 0xcc},
		PolicyTag:       "default-v1",
		RegisteredAt:    timestamppb.New(fixedRegistrySnapshot),
		UpdatedAt:       timestamppb.New(fixedRegistrySnapshot.Add(time.Hour)),
		Endpoints: []*Endpoint{
			{
				Protocol: "mcp/1.0",
				Url:      "https://tool.example.com/mcp",
				Priority: 100,
				Region:   "us-east-1",
			},
			{
				Protocol: "http/1.1",
				Url:      "https://tool.example.com/api",
				Priority: 50,
				Region:   "eu-west-1",
			},
		},
		McpProtocols: []string{"mcp/1.0", "mcp/1.1"},
		Description:  "A translation tool supporting English, Spanish, French",
		Metadata: map[string]string{
			"region": "us-east",
			"team":   "platform",
		},
		GasProfile: &GasProfile{
			LockGas:             50000,
			SettleGas:           80000,
			InvocationGas:       30000,
			CacheHitDiscountBps: 7500,
		},
		OwnerPubkey:   "ed25519-AAAAC3NzaC1lZDI1NTE5AAAAIExample",
		InputSchema:   `{"type":"object","properties":{"text":{"type":"string"}}}`,
		OutputSchema:  `{"type":"object","properties":{"translated":{"type":"string"}}}`,
		ErrorSchema:   `{"type":"object","properties":{"code":{"type":"integer"}}}`,
		VerifiedBadge: ToolVerifiedBadge(2), // non-zero sentinel
	}
}
