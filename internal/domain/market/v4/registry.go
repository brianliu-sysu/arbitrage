package v4

import (
	"context"
)

// PoolRegistry defines which V4 pools the system should track and sync.
type PoolRegistry interface {
	List(ctx context.Context) ([]PoolID, error)
	GetKey(ctx context.Context, id PoolID) (PoolKey, error)
	Add(ctx context.Context, id PoolID, key PoolKey) error
	Remove(ctx context.Context, id PoolID) error
}
