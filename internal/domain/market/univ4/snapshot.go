package univ4

import (
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
)

// Snapshot is a point-in-time copy of V4 pool market state at a block height.
type Snapshot struct {
	PoolID      PoolID
	BlockNumber uint64
	State       market.PoolState
	Ticks       market.TickTable
	Bitmap      market.TickBitmap
	CreatedAt   time.Time
}

func NewSnapshot(pool *Pool, blockNumber uint64, createdAt time.Time) *Snapshot {
	if pool == nil {
		return nil
	}
	return &Snapshot{
		PoolID:      pool.ID,
		BlockNumber: blockNumber,
		State:       pool.State.Clone(),
		Ticks:       pool.Ticks.Clone(),
		Bitmap:      pool.Bitmap.Clone(),
		CreatedAt:   createdAt,
	}
}

func (s *Snapshot) RestoreTo(pool *Pool) {
	if s == nil || pool == nil {
		return
	}
	pool.State = s.State.Clone()
	pool.Ticks = s.Ticks.Clone()
	pool.Bitmap = s.Bitmap.Clone()
	pool.LastBlockNumber = s.BlockNumber
}
