//go:build cosmos

package types

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const toolWasmContractSchemaV01 = "lumera.tool_wasm_contract.v0.1"

const maxToolWasmBytecodeCIDLen = 512

// ToolWasmContractV01 captures the metadata needed to route a ToolCard to a CosmWasm contract.
type ToolWasmContractV01 struct {
	Schema          string `json:"$schema"`
	ContractAddress string `json:"contract_address"`
	CodeID          uint64 `json:"code_id"`
	WasmBytecodeCID string `json:"wasm_bytecode_cid"`
	ExecuteMsg      string `json:"execute_msg"`
	QueryMsg        string `json:"query_msg"`
	MaxGas          uint64 `json:"max_gas,omitempty"`
	AllowFunds      bool   `json:"allow_funds,omitempty"`
}

// ParseToolWasmContractV01 parses a JSON-encoded tool wasm contract metadata value.
func ParseToolWasmContractV01(raw string) (*ToolWasmContractV01, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("tool wasm contract metadata is empty")
	}
	var contract ToolWasmContractV01
	if err := json.Unmarshal([]byte(raw), &contract); err != nil {
		return nil, fmt.Errorf("invalid tool wasm contract metadata: %w", err)
	}
	return &contract, nil
}

// Validate ensures the metadata is semantically valid.
func (c *ToolWasmContractV01) Validate() error {
	if c == nil {
		return fmt.Errorf("tool wasm contract metadata cannot be nil")
	}
	if strings.TrimSpace(c.Schema) == "" {
		return fmt.Errorf("tool wasm contract schema is required")
	}
	if c.Schema != toolWasmContractSchemaV01 {
		return fmt.Errorf("unsupported tool wasm contract schema: %s", c.Schema)
	}
	addr := strings.TrimSpace(c.ContractAddress)
	if addr == "" {
		return fmt.Errorf("contract_address is required")
	}
	if addr != c.ContractAddress {
		return fmt.Errorf("contract_address must not contain leading or trailing whitespace")
	}
	if _, err := sdk.AccAddressFromBech32(addr); err != nil {
		return fmt.Errorf("invalid contract_address: %w", err)
	}
	if c.CodeID == 0 {
		return fmt.Errorf("code_id must be > 0")
	}
	if err := validateToolWasmBytecodeCID(c.WasmBytecodeCID); err != nil {
		return fmt.Errorf("wasm_bytecode_cid: %w", err)
	}
	executeMsg := strings.TrimSpace(c.ExecuteMsg)
	if executeMsg == "" {
		return fmt.Errorf("execute_msg is required")
	}
	if executeMsg != c.ExecuteMsg {
		return fmt.Errorf("execute_msg must not contain leading or trailing whitespace")
	}
	queryMsg := strings.TrimSpace(c.QueryMsg)
	if queryMsg == "" {
		return fmt.Errorf("query_msg is required")
	}
	if queryMsg != c.QueryMsg {
		return fmt.Errorf("query_msg must not contain leading or trailing whitespace")
	}
	return nil
}

func validateToolWasmBytecodeCID(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("value is required")
	}
	if raw != strings.TrimSpace(raw) {
		return fmt.Errorf("value must not contain leading or trailing whitespace")
	}
	if len(raw) > maxToolWasmBytecodeCIDLen {
		return fmt.Errorf("value length %d exceeds maximum %d", len(raw), maxToolWasmBytecodeCIDLen)
	}
	if strings.ContainsFunc(raw, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) {
		return fmt.Errorf("value must not contain whitespace or control characters")
	}
	if hasToolWasmBytecodeDigestPrefix(raw) {
		return validateToolWasmBytecodeDigest(raw)
	}
	if strings.HasPrefix(raw, "ipfs://") {
		raw = strings.TrimPrefix(raw, "ipfs://")
	}
	if looksLikeToolWasmBytecodeCID(raw) {
		return nil
	}
	return fmt.Errorf("value must be sha256:<64 hex>, blake3:<64 hex>, or an IPFS CID")
}

func hasToolWasmBytecodeDigestPrefix(raw string) bool {
	return strings.HasPrefix(raw, "sha256:") || strings.HasPrefix(raw, "blake3:")
}

func validateToolWasmBytecodeDigest(raw string) error {
	_, digest, ok := strings.Cut(raw, ":")
	if !ok {
		return fmt.Errorf("digest prefix is malformed")
	}
	if len(digest) != 64 {
		return fmt.Errorf("digest must be 64 hex characters")
	}
	for _, r := range digest {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return fmt.Errorf("digest must be hex")
		}
	}
	return nil
}

func looksLikeToolWasmBytecodeCID(raw string) bool {
	if len(raw) < 16 || len(raw) > maxToolWasmBytecodeCIDLen {
		return false
	}
	if !(strings.HasPrefix(raw, "bafy") || strings.HasPrefix(raw, "bafk") || strings.HasPrefix(raw, "Qm")) {
		return false
	}
	for _, r := range raw {
		if !isToolWasmBytecodeCIDRune(r) {
			return false
		}
	}
	return true
}

func isToolWasmBytecodeCIDRune(r rune) bool {
	return (r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z')
}

// Marshal returns the JSON-encoded metadata value.
func (c *ToolWasmContractV01) Marshal() (string, error) {
	if c == nil {
		return "", fmt.Errorf("tool contract metadata cannot be nil")
	}
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
