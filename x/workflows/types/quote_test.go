package types

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudflare/circl/sign/ed448"
	"github.com/gowebpki/jcs"
)

func TestBundleQuote_PassportEvidenceAffectsIDAndCanonicalPayload(t *testing.T) {
	inputsHash, err := QuoteInputsHash(json.RawMessage(`{"asset":"ETH"}`))
	if err != nil {
		t.Fatalf("QuoteInputsHash: %v", err)
	}
	quote := &BundleQuote{
		WorkflowID:            "wf-passport",
		Version:               "1.0.0",
		InputsHash:            inputsHash,
		CallerPassportTier:    "trusted",
		CallerPassportActive:  true,
		CallerReputationScore: 700,
		CallerPassportBadges:  []string{"verified-spend"},
		Nonce:                 "nonce-passport",
		StepQuotes: []BundleStepQuote{
			{
				StepID:      "step-a",
				ToolID:      "tool.alpha",
				ToolVersion: "1.2.3",
				SubMaxCost:  QuoteCoin{Denom: "ulac", Amount: "7"},
				SubSloP95Ms: 200,
			},
		},
		TotalMaxCost:   QuoteCoin{Denom: "ulac", Amount: "8"},
		TotalSloP95Ms:  200,
		AnchoredHeight: 9,
		ExpiresAt:      "2026-01-01T00:02:00Z",
		RouterPubkey:   "ed448:" + "ab",
		Signed:         "ed448:" + "00",
	}
	quote.BundleID, err = ComputeBundleQuoteID(quote)
	if err != nil {
		t.Fatalf("ComputeBundleQuoteID: %v", err)
	}

	higherReputation := *quote
	higherReputation.CallerReputationScore = 701
	higherReputationID, err := ComputeBundleQuoteID(&higherReputation)
	if err != nil {
		t.Fatalf("ComputeBundleQuoteID higher reputation: %v", err)
	}
	if higherReputationID == quote.BundleID {
		t.Fatalf("bundle id did not include caller reputation score")
	}

	inactive := *quote
	inactive.CallerPassportActive = false
	inactiveID, err := ComputeBundleQuoteID(&inactive)
	if err != nil {
		t.Fatalf("ComputeBundleQuoteID inactive: %v", err)
	}
	if inactiveID == quote.BundleID {
		t.Fatalf("bundle id did not include caller active passport flag")
	}

	messyBadges := *quote
	messyBadges.CallerPassportBadges = []string{"VERIFIED-SPEND", " onchain-tx-simulation ", "verified-spend"}
	canonical, err := messyBadges.CanonicalBytes()
	if err != nil {
		t.Fatalf("CanonicalBytes messy badges: %v", err)
	}
	wantBadges := `"caller_passport_badges":["onchain-tx-simulation","verified-spend"]`
	if !strings.Contains(string(canonical), wantBadges) {
		t.Fatalf("canonical quote did not normalize passport badges:\n  got: %s\n want: %s", canonical, wantBadges)
	}
	if err := messyBadges.ValidateBasic(); err == nil || !strings.Contains(err.Error(), "caller_passport_badges") {
		t.Fatalf("ValidateBasic did not reject non-canonical passport badges: %v", err)
	}
}

func TestBundleQuoteSignatureRejectsExpectedRouterPubkeyMismatch(t *testing.T) {
	priv := ed448.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed448.SeedSize))
	otherPriv := ed448.NewKeyFromSeed(bytes.Repeat([]byte{0x43}, ed448.SeedSize))
	expectedPubkey, err := RouterPubkeyFromPrivateKey(priv)
	if err != nil {
		t.Fatalf("RouterPubkeyFromPrivateKey expected: %v", err)
	}
	otherPubkey, err := RouterPubkeyFromPrivateKey(otherPriv)
	if err != nil {
		t.Fatalf("RouterPubkeyFromPrivateKey other: %v", err)
	}
	inputsHash, err := QuoteInputsHash(json.RawMessage(`{"asset":"ETH"}`))
	if err != nil {
		t.Fatalf("QuoteInputsHash: %v", err)
	}
	quote := &BundleQuote{
		WorkflowID:            "wf-pubkey-mismatch",
		Version:               "1.0.0",
		InputsHash:            inputsHash,
		CallerPassportTier:    "standard",
		CallerPassportActive:  true,
		CallerReputationScore: 700,
		Nonce:                 "nonce-pubkey-mismatch",
		StepQuotes: []BundleStepQuote{
			{
				StepID:      "step-a",
				ToolID:      "tool.alpha",
				ToolVersion: "1.2.3",
				SubMaxCost:  QuoteCoin{Denom: "ulac", Amount: "7"},
				SubSloP95Ms: 200,
			},
		},
		TotalMaxCost:   QuoteCoin{Denom: "ulac", Amount: "8"},
		TotalSloP95Ms:  200,
		AnchoredHeight: 9,
		ExpiresAt:      "2026-01-01T00:02:00Z",
		RouterPubkey:   otherPubkey,
	}
	quote.BundleID, err = ComputeBundleQuoteID(quote)
	if err != nil {
		t.Fatalf("ComputeBundleQuoteID: %v", err)
	}
	canonical, err := quote.CanonicalBytes()
	if err != nil {
		t.Fatalf("CanonicalBytes: %v", err)
	}
	quote.Signed = "ed448:" + hex.EncodeToString(ed448.Sign(priv, canonical, ""))

	err = VerifyBundleQuoteSignature(quote, expectedPubkey)
	if err == nil || !strings.Contains(err.Error(), "router pubkey does not match expected public key") {
		t.Fatalf("VerifyBundleQuoteSignature accepted mismatched router_pubkey: %v", err)
	}
}

func TestBundleQuoteSignatureRejectsMismatchedDerivedBundleID(t *testing.T) {
	priv := ed448.NewKeyFromSeed(bytes.Repeat([]byte{0x46}, ed448.SeedSize))
	expectedPubkey, err := RouterPubkeyFromPrivateKey(priv)
	if err != nil {
		t.Fatalf("RouterPubkeyFromPrivateKey: %v", err)
	}
	quote := bundleQuoteValidationFixture(t)
	quote.BundleID = "not-derived-from-quote"
	quote.RouterPubkey = expectedPubkey
	quote.Signed = ""
	canonical, err := quote.CanonicalBytes()
	if err != nil {
		t.Fatalf("CanonicalBytes: %v", err)
	}
	quote.Signed = "ed448:" + hex.EncodeToString(ed448.Sign(priv, canonical, ""))

	err = VerifyBundleQuoteSignature(quote, expectedPubkey)
	if err == nil || !strings.Contains(err.Error(), "bundle quote bundle_id does not match canonical quote contents") {
		t.Fatalf("VerifyBundleQuoteSignature accepted mismatched bundle_id: %v", err)
	}
}

func TestBundleQuoteSignatureRejectsNonCanonicalFields(t *testing.T) {
	priv := ed448.NewKeyFromSeed(bytes.Repeat([]byte{0x44}, ed448.SeedSize))
	expectedPubkey, err := RouterPubkeyFromPrivateKey(priv)
	if err != nil {
		t.Fatalf("RouterPubkeyFromPrivateKey: %v", err)
	}
	quote := bundleQuoteValidationFixture(t)
	if err := SignBundleQuote(quote, priv); err != nil {
		t.Fatalf("SignBundleQuote: %v", err)
	}
	quote.WorkflowID = " " + quote.WorkflowID + " "

	err = VerifyBundleQuoteSignature(quote, expectedPubkey)
	if err == nil || !strings.Contains(err.Error(), "bundle quote workflow_id must be canonical") {
		t.Fatalf("VerifyBundleQuoteSignature accepted padded workflow_id: %v", err)
	}
}

func TestSignBundleQuoteRejectsNonCanonicalFields(t *testing.T) {
	priv := ed448.NewKeyFromSeed(bytes.Repeat([]byte{0x45}, ed448.SeedSize))
	tests := []struct {
		name   string
		mutate func(*BundleQuote)
		want   string
	}{
		{
			name: "workflow id padded",
			mutate: func(quote *BundleQuote) {
				quote.WorkflowID = " " + quote.WorkflowID + " "
			},
			want: "bundle quote workflow_id must be canonical",
		},
		{
			name: "step id padded",
			mutate: func(quote *BundleQuote) {
				quote.StepQuotes[0].StepID = " " + quote.StepQuotes[0].StepID + " "
			},
			want: "bundle quote step_id must be canonical",
		},
		{
			name: "total cost padded amount",
			mutate: func(quote *BundleQuote) {
				quote.TotalMaxCost.Amount = " 8 "
			},
			want: "quote coin amount must be canonical",
		},
		{
			name: "passport badges non canonical",
			mutate: func(quote *BundleQuote) {
				quote.CallerPassportBadges = []string{"VERIFIED-SPEND", "verified-spend"}
			},
			want: "bundle quote caller_passport_badges must be lowercase, sorted, and unique",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			quote := bundleQuoteValidationFixture(t)
			tc.mutate(quote)

			err := SignBundleQuote(quote, priv)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("SignBundleQuote error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestComputeBundleQuoteIDRejectsNonCanonicalFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*BundleQuote)
		want   string
	}{
		{
			name: "workflow id padded",
			mutate: func(quote *BundleQuote) {
				quote.WorkflowID = " " + quote.WorkflowID + " "
			},
			want: "bundle quote workflow_id must be canonical",
		},
		{
			name: "step id padded",
			mutate: func(quote *BundleQuote) {
				quote.StepQuotes[0].StepID = " " + quote.StepQuotes[0].StepID + " "
			},
			want: "bundle quote step_id must be canonical",
		},
		{
			name: "total cost padded amount",
			mutate: func(quote *BundleQuote) {
				quote.TotalMaxCost.Amount = " 8 "
			},
			want: "quote coin amount must be canonical",
		},
		{
			name: "passport badges non canonical",
			mutate: func(quote *BundleQuote) {
				quote.CallerPassportBadges = []string{"VERIFIED-SPEND", "verified-spend"}
			},
			want: "bundle quote caller_passport_badges must be lowercase, sorted, and unique",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			quote := bundleQuoteValidationFixture(t)
			quote.BundleID = ""
			quote.Signed = ""
			tc.mutate(quote)

			_, err := ComputeBundleQuoteID(quote)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ComputeBundleQuoteID error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestBundleQuoteValidateBasicRejectsNonCanonicalCoinFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*BundleQuote)
		want   string
	}{
		{
			name: "total cost padded denom",
			mutate: func(quote *BundleQuote) {
				quote.TotalMaxCost.Denom = " ulac "
			},
			want: "quote coin denom must be canonical",
		},
		{
			name: "total cost padded amount",
			mutate: func(quote *BundleQuote) {
				quote.TotalMaxCost.Amount = " 8 "
			},
			want: "quote coin amount must be canonical",
		},
		{
			name: "total cost leading zero amount",
			mutate: func(quote *BundleQuote) {
				quote.TotalMaxCost.Amount = "08"
			},
			want: "quote coin amount must be canonical",
		},
		{
			name: "step cost padded denom",
			mutate: func(quote *BundleQuote) {
				quote.StepQuotes[0].SubMaxCost.Denom = " ulac "
			},
			want: "quote coin denom must be canonical",
		},
		{
			name: "step cost padded amount",
			mutate: func(quote *BundleQuote) {
				quote.StepQuotes[0].SubMaxCost.Amount = " 7 "
			},
			want: "quote coin amount must be canonical",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			quote := bundleQuoteValidationFixture(t)
			tc.mutate(quote)

			err := quote.ValidateBasic()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateBasic error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestBundleQuoteValidateBasicRejectsNonCanonicalStepFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*BundleQuote)
		want   string
	}{
		{
			name: "step id padded",
			mutate: func(quote *BundleQuote) {
				quote.StepQuotes[0].StepID = " " + quote.StepQuotes[0].StepID + " "
			},
			want: "bundle quote step_id must be canonical",
		},
		{
			name: "tool id padded",
			mutate: func(quote *BundleQuote) {
				quote.StepQuotes[0].ToolID = " " + quote.StepQuotes[0].ToolID + " "
			},
			want: "bundle quote tool_id must be canonical",
		},
		{
			name: "tool version padded",
			mutate: func(quote *BundleQuote) {
				quote.StepQuotes[0].ToolVersion = " " + quote.StepQuotes[0].ToolVersion + " "
			},
			want: "bundle quote tool_version must be canonical",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			quote := bundleQuoteValidationFixture(t)
			tc.mutate(quote)

			err := quote.ValidateBasic()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateBasic error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestBundleQuoteValidateBasicRejectsInvalidStepQuotes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*BundleQuote)
		want   string
	}{
		{
			name: "empty step quotes",
			mutate: func(quote *BundleQuote) {
				quote.StepQuotes = nil
			},
			want: "bundle quote step_quotes cannot be empty",
		},
		{
			name: "duplicate step ids",
			mutate: func(quote *BundleQuote) {
				quote.StepQuotes = append(quote.StepQuotes, quote.StepQuotes[0])
			},
			want: "duplicate bundle quote step_id: step-a",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			quote := bundleQuoteValidationFixture(t)
			tc.mutate(quote)

			err := quote.ValidateBasic()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateBasic error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestBundleQuoteValidateBasicRejectsNonCanonicalTopLevelFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*BundleQuote)
		want   string
	}{
		{
			name: "bundle id padded",
			mutate: func(quote *BundleQuote) {
				quote.BundleID = " " + quote.BundleID + " "
			},
			want: "bundle quote bundle_id must be canonical",
		},
		{
			name: "workflow id padded",
			mutate: func(quote *BundleQuote) {
				quote.WorkflowID = " " + quote.WorkflowID + " "
			},
			want: "bundle quote workflow_id must be canonical",
		},
		{
			name: "version padded",
			mutate: func(quote *BundleQuote) {
				quote.Version = " " + quote.Version + " "
			},
			want: "bundle quote version must be canonical",
		},
		{
			name: "inputs hash padded",
			mutate: func(quote *BundleQuote) {
				quote.InputsHash = " " + quote.InputsHash + " "
			},
			want: "bundle quote inputs_hash must be canonical",
		},
		{
			name: "caller passport tier padded",
			mutate: func(quote *BundleQuote) {
				quote.CallerPassportTier = " " + quote.CallerPassportTier + " "
			},
			want: "bundle quote caller_passport_tier must be canonical",
		},
		{
			name: "nonce padded",
			mutate: func(quote *BundleQuote) {
				quote.Nonce = " " + quote.Nonce + " "
			},
			want: "bundle quote nonce must be canonical",
		},
		{
			name: "expires at padded",
			mutate: func(quote *BundleQuote) {
				quote.ExpiresAt = " " + quote.ExpiresAt + " "
			},
			want: "bundle quote expires_at must be canonical",
		},
		{
			name: "router pubkey padded",
			mutate: func(quote *BundleQuote) {
				quote.RouterPubkey = " " + quote.RouterPubkey + " "
			},
			want: "bundle quote router_pubkey must be canonical",
		},
		{
			name: "signature padded",
			mutate: func(quote *BundleQuote) {
				quote.Signed = " " + quote.Signed + " "
			},
			want: "bundle quote signed must be canonical",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			quote := bundleQuoteValidationFixture(t)
			tc.mutate(quote)

			err := quote.ValidateBasic()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateBasic error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestBundleQuoteRecordValidateRejectsNonCanonicalExpiresAt(t *testing.T) {
	quote := bundleQuoteValidationFixture(t)
	record := &BundleQuoteRecord{
		BundleID:      quote.BundleID,
		Quote:         quote,
		ExpiresAt:     " " + quote.ExpiresAt + " ",
		UpdatedHeight: 9,
	}

	err := record.Validate()
	if err == nil || !strings.Contains(err.Error(), "bundle quote record expires_at must be canonical") {
		t.Fatalf("BundleQuoteRecord.Validate error = %v, want canonical expires_at rejection", err)
	}
}

func TestBundleQuoteRecordValidateRejectsMismatchedDerivedBundleID(t *testing.T) {
	quote := bundleQuoteValidationFixture(t)
	quote.BundleID = "not-derived-from-quote"
	record := &BundleQuoteRecord{
		BundleID:      quote.BundleID,
		Quote:         quote,
		ExpiresAt:     quote.ExpiresAt,
		UpdatedHeight: 9,
	}

	err := record.Validate()
	if err == nil || !strings.Contains(err.Error(), "bundle quote bundle_id does not match canonical quote contents") {
		t.Fatalf("BundleQuoteRecord.Validate error = %v, want bundle_id derivation rejection", err)
	}
}

func FuzzQuote_CanonicalSerialization(f *testing.F) {
	f.Add("nonce-a", []byte(`{"b":2,"a":1}`))
	f.Add("nonce-b", []byte(`{"asset":"ETH","amount":"1.5","nested":{"z":false,"a":true}}`))
	f.Add("nonce-c", []byte(`[3,2,1]`))

	f.Fuzz(func(t *testing.T, nonce string, inputs []byte) {
		inputsHash, err := QuoteInputsHash(json.RawMessage(inputs))
		if err != nil {
			return
		}
		quote := &BundleQuote{
			WorkflowID:         "wf-canonical",
			Version:            "1.0.0",
			InputsHash:         inputsHash,
			CallerPassportTier: "standard",
			Nonce:              nonce,
			StepQuotes: []BundleStepQuote{
				{
					StepID:      "step-a",
					ToolID:      "tool.alpha",
					ToolVersion: "1.2.3",
					SubMaxCost:  QuoteCoin{Denom: "ulac", Amount: "7"},
					SubSloP95Ms: 200,
				},
			},
			TotalMaxCost:   QuoteCoin{Denom: "ulac", Amount: "8"},
			TotalSloP95Ms:  200,
			AnchoredHeight: 9,
			ExpiresAt:      "2026-01-01T00:02:00Z",
			RouterPubkey:   "ed448:" + "ab",
		}
		quote.BundleID, err = ComputeBundleQuoteID(quote)
		if err != nil {
			t.Fatalf("ComputeBundleQuoteID: %v", err)
		}

		canonical, err := quote.CanonicalBytes()
		if err != nil {
			t.Fatalf("CanonicalBytes: %v", err)
		}
		again, err := jcs.Transform(canonical)
		if err != nil {
			t.Fatalf("jcs.Transform: %v", err)
		}
		if string(canonical) != string(again) {
			t.Fatalf("canonical bytes not idempotent:\n  got: %s\n  jcs: %s", canonical, again)
		}
		againID, err := ComputeBundleQuoteID(quote)
		if err != nil {
			t.Fatalf("ComputeBundleQuoteID again: %v", err)
		}
		if againID != quote.BundleID {
			t.Fatalf("bundle id drifted: %s != %s", againID, quote.BundleID)
		}
	})
}

func bundleQuoteValidationFixture(t *testing.T) *BundleQuote {
	t.Helper()

	inputsHash, err := QuoteInputsHash(json.RawMessage(`{"asset":"ETH"}`))
	if err != nil {
		t.Fatalf("QuoteInputsHash: %v", err)
	}
	return &BundleQuote{
		BundleID:              "bundle-quote-canonical-coins",
		WorkflowID:            "wf-canonical-coins",
		Version:               "1.0.0",
		InputsHash:            inputsHash,
		CallerPassportTier:    "standard",
		CallerPassportActive:  true,
		CallerReputationScore: 700,
		Nonce:                 "nonce-canonical-coins",
		StepQuotes: []BundleStepQuote{
			{
				StepID:      "step-a",
				ToolID:      "tool.alpha",
				ToolVersion: "1.2.3",
				SubMaxCost:  QuoteCoin{Denom: "ulac", Amount: "7"},
				SubSloP95Ms: 200,
			},
		},
		TotalMaxCost:   QuoteCoin{Denom: "ulac", Amount: "8"},
		TotalSloP95Ms:  200,
		AnchoredHeight: 9,
		ExpiresAt:      "2026-01-01T00:02:00Z",
		RouterPubkey:   "ed448:" + "ab",
		Signed:         "ed448:" + "00",
	}
}
