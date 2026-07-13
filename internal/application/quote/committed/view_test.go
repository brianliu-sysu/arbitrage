package committed

import (
	"context"
	"strings"
	"testing"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketuniv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	"github.com/ethereum/go-ethereum/common"
)

func TestViewCommitAcceptsTypedNilRegistry(t *testing.T) {
	var registry *syncapp.PoolLifecycleService[common.Address]
	view := NewView(Sources{Univ3Registry: registry})

	if err := view.Commit(context.Background(), domainchain.MarketVersion{Number: 10, Generation: 1}, nil, nil, nil, nil, nil); err != nil {
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
	if err := view.Commit(context.Background(), domainchain.MarketVersion{Number: 10, Generation: 1}, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("commit block 10: %v", err)
	}

	source.pools[poolA].LastBlockNumber = 11
	committedA, err := view.Univ3Repository().Get(context.Background(), poolA)
	if err != nil {
		t.Fatalf("read committed pool: %v", err)
	}
	if committedA.LastBlockNumber != 10 {
		t.Fatalf("staging mutation leaked into committed view: got block %d", committedA.LastBlockNumber)
	}
	if err := view.Commit(context.Background(), domainchain.MarketVersion{Number: 11, Generation: 2}, []common.Address{poolA, poolB}, nil, nil, nil, nil); err == nil {
		t.Fatal("expected incomplete block commit to fail")
	}
	if view.BlockNumber() != 10 {
		t.Fatalf("failed commit replaced active view: got block %d", view.BlockNumber())
	}

	if err := view.Commit(context.Background(), domainchain.MarketVersion{Number: 11, Generation: 2}, []common.Address{poolA}, nil, nil, nil, nil); err != nil {
		t.Fatalf("commit block 11: %v", err)
	}
	committedA, _ = view.Univ3Repository().Get(context.Background(), poolA)
	committedB, err := view.Univ3Repository().Get(context.Background(), poolB)
	if err != nil {
		t.Fatalf("read committed pool B: %v", err)
	}
	if committedA.LastBlockNumber != 11 || committedB.LastBlockNumber != 10 {
		t.Fatalf("copy-on-write replaced unchanged pool: A=%d B=%d", committedA.LastBlockNumber, committedB.LastBlockNumber)
	}
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
	err := view.Commit(context.Background(), domainchain.MarketVersion{Number: 10, Generation: 1}, nil, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected commit failure")
	}
	if !strings.Contains(err.Error(), "want 10") {
		t.Fatalf("unexpected error: %v", err)
	}
}
