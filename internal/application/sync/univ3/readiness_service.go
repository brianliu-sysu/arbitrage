package syncv3

import (
	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/ethereum/go-ethereum/common"
)

type ReadinessService = syncapp.ReadinessService[common.Address]

func NewReadinessService() *ReadinessService {
	return syncapp.NewReadinessService[common.Address]()
}
