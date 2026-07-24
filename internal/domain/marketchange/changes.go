package marketchange

import (
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// Changes identifies pools changed by one unified market version.
type Changes struct {
	Univ3       []common.Address
	PancakeV3   []common.Address
	QuickSwapV3 []common.Address
	Univ4       []marketuniv4.PoolID
	Balancer    []marketbalancer.PoolID
}
