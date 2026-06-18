//go:build cosmos

package types

import (
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	insurancetypes "github.com/LumeraProtocol/lumera/x/insurance/types"
	nfttypes "github.com/LumeraProtocol/lumera/x/nft/types"
	oracletypes "github.com/LumeraProtocol/lumera/x/oracle/types"
	registrytypes "github.com/LumeraProtocol/lumera/x/registry/types"
	routertypes "github.com/LumeraProtocol/lumera/x/router/types"
)

// Proto registration order conformance tests.
// These tests prove that protobuf interface registration order does not
// affect serialization output. This guards against non-determinism that
// could arise from module initialization order varying across nodes.

type moduleRegistrar struct {
	name     string
	register func(codectypes.InterfaceRegistry)
}

// TestProtoRegistrationOrder_DoesNotAffectSerialization proves that
// registering module interfaces in different orders produces identical
// serialization bytes for the same message.
func TestProtoRegistrationOrder_DoesNotAffectSerialization(t *testing.T) {
	t.Parallel()

	registrars := []moduleRegistrar{
		{"credits", RegisterInterfaces},
		{"insurance", insurancetypes.RegisterInterfaces},
		{"nft", nfttypes.RegisterInterfaces},
		{"oracle", oracletypes.RegisterInterfaces},
		{"registry", registrytypes.RegisterInterfaces},
		{"router", routertypes.RegisterInterfaces},
	}

	// Create registries with forward and reverse registration order
	forwardReg := codectypes.NewInterfaceRegistry()
	for i := 0; i < len(registrars); i++ {
		registrars[i].register(forwardReg)
	}

	reverseReg := codectypes.NewInterfaceRegistry()
	for i := len(registrars) - 1; i >= 0; i-- {
		registrars[i].register(reverseReg)
	}

	// Also test interleaved order
	interleavedReg := codectypes.NewInterfaceRegistry()
	for i := 0; i < len(registrars); i += 2 {
		registrars[i].register(interleavedReg)
	}
	for i := 1; i < len(registrars); i += 2 {
		registrars[i].register(interleavedReg)
	}

	forwardCodec := codec.NewProtoCodec(forwardReg)
	reverseCodec := codec.NewProtoCodec(reverseReg)
	interleavedCodec := codec.NewProtoCodec(interleavedReg)

	testMsgs := []sdk.Msg{
		&MsgLockCredits{
			Router:    "lumera1abc123",
			SessionId: "session-001",
			Amount:    &basev1beta1.Coin{Denom: "ulac", Amount: "100"},
			ToolId:    "tool-001",
		},
		&routertypes.MsgRecordActivation{
			Authority: "lumera1def456",
			ToolId:    "tool-001",
			SessionId: "session-002",
			Activated: true,
		},
		&registrytypes.MsgRegisterTool{
			Owner: "lumera1ghi789",
		},
	}

	for _, msg := range testMsgs {
		msgName := proto.MessageName(msg.(proto.Message))

		forwardBytes, err := forwardCodec.Marshal(msg)
		require.NoError(t, err, "forward marshal %s", msgName)

		reverseBytes, err := reverseCodec.Marshal(msg)
		require.NoError(t, err, "reverse marshal %s", msgName)

		interleavedBytes, err := interleavedCodec.Marshal(msg)
		require.NoError(t, err, "interleaved marshal %s", msgName)

		require.Equal(t, forwardBytes, reverseBytes,
			"%s: forward vs reverse registration order produced different bytes — "+
				"this would cause state divergence across nodes with different module init order",
			msgName)

		require.Equal(t, forwardBytes, interleavedBytes,
			"%s: forward vs interleaved registration order produced different bytes",
			msgName)
	}
}

// TestProtoRegistrationOrder_AnyWrapping proves that Any-wrapped messages
// produce identical bytes regardless of interface registration order.
func TestProtoRegistrationOrder_AnyWrapping(t *testing.T) {
	t.Parallel()

	registrars := []moduleRegistrar{
		{"credits", RegisterInterfaces},
		{"router", routertypes.RegisterInterfaces},
		{"registry", registrytypes.RegisterInterfaces},
	}

	// Forward order registry
	forwardReg := codectypes.NewInterfaceRegistry()
	for _, r := range registrars {
		r.register(forwardReg)
	}

	// Reverse order registry
	reverseReg := codectypes.NewInterfaceRegistry()
	for i := len(registrars) - 1; i >= 0; i-- {
		registrars[i].register(reverseReg)
	}

	forwardCodec := codec.NewProtoCodec(forwardReg)
	reverseCodec := codec.NewProtoCodec(reverseReg)

	msg := &MsgLockCredits{
		Router:    "lumera1test",
		SessionId: "session-any-001",
		Amount:    &basev1beta1.Coin{Denom: "ulac", Amount: "500"},
		ToolId:    "tool-any-001",
	}

	// Wrap in Any using each registry
	forwardAny, err := codectypes.NewAnyWithValue(msg)
	require.NoError(t, err)

	reverseAny, err := codectypes.NewAnyWithValue(msg)
	require.NoError(t, err)

	// Marshal the Any wrappers
	forwardBytes, err := forwardCodec.Marshal(forwardAny)
	require.NoError(t, err)

	reverseBytes, err := reverseCodec.Marshal(reverseAny)
	require.NoError(t, err)

	require.Equal(t, forwardBytes, reverseBytes,
		"Any-wrapped message bytes differ by registration order — "+
			"would cause state hash divergence when storing Any in state")
}

// TestProtoRegistrationOrder_JSONMarshal proves JSON marshaling is
// deterministic regardless of registration order.
func TestProtoRegistrationOrder_JSONMarshal(t *testing.T) {
	t.Parallel()

	registrars := []moduleRegistrar{
		{"credits", RegisterInterfaces},
		{"router", routertypes.RegisterInterfaces},
		{"oracle", oracletypes.RegisterInterfaces},
	}

	forwardReg := codectypes.NewInterfaceRegistry()
	for _, r := range registrars {
		r.register(forwardReg)
	}

	reverseReg := codectypes.NewInterfaceRegistry()
	for i := len(registrars) - 1; i >= 0; i-- {
		registrars[i].register(reverseReg)
	}

	forwardCodec := codec.NewProtoCodec(forwardReg)
	reverseCodec := codec.NewProtoCodec(reverseReg)

	msg := &routertypes.MsgRecordInvocation{
		Authority: "lumera1jsontest",
		SessionId: "session-001",
		ToolId:    "tool-xyz",
		Success:   true,
	}

	forwardJSON, err := forwardCodec.MarshalJSON(msg)
	require.NoError(t, err)

	reverseJSON, err := reverseCodec.MarshalJSON(msg)
	require.NoError(t, err)

	require.JSONEq(t, string(forwardJSON), string(reverseJSON),
		"JSON marshal differs by registration order — "+
			"would cause client/node response divergence")
}

// TestProtoRegistrationOrder_Unmarshal proves that bytes marshaled with
// one registration order can be unmarshaled with a different order.
func TestProtoRegistrationOrder_UnmarshalCrossOrder(t *testing.T) {
	t.Parallel()

	forwardReg := codectypes.NewInterfaceRegistry()
	RegisterInterfaces(forwardReg)
	routertypes.RegisterInterfaces(forwardReg)

	reverseReg := codectypes.NewInterfaceRegistry()
	routertypes.RegisterInterfaces(reverseReg)
	RegisterInterfaces(reverseReg)

	forwardCodec := codec.NewProtoCodec(forwardReg)
	reverseCodec := codec.NewProtoCodec(reverseReg)

	original := &MsgSettleCredits{
		Router: "lumera1settle",
		LockId: "lock-001",
		ToolId: "tool-settle",
	}

	// Marshal with forward order
	forwardBytes, err := forwardCodec.Marshal(original)
	require.NoError(t, err)

	// Unmarshal with reverse order
	var decoded MsgSettleCredits
	err = reverseCodec.Unmarshal(forwardBytes, &decoded)
	require.NoError(t, err)

	require.Equal(t, original.Router, decoded.Router,
		"cross-order unmarshal must preserve Router")
	require.Equal(t, original.LockId, decoded.LockId,
		"cross-order unmarshal must preserve LockId")
}

// TestProtoRegistrationOrder_MultipleRegistrations proves that calling
// RegisterInterfaces multiple times doesn't affect serialization.
func TestProtoRegistrationOrder_IdempotentRegistration(t *testing.T) {
	t.Parallel()

	// Single registration
	singleReg := codectypes.NewInterfaceRegistry()
	RegisterInterfaces(singleReg)

	// Triple registration
	tripleReg := codectypes.NewInterfaceRegistry()
	RegisterInterfaces(tripleReg)
	RegisterInterfaces(tripleReg)
	RegisterInterfaces(tripleReg)

	singleCodec := codec.NewProtoCodec(singleReg)
	tripleCodec := codec.NewProtoCodec(tripleReg)

	msg := &MsgUnlockCredits{
		Router: "lumera1idempotent",
		LockId: "lock-idem-001",
		Reason: "test-reason",
	}

	singleBytes, err := singleCodec.Marshal(msg)
	require.NoError(t, err)

	tripleBytes, err := tripleCodec.Marshal(msg)
	require.NoError(t, err)

	require.Equal(t, singleBytes, tripleBytes,
		"idempotent registration must not affect serialization — "+
			"test harnesses and simulations routinely re-register")
}

// TestProtoRegistrationOrder_PartialRegistration proves that missing
// module registrations don't affect the bytes of registered types.
func TestProtoRegistrationOrder_PartialVsFullRegistration(t *testing.T) {
	t.Parallel()

	// Full registration (all modules)
	fullReg := codectypes.NewInterfaceRegistry()
	RegisterInterfaces(fullReg)
	routertypes.RegisterInterfaces(fullReg)
	registrytypes.RegisterInterfaces(fullReg)
	oracletypes.RegisterInterfaces(fullReg)
	insurancetypes.RegisterInterfaces(fullReg)
	nfttypes.RegisterInterfaces(fullReg)

	// Partial registration (credits only)
	partialReg := codectypes.NewInterfaceRegistry()
	RegisterInterfaces(partialReg)

	fullCodec := codec.NewProtoCodec(fullReg)
	partialCodec := codec.NewProtoCodec(partialReg)

	msg := &MsgLockCredits{
		Router:    "lumera1partial",
		SessionId: "session-partial",
		Amount:    &basev1beta1.Coin{Denom: "ulac", Amount: "999"},
	}

	fullBytes, err := fullCodec.Marshal(msg)
	require.NoError(t, err)

	partialBytes, err := partialCodec.Marshal(msg)
	require.NoError(t, err)

	require.Equal(t, fullBytes, partialBytes,
		"presence of other module registrations must not affect "+
			"credits message serialization — guards against registry "+
			"pollution affecting determinism")
}
