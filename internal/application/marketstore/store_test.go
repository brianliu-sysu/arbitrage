package marketstore

import (
	"context"
	"strings"
	"testing"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	"github.com/ethereum/go-ethereum/common"
)

func TestViewCommitAcceptsTypedNilRegistry(t *testing.T) {
	var registry *syncapp.PoolLifecycleService[common.Address]
	view := NewView(Sources{Univ3Registry: registry})

	if err := view.Publish(context.Background(), domainchain.MarketVersion{Number: 10, Generation: 1}, Changes{}); err != nil {
		t.Fatalf("commit with typed nil registry: %v", err)
	}
}

type testUniv3Repository struct {
	pools map[common.Address]*marketuniv3.Pool
}

func (r *testUniv3Repository) Save(_ context.Context, pool *marketuniv3.Pool) error {
	r.pools[pool.Address] = pool.Clone()
	return nil
}

func (r *testUniv3Repository) Get(_ context.Context, id common.Address) (*marketuniv3.Pool, error) {
	if pool := r.pools[id]; pool != nil {
		return pool.Clone(), nil
	}
	return nil, nil
}

func (r *testUniv3Repository) Delete(_ context.Context, id common.Address) error {
	delete(r.pools, id)
	return nil
}

func (r *testUniv3Repository) AdvanceSyncProgress(ctx context.Context, id common.Address, blockNumber uint64) error {
	return r.AdvanceSyncProgressMany(ctx, []common.Address{id}, blockNumber)
}

func (r *testUniv3Repository) AdvanceSyncProgressMany(_ context.Context, ids []common.Address, blockNumber uint64) error {
	for _, id := range ids {
		if pool := r.pools[id]; pool != nil {
			pool.LastBlockNumber = blockNumber
		}
	}
	return nil
}

type testAddressRegistry struct{ ids []common.Address }

func (r testAddressRegistry) List(context.Context) ([]common.Address, error) {
	return append([]common.Address(nil), r.ids...), nil
}
func (testAddressRegistry) Add(context.Context, common.Address) error    { return nil }
func (testAddressRegistry) Remove(context.Context, common.Address) error { return nil }

type recordingPublishObserver struct {
	publications []Publication
}

func (o *recordingPublishObserver) AfterMarketPublished(_ context.Context, publication Publication) {
	o.publications = append(o.publications, publication)
}

func TestTopologyVersionChangesOnlyWithPublishedPoolGraph(t *testing.T) {
	poolA := common.HexToAddress("0x0000000000000000000000000000000000000011")
	poolB := common.HexToAddress("0x0000000000000000000000000000000000000022")
	token0 := common.HexToAddress("0x0000000000000000000000000000000000000001")
	token1 := common.HexToAddress("0x0000000000000000000000000000000000000002")
	source := &testUniv3Repository{pools: map[common.Address]*marketuniv3.Pool{
		poolA: marketuniv3.NewPool(poolA, token0, token1, 500, 10),
		poolB: marketuniv3.NewPool(poolB, token0, token1, 3000, 60),
	}}
	source.pools[poolA].LastBlockNumber = 10
	source.pools[poolB].LastBlockNumber = 12
	registry := &testAddressRegistry{ids: []common.Address{poolA}}
	observer := &recordingPublishObserver{}
	view := NewView(Sources{Univ3Pools: source, Univ3Registry: registry})
	view.SetPublishObservers(observer)

	if err := view.Publish(context.Background(), domainchain.MarketVersion{Number: 10}, Changes{}); err != nil {
		t.Fatalf("publish initial topology: %v", err)
	}
	if got := view.Snapshot().TopologyVersion(); got != 1 {
		t.Fatalf("initial topology version = %d, want 1", got)
	}

	source.pools[poolA].LastBlockNumber = 11
	if err := view.Publish(context.Background(), domainchain.MarketVersion{Number: 11}, Changes{Univ3: []common.Address{poolA}}); err != nil {
		t.Fatalf("publish state-only update: %v", err)
	}
	if got := view.Snapshot().TopologyVersion(); got != 1 {
		t.Fatalf("state-only topology version = %d, want 1", got)
	}

	registry.ids = append(registry.ids, poolB)
	if err := view.Publish(context.Background(), domainchain.MarketVersion{Number: 12}, Changes{Univ3: []common.Address{poolB}}); err != nil {
		t.Fatalf("publish added pool: %v", err)
	}
	if got := view.Snapshot().TopologyVersion(); got != 2 {
		t.Fatalf("added-pool topology version = %d, want 2", got)
	}
	if len(observer.publications) != 3 ||
		!observer.publications[0].TopologyChanged ||
		observer.publications[1].TopologyChanged ||
		!observer.publications[2].TopologyChanged {
		t.Fatalf("unexpected topology publications: %+v", observer.publications)
	}
}

func TestViewKeepsOldSnapshotUntilCompleteBlockCommits(t *testing.T) {
	poolA := common.HexToAddress("0x0000000000000000000000000000000000000011")
	poolB := common.HexToAddress("0x0000000000000000000000000000000000000022")
	token0 := common.HexToAddress("0x0000000000000000000000000000000000000001")
	token1 := common.HexToAddress("0x0000000000000000000000000000000000000002")
	source := &testUniv3Repository{pools: map[common.Address]*marketuniv3.Pool{
		poolA: marketuniv3.NewPool(poolA, token0, token1, 500, 10),
		poolB: marketuniv3.NewPool(poolB, token0, token1, 3000, 60),
	}}
	source.pools[poolA].LastBlockNumber = 10
	source.pools[poolB].LastBlockNumber = 10
	view := NewView(Sources{
		Univ3Pools:    source,
		Univ3Registry: testAddressRegistry{ids: []common.Address{poolA, poolB}},
	})
	if err := view.Publish(context.Background(), domainchain.MarketVersion{Number: 10, Generation: 1}, Changes{}); err != nil {
		t.Fatalf("commit block 10: %v", err)
	}

	source.pools[poolA].LastBlockNumber = 11
	routeA := quoteunified.NewDirectV3Route(poolA, token0, token1)
	committedPools, err := view.Snapshot().LoadRoutePools(context.Background(), routeA)
	if err != nil {
		t.Fatalf("read committed pool: %v", err)
	}
	committedA := committedPools.V3[poolA]
	if committedA.LastBlockNumber != 10 {
		t.Fatalf("staging mutation leaked into committed view: got block %d", committedA.LastBlockNumber)
	}
	if err := view.Publish(context.Background(), domainchain.MarketVersion{Number: 11, Generation: 2}, Changes{Univ3: []common.Address{poolA, poolB}}); err == nil {
		t.Fatal("expected incomplete block commit to fail")
	}
	if view.Snapshot().Version().Number != 10 {
		t.Fatalf("failed commit replaced active view: got block %d", view.Snapshot().Version().Number)
	}

	if err := view.Publish(context.Background(), domainchain.MarketVersion{Number: 11, Generation: 2}, Changes{Univ3: []common.Address{poolA}}); err != nil {
		t.Fatalf("commit block 11: %v", err)
	}
	committedPools, err = view.Snapshot().LoadRoutePools(context.Background(), quoteunified.Route{
		TokenIn: token0, TokenOut: token1,
		Hops: []quoteunified.RouteHop{
			{Version: quoteunified.PoolVersionV3, PoolV3: poolA, TokenIn: token0, TokenOut: token1},
			{Version: quoteunified.PoolVersionV3, PoolV3: poolB, TokenIn: token0, TokenOut: token1},
		},
	})
	if err != nil {
		t.Fatalf("read committed pools: %v", err)
	}
	committedA = committedPools.V3[poolA]
	committedB := committedPools.V3[poolB]
	if committedA.LastBlockNumber != 11 || committedB.LastBlockNumber != 10 {
		t.Fatalf("copy-on-write replaced unchanged pool: A=%d B=%d", committedA.LastBlockNumber, committedB.LastBlockNumber)
	}
}

func TestSnapshotRemainsPinnedAfterNewVersionPublishes(t *testing.T) {
	poolID := common.HexToAddress("0x0000000000000000000000000000000000000011")
	token0 := common.HexToAddress("0x0000000000000000000000000000000000000001")
	token1 := common.HexToAddress("0x0000000000000000000000000000000000000002")
	source := &testUniv3Repository{pools: map[common.Address]*marketuniv3.Pool{
		poolID: marketuniv3.NewPool(poolID, token0, token1, 500, 10),
	}}
	source.pools[poolID].LastBlockNumber = 10
	view := NewView(Sources{
		Univ3Pools:    source,
		Univ3Registry: testAddressRegistry{ids: []common.Address{poolID}},
	})
	if err := view.Publish(context.Background(), domainchain.MarketVersion{Number: 10, Generation: 1}, Changes{}); err != nil {
		t.Fatalf("publish block 10: %v", err)
	}
	block10 := view.Snapshot()

	source.pools[poolID].LastBlockNumber = 11
	if err := view.Publish(context.Background(), domainchain.MarketVersion{Number: 11, Generation: 2}, Changes{Univ3: []common.Address{poolID}}); err != nil {
		t.Fatalf("publish block 11: %v", err)
	}

	route := quoteunified.NewDirectV3Route(poolID, token0, token1)
	pools, err := block10.LoadRoutePools(context.Background(), route)
	if err != nil {
		t.Fatalf("load pools from pinned snapshot: %v", err)
	}
	if block10.Version().Number != 10 || pools.V3[poolID].LastBlockNumber != 10 {
		t.Fatalf("snapshot advanced after publish: version=%d pool=%d", block10.Version().Number, pools.V3[poolID].LastBlockNumber)
	}
	if view.Snapshot().Version().Number != 11 {
		t.Fatalf("active snapshot did not advance: version=%d", view.Snapshot().Version().Number)
	}
}

func TestSnapshotRegistryChangesOnlyAfterPublish(t *testing.T) {
	poolA := common.HexToAddress("0x0000000000000000000000000000000000000011")
	poolB := common.HexToAddress("0x0000000000000000000000000000000000000022")
	token0 := common.HexToAddress("0x0000000000000000000000000000000000000001")
	token1 := common.HexToAddress("0x0000000000000000000000000000000000000002")
	source := &testUniv3Repository{pools: map[common.Address]*marketuniv3.Pool{
		poolA: marketuniv3.NewPool(poolA, token0, token1, 500, 10),
		poolB: marketuniv3.NewPool(poolB, token0, token1, 3000, 60),
	}}
	source.pools[poolA].LastBlockNumber = 10
	source.pools[poolB].LastBlockNumber = 10
	liveRegistry := &testAddressRegistry{ids: []common.Address{poolA, poolB}}
	view := NewView(Sources{Univ3Pools: source, Univ3Registry: liveRegistry})

	if err := view.Publish(context.Background(), domainchain.MarketVersion{Number: 10, Generation: 1}, Changes{}); err != nil {
		t.Fatalf("publish initial snapshot: %v", err)
	}
	liveRegistry.ids = []common.Address{poolA}

	edges := view.Snapshot().PoolEdges()
	if !containsV3Pool(edges, poolA) || !containsV3Pool(edges, poolB) {
		t.Fatalf("live registry mutation leaked into committed graph: %v", edges)
	}

	source.pools[poolA].LastBlockNumber = 11
	if err := view.Publish(context.Background(), domainchain.MarketVersion{Number: 11, Generation: 2}, Changes{Univ3: []common.Address{poolA}}); err != nil {
		t.Fatalf("publish updated snapshot: %v", err)
	}
	edges = view.Snapshot().PoolEdges()
	if len(edges) != 1 || !containsV3Pool(edges, poolA) {
		t.Fatalf("unexpected committed graph after publish: %v", edges)
	}
}

func containsV3Pool(edges []quoteunified.PoolEdge, target common.Address) bool {
	for _, edge := range edges {
		if edge.Version == quoteunified.PoolVersionV3 && edge.PoolV3 == target {
			return true
		}
	}
	return false
}

func TestViewCommitReportsFirstMismatchButScansRemaining(t *testing.T) {
	poolA := common.HexToAddress("0x0000000000000000000000000000000000000011")
	poolB := common.HexToAddress("0x0000000000000000000000000000000000000022")
	token0 := common.HexToAddress("0x0000000000000000000000000000000000000001")
	token1 := common.HexToAddress("0x0000000000000000000000000000000000000002")
	source := &testUniv3Repository{pools: map[common.Address]*marketuniv3.Pool{
		poolA: marketuniv3.NewPool(poolA, token0, token1, 500, 10),
		poolB: marketuniv3.NewPool(poolB, token0, token1, 3000, 60),
	}}
	source.pools[poolA].LastBlockNumber = 5
	source.pools[poolB].LastBlockNumber = 7
	view := NewView(Sources{
		Univ3Pools:    source,
		Univ3Registry: testAddressRegistry{ids: []common.Address{poolA, poolB}},
	})
	err := view.Publish(context.Background(), domainchain.MarketVersion{Number: 10, Generation: 1}, Changes{})
	if err == nil {
		t.Fatal("expected commit failure")
	}
	if !strings.Contains(err.Error(), "want 10") {
		t.Fatalf("unexpected error: %v", err)
	}
}
