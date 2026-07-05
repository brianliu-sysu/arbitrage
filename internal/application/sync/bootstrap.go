package syncapp

// NeedsChainRebootstrap reports whether persisted pool state is too stale for snapshot restore.
func NeedsChainRebootstrap(lastBlockNumber, blockNumber, threshold uint64) bool {
	if threshold == 0 || blockNumber <= lastBlockNumber {
		return false
	}
	return blockNumber-lastBlockNumber > threshold
}
