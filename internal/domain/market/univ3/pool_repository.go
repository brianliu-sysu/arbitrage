package univ3

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

// PoolRepository persists Uniswap V3 pool aggregates keyed by contract address.
type PoolRepository interface {
	Save(ctx context.Context, pool *Pool) error
	Get(ctx context.Context, address common.Address) (*Pool, error)
	Delete(ctx context.Context, address common.Address) error
	AdvanceSyncProgress(ctx context.Context, address common.Address, blockNumber uint64) error
	AdvanceSyncProgressMany(ctx context.Context, addresses []common.Address, blockNumber uint64) error
}
