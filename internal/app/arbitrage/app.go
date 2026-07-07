package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"sync"
	"time"

	arbitrageapp "github.com/brianliu-sysu/uniswapv3/internal/application/arbitrage"
	assetapp "github.com/brianliu-sysu/uniswapv3/internal/application/asset"
	poolsapp "github.com/brianliu-sysu/uniswapv3/internal/application/pools"
	quotecombined "github.com/brianliu-sysu/uniswapv3/internal/application/quote/combined"
	quotepancakev3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/pancakev3"
	quoteuniv3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/univ3"
	quoteuniv4 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/univ4"
	syncbalancer "github.com/brianliu-sysu/uniswapv3/internal/application/sync/balancer"
	syncpancakev3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/pancakev3"
	syncv3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/univ3"
	syncv4 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/univ4"
	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quotebalancerdomain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/balancer"
	quotepancakev3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/pancakev3"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	quoteuniv3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ3"
	quoteuniv4domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ4"
	chaininfra "github.com/brianliu-sysu/uniswapv3/internal/infrastructure/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/logging"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/registry"
	httpapi "github.com/brianliu-sysu/uniswapv3/internal/interfaces/http"
	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

// Params holds runtime options for the arbitrage application.
type Params struct {
	ConfigPath string
}

// ParseFlags parses standard CLI flags for the arbitrage binary.
func ParseFlags() Params {
	configPath := flag.String("config", config.DefaultPath, "path to config yaml")
	flag.Parse()
	return Params{ConfigPath: *configPath}
}

// Module wires dependencies and starts pool sync on application lifecycle.
func Module(params Params) fx.Option {
	return fx.Options(
		fx.Supply(params),
		fx.Provide(
			loadConfig,
			newLogger,
			newPersistence,
			newBlockchain,
			newPoolRegistry,
			newPancakePoolRegistry,
			newV4PoolRegistry,
			newBalancerPoolRegistry,
			newRuntimeBundle,
			newQuoteV3AppService,
			newQuotePancakeV3AppService,
			newQuoteV4AppService,
			newQuoteCombinedAppService,
			newPoolsAppService,
			newHTTPRouter,
		),
		fx.Invoke(registerLoggerLifecycle, registerSyncLifecycle, registerHTTPLifecycle),
	)
}

func loadConfig(params Params) (config.Config, error) {
	path := params.ConfigPath
	if path == "" {
		path = config.DefaultPath
	}
	cfg, err := config.Load(path)
	if err != nil {
		return config.Config{}, err
	}
	if cfg.RPC.URL == "" {
		return config.Config{}, fmt.Errorf("rpc.url is required")
	}
	if cfg.RPC.WSURL == "" {
		return config.Config{}, fmt.Errorf("rpc.ws_url is required for block head subscription")
	}
	return cfg, nil
}

func newLogger(cfg config.Config) (*zap.Logger, error) {
	return logging.New(cfg.Log)
}

func registerLoggerLifecycle(lifecycle fx.Lifecycle, logger *zap.Logger) {
	lifecycle.Append(fx.Hook{
		OnStop: func(_ context.Context) error {
			return logger.Sync()
		},
	})
}

func newPersistence(cfg config.Config) (*persistence.Services, error) {
	return persistence.NewServices(context.Background(), cfg.PersistenceConfig())
}

func newBlockchain(cfg config.Config) (*chaininfra.Services, error) {
	return chaininfra.NewServices(cfg.BlockchainConfig())
}

func newPoolRegistry(cfg config.Config) *registry.CompositeRegistry {
	return registry.NewCompositeRegistry(cfg.Sync.Univ3)
}

func newPancakePoolRegistry(cfg config.Config) *registry.PancakeCompositeRegistry {
	if !cfg.Sync.PancakeV3.IsActive() {
		return nil
	}
	return registry.NewPancakeCompositeRegistry(cfg.Sync.PancakeV3)
}

func newV4PoolRegistry(cfg config.Config) (*registry.CompositeV4Registry, error) {
	if !cfg.Sync.Univ4.IsActive() {
		return nil, nil
	}
	return registry.NewCompositeV4Registry(cfg.Sync.Univ4)
}

func newBalancerPoolRegistry(cfg config.Config) (*registry.CompositeBalancerRegistry, error) {
	if !cfg.Sync.Balancer.IsActive() {
		return nil, nil
	}
	return registry.NewCompositeBalancerRegistry(cfg.Sync.Balancer, cfg.BlockchainConfig().BalancerVaultAddress)
}

type runtimeBundle struct {
	Sync         *syncv3.Services
	SyncPancake  *syncpancakev3.Services
	SyncV4       *syncv4.Services
	SyncBalancer *syncbalancer.Services
	Arbitrage    *arbitrageapp.Services
}

func newRuntimeBundle(
	cfg config.Config,
	logger *zap.Logger,
	store *persistence.Services,
	chain *chaininfra.Services,
	poolRegistry *registry.CompositeRegistry,
	pancakePoolRegistry *registry.PancakeCompositeRegistry,
	v4PoolRegistry *registry.CompositeV4Registry,
	balancerPoolRegistry *registry.CompositeBalancerRegistry,
) (*runtimeBundle, error) {
	deps := chain.SyncDeps()
	persistDeps := store.SyncDeps()

	deps.Config = cfg.SyncConfig()
	deps.Pools = persistDeps.Pools
	deps.Snapshots = persistDeps.Snapshots
	deps.Checkpoints = persistDeps.Checkpoints
	deps.Registry = poolRegistry
	deps.Health = append(deps.Health, persistDeps.Health...)
	deps.Listener = syncv3.NopChangedPoolsListener{}

	var syncServices *syncv3.Services
	if cfg.Sync.Univ3.IsActive() {
		syncServices = syncv3.NewServices(deps)
	}

	var syncPancakeServices *syncpancakev3.Services
	if cfg.Sync.PancakeV3.IsActive() {
		if pancakePoolRegistry == nil {
			return nil, fmt.Errorf("pancake pool registry is required when sync.pancakev3 is enabled")
		}
		pancakeDeps := chain.SyncPancakeV3Deps()
		pancakePersist := store.SyncPancakeV3Deps()
		pancakeDeps.Config = cfg.SyncConfig()
		pancakeDeps.Pools = pancakePersist.Pools
		pancakeDeps.Snapshots = pancakePersist.Snapshots
		pancakeDeps.Checkpoints = pancakePersist.Checkpoints
		pancakeDeps.Registry = pancakePoolRegistry
		pancakeDeps.Health = append(pancakeDeps.Health, pancakePersist.Health...)
		pancakeDeps.Listener = syncpancakev3.NopChangedPoolsListener{}
		syncPancakeServices = syncpancakev3.NewServices(pancakeDeps)
	}

	var syncV4Services *syncv4.Services
	if cfg.Sync.Univ4.IsActive() {
		if v4PoolRegistry == nil {
			return nil, fmt.Errorf("univ4 pool registry is required when sync.univ4 is enabled")
		}
		v4Deps := chain.SyncV4Deps()
		v4Persist := store.SyncV4Deps()
		v4Deps.Config = cfg.SyncConfig()
		v4Deps.Pools = v4Persist.Pools
		v4Deps.Snapshots = v4Persist.Snapshots
		v4Deps.Checkpoints = v4Persist.Checkpoints
		v4Deps.Registry = v4PoolRegistry
		v4Deps.Health = append(v4Deps.Health, v4Persist.Health...)
		v4Deps.Listener = syncv4.NopChangedPoolsListener{}
		syncV4Services = syncv4.NewServices(v4Deps)
	}

	var syncBalancerServices *syncbalancer.Services
	if cfg.Sync.Balancer.IsActive() {
		if balancerPoolRegistry == nil {
			return nil, fmt.Errorf("balancer pool registry is required when sync.balancer is enabled")
		}
		balancerDeps := chain.SyncBalancerDeps()
		balancerPersist := store.SyncBalancerDeps()
		balancerDeps.Config = cfg.SyncConfig()
		balancerDeps.Pools = balancerPersist.Pools
		balancerDeps.Snapshots = balancerPersist.Snapshots
		balancerDeps.Checkpoints = balancerPersist.Checkpoints
		balancerDeps.Registry = balancerPoolRegistry
		balancerDeps.Health = append(balancerDeps.Health, balancerPersist.Health...)
		balancerDeps.Listener = syncbalancer.NopChangedPoolsListener{}
		syncBalancerServices = syncbalancer.NewServices(balancerDeps)
	}

	triangleCfg := cfg.Arbitrage.Triangle
	configuredStartTokens := triangleCfg.StartTokenAddresses()
	minNetProfit := triangleCfg.MinNetProfit()
	if !cfg.TriangleArbitrageEnabled() {
		configuredStartTokens = nil
		minNetProfit = nil
	}

	readiness := &quotecombined.SyncReadiness{}
	if syncServices != nil {
		readiness.V3 = syncServices.Readiness
	}
	if syncPancakeServices != nil {
		readiness.Pancake = syncPancakeServices.Readiness
	}
	if syncV4Services != nil {
		readiness.V4 = syncV4Services.Readiness
	}
	if syncBalancerServices != nil {
		readiness.Balancer = syncBalancerServices.Readiness
	}

	arbitrageServices := arbitrageapp.NewServices(arbitrageapp.ServiceDeps{
		Logger:           logger,
		Pools:            store.Pools,
		PancakePools:     store.PancakePools,
		V4Pools:          store.V4Pools,
		BalancerPools:    store.BalancerPools,
		Registry:         poolRegistry,
		PancakeRegistry:  pancakePoolRegistry.AsPoolRegistry(),
		V4Registry:       v4PoolRegistry.AsPoolRegistry(),
		BalancerRegistry: balancerPoolRegistry.AsPoolRegistry(),
		Quotes: quoteunified.NewQuoteService(
			quoteuniv3domain.NewQuoteService(),
			quotepancakev3domain.NewQuoteService(),
			quoteuniv4domain.NewQuoteService(),
			quotebalancerdomain.NewQuoteService(),
		),
		Readiness:             readiness,
		Repository:            store.Opportunities,
		ConfiguredStartTokens: configuredStartTokens,
		MinNetProfitWei:       minNetProfit,
		MinAmount:             triangleCfg.OptimizerMinAmount(),
		MaxAmount:             triangleCfg.OptimizerMaxAmount(),
		OptimizerIterations:   triangleCfg.OptimizerIterations,
	})

	if syncServices != nil {
		syncServices.BlockApply.SetListener(arbitrageServices)
		syncServices.BlockApply.SetLogger(logger.Named("sync.clv3"))
	}
	if syncPancakeServices != nil {
		syncPancakeServices.BlockApply.SetListener(arbitrageapp.PancakePoolListener{Services: arbitrageServices})
		syncPancakeServices.BlockApply.SetLogger(logger.Named("sync.pancakev3"))
	}
	if syncV4Services != nil {
		syncV4Services.BlockApply.SetListener(arbitrageapp.V4PoolListener{Services: arbitrageServices})
		syncV4Services.BlockApply.SetLogger(logger.Named("sync.univ4"))
		syncV4Services.Bootstrap.SetLogger(logger.Named("sync.univ4"))
	}
	if syncBalancerServices != nil {
		syncBalancerServices.BlockApply.SetListener(arbitrageapp.BalancerPoolListener{Services: arbitrageServices})
		syncBalancerServices.BlockApply.SetLogger(logger.Named("sync.balancer"))
		syncBalancerServices.Bootstrap.SetLogger(logger.Named("sync.balancer"))
	}
	if cfg.TriangleArbitrageEnabled() {
		arbitrageServices.LogDiagnostics(context.Background(), logger, "startup")
	} else {
		logger.Info("triangle arbitrage disabled")
	}

	return &runtimeBundle{
		Sync:         syncServices,
		SyncPancake:  syncPancakeServices,
		SyncV4:       syncV4Services,
		SyncBalancer: syncBalancerServices,
		Arbitrage:    arbitrageServices,
	}, nil
}

func newQuoteV3AppService(
	cfg config.Config,
	store *persistence.Services,
	poolRegistry *registry.CompositeRegistry,
	bundle *runtimeBundle,
) *quoteuniv3.AppService {
	if !cfg.Sync.Univ3.IsActive() || bundle.Sync == nil {
		return nil
	}
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}
	return quoteuniv3.NewAppService(
		store.Pools,
		poolRegistry,
		quoteuniv3domain.NewQuoteService(),
		bundle.Sync.Readiness,
		maxHops,
	)
}

func newQuotePancakeV3AppService(
	cfg config.Config,
	store *persistence.Services,
	pancakePoolRegistry *registry.PancakeCompositeRegistry,
	bundle *runtimeBundle,
) *quotepancakev3.AppService {
	if !cfg.Sync.PancakeV3.IsActive() || bundle.SyncPancake == nil || pancakePoolRegistry == nil {
		return nil
	}
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}
	return quotepancakev3.NewAppService(
		store.PancakePools,
		pancakePoolRegistry,
		quotepancakev3domain.NewQuoteService(),
		bundle.SyncPancake.Readiness,
		maxHops,
	)
}

func newQuoteV4AppService(
	cfg config.Config,
	store *persistence.Services,
	v4PoolRegistry *registry.CompositeV4Registry,
	bundle *runtimeBundle,
) *quoteuniv4.AppService {
	if bundle.SyncV4 == nil || v4PoolRegistry == nil {
		return nil
	}
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}
	return quoteuniv4.NewAppService(
		store.V4Pools,
		v4PoolRegistry,
		quoteuniv4domain.NewQuoteService(),
		bundle.SyncV4.Readiness,
		maxHops,
	)
}

func newQuoteCombinedAppService(
	cfg config.Config,
	store *persistence.Services,
	poolRegistry *registry.CompositeRegistry,
	pancakePoolRegistry *registry.PancakeCompositeRegistry,
	v4PoolRegistry *registry.CompositeV4Registry,
	balancerPoolRegistry *registry.CompositeBalancerRegistry,
	bundle *runtimeBundle,
) *quotecombined.AppService {
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}

	readiness := &quotecombined.SyncReadiness{}
	if bundle.Sync != nil {
		readiness.V3 = bundle.Sync.Readiness
	}
	if bundle.SyncPancake != nil {
		readiness.Pancake = bundle.SyncPancake.Readiness
	}
	if bundle.SyncV4 != nil {
		readiness.V4 = bundle.SyncV4.Readiness
	}
	if bundle.SyncBalancer != nil {
		readiness.Balancer = bundle.SyncBalancer.Readiness
	}

	return quotecombined.NewAppService(
		store.Pools,
		store.PancakePools,
		store.V4Pools,
		store.BalancerPools,
		poolRegistry,
		pancakePoolRegistry.AsPoolRegistry(),
		v4PoolRegistry.AsPoolRegistry(),
		balancerPoolRegistry.AsPoolRegistry(),
		quoteunified.NewQuoteService(
			quoteuniv3domain.NewQuoteService(),
			quotepancakev3domain.NewQuoteService(),
			quoteuniv4domain.NewQuoteService(),
			quotebalancerdomain.NewQuoteService(),
		),
		readiness,
		maxHops,
	)
}

func newPoolsAppService(
	_ config.Config,
	store *persistence.Services,
	poolRegistry *registry.CompositeRegistry,
	pancakePoolRegistry *registry.PancakeCompositeRegistry,
	v4PoolRegistry *registry.CompositeV4Registry,
	chain *chaininfra.Services,
) *poolsapp.AppService {
	var pancakeRegistry marketpancake.PoolRegistry
	if pancakePoolRegistry != nil {
		pancakeRegistry = pancakePoolRegistry.AsPoolRegistry()
	}
	var v4Registry marketv4.PoolRegistry
	if v4PoolRegistry != nil {
		v4Registry = v4PoolRegistry.AsPoolRegistry()
	}

	tokenService := assetapp.NewTokenMetadataService(store.Tokens, chain.ERC20)
	return poolsapp.NewAppService(
		store.Pools,
		store.PancakePools,
		store.V4Pools,
		poolRegistry,
		pancakeRegistry,
		v4Registry,
		tokenService,
		chaininfra.NewPoolsChainReaders(chain.Client, chain.PoolReader, chain.PancakePoolReader, chain.V4PoolReader).AsChainReaders(),
	)
}

func newHTTPRouter(
	quoteCombined *quotecombined.AppService,
	quoteV3 *quoteuniv3.AppService,
	quotePancakeV3 *quotepancakev3.AppService,
	quoteV4 *quoteuniv4.AppService,
	store *persistence.Services,
	pools *poolsapp.AppService,
) *gin.Engine {
	return httpapi.NewRouter(httpapi.Handlers{
		Health:         httpapi.NewHealthHandler(),
		QuoteCombined:  httpapi.NewQuoteCombinedHandler(quoteCombined),
		QuoteV3:        httpapi.NewQuoteV3Handler(quoteV3),
		QuotePancakeV3: httpapi.NewQuotePancakeV3Handler(quotePancakeV3),
		QuoteV4:        httpapi.NewQuoteV4Handler(quoteV4),
		Opportunities:  httpapi.NewOpportunityHandler(store.Opportunities),
		Pools:          httpapi.NewPoolsHandler(pools),
	})
}

type syncLifecycle struct {
	runCtx               context.Context
	cancel               context.CancelFunc
	wg                   sync.WaitGroup
	orchestrator         *syncv3.SyncOrchestrator
	orchestratorPancake  *syncpancakev3.SyncOrchestrator
	orchestratorV4       *syncv4.SyncOrchestrator
	orchestratorBalancer *syncbalancer.SyncOrchestrator
	bundle               *runtimeBundle
	chain                *chaininfra.Services
	store                *persistence.Services
	cfg                  config.Config
	logger               *zap.Logger
}

func registerSyncLifecycle(
	lifecycle fx.Lifecycle,
	cfg config.Config,
	logger *zap.Logger,
	bundle *runtimeBundle,
	chain *chaininfra.Services,
	store *persistence.Services,
) {
	runner := &syncLifecycle{
		orchestrator:        nil,
		orchestratorPancake: nil,
		orchestratorV4:      nil,
		bundle:              bundle,
		chain:               chain,
		store:               store,
		cfg:                 cfg,
		logger:              logger,
	}
	if bundle.Sync != nil {
		runner.orchestrator = bundle.Sync.NewOrchestrator(chain.Client)
	}
	if bundle.SyncPancake != nil {
		runner.orchestratorPancake = bundle.SyncPancake.NewOrchestrator(chain.Client)
	}
	if bundle.SyncV4 != nil {
		runner.orchestratorV4 = bundle.SyncV4.NewOrchestrator(chain.Client)
	}
	if bundle.SyncBalancer != nil {
		runner.orchestratorBalancer = bundle.SyncBalancer.NewOrchestrator(chain.Client)
	}
	lifecycle.Append(fx.Hook{
		OnStart: runner.start,
		OnStop:  runner.stop,
	})
}

func (r *syncLifecycle) start(_ context.Context) error {
	runCtx, cancel := context.WithCancel(context.Background())
	r.runCtx = runCtx
	r.cancel = cancel

	r.logger.Info("starting pool sync",
		zap.String("persistence", r.store.BackendName()),
		zap.Bool("memory_mode", r.cfg.MemoryMode()),
		zap.Bool("univ3", r.cfg.Sync.Univ3.IsActive()),
		zap.Bool("univ3_subgraph", r.cfg.Sync.Univ3.Subgraph.IsEnabled()),
		zap.Int("univ3_pools", len(r.cfg.Sync.Univ3.Pools)),
		zap.Bool("pancakev3", r.cfg.Sync.PancakeV3.IsActive()),
		zap.Bool("pancakev3_subgraph", r.cfg.Sync.PancakeV3.Subgraph.IsEnabled()),
		zap.Int("pancakev3_pools", len(r.cfg.Sync.PancakeV3.Pools)),
		zap.Bool("univ4", r.cfg.Sync.Univ4.IsActive()),
		zap.Int("univ4_poolmanager_pools", len(r.cfg.Sync.Univ4.PoolManager.Pools)),
		zap.Bool("univ4_subgraph", r.cfg.Sync.Univ4.Subgraph.IsEnabled()),
		zap.Bool("balancer", r.cfg.Sync.Balancer.IsActive()),
		zap.Bool("balancer_subgraph", r.cfg.Sync.Balancer.Subgraph.IsEnabled()),
		zap.Int("balancer_pools", len(r.cfg.Sync.Balancer.Pools)),
	)

	if r.orchestrator != nil {
		r.runSync("univ3", func(ctx context.Context) error {
			return r.orchestrator.Start(ctx)
		})
	}

	if r.orchestratorPancake != nil {
		r.runSync("pancakev3", func(ctx context.Context) error {
			return r.orchestratorPancake.Start(ctx)
		})
	}

	if r.orchestratorV4 != nil {
		r.runSync("univ4", func(ctx context.Context) error {
			return r.orchestratorV4.Start(ctx)
		})
	}

	if r.orchestratorBalancer != nil {
		r.runSync("balancer", func(ctx context.Context) error {
			return r.orchestratorBalancer.Start(ctx)
		})
	}

	if r.bundle != nil && r.bundle.Arbitrage != nil && r.cfg.TriangleArbitrageEnabled() {
		r.runArbitrageRouteWatcher()
	}
	return nil
}

func (r *syncLifecycle) runSync(name string, run func(context.Context) error) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				r.logger.Error("pool sync panicked",
					zap.String("sync", name),
					zap.Any("panic", recovered),
					zap.Stack("stack"),
				)
			}
		}()

		if err := run(r.runCtx); err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Error("pool sync stopped", zap.String("sync", name), zap.Error(err))
		}
	}()
}

func (r *syncLifecycle) runArbitrageRouteWatcher() {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()

		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		protocolReady := make(map[string]bool)
		for {
			select {
			case <-r.runCtx.Done():
				return
			case <-ticker.C:
				if r.tryRefreshArbitrageRoutes(protocolReady) {
					r.bundle.Arbitrage.LogDiagnostics(r.runCtx, r.logger, "routes_refreshed")
				}
			}
		}
	}()
}

func (r *syncLifecycle) tryRefreshArbitrageRoutes(protocolReady map[string]bool) bool {
	if r.bundle == nil || r.bundle.Arbitrage == nil {
		return false
	}

	changed := false
	for _, protocol := range r.arbitrageProtocols() {
		if protocol.ready() {
			if !protocolReady[protocol.name] {
				protocolReady[protocol.name] = true
				changed = true
			}
		}
	}
	if !changed {
		return false
	}

	routes, err := r.bundle.Arbitrage.RefreshTriangleRoutes(r.runCtx)
	if err != nil {
		r.logger.Warn("refresh triangle routes failed", zap.Error(err))
		return false
	}

	r.logger.Info("triangle routes refreshed",
		zap.Int("routes", routes),
		zap.Int("start_tokens", len(r.bundle.Arbitrage.StartTokens())),
	)
	return true
}

type arbitrageProtocolState struct {
	name  string
	ready func() bool
}

func (r *syncLifecycle) arbitrageProtocols() []arbitrageProtocolState {
	protocols := []arbitrageProtocolState{
		{
			name: "univ3",
			ready: func() bool {
				return r.bundle.Sync != nil && r.bundle.Sync.Readiness != nil && r.bundle.Sync.Readiness.IsSystemReady()
			},
		},
	}
	if r.bundle.SyncPancake != nil && r.bundle.SyncPancake.Readiness != nil {
		protocols = append(protocols, arbitrageProtocolState{
			name: "pancakev3",
			ready: func() bool {
				return r.bundle.SyncPancake.Readiness.IsSystemReady()
			},
		})
	}
	if r.bundle.SyncV4 != nil && r.bundle.SyncV4.Readiness != nil {
		protocols = append(protocols, arbitrageProtocolState{
			name: "univ4",
			ready: func() bool {
				return r.bundle.SyncV4.Readiness.IsSystemReady()
			},
		})
	}
	if r.bundle.SyncBalancer != nil && r.bundle.SyncBalancer.Readiness != nil {
		protocols = append(protocols, arbitrageProtocolState{
			name: "balancer",
			ready: func() bool {
				return r.bundle.SyncBalancer.Readiness.IsSystemReady()
			},
		})
	}
	return protocols
}

func (r *syncLifecycle) stop(ctx context.Context) error {
	if r.cancel != nil {
		r.cancel()
	}

	waitDone := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
	case <-ctx.Done():
		r.logger.Warn("pool sync shutdown timed out", zap.Error(ctx.Err()))
	}

	r.chain.Close()
	r.store.Close()
	r.logger.Info("pool sync shutdown complete")
	return nil
}

type httpLifecycle struct {
	server *http.Server
	logger *zap.Logger
}

func registerHTTPLifecycle(lifecycle fx.Lifecycle, cfg config.Config, logger *zap.Logger, router *gin.Engine) {
	if !cfg.HTTP.Enabled {
		logger.Info("http server disabled")
		return
	}

	runner := &httpLifecycle{
		server: &http.Server{
			Addr:              cfg.HTTP.ListenAddr(),
			Handler:           router,
			ReadHeaderTimeout: 5 * time.Second,
		},
		logger: logger,
	}
	lifecycle.Append(fx.Hook{
		OnStart: runner.start,
		OnStop:  runner.stop,
	})
}

func (h *httpLifecycle) start(_ context.Context) error {
	go func() {
		h.logger.Info("starting http server",
			zap.String("addr", h.server.Addr),
			zap.String("health", "GET /health, GET /api/v1/health"),
			zap.String("quote_cross_pool", "POST /api/v1/quote"),
			zap.String("quote_v3", "POST /api/v1/univ3/quote"),
			zap.String("quote_pancakev3", "POST /api/v1/pancakev3/quote"),
			zap.String("quote_v4", "POST /api/v1/univ4/quote"),
		)
		if err := h.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.logger.Error("http server stopped", zap.Error(err))
		}
	}()
	return nil
}

func (h *httpLifecycle) stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := h.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}
	h.logger.Info("http server shutdown complete")
	return nil
}
