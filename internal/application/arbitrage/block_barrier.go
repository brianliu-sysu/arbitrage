package arbitrageapp

import (
	"sync"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/marketchange"
)

// MarketChanges contains all pools changed by one unified market block.
type MarketChanges = marketchange.Changes

type protocolBlockState struct {
	changes  MarketChanges
	reported bool
}

type pendingBlock struct {
	byProtocol map[SyncProtocol]*protocolBlockState
}

type blockBarrier struct {
	mu          sync.Mutex
	enabled     map[SyncProtocol]struct{}
	pending     map[uint64]*pendingBlock
	flushing    map[uint64]struct{}
	lastVersion domainchain.MarketVersion
	prepared    domainchain.MarketVersion
	generation  uint64
}

func newBlockBarrier(enabled []SyncProtocol) *blockBarrier {
	protocols := make(map[SyncProtocol]struct{}, len(enabled))
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
		block = &pendingBlock{byProtocol: make(map[SyncProtocol]*protocolBlockState)}
		b.pending[report.BlockNumber] = block
	}
	state := block.byProtocol[report.Protocol]
	if state == nil {
		state = &protocolBlockState{}
		block.byProtocol[report.Protocol] = state
	}
	state.reported = true
	state.changes = mergeMarketChanges(state.changes, report.Changes)
}

func (b *blockBarrier) prepare(head domainchain.BlockHeader) (domainchain.MarketVersion, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.prepared.IsZero() && b.prepared.Number == head.Number && b.prepared.Hash == head.Hash {
		return b.prepared, false
	}
	b.generation++
	b.prepared = domainchain.MarketVersion{Number: head.Number, Hash: head.Hash, Generation: b.generation}
	return b.prepared, true
}

func (b *blockBarrier) beginFinalize(head domainchain.BlockHeader) (domainchain.MarketVersion, MarketChanges, int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	enabledCount := len(b.enabled)
	if !b.isReadyLocked(head.Number) {
		return domainchain.MarketVersion{}, MarketChanges{}, enabledCount, false
	}
	if _, flushing := b.flushing[head.Number]; flushing {
		return domainchain.MarketVersion{}, MarketChanges{}, enabledCount, false
	}
	if b.prepared.Number != head.Number {
		b.generation++
		b.prepared = domainchain.MarketVersion{Number: head.Number, Hash: head.Hash, Generation: b.generation}
	}
	b.flushing[head.Number] = struct{}{}
	return b.prepared, b.collectChangedLocked(head.Number), enabledCount, true
}

func (b *blockBarrier) abortFinalize(blockNumber uint64) {
	b.mu.Lock()
	delete(b.flushing, blockNumber)
	b.mu.Unlock()
}

func (b *blockBarrier) complete(version domainchain.MarketVersion, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.flushing, version.Number)
	if err != nil || b.lastVersion.SameBlock(version) {
		return
	}
	b.lastVersion = version
	delete(b.pending, version.Number)
	for number := range b.pending {
		if number < version.Number {
			delete(b.pending, number)
		}
	}
}

func (b *blockBarrier) isReadyLocked(blockNumber uint64) bool {
	block := b.pending[blockNumber]
	if block == nil {
		return false
	}
	if len(b.enabled) == 0 {
		for _, state := range block.byProtocol {
			if state != nil && state.reported {
				return true
			}
		}
		return false
	}
	for protocol := range b.enabled {
		state := block.byProtocol[protocol]
		if state == nil || !state.reported {
			return false
		}
	}
	return true
}

func (b *blockBarrier) collectChangedLocked(blockNumber uint64) MarketChanges {
	block := b.pending[blockNumber]
	if block == nil {
		return MarketChanges{}
	}
	var changes MarketChanges
	for _, state := range block.byProtocol {
		if state != nil {
			changes = mergeMarketChanges(changes, state.changes)
		}
	}
	return changes
}

func mergeMarketChanges(dst, src MarketChanges) MarketChanges {
	dst.Univ3 = mergeAddresses(dst.Univ3, src.Univ3)
	dst.PancakeV3 = mergeAddresses(dst.PancakeV3, src.PancakeV3)
	dst.QuickSwapV3 = mergeAddresses(dst.QuickSwapV3, src.QuickSwapV3)
	dst.Univ4 = mergeV4IDs(dst.Univ4, src.Univ4)
	dst.Balancer = mergeBalancerIDs(dst.Balancer, src.Balancer)
	return dst
}
