package runner

import "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"

type blockHistory struct {
	maxDepth uint64
	headers  map[uint64]blockchain.BlockHeader
}

func newBlockHistory(maxDepth uint64) *blockHistory {
	return &blockHistory{
		maxDepth: maxDepth,
		headers:  make(map[uint64]blockchain.BlockHeader, maxDepth+1),
	}
}

func (h *blockHistory) Header(blockNumber uint64) (blockchain.BlockHeader, bool) {
	if h == nil {
		return blockchain.BlockHeader{}, false
	}
	header, ok := h.headers[blockNumber]
	return header, ok
}

func (h *blockHistory) Reset(headers map[uint64]blockchain.BlockHeader) {
	clear(h.headers)
	var highest uint64
	for _, header := range headers {
		if header.Number > highest {
			highest = header.Number
		}
	}
	lowest := uint64(0)
	if highest > h.maxDepth {
		lowest = highest - h.maxDepth
	}
	for _, header := range headers {
		if header.Number >= lowest {
			h.headers[header.Number] = header
		}
	}
}

func (h *blockHistory) Commit(header blockchain.BlockHeader) {
	if h == nil {
		return
	}
	h.headers[header.Number] = header
	if header.Number <= h.maxDepth {
		return
	}
	delete(h.headers, header.Number-h.maxDepth-1)
}

func (h *blockHistory) ReplaceAfter(ancestor uint64, headers []blockchain.BlockHeader) {
	for blockNumber := range h.headers {
		if blockNumber > ancestor {
			delete(h.headers, blockNumber)
		}
	}
	for _, header := range headers {
		h.Commit(header)
	}
}
