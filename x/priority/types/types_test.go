package types

import (
	"testing"
)

func TestModuleConstants(t *testing.T) {
	t.Parallel()
	if ModuleName != "priority" {
		t.Errorf("ModuleName = %q, want %q", ModuleName, "priority")
	}
	if StoreKey != ModuleName {
		t.Errorf("StoreKey = %q, want %q", StoreKey, ModuleName)
	}
	if MemStoreKey != "mem_priority" {
		t.Errorf("MemStoreKey = %q, want %q", MemStoreKey, "mem_priority")
	}
}

func TestKeyPrefixesUnique(t *testing.T) {
	t.Parallel()
	prefixes := [][]byte{ParamsKeyPrefix, AssignmentKeyPrefix}
	seen := make(map[byte]struct{})
	for _, p := range prefixes {
		if len(p) != 1 {
			t.Fatalf("prefix %v should be length 1", p)
		}
		if _, ok := seen[p[0]]; ok {
			t.Errorf("duplicate prefix byte 0x%02x", p[0])
		}
		seen[p[0]] = struct{}{}
	}
}

func TestSentinelErrors(t *testing.T) {
	t.Parallel()
	errs := []error{ErrPriorityTierNotFound, ErrInvalidAssignment}
	for _, e := range errs {
		if e == nil {
			t.Error("sentinel error should not be nil")
		}
	}

	type coder interface{ ABCICode() uint32 }
	codes := make(map[uint32]string)
	for _, e := range errs {
		c, ok := e.(coder)
		if !ok {
			continue
		}
		code := c.ABCICode()
		if prev, dup := codes[code]; dup {
			t.Errorf("duplicate error code %d: %q and %q", code, prev, e.Error())
		}
		codes[code] = e.Error()
	}
}
