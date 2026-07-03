package blockchain

import "fmt"

// BlockRange is an inclusive block interval.
type BlockRange struct {
	From uint64
	To   uint64
}

func NewBlockRange(from, to uint64) (BlockRange, error) {
	if to < from {
		return BlockRange{}, fmt.Errorf("invalid block range: from %d to %d", from, to)
	}
	return BlockRange{From: from, To: to}, nil
}

func (r BlockRange) Contains(blockNumber uint64) bool {
	return blockNumber >= r.From && blockNumber <= r.To
}
