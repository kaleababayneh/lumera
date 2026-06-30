package types

const (
	// ModuleName identifies the priority module within the app runtime.
	ModuleName = "priority"
	// StoreKey defines the main KV store key for module state.
	StoreKey = ModuleName
	// MemStoreKey provides the in-memory store key used for transient state.
	MemStoreKey = "mem_priority"
)

var (
	// ParamsKeyPrefix prefixes parameters stored in the collection backend.
	ParamsKeyPrefix = []byte{0x01}
	// AssignmentKeyPrefix prefixes policy assignment records.
	AssignmentKeyPrefix = []byte{0x10}
)
