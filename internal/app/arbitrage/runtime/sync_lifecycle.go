package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"go.uber.org/zap"
)

type syncLifecycle struct {
	runCtx     context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	bootstraps protocolBootstrapPlan
	sharedHead *syncapp.SharedHeadRunner
	runtime    *chainRuntime
	logger     *zap.Logger
}

func newSyncLifecycles(logger *zap.Logger, runtimes *runtimeSet) ([]*syncLifecycle, error) {
	runners := make([]*syncLifecycle, 0, len(runtimes.chains))
	for _, runtime := range runtimes.chains {
		runner, err := newSyncLifecycle(runtime, logger.Named(runtime.cfg.Name))
		if err != nil {
			return nil, fmt.Errorf("configure %s sync lifecycle: %w", runtime.cfg.Name, err)
		}
		runners = append(runners, runner)
	}
	return runners, nil
}

func newSyncLifecycle(runtime *chainRuntime, logger *zap.Logger) (*syncLifecycle, error) {
	runner := &syncLifecycle{
		runtime: runtime,
		logger:  logger,
	}
	handlers := make([]syncapp.NamedHeadHandler, 0, 5)
	for _, module := range runtime.protocols.modules {
		runner.bootstraps.add(module.Name(), module.Bootstrapper())
		handlers = append(handlers, syncapp.NamedHeadHandler{Name: module.Name(), Handler: module.HeadHandler()})
	}
	if len(handlers) > 0 {
		var coordinator syncapp.HeadCoordinator
		if runtime.Arbitrage != nil && runtime.Arbitrage.Coordinator != nil {
			coordinator = runtime.Arbitrage.Coordinator
		}
		var subscriber syncapp.HeadSubscriber
		var blocks syncapp.CanonicalBlockReader
		if runtime.resources.blockchain != nil {
			subscriber = runtime.resources.blockchain.HeadSub
			blocks = runtime.resources.blockchain.Client
		}
		sharedHead, err := syncapp.NewSharedHeadRunner(
			syncapp.SharedHeadDependencies{
				Subscriber:  subscriber,
				LogFetcher:  runtime.resources.protocols.headLogFetcher,
				Blocks:      blocks,
				Coordinator: coordinator,
			},
			handlers,
			runtime.cfg.Sync.ReorgMaxDepth,
			logger.Named("shared-head"),
		)
		if err != nil {
			return nil, fmt.Errorf("configure shared head runner: %w", err)
		}
		runner.sharedHead = sharedHead
	}
	return runner, nil
}

func (r *syncLifecycle) start(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(context.Background())
	r.runCtx = runCtx
	r.cancel = cancel
	if r.runtime != nil && r.runtime.Arbitrage != nil && r.runtime.Arbitrage.Coordinator != nil {
		r.runtime.Arbitrage.Coordinator.SetScanContext(runCtx)
	}

	r.logger.Info("starting pool sync",
		zap.Uint64("chain_id", r.runtime.cfg.ChainID),
		zap.String("chain", r.runtime.cfg.Name),
		zap.String("persistence", r.runtime.resources.stores.backendName()),
		zap.Bool("memory_mode", r.runtime.resources.stores.usesMemory()),
		zap.Bool("univ3", r.runtime.cfg.Sync.Univ3.IsActive()),
		zap.Bool("univ3_subgraph", r.runtime.cfg.Sync.Univ3.Subgraph.IsEnabled()),
		zap.Int("univ3_pools", len(r.runtime.cfg.Sync.Univ3.Pools)),
		zap.Bool("pancakev3", r.runtime.cfg.Sync.PancakeV3.IsActive()),
		zap.Bool("pancakev3_subgraph", r.runtime.cfg.Sync.PancakeV3.Subgraph.IsEnabled()),
		zap.Int("pancakev3_pools", len(r.runtime.cfg.Sync.PancakeV3.Pools)),
		zap.Bool("quickswapv3", r.runtime.cfg.Sync.QuickSwapV3.IsActive()),
		zap.Bool("quickswapv3_subgraph", r.runtime.cfg.Sync.QuickSwapV3.Subgraph.IsEnabled()),
		zap.Int("quickswapv3_pools", len(r.runtime.cfg.Sync.QuickSwapV3.Pools)),
		zap.Bool("univ4", r.runtime.cfg.Sync.Univ4.IsActive()),
		zap.Int("univ4_poolmanager_pools", len(r.runtime.cfg.Sync.Univ4.PoolManager.Pools)),
		zap.Bool("univ4_subgraph", r.runtime.cfg.Sync.Univ4.Subgraph.IsEnabled()),
		zap.Bool("balancer", r.runtime.cfg.Sync.Balancer.IsActive()),
		zap.Bool("balancer_subgraph", r.runtime.cfg.Sync.Balancer.Subgraph.IsEnabled()),
		zap.Int("balancer_pools", len(r.runtime.cfg.Sync.Balancer.Pools)),
	)

	if r.sharedHead != nil {
		startup := make(chan error, 1)
		var startupOnce sync.Once
		reportStartup := func(err error) {
			startupOnce.Do(func() {
				startup <- err
			})
		}
		r.runSync("shared-head", func(ctx context.Context) error {
			err := r.runSharedHeadLifecycle(ctx, func() {
				reportStartup(nil)
			})
			reportStartup(err)
			return err
		})
		select {
		case err := <-startup:
			if err != nil {
				return fmt.Errorf("start shared head lifecycle: %w", err)
			}
		case <-ctx.Done():
			cancel()
			return ctx.Err()
		}
	}

	if r.runtime != nil && r.runtime.Arbitrage != nil && r.runtime.cfg.ArbitrageEnabled() {
		r.runArbitrageRouteWatcher()
	}
	r.runSubgraphDiscoveryWatchers()
	return nil
}

func (r *syncLifecycle) runSharedHeadLifecycle(ctx context.Context, onReady func()) error {
	if !r.bootstraps.hasAny() {
		<-ctx.Done()
		return ctx.Err()
	}
	head, err := r.runtime.resources.blockchain.Client.GetLatestBlockHeader(ctx)
	if err != nil {
		return fmt.Errorf("load shared bootstrap head: %w", err)
	}
	if err := runBootstrapTasks(ctx, head, r.bootstraps.tasks); err != nil {
		return err
	}
	if err := r.sharedHead.InitializeLocalHead(ctx, head); err != nil {
		return fmt.Errorf("initialize shared local head: %w", err)
	}
	heads, err := r.sharedHead.Subscribe(ctx)
	if err != nil {
		return fmt.Errorf("subscribe initial shared head: %w", err)
	}
	return r.sharedHead.RunSubscribed(ctx, heads, onReady)
}

func runBootstrapTasks(ctx context.Context, head domainchain.BlockHeader, tasks []bootstrapTask) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(tasks))
	for _, task := range tasks {
		task := task
		startSafeGoroutine(&wg, func(recovered any) {
			errCh <- fmt.Errorf("%s bootstrap panicked: %v", task.name, recovered)
		}, func() {
			if err := task.bootstrapper.StartBootstrapAt(ctx, head); err != nil {
				errCh <- fmt.Errorf("%s bootstrap: %w", task.name, err)
			}
		})
	}
	wg.Wait()
	close(errCh)
	var bootstrapErrors []error
	for err := range errCh {
		bootstrapErrors = append(bootstrapErrors, err)
	}
	return errors.Join(bootstrapErrors...)
}

func (r *syncLifecycle) runSync(name string, run func(context.Context) error) {
	r.startSafeGoroutine(name, func() {
		if err := run(r.runCtx); err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Error("pool sync stopped", zap.Uint64("chain_id", r.runtime.cfg.ChainID), zap.String("sync", name), zap.Error(err))
		}
	})
}

func (r *syncLifecycle) startSafeGoroutine(name string, run func()) {
	startSafeGoroutine(&r.wg, func(recovered any) {
		r.logger.Error("pool sync panicked",
			zap.Uint64("chain_id", r.runtime.cfg.ChainID),
			zap.String("sync", name),
			zap.Any("panic", recovered),
			zap.Stack("stack"),
		)
	}, run)
}

func (r *syncLifecycle) stop(ctx context.Context) error {
	if r.cancel != nil {
		r.cancel()
	}

	shutdownDone := make(chan struct{})
	go func() {
		r.wg.Wait()
		r.runtime.persistenceWG.Wait()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		r.runtime.resources.Close()
		r.logger.Info("pool sync shutdown complete", zap.Uint64("chain_id", r.runtime.cfg.ChainID))
		return nil
	case <-ctx.Done():
		if r.runtime.resources.cancelPersistence != nil {
			r.runtime.resources.cancelPersistence()
		}
		r.logger.Warn("pool sync shutdown timed out; deferring resource close until background work exits", zap.Error(ctx.Err()))
		startSafeGoroutine(nil, func(recovered any) {
			r.logger.Error("deferred resource close panicked", zap.Any("panic", recovered), zap.Stack("stack"))
		}, func() {
			<-shutdownDone
			r.runtime.resources.Close()
		})
		return ctx.Err()
	}
}
