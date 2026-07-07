package syncapp

import (
	"context"
	"fmt"
	"sync"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum/common"
)

// ReorgRecovery handles chain reorg detection and recovery.
type ReorgRecovery[PoolID comparable] interface {
	DetectReorg(ctx context.Context, localHead, remoteHead blockchain.BlockHeader) (*blockchain.Reorg, error)
	Recover(ctx context.Context, reorg blockchain.Reorg, pools []PoolID) error
}

// HeadSyncHooks configures protocol-specific live head sync behavior.
type HeadSyncHooks[PoolID comparable, Event any] struct {
	FetchHeadLogs  func(context.Context, []PoolID, uint64) ([]RawLog, error)
	ParseEvents    func([]RawLog) ([]Event, error)
	ApplyBlock     func(context.Context, uint64, common.Hash, []Event, []PoolID, bool) error
	MarkPoolsReady func(context.Context, []PoolID) error
}

// HeadSyncService subscribes to new heads and drives block apply flow.
type HeadSyncService[PoolID comparable, Event any] struct {
	lifecycle  *PoolLifecycleService[PoolID]
	reorg      ReorgRecovery[PoolID]
	catchup    *CatchupService[PoolID, Event]
	blocks     BlockReader
	subscriber HeadSubscriber
	hooks      HeadSyncHooks[PoolID, Event]

	mu        sync.RWMutex
	localHead blockchain.BlockHeader
}

func NewHeadSyncService[PoolID comparable, Event any](
	lifecycle *PoolLifecycleService[PoolID],
	reorg ReorgRecovery[PoolID],
	catchup *CatchupService[PoolID, Event],
	blocks BlockReader,
	subscriber HeadSubscriber,
	hooks HeadSyncHooks[PoolID, Event],
) *HeadSyncService[PoolID, Event] {
	return &HeadSyncService[PoolID, Event]{
		lifecycle:  lifecycle,
		reorg:      reorg,
		catchup:    catchup,
		blocks:     blocks,
		subscriber: subscriber,
		hooks:      hooks,
	}
}

func (s *HeadSyncService[PoolID, Event]) SetLocalHead(head blockchain.BlockHeader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.localHead = head
}

func (s *HeadSyncService[PoolID, Event]) LocalHead() blockchain.BlockHeader {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.localHead
}

func (s *HeadSyncService[PoolID, Event]) Run(ctx context.Context) error {
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

func (s *HeadSyncService[PoolID, Event]) handleHead(ctx context.Context, head blockchain.BlockHeader) error {
	localHead := s.LocalHead()
	if ShouldSkipHeadNotification(localHead, head) {
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

	if NeedsHeadGapCatchup(localHead, head) {
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

	logs, err := s.hooks.FetchHeadLogs(ctx, pools, head.Number)
	if err != nil {
		return fmt.Errorf("fetch logs for head %d: %w", head.Number, err)
	}

	events, err := s.hooks.ParseEvents(logs)
	if err != nil {
		return fmt.Errorf("parse events for head %d: %w", head.Number, err)
	}

	if err := s.hooks.ApplyBlock(ctx, head.Number, head.Hash, events, pools, false); err != nil {
		return fmt.Errorf("apply head block %d: %w", head.Number, err)
	}

	if err := s.hooks.MarkPoolsReady(ctx, pools); err != nil {
		return fmt.Errorf("mark pools ready: %w", err)
	}
	s.SetLocalHead(head)
	return nil
}

// HandleHead exposes single-head processing for tests and manual replay.
func (s *HeadSyncService[PoolID, Event]) HandleHead(ctx context.Context, head blockchain.BlockHeader) error {
	return s.handleHead(ctx, head)
}

func (s *HeadSyncService[PoolID, Event]) catchUpGap(ctx context.Context, localHead, head blockchain.BlockHeader) error {
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
