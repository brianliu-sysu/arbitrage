package syncv4

import (
	"context"
	"fmt"
	"sync"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
)

// HeadSyncService subscribes to new heads and drives V4 block apply flow.
type HeadSyncService struct {
	fetcher    LogFetcher
	parser     EventParser
	blockApply *BlockApplyService
	lifecycle  *PoolLifecycleService
	reorg      *ReorgRecoveryService
	readiness  *ReadinessService
	catchup    *CatchupService
	blocks     BlockReader
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
	catchup *CatchupService,
	blocks BlockReader,
	subscriber HeadSubscriber,
) *HeadSyncService {
	return &HeadSyncService{
		fetcher:    fetcher,
		parser:     parser,
		blockApply: blockApply,
		lifecycle:  lifecycle,
		reorg:      reorg,
		readiness:  readiness,
		catchup:    catchup,
		blocks:     blocks,
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
	if syncapp.ShouldSkipHeadNotification(localHead, head) {
		return nil
	}
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

	if syncapp.NeedsHeadGapCatchup(localHead, head) {
		if err := s.catchUpGap(ctx, localHead, head); err != nil {
			return err
		}
		localHead = s.LocalHead()
	}

	pools := s.lifecycle.ListActive()
	if len(pools) == 0 {
		s.SetLocalHead(head)
		return nil
	}

	logs, err := s.fetcher.FetchLogs(ctx, LogFilter{
		PoolIDs:   pools,
		FromBlock: head.Number,
		ToBlock:   head.Number,
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

func (s *HeadSyncService) HandleHead(ctx context.Context, head blockchain.BlockHeader) error {
	return s.handleHead(ctx, head)
}

func (s *HeadSyncService) catchUpGap(ctx context.Context, localHead, head blockchain.BlockHeader) error {
	if s.catchup == nil || s.blocks == nil {
		return fmt.Errorf("missing catchup services for head gap %d -> %d", localHead.Number, head.Number)
	}
	if err := s.catchup.CatchUpAll(ctx, head.Number-1); err != nil {
		return fmt.Errorf("catch up gap before head %d: %w", head.Number, err)
	}
	gapHead, err := s.blocks.GetBlockHeader(ctx, head.Number-1)
	if err != nil {
		return fmt.Errorf("load gap head %d: %w", head.Number-1, err)
	}
	s.SetLocalHead(gapHead)
	return nil
}
