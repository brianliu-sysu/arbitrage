package blockchain

import "github.com/ethereum/go-ethereum/common"

// Reorg describes a chain reorganization detected during head sync.
type Reorg struct {
	DetectedAtBlock uint64
	LocalHead       BlockHeader
	RemoteHead      BlockHeader
	CommonAncestor  uint64
}

func NewReorg(detectedAt uint64, localHead, remoteHead BlockHeader, commonAncestor uint64) Reorg {
	return Reorg{
		DetectedAtBlock: detectedAt,
		LocalHead:       localHead,
		RemoteHead:      remoteHead,
		CommonAncestor:  commonAncestor,
	}
}

func (r Reorg) CommonAncestorHash() common.Hash {
	if r.LocalHead.Number == r.CommonAncestor {
		return r.LocalHead.Hash
	}
	return common.Hash{}
}
