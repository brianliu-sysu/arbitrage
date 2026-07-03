package market

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

// PoolRegistry defines which pools the system should track and sync.
type PoolRegistry interface {
	List(ctx context.Context) ([]common.Address, error)
	Add(ctx context.Context, address common.Address) error
	Remove(ctx context.Context, address common.Address) error
}
