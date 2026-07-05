package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"time"

	arbitrageapp "github.com/brianliu-sysu/uniswapv3/internal/application/arbitrage"
	quotecombined "github.com/brianliu-sysu/uniswapv3/internal/application/quote/combined"
	quoteuniv3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/univ3"
	quoteuniv4 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/univ4"
	syncv3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/univ3"
	syncv4 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/univ4"
	"github.com/brianliu-sysu/uniswapv3/internal/config"
	quoteuniv3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ3"
	quoteuniv4domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ4"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
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
			newV4PoolRegistry,
			newRuntimeBundle,
			provideSyncServices,
			provideSyncV4Services,
			provideArbitrageServices,
			newSyncOrchestrator,
			newSyncV4Orchestrator,
			newQuoteV3AppService,
			newQuoteV4AppService,
			newQuoteCombinedAppService,
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
	return registry.NewCompositeRegistry(cfg.Sync.V3)
}

func newV4PoolRegistry(cfg config.Config) (*registry.CompositeV4Registry, error) {
	if !cfg.Sync.V4.IsActive() {
		return nil, nil
	}
	return registry.NewCompositeV4Registry(cfg.Sync.V4)
}

type runtimeBundle struct {
	Sync      *syncv3.Services
	SyncV4    *syncv4.Services
	Arbitrage *arbitrageapp.Services
}

func newRuntimeBundle(
	cfg config.Config,
	logger *zap.Logger,
	store *persistence.Services,
	chain *chaininfra.Services,
	poolRegistry *registry.CompositeRegistry,
	v4PoolRegistry *registry.CompositeV4Registry,
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

	syncServices := syncv3.NewServices(deps)

	var syncV4Services *syncv4.Services
	if cfg.Sync.V4.IsActive() {
		if v4PoolRegistry == nil {
			return nil, fmt.Errorf("v4 pool registry is required when sync.v4 is enabled")
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

	triangleCfg := cfg.Arbitrage.Triangle
	strategies := arbitrageapp.TriangleStrategies(triangleCfg.StartTokenAddresses(), triangleCfg.MinNetProfit())
	if !cfg.TriangleArbitrageEnabled() {
		strategies = nil
	}

	readiness := &quotecombined.SyncReadiness{V3: syncServices.Readiness}
	if syncV4Services != nil {
		readiness.V4 = syncV4Services.Readiness
	}

	arbitrageServices := arbitrageapp.NewServices(arbitrageapp.ServiceDeps{
		Logger:              logger,
		Pools:               store.Pools,
		V4Pools:             store.V4Pools,
		Registry:            poolRegistry,
		V4Registry:          v4PoolRegistry,
		Quotes: quoteunified.NewQuoteService(
			quoteuniv3domain.NewQuoteService(),
			quoteuniv4domain.NewQuoteService(),
		),
		Readiness:           readiness,
		Repository:          store.Opportunities,
		Strategies:          strategies,
		MinAmount:           triangleCfg.OptimizerMinAmount(),
		MaxAmount:           triangleCfg.OptimizerMaxAmount(),
		OptimizerIterations: triangleCfg.OptimizerIterations,
	})

	syncServices.BlockApply.SetListener(arbitrageServices)
	if syncV4Services != nil {
		syncV4Services.BlockApply.SetListener(arbitrageapp.V4PoolListener{Services: arbitrageServices})
	}
	if cfg.TriangleArbitrageEnabled() {
		logger.Info("triangle arbitrage enabled",
			zap.Int("start_tokens", len(triangleCfg.StartTokens)),
			zap.Int("routes", len(arbitrageServices.Scan.Routes())),
		)
	} else {
		logger.Info("triangle arbitrage disabled")
	}

	return &runtimeBundle{
		Sync:      syncServices,
		SyncV4:    syncV4Services,
		Arbitrage: arbitrageServices,
	}, nil
}

func provideSyncServices(bundle *runtimeBundle) *syncv3.Services {
	return bundle.Sync
}

func provideSyncV4Services(bundle *runtimeBundle) *syncv4.Services {
	return bundle.SyncV4
}

func provideArbitrageServices(bundle *runtimeBundle) *arbitrageapp.Services {
	return bundle.Arbitrage
}

func newSyncOrchestrator(services *syncv3.Services, chain *chaininfra.Services) *syncv3.SyncOrchestrator {
	return services.NewOrchestrator(chain.Client)
}

func newSyncV4Orchestrator(services *syncv4.Services, chain *chaininfra.Services) *syncv4.SyncOrchestrator {
	if services == nil {
		return nil
	}
	return services.NewOrchestrator(chain.Client)
}

func newQuoteV3AppService(
	cfg config.Config,
	store *persistence.Services,
	poolRegistry *registry.CompositeRegistry,
	syncServices *syncv3.Services,
) *quoteuniv3.AppService {
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}
	return quoteuniv3.NewAppService(
		store.Pools,
		poolRegistry,
		quoteuniv3domain.NewQuoteService(),
		syncServices.Readiness,
		maxHops,
	)
}

func newQuoteV4AppService(
	cfg config.Config,
	store *persistence.Services,
	v4PoolRegistry *registry.CompositeV4Registry,
	syncV4Services *syncv4.Services,
) *quoteuniv4.AppService {
	if syncV4Services == nil || v4PoolRegistry == nil {
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
		syncV4Services.Readiness,
		maxHops,
	)
}

func newQuoteCombinedAppService(
	cfg config.Config,
	store *persistence.Services,
	poolRegistry *registry.CompositeRegistry,
	v4PoolRegistry *registry.CompositeV4Registry,
	syncServices *syncv3.Services,
	syncV4Services *syncv4.Services,
) *quotecombined.AppService {
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}

	readiness := &quotecombined.SyncReadiness{V3: syncServices.Readiness}
	if syncV4Services != nil {
		readiness.V4 = syncV4Services.Readiness
	}

	return quotecombined.NewAppService(
		store.Pools,
		store.V4Pools,
		poolRegistry,
		v4PoolRegistry,
		quoteunified.NewQuoteService(
			quoteuniv3domain.NewQuoteService(),
			quoteuniv4domain.NewQuoteService(),
		),
		readiness,
		maxHops,
	)
}

func newHTTPRouter(
	quoteCombined *quotecombined.AppService,
	quoteV3 *quoteuniv3.AppService,
	quoteV4 *quoteuniv4.AppService,
) *gin.Engine {
	return httpapi.NewRouter(httpapi.Handlers{
		Health:        httpapi.NewHealthHandler(),
		QuoteCombined: httpapi.NewQuoteCombinedHandler(quoteCombined),
		QuoteV3:       httpapi.NewQuoteV3Handler(quoteV3),
		QuoteV4:       httpapi.NewQuoteV4Handler(quoteV4),
	})
}

type syncLifecycle struct {
	cancel         context.CancelFunc
	orchestrator   *syncv3.SyncOrchestrator
	orchestratorV4 *syncv4.SyncOrchestrator
	chain          *chaininfra.Services
	store          *persistence.Services
	cfg            config.Config
	logger         *zap.Logger
}

func registerSyncLifecycle(
	lifecycle fx.Lifecycle,
	cfg config.Config,
	logger *zap.Logger,
	orchestrator *syncv3.SyncOrchestrator,
	orchestratorV4 *syncv4.SyncOrchestrator,
	chain *chaininfra.Services,
	store *persistence.Services,
) {
	runner := &syncLifecycle{
		orchestrator:   orchestrator,
		orchestratorV4: orchestratorV4,
		chain:          chain,
		store:          store,
		cfg:            cfg,
		logger:         logger,
	}
	lifecycle.Append(fx.Hook{
		OnStart: runner.start,
		OnStop:  runner.stop,
	})
}

func (r *syncLifecycle) start(_ context.Context) error {
	runCtx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel

	r.logger.Info("starting pool sync",
		zap.String("persistence", r.store.BackendName()),
		zap.Bool("memory_mode", r.cfg.MemoryMode()),
		zap.Bool("v3", r.cfg.Sync.V3.IsActive()),
		zap.Bool("v3_subgraph", r.cfg.Sync.V3.Subgraph.IsEnabled()),
		zap.Int("v3_pools", len(r.cfg.Sync.V3.Pools)),
		zap.Bool("v4", r.cfg.Sync.V4.IsActive()),
		zap.Int("v4_poolmanager_pools", len(r.cfg.Sync.V4.PoolManager.Pools)),
		zap.Bool("v4_subgraph", r.cfg.Sync.V4.Subgraph.IsEnabled()),
	)

	go func() {
		if err := r.orchestrator.Start(runCtx); err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Error("v3 pool sync stopped", zap.Error(err))
		}
	}()

	if r.orchestratorV4 != nil {
		go func() {
			if err := r.orchestratorV4.Start(runCtx); err != nil && !errors.Is(err, context.Canceled) {
				r.logger.Error("v4 pool sync stopped", zap.Error(err))
			}
		}()
	}
	return nil
}

func (r *syncLifecycle) stop(_ context.Context) error {
	if r.cancel != nil {
		r.cancel()
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
