package syncapp

import (
	"context"
	"fmt"
	"sync"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
)

// HeadSyncService subscribes to new heads and drives block apply flow.
type HeadSyncService struct {
	fetcher    LogFetcher
	parser     EventParser
	blockApply *BlockApplyService
	lifecycle  *PoolLifecycleService
	reorg      *ReorgRecoveryService
	readiness  *ReadinessService
	subscriber HeadSubscriber

	mu        sync.RWMutex
	localHead blockchain.BlockHeader
}

func NewHeadSyncService(
	fetcher LogFetcher,
	parser EventParser,
	blockApply *BlockApplyService,
	lifecycle *PoolLifecycleService,
	reorg *ReorgRecoveryService,
	readiness *ReadinessService,
	subscriber HeadSubscriber,
) *HeadSyncService {
	return &HeadSyncService{
		fetcher:    fetcher,
		parser:     parser,
		blockApply: blockApply,
		lifecycle:  lifecycle,
		reorg:      reorg,
		readiness:  readiness,
		subscriber: subscriber,
	}
}

func (s *HeadSyncService) SetLocalHead(head blockchain.BlockHeader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.localHead = head
}

func (s *HeadSyncService) LocalHead() blockchain.BlockHeader {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.localHead
}

func (s *HeadSyncService) Run(ctx context.Context) error {
	heads, err := s.subscriber.SubscribeNewHead(ctx)
	if err != nil {
		return fmt.Errorf("subscribe new head: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case head, ok := <-heads:
			if !ok {
				return fmt.Errorf("head subscription closed")
			}
			if err := s.handleHead(ctx, head); err != nil {
				return err
			}
		}
	}
}

func (s *HeadSyncService) handleHead(ctx context.Context, head blockchain.BlockHeader) error {
	localHead := s.LocalHead()
	if localHead.Number > 0 {
		reorgEvent, err := s.reorg.DetectReorg(ctx, localHead, head)
		if err != nil {
			return fmt.Errorf("detect reorg: %w", err)
		}
		if reorgEvent != nil {
			if err := s.reorg.Recover(ctx, *reorgEvent, s.lifecycle.ListActive()); err != nil {
				return fmt.Errorf("recover reorg: %w", err)
			}
			s.SetLocalHead(head)
			return nil
		}
	}

	pools := s.lifecycle.ListActive()
	if len(pools) == 0 {
		s.SetLocalHead(head)
		return nil
	}

	logs, err := s.fetcher.FetchLogs(ctx, LogFilter{
		PoolAddresses: pools,
		FromBlock:     head.Number,
		ToBlock:       head.Number,
	})
	if err != nil {
		return fmt.Errorf("fetch logs for head %d: %w", head.Number, err)
	}

	events, err := s.parser.ParsePoolEvents(logs)
	if err != nil {
		return fmt.Errorf("parse events for head %d: %w", head.Number, err)
	}

	if _, err := s.blockApply.ApplyBlock(ctx, ApplyBlockRequest{
		BlockNumber:  head.Number,
		BlockHash:    head.Hash,
		Events:       events,
		TrackedPools: pools,
	}); err != nil {
		return fmt.Errorf("apply head block %d: %w", head.Number, err)
	}

	if err := s.blockApply.MarkPoolsReady(ctx, pools); err != nil {
		return fmt.Errorf("mark pools ready: %w", err)
	}
	s.SetLocalHead(head)
	return nil
}

// HandleHead exposes single-head processing for tests and manual replay.
func (s *HeadSyncService) HandleHead(ctx context.Context, head blockchain.BlockHeader) error {
	return s.handleHead(ctx, head)
}
