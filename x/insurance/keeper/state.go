
package keeper

import (
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"
	"cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/LumeraProtocol/lumera/x/insurance/types"
)

// State encapsulates the insurance module's collections-based state management
type State struct {
	// Module parameters
	Params collections.Item[*types.Params]

	// Pool state tracking
	PoolBalance collections.Item[*types.PoolState]
	PoolMetrics collections.Item[*types.PoolMetrics]

	// Claims management (using IndexedMap for both map and index functionality)
	ClaimsByReceipt *collections.IndexedMap[string, *types.Claim, ClaimIndexes]
	ClaimCounter    collections.Sequence

	// Contributions tracking (using IndexedMap for both map and index functionality)
	ContribByReceipt *collections.IndexedMap[string, *types.Contribution, ContributionIndexes]
	ContribCounter   collections.Sequence

	// Publisher risk profiles
	PublisherRisks collections.Map[string, *types.PublisherRisk]

	// Payouts tracking (using IndexedMap for both map and index functionality)
	PayoutsByClaimID *collections.IndexedMap[string, *types.Payout, PayoutIndexes]
	PayoutCounter    collections.Sequence

	// Receipt ownership for claim validation
	ReceiptOwners collections.Map[string, string]
}

// ClaimIndexes defines secondary indexes for claims
type ClaimIndexes struct {
	Receipt   *indexes.Unique[string, string, *types.Claim]
	Claimant  *indexes.Multi[string, string, *types.Claim]
	Publisher *indexes.Multi[string, string, *types.Claim]
	Status    *indexes.Multi[collections.Pair[string, time.Time], string, *types.Claim]
}

// IndexesList returns the collection indexes configured for claims.
func (i ClaimIndexes) IndexesList() []collections.Index[string, *types.Claim] {
	return []collections.Index[string, *types.Claim]{
		i.Receipt,
		i.Claimant,
		i.Publisher,
		i.Status,
	}
}

// NewClaimIndexes creates the index set used for querying claims.
func NewClaimIndexes(sb *collections.SchemaBuilder) ClaimIndexes {
	return ClaimIndexes{
		Receipt: indexes.NewUnique(
			sb, types.ClaimReceiptIndexPrefix, "claim_by_receipt",
			collections.StringKey, collections.StringKey,
			func(_ string, v *types.Claim) (string, error) {
				return v.ReceiptId, nil
			},
		),
		Claimant: indexes.NewMulti(
			sb, types.ClaimClaimantIndexPrefix, "claims_by_claimant",
			collections.StringKey, collections.StringKey,
			func(_ string, v *types.Claim) (string, error) {
				return v.ClaimantId, nil
			},
		),
		Publisher: indexes.NewMulti(
			sb, types.ClaimPublisherIndexPrefix, "claims_by_publisher",
			collections.StringKey, collections.StringKey,
			func(_ string, v *types.Claim) (string, error) {
				return v.PublisherId, nil
			},
		),
		Status: indexes.NewMulti(
			sb, types.ClaimStatusIndexPrefix, "claims_by_status",
			collections.PairKeyCodec(collections.StringKey, sdk.TimeKey), collections.StringKey,
			func(_ string, v *types.Claim) (collections.Pair[string, time.Time], error) {
				t := time.Time{}
				if v.CreatedAt != nil {
					t = *v.CreatedAt
				}
				return collections.Join(v.Status.String(), t), nil
			},
		),
	}
}

// ContributionIndexes defines secondary indexes for contributions
type ContributionIndexes struct {
	Receipt   *indexes.Multi[string, string, *types.Contribution]
	Publisher *indexes.Multi[string, string, *types.Contribution]
	ToolID    *indexes.Multi[string, string, *types.Contribution]
}

// IndexesList returns the configured contribution indexes.
func (i ContributionIndexes) IndexesList() []collections.Index[string, *types.Contribution] {
	return []collections.Index[string, *types.Contribution]{
		i.Receipt,
		i.Publisher,
		i.ToolID,
	}
}

// NewContributionIndexes constructs the contribution secondary indexes.
func NewContributionIndexes(sb *collections.SchemaBuilder) ContributionIndexes {
	return ContributionIndexes{
		Receipt: indexes.NewMulti(
			sb, types.ContribReceiptIndexPrefix, "contrib_by_receipt",
			collections.StringKey, collections.StringKey,
			func(_ string, v *types.Contribution) (string, error) {
				return v.ReceiptId, nil
			},
		),
		Publisher: indexes.NewMulti(
			sb, types.ContribPublisherIndexPrefix, "contribs_by_publisher",
			collections.StringKey, collections.StringKey,
			func(_ string, v *types.Contribution) (string, error) {
				return v.PublisherId, nil
			},
		),
		ToolID: indexes.NewMulti(
			sb, types.ContribToolIndexPrefix, "contribs_by_tool",
			collections.StringKey, collections.StringKey,
			func(_ string, v *types.Contribution) (string, error) {
				return v.ToolId, nil
			},
		),
	}
}

// PayoutIndexes defines secondary indexes for payouts
type PayoutIndexes struct {
	ClaimID   *indexes.Multi[string, string, *types.Payout]
	Recipient *indexes.Multi[string, string, *types.Payout]
	Status    *indexes.Multi[string, string, *types.Payout]
}

// IndexesList returns the configured payout indexes.
func (i PayoutIndexes) IndexesList() []collections.Index[string, *types.Payout] {
	return []collections.Index[string, *types.Payout]{
		i.ClaimID,
		i.Recipient,
		i.Status,
	}
}

// NewPayoutIndexes constructs the index set for payout records.
func NewPayoutIndexes(sb *collections.SchemaBuilder) PayoutIndexes {
	return PayoutIndexes{
		ClaimID: indexes.NewMulti(
			sb, types.PayoutClaimIndexPrefix, "payouts_by_claim",
			collections.StringKey, collections.StringKey,
			func(_ string, v *types.Payout) (string, error) {
				return v.ClaimId, nil
			},
		),
		Recipient: indexes.NewMulti(
			sb, types.PayoutRecipientIndexPrefix, "payouts_by_recipient",
			collections.StringKey, collections.StringKey,
			func(_ string, v *types.Payout) (string, error) {
				return v.RecipientId, nil
			},
		),
		Status: indexes.NewMulti(
			sb, types.PayoutStatusIndexPrefix, "payouts_by_status",
			collections.StringKey, collections.StringKey,
			func(_ string, v *types.Payout) (string, error) {
				return v.Status.String(), nil
			},
		),
	}
}

// NewState creates a new State instance with all collections initialized
func NewState(cdc codec.BinaryCodec, storeService store.KVStoreService) State {
	sb := collections.NewSchemaBuilder(storeService)

	// Initialize claim indexes
	claimIndexes := NewClaimIndexes(sb)

	// Initialize contribution indexes
	contribIndexes := NewContributionIndexes(sb)

	// Initialize payout indexes
	payoutIndexes := NewPayoutIndexes(sb)

	// Create indexed maps first (these replace the non-indexed maps)
	claimsByReceipt := collections.NewIndexedMap(
		sb,
		types.GetClaimsKeyPrefix(),
		"claims_indexed",
		collections.StringKey,
		collPtrValue[types.Claim](cdc),
		claimIndexes,
	)

	contribByReceipt := collections.NewIndexedMap(
		sb,
		types.GetContributionsKeyPrefix(),
		"contributions_indexed",
		collections.StringKey,
		collPtrValue[types.Contribution](cdc),
		contribIndexes,
	)

	payoutsByClaimID := collections.NewIndexedMap(
		sb,
		types.GetPayoutsKeyPrefix(),
		"payouts_indexed",
		collections.StringKey,
		collPtrValue[types.Payout](cdc),
		payoutIndexes,
	)

	state := State{
		Params: collections.NewItem(
			sb,
			types.ParamsKeyPrefix(),
			"params",
			collPtrValue[types.Params](cdc),
		),
		PoolBalance: collections.NewItem(
			sb,
			types.PoolBalanceKeyPrefix(),
			"pool_balance",
			collPtrValue[types.PoolState](cdc),
		),
		PoolMetrics: collections.NewItem(
			sb,
			types.PoolMetricsKeyPrefix(),
			"pool_metrics",
			collPtrValue[types.PoolMetrics](cdc),
		),
		ClaimCounter: collections.NewSequence(
			sb,
			types.ClaimCounterKey(),
			"claim_counter",
		),
		ContribCounter: collections.NewSequence(
			sb,
			types.ContribCounterKey(),
			"contrib_counter",
		),
		PublisherRisks: collections.NewMap(
			sb,
			types.PublisherRisksKeyPrefix(),
			"publisher_risks",
			collections.StringKey,
			collPtrValue[types.PublisherRisk](cdc),
		),
		PayoutCounter: collections.NewSequence(
			sb,
			types.PayoutCounterKey(),
			"payout_counter",
		),
		// Indexed maps (provide both map and index functionality)
		ClaimsByReceipt:  claimsByReceipt,
		ContribByReceipt: contribByReceipt,
		PayoutsByClaimID: payoutsByClaimID,
		ReceiptOwners: collections.NewMap(
			sb,
			types.GetReceiptOwnersKeyPrefix(),
			"receipt_owners",
			collections.StringKey,
			collections.StringValue,
		),
	}

	// Build the schema
	if _, err := sb.Build(); err != nil {
		panic(err)
	}

	return state
}
