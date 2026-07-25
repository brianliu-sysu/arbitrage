package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/application/marketstore"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence"
	"go.uber.org/zap"
)

const marketPersistenceTimeout = 30 * time.Second

type poolSource[ID comparable, Pool any] interface {
	Get(context.Context, ID) (*Pool, error)
}

type poolSink[Pool any] interface {
	Save(context.Context, *Pool) error
}

func persistChangedPools[ID comparable, Pool any](ctx context.Context, ids []ID, source poolSource[ID, Pool], sink poolSink[Pool]) error {
	for _, id := range ids {
		pool, err := source.Get(ctx, id)
		if err != nil {
			return err
		}
		if pool != nil {
			if err := sink.Save(ctx, pool); err != nil {
				return err
			}
		}
	}
	return nil
}

type marketPersistenceObserver struct {
	runtime *chainRuntime
	logger  *zap.Logger
}

func (o *marketPersistenceObserver) AfterMarketPublished(
	_ context.Context,
	publication marketstore.Publication,
) {
	version := publication.Version
	startSafeGoroutine(&o.runtime.persistenceWG, func(recovered any) {
		o.logger.Error("market persistence panicked", zap.Uint64("block", version.Number), zap.Any("panic", recovered), zap.Stack("stack"))
	}, func() {
		baseCtx := o.runtime.resources.persistenceCtx
		if baseCtx == nil {
			baseCtx = context.Background()
		}
		persistCtx, cancel := context.WithTimeout(baseCtx, marketPersistenceTimeout)
		defer cancel()
		if err := persistMarketChanges(persistCtx, o.runtime.resources.stores.runtime, o.runtime.resources.stores.durable, publication.Changes); err != nil {
			o.logger.Error("persist market snapshot failed", zap.Uint64("block", version.Number), zap.Error(err))
		}
	})
}

func persistMarketChanges(
	ctx context.Context,
	source *persistence.Services,
	sink *persistence.Services,
	changes marketstore.Changes,
) error {
	if err := persistChangedPools(ctx, changes.Univ3, source.Pools, sink.Pools); err != nil {
		return fmt.Errorf("persist univ3 pools: %w", err)
	}
	if err := persistChangedPools(ctx, changes.PancakeV3, source.PancakePools, sink.PancakePools); err != nil {
		return fmt.Errorf("persist pancakev3 pools: %w", err)
	}
	if err := persistChangedPools(ctx, changes.QuickSwapV3, source.QuickSwapPools, sink.QuickSwapPools); err != nil {
		return fmt.Errorf("persist quickswapv3 pools: %w", err)
	}
	if err := persistChangedPools(ctx, changes.Univ4, source.V4Pools, sink.V4Pools); err != nil {
		return fmt.Errorf("persist univ4 pools: %w", err)
	}
	if err := persistChangedPools(ctx, changes.Balancer, source.BalancerPools, sink.BalancerPools); err != nil {
		return fmt.Errorf("persist balancer pools: %w", err)
	}
	return nil
}
