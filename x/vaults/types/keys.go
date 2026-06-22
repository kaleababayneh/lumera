package types

const (
	// ModuleName defines the vault module name.
	ModuleName = "vaults"
	// StoreKey identifies the main KV store for vault state.
	StoreKey = ModuleName
	// RouterKey is used for message routing.
	RouterKey = ModuleName
	// MemStoreKey names the reserved transient in-memory store.
	//
	// Declared per the Cosmos SDK module-keys convention ("mem_" +
	// ModuleName) so ecosystem tooling, state-sync scaffolding, and
	// upgrade handlers can resolve it uniformly. Not currently
	// mounted in app/runtime.go — the vaults keeper has no
	// transient-block-scope state today. The constant is kept so
	// that when a transient store becomes necessary (e.g., per-block
	// rate-limit counters), the key already has its canonical name
	// and no caller that resolved it by convention breaks.
	//
	// Pin tests in keys_test.go assert the literal "mem_vaults"
	// value so an accidental rename surfaces loudly.
	MemStoreKey = "mem_vaults"
)

var (
	// VaultKeyPrefix stores vault records by id.
	VaultKeyPrefix = []byte{0x01}
	// VaultIndexKeyPrefix indexes vaults by owner address.
	VaultIndexKeyPrefix = []byte{0x02}
	// VaultSeqKeyPrefix tracks the auto-incrementing vault sequence.
	VaultSeqKeyPrefix = []byte{0x03}
)
