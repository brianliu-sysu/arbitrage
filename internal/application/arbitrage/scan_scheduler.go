package arbitrageapp

import (
	"context"
	"errors"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/marketchange"
	"go.uber.org/zap"
)

// ScanScheduler owns cancellation and execution of arbitrage scans.
type ScanScheduler struct {
	scans    *scanController
	executor BlockScanExecutor
	logger   *zap.Logger
}

type scheduledScan struct {
	scheduler *ScanScheduler
	version   domainchain.MarketVersion
	changes   marketchange.Changes
}

func NewScanScheduler(executor BlockScanExecutor, logger *zap.Logger) *ScanScheduler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ScanScheduler{
		scans:    &scanController{},
		executor: executor,
		logger:   logger,
	}
}

func (s *ScanScheduler) CancelCurrent(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.scans.CancelCurrent(ctx)
}

func (s *ScanScheduler) Start(ctx context.Context, version domainchain.MarketVersion, changes marketchange.Changes) {
	if s == nil || s.executor == nil {
		return
	}
	s.scans.Start(ctx, version.Number, &scheduledScan{
		scheduler: s,
		version:   version,
		changes:   changes,
	})
}

func (s *ScanScheduler) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.scans.Close(ctx)
}

func (s *scheduledScan) Run(ctx context.Context) error {
	return s.scheduler.executor.Execute(ctx, s.version, s.changes)
}

func (s *scheduledScan) Complete(err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		s.scheduler.logger.Error("arbitrage block scan failed",
			zap.Uint64("block", s.version.Number),
			zap.Error(err),
		)
	}
}
