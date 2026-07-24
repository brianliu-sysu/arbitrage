package marketstore

import (
	"context"
	"sync"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/marketchange"
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
	Univ3Registry     Registry[common.Address]
	PancakeRegistry   Registry[common.Address]
	QuickSwapRegistry Registry[common.Address]
	Univ4Registry     Registry[marketuniv4.PoolID]
	BalancerRegistry  Registry[marketbalancer.PoolID]
}

// Registry is the read-only pool membership required to build a committed view.
type Registry[PoolID comparable] interface {
	List(context.Context) ([]PoolID, error)
}

// Changes identifies the pools that must be refreshed for one market version.
type Changes = marketchange.Changes

// PublishListener observes a market version after its snapshot becomes visible.
type PublishListener interface {
	AfterMarketPublished(domainchain.MarketVersion, Changes)
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
	mu       sync.RWMutex
	sources  Sources
	active   snapshot
	logger   *zap.Logger
	listener PublishListener
}

// SetPublishListener configures work that starts after the snapshot is visible.
func (v *View) SetPublishListener(listener PublishListener) {
	if v == nil {
		return
	}
	v.listener = listener
}

func NewView(sources Sources) *View {
	return &View{sources: sources, active: emptySnapshot(), logger: zap.NewNop()}
}

// Store is the atomically published in-memory market state.
type Store = View

// NewStore constructs the live market state store.
func NewStore(sources Sources) *Store {
	return NewView(sources)
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

// Publish atomically exposes a fully applied market block to quote and arbitrage readers.
func (v *View) Publish(
	ctx context.Context,
	version domainchain.MarketVersion,
	changes Changes,
) error {
	return v.commit(ctx, version, changes)
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
