package syncapp

import "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"

// NeedsChainRebootstrap reports whether persisted pool state is too stale for snapshot restore.
func NeedsChainRebootstrap(lastBlockNumber, blockNumber, threshold uint64) bool {
	if threshold == 0 || blockNumber <= lastBlockNumber {
		return false
	}
	return blockNumber-lastBlockNumber > threshold
}

// NeedsHeadGapCatchup reports whether live head notifications skipped one or more blocks.
func NeedsHeadGapCatchup(localHead, remoteHead blockchain.BlockHeader) bool {
	return localHead.Number > 0 && remoteHead.Number > localHead.Number+1
}
