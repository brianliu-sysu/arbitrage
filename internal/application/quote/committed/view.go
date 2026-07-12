package committed

import (
	"context"
	"fmt"
	"sync"
	"time"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// Sources contains live repositories and registries used to build immutable quote views.
type Sources struct {
	Univ3Pools        marketuniv3.PoolRepository
	PancakePools      marketpancake.PoolRepository
	QuickSwapPools    marketquick.PoolRepository
	Univ4Pools        marketuniv4.PoolRepository
	BalancerPools     marketbalancer.PoolRepository
	Univ3Registry     marketuniv3.PoolRegistry
	PancakeRegistry   marketpancake.PoolRegistry
	QuickSwapRegistry marketquick.PoolRegistry
	Univ4Registry     marketuniv4.PoolRegistry
	BalancerRegistry  marketbalancer.PoolRegistry
}

type snapshot struct {
	version   domainchain.MarketVersion
	univ3     map[common.Address]*marketuniv3.Pool
	pancake   map[common.Address]*marketpancake.Pool
	quickSwap map[common.Address]*marketquick.Pool
	univ4     map[marketuniv4.PoolID]*marketuniv4.Pool
	balancer  map[marketbalancer.PoolID]*marketbalancer.Pool
}

// View atomically publishes complete market snapshots for quote readers.
type View struct {
	mu      sync.RWMutex
	sources Sources
	active  snapshot
	logger  *zap.Logger
}

func NewView(sources Sources) *View {
	return &View{sources: sources, active: emptySnapshot(), logger: zap.NewNop()}
}

// SetLogger configures diagnostic logging for commit failures.
func (v *View) SetLogger(logger *zap.Logger) {
	if v == nil {
		return
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	v.logger = logger
}

func emptySnapshot() snapshot {
	return snapshot{
		univ3:     make(map[common.Address]*marketuniv3.Pool),
		pancake:   make(map[common.Address]*marketpancake.Pool),
		quickSwap: make(map[common.Address]*marketquick.Pool),
		univ4:     make(map[marketuniv4.PoolID]*marketuniv4.Pool),
		balancer:  make(map[marketbalancer.PoolID]*marketbalancer.Pool),
	}
}

// Commit builds a complete snapshot for blockNumber and atomically publishes it.
func (v *View) Commit(
	ctx context.Context,
	version domainchain.MarketVersion,
	univ3Changed []common.Address,
	pancakeChanged []common.Address,
	quickSwapChanged []common.Address,
	univ4Changed []marketuniv4.PoolID,
	balancerChanged []marketbalancer.PoolID,
) error {
	if v == nil {
		return nil
	}
	if version.Number == 0 {
		return fmt.Errorf("committed market view block number must be positive")
	}
	started := time.Now()
	v.mu.RLock()
	current := v.active
	v.mu.RUnlock()
	next := cloneSnapshot(current)
	next.version = version
	fullRegistryCommit := current.version.IsZero()
	v.logger.Debug("committed market view commit start",
		zap.Uint64("block", version.Number),
		zap.Uint64("generation", version.Generation),
		zap.String("hash", version.Hash.Hex()),
		zap.Uint64("previous_block", current.version.Number),
		zap.Uint64("previous_generation", current.version.Generation),
		zap.Bool("full_registry_commit", fullRegistryCommit),
		zap.Int("univ3_changed", len(univ3Changed)),
		zap.Int("pancakev3_changed", len(pancakeChanged)),
		zap.Int("quickswapv3_changed", len(quickSwapChanged)),
		zap.Int("univ4_changed", len(univ4Changed)),
		zap.Int("balancer_changed", len(balancerChanged)),
		zap.Int("univ3_snapshot", len(current.univ3)),
		zap.Int("pancakev3_snapshot", len(current.pancake)),
		zap.Int("quickswapv3_snapshot", len(current.quickSwap)),
		zap.Int("univ4_snapshot", len(current.univ4)),
		zap.Int("balancer_snapshot", len(current.balancer)),
	)
	var univ3Registry interface {
		List(context.Context) ([]common.Address, error)
	} = v.sources.Univ3Registry
	var pancakeRegistry interface {
		List(context.Context) ([]common.Address, error)
	} = v.sources.PancakeRegistry
	var quickSwapRegistry interface {
		List(context.Context) ([]common.Address, error)
	} = v.sources.QuickSwapRegistry
	var univ4Registry interface {
		List(context.Context) ([]marketuniv4.PoolID, error)
	} = v.sources.Univ4Registry
	var balancerRegistry interface {
		List(context.Context) ([]marketbalancer.PoolID, error)
	} = v.sources.BalancerRegistry
	if !fullRegistryCommit {
		ids, err := reconcileRegistry(ctx, v.sources.Univ3Registry, next.univ3, univ3Changed)
		if err != nil {
			return fmt.Errorf("reconcile univ3 registry: %w", err)
		}
		univ3Registry = addressIDs(ids)
		ids, err = reconcileRegistry(ctx, v.sources.PancakeRegistry, next.pancake, pancakeChanged)
		if err != nil {
			return fmt.Errorf("reconcile pancakev3 registry: %w", err)
		}
		pancakeRegistry = addressIDs(ids)
		ids, err = reconcileRegistry(ctx, v.sources.QuickSwapRegistry, next.quickSwap, quickSwapChanged)
		if err != nil {
			return fmt.Errorf("reconcile quickswapv3 registry: %w", err)
		}
		quickSwapRegistry = addressIDs(ids)
		v4Load, err := reconcileRegistry(ctx, v.sources.Univ4Registry, next.univ4, univ4Changed)
		if err != nil {
			return fmt.Errorf("reconcile univ4 registry: %w", err)
		}
		univ4Registry = v4IDs(v4Load)
		balancerLoad, err := reconcileRegistry(ctx, v.sources.BalancerRegistry, next.balancer, balancerChanged)
		if err != nil {
			return fmt.Errorf("reconcile balancer registry: %w", err)
		}
		balancerRegistry = balancerIDs(balancerLoad)
	}
	if err := loadAddressPools(ctx, v.logger, version.Number, univ3Registry, v.sources.Univ3Pools, next.univ3, addressSet(univ3Changed), fullRegistryCommit, "univ3"); err != nil {
		return err
	}
	if err := loadAddressPools(ctx, v.logger, version.Number, pancakeRegistry, v.sources.PancakePools, next.pancake, addressSet(pancakeChanged), fullRegistryCommit, "pancakev3"); err != nil {
		return err
	}
	if err := loadAddressPools(ctx, v.logger, version.Number, quickSwapRegistry, v.sources.QuickSwapPools, next.quickSwap, addressSet(quickSwapChanged), fullRegistryCommit, "quickswapv3"); err != nil {
		return err
	}
	if err := loadIDPools(ctx, v.logger, version.Number, univ4Registry, v.sources.Univ4Pools, next.univ4, v4Set(univ4Changed), fullRegistryCommit, "univ4"); err != nil {
		return err
	}
	if err := loadIDPools(ctx, v.logger, version.Number, balancerRegistry, v.sources.BalancerPools, next.balancer, balancerSet(balancerChanged), fullRegistryCommit, "balancer"); err != nil {
		return err
	}
	v.mu.Lock()
	v.active = next
	v.mu.Unlock()
	v.logger.Debug("committed market view commit done",
		zap.Uint64("block", version.Number),
		zap.Uint64("generation", version.Generation),
		zap.String("hash", version.Hash.Hex()),
		zap.Int("univ3_snapshot", len(next.univ3)),
		zap.Int("pancakev3_snapshot", len(next.pancake)),
		zap.Int("quickswapv3_snapshot", len(next.quickSwap)),
		zap.Int("univ4_snapshot", len(next.univ4)),
		zap.Int("balancer_snapshot", len(next.balancer)),
		zap.Int64("duration_ms", time.Since(started).Milliseconds()),
	)
	return nil
}

func cloneSnapshot(current snapshot) snapshot {
	next := emptySnapshot()
	for id, pool := range current.univ3 {
		next.univ3[id] = pool
	}
	for id, pool := range current.pancake {
		next.pancake[id] = pool
	}
	for id, pool := range current.quickSwap {
		next.quickSwap[id] = pool
	}
	for id, pool := range current.univ4 {
		next.univ4[id] = pool
	}
	for id, pool := range current.balancer {
		next.balancer[id] = pool
	}
	return next
}

func reconcileRegistry[ID comparable, P any](
	ctx context.Context,
	registry interface {
		List(context.Context) ([]ID, error)
	},
	existing map[ID]*P,
	changed []ID,
) ([]ID, error) {
	if registry == nil {
		return nil, nil
	}
	ids, err := registry.List(ctx)
	if err != nil {
		return nil, err
	}
	active := make(map[ID]struct{}, len(ids))
	loadSet := make(map[ID]struct{}, len(changed))
	for _, id := range changed {
		loadSet[id] = struct{}{}
	}
	for _, id := range ids {
		active[id] = struct{}{}
		if existing[id] == nil {
			loadSet[id] = struct{}{}
		}
	}
	for id := range existing {
		if _, ok := active[id]; !ok {
			delete(existing, id)
		}
	}
	toLoad := make([]ID, 0, len(loadSet))
	for id := range loadSet {
		if _, ok := active[id]; ok {
			toLoad = append(toLoad, id)
		}
	}
	return toLoad, nil
}

func (v *View) BlockNumber() uint64 {
	if v == nil {
		return 0
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.active.version.Number
}

func (v *View) Version() domainchain.MarketVersion {
	if v == nil {
		return domainchain.MarketVersion{}
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.active.version
}

func (v *View) Generation() uint64 { return v.Version().Generation }

type addressIDs []common.Address

func (ids addressIDs) List(context.Context) ([]common.Address, error) { return ids, nil }

type v4IDs []marketuniv4.PoolID

func (ids v4IDs) List(context.Context) ([]marketuniv4.PoolID, error) { return ids, nil }

type balancerIDs []marketbalancer.PoolID

func (ids balancerIDs) List(context.Context) ([]marketbalancer.PoolID, error) { return ids, nil }

func (v *View) IsSystemReady() bool { return v.BlockNumber() > 0 }

func (v *View) IsV3PoolReady(id common.Address) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.active.univ3[id] != nil
}
func (v *View) IsPancakeV3PoolReady(id common.Address) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.active.pancake[id] != nil
}
func (v *View) IsQuickSwapV3PoolReady(id common.Address) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.active.quickSwap[id] != nil
}
func (v *View) IsV4PoolReady(id marketuniv4.PoolID) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.active.univ4[id] != nil
}
func (v *View) IsBalancerPoolReady(id marketbalancer.PoolID) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.active.balancer[id] != nil
}

type addressReadiness struct {
	view *View
	kind uint8
}

func (r addressReadiness) IsSystemReady() bool { return r.view.IsSystemReady() }
func (r addressReadiness) BlockNumber() uint64 { return r.view.BlockNumber() }
func (r addressReadiness) Generation() uint64  { return r.view.Version().Generation }
func (r addressReadiness) IsPoolReady(id common.Address) bool {
	switch r.kind {
	case 1:
		return r.view.IsV3PoolReady(id)
	case 2:
		return r.view.IsPancakeV3PoolReady(id)
	default:
		return r.view.IsQuickSwapV3PoolReady(id)
	}
}

type v4Readiness struct{ view *View }

func (r v4Readiness) IsSystemReady() bool                    { return r.view.IsSystemReady() }
func (r v4Readiness) BlockNumber() uint64                    { return r.view.BlockNumber() }
func (r v4Readiness) Generation() uint64                     { return r.view.Version().Generation }
func (r v4Readiness) IsPoolReady(id marketuniv4.PoolID) bool { return r.view.IsV4PoolReady(id) }

func (v *View) Univ3Readiness() addressReadiness     { return addressReadiness{view: v, kind: 1} }
func (v *View) PancakeReadiness() addressReadiness   { return addressReadiness{view: v, kind: 2} }
func (v *View) QuickSwapReadiness() addressReadiness { return addressReadiness{view: v, kind: 3} }
func (v *View) Univ4Readiness() v4Readiness          { return v4Readiness{view: v} }

func loadAddressPools[P any](
	ctx context.Context,
	logger *zap.Logger,
	blockNumber uint64,
	registry interface {
		List(context.Context) ([]common.Address, error)
	},
	repository interface {
		Get(context.Context, common.Address) (*P, error)
	},
	dst map[common.Address]*P,
	changed map[common.Address]struct{},
	fullRegistryCommit bool,
	label string,
) error {
	if registry == nil || repository == nil {
		return nil
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	ids, err := registry.List(ctx)
	if err != nil {
		return fmt.Errorf("list %s pools: %w", label, err)
	}
	var firstErr error
	mismatches := 0
	for _, id := range ids {
		pool, err := repository.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("load %s pool %s: %w", label, id.Hex(), err)
		}
		if pool == nil {
			return fmt.Errorf("%s pool %s not found", label, id.Hex())
		}
		lastBlock, clone, status, err := addressPoolSnapshot(pool)
		if err != nil {
			return fmt.Errorf("snapshot %s pool %s: %w", label, id.Hex(), err)
		}
		if lastBlock != blockNumber {
			mismatches++
			_, inChanged := changed[id]
			_, inPreviousSnapshot := dst[id]
			logger.Debug("committed market view pool block mismatch",
				zap.String("protocol", label),
				zap.String("pool", id.Hex()),
				zap.Uint64("pool_block", lastBlock),
				zap.Uint64("want_block", blockNumber),
				zap.Int64("lag", blockLag(lastBlock, blockNumber)),
				zap.Bool("in_changed_set", inChanged),
				zap.Bool("in_previous_snapshot", inPreviousSnapshot),
				zap.Bool("full_registry_commit", fullRegistryCommit),
				zap.String("status", string(status)),
				zap.Int("load_set_size", len(ids)),
			)
			if firstErr == nil {
				firstErr = fmt.Errorf("%s pool %s is at block %d, want %d", label, id.Hex(), lastBlock, blockNumber)
			}
			continue
		}
		dst[id] = clone.(*P)
	}
	if firstErr != nil {
		logger.Debug("committed market view protocol load failed",
			zap.String("protocol", label),
			zap.Uint64("want_block", blockNumber),
			zap.Bool("full_registry_commit", fullRegistryCommit),
			zap.Int("load_set_size", len(ids)),
			zap.Int("mismatches", mismatches),
			zap.Error(firstErr),
		)
		return firstErr
	}
	return nil
}

func addressPoolSnapshot[P any](pool *P) (uint64, any, market.PoolStatus, error) {
	switch value := any(pool).(type) {
	case *marketuniv3.Pool:
		return value.LastBlockNumber, value.Clone(), value.Status, nil
	case *marketpancake.Pool:
		return value.LastBlockNumber, value.Clone(), value.Status, nil
	case *marketquick.Pool:
		return value.LastBlockNumber, value.Clone(), value.Status, nil
	default:
		return 0, nil, "", fmt.Errorf("unsupported pool type %T", pool)
	}
}

func loadIDPools[ID comparable, P any](
	ctx context.Context,
	logger *zap.Logger,
	blockNumber uint64,
	registry interface {
		List(context.Context) ([]ID, error)
	},
	repository interface {
		Get(context.Context, ID) (*P, error)
	},
	dst map[ID]*P,
	changed map[ID]struct{},
	fullRegistryCommit bool,
	label string,
) error {
	if registry == nil || repository == nil {
		return nil
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	ids, err := registry.List(ctx)
	if err != nil {
		return fmt.Errorf("list %s pools: %w", label, err)
	}
	var firstErr error
	mismatches := 0
	for _, id := range ids {
		pool, err := repository.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("load %s pool: %w", label, err)
		}
		if pool == nil {
			return fmt.Errorf("%s pool not found", label)
		}
		lastBlock, clone, status, poolKey, err := idPoolSnapshot(pool)
		if err != nil {
			return fmt.Errorf("snapshot %s pool: %w", label, err)
		}
		if lastBlock != blockNumber {
			mismatches++
			_, inChanged := changed[id]
			_, inPreviousSnapshot := dst[id]
			logger.Debug("committed market view pool block mismatch",
				zap.String("protocol", label),
				zap.String("pool", poolKey),
				zap.Uint64("pool_block", lastBlock),
				zap.Uint64("want_block", blockNumber),
				zap.Int64("lag", blockLag(lastBlock, blockNumber)),
				zap.Bool("in_changed_set", inChanged),
				zap.Bool("in_previous_snapshot", inPreviousSnapshot),
				zap.Bool("full_registry_commit", fullRegistryCommit),
				zap.String("status", string(status)),
				zap.Int("load_set_size", len(ids)),
			)
			if firstErr == nil {
				firstErr = fmt.Errorf("%s pool %s is at block %d, want %d", label, poolKey, lastBlock, blockNumber)
			}
			continue
		}
		dst[id] = clone.(*P)
	}
	if firstErr != nil {
		logger.Debug("committed market view protocol load failed",
			zap.String("protocol", label),
			zap.Uint64("want_block", blockNumber),
			zap.Bool("full_registry_commit", fullRegistryCommit),
			zap.Int("load_set_size", len(ids)),
			zap.Int("mismatches", mismatches),
			zap.Error(firstErr),
		)
		return firstErr
	}
	return nil
}

func idPoolSnapshot[P any](pool *P) (uint64, any, market.PoolStatus, string, error) {
	switch value := any(pool).(type) {
	case *marketuniv4.Pool:
		return value.LastBlockNumber, value.Clone(), value.Status, value.ID.String(), nil
	case *marketbalancer.Pool:
		return value.LastBlockNumber, value.Clone(), value.Status, value.ID.String(), nil
	default:
		return 0, nil, "", "", fmt.Errorf("unsupported pool type %T", pool)
	}
}

func blockLag(poolBlock, wantBlock uint64) int64 {
	if wantBlock >= poolBlock {
		return int64(wantBlock - poolBlock)
	}
	return -int64(poolBlock - wantBlock)
}

func addressSet(ids []common.Address) map[common.Address]struct{} {
	out := make(map[common.Address]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func v4Set(ids []marketuniv4.PoolID) map[marketuniv4.PoolID]struct{} {
	out := make(map[marketuniv4.PoolID]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func balancerSet(ids []marketbalancer.PoolID) map[marketbalancer.PoolID]struct{} {
	out := make(map[marketbalancer.PoolID]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func readOnlyError() error { return fmt.Errorf("committed market view is read-only") }

type Univ3Repository struct{ view *View }
type PancakeRepository struct{ view *View }
type QuickSwapRepository struct{ view *View }
type Univ4Repository struct{ view *View }
type BalancerRepository struct{ view *View }

func (v *View) Univ3Repository() *Univ3Repository         { return &Univ3Repository{view: v} }
func (v *View) PancakeRepository() *PancakeRepository     { return &PancakeRepository{view: v} }
func (v *View) QuickSwapRepository() *QuickSwapRepository { return &QuickSwapRepository{view: v} }
func (v *View) Univ4Repository() *Univ4Repository         { return &Univ4Repository{view: v} }
func (v *View) BalancerRepository() *BalancerRepository   { return &BalancerRepository{view: v} }

func (r *Univ3Repository) Get(_ context.Context, id common.Address) (*marketuniv3.Pool, error) {
	r.view.mu.RLock()
	defer r.view.mu.RUnlock()
	if pool := r.view.active.univ3[id]; pool != nil {
		return pool.Clone(), nil
	}
	return nil, nil
}
func (r *Univ3Repository) Save(context.Context, *marketuniv3.Pool) error { return readOnlyError() }
func (r *Univ3Repository) Delete(context.Context, common.Address) error  { return readOnlyError() }
func (r *Univ3Repository) AdvanceSyncProgress(context.Context, common.Address, uint64) error {
	return readOnlyError()
}
func (r *Univ3Repository) AdvanceSyncProgressMany(context.Context, []common.Address, uint64) error {
	return readOnlyError()
}

func (r *PancakeRepository) Get(_ context.Context, id common.Address) (*marketpancake.Pool, error) {
	r.view.mu.RLock()
	defer r.view.mu.RUnlock()
	if pool := r.view.active.pancake[id]; pool != nil {
		return pool.Clone(), nil
	}
	return nil, nil
}
func (r *PancakeRepository) Save(context.Context, *marketpancake.Pool) error { return readOnlyError() }
func (r *PancakeRepository) Delete(context.Context, common.Address) error    { return readOnlyError() }
func (r *PancakeRepository) AdvanceSyncProgress(context.Context, common.Address, uint64) error {
	return readOnlyError()
}
func (r *PancakeRepository) AdvanceSyncProgressMany(context.Context, []common.Address, uint64) error {
	return readOnlyError()
}

func (r *QuickSwapRepository) Get(_ context.Context, id common.Address) (*marketquick.Pool, error) {
	r.view.mu.RLock()
	defer r.view.mu.RUnlock()
	if pool := r.view.active.quickSwap[id]; pool != nil {
		return pool.Clone(), nil
	}
	return nil, nil
}
func (r *QuickSwapRepository) Save(context.Context, *marketquick.Pool) error { return readOnlyError() }
func (r *QuickSwapRepository) Delete(context.Context, common.Address) error  { return readOnlyError() }
func (r *QuickSwapRepository) AdvanceSyncProgress(context.Context, common.Address, uint64) error {
	return readOnlyError()
}
func (r *QuickSwapRepository) AdvanceSyncProgressMany(context.Context, []common.Address, uint64) error {
	return readOnlyError()
}

func (r *Univ4Repository) Get(_ context.Context, id marketuniv4.PoolID) (*marketuniv4.Pool, error) {
	r.view.mu.RLock()
	defer r.view.mu.RUnlock()
	if pool := r.view.active.univ4[id]; pool != nil {
		return pool.Clone(), nil
	}
	return nil, nil
}
func (r *Univ4Repository) Save(context.Context, *marketuniv4.Pool) error    { return readOnlyError() }
func (r *Univ4Repository) Delete(context.Context, marketuniv4.PoolID) error { return readOnlyError() }
func (r *Univ4Repository) AdvanceSyncProgress(context.Context, marketuniv4.PoolID, uint64) error {
	return readOnlyError()
}
func (r *Univ4Repository) AdvanceSyncProgressMany(context.Context, []marketuniv4.PoolID, uint64) error {
	return readOnlyError()
}

func (r *BalancerRepository) Get(_ context.Context, id marketbalancer.PoolID) (*marketbalancer.Pool, error) {
	r.view.mu.RLock()
	defer r.view.mu.RUnlock()
	if pool := r.view.active.balancer[id]; pool != nil {
		return pool.Clone(), nil
	}
	return nil, nil
}
func (r *BalancerRepository) Save(context.Context, *marketbalancer.Pool) error {
	return readOnlyError()
}
func (r *BalancerRepository) Delete(context.Context, marketbalancer.PoolID) error {
	return readOnlyError()
}
func (r *BalancerRepository) AdvanceSyncProgress(context.Context, marketbalancer.PoolID, uint64) error {
	return readOnlyError()
}
func (r *BalancerRepository) AdvanceSyncProgressMany(context.Context, []marketbalancer.PoolID, uint64) error {
	return readOnlyError()
}
