package market

// PoolStatus represents the lifecycle state of a pool in the sync pipeline.
type PoolStatus string

const (
	PoolStatusUnknown       PoolStatus = "unknown"
	PoolStatusBootstrapping PoolStatus = "bootstrapping"
	PoolStatusCatchingUp    PoolStatus = "catching_up"
	PoolStatusSyncing       PoolStatus = "syncing"
	PoolStatusReady         PoolStatus = "ready"
	PoolStatusStopped       PoolStatus = "stopped"
	PoolStatusError         PoolStatus = "error"
)

func (s PoolStatus) IsReady() bool {
	return s == PoolStatusReady
}
