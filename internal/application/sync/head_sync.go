package syncapp

import "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"

// ShouldSkipHeadNotification reports whether an incoming head was already processed.
func ShouldSkipHeadNotification(localHead, remoteHead blockchain.BlockHeader) bool {
	if localHead.Number == 0 {
		return false
	}
	if remoteHead.Number < localHead.Number {
		return true
	}
	return remoteHead.Number == localHead.Number && remoteHead.Hash == localHead.Hash
}
