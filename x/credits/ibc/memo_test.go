//go:build cosmos

package ibc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSettlementMemoRoundtrip(t *testing.T) {
	memo := SettlementMemo{
		SettlementID:  "settlement-1",
		ReceiptHash:   "blake3:abc",
		Router:        "router1",
		Publisher:     "publisher1",
		RefundAddress: "lumera1refund",
	}

	encoded, err := BuildSettlementMemo(memo)
	require.NoError(t, err)

	decoded, ok, err := ParseSettlementMemo(encoded)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, MemoTypeSettlement, decoded.Type)
	require.Equal(t, memo.SettlementID, decoded.SettlementID)
	require.Equal(t, memo.RefundAddress, decoded.RefundAddress)
}

func TestSettlementMemoValidation_EmptySettlementID(t *testing.T) {
	memo := SettlementMemo{SettlementID: "", RefundAddress: "lumera1refund"}
	_, err := BuildSettlementMemo(memo)
	require.Error(t, err)
	require.Contains(t, err.Error(), "settlement_id is required")
}

func TestSettlementMemoValidation_EmptyRefundAddress(t *testing.T) {
	memo := SettlementMemo{SettlementID: "settlement-1", RefundAddress: ""}
	_, err := BuildSettlementMemo(memo)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refund_address is required")
}

func TestSettlementMemoValidation_WhitespaceSettlementID(t *testing.T) {
	memo := SettlementMemo{SettlementID: "   ", RefundAddress: "lumera1refund"}
	_, err := BuildSettlementMemo(memo)
	require.Error(t, err)
	require.Contains(t, err.Error(), "settlement_id is required")
}

func TestSettlementMemoValidation_WhitespaceRefundAddress(t *testing.T) {
	memo := SettlementMemo{SettlementID: "settlement-1", RefundAddress: "   "}
	_, err := BuildSettlementMemo(memo)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refund_address is required")
}

func TestSettlementMemoValidation_NilMemo(t *testing.T) {
	var memo *SettlementMemo
	err := memo.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be nil")
}

func TestSettlementMemoValidation_WrongType(t *testing.T) {
	memo := SettlementMemo{
		Type:          "wrong_type",
		SettlementID:  "settlement-1",
		RefundAddress: "lumera1refund",
	}
	err := memo.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported memo type")
}

func TestSettlementMemoValidation_RejectsSurroundingWhitespace(t *testing.T) {
	tests := []struct {
		name       string
		memo       SettlementMemo
		wantErrMsg string
	}{
		{
			name: "type",
			memo: SettlementMemo{
				Type:          " " + MemoTypeSettlement,
				SettlementID:  "settlement-1",
				RefundAddress: "lumera1refund",
			},
			wantErrMsg: "type must not include surrounding whitespace",
		},
		{
			name: "type whitespace only",
			memo: SettlementMemo{
				Type:          "   ",
				SettlementID:  "settlement-1",
				RefundAddress: "lumera1refund",
			},
			wantErrMsg: "type must not include surrounding whitespace",
		},
		{
			name: "settlement_id",
			memo: SettlementMemo{
				SettlementID:  " settlement-1",
				RefundAddress: "lumera1refund",
			},
			wantErrMsg: "settlement_id must not include surrounding whitespace",
		},
		{
			name: "refund_address",
			memo: SettlementMemo{
				SettlementID:  "settlement-1",
				RefundAddress: "lumera1refund ",
			},
			wantErrMsg: "refund_address must not include surrounding whitespace",
		},
		{
			name: "receipt_hash",
			memo: SettlementMemo{
				SettlementID:  "settlement-1",
				ReceiptHash:   " blake3:abc",
				RefundAddress: "lumera1refund",
			},
			wantErrMsg: "receipt_hash must not include surrounding whitespace",
		},
		{
			name: "router",
			memo: SettlementMemo{
				SettlementID:  "settlement-1",
				Router:        "lumera1router ",
				RefundAddress: "lumera1refund",
			},
			wantErrMsg: "router must not include surrounding whitespace",
		},
		{
			name: "publisher",
			memo: SettlementMemo{
				SettlementID:  "settlement-1",
				Publisher:     " lumera1publisher",
				RefundAddress: "lumera1refund",
			},
			wantErrMsg: "publisher must not include surrounding whitespace",
		},
		{
			name: "tool_id",
			memo: SettlementMemo{
				SettlementID:  "settlement-1",
				ToolID:        "tool-abc ",
				RefundAddress: "lumera1refund",
			},
			wantErrMsg: "tool_id must not include surrounding whitespace",
		},
		{
			name: "toolpack_id",
			memo: SettlementMemo{
				SettlementID:  "settlement-1",
				ToolpackID:    " toolpack-def",
				RefundAddress: "lumera1refund",
			},
			wantErrMsg: "toolpack_id must not include surrounding whitespace",
		},
		{
			name: "action_id",
			memo: SettlementMemo{
				SettlementID:  "settlement-1",
				ActionID:      "action-456 ",
				RefundAddress: "lumera1refund",
			},
			wantErrMsg: "action_id must not include surrounding whitespace",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.memo.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErrMsg)
		})
	}
}

func TestSettlementMemoValidation_RejectsWhitespaceOnlyOptionalFields(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*SettlementMemo)
		wantErrMsg string
	}{
		{
			name: "receipt_hash",
			mutate: func(memo *SettlementMemo) {
				memo.ReceiptHash = "   "
			},
			wantErrMsg: "receipt_hash must not include surrounding whitespace",
		},
		{
			name: "router",
			mutate: func(memo *SettlementMemo) {
				memo.Router = "\t"
			},
			wantErrMsg: "router must not include surrounding whitespace",
		},
		{
			name: "publisher",
			mutate: func(memo *SettlementMemo) {
				memo.Publisher = "\n"
			},
			wantErrMsg: "publisher must not include surrounding whitespace",
		},
		{
			name: "tool_id",
			mutate: func(memo *SettlementMemo) {
				memo.ToolID = "   "
			},
			wantErrMsg: "tool_id must not include surrounding whitespace",
		},
		{
			name: "toolpack_id",
			mutate: func(memo *SettlementMemo) {
				memo.ToolpackID = "\t"
			},
			wantErrMsg: "toolpack_id must not include surrounding whitespace",
		},
		{
			name: "action_id",
			mutate: func(memo *SettlementMemo) {
				memo.ActionID = "\n"
			},
			wantErrMsg: "action_id must not include surrounding whitespace",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			memo := SettlementMemo{
				SettlementID:  "settlement-1",
				RefundAddress: "lumera1refund",
			}
			tc.mutate(&memo)
			err := memo.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErrMsg)
		})
	}
}

func TestSettlementMemoValidation_RejectsEmbeddedWhitespaceAndControl(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*SettlementMemo)
		wantErrMsg string
	}{
		{
			name: "type internal space",
			mutate: func(memo *SettlementMemo) {
				memo.Type = "lumera credits"
			},
			wantErrMsg: "unsupported memo type",
		},
		{
			name: "settlement_id newline",
			mutate: func(memo *SettlementMemo) {
				memo.SettlementID = "settlement\n1"
			},
			wantErrMsg: "settlement_id must not include whitespace or control characters",
		},
		{
			name: "refund_address tab",
			mutate: func(memo *SettlementMemo) {
				memo.RefundAddress = "lumera1\trefund"
			},
			wantErrMsg: "refund_address must not include whitespace or control characters",
		},
		{
			name: "receipt_hash space",
			mutate: func(memo *SettlementMemo) {
				memo.ReceiptHash = "blake3:abc def"
			},
			wantErrMsg: "receipt_hash must not include whitespace or control characters",
		},
		{
			name: "router nul",
			mutate: func(memo *SettlementMemo) {
				memo.Router = "lumera1router\x00"
			},
			wantErrMsg: "router must not include whitespace or control characters",
		},
		{
			name: "publisher carriage return",
			mutate: func(memo *SettlementMemo) {
				memo.Publisher = "lumera1\rpublisher"
			},
			wantErrMsg: "publisher must not include whitespace or control characters",
		},
		{
			name: "tool_id form feed",
			mutate: func(memo *SettlementMemo) {
				memo.ToolID = "tool\fabc"
			},
			wantErrMsg: "tool_id must not include whitespace or control characters",
		},
		{
			name: "toolpack_id vertical tab",
			mutate: func(memo *SettlementMemo) {
				memo.ToolpackID = "toolpack\vdef"
			},
			wantErrMsg: "toolpack_id must not include whitespace or control characters",
		},
		{
			name: "action_id delete",
			mutate: func(memo *SettlementMemo) {
				memo.ActionID = "action\x7f456"
			},
			wantErrMsg: "action_id must not include whitespace or control characters",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			memo := SettlementMemo{
				Type:          MemoTypeSettlement,
				SettlementID:  "settlement-1",
				RefundAddress: "lumera1refund",
			}
			tc.mutate(&memo)

			err := memo.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErrMsg)
		})
	}
}

func TestSettlementMemoValidation_ExplicitCorrectType(t *testing.T) {
	memo := SettlementMemo{
		Type:          MemoTypeSettlement,
		SettlementID:  "settlement-1",
		RefundAddress: "lumera1refund",
	}
	err := memo.Validate()
	require.NoError(t, err)
}

func TestSettlementMemoValidation_EmptyTypeDefaultsToSettlement(t *testing.T) {
	memo := SettlementMemo{
		Type:          "", // Empty type should default
		SettlementID:  "settlement-1",
		RefundAddress: "lumera1refund",
	}
	err := memo.Validate()
	require.NoError(t, err)
}

func TestBuildSettlementMemo_RejectsWhitespaceOnlyType(t *testing.T) {
	memo := SettlementMemo{
		Type:          "   ",
		SettlementID:  "settlement-1",
		RefundAddress: "lumera1refund",
	}
	_, err := BuildSettlementMemo(memo)
	require.Error(t, err)
	require.Contains(t, err.Error(), "type must not include surrounding whitespace")
}

func TestParseSettlementMemo_EmptyString(t *testing.T) {
	decoded, ok, err := ParseSettlementMemo("")
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, decoded)
}

func TestParseSettlementMemo_WhitespaceString(t *testing.T) {
	decoded, ok, err := ParseSettlementMemo("   ")
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, decoded)
}

func TestParseSettlementMemo_InvalidJSON(t *testing.T) {
	decoded, ok, err := ParseSettlementMemo("not valid json")
	require.Error(t, err)
	require.False(t, ok)
	require.Nil(t, decoded)
	require.Contains(t, err.Error(), "decode settlement memo")
}

func TestParseSettlementMemo_NonLumeraMemo(t *testing.T) {
	decoded, ok, err := ParseSettlementMemo("{\"other\":{\"foo\":\"bar\"}}")
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, decoded)
}

func TestParseSettlementMemo_LumeraButNullValue(t *testing.T) {
	decoded, ok, err := ParseSettlementMemo("{\"lumera\":null}")
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, decoded)
}

func TestParseSettlementMemo_LumeraWithInvalidData(t *testing.T) {
	// Lumera envelope exists but settlement_id is missing
	decoded, ok, err := ParseSettlementMemo("{\"lumera\":{\"refund_address\":\"lumera1refund\"}}")
	require.Error(t, err)
	require.False(t, ok)
	require.Nil(t, decoded)
	require.Contains(t, err.Error(), "settlement_id is required")
}

func TestParseSettlementMemo_LumeraWithMissingRefund(t *testing.T) {
	decoded, ok, err := ParseSettlementMemo("{\"lumera\":{\"settlement_id\":\"settlement-1\"}}")
	require.Error(t, err)
	require.False(t, ok)
	require.Nil(t, decoded)
	require.Contains(t, err.Error(), "refund_address is required")
}

func TestParseSettlementMemo_RejectsEmbeddedControlFields(t *testing.T) {
	raw := `{"lumera":{"settlement_id":"settlement\n1","refund_address":"lumera1refund"}}`

	decoded, ok, err := ParseSettlementMemo(raw)
	require.Error(t, err)
	require.False(t, ok)
	require.Nil(t, decoded)
	require.Contains(t, err.Error(), "settlement_id must not include whitespace or control characters")
}

func TestParseSettlementMemo_DefaultsTypeIfEmpty(t *testing.T) {
	// Valid memo without explicit type should default to settlement type
	raw := `{"lumera":{"settlement_id":"settlement-1","refund_address":"lumera1refund"}}`
	decoded, ok, err := ParseSettlementMemo(raw)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, MemoTypeSettlement, decoded.Type)
}

func TestParseSettlementMemo_RejectsOversizedMemo(t *testing.T) {
	raw := `{"lumera":{"settlement_id":"` + strings.Repeat("x", MaxSettlementMemoBytes) + `","refund_address":"lumera1refund"}}`

	decoded, ok, err := ParseSettlementMemo(raw)
	require.Error(t, err)
	require.False(t, ok)
	require.Nil(t, decoded)
	require.Contains(t, err.Error(), "settlement memo exceeds maximum size")
}

func TestParseSettlementMemo_RejectsDuplicateLumeraEnvelope(t *testing.T) {
	raw := `{"lumera":null,"lumera":{"settlement_id":"settlement-1","refund_address":"lumera1refund"}}`

	decoded, ok, err := ParseSettlementMemo(raw)
	require.Error(t, err)
	require.False(t, ok)
	require.Nil(t, decoded)
	require.Contains(t, err.Error(), `duplicate JSON field "lumera"`)
}

func TestParseSettlementMemo_RejectsDuplicateLumeraSettlementField(t *testing.T) {
	raw := `{"lumera":{"settlement_id":"first","settlement_id":"second","refund_address":"lumera1refund"}}`

	decoded, ok, err := ParseSettlementMemo(raw)
	require.Error(t, err)
	require.False(t, ok)
	require.Nil(t, decoded)
	require.Contains(t, err.Error(), `duplicate Lumera settlement field "settlement_id"`)
}

func TestParseSettlementMemo_RedactsRequestControlledDiagnostics(t *testing.T) {
	rawValue := "memo-value-" + strings.Repeat("x", 20)
	sensitiveField := "Authorization: Bearer " + rawValue
	raw := `{"lumera":{"` + sensitiveField + `":"first","` + sensitiveField + `":"second","settlement_id":"settlement-1","refund_address":"lumera1refund"}}`

	decoded, ok, err := ParseSettlementMemo(raw)

	require.Error(t, err)
	require.False(t, ok)
	require.Nil(t, decoded)
	require.Contains(t, err.Error(), "duplicate Lumera settlement field")
	require.Contains(t, err.Error(), "Bearer [REDACTED]")
	require.NotContains(t, err.Error(), rawValue)

	raw = `{"lumera":{"type":"Authorization: Bearer ` + rawValue + `","settlement_id":"settlement-1","refund_address":"lumera1refund"}}`
	decoded, ok, err = ParseSettlementMemo(raw)

	require.Error(t, err)
	require.False(t, ok)
	require.Nil(t, decoded)
	require.Contains(t, err.Error(), "unsupported memo type")
	require.Contains(t, err.Error(), "Bearer [REDACTED]")
	require.NotContains(t, err.Error(), rawValue)
}

func TestParseSettlementMemo_IgnoresUnrelatedTopLevelMiddlewareKeys(t *testing.T) {
	raw := `{"memo":"packet-forward","memo":"rate-limit","lumera":{"settlement_id":"settlement-1","refund_address":"lumera1refund"}}`

	decoded, ok, err := ParseSettlementMemo(raw)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "settlement-1", decoded.SettlementID)
	require.Equal(t, "lumera1refund", decoded.RefundAddress)
}

func TestBuildSettlementMemo_DefaultsType(t *testing.T) {
	memo := SettlementMemo{
		SettlementID:  "settlement-1",
		RefundAddress: "lumera1refund",
	}
	encoded, err := BuildSettlementMemo(memo)
	require.NoError(t, err)

	// Verify the type was set in the output
	var env MemoEnvelope
	err = json.Unmarshal([]byte(encoded), &env)
	require.NoError(t, err)
	require.Equal(t, MemoTypeSettlement, env.Lumera.Type)
}

func TestBuildSettlementMemo_RejectsOversizedOutput(t *testing.T) {
	memo := SettlementMemo{
		SettlementID:  strings.Repeat("x", MaxSettlementMemoBytes),
		RefundAddress: "lumera1refund",
	}

	encoded, err := BuildSettlementMemo(memo)
	require.Error(t, err)
	require.Empty(t, encoded)
	require.Contains(t, err.Error(), "settlement memo exceeds maximum size")
}

func TestBuildSettlementMemo_AllFields(t *testing.T) {
	memo := SettlementMemo{
		Type:          MemoTypeSettlement,
		SettlementID:  "settlement-123",
		ReceiptHash:   "blake3:xyz789",
		Router:        "lumera1router",
		Publisher:     "lumera1publisher",
		ToolID:        "tool-abc",
		ToolpackID:    "toolpack-def",
		ActionID:      "action-456",
		RefundAddress: "lumera1refund",
	}

	encoded, err := BuildSettlementMemo(memo)
	require.NoError(t, err)

	decoded, ok, err := ParseSettlementMemo(encoded)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, memo.SettlementID, decoded.SettlementID)
	require.Equal(t, memo.ReceiptHash, decoded.ReceiptHash)
	require.Equal(t, memo.Router, decoded.Router)
	require.Equal(t, memo.Publisher, decoded.Publisher)
	require.Equal(t, memo.ToolID, decoded.ToolID)
	require.Equal(t, memo.ToolpackID, decoded.ToolpackID)
	require.Equal(t, memo.ActionID, decoded.ActionID)
	require.Equal(t, memo.RefundAddress, decoded.RefundAddress)
}

func TestMemoTypeConstant(t *testing.T) {
	require.Equal(t, "lumera_credits_settlement", MemoTypeSettlement)
}

func TestMemoEnvelope_JSONStructure(t *testing.T) {
	memo := SettlementMemo{
		SettlementID:  "settlement-1",
		RefundAddress: "lumera1refund",
	}
	encoded, err := BuildSettlementMemo(memo)
	require.NoError(t, err)

	// Verify the JSON structure has the "lumera" key
	var raw map[string]interface{}
	err = json.Unmarshal([]byte(encoded), &raw)
	require.NoError(t, err)
	require.Contains(t, raw, "lumera")

	lumera, ok := raw["lumera"].(map[string]interface{})
	require.True(t, ok)
	require.Contains(t, lumera, "type")
	require.Contains(t, lumera, "settlement_id")
	require.Contains(t, lumera, "refund_address")
}
