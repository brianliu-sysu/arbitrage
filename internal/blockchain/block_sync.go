package blockchain

import (
	"context"
	"fmt"
	"sync"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/ethereum/go-ethereum/core/types"
)

// HeaderSubscriber 订阅新区块头。
type HeaderSubscriber interface {
	SubscribeNewHead(ctx context.Context) (<-chan *types.Header, func(), error)
}

// BlockSync 协调区块同步：Header → ProcessBlock → Next Block。
type BlockSync struct {
	chainName  string
	subscriber HeaderSubscriber
	processor  BlockProcessor
	syncRepo   interface {
		GetLastProcessedBlock(ctx context.Context, chainName string) (uint64, error)
	}
	logger logx.Logger

	mu    sync.Mutex
	runMu sync.Mutex
}

// NewBlockSync 创建 BlockSync 协调器。
func NewBlockSync(
	chainName string,
	subscriber HeaderSubscriber,
	processor BlockProcessor,
	syncRepo interface {
		GetLastProcessedBlock(ctx context.Context, chainName string) (uint64, error)
	},
	logger logx.Logger,
) *BlockSync {
	return &BlockSync{
		chainName:  chainName,
		subscriber: subscriber,
		processor:  processor,
		syncRepo:   syncRepo,
		logger:     logger,
	}
}

// Run 启动同步循环：收到 newHead 后顺序处理 LastBlock+1..head。
func (s *BlockSync) Run(ctx context.Context) error {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	heads, unsub, err := s.subscriber.SubscribeNewHead(ctx)
	if err != nil {
		return fmt.Errorf("subscribe new head: %w", err)
	}
	defer unsub()

	last, err := s.syncRepo.GetLastProcessedBlock(ctx, s.chainName)
	if err != nil {
		return fmt.Errorf("get last processed block: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case head, ok := <-heads:
			if !ok {
				return fmt.Errorf("header subscription closed")
			}
			if head == nil {
				continue
			}
			target := head.Number.Uint64()
			for b := last + 1; b <= target; b++ {
				if err := s.processor.ProcessBlock(ctx, b); err != nil {
					s.logger.Error("process block failed", "chain", s.chainName, "block", b, "error", err)
					continue
				}
				last = b
			}
		}
	}
}

// CatchUp 从 last+1 追到 target（启动时补块）。
func (s *BlockSync) CatchUp(ctx context.Context, target uint64) error {
	last, err := s.syncRepo.GetLastProcessedBlock(ctx, s.chainName)
	if err != nil {
		return err
	}
	return s.CatchUpFrom(ctx, last+1, target)
}

// CatchUpFrom 处理 [from, to] 闭区间内的区块。
func (s *BlockSync) CatchUpFrom(ctx context.Context, from, to uint64) error {
	if from > to {
		return nil
	}
	for b := from; b <= to; b++ {
		if err := s.processor.ProcessBlock(ctx, b); err != nil {
			return fmt.Errorf("catch up block %d: %w", b, err)
		}
	}
	return nil
}
