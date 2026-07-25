package marketpipeline

import (
	"sync"

	appmetrics "github.com/brianliu-sysu/uniswapv3/internal/application/metrics"
	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/marketchange"
)

type pendingBlock struct {
	byProtocol map[Protocol]marketchange.Changes
}

type blockBarrier struct {
	mu          sync.Mutex
	enabled     map[Protocol]struct{}
	pending     map[uint64]*pendingBlock
	flushing    map[uint64]struct{}
	lastVersion domainchain.MarketVersion
	prepared    domainchain.MarketVersion
	generation  uint64
}

func newBlockBarrier(enabled []Protocol) *blockBarrier {
	protocols := make(map[Protocol]struct{}, len(enabled))
	for _, protocol := range enabled {
		if protocol != "" {
			protocols[protocol] = struct{}{}
		}
	}
	return &blockBarrier{
		enabled:  protocols,
		pending:  make(map[uint64]*pendingBlock),
		flushing: make(map[uint64]struct{}),
	}
}

func (b *blockBarrier) report(report ProtocolBlockReport) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if report.BlockNumber < b.lastVersion.Number ||
		(report.BlockNumber == b.lastVersion.Number && b.prepared.SameBlock(b.lastVersion)) {
		return
	}
	if len(b.enabled) > 0 {
		if _, ok := b.enabled[report.Protocol]; !ok {
			return
		}
	}
	block := b.pending[report.BlockNumber]
	if block == nil {
		block = &pendingBlock{byProtocol: make(map[Protocol]marketchange.Changes)}
		b.pending[report.BlockNumber] = block
	}
	block.byProtocol[report.Protocol] = mergeChanges(block.byProtocol[report.Protocol], report.Changes)
	appmetrics.SetBarrier(len(b.pending), b.lastVersion.Number)
}

func (b *blockBarrier) prepare(head domainchain.BlockHeader) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.prepared.IsZero() && b.prepared.Number == head.Number && b.prepared.Hash == head.Hash {
		return false
	}
	b.generation++
	b.prepared = domainchain.MarketVersion{Number: head.Number, Hash: head.Hash, Generation: b.generation}
	return true
}

func (b *blockBarrier) beginFinalize(head domainchain.BlockHeader) (domainchain.MarketVersion, marketchange.Changes, int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	enabledCount := len(b.enabled)
	if !b.isReadyLocked(head.Number) {
		return domainchain.MarketVersion{}, marketchange.Changes{}, enabledCount, false
	}
	if _, flushing := b.flushing[head.Number]; flushing {
		return domainchain.MarketVersion{}, marketchange.Changes{}, enabledCount, false
	}
	if b.prepared.Number != head.Number {
		b.generation++
		b.prepared = domainchain.MarketVersion{Number: head.Number, Hash: head.Hash, Generation: b.generation}
	}
	b.flushing[head.Number] = struct{}{}
	return b.prepared, b.collectChangesLocked(head.Number), enabledCount, true
}

func (b *blockBarrier) abortFinalize(blockNumber uint64) {
	b.mu.Lock()
	delete(b.flushing, blockNumber)
	b.mu.Unlock()
}

func (b *blockBarrier) complete(version domainchain.MarketVersion) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.flushing, version.Number)
	if b.lastVersion.SameBlock(version) {
		return
	}
	b.lastVersion = version
	delete(b.pending, version.Number)
	for number := range b.pending {
		if number < version.Number {
			delete(b.pending, number)
		}
	}
	appmetrics.SetBarrier(len(b.pending), b.lastVersion.Number)
}

func (b *blockBarrier) isReadyLocked(blockNumber uint64) bool {
	block := b.pending[blockNumber]
	if block == nil {
		return false
	}
	if len(b.enabled) == 0 {
		return len(block.byProtocol) > 0
	}
	for protocol := range b.enabled {
		if _, reported := block.byProtocol[protocol]; !reported {
			return false
		}
	}
	return true
}

func (b *blockBarrier) collectChangesLocked(blockNumber uint64) marketchange.Changes {
	block := b.pending[blockNumber]
	if block == nil {
		return marketchange.Changes{}
	}
	var changes marketchange.Changes
	for _, protocolChanges := range block.byProtocol {
		changes = mergeChanges(changes, protocolChanges)
	}
	return changes
}

func mergeChanges(dst, src marketchange.Changes) marketchange.Changes {
	dst.Univ3 = mergeComparable(dst.Univ3, src.Univ3)
	dst.PancakeV3 = mergeComparable(dst.PancakeV3, src.PancakeV3)
	dst.QuickSwapV3 = mergeComparable(dst.QuickSwapV3, src.QuickSwapV3)
	dst.Univ4 = mergeComparable(dst.Univ4, src.Univ4)
	dst.Balancer = mergeComparable(dst.Balancer, src.Balancer)
	return dst
}

func mergeComparable[T comparable](dst, src []T) []T {
	if len(src) == 0 {
		return dst
	}
	seen := make(map[T]struct{}, len(dst)+len(src))
	out := make([]T, 0, len(dst)+len(src))
	for _, value := range append(dst, src...) {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
