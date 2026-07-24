package arbitrageapp

import (
	"context"
	"sync"
	"time"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"go.uber.org/zap"
)

// BlockScanExecutor scans and publishes opportunities for one committed market version.
type BlockScanExecutor interface {
	Execute(context.Context, domainchain.MarketVersion, MarketChanges) error
}

type blockScanPipeline struct {
	routeMu       *sync.Mutex
	scan          *ScanService
	opportunities *OpportunityService
	publish       *PublishService
	logger        *zap.Logger
}

func newBlockScanPipeline(
	routeMu *sync.Mutex,
	scan *ScanService,
	opportunities *OpportunityService,
	publish *PublishService,
	logger *zap.Logger,
) BlockScanExecutor {
	return &blockScanPipeline{
		routeMu:       routeMu,
		scan:          scan,
		opportunities: opportunities,
		publish:       publish,
		logger:        logger,
	}
}

func (p *blockScanPipeline) Execute(ctx context.Context, version domainchain.MarketVersion, changes MarketChanges) error {
	if p.scan == nil || p.opportunities == nil || p.publish == nil {
		return nil
	}
	if p.routeMu != nil {
		p.routeMu.Lock()
		defer p.routeMu.Unlock()
	}
	routes := p.scan.FindAffected(changes.Univ3, changes.PancakeV3, changes.QuickSwapV3, changes.Univ4, changes.Balancer)
	started := time.Now()
	p.logger.Debug("arbitrage block barrier flushed",
		zap.Uint64("block", version.Number),
		zap.Int("univ3_pools", len(changes.Univ3)),
		zap.Int("pancakev3_pools", len(changes.PancakeV3)),
		zap.Int("quickswapv3_pools", len(changes.QuickSwapV3)),
		zap.Int("univ4_pools", len(changes.Univ4)),
		zap.Int("balancer_pools", len(changes.Balancer)),
		zap.Int("affected_routes", len(routes)),
	)
	if len(routes) == 0 {
		return nil
	}
	opportunities, err := p.opportunities.Generate(ctx, GenerateRequest{
		BlockNumber: version.Number,
		Version:     version,
		Routes:      routes,
	})
	if err != nil {
		return err
	}
	p.logger.Debug("arbitrage opportunities generated",
		zap.Uint64("block", version.Number),
		zap.Int("affected_routes", len(routes)),
		zap.Int("opportunities", len(opportunities)),
		zap.Int64("duration_ms", time.Since(started).Milliseconds()),
	)
	return p.publish.Publish(ctx, opportunities)
}
