
package types

import (
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

var testWasmBytecodeCID = "sha256:" + strings.Repeat("b", 64)

func validContractAddr() string {
	return sdk.AccAddress([]byte("contract-address-pad1")).String()
}

func validContract() *ToolWasmContractV01 {
	return &ToolWasmContractV01{
		Schema:          toolWasmContractSchemaV01,
		ContractAddress: validContractAddr(),
		CodeID:          9,
		WasmBytecodeCID: testWasmBytecodeCID,
		ExecuteMsg:      "tool_execute",
		QueryMsg:        "tool_query",
		MaxGas:          3_000_000,
		AllowFunds:      false,
	}
}

func TestToolWasmContractV01Validate(t *testing.T) {
	require.NoError(t, validContract().Validate())
}

func TestToolWasmContractV01Validate_AllowFundsTrue(t *testing.T) {
	c := validContract()
	c.AllowFunds = true
	require.NoError(t, c.Validate())
}

func TestToolWasmContractV01Validate_MaxGasZero(t *testing.T) {
	c := validContract()
	c.MaxGas = 0
	require.NoError(t, c.Validate(), "MaxGas=0 is valid (omitempty optional field)")
}

func TestToolWasmContractV01Validate_BytecodeCIDVariants(t *testing.T) {
	for _, cid := range []string{
		"blake3:" + strings.Repeat("b", 64),
		"ipfs://bafybeigdyrztbafybeigdyrzt",
		"QmWasmBytecodeCID12345",
	} {
		c := validContract()
		c.WasmBytecodeCID = cid
		require.NoError(t, c.Validate(), "cid %q should validate", cid)
	}
}

func TestToolWasmContractV01Validate_NilContract(t *testing.T) {
	var c *ToolWasmContractV01
	err := c.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}

func TestToolWasmContractV01Validate_FieldErrors(t *testing.T) {
	tests := []struct {
		name   string
		modify func(c *ToolWasmContractV01)
		errMsg string
	}{
		{
			name:   "empty schema",
			modify: func(c *ToolWasmContractV01) { c.Schema = "" },
			errMsg: "schema is required",
		},
		{
			name:   "whitespace schema",
			modify: func(c *ToolWasmContractV01) { c.Schema = "   " },
			errMsg: "schema is required",
		},
		{
			name:   "wrong schema version",
			modify: func(c *ToolWasmContractV01) { c.Schema = "lumera.tool_wasm_contract.v99" },
			errMsg: "unsupported tool wasm contract schema",
		},
		{
			name:   "empty contract_address",
			modify: func(c *ToolWasmContractV01) { c.ContractAddress = "" },
			errMsg: "contract_address is required",
		},
		{
			name:   "whitespace contract_address",
			modify: func(c *ToolWasmContractV01) { c.ContractAddress = "   " },
			errMsg: "contract_address is required",
		},
		{
			name:   "padded contract_address",
			modify: func(c *ToolWasmContractV01) { c.ContractAddress = " " + c.ContractAddress },
			errMsg: "contract_address must not contain leading or trailing whitespace",
		},
		{
			name:   "invalid bech32 contract_address",
			modify: func(c *ToolWasmContractV01) { c.ContractAddress = "not-a-bech32-addr" },
			errMsg: "invalid contract_address",
		},
		{
			name:   "code_id zero",
			modify: func(c *ToolWasmContractV01) { c.CodeID = 0 },
			errMsg: "code_id must be > 0",
		},
		{
			name:   "empty wasm_bytecode_cid",
			modify: func(c *ToolWasmContractV01) { c.WasmBytecodeCID = "" },
			errMsg: "wasm_bytecode_cid",
		},
		{
			name:   "whitespace wasm_bytecode_cid",
			modify: func(c *ToolWasmContractV01) { c.WasmBytecodeCID = "   " },
			errMsg: "wasm_bytecode_cid",
		},
		{
			name:   "padded wasm_bytecode_cid",
			modify: func(c *ToolWasmContractV01) { c.WasmBytecodeCID = c.WasmBytecodeCID + "\n" },
			errMsg: "leading or trailing whitespace",
		},
		{
			name:   "invalid wasm_bytecode_cid",
			modify: func(c *ToolWasmContractV01) { c.WasmBytecodeCID = "sha256:not-hex" },
			errMsg: "digest must be 64 hex characters",
		},
		{
			name:   "empty execute_msg",
			modify: func(c *ToolWasmContractV01) { c.ExecuteMsg = "" },
			errMsg: "execute_msg is required",
		},
		{
			name:   "whitespace execute_msg",
			modify: func(c *ToolWasmContractV01) { c.ExecuteMsg = "   " },
			errMsg: "execute_msg is required",
		},
		{
			name:   "padded execute_msg",
			modify: func(c *ToolWasmContractV01) { c.ExecuteMsg = " " + c.ExecuteMsg },
			errMsg: "execute_msg must not contain leading or trailing whitespace",
		},
		{
			name:   "empty query_msg",
			modify: func(c *ToolWasmContractV01) { c.QueryMsg = "" },
			errMsg: "query_msg is required",
		},
		{
			name:   "whitespace query_msg",
			modify: func(c *ToolWasmContractV01) { c.QueryMsg = "   " },
			errMsg: "query_msg is required",
		},
		{
			name:   "padded query_msg",
			modify: func(c *ToolWasmContractV01) { c.QueryMsg += "\t" },
			errMsg: "query_msg must not contain leading or trailing whitespace",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := validContract()
			tc.modify(c)
			err := c.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errMsg)
		})
	}
}

func TestParseToolWasmContractV01_RoundTrip(t *testing.T) {
	original := validContract()
	raw, err := original.Marshal()
	require.NoError(t, err)
	require.NotEmpty(t, raw)

	parsed, err := ParseToolWasmContractV01(raw)
	require.NoError(t, err)
	require.Equal(t, original.Schema, parsed.Schema)
	require.Equal(t, original.ContractAddress, parsed.ContractAddress)
	require.Equal(t, original.CodeID, parsed.CodeID)
	require.Equal(t, original.WasmBytecodeCID, parsed.WasmBytecodeCID)
	require.Equal(t, original.ExecuteMsg, parsed.ExecuteMsg)
	require.Equal(t, original.QueryMsg, parsed.QueryMsg)
	require.Equal(t, original.MaxGas, parsed.MaxGas)
	require.Equal(t, original.AllowFunds, parsed.AllowFunds)
}

func TestParseToolWasmContractV01_RoundTripWithAllowFunds(t *testing.T) {
	original := validContract()
	original.AllowFunds = true
	raw, err := original.Marshal()
	require.NoError(t, err)

	parsed, err := ParseToolWasmContractV01(raw)
	require.NoError(t, err)
	require.True(t, parsed.AllowFunds)
}

func TestParseToolWasmContractV01_Errors(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		errMsg string
	}{
		{
			name:   "empty string",
			input:  "",
			errMsg: "metadata is empty",
		},
		{
			name:   "whitespace only",
			input:  "   \t\n  ",
			errMsg: "metadata is empty",
		},
		{
			name:   "invalid JSON",
			input:  "{not json at all",
			errMsg: "invalid tool wasm contract metadata",
		},
		{
			name:   "valid JSON wrong shape",
			input:  `{"foo": "bar"}`,
			errMsg: "", // parses ok but yields zero-value struct
		},
		{
			name:   "JSON array instead of object",
			input:  `[1, 2, 3]`,
			errMsg: "invalid tool wasm contract metadata",
		},
		{
			name:   "JSON number",
			input:  `42`,
			errMsg: "invalid tool wasm contract metadata",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := ParseToolWasmContractV01(tc.input)
			if tc.errMsg != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
				require.Nil(t, parsed)
			} else {
				// Valid JSON but wrong shape: parses without error, yields zero-value fields
				require.NoError(t, err)
				require.NotNil(t, parsed)
			}
		})
	}
}

func TestToolWasmContractV01Marshal_NilContract(t *testing.T) {
	var c *ToolWasmContractV01
	raw, err := c.Marshal()
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
	require.Empty(t, raw)
}

func TestToolWasmContractV01Marshal_OmitsZeroOptionalFields(t *testing.T) {
	c := validContract()
	c.MaxGas = 0
	c.AllowFunds = false
	raw, err := c.Marshal()
	require.NoError(t, err)
	// MaxGas has omitempty, so it should be absent in output
	require.NotContains(t, raw, `"max_gas"`)
	// AllowFunds has omitempty, so false should be absent
	require.NotContains(t, raw, `"allow_funds"`)
}

func TestToolWasmContractV01Marshal_IncludesNonZeroOptionalFields(t *testing.T) {
	c := validContract()
	c.MaxGas = 5_000_000
	c.AllowFunds = true
	raw, err := c.Marshal()
	require.NoError(t, err)
	require.Contains(t, raw, `"max_gas":5000000`)
	require.Contains(t, raw, `"allow_funds":true`)
}

func TestToolWasmContractV01_SchemaConstant(t *testing.T) {
	require.Equal(t, "lumera.tool_wasm_contract.v0.1", toolWasmContractSchemaV01)
}

func TestToolWasmContractV01Validate_LargeCodeID(t *testing.T) {
	c := validContract()
	c.CodeID = 1<<63 - 1 // max int64
	require.NoError(t, c.Validate())
}

func TestToolWasmContractV01Validate_LongMessageNames(t *testing.T) {
	c := validContract()
	c.ExecuteMsg = strings.Repeat("x", 256)
	c.QueryMsg = strings.Repeat("y", 256)
	require.NoError(t, c.Validate(), "long message names should be valid")
}
