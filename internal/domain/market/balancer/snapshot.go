package balancer

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Snapshot is a point-in-time copy of Balancer pool market state.
type Snapshot struct {
	PoolID            PoolID
	BlockNumber       uint64
	Tokens            []common.Address
	Balances          map[common.Address]*big.Int
	Weights           map[common.Address]*big.Int
	Amplification     *big.Int
	SwapFeePercentage *big.Int
	CreatedAt         time.Time
}

func NewSnapshot(pool *Pool, blockNumber uint64, createdAt time.Time) *Snapshot {
	if pool == nil {
		return nil
	}
	return &Snapshot{
		PoolID:            pool.ID,
		BlockNumber:       blockNumber,
		Tokens:            cloneAddresses(pool.Tokens),
		Balances:          cloneIntMap(pool.Balances),
		Weights:           cloneIntMap(pool.Weights),
		Amplification:     cloneInt(pool.Amplification),
		SwapFeePercentage: cloneInt(pool.SwapFeePercentage),
		CreatedAt:         createdAt,
	}
}

func (s *Snapshot) RestoreTo(pool *Pool) {
	if s == nil || pool == nil {
		return
	}
	pool.Tokens = cloneAddresses(s.Tokens)
	pool.Balances = cloneIntMap(s.Balances)
	pool.Weights = cloneIntMap(s.Weights)
	pool.Amplification = cloneInt(s.Amplification)
	pool.SwapFeePercentage = cloneInt(s.SwapFeePercentage)
	pool.LastBlockNumber = s.BlockNumber
}
