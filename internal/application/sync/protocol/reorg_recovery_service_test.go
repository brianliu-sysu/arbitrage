package protocol

import (
	"context"
	"strconv"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
)

type reorgTestPool struct {
	status market.PoolStatus
}

type reorgTestRepository struct {
	pools map[int]*reorgTestPool
}

func (r *reorgTestRepository) Get(_ context.Context, id int) (*reorgTestPool, error) {
	return r.pools[id], nil
}

func (r *reorgTestRepository) Save(context.Context, *reorgTestPool) error {
	return nil
}

type reorgTestState struct{}

func (s *reorgTestState) Restore(context.Context) error {
	return nil
}

type reorgTestCoordinator struct {
	t           *testing.T
	notifyCalls int
}

func (c *reorgTestCoordinator) CaptureRecoveryState(context.Context, []int) (ReorgRecoveryState, error) {
	return &reorgTestState{}, nil
}

func (c *reorgTestCoordinator) NotifyPoolsChanged(_ context.Context, blockNumber uint64, ids []int) error {
	c.notifyCalls++
	if blockNumber != 11 || len(ids) != 2 {
		c.t.Fatalf("unexpected recovery notification block=%d pools=%v", blockNumber, ids)
	}
	return nil
}

type reorgTestReadiness struct{}

func (r *reorgTestReadiness) SetPoolReady(int, bool) {}
func (r *reorgTestReadiness) IsPoolReady(int) bool   { return false }

type reorgTestProtocol struct{}

func (p *reorgTestProtocol) FormatPoolID(id int) string {
	return strconv.Itoa(id)
}

func (p *reorgTestProtocol) IsNilPool(pool *reorgTestPool) bool {
	return pool == nil
}

func (p *reorgTestProtocol) DeleteSnapshotsAfter(context.Context, int, uint64) error {
	return nil
}

func (p *reorgTestProtocol) RestorePoolState(
	_ context.Context,
	_ *reorgTestPool,
	id int,
	_ uint64,
) (uint64, error) {
	return uint64(8 + id), nil
}

func (p *reorgTestProtocol) SetPoolStatus(pool *reorgTestPool, status market.PoolStatus) {
	pool.status = status
}

func TestReorgRecoveryPreparesReplayPlanAndCommitsOnce(t *testing.T) {
	pools := map[int]*reorgTestPool{1: {}, 2: {}}
	coordinator := &reorgTestCoordinator{t: t}
	service := NewReorgRecoveryService(
		ReorgRecoveryDeps[int, *reorgTestPool]{
			Pools:       &reorgTestRepository{pools: pools},
			Coordinator: coordinator,
			Readiness:   &reorgTestReadiness{},
		},
		&reorgTestProtocol{},
	)

	plan, err := service.Prepare(context.Background(), blockchain.Reorg{
		CommonAncestor: 9,
		RemoteHead:     blockchain.BlockHeader{Number: 11},
	}, []int{1, 2})
	if err != nil {
		t.Fatalf("prepare recovery: %v", err)
	}
	if plan.ReplayFrom() != 9 {
		t.Fatalf("expected earliest replay block 9, got %d", plan.ReplayFrom())
	}
	if got := plan.PoolsForBlock(9); len(got) != 1 || got[0] != 1 {
		t.Fatalf("unexpected pools at block 9: %v", got)
	}
	if got := plan.PoolsForBlock(10); len(got) != 2 {
		t.Fatalf("unexpected pools at block 10: %v", got)
	}
	if err := plan.Commit(context.Background()); err != nil {
		t.Fatalf("commit recovery: %v", err)
	}
	if coordinator.notifyCalls != 1 {
		t.Fatalf("expected one aggregate notification, got %d", coordinator.notifyCalls)
	}
	for id, pool := range pools {
		if pool.status != market.PoolStatusReady {
			t.Fatalf("expected pool %d ready, got %s", id, pool.status)
		}
	}
}
