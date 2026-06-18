
package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMsgSetSLATemplate_ValidateBasic pins the three-branch
// governance-authority validator at msgs.go:398-409:
//  1. Authority via sanitizeAddress (bech32 required).
//  2. SlaId non-empty/non-whitespace.
//  3. Payload non-empty/non-whitespace.
//
// Shares exact shape with MsgSetDisputeTerms below (the three
// fields are renamed but the guard structure is identical). Pinning
// both independently defends against a refactor that silently
// dropped a branch from one but kept it on the other.
func TestMsgSetSLATemplate_ValidateBasic(t *testing.T) {
	validAuth := validBech32Addr(t, 0x51)

	cases := []struct {
		name      string
		msg       *MsgSetSLATemplate
		wantErr   bool
		errSubstr string
	}{
		{"happy_path", &MsgSetSLATemplate{Authority: validAuth, SlaId: "sla-1", Payload: "{\"terms\":\"ok\"}"}, false, ""},
		{"empty_authority", &MsgSetSLATemplate{Authority: "", SlaId: "sla-1", Payload: "x"}, true, "address"},
		{"invalid_bech32_authority", &MsgSetSLATemplate{Authority: "not-bech32", SlaId: "sla-1", Payload: "x"}, true, ""},
		{"empty_sla_id", &MsgSetSLATemplate{Authority: validAuth, SlaId: "", Payload: "x"}, true, "sla_id"},
		{"whitespace_sla_id", &MsgSetSLATemplate{Authority: validAuth, SlaId: "   ", Payload: "x"}, true, "sla_id"},
		{"empty_payload", &MsgSetSLATemplate{Authority: validAuth, SlaId: "sla-1", Payload: ""}, true, "payload"},
		{"whitespace_payload", &MsgSetSLATemplate{Authority: validAuth, SlaId: "sla-1", Payload: "\t"}, true, "payload"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr {
				require.Error(t, err)
				if tc.errSubstr != "" {
					require.Contains(t, err.Error(), tc.errSubstr)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestMsgSetDisputeTerms_ValidateBasic pins the identical shape to
// MsgSetSLATemplate but with DisputeTermsId in place of SlaId. Any
// refactor that shares validation logic between the two must keep
// both test suites green; a drift where only one gained/lost a
// branch surfaces here.
func TestMsgSetDisputeTerms_ValidateBasic(t *testing.T) {
	validAuth := validBech32Addr(t, 0x52)

	cases := []struct {
		name      string
		msg       *MsgSetDisputeTerms
		wantErr   bool
		errSubstr string
	}{
		{"happy_path", &MsgSetDisputeTerms{Authority: validAuth, DisputeTermsId: "terms-1", Payload: "{}"}, false, ""},
		{"empty_authority", &MsgSetDisputeTerms{Authority: "", DisputeTermsId: "terms-1", Payload: "x"}, true, "address"},
		{"empty_dispute_terms_id", &MsgSetDisputeTerms{Authority: validAuth, DisputeTermsId: "", Payload: "x"}, true, "dispute_terms_id"},
		{"whitespace_dispute_terms_id", &MsgSetDisputeTerms{Authority: validAuth, DisputeTermsId: " ", Payload: "x"}, true, "dispute_terms_id"},
		{"empty_payload", &MsgSetDisputeTerms{Authority: validAuth, DisputeTermsId: "terms-1", Payload: ""}, true, "payload"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr {
				require.Error(t, err)
				if tc.errSubstr != "" {
					require.Contains(t, err.Error(), tc.errSubstr)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestMsgSetLaneRegistryEntry_ValidateBasic pins the
// explicit-nil-then-field-check shape at msgs.go:431-442. Unlike
// MsgSubmitReceipt / MsgChallengeReceipt (tick 13), this validator
// uses a SEPARATE nil-check then a SEPARATE field access rather
// than the short-circuit `||` form:
//
//	if m.GetEntry() == nil { return err }
//	if trim(m.GetEntry().GetLaneId()) == "" { return err }
//
// Pinning this form here alongside the short-circuit form from
// tick 13 means either direction of "unification" refactor surfaces.
func TestMsgSetLaneRegistryEntry_ValidateBasic(t *testing.T) {
	validAuth := validBech32Addr(t, 0x53)
	validEntry := &LaneRegistryEntry{LaneId: "lane-1"}

	cases := []struct {
		name      string
		msg       *MsgSetLaneRegistryEntry
		wantErr   bool
		errSubstr string
	}{
		{"happy_path", &MsgSetLaneRegistryEntry{Authority: validAuth, Entry: validEntry}, false, ""},
		{"empty_authority", &MsgSetLaneRegistryEntry{Authority: "", Entry: validEntry}, true, "address"},
		{"nil_entry", &MsgSetLaneRegistryEntry{Authority: validAuth, Entry: nil}, true, "entry"},
		{"empty_lane_id", &MsgSetLaneRegistryEntry{Authority: validAuth, Entry: &LaneRegistryEntry{LaneId: ""}}, true, "lane_id"},
		{"whitespace_lane_id", &MsgSetLaneRegistryEntry{Authority: validAuth, Entry: &LaneRegistryEntry{LaneId: "\t "}}, true, "lane_id"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr {
				require.Error(t, err)
				if tc.errSubstr != "" {
					require.Contains(t, err.Error(), tc.errSubstr)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestMsgSetOriginRoutingConfig_ValidateBasic pins the same authority
// validation contract used by sibling governance messages in this file:
// authority must be a valid bech32 address, not merely a non-empty string.
// This closes the prior parity gap where "not-a-valid-bech32" could pass
// stateless validation and only fail later at the stateful authority check.
func TestMsgSetOriginRoutingConfig_ValidateBasic(t *testing.T) {
	validAuth := validBech32Addr(t, 0x54)
	validConfig := &OriginRoutingConfig{OriginId: "origin-1"}

	cases := []struct {
		name      string
		msg       *MsgSetOriginRoutingConfig
		wantErr   bool
		errSubstr string
	}{
		{"happy_path", &MsgSetOriginRoutingConfig{Authority: validAuth, Config: validConfig}, false, ""},
		{"empty_authority", &MsgSetOriginRoutingConfig{Authority: "", Config: validConfig}, true, "address"},
		{"whitespace_authority", &MsgSetOriginRoutingConfig{Authority: "  ", Config: validConfig}, true, "address"},
		{"invalid_bech32_authority", &MsgSetOriginRoutingConfig{Authority: "not-a-bech32-string", Config: validConfig}, true, ""},
		{"nil_config", &MsgSetOriginRoutingConfig{Authority: validAuth, Config: nil}, true, "config"},
		{"empty_origin_id", &MsgSetOriginRoutingConfig{Authority: validAuth, Config: &OriginRoutingConfig{OriginId: ""}}, true, "origin_id"},
		{"whitespace_origin_id", &MsgSetOriginRoutingConfig{Authority: validAuth, Config: &OriginRoutingConfig{OriginId: "\n"}}, true, "origin_id"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.wantErr {
				require.Error(t, err)
				if tc.errSubstr != "" {
					require.Contains(t, err.Error(), tc.errSubstr)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
