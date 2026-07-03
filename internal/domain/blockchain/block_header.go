package blockchain

import "github.com/ethereum/go-ethereum/common"

// BlockHeader is a minimal block identity used for sync and reorg detection.
type BlockHeader struct {
	Number     uint64
	Hash       common.Hash
	ParentHash common.Hash
}
