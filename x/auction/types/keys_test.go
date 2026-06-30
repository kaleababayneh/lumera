package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Pins the module-identity strings in auction/types/keys.go.
// Previously unpinned. Consensus-critical: these are the routing
// keys the cosmos runtime uses to dispatch transactions and
// queries, so a silent rename (e.g., "auction" → "auctions")
// would reroute every existing tx to nowhere.

// TestModuleIdentity_StableStrings pins the five auction-module
// identity constants at "auction". A rename to any of these is
// a chain-fork event — previously committed auction data would
// be inaccessible under a new module name.
func TestModuleIdentity_StableStrings(t *testing.T) {
	assert.Equal(t, "auction", ModuleName,
		"ModuleName is the canonical module identifier — rename is a chain-fork event")
	assert.Equal(t, "auction", StoreKey,
		"StoreKey must match ModuleName so KVStore reads resolve to the right subtree")
	assert.Equal(t, "auction", RouterKey,
		"RouterKey must match ModuleName for msg routing")
	assert.Equal(t, "auction", QuerierRoute,
		"QuerierRoute must match ModuleName for gRPC query routing")
	assert.Equal(t, "auction", DefaultParamspace,
		"DefaultParamspace must match ModuleName for params subspace lookup")
}

// TestDefaultCreditDenom_StableValue pins the canonical credit
// denomination at "ulac". Auctions bid and settle in this denom;
// a drift to "ulumera" or "lac" would silently route funds to
// the wrong denom subtree in x/bank, causing bids to settle at
// wrong amounts (or fail for denom mismatch far from the
// auction keeper).
func TestDefaultCreditDenom_StableValue(t *testing.T) {
	assert.Equal(t, "ulac", DefaultCreditDenom,
		"DefaultCreditDenom is the on-wire denom contract — a rename would orphan all existing bid funds")
}
