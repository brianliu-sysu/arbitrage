package arbitrageapp

import (
	"context"
	"errors"
	"sync"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/marketchange"
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
	Protocol    SyncProtocol
	BlockNumber uint64
	Changes     MarketChanges
}

// MarketPublisher atomically publishes a complete block for quote readers.
type MarketPublisher interface {
	Publish(context.Context, domainchain.MarketVersion, marketchange.Changes) error
}

// BlockCoordinator publishes and scans a block after all enabled protocols report it.
type BlockCoordinator struct {
	barrier     *blockBarrier
	scans       *scanController
	executor    BlockScanExecutor
	marketStore MarketPublisher
	logger      *zap.Logger
}

// coordinatorScanTask keeps the immutable version and change snapshot selected
// by the barrier because the coordinator may advance while the scan is running.
type coordinatorScanTask struct {
	coordinator *BlockCoordinator
	version     domainchain.MarketVersion
	changes     MarketChanges
}

func (t *coordinatorScanTask) Run(ctx context.Context) error {
	return t.coordinator.executor.Execute(ctx, t.version, t.changes)
}

func (t *coordinatorScanTask) Complete(err error) {
	t.coordinator.barrier.complete(t.version, err)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.coordinator.logger.Error("arbitrage block scan failed",
			zap.Uint64("block", t.version.Number),
			zap.Error(err),
		)
	}
}

func NewBlockCoordinator(
	enabled []SyncProtocol,
	routeMu *sync.Mutex,
	scan *ScanService,
	opportunities *OpportunityService,
	publish *PublishService,
	marketStore MarketPublisher,
	logger *zap.Logger,
) *BlockCoordinator {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &BlockCoordinator{
		barrier:     newBlockBarrier(enabled),
		scans:       &scanController{},
		executor:    newBlockScanPipeline(routeMu, scan, opportunities, publish, logger),
		marketStore: marketStore,
		logger:      logger,
	}
}

// SetScanContext binds asynchronous scans to the application lifecycle context.
func (c *BlockCoordinator) SetScanContext(ctx context.Context) {
	if c != nil {
		c.scans.SetContext(ctx)
	}
}

// ReportApplied records that a protocol finished applying a block.
func (c *BlockCoordinator) ReportApplied(_ context.Context, report ProtocolBlockReport) error {
	if c == nil || report.BlockNumber == 0 || report.Protocol == "" {
		return nil
	}
	c.barrier.report(report)
	return nil
}

// FinalizeHead atomically publishes one fully applied market block and starts arbitrage.
func (c *BlockCoordinator) FinalizeHead(ctx context.Context, head domainchain.BlockHeader) error {
	if c == nil || head.Number == 0 {
		return nil
	}
	version, changes, enabledCount, ready := c.barrier.beginFinalize(head)
	if !ready {
		c.logger.Debug("market block not ready for unified publish",
			zap.Uint64("block", head.Number),
			zap.Int("enabled_protocols", enabledCount),
		)
		return nil
	}
	if c.marketStore != nil {
		err := c.marketStore.Publish(ctx, version, changes)
		if err != nil {
			c.barrier.abortFinalize(head.Number)
			c.logger.Warn("skip market view commit",
				zap.Uint64("block", head.Number),
				zap.Uint64("generation", version.Generation),
				zap.String("hash", version.Hash.Hex()),
				zap.Int("univ3_changed", len(changes.Univ3)),
				zap.Int("pancakev3_changed", len(changes.PancakeV3)),
				zap.Int("quickswapv3_changed", len(changes.QuickSwapV3)),
				zap.Int("univ4_changed", len(changes.Univ4)),
				zap.Int("balancer_changed", len(changes.Balancer)),
				zap.Error(err),
			)
			return nil
		}
	}
	c.startScan(ctx, version, changes)
	return nil
}

// PrepareHead cancels a scan for any different market version before pool state mutates.
func (c *BlockCoordinator) PrepareHead(ctx context.Context, head domainchain.BlockHeader) error {
	if c == nil {
		return nil
	}
	_, changed := c.barrier.prepare(head)
	if !changed {
		return nil
	}
	return c.scans.CancelCurrent(ctx)
}

// CancelBefore stops an older scan before newer block state is applied.
func (c *BlockCoordinator) CancelBefore(ctx context.Context, blockNumber uint64) error {
	if c == nil {
		return nil
	}
	return c.scans.CancelBefore(ctx, blockNumber)
}

func (c *BlockCoordinator) waitForIdle(ctx context.Context) error {
	if c == nil {
		return nil
	}
	return c.scans.WaitForIdle(ctx)
}

func (c *BlockCoordinator) startScan(ctx context.Context, version domainchain.MarketVersion, changes MarketChanges) {
	c.scans.Start(ctx, version.Number, &coordinatorScanTask{
		coordinator: c,
		version:     version,
		changes:     changes,
	})
}

func mergeAddresses(dst, src []common.Address) []common.Address {
	if len(src) == 0 {
		return dst
	}
	seen := make(map[common.Address]struct{}, len(dst)+len(src))
	out := make([]common.Address, 0, len(dst)+len(src))
	for _, address := range append(dst, src...) {
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
	for _, id := range append(dst, src...) {
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
	for _, id := range append(dst, src...) {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
