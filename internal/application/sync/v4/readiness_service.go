package syncv4

import (
	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
)

type ReadinessService = syncapp.ReadinessService[marketv4.PoolID]

func NewReadinessService() *ReadinessService {
	return syncapp.NewReadinessService[marketv4.PoolID]()
}
