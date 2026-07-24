package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
)

const preparedBlockRollbackTimeout = 30 * time.Second

// PreparedBlock contains protocol state changes that have been parsed but not applied.
type preparedBlockFuncs struct {
	apply    func(context.Context) error
	rollback func(context.Context) error
}

func (f *preparedBlockFuncs) Apply(ctx context.Context) error {
	return f.apply(ctx)
}

func (f *preparedBlockFuncs) Rollback(ctx context.Context) error {
	if f.rollback == nil {
		return nil
	}
	return f.rollback(ctx)
}

func newPreparedBlock(apply, rollback func(context.Context) error) PreparedBlock {
	return &preparedBlockFuncs{apply: apply, rollback: rollback}
}

// MarketBlockProcessor prepares every protocol before applying any state changes.
type MarketBlockProcessor struct {
	handlers []NamedHeadHandler
}

func NewMarketBlockProcessor(handlers []NamedHeadHandler) *MarketBlockProcessor {
	return &MarketBlockProcessor{handlers: append([]NamedHeadHandler(nil), handlers...)}
}

func (p *MarketBlockProcessor) Process(ctx context.Context, head blockchain.BlockHeader, logs []RawLog) ([]string, error) {
	prepared := make([]PreparedBlock, 0, len(p.handlers))
	names := make([]string, 0, len(p.handlers))
	for _, handler := range p.handlers {
		block, err := handler.Handler.PrepareBlock(ctx, head, logs)
		if err != nil {
			return nil, fmt.Errorf("prepare protocol %s: %w", handler.Name, err)
		}
		if block == nil {
			return nil, fmt.Errorf("prepare protocol %s returned nil block", handler.Name)
		}
		prepared = append(prepared, block)
		names = append(names, handler.Name)
	}
	for index, block := range prepared {
		if block == nil {
			continue
		}
		if err := block.Apply(ctx); err != nil {
			applyErr := fmt.Errorf("apply protocol %s: %w", names[index], err)
			rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), preparedBlockRollbackTimeout)
			rollbackErr := rollbackPreparedBlocks(rollbackCtx, prepared[:index+1], names[:index+1])
			cancel()
			return nil, errors.Join(applyErr, rollbackErr)
		}
	}
	return names, nil
}

type preparedMarketReorg struct {
	plans      []PreparedReorg
	names      []string
	replayFrom uint64
}

func (p *MarketBlockProcessor) prepareReorg(ctx context.Context, reorg blockchain.Reorg) (*preparedMarketReorg, error) {
	recovery := &preparedMarketReorg{replayFrom: ^uint64(0)}
	for _, handler := range p.handlers {
		transition, ok := handler.Handler.(ReorgPreparer)
		if !ok {
			continue
		}
		plan, err := transition.PrepareReorg(ctx, reorg)
		if err != nil {
			rollbackErr := recovery.Rollback(ctx)
			return nil, errors.Join(
				fmt.Errorf("prepare protocol %s reorg at block %d: %w", handler.Name, reorg.RemoteHead.Number, err),
				rollbackErr,
			)
		}
		if plan == nil {
			return nil, fmt.Errorf("prepare protocol %s reorg returned nil plan", handler.Name)
		}
		recovery.plans = append(recovery.plans, plan)
		recovery.names = append(recovery.names, handler.Name)
		if plan.ReplayFrom() < recovery.replayFrom {
			recovery.replayFrom = plan.ReplayFrom()
		}
	}
	if len(recovery.plans) == 0 {
		recovery.replayFrom = reorg.CommonAncestor + 1
	}
	return recovery, nil
}

func (r *preparedMarketReorg) ReplayFrom() uint64 {
	return r.replayFrom
}

func (r *preparedMarketReorg) Process(
	ctx context.Context,
	head blockchain.BlockHeader,
	logs []RawLog,
) error {
	prepared := make([]PreparedBlock, 0, len(r.plans))
	for index, plan := range r.plans {
		block, err := plan.PrepareBlock(ctx, head, logs)
		if err != nil {
			return fmt.Errorf("prepare protocol %s recovery block %d: %w", r.names[index], head.Number, err)
		}
		prepared = append(prepared, block)
	}
	for index, block := range prepared {
		if err := block.Apply(ctx); err != nil {
			rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), preparedBlockRollbackTimeout)
			rollbackErr := rollbackPreparedBlocks(rollbackCtx, prepared[:index+1], r.names[:index+1])
			cancel()
			return errors.Join(
				fmt.Errorf("apply protocol %s recovery block %d: %w", r.names[index], head.Number, err),
				rollbackErr,
			)
		}
	}
	return nil
}

func (r *preparedMarketReorg) Commit(ctx context.Context) error {
	for index, plan := range r.plans {
		if err := plan.Commit(ctx); err != nil {
			return fmt.Errorf("commit protocol %s reorg: %w", r.names[index], err)
		}
	}
	return nil
}

func (r *preparedMarketReorg) Rollback(ctx context.Context) error {
	var rollbackErr error
	for index := len(r.plans) - 1; index >= 0; index-- {
		if err := r.plans[index].Rollback(ctx); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("rollback protocol %s reorg: %w", r.names[index], err))
		}
	}
	return rollbackErr
}

func rollbackPreparedBlocks(ctx context.Context, prepared []PreparedBlock, names []string) error {
	var rollbackErr error
	for index := len(prepared) - 1; index >= 0; index-- {
		if prepared[index] == nil {
			continue
		}
		if err := prepared[index].Rollback(ctx); err != nil {
			rollbackErr = errors.Join(
				rollbackErr,
				fmt.Errorf("rollback protocol %s: %w", names[index], err),
			)
		}
	}
	return rollbackErr
}
