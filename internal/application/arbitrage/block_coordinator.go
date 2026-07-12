package arbitrageapp

import (
	"context"
	"errors"
	"sync"
	"time"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// SyncProtocol identifies a market-data sync pipeline that must align before quoting.
type SyncProtocol string

const (
	SyncProtocolUniv3       SyncProtocol = "univ3"
	SyncProtocolPancakeV3   SyncProtocol = "pancakev3"
	SyncProtocolQuickSwapV3 SyncProtocol = "quickswapv3"
	SyncProtocolUniv4       SyncProtocol = "univ4"
	SyncProtocolBalancer    SyncProtocol = "balancer"
)

// ProtocolBlockReport is one protocol's apply result for a block.
type ProtocolBlockReport struct {
	Protocol       SyncProtocol
	BlockNumber    uint64
	Univ3Pools     []common.Address
	PancakePools   []common.Address
	QuickSwapPools []common.Address
	Univ4Pools     []marketuniv4.PoolID
	BalancerPools  []marketbalancer.PoolID
}

type protocolBlockState struct {
	univ3     []common.Address
	pancake   []common.Address
	quickSwap []common.Address
	univ4     []marketuniv4.PoolID
	balancer  []marketbalancer.PoolID
	reported  bool
}

type pendingBlock struct {
	byProtocol map[SyncProtocol]*protocolBlockState
}

// BlockCoordinator waits until every enabled sync protocol has applied the same
// block before scanning affected routes. This prevents cross-protocol quotes from
// mixing pool state from different heads.
type BlockCoordinator struct {
	mu            sync.Mutex
	enabled       map[SyncProtocol]struct{}
	pending       map[uint64]*pendingBlock
	flushing      map[uint64]bool
	lastFlushed   uint64
	lastVersion   domainchain.MarketVersion
	prepared      domainchain.MarketVersion
	generation    uint64
	routeMu       *sync.Mutex
	scan          *ScanService
	opportunities *OpportunityService
	publish       *PublishService
	logger        *zap.Logger
	flushFn       func(context.Context, uint64, []common.Address, []common.Address, []common.Address, []marketuniv4.PoolID, []marketbalancer.PoolID) error
	marketView    MarketViewCommitter
	scanMu        sync.Mutex
	scanBase      context.Context
	scanBlock     uint64
	scanCancel    context.CancelFunc
	scanDone      chan struct{}
}

// MarketViewCommitter atomically publishes a complete block for quote readers.
type MarketViewCommitter interface {
	Commit(context.Context, domainchain.MarketVersion, []common.Address, []common.Address, []common.Address, []marketuniv4.PoolID, []marketbalancer.PoolID) error
}

// SetScanContext binds asynchronous scans to the application lifecycle context.
func (c *BlockCoordinator) SetScanContext(ctx context.Context) {
	if c == nil {
		return
	}
	c.scanMu.Lock()
	c.scanBase = ctx
	c.scanMu.Unlock()
}

func NewBlockCoordinator(
	enabled []SyncProtocol,
	routeMu *sync.Mutex,
	scan *ScanService,
	opportunities *OpportunityService,
	publish *PublishService,
	marketView MarketViewCommitter,
	logger *zap.Logger,
) *BlockCoordinator {
	set := make(map[SyncProtocol]struct{}, len(enabled))
	for _, protocol := range enabled {
		if protocol == "" {
			continue
		}
		set[protocol] = struct{}{}
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &BlockCoordinator{
		enabled:       set,
		pending:       make(map[uint64]*pendingBlock),
		flushing:      make(map[uint64]bool),
		routeMu:       routeMu,
		scan:          scan,
		opportunities: opportunities,
		publish:       publish,
		marketView:    marketView,
		logger:        logger,
	}
}

// ReportApplied records that a protocol finished applying blockNumber and may flush.
func (c *BlockCoordinator) ReportApplied(ctx context.Context, report ProtocolBlockReport) error {
	if c == nil {
		return nil
	}
	if report.BlockNumber == 0 || report.Protocol == "" {
		return nil
	}

	c.mu.Lock()
	if report.BlockNumber < c.lastVersion.Number ||
		(report.BlockNumber == c.lastVersion.Number && c.prepared.SameBlock(c.lastVersion)) {
		c.mu.Unlock()
		return nil
	}
	if len(c.enabled) > 0 {
		if _, ok := c.enabled[report.Protocol]; !ok {
			c.mu.Unlock()
			return nil
		}
	}

	block := c.pending[report.BlockNumber]
	if block == nil {
		block = &pendingBlock{byProtocol: make(map[SyncProtocol]*protocolBlockState)}
		c.pending[report.BlockNumber] = block
	}
	state := block.byProtocol[report.Protocol]
	if state == nil {
		state = &protocolBlockState{}
		block.byProtocol[report.Protocol] = state
	}
	state.reported = true
	state.univ3 = mergeAddresses(state.univ3, report.Univ3Pools)
	state.pancake = mergeAddresses(state.pancake, report.PancakePools)
	state.quickSwap = mergeAddresses(state.quickSwap, report.QuickSwapPools)
	state.univ4 = mergeV4IDs(state.univ4, report.Univ4Pools)
	state.balancer = mergeBalancerIDs(state.balancer, report.BalancerPools)

	ready := c.isBlockReadyLocked(report.BlockNumber) && !c.flushing[report.BlockNumber]
	var (
		univ3     []common.Address
		pancake   []common.Address
		quickSwap []common.Address
		univ4     []marketuniv4.PoolID
		balancer  []marketbalancer.PoolID
	)
	if ready {
		univ3, pancake, quickSwap, univ4, balancer = c.collectChangedLocked(report.BlockNumber)
		c.flushing[report.BlockNumber] = true
	}
	enabledCount := len(c.enabled)
	c.mu.Unlock()

	if !ready {
		c.logger.Debug("arbitrage block barrier waiting",
			zap.Uint64("block", report.BlockNumber),
			zap.String("protocol", string(report.Protocol)),
			zap.Int("enabled_protocols", enabledCount),
		)
		return nil
	}
	c.mu.Lock()
	if c.prepared.Number != report.BlockNumber {
		c.generation++
		c.prepared = domainchain.MarketVersion{Number: report.BlockNumber, Generation: c.generation}
	}
	version := c.prepared
	c.mu.Unlock()
	if c.marketView != nil {
		if err := c.marketView.Commit(ctx, version, univ3, pancake, quickSwap, univ4, balancer); err != nil {
			c.mu.Lock()
			delete(c.flushing, report.BlockNumber)
			c.mu.Unlock()
			// Skip this block's quote publish, but do not fail ApplyBlock / shared-head.
			c.logger.Warn("skip market view commit",
				zap.Uint64("block", report.BlockNumber),
				zap.Uint64("generation", version.Generation),
				zap.String("hash", version.Hash.Hex()),
				zap.Int("univ3_changed", len(univ3)),
				zap.Int("pancakev3_changed", len(pancake)),
				zap.Int("quickswapv3_changed", len(quickSwap)),
				zap.Int("univ4_changed", len(univ4)),
				zap.Int("balancer_changed", len(balancer)),
				zap.Error(err),
			)
			return nil
		}
	}
	c.startScan(ctx, version, univ3, pancake, quickSwap, univ4, balancer)
	return nil
}

// PrepareHead cancels a scan for any different market version before pool state mutates.
func (c *BlockCoordinator) PrepareHead(ctx context.Context, head domainchain.BlockHeader) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if !c.prepared.IsZero() && c.prepared.Number == head.Number && c.prepared.Hash == head.Hash {
		c.mu.Unlock()
		return nil
	}
	c.generation++
	c.prepared = domainchain.MarketVersion{Number: head.Number, Hash: head.Hash, Generation: c.generation}
	c.mu.Unlock()
	return c.cancelCurrentScan(ctx)
}

// CancelBefore stops an older scan and waits for it to exit before blockNumber state is applied.
func (c *BlockCoordinator) CancelBefore(ctx context.Context, blockNumber uint64) error {
	if c == nil {
		return nil
	}
	c.scanMu.Lock()
	if c.scanDone == nil || c.scanBlock >= blockNumber {
		c.scanMu.Unlock()
		return nil
	}
	cancel := c.scanCancel
	done := c.scanDone
	c.scanMu.Unlock()
	if cancel != nil {
		cancel()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (c *BlockCoordinator) cancelCurrentScan(ctx context.Context) error {
	c.scanMu.Lock()
	cancel := c.scanCancel
	done := c.scanDone
	c.scanMu.Unlock()
	if done == nil {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (c *BlockCoordinator) waitForIdle(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.scanMu.Lock()
	done := c.scanDone
	c.scanMu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (c *BlockCoordinator) startScan(
	ctx context.Context,
	version domainchain.MarketVersion,
	univ3Pools []common.Address,
	pancakePools []common.Address,
	quickSwapPools []common.Address,
	univ4Pools []marketuniv4.PoolID,
	balancerPools []marketbalancer.PoolID,
) {
	blockNumber := version.Number
	c.scanMu.Lock()
	if c.scanDone != nil && c.scanBlock >= blockNumber {
		c.scanMu.Unlock()
		return
	}
	c.scanMu.Unlock()
	if err := c.CancelBefore(ctx, blockNumber); err != nil {
		return
	}
	c.scanMu.Lock()
	baseCtx := c.scanBase
	if baseCtx == nil {
		baseCtx = context.WithoutCancel(ctx)
	}
	scanCtx, cancel := context.WithCancel(baseCtx)
	done := make(chan struct{})
	c.scanBlock = blockNumber
	c.scanCancel = cancel
	c.scanDone = done
	c.scanMu.Unlock()

	go func() {
		var err error
		if c.flushFn != nil {
			err = c.flushFn(scanCtx, blockNumber, univ3Pools, pancakePools, quickSwapPools, univ4Pools, balancerPools)
		} else {
			err = c.flush(scanCtx, version, univ3Pools, pancakePools, quickSwapPools, univ4Pools, balancerPools)
		}

		c.mu.Lock()
		delete(c.flushing, blockNumber)
		if err == nil && !c.lastVersion.SameBlock(version) {
			c.lastFlushed = blockNumber
			c.lastVersion = version
			delete(c.pending, blockNumber)
			c.dropPendingBeforeLocked(blockNumber)
		}
		c.mu.Unlock()

		c.scanMu.Lock()
		if c.scanDone == done {
			c.scanBlock = 0
			c.scanCancel = nil
			c.scanDone = nil
		}
		close(done)
		c.scanMu.Unlock()

		if err != nil && !errors.Is(err, context.Canceled) {
			c.logger.Error("arbitrage block scan failed", zap.Uint64("block", blockNumber), zap.Error(err))
		}
	}()
}

func (c *BlockCoordinator) isBlockReadyLocked(blockNumber uint64) bool {
	block := c.pending[blockNumber]
	if block == nil {
		return false
	}
	if len(c.enabled) == 0 {
		// No barrier configured: any single protocol report may flush.
		for _, state := range block.byProtocol {
			if state != nil && state.reported {
				return true
			}
		}
		return false
	}
	for protocol := range c.enabled {
		state := block.byProtocol[protocol]
		if state == nil || !state.reported {
			return false
		}
	}
	return true
}

func (c *BlockCoordinator) collectChangedLocked(blockNumber uint64) (
	[]common.Address,
	[]common.Address,
	[]common.Address,
	[]marketuniv4.PoolID,
	[]marketbalancer.PoolID,
) {
	block := c.pending[blockNumber]
	if block == nil {
		return nil, nil, nil, nil, nil
	}
	var (
		univ3     []common.Address
		pancake   []common.Address
		quickSwap []common.Address
		univ4     []marketuniv4.PoolID
		balancer  []marketbalancer.PoolID
	)
	for _, state := range block.byProtocol {
		if state == nil {
			continue
		}
		univ3 = mergeAddresses(univ3, state.univ3)
		pancake = mergeAddresses(pancake, state.pancake)
		quickSwap = mergeAddresses(quickSwap, state.quickSwap)
		univ4 = mergeV4IDs(univ4, state.univ4)
		balancer = mergeBalancerIDs(balancer, state.balancer)
	}
	return univ3, pancake, quickSwap, univ4, balancer
}

func (c *BlockCoordinator) dropPendingBeforeLocked(blockNumber uint64) {
	for number := range c.pending {
		if number < blockNumber {
			delete(c.pending, number)
		}
	}
}

func (c *BlockCoordinator) flush(
	ctx context.Context,
	version domainchain.MarketVersion,
	univ3Pools []common.Address,
	pancakePools []common.Address,
	quickSwapPools []common.Address,
	univ4Pools []marketuniv4.PoolID,
	balancerPools []marketbalancer.PoolID,
) error {
	blockNumber := version.Number
	if c.scan == nil || c.opportunities == nil || c.publish == nil {
		return nil
	}
	if c.routeMu != nil {
		c.routeMu.Lock()
		defer c.routeMu.Unlock()
	}
	routes := c.scan.FindAffected(univ3Pools, pancakePools, quickSwapPools, univ4Pools, balancerPools)
	started := time.Now()
	c.logger.Debug("arbitrage block barrier flushed",
		zap.Uint64("block", blockNumber),
		zap.Int("univ3_pools", len(univ3Pools)),
		zap.Int("pancakev3_pools", len(pancakePools)),
		zap.Int("quickswapv3_pools", len(quickSwapPools)),
		zap.Int("univ4_pools", len(univ4Pools)),
		zap.Int("balancer_pools", len(balancerPools)),
		zap.Int("affected_routes", len(routes)),
	)
	if len(routes) == 0 {
		return nil
	}
	opportunities, err := c.opportunities.Generate(ctx, GenerateRequest{
		BlockNumber: blockNumber,
		Version:     version,
		Routes:      routes,
	})
	if err != nil {
		return err
	}
	c.logger.Debug("arbitrage opportunities generated",
		zap.Uint64("block", blockNumber),
		zap.Int("affected_routes", len(routes)),
		zap.Int("opportunities", len(opportunities)),
		zap.Int64("duration_ms", time.Since(started).Milliseconds()),
	)
	return c.publish.Publish(ctx, opportunities)
}

func mergeAddresses(dst, src []common.Address) []common.Address {
	if len(src) == 0 {
		return dst
	}
	seen := make(map[common.Address]struct{}, len(dst)+len(src))
	out := make([]common.Address, 0, len(dst)+len(src))
	for _, address := range dst {
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		out = append(out, address)
	}
	for _, address := range src {
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		out = append(out, address)
	}
	return out
}

func mergeV4IDs(dst, src []marketuniv4.PoolID) []marketuniv4.PoolID {
	if len(src) == 0 {
		return dst
	}
	seen := make(map[marketuniv4.PoolID]struct{}, len(dst)+len(src))
	out := make([]marketuniv4.PoolID, 0, len(dst)+len(src))
	for _, id := range dst {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range src {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func mergeBalancerIDs(dst, src []marketbalancer.PoolID) []marketbalancer.PoolID {
	if len(src) == 0 {
		return dst
	}
	seen := make(map[marketbalancer.PoolID]struct{}, len(dst)+len(src))
	out := make([]marketbalancer.PoolID, 0, len(dst)+len(src))
	for _, id := range dst {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range src {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
