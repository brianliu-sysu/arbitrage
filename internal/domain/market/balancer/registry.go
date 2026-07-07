package balancer

import "context"

// PoolRegistry defines which Balancer pools the system should track and sync.
type PoolRegistry interface {
	List(ctx context.Context) ([]PoolID, error)
	GetSpec(ctx context.Context, id PoolID) (PoolSpec, error)
	Add(ctx context.Context, id PoolID, spec PoolSpec) error
	Remove(ctx context.Context, id PoolID) error
}
