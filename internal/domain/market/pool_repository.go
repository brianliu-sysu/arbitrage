package market

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

type PoolRepository interface {
	Save(ctx context.Context, pool *Pool) error
	Get(ctx context.Context, address common.Address) (*Pool, error)
	Delete(ctx context.Context, address common.Address) error
}
