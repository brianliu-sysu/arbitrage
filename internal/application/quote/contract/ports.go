package contract

import "context"

// SystemReadiness gates quote operations until a committed market view exists.
type SystemReadiness interface {
	IsSystemReady() bool
}

// PoolReadiness reports whether a pool belongs to the committed quote view.
type PoolReadiness[PoolID comparable] interface {
	SystemReadiness
	IsPoolReady(PoolID) bool
}

// PoolRepository is the read-only pool state required by quoting.
type PoolRepository[PoolID comparable, Pool any] interface {
	Get(context.Context, PoolID) (*Pool, error)
}

// PoolRegistry lists pools contained in the committed quote view.
type PoolRegistry[PoolID comparable] interface {
	List(context.Context) ([]PoolID, error)
}

// ViewRevision returns the strongest committed-view revision exposed by a
// readiness implementation.
func ViewRevision(readiness SystemReadiness) uint64 {
	if versioned, ok := readiness.(interface{ Generation() uint64 }); ok {
		return versioned.Generation()
	}
	if versioned, ok := readiness.(interface{ BlockNumber() uint64 }); ok {
		return versioned.BlockNumber()
	}
	return 0
}
