package protocol

import (
	"context"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"go.uber.org/zap"
)

type ProtocolLifecycleConfigurer[PoolID comparable] interface {
	SetListener(PoolsChangedNotifier[PoolID])
	SetLogger(*zap.Logger)
}

// ProtocolLifecycle groups the runtime lifecycle of one pool protocol.
type ProtocolLifecycle[PoolID comparable] struct {
	orchestrator *SyncOrchestrator[PoolID]
}

func NewProtocolLifecycle[PoolID comparable](
	orchestrator *SyncOrchestrator[PoolID],
) *ProtocolLifecycle[PoolID] {
	return &ProtocolLifecycle[PoolID]{orchestrator: orchestrator}
}

func (l *ProtocolLifecycle[PoolID]) ListActive() []PoolID {
	return l.orchestrator.ListActivePools()
}

func (l *ProtocolLifecycle[PoolID]) List(context.Context) ([]PoolID, error) {
	return l.ListActive(), nil
}

func (l *ProtocolLifecycle[PoolID]) StartAll(ctx context.Context, blockNumber uint64) error {
	return l.orchestrator.StartAll(ctx, blockNumber)
}

func (l *ProtocolLifecycle[PoolID]) SetListener(listener PoolsChangedNotifier[PoolID]) {
	l.orchestrator.SetListener(listener)
}

func (l *ProtocolLifecycle[PoolID]) SetLogger(logger *zap.Logger) {
	l.orchestrator.SetLogger(logger)
}

func (l *ProtocolLifecycle[PoolID]) BlockPreparer() BlockPreparer {
	return l.orchestrator.BlockPreparer()
}

func (l *ProtocolLifecycle[PoolID]) StartBootstrapAt(ctx context.Context, head blockchain.BlockHeader) error {
	return l.orchestrator.StartBootstrapAt(ctx, head)
}

func (l *ProtocolLifecycle[PoolID]) AddPool(ctx context.Context, poolID PoolID) error {
	return l.orchestrator.AddPool(ctx, poolID)
}

func (l *ProtocolLifecycle[PoolID]) CatchUpPool(ctx context.Context, poolID PoolID, blockNumber uint64) error {
	return l.orchestrator.CatchUpPool(ctx, poolID, blockNumber)
}

func (l *ProtocolLifecycle[PoolID]) CatchUpAll(ctx context.Context, blockNumber uint64) error {
	return l.orchestrator.CatchUpAll(ctx, blockNumber)
}
