package syncapp

// ReorgReplayFromBlock returns the first block to replay after restoring reorg state.
// When a snapshot exists, replay must continue from snapshot.BlockNumber+1 so events
// between the snapshot and the common ancestor are not skipped.
func ReorgReplayFromBlock(snapshotBlock, commonAncestor uint64, hasSnapshot bool) uint64 {
	if hasSnapshot {
		return snapshotBlock + 1
	}
	return commonAncestor + 1
}
