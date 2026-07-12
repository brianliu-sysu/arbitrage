package blockchain

import "github.com/ethereum/go-ethereum/common"

// MarketVersion identifies one canonical committed market state.
type MarketVersion struct {
	Number     uint64
	Hash       common.Hash
	Generation uint64
}

func (v MarketVersion) IsZero() bool { return v.Number == 0 }

func (v MarketVersion) SameBlock(other MarketVersion) bool {
	return v.Number == other.Number && v.Hash == other.Hash
}
