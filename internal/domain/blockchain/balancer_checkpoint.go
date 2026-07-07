package blockchain

import (
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

// BalancerCheckpoint records how far a Balancer pool has been synced locally.
type BalancerCheckpoint struct {
	PoolID      marketbalancer.PoolID
	BlockNumber uint64
	BlockHash   common.Hash
}
