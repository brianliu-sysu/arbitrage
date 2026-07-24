package syncv4

import (
	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
)

type ReadinessService = syncapp.ReadinessService[marketv4.PoolID]

func NewReadinessService() *ReadinessService {
	return syncapp.NewReadinessService[marketv4.PoolID]()
}
