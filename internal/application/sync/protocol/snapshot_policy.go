package protocol

// SnapshotPolicy decides when snapshots should be created.
type SnapshotPolicy struct {
	BlockInterval uint64
}

func (p SnapshotPolicy) ShouldSnapshot(lastSnapshotBlock, currentBlock uint64) bool {
	if p.BlockInterval == 0 {
		return false
	}
	if lastSnapshotBlock == 0 {
		return currentBlock >= p.BlockInterval
	}
	return currentBlock >= lastSnapshotBlock+p.BlockInterval
}
