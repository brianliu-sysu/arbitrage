package univ3

import (
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
)

// Snapshot is a point-in-time copy of Uniswap V3 pool market state at a block height.
type Snapshot = clv3.Snapshot

func NewSnapshot(pool *Pool, blockNumber uint64, createdAt time.Time) *Snapshot {
	return clv3.NewSnapshot(&pool.Pool, blockNumber, createdAt)
}

func RestoreSnapshot(snapshot *Snapshot, pool *Pool) {
	if snapshot == nil || pool == nil {
		return
	}
	snapshot.RestoreTo(&pool.Pool)
}
