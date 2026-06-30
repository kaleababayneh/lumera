// Package keeper provides state access for the workflows module.
package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/Masterminds/semver/v3"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	registrytypes "github.com/LumeraProtocol/lumera/x/registry/types"
	"github.com/LumeraProtocol/lumera/x/workflows/types"
)

// ConsensusVersion defines the module consensus version for migrations.
const ConsensusVersion = 1

type jsonValueCodec[T any] struct{}

func (j jsonValueCodec[T]) Encode(value *T) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}

func (j jsonValueCodec[T]) Decode(b []byte) (*T, error) {
	if b == nil {
		return nil, nil
	}
	var value T
	if err := json.Unmarshal(b, &value); err != nil {
		return nil, err
	}
	return &value, nil
}

func (j jsonValueCodec[T]) EncodeJSON(value *T) ([]byte, error) { return j.Encode(value) }
func (j jsonValueCodec[T]) DecodeJSON(b []byte) (*T, error)     { return j.Decode(b) }

func (j jsonValueCodec[T]) Stringify(value *T) string {
	bz, err := j.Encode(value)
	if err != nil {
		return fmt.Sprintf("<error: %v>", err)
	}
	return string(bz)
}

func (j jsonValueCodec[T]) ValueType() string {
	var zero T
	return fmt.Sprintf("%T", zero)
}

func newJSONValueCodec[T any]() jsonValueCodec[T] { return jsonValueCodec[T]{} }

// State encapsulates the module collections state.
type State struct {
	Schema       collections.Schema
	Params       collections.Item[*types.Params]
	Workflows    collections.Map[string, *types.WorkflowRecord]
	AuthorBonds  collections.Map[string, *types.AuthorBondRecord]
	BundleQuotes collections.Map[string, *types.BundleQuoteRecord]
}

// Keeper provides the module's state access layer.
type Keeper struct {
	cdc          codec.BinaryCodec
	storeService corestore.KVStoreService
	authority    string
	logger       log.Logger
	state        State
	toolCards    WorkflowToolCardReader
}

// WorkflowToolCardReader is the registry ToolCard surface used for publish-time workflow checks.
type WorkflowToolCardReader interface {
	GetToolCard(ctx sdk.Context, toolID string) (*registrytypes.ToolCard, bool)
}

// NewKeeper constructs a Keeper instance.
func NewKeeper(cdc codec.BinaryCodec, storeService corestore.KVStoreService, authority string, logger log.Logger) *Keeper {
	sb := collections.NewSchemaBuilder(storeService)
	state := State{
		Params: collections.NewItem(
			sb,
			collections.NewPrefix(types.ParamsPrefix),
			"params",
			newJSONValueCodec[types.Params](),
		),
		Workflows: collections.NewMap(
			sb,
			collections.NewPrefix(types.WorkflowPrefix),
			"workflows",
			collections.StringKey,
			newJSONValueCodec[types.WorkflowRecord](),
		),
		AuthorBonds: collections.NewMap(
			sb,
			collections.NewPrefix(types.AuthorBondPrefix),
			"author_bonds",
			collections.StringKey,
			newJSONValueCodec[types.AuthorBondRecord](),
		),
		BundleQuotes: collections.NewMap(
			sb,
			collections.NewPrefix(types.BundleQuotePrefix),
			"bundle_quotes",
			collections.StringKey,
			newJSONValueCodec[types.BundleQuoteRecord](),
		),
	}
	schema, err := sb.Build()
	if err != nil {
		panic(fmt.Errorf("workflows schema build failed: %w", err))
	}
	state.Schema = schema

	return &Keeper{
		cdc:          cdc,
		storeService: storeService,
		authority:    authority,
		logger:       logger.With("module", "x/"+types.ModuleName),
		state:        state,
	}
}

// Schema returns the underlying collections schema.
func (k *Keeper) Schema() collections.Schema { return k.state.Schema }

// Logger returns the module logger.
func (k *Keeper) Logger() log.Logger { return k.logger }

// Authority returns the governance authority address.
func (k *Keeper) Authority() string { return k.authority }

// SetWorkflowToolCardReader wires the registry ToolCard reader used by publish-time validation.
func (k *Keeper) SetWorkflowToolCardReader(reader WorkflowToolCardReader) {
	k.toolCards = reader
}

// GetParams obtains stored params or defaults when unset.
func (k *Keeper) GetParams(ctx context.Context) (*types.Params, error) {
	params, err := k.state.Params.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.DefaultParams(), nil
		}
		return nil, fmt.Errorf("load workflows params: %w", err)
	}
	if params == nil {
		return types.DefaultParams(), nil
	}
	return params, nil
}

// SetParams validates and stores governance parameters.
func (k *Keeper) SetParams(ctx context.Context, params *types.Params) error {
	if err := params.Validate(); err != nil {
		return err
	}
	if err := k.state.Params.Set(ctx, params); err != nil {
		return fmt.Errorf("store workflows params: %w", err)
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeParamsUpdated,
			sdk.NewAttribute(types.AttributeKeyModule, types.ModuleName),
			sdk.NewAttribute(types.AttributeKeyMinAuthorBond, params.MinAuthorBond()),
			sdk.NewAttribute(types.AttributeKeyWastedWorkBPS, fmt.Sprintf("%d", params.WastedWorkBPS)),
		),
	)
	return nil
}

func (k *Keeper) validateWorkflowCardToolResolution(ctx context.Context, card *types.WorkflowCard) error {
	if k.toolCards == nil {
		return nil
	}
	resolver := workflowToolCardResolver{
		ctx:    sdk.UnwrapSDKContext(ctx),
		reader: k.toolCards,
	}
	return types.StaticCheckWorkflowCardWithToolResolver(card, resolver)
}

type workflowToolCardResolver struct {
	ctx    sdk.Context
	reader WorkflowToolCardReader
}

func (r workflowToolCardResolver) ResolveWorkflowTool(toolID, exactVersion string) (types.WorkflowToolDescriptor, bool, error) {
	tool, found := r.lookup(toolID)
	if !found {
		return types.WorkflowToolDescriptor{}, false, nil
	}
	version := strings.TrimSpace(tool.GetVersion())
	if version != strings.TrimSpace(exactVersion) {
		return types.WorkflowToolDescriptor{}, false, nil
	}
	return types.WorkflowToolDescriptor{
		ToolID:       strings.TrimSpace(tool.GetToolId()),
		Version:      version,
		InputSchema:  tool.GetInputSchema(),
		OutputSchema: tool.GetOutputSchema(),
	}, true, nil
}

func (r workflowToolCardResolver) WorkflowToolVersions(toolID string) []string {
	tool, found := r.lookup(toolID)
	if !found {
		return nil
	}
	version := strings.TrimSpace(tool.GetVersion())
	if version == "" {
		return nil
	}
	return []string{version}
}

func (r workflowToolCardResolver) lookup(toolID string) (*registrytypes.ToolCard, bool) {
	if r.reader == nil {
		return nil, false
	}
	tool, found := r.reader.GetToolCard(r.ctx, strings.TrimSpace(toolID))
	if !found || tool == nil {
		return nil, false
	}
	return tool, true
}

// PutWorkflow stores scaffold workflow state. Business validation lands with storage/bond logic.
func (k *Keeper) PutWorkflow(ctx context.Context, workflow *types.WorkflowRecord) error {
	if workflow == nil {
		return types.ErrInvalidWorkflow.Wrap("workflow cannot be nil")
	}
	key, err := types.WorkflowKey(workflow.WorkflowID, workflow.Version)
	if err != nil {
		return err
	}
	return k.state.Workflows.Set(ctx, key, workflow)
}

// GetWorkflow loads a scaffold workflow state record.
func (k *Keeper) GetWorkflow(ctx context.Context, workflowID, version string) (*types.WorkflowRecord, bool, error) {
	key, err := types.WorkflowKey(workflowID, version)
	if err != nil {
		return nil, false, err
	}
	workflow, err := k.state.Workflows.Get(ctx, key)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("load workflow %s: %w", key, err)
	}
	return workflow, true, nil
}

// PutAuthorBond stores scaffold author-bond state.
func (k *Keeper) PutAuthorBond(ctx context.Context, bond *types.AuthorBondRecord) error {
	if bond == nil {
		return fmt.Errorf("author bond cannot be nil")
	}
	bond.AuthorAddress = strings.TrimSpace(bond.AuthorAddress)
	if bond.AuthorAddress == "" {
		return fmt.Errorf("author bond missing author address")
	}
	bond.LockedFor = uniqueSortedStrings(bond.LockedFor)
	return k.state.AuthorBonds.Set(ctx, bond.AuthorAddress, bond)
}

// GetAuthorBond loads author-bond state.
func (k *Keeper) GetAuthorBond(ctx context.Context, author string) (*types.AuthorBondRecord, bool, error) {
	author = strings.TrimSpace(author)
	if author == "" {
		return nil, false, fmt.Errorf("author address cannot be empty")
	}
	bond, err := k.state.AuthorBonds.Get(ctx, author)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("load author bond %s: %w", author, err)
	}
	return bond, true, nil
}

// RemoveAuthorBond removes empty author-bond state.
func (k *Keeper) RemoveAuthorBond(ctx context.Context, author string) error {
	author = strings.TrimSpace(author)
	if author == "" {
		return fmt.Errorf("author address cannot be empty")
	}
	if err := k.state.AuthorBonds.Remove(ctx, author); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("remove author bond %s: %w", author, err)
	}
	return nil
}

// ListWorkflowVersions returns all stored versions for a workflow id in semver order.
func (k *Keeper) ListWorkflowVersions(ctx context.Context, workflowID string) ([]*types.WorkflowRecord, error) {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return nil, types.ErrInvalidWorkflow.Wrap("workflow_id cannot be empty")
	}
	var out []*types.WorkflowRecord
	if err := k.state.Workflows.Walk(ctx, nil, func(_ string, workflow *types.WorkflowRecord) (bool, error) {
		if workflow != nil && strings.TrimSpace(workflow.WorkflowID) == workflowID {
			out = append(out, workflow)
		}
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterate workflow versions: %w", err)
	}
	sort.Slice(out, func(i, j int) bool {
		return compareSemver(out[i].Version, out[j].Version) < 0
	})
	return out, nil
}

// PublishWorkflow stores a new workflow version and locks author bond for it.
func (k *Keeper) PublishWorkflow(ctx context.Context, msg *types.MsgPublishWorkflow) error {
	if msg == nil {
		return fmt.Errorf("workflow publish request cannot be nil")
	}
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	card := msg.GetWorkflowCard()
	if err := validateWorkflowCardSemver(card); err != nil {
		return err
	}
	if err := k.validateWorkflowCardToolResolution(ctx, card); err != nil {
		return err
	}
	key, err := types.WorkflowKey(card.GetWorkflowId(), card.GetVersion())
	if err != nil {
		return err
	}
	if _, found, err := k.GetWorkflow(ctx, card.GetWorkflowId(), card.GetVersion()); err != nil {
		return err
	} else if found {
		return types.ErrInvalidWorkflow.Wrapf("workflow version already exists: %s", key)
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	required, err := requiredAuthorBond(card, params)
	if err != nil {
		return err
	}
	incoming, err := normalizeWorkflowCoin(workflowCoinPtr(msg.Bond), params.BondDenom)
	if err != nil {
		return err
	}
	if incoming == nil {
		incoming = zeroWorkflowCoin(params.BondDenom)
	}
	if err := k.upsertAuthorBond(ctx, msg.GetAuthor(), incoming, required, key); err != nil {
		return err
	}
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if err := k.PutWorkflow(ctx, &types.WorkflowRecord{
		WorkflowID:    card.GetWorkflowId(),
		Version:       card.GetVersion(),
		Status:        types.WorkflowStatusActive,
		AuthorAddress: strings.TrimSpace(msg.GetAuthor()),
		Card:          card,
		CreatedHeight: height,
		UpdatedHeight: height,
	}); err != nil {
		return err
	}
	k.emitWorkflowTransition(ctx, types.EventTypeWorkflowPublished, card.GetWorkflowId(), "", card.GetVersion(), msg.GetAuthor(), incoming, "publish")
	return nil
}

// UpgradeWorkflow stores a newer workflow version while preserving older active versions.
func (k *Keeper) UpgradeWorkflow(ctx context.Context, msg *types.MsgUpgradeWorkflow) error {
	if msg == nil {
		return fmt.Errorf("workflow upgrade request cannot be nil")
	}
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	card := msg.GetWorkflowCard()
	if err := validateWorkflowCardSemver(card); err != nil {
		return err
	}
	if err := k.validateWorkflowCardToolResolution(ctx, card); err != nil {
		return err
	}
	if compareSemver(card.GetVersion(), msg.GetFromVersion()) <= 0 {
		return types.ErrInvalidWorkflow.Wrapf("workflow version %s must be greater than %s", card.GetVersion(), msg.GetFromVersion())
	}
	prev, found, err := k.GetWorkflow(ctx, msg.GetWorkflowID(), msg.GetFromVersion())
	if err != nil {
		return err
	}
	if !found {
		return types.ErrInvalidWorkflow.Wrapf("from workflow version not found: %s/%s", msg.GetWorkflowID(), msg.GetFromVersion())
	}
	if prev.AuthorAddress != strings.TrimSpace(msg.GetAuthor()) {
		return types.ErrInvalidWorkflow.Wrap("only the workflow author may upgrade")
	}
	if _, found, err := k.GetWorkflow(ctx, card.GetWorkflowId(), card.GetVersion()); err != nil {
		return err
	} else if found {
		return types.ErrInvalidWorkflow.Wrapf("workflow version already exists: %s/%s", card.GetWorkflowId(), card.GetVersion())
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return err
	}
	versions, err := k.ListWorkflowVersions(ctx, card.GetWorkflowId())
	if err != nil {
		return err
	}
	if uint32(len(versions)+1) > params.MaxWorkflowVersions {
		return types.ErrInvalidWorkflow.Wrapf("workflow %s exceeds max versions %d", card.GetWorkflowId(), params.MaxWorkflowVersions)
	}
	key, err := types.WorkflowKey(card.GetWorkflowId(), card.GetVersion())
	if err != nil {
		return err
	}
	required, err := requiredAuthorBond(card, params)
	if err != nil {
		return err
	}
	if err := k.ensureAuthorBondLocked(ctx, msg.GetAuthor(), key, required); err != nil {
		return err
	}
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if err := k.PutWorkflow(ctx, &types.WorkflowRecord{
		WorkflowID:    card.GetWorkflowId(),
		Version:       card.GetVersion(),
		Status:        types.WorkflowStatusActive,
		AuthorAddress: strings.TrimSpace(msg.GetAuthor()),
		Card:          card,
		CreatedHeight: height,
		UpdatedHeight: height,
	}); err != nil {
		return err
	}
	k.emitWorkflowTransition(ctx, types.EventTypeWorkflowUpgraded, card.GetWorkflowId(), msg.GetFromVersion(), card.GetVersion(), msg.GetAuthor(), zeroWorkflowCoin(params.BondDenom), "upgrade")
	return nil
}

// DeactivateWorkflow marks a workflow version inactive and unlocks that version from author bond.
func (k *Keeper) DeactivateWorkflow(ctx context.Context, msg *types.MsgDeactivateWorkflow) error {
	if msg == nil {
		return fmt.Errorf("workflow deactivate request cannot be nil")
	}
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	workflow, found, err := k.GetWorkflow(ctx, msg.GetWorkflowID(), msg.GetVersion())
	if err != nil {
		return err
	}
	if !found {
		return types.ErrInvalidWorkflow.Wrapf("workflow version not found: %s/%s", msg.GetWorkflowID(), msg.GetVersion())
	}
	if workflow.AuthorAddress != strings.TrimSpace(msg.GetAuthor()) {
		return types.ErrInvalidWorkflow.Wrap("only the workflow author may deactivate")
	}
	workflow.Status = types.WorkflowStatusInactive
	workflow.UpdatedHeight = sdk.UnwrapSDKContext(ctx).BlockHeight()
	if err := k.PutWorkflow(ctx, workflow); err != nil {
		return err
	}
	key, err := types.WorkflowKey(msg.GetWorkflowID(), msg.GetVersion())
	if err != nil {
		return err
	}
	if err := k.unlockAuthorBond(ctx, msg.GetAuthor(), key); err != nil {
		return err
	}
	k.emitWorkflowTransition(ctx, types.EventTypeWorkflowDeactivated, msg.GetWorkflowID(), msg.GetVersion(), "", msg.GetAuthor(), nil, strings.TrimSpace(msg.Reason))
	return nil
}

// WithdrawBond withdraws unlocked workflow-author bond. Amount nil withdraws the full unlocked bond.
func (k *Keeper) WithdrawBond(ctx context.Context, msg *types.MsgWithdrawBond) error {
	if msg == nil {
		return fmt.Errorf("workflow withdraw bond request cannot be nil")
	}
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	bond, found, err := k.GetAuthorBond(ctx, msg.GetAuthor())
	if err != nil {
		return err
	}
	if !found {
		return types.ErrInvalidWorkflow.Wrapf("author bond not found: %s", msg.GetAuthor())
	}
	if len(bond.LockedFor) > 0 {
		return types.ErrInvalidWorkflow.Wrap("cannot withdraw author bond while workflow versions are locked")
	}
	bondDenom := workflowBondDenom(bond.Bond)
	withdraw, err := normalizeWorkflowCoin(workflowCoinPtr(msg.Amount), bondDenom)
	if err != nil {
		return err
	}
	current, err := workflowCoinAmount(bond.Bond)
	if err != nil {
		return err
	}
	if withdraw == nil {
		withdraw = cloneWorkflowCoin(bond.Bond)
		if withdraw == nil {
			withdraw = zeroWorkflowCoin(bondDenom)
		}
	}
	withdrawAmount, err := workflowCoinAmount(withdraw)
	if err != nil {
		return err
	}
	if withdrawAmount.GT(current) {
		return types.ErrInvalidWorkflow.Wrap("withdrawal exceeds bonded amount")
	}
	remaining := current.Sub(withdrawAmount)
	if remaining.IsZero() {
		if err := k.RemoveAuthorBond(ctx, msg.GetAuthor()); err != nil {
			return err
		}
	} else {
		bond.Bond.Amount = remaining
		bond.UpdatedHeight = sdk.UnwrapSDKContext(ctx).BlockHeight()
		if err := k.PutAuthorBond(ctx, bond); err != nil {
			return err
		}
	}
	k.emitBondWithdrawn(ctx, msg.GetAuthor(), withdraw, &sdk.Coin{Denom: withdraw.Denom, Amount: remaining})
	return nil
}

// TopUpAuthorBond adds unlocked bond balance for workflow publication or upgrades.
func (k *Keeper) TopUpAuthorBond(ctx context.Context, msg *types.MsgTopUpAuthorBond) error {
	if msg == nil {
		return fmt.Errorf("workflow top up author bond request cannot be nil")
	}
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	author := strings.TrimSpace(msg.GetAuthor())
	topUp, err := normalizeWorkflowCoin(workflowCoinPtr(msg.GetAmount()), "")
	if err != nil {
		return err
	}
	bond, found, err := k.GetAuthorBond(ctx, author)
	if err != nil {
		return err
	}
	if !found {
		bond = &types.AuthorBondRecord{
			AuthorAddress: author,
			Bond:          zeroWorkflowCoin(topUp.Denom),
			Slashed:       zeroWorkflowCoin(topUp.Denom),
		}
	}
	if workflowBondDenom(bond.Bond) != topUp.Denom {
		return types.ErrInvalidWorkflow.Wrapf("author bond denom %s does not match top-up denom %s", workflowBondDenom(bond.Bond), topUp.Denom)
	}
	total, err := addWorkflowCoins(bond.Bond, topUp)
	if err != nil {
		return err
	}
	bond.Bond = total
	if bond.Slashed == nil {
		bond.Slashed = zeroWorkflowCoin(topUp.Denom)
	}
	bond.UpdatedHeight = sdk.UnwrapSDKContext(ctx).BlockHeight()
	if err := k.PutAuthorBond(ctx, bond); err != nil {
		return err
	}
	k.emitBondToppedUp(ctx, author, topUp, total)
	return nil
}

// SlashWorkflowAuthorBond reduces an author's bond for a disputed workflow version.
func (k *Keeper) SlashWorkflowAuthorBond(ctx context.Context, workflowID, version string, amount *sdk.Coin, reason string) error {
	workflow, found, err := k.GetWorkflow(ctx, workflowID, version)
	if err != nil {
		return err
	}
	if !found {
		return types.ErrInvalidWorkflow.Wrapf("workflow version not found: %s/%s", workflowID, version)
	}
	bond, found, err := k.GetAuthorBond(ctx, workflow.AuthorAddress)
	if err != nil {
		return err
	}
	if !found {
		return types.ErrInvalidWorkflow.Wrapf("author bond not found: %s", workflow.AuthorAddress)
	}
	slash, err := normalizeWorkflowCoin(amount, workflowBondDenom(bond.Bond))
	if err != nil {
		return err
	}
	if slash == nil {
		return types.ErrInvalidWorkflow.Wrap("slash amount is required")
	}
	current, err := workflowCoinAmount(bond.Bond)
	if err != nil {
		return err
	}
	slashAmount, err := workflowCoinAmount(slash)
	if err != nil {
		return err
	}
	if slashAmount.GT(current) {
		slashAmount = current
		slash.Amount = current
	}
	if slashAmount.IsZero() {
		return types.ErrInvalidWorkflow.Wrap("insufficient author bond to slash")
	}
	priorSlashed, err := workflowCoinAmount(bond.Slashed)
	if err != nil {
		return err
	}
	remaining := current.Sub(slashAmount)
	bond.Bond.Amount = remaining
	bond.Slashed = &sdk.Coin{Denom: slash.Denom, Amount: priorSlashed.Add(slashAmount)}
	bond.UpdatedHeight = sdk.UnwrapSDKContext(ctx).BlockHeight()
	if err := k.PutAuthorBond(ctx, bond); err != nil {
		return err
	}
	k.emitBondSlashed(ctx, workflow.WorkflowID, workflow.Version, workflow.AuthorAddress, slash, bond.Bond, reason)
	return nil
}

// EmitLifecycleEvent records structured lifecycle telemetry for module hooks.
func (k *Keeper) EmitLifecycleEvent(ctx context.Context, phase string) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	eventJSON := fmt.Sprintf(`{"module":%q,"phase":%q,"height":%d,"status":"ok"}`, types.ModuleName, phase, sdkCtx.BlockHeight())
	k.Logger().Info("workflows lifecycle", "event_json", eventJSON)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeLifecycle,
			sdk.NewAttribute(types.AttributeKeyModule, types.ModuleName),
			sdk.NewAttribute(types.AttributeKeyPhase, phase),
			sdk.NewAttribute(types.AttributeKeyStatus, "ok"),
		),
	)
}

func (k *Keeper) upsertAuthorBond(ctx context.Context, author string, incoming *sdk.Coin, required *sdk.Coin, workflowKey string) error {
	author = strings.TrimSpace(author)
	existing, found, err := k.GetAuthorBond(ctx, author)
	if err != nil {
		return err
	}
	if !found {
		existing = &types.AuthorBondRecord{
			AuthorAddress: author,
			Bond:          zeroWorkflowCoin(required.Denom),
			Slashed:       zeroWorkflowCoin(required.Denom),
		}
	}
	total, err := addWorkflowCoins(existing.Bond, incoming)
	if err != nil {
		return err
	}
	requiredAmount, err := workflowCoinAmount(required)
	if err != nil {
		return err
	}
	totalAmount, err := workflowCoinAmount(total)
	if err != nil {
		return err
	}
	if totalAmount.LT(requiredAmount) {
		return types.ErrInvalidWorkflow.Wrapf("author bond must be >= %s%s", required.Amount, required.Denom)
	}
	existing.Bond = total
	existing.LockedFor = append(existing.LockedFor, workflowKey)
	existing.LockedFor = uniqueSortedStrings(existing.LockedFor)
	existing.UpdatedHeight = sdk.UnwrapSDKContext(ctx).BlockHeight()
	return k.PutAuthorBond(ctx, existing)
}

func (k *Keeper) ensureAuthorBondLocked(ctx context.Context, author string, workflowKey string, required *sdk.Coin) error {
	bond, found, err := k.GetAuthorBond(ctx, author)
	if err != nil {
		return err
	}
	if !found {
		return types.ErrInvalidWorkflow.Wrapf("author bond not found: %s", author)
	}
	if required != nil {
		if strings.TrimSpace(required.GetDenom()) != workflowBondDenom(bond.Bond) {
			return types.ErrInvalidWorkflow.Wrapf("author bond denom %s does not match required denom %s", workflowBondDenom(bond.Bond), required.GetDenom())
		}
		currentAmount, err := workflowCoinAmount(bond.Bond)
		if err != nil {
			return err
		}
		requiredAmount, err := workflowCoinAmount(required)
		if err != nil {
			return err
		}
		if currentAmount.LT(requiredAmount) {
			return types.ErrInvalidWorkflow.Wrapf("author bond must be >= %s%s", required.Amount, required.Denom)
		}
	}
	bond.LockedFor = append(bond.LockedFor, workflowKey)
	bond.LockedFor = uniqueSortedStrings(bond.LockedFor)
	bond.UpdatedHeight = sdk.UnwrapSDKContext(ctx).BlockHeight()
	return k.PutAuthorBond(ctx, bond)
}

func (k *Keeper) unlockAuthorBond(ctx context.Context, author string, workflowKey string) error {
	bond, found, err := k.GetAuthorBond(ctx, author)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	bond.LockedFor = removeString(bond.LockedFor, workflowKey)
	bond.UpdatedHeight = sdk.UnwrapSDKContext(ctx).BlockHeight()
	return k.PutAuthorBond(ctx, bond)
}

func (k *Keeper) emitWorkflowTransition(ctx context.Context, eventType string, workflowID string, prevVersion string, newVersion string, actor string, bondDelta *sdk.Coin, reason string) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if reason == "" {
		reason = "unspecified"
	}
	bondDeltaText := ""
	if bondDelta != nil {
		bondDeltaText = workflowCoinString(bondDelta)
	}
	k.Logger().Info(
		"workflow state transition",
		"workflow_id", workflowID,
		"prev_version", prevVersion,
		"new_version", newVersion,
		"bond_delta", bondDeltaText,
		"actor", actor,
		"reason", reason,
	)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			eventType,
			sdk.NewAttribute(types.AttributeKeyWorkflowID, workflowID),
			sdk.NewAttribute(types.AttributeKeyPrevVersion, prevVersion),
			sdk.NewAttribute(types.AttributeKeyNewVersion, newVersion),
			sdk.NewAttribute(types.AttributeKeyActor, actor),
			sdk.NewAttribute(types.AttributeKeyBondDelta, bondDeltaText),
			sdk.NewAttribute(types.AttributeKeyReason, reason),
		),
	)
}

func (k *Keeper) emitBondWithdrawn(ctx context.Context, author string, amount *sdk.Coin, remaining *sdk.Coin) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeAuthorBondWithdrawn,
			sdk.NewAttribute(types.AttributeKeyActor, author),
			sdk.NewAttribute(types.AttributeKeyAmount, workflowCoinString(amount)),
			sdk.NewAttribute(types.AttributeKeyRemaining, workflowCoinString(remaining)),
		),
	)
}

func (k *Keeper) emitBondToppedUp(ctx context.Context, author string, amount *sdk.Coin, remaining *sdk.Coin) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeAuthorBondToppedUp,
			sdk.NewAttribute(types.AttributeKeyActor, author),
			sdk.NewAttribute(types.AttributeKeyAmount, workflowCoinString(amount)),
			sdk.NewAttribute(types.AttributeKeyRemaining, workflowCoinString(remaining)),
		),
	)
}

func (k *Keeper) emitBondSlashed(ctx context.Context, workflowID string, version string, author string, amount *sdk.Coin, remaining *sdk.Coin, reason string) {
	if strings.TrimSpace(reason) == "" {
		reason = "dispute"
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeAuthorBondSlashed,
			sdk.NewAttribute(types.AttributeKeyWorkflowID, workflowID),
			sdk.NewAttribute(types.AttributeKeyVersion, version),
			sdk.NewAttribute(types.AttributeKeyActor, author),
			sdk.NewAttribute(types.AttributeKeyAmount, workflowCoinString(amount)),
			sdk.NewAttribute(types.AttributeKeyRemaining, workflowCoinString(remaining)),
			sdk.NewAttribute(types.AttributeKeyReason, reason),
		),
	)
}

func validateWorkflowCardSemver(card *types.WorkflowCard) error {
	if card == nil {
		return types.ErrInvalidWorkflow.Wrap("workflow_card is required")
	}
	if _, err := semver.NewVersion(strings.TrimSpace(card.GetVersion())); err != nil {
		return types.ErrInvalidWorkflow.Wrapf("invalid semantic version %q", card.GetVersion())
	}
	return nil
}

func compareSemver(left, right string) int {
	leftVersion, leftErr := semver.NewVersion(strings.TrimSpace(left))
	rightVersion, rightErr := semver.NewVersion(strings.TrimSpace(right))
	if leftErr != nil || rightErr != nil {
		return strings.Compare(left, right)
	}
	return leftVersion.Compare(rightVersion)
}

func requiredAuthorBond(card *types.WorkflowCard, params *types.Params) (*sdk.Coin, error) {
	paramAmount, ok := math.NewIntFromString(strings.TrimSpace(params.MinAuthorBondAmount))
	if !ok {
		return nil, types.ErrInvalidWorkflow.Wrapf("params min_author_bond_amount must be an integer: %q", params.MinAuthorBondAmount)
	}
	required := &sdk.Coin{Denom: strings.TrimSpace(params.BondDenom), Amount: paramAmount}
	if card == nil || card.GetPricing() == nil || workflowCoinIsUnset(card.GetPricing().GetMinBond()) {
		return required, nil
	}
	cardBond := card.GetPricing().GetMinBond()
	if strings.TrimSpace(cardBond.GetDenom()) != strings.TrimSpace(params.BondDenom) {
		return nil, types.ErrInvalidWorkflow.Wrapf("workflow min_bond denom %s does not match params denom %s", cardBond.GetDenom(), params.BondDenom)
	}
	cardAmount, err := workflowCoinAmount(&cardBond)
	if err != nil {
		return nil, err
	}
	if cardAmount.GT(paramAmount) {
		clone := cardBond
		return &clone, nil
	}
	return required, nil
}

// normalizeWorkflowCoin validates and canonicalizes a coin. A nil input coin
// returns (nil, nil) to signal "unset". The default denom is applied when the
// coin carries no denom of its own.
func normalizeWorkflowCoin(coin *sdk.Coin, defaultDenom string) (*sdk.Coin, error) {
	if coin == nil {
		return nil, nil
	}
	denom := strings.TrimSpace(coin.GetDenom())
	if denom == "" {
		denom = strings.TrimSpace(defaultDenom)
	}
	if denom == "" {
		return nil, types.ErrInvalidWorkflow.Wrap("coin denom is required")
	}
	if coin.Amount.IsNil() {
		return nil, types.ErrInvalidWorkflow.Wrap("coin amount is required")
	}
	if coin.Amount.IsNegative() {
		return nil, types.ErrInvalidWorkflow.Wrap("coin amount cannot be negative")
	}
	return &sdk.Coin{Denom: denom, Amount: coin.Amount}, nil
}

func workflowCoinAmount(coin *sdk.Coin) (math.Int, error) {
	if coin == nil || coin.Amount.IsNil() {
		return math.ZeroInt(), nil
	}
	if coin.Amount.IsNegative() {
		return math.Int{}, types.ErrInvalidWorkflow.Wrap("coin amount cannot be negative")
	}
	return coin.Amount, nil
}

func addWorkflowCoins(left *sdk.Coin, right *sdk.Coin) (*sdk.Coin, error) {
	if left == nil {
		return cloneWorkflowCoin(right), nil
	}
	if right == nil {
		return cloneWorkflowCoin(left), nil
	}
	if strings.TrimSpace(left.GetDenom()) != strings.TrimSpace(right.GetDenom()) {
		return nil, types.ErrInvalidWorkflow.Wrapf("coin denom mismatch: %s != %s", left.GetDenom(), right.GetDenom())
	}
	leftAmount, err := workflowCoinAmount(left)
	if err != nil {
		return nil, err
	}
	rightAmount, err := workflowCoinAmount(right)
	if err != nil {
		return nil, err
	}
	return &sdk.Coin{Denom: strings.TrimSpace(left.GetDenom()), Amount: leftAmount.Add(rightAmount)}, nil
}

func cloneWorkflowCoin(coin *sdk.Coin) *sdk.Coin {
	if coin == nil {
		return nil
	}
	return &sdk.Coin{Denom: strings.TrimSpace(coin.GetDenom()), Amount: coin.Amount}
}

func zeroWorkflowCoin(denom string) *sdk.Coin {
	return &sdk.Coin{Denom: strings.TrimSpace(denom), Amount: math.ZeroInt()}
}

func workflowCoinString(coin *sdk.Coin) string {
	if coin == nil || coin.Amount.IsNil() {
		return ""
	}
	return coin.Amount.String() + strings.TrimSpace(coin.GetDenom())
}

func workflowBondDenom(coin *sdk.Coin) string {
	if coin == nil {
		return ""
	}
	return strings.TrimSpace(coin.GetDenom())
}

// workflowCoinIsUnset reports whether a value sdk.Coin (gogoproto field) was not
// provided: no denom and a nil-or-zero amount. (A JSON round-trip turns an unset
// coin into {"amount":"0"}, so a zero amount must also count as unset.)
func workflowCoinIsUnset(coin sdk.Coin) bool {
	return coin.Denom == "" && (coin.Amount.IsNil() || coin.Amount.IsZero())
}

// workflowCoinPtr lifts a value sdk.Coin (a message/proto field) to a *sdk.Coin,
// returning nil for the zero value so the helper chain can treat it as "unset".
func workflowCoinPtr(coin sdk.Coin) *sdk.Coin {
	if workflowCoinIsUnset(coin) {
		return nil
	}
	return &coin
}

// quoteCoinToSDK converts the JSON-stable QuoteCoin (string amount) into a
// value sdk.Coin with a math.Int amount.
func quoteCoinToSDK(coin types.QuoteCoin) (sdk.Coin, error) {
	amount, ok := math.NewIntFromString(strings.TrimSpace(coin.Amount))
	if !ok {
		return sdk.Coin{}, types.ErrInvalidWorkflow.Wrapf("quote coin amount must be an integer: %q", coin.Amount)
	}
	return sdk.Coin{Denom: strings.TrimSpace(coin.Denom), Amount: amount}, nil
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func removeString(values []string, needle string) []string {
	needle = strings.TrimSpace(needle)
	out := values[:0]
	for _, value := range values {
		if strings.TrimSpace(value) == needle {
			continue
		}
		out = append(out, value)
	}
	return uniqueSortedStrings(out)
}
