package market

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

type PoolRepository interface {
	Save(ctx context.Context, pool *Pool) error
	Get(ctx context.Context, address common.Address) (*Pool, error)
	Delete(ctx context.Context, address common.Address) error
	// AdvanceSyncProgress updates last_block_number and catchup status without rewriting ticks/bitmap.
	AdvanceSyncProgress(ctx context.Context, address common.Address, blockNumber uint64) error
	// AdvanceSyncProgressMany batch-updates sync progress for multiple pools in one operation.
	AdvanceSyncProgressMany(ctx context.Context, addresses []common.Address, blockNumber uint64) error
}
