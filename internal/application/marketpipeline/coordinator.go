package marketpipeline

import (
	"context"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/marketchange"
	"go.uber.org/zap"
)

// Protocol identifies a market-data sync pipeline that must align before publishing.
type Protocol string

const (
	ProtocolUniv3       Protocol = "univ3"
	ProtocolPancakeV3   Protocol = "pancakev3"
	ProtocolQuickSwapV3 Protocol = "quickswapv3"
	ProtocolUniv4       Protocol = "univ4"
	ProtocolBalancer    Protocol = "balancer"
)

// ProtocolBlockReport is one protocol's apply result for a block.
type ProtocolBlockReport struct {
	Protocol    Protocol
	BlockNumber uint64
	Changes     marketchange.Changes
}

// Publisher atomically publishes a complete block for market readers.
type Publisher interface {
	Publish(context.Context, domainchain.MarketVersion, marketchange.Changes) error
}

// Coordinator aligns protocol reports and publishes a complete market block.
type Coordinator struct {
	barrier   *blockBarrier
	publisher Publisher
	logger    *zap.Logger
}

func NewCoordinator(enabled []Protocol, publisher Publisher, logger *zap.Logger) *Coordinator {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Coordinator{
		barrier:   newBlockBarrier(enabled),
		publisher: publisher,
		logger:    logger,
	}
}

// ReportApplied records that a protocol finished applying a block.
func (c *Coordinator) ReportApplied(_ context.Context, report ProtocolBlockReport) error {
	if c == nil || report.BlockNumber == 0 || report.Protocol == "" {
		return nil
	}
	c.barrier.report(report)
	return nil
}

// PrepareHead selects the market version that protocol reports will complete.
func (c *Coordinator) PrepareHead(head domainchain.BlockHeader) bool {
	if c == nil {
		return false
	}
	return c.barrier.prepare(head)
}

// CommitHead publishes a complete market block.
func (c *Coordinator) CommitHead(
	ctx context.Context,
	head domainchain.BlockHeader,
) (domainchain.MarketVersion, marketchange.Changes, bool) {
	if c == nil || head.Number == 0 {
		return domainchain.MarketVersion{}, marketchange.Changes{}, false
	}
	version, changes, enabledCount, ready := c.barrier.beginFinalize(head)
	if !ready {
		c.logger.Debug("market block not ready for unified publish",
			zap.Uint64("block", head.Number),
			zap.Int("enabled_protocols", enabledCount),
		)
		return domainchain.MarketVersion{}, marketchange.Changes{}, false
	}
	if c.publisher != nil {
		if err := c.publisher.Publish(ctx, version, changes); err != nil {
			c.barrier.abortFinalize(head.Number)
			c.logger.Warn("skip market view commit",
				zap.Uint64("block", head.Number),
				zap.Uint64("generation", version.Generation),
				zap.String("hash", version.Hash.Hex()),
				zap.Error(err),
			)
			return domainchain.MarketVersion{}, marketchange.Changes{}, false
		}
	}
	c.barrier.complete(version)
	return version, changes, true
}
