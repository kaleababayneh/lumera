
package types

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file closes DIRECT-test coverage for x/credits/types/
// codec.go. The RegisterLegacyAminoCodec and RegisterInterfaces
// functions had ZERO direct tests prior despite carrying WIRE-
// FACING contract invariants: the amino names in
// RegisterConcrete calls ("lumera/credits/MsgLockCredits", etc.)
// are the on-wire message-type identifiers that every external
// client (wallet, CLI, SDK bindings) matches against.
//
// Scan-angle #6 (security-critical invariants tested only at
// happy path). The amino names are chain-fork-event signals
// just like the keys.go prefix bytes: a silent rename would
// invalidate every pre-existing signed transaction at upgrade
// (transactions carry the amino type-URL in their signing
// envelope).
//
// Scan-angle #5 (sibling-pattern pinning with structural
// invariants):
//   - 6 messages registered on legacy amino: Swap LUME↔LAC,
//     Lock, Unlock, Settle, SettleOverdraft. MsgUpdateParams is NOT on legacy
//     amino (governance messages use Msg interface only).
//   - 7 messages AND 7 responses registered on the interface
//     registry (all Tx messages including governance).
//   - A refactor that added a new Msg to the Tx service MUST
//     register it in BOTH places (amino + interface) OR
//     intentionally omit from amino with a documented reason.

// TestRegisterLegacyAminoCodec_AllTxMessagesRegistered pins
// the six legacy-amino registrations. Each Msg name is a
// wire-facing string.
func TestRegisterLegacyAminoCodec_AllTxMessagesRegistered(t *testing.T) {
	t.Parallel()
	cdc := codec.NewLegacyAmino()
	RegisterLegacyAminoCodec(cdc)

	// For each registered msg type, MarshalJSON must succeed
	// AND produce a JSON object containing the amino type URL.
	// This proves the concrete registration actually landed.
	cases := []struct {
		name      string
		msg       sdk.Msg
		wantAmino string
	}{
		{"MsgLockCredits", &MsgLockCredits{}, "lumera/credits/MsgLockCredits"},
		{"MsgSettleCredits", &MsgSettleCredits{}, "lumera/credits/MsgSettleCredits"},
		{"MsgSettleOverdraft", &MsgSettleOverdraft{}, "lumera/credits/MsgSettleOverdraft"},
		{"MsgUnlockCredits", &MsgUnlockCredits{}, "lumera/credits/MsgUnlockCredits"},
		{"MsgSwapLUMEtoLAC", &MsgSwapLUMEtoLAC{}, "lumera/credits/MsgSwapLUMEtoLAC"},
		{"MsgSwapLACtoLUME", &MsgSwapLACtoLUME{}, "lumera/credits/MsgSwapLACtoLUME"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			// Marshal should succeed after concrete registration.
			bz, err := cdc.MarshalJSON(c.msg)
			require.NoError(t, err,
				"%s MarshalJSON requires concrete registration. "+
					"Pins that RegisterLegacyAminoCodec called "+
					"cdc.RegisterConcrete(&%s{}, %q, nil).",
				c.name, c.name, c.wantAmino)

			// The output must contain the amino type URL. A silent
			// rename would produce a marshalled payload with a
			// DIFFERENT URL, breaking every external signer that
			// matches on the type-URL.
			assert.Contains(t, string(bz), c.wantAmino,
				"%s amino output must contain %q. Pins the wire-"+
					"facing amino type-URL: a refactor renaming this "+
					"string would invalidate every pre-existing signed "+
					"transaction at upgrade time — clients match on "+
					"this exact string to route signing.",
				c.name, c.wantAmino)
		})
	}
}

// TestRegisterLegacyAminoCodec_AminoNamesFollowLumeraConvention
// pins the 'lumera/credits/<TypeName>' prefix convention. A
// refactor that used a different prefix (e.g., 'cosmos/credits/')
// would break wallet integrations that filter by prefix.
func TestRegisterLegacyAminoCodec_AminoNameConventionPinned(t *testing.T) {
	t.Parallel()
	cdc := codec.NewLegacyAmino()
	RegisterLegacyAminoCodec(cdc)

	for _, tc := range []struct {
		name string
		msg  sdk.Msg
	}{
		{"MsgLockCredits", &MsgLockCredits{}},
		{"MsgSettleCredits", &MsgSettleCredits{}},
		{"MsgSettleOverdraft", &MsgSettleOverdraft{}},
		{"MsgUnlockCredits", &MsgUnlockCredits{}},
		{"MsgSwapLUMEtoLAC", &MsgSwapLUMEtoLAC{}},
		{"MsgSwapLACtoLUME", &MsgSwapLACtoLUME{}},
	} {
		bz, err := cdc.MarshalJSON(tc.msg)
		require.NoError(t, err)
		// Every amino name starts with 'lumera/credits/'.
		assert.Contains(t, string(bz), `"type":"lumera/credits/`,
			"%s amino URL follows 'lumera/credits/' prefix. Pins the "+
				"module-scoped naming convention shared across every "+
				"Tx message in this module.",
			tc.name)
	}
}

// TestRegisterInterfaces_TxMessagesRegistered pins the 7 Msg
// implementations on the interface registry. Uses
// Resolve/Unpack to verify each type is resolvable by its
// proto URL (the v1 interface-registry equivalent of the
// legacy amino type-URL).
func TestRegisterInterfaces_TxMessagesRegistered(t *testing.T) {
	t.Parallel()
	reg := codectypes.NewInterfaceRegistry()
	RegisterInterfaces(reg)

	// Every registered msg has a proto type URL. The registry
	// must resolve each one without error.
	for _, tc := range []struct {
		name   string
		msgURL string
	}{
		{"MsgLockCredits", "/lumera.credits.v1.MsgLockCredits"},
		{"MsgSettleCredits", "/lumera.credits.v1.MsgSettleCredits"},
		{"MsgSettleOverdraft", "/lumera.credits.v1.MsgSettleOverdraft"},
		{"MsgUnlockCredits", "/lumera.credits.v1.MsgUnlockCredits"},
		{"MsgSwapLUMEtoLAC", "/lumera.credits.v1.MsgSwapLUMEtoLAC"},
		{"MsgSwapLACtoLUME", "/lumera.credits.v1.MsgSwapLACtoLUME"},
		{"MsgUpdateParams", "/lumera.credits.v1.MsgUpdateParams"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resolved, err := reg.Resolve(tc.msgURL)
			require.NoError(t, err,
				"%s must resolve via interface registry. Pins "+
					"RegisterInterfaces(sdk.Msg) registration. A "+
					"refactor that forgot to add a new Msg here would "+
					"make Any-wrapped Msg unpacking fail at runtime "+
					"for that type.",
				tc.name)
			require.NotNil(t, resolved,
				"resolved type is non-nil for %s", tc.name)
		})
	}
}

// TestRegisterInterfaces_MsgResponsesRegistered pins the 7
// response types registered on the interface registry.
// MsgResponse registrations are required for gRPC response
// unpacking in simulation and tx-result parsing.
func TestRegisterInterfaces_MsgResponsesRegistered(t *testing.T) {
	t.Parallel()
	reg := codectypes.NewInterfaceRegistry()
	RegisterInterfaces(reg)

	for _, tc := range []struct {
		name string
		url  string
	}{
		{"MsgLockCreditsResponse", "/lumera.credits.v1.MsgLockCreditsResponse"},
		{"MsgSettleCreditsResponse", "/lumera.credits.v1.MsgSettleCreditsResponse"},
		{"MsgSettleOverdraftResponse", "/lumera.credits.v1.MsgSettleOverdraftResponse"},
		{"MsgUnlockCreditsResponse", "/lumera.credits.v1.MsgUnlockCreditsResponse"},
		{"MsgSwapLUMEtoLACResponse", "/lumera.credits.v1.MsgSwapLUMEtoLACResponse"},
		{"MsgSwapLACtoLUMEResponse", "/lumera.credits.v1.MsgSwapLACtoLUMEResponse"},
		{"MsgUpdateParamsResponse", "/lumera.credits.v1.MsgUpdateParamsResponse"},
	} {
		resolved, err := reg.Resolve(tc.url)
		require.NoError(t, err,
			"%s must resolve. Pins the response registration — "+
				"missing this would break tx-result JSON serialization "+
				"and simulation-mode callers that unpack MsgResponse "+
				"from Any.", tc.name)
		require.NotNil(t, resolved)
	}
}

// TestRegisterInterfaces_MsgUpdateParamsOnlyInInterfaceRegistry
// is the scan-angle #5 ASYMMETRY anchor. MsgUpdateParams is
// registered on the INTERFACE REGISTRY (it's a governance
// message) but NOT on the legacy amino codec (governance
// messages don't flow through the legacy amino sign path).
func TestCodec_MsgUpdateParamsNotRegisteredOnAmino(t *testing.T) {
	t.Parallel()
	cdc := codec.NewLegacyAmino()
	RegisterLegacyAminoCodec(cdc)

	// MsgUpdateParams is NOT registered on legacy amino.
	// Marshal should fail or produce a non-amino output.
	out, err := cdc.MarshalJSON(&MsgUpdateParams{})
	// Either errors or serializes without an amino type URL — both
	// indicate the type wasn't registered with the 'lumera/credits/'
	// prefix. Pinned as the scan-angle #5 ASYMMETRY: governance
	// messages skip the amino path because the sdk uses a different
	// signing format for MsgUpdateParams.
	if err == nil {
		// Legacy amino with an unregistered type produces plain-Marshal
		// output without a type wrapper; the amino URL only appears when
		// RegisterConcrete was called for the type.
		require.NotContains(t, string(out), "lumera/credits/MsgUpdateParams",
			"MsgUpdateParams must not carry the legacy amino type URL — "+
				"its absence from RegisterLegacyAminoCodec is deliberate")
	}
	// Primary pin: verify that MsgUpdateParams IS registered on
	// the interface registry (so we know the asymmetry is
	// deliberate, not an omission).
	reg := codectypes.NewInterfaceRegistry()
	RegisterInterfaces(reg)
	resolved, err := reg.Resolve("/lumera.credits.v1.MsgUpdateParams")
	require.NoError(t, err,
		"MsgUpdateParams IS registered on interface registry — "+
			"pinning the asymmetry: it's a governance message so it "+
			"skips legacy amino, but it MUST be on the modern "+
			"interface registry for proto-Any unpacking. A refactor "+
			"that forgot interface registration would break every "+
			"governance proposal signing MsgUpdateParams.")
	require.NotNil(t, resolved)
}

// TestPackageGlobals_AminoAndModuleCdcInitialized pins the
// package-level globals declared at codec.go:48-52. The init()
// function at :54-71 calls RegisterLegacyAminoCodec(Amino)
// then Amino.Seal() — a refactor that removed or reordered
// the init would leave the package in an unusable state.
func TestPackageGlobals_AminoAndModuleCdcInitialized(t *testing.T) {
	t.Parallel()
	require.NotNil(t, Amino,
		"Amino package-global is initialized by var declaration + "+
			"mutated by init()")
	require.NotNil(t, ModuleCdc,
		"ModuleCdc package-global initialized")

	// Amino should be sealed (init calls Amino.Seal()).
	// Attempting another RegisterConcrete on a sealed amino
	// panics — use this as a probe.
	assert.Panics(t, func() {
		Amino.RegisterConcrete(&MsgLockCredits{}, "test/dup", nil)
	}, "Amino is SEALED — pins that init() ran Seal() as the "+
		"final step. A refactor that missed Seal would let "+
		"runtime code mutate the amino registration, producing "+
		"non-deterministic signatures across nodes.")

	// The package-global Amino IS the one that was registered.
	// Marshal a known type to verify.
	bz, err := Amino.MarshalJSON(&MsgLockCredits{})
	require.NoError(t, err)
	assert.Contains(t, string(bz), "lumera/credits/MsgLockCredits",
		"package-global Amino has the credits types registered "+
			"(pins init() → RegisterLegacyAminoCodec(Amino) wiring)")
}

// TestRegisterInterfaces_IsIdempotent pins that calling
// RegisterInterfaces twice on the same registry does not
// panic or return an error. Simulations and test harnesses
// routinely re-wire registries.
func TestRegisterInterfaces_IdempotentCalls(t *testing.T) {
	t.Parallel()
	reg := codectypes.NewInterfaceRegistry()
	RegisterInterfaces(reg)
	assert.NotPanics(t, func() { RegisterInterfaces(reg) },
		"RegisterInterfaces must be idempotent. Pins test-harness "+
			"compatibility: a refactor that panicked on re-register "+
			"would break simulation and multi-module test setups "+
			"that rewire registries after module-graph changes.")
}
