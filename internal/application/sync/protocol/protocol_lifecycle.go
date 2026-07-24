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
	Pools        *PoolLifecycleService[PoolID]
	BlockHandler BlockHandler
	Readiness    *ReadinessService[PoolID]
	orchestrator *SyncOrchestrator[PoolID]
	configurer   ProtocolLifecycleConfigurer[PoolID]
}

func NewProtocolLifecycle[PoolID comparable](
	pools *PoolLifecycleService[PoolID],
	blockHandler BlockHandler,
	readiness *ReadinessService[PoolID],
	orchestrator *SyncOrchestrator[PoolID],
	configurer ProtocolLifecycleConfigurer[PoolID],
) *ProtocolLifecycle[PoolID] {
	return &ProtocolLifecycle[PoolID]{
		Pools:        pools,
		BlockHandler: blockHandler,
		Readiness:    readiness,
		orchestrator: orchestrator,
		configurer:   configurer,
	}
}

func (l *ProtocolLifecycle[PoolID]) SetListener(listener PoolsChangedNotifier[PoolID]) {
	l.configurer.SetListener(listener)
}

func (l *ProtocolLifecycle[PoolID]) SetLogger(logger *zap.Logger) {
	l.configurer.SetLogger(logger)
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
