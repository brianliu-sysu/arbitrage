package syncapp

import (
	"context"
	"math/big"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

type reorgTestPool struct {
	status market.PoolStatus
}

type reorgTestBlockReader struct{}

func (reorgTestBlockReader) GetBlockHeader(_ context.Context, blockNumber uint64) (blockchain.BlockHeader, error) {
	return blockchain.BlockHeader{Number: blockNumber, Hash: common.BigToHash(new(big.Int).SetUint64(blockNumber))}, nil
}

func (reorgTestBlockReader) GetLatestBlockHeader(context.Context) (blockchain.BlockHeader, error) {
	return blockchain.BlockHeader{}, nil
}

func TestReorgRecoverySuppressesReplayNotificationsAndReportsOnce(t *testing.T) {
	pools := map[int]*reorgTestPool{1: {}, 2: {}}
	applyCalls := 0
	notifyCalls := 0
	var notifiedPools []int
	service := NewReorgRecoveryService(10, reorgTestBlockReader{}, ReorgRecoveryHooks[int, uint64, *reorgTestPool]{
		FormatPoolID:         func(id int) string { return string(rune('0' + id)) },
		DeleteSnapshotsAfter: func(context.Context, int, uint64) error { return nil },
		LoadPool: func(_ context.Context, id int) (*reorgTestPool, error) {
			return pools[id], nil
		},
		SavePool:         func(context.Context, *reorgTestPool) error { return nil },
		IsNilPool:        func(pool *reorgTestPool) bool { return pool == nil },
		RestorePoolState: func(context.Context, *reorgTestPool, int, uint64) (uint64, error) { return 10, nil },
		SetPoolStatus:    func(pool *reorgTestPool, status market.PoolStatus) { pool.status = status },
		SetPoolReady:     func(int, bool) {},
		FetchReplayLogs:  func(context.Context, int, uint64, uint64) ([]RawLog, error) { return nil, nil },
		ParseEvents:      func([]RawLog) ([]uint64, error) { return nil, nil },
		EventBlockNumber: func(event uint64) uint64 { return event },
		ApplyBlock: func(_ context.Context, _ uint64, _ common.Hash, _ []uint64, _ []int, suppressListener bool) error {
			applyCalls++
			if !suppressListener {
				t.Fatal("reorg replay must suppress per-pool listener notifications")
			}
			return nil
		},
		NotifyRecovered: func(_ context.Context, blockNumber uint64, ids []int) error {
			notifyCalls++
			if blockNumber != 11 {
				t.Fatalf("expected recovery notification at block 11, got %d", blockNumber)
			}
			notifiedPools = append([]int(nil), ids...)
			return nil
		},
	})

	err := service.Recover(context.Background(), blockchain.Reorg{
		CommonAncestor: 9,
		RemoteHead:     blockchain.BlockHeader{Number: 11},
	}, []int{1, 2})
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if applyCalls != 4 {
		t.Fatalf("expected four silent replay calls, got %d", applyCalls)
	}
	if notifyCalls != 1 || len(notifiedPools) != 2 {
		t.Fatalf("expected one aggregate notification for both pools, calls=%d pools=%v", notifyCalls, notifiedPools)
	}
	for id, pool := range pools {
		if pool.status != market.PoolStatusReady {
			t.Fatalf("expected pool %d ready before aggregate notification, got %s", id, pool.status)
		}
	}
}
