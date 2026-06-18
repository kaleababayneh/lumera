//go:build cosmos

// Package ibc implements IBC memo parsing for credits transfers.
package ibc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LumeraProtocol/lumera/internal/logging"
)

const (
	// MemoTypeSettlement marks Lumera credits settlement metadata embedded in ICS-20 memos.
	MemoTypeSettlement = "lumera_credits_settlement"

	// MaxSettlementMemoBytes caps the Lumera memo parser/build surface.
	// ICS-20 memos are counterparty-controlled arbitrary bytes for middleware;
	// 64 KiB matches the packet-data cap while bounding direct parser calls.
	MaxSettlementMemoBytes = 64 * 1024
)

// MemoEnvelope wraps Lumera-specific metadata to avoid collisions with other memo formats.
type MemoEnvelope struct {
	Lumera *SettlementMemo `json:"lumera,omitempty"`
}

// SettlementMemo captures settlement metadata embedded in an ICS-20 transfer memo.
type SettlementMemo struct {
	Type          string `json:"type"`
	SettlementID  string `json:"settlement_id"`
	ReceiptHash   string `json:"receipt_hash,omitempty"`
	Router        string `json:"router,omitempty"`
	Publisher     string `json:"publisher,omitempty"`
	ToolID        string `json:"tool_id,omitempty"`
	ToolpackID    string `json:"toolpack_id,omitempty"`
	ActionID      string `json:"action_id,omitempty"`
	RefundAddress string `json:"refund_address,omitempty"`
}

// Validate checks memo fields for required settlement metadata.
func (m *SettlementMemo) Validate() error {
	if m == nil {
		return fmt.Errorf("settlement memo cannot be nil")
	}
	if m.Type != "" && strings.TrimSpace(m.Type) == "" {
		return fmt.Errorf("type must not include surrounding whitespace")
	}
	if err := requireNoSurroundingWhitespace("type", m.Type); err != nil {
		return err
	}
	if err := requireNoSurroundingWhitespace("settlement_id", m.SettlementID); err != nil {
		return err
	}
	if err := requireNoASCIIWhitespaceOrControl("settlement_id", m.SettlementID); err != nil {
		return err
	}
	if err := requireNoSurroundingWhitespace("refund_address", m.RefundAddress); err != nil {
		return err
	}
	if err := requireNoASCIIWhitespaceOrControl("refund_address", m.RefundAddress); err != nil {
		return err
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "receipt_hash", value: m.ReceiptHash},
		{name: "router", value: m.Router},
		{name: "publisher", value: m.Publisher},
		{name: "tool_id", value: m.ToolID},
		{name: "toolpack_id", value: m.ToolpackID},
		{name: "action_id", value: m.ActionID},
	} {
		if err := requireOptionalNoSurroundingWhitespace(field.name, field.value); err != nil {
			return err
		}
		if err := requireNoASCIIWhitespaceOrControl(field.name, field.value); err != nil {
			return err
		}
	}
	memoType := strings.TrimSpace(m.Type)
	if memoType == "" {
		memoType = MemoTypeSettlement
	}
	if memoType != MemoTypeSettlement {
		return fmt.Errorf("unsupported memo type: %s", redactMemoDiagnostic(memoType))
	}
	if strings.TrimSpace(m.SettlementID) == "" {
		return fmt.Errorf("settlement_id is required")
	}
	if strings.TrimSpace(m.RefundAddress) == "" {
		return fmt.Errorf("refund_address is required")
	}
	return nil
}

func requireNoASCIIWhitespaceOrControl(fieldName, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	for i := 0; i < len(value); i++ {
		if value[i] <= ' ' || value[i] == 0x7f {
			return fmt.Errorf("%s must not include whitespace or control characters", fieldName)
		}
	}
	return nil
}

func requireNoSurroundingWhitespace(fieldName, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%s must not include surrounding whitespace", fieldName)
	}
	return nil
}

func requireOptionalNoSurroundingWhitespace(fieldName, value string) error {
	if value == "" {
		return nil
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%s must not include surrounding whitespace", fieldName)
	}
	return nil
}

// BuildSettlementMemo returns the JSON memo string for a settlement transfer.
func BuildSettlementMemo(memo SettlementMemo) (string, error) {
	if memo.Type == "" {
		memo.Type = MemoTypeSettlement
	}
	if err := memo.Validate(); err != nil {
		return "", err
	}
	env := MemoEnvelope{Lumera: &memo}
	raw, err := json.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("marshal settlement memo: %w", err)
	}
	if len(raw) > MaxSettlementMemoBytes {
		return "", fmt.Errorf("settlement memo exceeds maximum size: %d > %d bytes", len(raw), MaxSettlementMemoBytes)
	}
	return string(raw), nil
}

// ParseSettlementMemo extracts Lumera settlement metadata from a memo string.
// Returns ok=false when the memo does not contain a Lumera envelope.
func ParseSettlementMemo(raw string) (*SettlementMemo, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false, nil
	}
	if len([]byte(trimmed)) > MaxSettlementMemoBytes {
		return nil, false, fmt.Errorf("settlement memo exceeds maximum size: %d > %d bytes", len([]byte(trimmed)), MaxSettlementMemoBytes)
	}
	if err := rejectAmbiguousLumeraMemo([]byte(trimmed)); err != nil {
		return nil, false, fmt.Errorf("decode settlement memo: %w", err)
	}
	var env MemoEnvelope
	if err := json.Unmarshal([]byte(trimmed), &env); err != nil {
		return nil, false, fmt.Errorf("decode settlement memo: %w", err)
	}
	if env.Lumera == nil {
		return nil, false, nil
	}
	if env.Lumera.Type == "" {
		env.Lumera.Type = MemoTypeSettlement
	}
	if err := env.Lumera.Validate(); err != nil {
		return nil, false, err
	}
	return env.Lumera, true, nil
}

func rejectAmbiguousLumeraMemo(raw []byte) error {
	if duplicated, err := duplicateObjectKey(raw, "lumera"); err != nil {
		return err
	} else if duplicated {
		return fmt.Errorf("duplicate JSON field %q", "lumera")
	}

	var env struct {
		Lumera json.RawMessage `json:"lumera"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}
	if len(bytes.TrimSpace(env.Lumera)) == 0 || bytes.Equal(bytes.TrimSpace(env.Lumera), []byte("null")) {
		return nil
	}

	key, duplicated, err := firstDuplicateObjectKey(env.Lumera)
	if err != nil {
		return err
	}
	if duplicated {
		return fmt.Errorf("duplicate Lumera settlement field %q", redactMemoDiagnostic(key))
	}
	return nil
}

func redactMemoDiagnostic(value string) string {
	return logging.RedactPII(strings.TrimSpace(value))
}

func duplicateObjectKey(raw []byte, target string) (bool, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	token, err := dec.Token()
	if err != nil {
		return false, err
	}
	delim, ok := token.(json.Delim)
	if !ok || !isObjectStart(delim) {
		return false, nil
	}

	seen := false
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return false, err
		}
		key, ok := token.(string)
		if !ok {
			return false, fmt.Errorf("expected JSON object key")
		}
		if objectKeyMatches(key, target) {
			if seen {
				return true, nil
			}
			seen = true
		}
		if err := skipJSONValue(dec); err != nil {
			return false, err
		}
	}
	return false, consumeObjectEnd(dec)
}

func firstDuplicateObjectKey(raw []byte) (string, bool, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	token, err := dec.Token()
	if err != nil {
		return "", false, err
	}
	delim, ok := token.(json.Delim)
	if !ok || !isObjectStart(delim) {
		return "", false, nil
	}

	seen := make(map[string]struct{})
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return "", false, err
		}
		key, ok := token.(string)
		if !ok {
			return "", false, fmt.Errorf("expected JSON object key")
		}
		if _, exists := seen[key]; exists {
			return key, true, nil
		}
		seen[key] = struct{}{}
		if err := skipJSONValue(dec); err != nil {
			return "", false, err
		}
	}
	if err := consumeObjectEnd(dec); err != nil {
		return "", false, err
	}
	return "", false, nil
}

func skipJSONValue(dec *json.Decoder) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		for dec.More() {
			if _, err := dec.Token(); err != nil {
				return err
			}
			if err := skipJSONValue(dec); err != nil {
				return err
			}
		}
		return consumeObjectEnd(dec)
	case '[':
		for dec.More() {
			if err := skipJSONValue(dec); err != nil {
				return err
			}
		}
		return consumeArrayEnd(dec)
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
}

func consumeObjectEnd(dec *json.Decoder) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok || !isObjectEnd(delim) {
		return fmt.Errorf("expected JSON object end")
	}
	return nil
}

func consumeArrayEnd(dec *json.Decoder) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok || !isArrayEnd(delim) {
		return fmt.Errorf("expected JSON array end")
	}
	return nil
}

func objectKeyMatches(key, target string) bool {
	matches := map[string]struct{}{target: {}}
	_, ok := matches[key]
	return ok
}

func isObjectStart(delim json.Delim) bool {
	return jsonDelimMatches(delim, '{')
}

func isObjectEnd(delim json.Delim) bool {
	return jsonDelimMatches(delim, '}')
}

func isArrayEnd(delim json.Delim) bool {
	return jsonDelimMatches(delim, ']')
}

func jsonDelimMatches(delim, target json.Delim) bool {
	matches := map[json.Delim]struct{}{target: {}}
	_, ok := matches[delim]
	return ok
}
