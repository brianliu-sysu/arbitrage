package balancersync

import (
	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
)

type ReadinessService = syncapp.ReadinessService[marketbalancer.PoolID]

func NewReadinessService() *ReadinessService {
	return syncapp.NewReadinessService[marketbalancer.PoolID]()
}
