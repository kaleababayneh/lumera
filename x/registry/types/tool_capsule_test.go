
package types

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func validToolCapsule() *ToolCapsule {
	return &ToolCapsule{
		ToolId:      "tool-1",
		CapsuleType: CapsuleTypeWASM,
		CapsuleHash: bytes.Repeat([]byte{0x01}, 32),
		Entrypoint:  "/app/entry.wasm",
		Runtime:     CapsuleRuntimeWasmtime,
		EgressAllowlist: []string{
			"https://api.example.com",
		},
		ResourceLimits: &CapsuleResourceLimits{
			CpuMs: 1000,
			MemMb: 256,
			NetKb: 1024,
		},
		PublisherSig: bytes.Repeat([]byte{0x02}, 64),
	}
}

func TestToolCapsuleValidate(t *testing.T) {
	capsule := validToolCapsule()
	require.NoError(t, capsule.Validate())

	capsule.ToolId = ""
	require.Error(t, capsule.Validate())
	capsule.ToolId = "tool-1"

	capsule.CapsuleType = "invalid"
	require.Error(t, capsule.Validate())
	capsule.CapsuleType = CapsuleTypeWASM

	capsule.CapsuleHash = []byte{0x01}
	require.Error(t, capsule.Validate())
	capsule.CapsuleHash = bytes.Repeat([]byte{0x01}, 32)

	capsule.Entrypoint = ""
	require.Error(t, capsule.Validate())
	capsule.Entrypoint = "/app/entry.wasm"

	capsule.Runtime = "invalid"
	require.Error(t, capsule.Validate())
	capsule.Runtime = CapsuleRuntimeWasmtime

	capsule.EgressAllowlist = nil
	require.Error(t, capsule.Validate())
	capsule.EgressAllowlist = []string{"https://api.example.com"}

	capsule.EgressAllowlist = []string{"ftp://example.com"}
	require.Error(t, capsule.Validate())
	capsule.EgressAllowlist = []string{"https://api.example.com"}

	capsule.ResourceLimits = nil
	require.Error(t, capsule.Validate())
	capsule.ResourceLimits = &CapsuleResourceLimits{CpuMs: 1000, MemMb: 256, NetKb: 1024}

	capsule.PublisherSig = nil
	require.Error(t, capsule.Validate())
	capsule.PublisherSig = bytes.Repeat([]byte{0x02}, 64)
}

func TestCapsuleResourceLimitsValidate(t *testing.T) {
	limits := &CapsuleResourceLimits{CpuMs: 1, MemMb: 1, NetKb: 1}
	require.NoError(t, limits.Validate())

	limits.CpuMs = 0
	require.Error(t, limits.Validate())
	limits.CpuMs = 1

	limits.MemMb = 0
	require.Error(t, limits.Validate())
	limits.MemMb = 1

	limits.NetKb = 0
	require.Error(t, limits.Validate())
}
