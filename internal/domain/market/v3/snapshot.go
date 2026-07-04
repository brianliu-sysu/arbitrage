package v3

import (
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

// Snapshot is a point-in-time copy of V3 pool market state at a block height.
type Snapshot struct {
	PoolAddress common.Address
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
		PoolAddress: pool.Address,
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
