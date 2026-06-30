package types

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReceipt_CrossLanguageCanonicalGolden(t *testing.T) {
	receipt, pubkey, _ := workflowReceiptFixture(t)
	canonical, err := CanonicalWorkflowReceiptBytes(receipt)
	require.NoError(t, err)
	canonicalHash := sha256.Sum256(canonical)
	proof, err := BuildWorkflowReceiptProof(receipt, "step-b")
	require.NoError(t, err)

	const wantPubkey = "ed448:323b03041a3d80a4cfb917e051194984b1330f948ba66349042bc920601591e45bc950188e04cd60808fb6e4346d78edd33d0def662b239700"
	const wantCanonical = `{"bundle_id":"bundle-workflow-receipt","canonical_step_order":["step-a","step-b"],"completed_at":"2026-05-08T12:00:00Z","executor_pubkey":"ed448:323b03041a3d80a4cfb917e051194984b1330f948ba66349042bc920601591e45bc950188e04cd60808fb6e4346d78edd33d0def662b239700","lock_id":"lock-bundle-workflow-receipt","merkle_root":"5kBKCkb1UuLRDyuYtd40YDo+zsofxSHShuxeBzJADgI=","non_deterministic_inputs":[{"anchored_height":42,"input_hash":"sHrMYSQNuAw3dpyBifgqOJwpv/iT9x4gGmfBrSQhSqg=","input_id":"wall_clock.completed_at","source":"workflow.invoke.completed_at"},{"anchored_height":41,"input_hash":"a9AeYMsrnM/PIfpyljtD6FeCB+pAptvz5wJTZ8YxK+w=","input_id":"random_nonce.bundle_quote","source":"bundle_quote.nonce"},{"anchored_height":41,"input_hash":"PZFPk0jJzA/4p5cWcAufzU0vPnEWCABOuPE4vLp/FNk=","input_id":"oracle_height.bundle_quote","source":"bundle_quote.anchored_height"}],"outcome":"FINALIZED","step_receipt_hashes":["m93IPgVpVUj15k8zA3uVSOlSv40D+zMD2xfvNkFtl14=","+XTL43FW0bMVF1bpzWF/6TzMVSi+ELM1DPP1eR9JT2Q="],"step_receipts":[{"attempt_count":1,"cost":{"amount":"3","denom":"ulac"},"duration_ms":3,"failure_action":1,"outcome":"success","receipt_hash":"m93IPgVpVUj15k8zA3uVSOlSv40D+zMD2xfvNkFtl14=","step_id":"step-a","tool_id":"tool.step-a","tool_version":"1.0.0"},{"attempt_count":1,"cost":{"amount":"5","denom":"ulac"},"duration_ms":4,"failure_action":1,"outcome":"success","receipt_hash":"+XTL43FW0bMVF1bpzWF/6TzMVSi+ELM1DPP1eR9JT2Q=","step_id":"step-b","tool_id":"tool.step-b","tool_version":"1.0.1"}],"total_cost":{"amount":"8","denom":"ulac"},"trace_id":"trace-receipt","version":"1.0.0","workflow_id":"wf-receipt"}`
	const wantCanonicalSHA256 = "3109e95f2ff0efa5076fa4a5317654dfac9da8516b3eb3a480bd50a040cbd3da"
	const wantStepAHash = "m93IPgVpVUj15k8zA3uVSOlSv40D+zMD2xfvNkFtl14="
	const wantStepBHash = "+XTL43FW0bMVF1bpzWF/6TzMVSi+ELM1DPP1eR9JT2Q="
	const wantMerkleRoot = "5kBKCkb1UuLRDyuYtd40YDo+zsofxSHShuxeBzJADgI="
	const wantExecutorSig = "948b3941d74977b3e2048881b95d55beabadf5bf676dc2673a325022009a15fc025a960a911251c0c2aac7559978f4eb5e50ad81fc17d36980f80e290a9d6831e55654c18998489ac3ae5573b6e8251fb47d03dadc5e2e492a03a1ec751ac424d584556a4e25307b858c09c4b255bffc3600"

	require.Equal(t, wantPubkey, pubkey)
	require.Equal(t, wantCanonical, string(canonical))
	require.Equal(t, wantCanonicalSHA256, hex.EncodeToString(canonicalHash[:]))
	require.Equal(t, wantStepAHash, base64.StdEncoding.EncodeToString(receipt.StepReceiptHashes[0]))
	require.Equal(t, wantStepBHash, base64.StdEncoding.EncodeToString(receipt.StepReceiptHashes[1]))
	require.Equal(t, wantMerkleRoot, base64.StdEncoding.EncodeToString(receipt.MerkleRoot))
	require.Equal(t, wantExecutorSig, hex.EncodeToString(receipt.ExecutorSig))
	require.Equal(t, wantStepBHash, base64.StdEncoding.EncodeToString(proof.LeafHash))
	require.Equal(t, wantStepAHash, base64.StdEncoding.EncodeToString(proof.Siblings[0]))
	require.Equal(t, []bool{false}, proof.SiblingOnRight)
}
