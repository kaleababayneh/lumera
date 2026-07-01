package types

import "testing"

func TestCAC_CanonicalCacheKeyMapOrderMetamorphic(t *testing.T) {
	t.Parallel()

	orders := [][]int{
		{0, 1, 2, 3},
		{3, 2, 1, 0},
		{1, 3, 0, 2},
		{2, 0, 3, 1},
		{3, 0, 2, 1},
	}
	nestedOrders := [][]int{
		{0, 1, 2},
		{2, 1, 0},
		{1, 0, 2},
		{2, 0, 1},
		{0, 2, 1},
	}

	baseArgs := canonicalOrderTestArgs(orders[0], nestedOrders[0])
	baseCanonical, err := CanonicalizeRequestArgs(baseArgs)
	if err != nil {
		t.Fatalf("canonicalize base args: %v", err)
	}
	baseRequestHash := ComputeCanonicalRequestHash("tool.extract", "req-42", baseArgs)
	baseCacheKey := ComputeCanonicalCacheKey("session-7", "policy-v3", " tool.extract ", " v1 ", baseArgs)

	for i := range orders {
		args := canonicalOrderTestArgs(orders[i], nestedOrders[i])

		canonical, err := CanonicalizeRequestArgs(args)
		if err != nil {
			t.Fatalf("canonicalize permuted args %d: %v", i, err)
		}
		if string(canonical) != string(baseCanonical) {
			t.Fatalf("permutation %d changed canonical args\nbase=%s\n got=%s", i, baseCanonical, canonical)
		}

		requestHash := ComputeCanonicalRequestHash("tool.extract", "req-42", args)
		if requestHash != baseRequestHash {
			t.Fatalf("permutation %d changed request hash: base=%q got=%q", i, baseRequestHash, requestHash)
		}

		cacheKey := ComputeCanonicalCacheKey("session-7", "policy-v3", "tool.extract", "v1", args)
		if cacheKey != baseCacheKey {
			t.Fatalf("permutation %d changed cache key: base=%q got=%q", i, baseCacheKey, cacheKey)
		}
	}
}

func FuzzCAC_CanonicalHash(f *testing.F) {
	f.Add("tool.echo", "req-1", "hello", "world")
	f.Add("wallet.positions", "req-2", "wallet", "inj1abc")

	f.Fuzz(func(t *testing.T, toolID, requestID, key, value string) {
		args := map[string]any{
			key: value,
			"nested": map[string]any{
				"tool": toolID,
				"req":  requestID,
			},
		}

		hash1 := ComputeCanonicalRequestHash(toolID, requestID, args)
		hash2 := ComputeCanonicalRequestHash(toolID, requestID, args)
		if hash1 != hash2 {
			t.Fatalf("canonical hash must be deterministic: %q != %q", hash1, hash2)
		}
		if len(hash1) <= len("blake3:") || hash1[:7] != "blake3:" {
			t.Fatalf("unexpected hash format: %q", hash1)
		}
	})
}

func canonicalOrderTestArgs(order []int, nestedOrder []int) map[string]any {
	topFields := []struct {
		key   string
		value any
	}{
		{key: "alpha", value: "one"},
		{key: "nested", value: canonicalOrderNestedArgs(nestedOrder)},
		{key: "count", value: 3},
		{key: "tags", value: []any{"clean", "html", "extract"}},
	}

	args := make(map[string]any, len(topFields))
	for _, idx := range order {
		field := topFields[idx]
		args[field.key] = field.value
	}
	return args
}

func canonicalOrderNestedArgs(order []int) map[string]any {
	nestedFields := []struct {
		key   string
		value any
	}{
		{key: "zeta", value: true},
		{key: "beta", value: "two"},
		{key: "limits", value: map[string]any{"max": 5, "min": 1}},
	}

	nested := make(map[string]any, len(nestedFields))
	for _, idx := range order {
		field := nestedFields[idx]
		nested[field.key] = field.value
	}
	return nested
}
