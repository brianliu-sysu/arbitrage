package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	arbitrageapp "github.com/brianliu-sysu/uniswapv3/internal/application/arbitrage"
	assetapp "github.com/brianliu-sysu/uniswapv3/internal/application/asset"
	contractapp "github.com/brianliu-sysu/uniswapv3/internal/application/contract"
	poolmanager "github.com/brianliu-sysu/uniswapv3/internal/application/poolmanager"
	poolsapp "github.com/brianliu-sysu/uniswapv3/internal/application/pools"
	quotecombined "github.com/brianliu-sysu/uniswapv3/internal/application/quote/combined"
	quotecommitted "github.com/brianliu-sysu/uniswapv3/internal/application/quote/committed"
	quotepancakev3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/pancakev3"
	quotequickswapv3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/quickswapv3"
	quoteuniv3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/univ3"
	quoteuniv4 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/univ4"
	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	syncbalancer "github.com/brianliu-sysu/uniswapv3/internal/application/sync/balancer"
	syncpancakev3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/pancakev3"
	syncquickswapv3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/quickswapv3"
	syncv3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/univ3"
	syncv4 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/univ4"
	"github.com/brianliu-sysu/uniswapv3/internal/config"
	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quotebalancerdomain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/balancer"
	quotepancakev3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/pancakev3"
	quotequickswapv3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/quickswapv3"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	quoteuniv3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ3"
	quoteuniv4domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ4"
	chaininfra "github.com/brianliu-sysu/uniswapv3/internal/infrastructure/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/logging"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/registry"
	httpapi "github.com/brianliu-sysu/uniswapv3/internal/interfaces/http"
	"github.com/ethereum/go-ethereum/common"
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
			newQuickSwapPoolRegistry,
			newV4PoolRegistry,
			newBalancerPoolRegistry,
			newRuntimeBundle,
			newRuntimeSet,
			newQuoteV3AppService,
			newQuotePancakeV3AppService,
			newQuoteQuickSwapV3AppService,
			newQuoteV4AppService,
			newQuoteCombinedAppService,
			newPoolsAppService,
			newContractExecutorAppService,
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
	normalizedChains := cfg.NormalizedChains()
	if len(normalizedChains) == 0 {
		return config.Config{}, fmt.Errorf("at least one chain must be enabled")
	}
	for _, chain := range normalizedChains {
		if chain.RPC.URL == "" {
			return config.Config{}, fmt.Errorf("chains[%d]: rpc.url is required", chain.ChainID)
		}
		if chain.RPC.WSURL == "" {
			return config.Config{}, fmt.Errorf("chains[%d]: rpc.ws_url is required for block head subscription", chain.ChainID)
		}
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
	chainCfg := cfg.PrimaryRuntimeConfig()
	return chaininfra.NewServices(chainCfg.BlockchainConfig())
}

func newContractExecutorAppService() (*contractapp.AppService, error) {
	broadcaster, err := chaininfra.NewContractExecutorBroadcaster()
	if err != nil {
		return nil, err
	}
	return contractapp.NewAppService(broadcaster), nil
}

func newPoolRegistry(cfg config.Config) *registry.CompositeRegistry {
	chainCfg := cfg.PrimaryRuntimeConfig()
	return registry.NewCompositeRegistry(chainCfg.Sync.Univ3)
}

func newPancakePoolRegistry(cfg config.Config) *registry.PancakeCompositeRegistry {
	chainCfg := cfg.PrimaryRuntimeConfig()
	if !chainCfg.Sync.PancakeV3.IsActive() {
		return nil
	}
	return registry.NewPancakeCompositeRegistry(chainCfg.Sync.PancakeV3)
}

func newQuickSwapPoolRegistry(cfg config.Config) *registry.QuickSwapCompositeRegistry {
	chainCfg := cfg.PrimaryRuntimeConfig()
	if !chainCfg.Sync.QuickSwapV3.IsActive() {
		return nil
	}
	return registry.NewQuickSwapCompositeRegistry(chainCfg.Sync.QuickSwapV3)
}

func newV4PoolRegistry(cfg config.Config) (*registry.CompositeV4Registry, error) {
	chainCfg := cfg.PrimaryRuntimeConfig()
	if !chainCfg.Sync.Univ4.IsActive() {
		return nil, nil
	}
	return registry.NewCompositeV4Registry(chainCfg.Sync.Univ4)
}

func newBalancerPoolRegistry(cfg config.Config) (*registry.CompositeBalancerRegistry, error) {
	chainCfg := cfg.PrimaryRuntimeConfig()
	if !chainCfg.Sync.Balancer.IsActive() {
		return nil, nil
	}
	blockchainCfg := chainCfg.BlockchainConfig()
	return registry.NewCompositeBalancerRegistry(chainCfg.Sync.Balancer, blockchainCfg.BalancerVaultAddress, blockchainCfg.BalancerVaultV3Address)
}

type runtimeBundle struct {
	ChainID       uint64
	ChainName     string
	Sync          *syncv3.Services
	SyncPancake   *syncpancakev3.Services
	SyncQuickSwap *syncquickswapv3.Services
	SyncV4        *syncv4.Services
	SyncBalancer  *syncbalancer.Services
	Arbitrage     *arbitrageapp.Services
	MarketView    *quotecommitted.View
	PoolManagers  runtimePoolManagers
}

type runtimePoolManagers struct {
	V3          *poolmanager.PoolManager[common.Address]
	PancakeV3   *poolmanager.PoolManager[common.Address]
	QuickSwapV3 *poolmanager.PoolManager[common.Address]
	V4          *poolmanager.PoolManager[marketv4.PoolID]
	Balancer    *poolmanager.PoolManager[marketbalancer.PoolID]
}

type chainRuntime struct {
	cfg                   config.Config
	store                 *persistence.Services
	chain                 *chaininfra.Services
	poolRegistry          *registry.CompositeRegistry
	pancakePoolRegistry   *registry.PancakeCompositeRegistry
	quickSwapPoolRegistry *registry.QuickSwapCompositeRegistry
	v4PoolRegistry        *registry.CompositeV4Registry
	balancerPoolRegistry  *registry.CompositeBalancerRegistry
	bundle                *runtimeBundle
	ownsInfrastructure    bool
}

type runtimeSet struct {
	chains []*chainRuntime
}

func newRuntimeBundle(
	cfg config.Config,
	logger *zap.Logger,
	store *persistence.Services,
	chain *chaininfra.Services,
	poolRegistry *registry.CompositeRegistry,
	pancakePoolRegistry *registry.PancakeCompositeRegistry,
	quickSwapPoolRegistry *registry.QuickSwapCompositeRegistry,
	v4PoolRegistry *registry.CompositeV4Registry,
	balancerPoolRegistry *registry.CompositeBalancerRegistry,
	contractExecutor *contractapp.AppService,
) (*runtimeBundle, error) {
	cfg = cfg.PrimaryRuntimeConfig()
	deps := syncv3.ServiceDeps{
		Config:      cfg.SyncConfig(),
		Pools:       store.Pools,
		Snapshots:   store.Snapshots,
		Checkpoints: store.Checkpoints,
		Registry:    poolRegistry,
		Fetcher:     chain.LogFetcher,
		Parser:      chain.Parser,
		Blocks:      chain.Client,
		Bootstrap:   chain.PoolReader,
		Subscriber:  chain.HeadSub,
		Health:      []syncv3.HealthProbe{chain.Client},
		Listener:    syncv3.NopChangedPoolsListener{},
	}
	if store.Postgres != nil {
		deps.Health = append(deps.Health, store.Postgres)
	}

	var syncServices *syncv3.Services
	if cfg.Sync.Univ3.IsActive() {
		syncServices = syncv3.NewServices(deps)
	}

	var syncPancakeServices *syncpancakev3.Services
	if cfg.Sync.PancakeV3.IsActive() {
		if pancakePoolRegistry == nil {
			return nil, fmt.Errorf("pancake pool registry is required when sync.pancakev3 is enabled")
		}
		pancakeDeps := syncpancakev3.ServiceDeps{
			Config:      cfg.SyncConfig(),
			Pools:       store.PancakePools,
			Snapshots:   store.PancakeSnapshots,
			Checkpoints: store.PancakeCheckpoints,
			Registry:    pancakePoolRegistry,
			Fetcher:     chain.PancakeLogFetcher,
			Parser:      chain.PancakeParser,
			Blocks:      chain.Client,
			Bootstrap:   chain.PancakePoolReader,
			Subscriber:  chain.HeadSub,
			Health:      []syncpancakev3.HealthProbe{chain.Client},
			Listener:    syncpancakev3.NopChangedPoolsListener{},
		}
		if store.Postgres != nil {
			pancakeDeps.Health = append(pancakeDeps.Health, store.Postgres)
		}
		syncPancakeServices = syncpancakev3.NewServices(pancakeDeps)
	}

	var syncQuickSwapServices *syncquickswapv3.Services
	if cfg.Sync.QuickSwapV3.IsActive() {
		if quickSwapPoolRegistry == nil {
			return nil, fmt.Errorf("quickswap pool registry is required when sync.quickswapv3 is enabled")
		}
		quickSwapDeps := syncquickswapv3.ServiceDeps{
			Config:      cfg.SyncConfig(),
			Pools:       store.QuickSwapPools,
			Snapshots:   store.QuickSwapSnapshots,
			Checkpoints: store.QuickSwapCheckpoints,
			Registry:    quickSwapPoolRegistry,
			Fetcher:     chain.QuickSwapLogFetcher,
			Parser:      chain.QuickSwapParser,
			Blocks:      chain.Client,
			Bootstrap:   chain.QuickSwapPoolReader,
			Subscriber:  chain.HeadSub,
			Health:      []syncquickswapv3.HealthProbe{chain.Client},
			Listener:    syncquickswapv3.NopChangedPoolsListener{},
		}
		if store.Postgres != nil {
			quickSwapDeps.Health = append(quickSwapDeps.Health, store.Postgres)
		}
		syncQuickSwapServices = syncquickswapv3.NewServices(quickSwapDeps)
	}

	var syncV4Services *syncv4.Services
	if cfg.Sync.Univ4.IsActive() {
		if v4PoolRegistry == nil {
			return nil, fmt.Errorf("univ4 pool registry is required when sync.univ4 is enabled")
		}
		v4Deps := syncv4.ServiceDeps{
			Config:      cfg.SyncConfig(),
			Pools:       store.V4Pools,
			Snapshots:   store.V4Snapshots,
			Checkpoints: store.V4Checkpoints,
			Registry:    v4PoolRegistry,
			Fetcher:     chain.V4LogFetcher,
			Parser:      chain.V4Parser,
			Blocks:      chain.Client,
			Bootstrap:   chain.V4PoolReader,
			Subscriber:  chain.HeadSub,
			Health:      []syncv4.HealthProbe{chain.Client},
			Listener:    syncv4.NopChangedPoolsListener{},
		}
		if store.Postgres != nil {
			v4Deps.Health = append(v4Deps.Health, store.Postgres)
		}
		syncV4Services = syncv4.NewServices(v4Deps)
	}

	var syncBalancerServices *syncbalancer.Services
	if cfg.Sync.Balancer.IsActive() {
		if balancerPoolRegistry == nil {
			return nil, fmt.Errorf("balancer pool registry is required when sync.balancer is enabled")
		}
		balancerDeps := syncbalancer.ServiceDeps{
			Config:      cfg.SyncConfig(),
			Pools:       store.BalancerPools,
			Snapshots:   store.BalancerSnapshots,
			Checkpoints: store.BalancerCheckpoints,
			Registry:    balancerPoolRegistry,
			Fetcher:     chain.BalancerLogFetcher,
			Parser:      chain.BalancerParser,
			Blocks:      chain.Client,
			Bootstrap:   chain.BalancerPoolReader,
			Subscriber:  chain.HeadSub,
			Health:      []syncbalancer.HealthProbe{chain.Client},
			Listener:    syncbalancer.NopChangedPoolsListener{},
		}
		if store.Postgres != nil {
			balancerDeps.Health = append(balancerDeps.Health, store.Postgres)
		}
		syncBalancerServices = syncbalancer.NewServices(balancerDeps)
	}

	triangleCfg := cfg.Arbitrage.Triangle
	spreadCfg := cfg.Arbitrage.Spread
	configuredStartTokens := triangleCfg.StartTokenAddresses()
	spreadStartTokens := spreadCfg.StartTokenAddresses()
	minNetProfit := triangleCfg.MinNetProfit()
	spreadMinNetProfit := spreadCfg.MinNetProfit()
	triangleEnabled := cfg.TriangleArbitrageEnabled()
	spreadEnabled := cfg.SpreadArbitrageEnabled()
	if !triangleEnabled {
		configuredStartTokens = nil
		minNetProfit = nil
	}
	if !spreadEnabled {
		spreadStartTokens = nil
		spreadMinNetProfit = nil
	}

	optimizerMinAmount := triangleCfg.OptimizerMinAmount()
	optimizerMaxAmount := triangleCfg.OptimizerMaxAmount()
	optimizerIterations := triangleCfg.OptimizerIterations
	if !triangleEnabled && spreadEnabled {
		optimizerMinAmount = spreadCfg.OptimizerMinAmount()
		optimizerMaxAmount = spreadCfg.OptimizerMaxAmount()
		optimizerIterations = spreadCfg.OptimizerIterations
	}

	readiness := &quotecombined.SyncReadiness{}
	if syncServices != nil {
		readiness.V3 = syncServices.Readiness
	}
	if syncPancakeServices != nil {
		readiness.Pancake = syncPancakeServices.Readiness
	}
	if syncQuickSwapServices != nil {
		readiness.QuickSwap = syncQuickSwapServices.Readiness
	}
	if syncV4Services != nil {
		readiness.V4 = syncV4Services.Readiness
	}
	if syncBalancerServices != nil {
		readiness.Balancer = syncBalancerServices.Readiness
	}
	var univ3Active *syncapp.PoolLifecycleService[common.Address]
	var pancakeActive *syncapp.PoolLifecycleService[common.Address]
	var quickSwapActive *syncapp.PoolLifecycleService[common.Address]
	var univ4Active *syncapp.PoolLifecycleService[marketv4.PoolID]
	var balancerActive *syncapp.PoolLifecycleService[marketbalancer.PoolID]
	if syncServices != nil {
		univ3Active = syncServices.Lifecycle
	}
	if syncPancakeServices != nil {
		pancakeActive = syncPancakeServices.Lifecycle
	}
	if syncQuickSwapServices != nil {
		quickSwapActive = syncQuickSwapServices.Lifecycle
	}
	if syncV4Services != nil {
		univ4Active = syncV4Services.Lifecycle
	}
	if syncBalancerServices != nil {
		balancerActive = syncBalancerServices.Lifecycle
	}
	marketView := quotecommitted.NewView(quotecommitted.Sources{
		Univ3Pools:        store.Pools,
		PancakePools:      store.PancakePools,
		QuickSwapPools:    store.QuickSwapPools,
		Univ4Pools:        store.V4Pools,
		BalancerPools:     store.BalancerPools,
		Univ3Registry:     univ3Active,
		PancakeRegistry:   pancakeActive,
		QuickSwapRegistry: quickSwapActive,
		Univ4Registry:     univ4Active,
		BalancerRegistry:  balancerActive,
	})
	marketView.SetLogger(logger.Named("market-view"))

	arbitrageServices := arbitrageapp.NewServices(arbitrageapp.ServiceDeps{
		Logger:            logger,
		Pools:             store.Pools,
		PancakePools:      store.PancakePools,
		QuickSwapPools:    store.QuickSwapPools,
		V4Pools:           store.V4Pools,
		BalancerPools:     store.BalancerPools,
		Registry:          poolRegistry,
		PancakeRegistry:   pancakePoolRegistry.AsPoolRegistry(),
		QuickSwapRegistry: quickSwapPoolRegistry.AsPoolRegistry(),
		V4Registry:        v4PoolRegistry.AsPoolRegistry(),
		BalancerRegistry:  balancerPoolRegistry.AsPoolRegistry(),
		Quotes: quoteunified.NewQuoteService(
			quoteuniv3domain.NewQuoteService(),
			quotepancakev3domain.NewQuoteService(),
			quoteuniv4domain.NewQuoteService(),
			quotebalancerdomain.NewQuoteService(),
		),
		Readiness:             readiness,
		Repository:            store.Opportunities,
		Executor:              contractExecutor,
		ExecutionHead:         chain.Client,
		Execution:             executionConfigFromRuntime(cfg),
		LivePlan:              livePlanConfigFromRuntime(cfg),
		TriangleEnabled:       triangleEnabled,
		SpreadEnabled:         spreadEnabled,
		ConfiguredStartTokens: configuredStartTokens,
		SpreadStartTokens:     spreadStartTokens,
		MinNetProfitWei:       minNetProfit,
		SpreadMinNetProfitWei: spreadMinNetProfit,
		FlashLoanOptions: []domainarb.FlashLoanOption{
			{Protocol: domainarb.FlashLoanProtocolBalancer, FeePPM: cfg.Arbitrage.FlashLoan.BalancerFee()},
			{Protocol: domainarb.FlashLoanProtocolUniv4, FeePPM: cfg.Arbitrage.FlashLoan.Univ4Fee()},
		},
		MinAmount:                 optimizerMinAmount,
		MaxAmount:                 optimizerMaxAmount,
		OptimizerIterations:       optimizerIterations,
		EnabledProtocols:          enabledSyncProtocols(cfg),
		MarketView:                marketView,
		MarketVersion:             marketView,
		OpportunityPools:          marketView.Univ3Repository(),
		OpportunityPancakePools:   marketView.PancakeRepository(),
		OpportunityQuickSwapPools: marketView.QuickSwapRepository(),
		OpportunityV4Pools:        marketView.Univ4Repository(),
		OpportunityBalancerPools:  marketView.BalancerRepository(),
	})

	if syncServices != nil {
		syncServices.BlockApply.SetListener(arbitrageServices)
		syncServices.BlockApply.SetLogger(logger.Named("sync.clv3"))
	}
	if syncPancakeServices != nil {
		syncPancakeServices.BlockApply.SetListener(arbitrageapp.PancakePoolListener{Services: arbitrageServices})
		syncPancakeServices.BlockApply.SetLogger(logger.Named("sync.pancakev3"))
	}
	if syncQuickSwapServices != nil {
		syncQuickSwapServices.BlockApply.SetListener(arbitrageapp.QuickSwapPoolListener{Services: arbitrageServices})
		syncQuickSwapServices.BlockApply.SetLogger(logger.Named("sync.quickswapv3"))
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
	if cfg.ArbitrageEnabled() {
		arbitrageServices.LogDiagnostics(context.Background(), logger, "startup")
	} else {
		logger.Info("arbitrage discovery disabled")
	}

	return &runtimeBundle{
		ChainID:       cfg.ChainID,
		ChainName:     cfg.PrimaryChainName(),
		Sync:          syncServices,
		SyncPancake:   syncPancakeServices,
		SyncQuickSwap: syncQuickSwapServices,
		SyncV4:        syncV4Services,
		SyncBalancer:  syncBalancerServices,
		Arbitrage:     arbitrageServices,
		MarketView:    marketView,
		PoolManagers:  newRuntimePoolManagers(chain, syncServices, syncPancakeServices, syncQuickSwapServices, syncV4Services, syncBalancerServices, arbitrageServices),
	}, nil
}

type activeBalancerRegistry struct {
	lifecycle *syncapp.PoolLifecycleService[marketbalancer.PoolID]
	registry  marketbalancer.PoolRegistry
}

func (r activeBalancerRegistry) List(ctx context.Context) ([]marketbalancer.PoolID, error) {
	if r.lifecycle == nil {
		return nil, nil
	}
	return r.lifecycle.List(ctx)
}

func (r activeBalancerRegistry) GetSpec(ctx context.Context, id marketbalancer.PoolID) (marketbalancer.PoolSpec, error) {
	if r.registry == nil {
		return marketbalancer.PoolSpec{}, nil
	}
	return r.registry.GetSpec(ctx, id)
}

func newRuntimePoolManagers(
	chain *chaininfra.Services,
	syncServices *syncv3.Services,
	syncPancakeServices *syncpancakev3.Services,
	syncQuickSwapServices *syncquickswapv3.Services,
	syncV4Services *syncv4.Services,
	syncBalancerServices *syncbalancer.Services,
	arbitrageServices *arbitrageapp.Services,
) runtimePoolManagers {
	if chain == nil || arbitrageServices == nil {
		return runtimePoolManagers{}
	}
	managers := runtimePoolManagers{}
	if syncServices != nil {
		managers.V3 = poolmanager.NewPoolManager[common.Address](syncServices.NewOrchestrator(chain.Client), arbitrageServices)
	}
	if syncPancakeServices != nil {
		managers.PancakeV3 = poolmanager.NewPoolManager[common.Address](syncPancakeServices.NewOrchestrator(chain.Client), arbitrageServices)
	}
	if syncQuickSwapServices != nil {
		managers.QuickSwapV3 = poolmanager.NewPoolManager[common.Address](syncQuickSwapServices.NewOrchestrator(chain.Client), arbitrageServices)
	}
	if syncV4Services != nil {
		managers.V4 = poolmanager.NewPoolManager[marketv4.PoolID](syncV4Services.NewOrchestrator(chain.Client), arbitrageServices)
	}
	if syncBalancerServices != nil {
		managers.Balancer = poolmanager.NewPoolManager[marketbalancer.PoolID](syncBalancerServices.NewOrchestrator(chain.Client), arbitrageServices)
	}
	return managers
}

func enabledSyncProtocols(cfg config.Config) []arbitrageapp.SyncProtocol {
	protocols := make([]arbitrageapp.SyncProtocol, 0, 5)
	if cfg.Sync.Univ3.IsActive() {
		protocols = append(protocols, arbitrageapp.SyncProtocolUniv3)
	}
	if cfg.Sync.PancakeV3.IsActive() {
		protocols = append(protocols, arbitrageapp.SyncProtocolPancakeV3)
	}
	if cfg.Sync.QuickSwapV3.IsActive() {
		protocols = append(protocols, arbitrageapp.SyncProtocolQuickSwapV3)
	}
	if cfg.Sync.Univ4.IsActive() {
		protocols = append(protocols, arbitrageapp.SyncProtocolUniv4)
	}
	if cfg.Sync.Balancer.IsActive() {
		protocols = append(protocols, arbitrageapp.SyncProtocolBalancer)
	}
	return protocols
}

func executionConfigFromRuntime(cfg config.Config) arbitrageapp.ExecutionConfig {
	execution := cfg.Arbitrage.Execution
	return arbitrageapp.ExecutionConfig{
		Enabled:             execution.Enabled,
		RPCURL:              execution.ResolveRPCURL(cfg.RPC.URL),
		PrivateKey:          execution.PrivateKey,
		Executor:            execution.Executor(),
		FlashbotsRPCURL:     execution.FlashbotsRPCURL,
		FlashbotsPaymentBPS: execution.FlashbotsPaymentBPS,
		WrappedNativeToken:  execution.WETH(),
		GasLimit:            execution.GasLimit,
		GasPriceWei:         execution.GasPrice(),
		SkipEstimate:        execution.SkipEstimate,
		BroadcastToken:      execution.BroadcastToken,
		MaxOpportunityAge:   maxOpportunityAge(execution.MaxOpportunityAge),
		AllowedRouters:      execution.AllowedRouterAddresses(),
		AllowedSpenders:     execution.AllowedSpenderAddresses(),
	}
}

func livePlanConfigFromRuntime(cfg config.Config) arbitrageapp.LivePlanConfig {
	blockchainCfg := cfg.BlockchainConfig()
	execution := cfg.Arbitrage.Execution
	return arbitrageapp.LivePlanConfig{
		WETH:                execution.WETH(),
		BalancerVault:       blockchainCfg.BalancerVaultAddress,
		BalancerVaultV3:     blockchainCfg.BalancerVaultV3Address,
		BalancerRouterV3:    execution.BalancerRouterV3Address(),
		PoolManager:         blockchainCfg.PoolManagerAddress,
		SwapRouterV3:        execution.SwapRouterV3Address(),
		SwapRouterPancakeV3: execution.SwapRouterPancakeV3Address(),
		UniversalRouter:     execution.UniversalRouterAddress(),
		Executor:            execution.Executor(),
	}
}

func maxOpportunityAge(configured uint64) uint64 {
	if configured == 0 {
		return 3
	}
	return configured
}

func newRuntimeSet(
	cfg config.Config,
	logger *zap.Logger,
	primaryStore *persistence.Services,
	primaryChain *chaininfra.Services,
	primaryPoolRegistry *registry.CompositeRegistry,
	primaryPancakePoolRegistry *registry.PancakeCompositeRegistry,
	primaryQuickSwapPoolRegistry *registry.QuickSwapCompositeRegistry,
	primaryV4PoolRegistry *registry.CompositeV4Registry,
	primaryBalancerPoolRegistry *registry.CompositeBalancerRegistry,
	primaryBundle *runtimeBundle,
	contractExecutor *contractapp.AppService,
) (*runtimeSet, error) {
	normalized := cfg.NormalizedChains()
	if len(normalized) > 1 && !cfg.MemoryMode() {
		return nil, fmt.Errorf("multi-chain runtime currently requires persistence.memory=true until postgres repositories are chain_id scoped")
	}

	set := &runtimeSet{
		chains: []*chainRuntime{{
			cfg:                   cfg.PrimaryRuntimeConfig(),
			store:                 primaryStore,
			chain:                 primaryChain,
			poolRegistry:          primaryPoolRegistry,
			pancakePoolRegistry:   primaryPancakePoolRegistry,
			quickSwapPoolRegistry: primaryQuickSwapPoolRegistry,
			v4PoolRegistry:        primaryV4PoolRegistry,
			balancerPoolRegistry:  primaryBalancerPoolRegistry,
			bundle:                primaryBundle,
		}},
	}

	for _, chain := range normalized[1:] {
		chainCfg := cfg.RuntimeConfigForChain(chain)
		store := persistence.MemoryServices()
		chainServices, err := chaininfra.NewServices(chainCfg.BlockchainConfig())
		if err != nil {
			set.Close()
			return nil, fmt.Errorf("chain %d blockchain services: %w", chain.ChainID, err)
		}
		poolRegistry := registry.NewCompositeRegistry(chainCfg.Sync.Univ3)
		pancakePoolRegistry := newPancakePoolRegistry(chainCfg)
		quickSwapPoolRegistry := newQuickSwapPoolRegistry(chainCfg)
		v4PoolRegistry, err := newV4PoolRegistry(chainCfg)
		if err != nil {
			chainServices.Close()
			set.Close()
			return nil, fmt.Errorf("chain %d v4 registry: %w", chain.ChainID, err)
		}
		balancerPoolRegistry, err := newBalancerPoolRegistry(chainCfg)
		if err != nil {
			chainServices.Close()
			set.Close()
			return nil, fmt.Errorf("chain %d balancer registry: %w", chain.ChainID, err)
		}
		bundle, err := newRuntimeBundle(
			chainCfg,
			logger.Named(chain.Name),
			store,
			chainServices,
			poolRegistry,
			pancakePoolRegistry,
			quickSwapPoolRegistry,
			v4PoolRegistry,
			balancerPoolRegistry,
			contractExecutor,
		)
		if err != nil {
			chainServices.Close()
			set.Close()
			return nil, fmt.Errorf("chain %d runtime bundle: %w", chain.ChainID, err)
		}
		set.chains = append(set.chains, &chainRuntime{
			cfg:                   chainCfg,
			store:                 store,
			chain:                 chainServices,
			poolRegistry:          poolRegistry,
			pancakePoolRegistry:   pancakePoolRegistry,
			quickSwapPoolRegistry: quickSwapPoolRegistry,
			v4PoolRegistry:        v4PoolRegistry,
			balancerPoolRegistry:  balancerPoolRegistry,
			bundle:                bundle,
			ownsInfrastructure:    true,
		})
	}
	return set, nil
}

func (s *runtimeSet) Close() {
	if s == nil {
		return
	}
	for i := len(s.chains) - 1; i >= 0; i-- {
		chain := s.chains[i]
		if chain == nil || !chain.ownsInfrastructure {
			continue
		}
		if chain.chain != nil {
			chain.chain.Close()
		}
		if chain.store != nil {
			chain.store.Close()
		}
	}
}

func newQuoteV3AppService(
	cfg config.Config,
	store *persistence.Services,
	poolRegistry *registry.CompositeRegistry,
	bundle *runtimeBundle,
) *quoteuniv3.AppService {
	cfg = cfg.PrimaryRuntimeConfig()
	if !cfg.Sync.Univ3.IsActive() || bundle.Sync == nil {
		return nil
	}
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}
	return quoteuniv3.NewAppService(
		bundle.MarketView.Univ3Repository(),
		bundle.Sync.Lifecycle,
		quoteuniv3domain.NewQuoteService(),
		bundle.MarketView.Univ3Readiness(),
		maxHops,
	)
}

func newQuotePancakeV3AppService(
	cfg config.Config,
	store *persistence.Services,
	pancakePoolRegistry *registry.PancakeCompositeRegistry,
	bundle *runtimeBundle,
) *quotepancakev3.AppService {
	cfg = cfg.PrimaryRuntimeConfig()
	if !cfg.Sync.PancakeV3.IsActive() || bundle.SyncPancake == nil || pancakePoolRegistry == nil {
		return nil
	}
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}
	return quotepancakev3.NewAppService(
		bundle.MarketView.PancakeRepository(),
		bundle.SyncPancake.Lifecycle,
		quotepancakev3domain.NewQuoteService(),
		bundle.MarketView.PancakeReadiness(),
		maxHops,
	)
}

func newQuoteQuickSwapV3AppService(
	cfg config.Config,
	store *persistence.Services,
	quickSwapPoolRegistry *registry.QuickSwapCompositeRegistry,
	bundle *runtimeBundle,
) *quotequickswapv3.AppService {
	cfg = cfg.PrimaryRuntimeConfig()
	if !cfg.Sync.QuickSwapV3.IsActive() || bundle.SyncQuickSwap == nil || quickSwapPoolRegistry == nil {
		return nil
	}
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}
	return quotequickswapv3.NewAppService(
		bundle.MarketView.QuickSwapRepository(),
		bundle.SyncQuickSwap.Lifecycle,
		quotequickswapv3domain.NewQuoteService(),
		bundle.MarketView.QuickSwapReadiness(),
		maxHops,
	)
}

func newQuoteV4AppService(
	cfg config.Config,
	store *persistence.Services,
	v4PoolRegistry *registry.CompositeV4Registry,
	bundle *runtimeBundle,
) *quoteuniv4.AppService {
	cfg = cfg.PrimaryRuntimeConfig()
	if bundle.SyncV4 == nil || v4PoolRegistry == nil {
		return nil
	}
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}
	return quoteuniv4.NewAppService(
		bundle.MarketView.Univ4Repository(),
		bundle.SyncV4.Lifecycle,
		quoteuniv4domain.NewQuoteService(),
		bundle.MarketView.Univ4Readiness(),
		maxHops,
	)
}

func newQuoteCombinedAppService(
	cfg config.Config,
	store *persistence.Services,
	poolRegistry *registry.CompositeRegistry,
	pancakePoolRegistry *registry.PancakeCompositeRegistry,
	quickSwapPoolRegistry *registry.QuickSwapCompositeRegistry,
	v4PoolRegistry *registry.CompositeV4Registry,
	balancerPoolRegistry *registry.CompositeBalancerRegistry,
	bundle *runtimeBundle,
) *quotecombined.AppService {
	cfg = cfg.PrimaryRuntimeConfig()
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}
	var univ3Active *syncapp.PoolLifecycleService[common.Address]
	var pancakeActive *syncapp.PoolLifecycleService[common.Address]
	var quickSwapActive *syncapp.PoolLifecycleService[common.Address]
	var univ4Active *syncapp.PoolLifecycleService[marketv4.PoolID]
	var balancerActive *syncapp.PoolLifecycleService[marketbalancer.PoolID]
	if bundle.Sync != nil {
		univ3Active = bundle.Sync.Lifecycle
	}
	if bundle.SyncPancake != nil {
		pancakeActive = bundle.SyncPancake.Lifecycle
	}
	if bundle.SyncQuickSwap != nil {
		quickSwapActive = bundle.SyncQuickSwap.Lifecycle
	}
	if bundle.SyncV4 != nil {
		univ4Active = bundle.SyncV4.Lifecycle
	}
	if bundle.SyncBalancer != nil {
		balancerActive = bundle.SyncBalancer.Lifecycle
	}

	return quotecombined.NewAppService(
		bundle.MarketView.Univ3Repository(),
		bundle.MarketView.PancakeRepository(),
		bundle.MarketView.QuickSwapRepository(),
		bundle.MarketView.Univ4Repository(),
		bundle.MarketView.BalancerRepository(),
		univ3Active,
		pancakeActive,
		quickSwapActive,
		univ4Active,
		activeBalancerRegistry{lifecycle: balancerActive, registry: balancerPoolRegistry},
		quoteunified.NewQuoteService(
			quoteuniv3domain.NewQuoteService(),
			quotepancakev3domain.NewQuoteService(),
			quoteuniv4domain.NewQuoteService(),
			quotebalancerdomain.NewQuoteService(),
		),
		bundle.MarketView,
		maxHops,
	)
}

func newPoolsAppService(
	cfg config.Config,
	store *persistence.Services,
	poolRegistry *registry.CompositeRegistry,
	pancakePoolRegistry *registry.PancakeCompositeRegistry,
	v4PoolRegistry *registry.CompositeV4Registry,
	balancerPoolRegistry *registry.CompositeBalancerRegistry,
	chain *chaininfra.Services,
) *poolsapp.AppService {
	cfg = cfg.PrimaryRuntimeConfig()
	_ = cfg
	var pancakeRegistry marketpancake.PoolRegistry
	if pancakePoolRegistry != nil {
		pancakeRegistry = pancakePoolRegistry.AsPoolRegistry()
	}
	var v4Registry marketv4.PoolRegistry
	if v4PoolRegistry != nil {
		v4Registry = v4PoolRegistry.AsPoolRegistry()
	}
	var balancerRegistry marketbalancer.PoolRegistry
	if balancerPoolRegistry != nil {
		balancerRegistry = balancerPoolRegistry.AsPoolRegistry()
	}

	tokenService := assetapp.NewTokenMetadataService(store.Tokens, chain.ERC20)
	chainReaders := chaininfra.NewPoolsChainReaders(chain.Client, chain.PoolReader, chain.PancakePoolReader, chain.V4PoolReader, chain.BalancerPoolReader)
	return poolsapp.NewAppService(
		store.Pools,
		store.PancakePools,
		store.V4Pools,
		store.BalancerPools,
		poolRegistry,
		pancakeRegistry,
		v4Registry,
		balancerRegistry,
		tokenService,
		&poolsapp.ChainReaders{
			Head:     chainReaders,
			V4:       chainReaders,
			V3:       chainReaders,
			Pancake:  chainReaders.PancakeReader(),
			Balancer: chainReaders,
		},
	)
}

func newHTTPRouter(runtimes *runtimeSet, contractExecutor *contractapp.AppService, logger *zap.Logger) *gin.Engine {
	chains, services := newHTTPChainServices(runtimes, contractExecutor, logger)
	return httpapi.NewRouter(httpapi.Handlers{
		Health:           httpapi.NewHealthHandler(),
		QuoteCombined:    httpapi.NewQuoteCombinedChainHandler(chains, services.quoteCombined),
		QuoteV3:          httpapi.NewQuoteV3ChainHandler(chains, services.quoteV3),
		QuotePancakeV3:   httpapi.NewQuotePancakeV3ChainHandler(chains, services.quotePancakeV3),
		QuoteQuickSwapV3: httpapi.NewQuoteQuickSwapV3ChainHandler(chains, services.quoteQuickSwapV3),
		QuoteV4:          httpapi.NewQuoteV4ChainHandler(chains, services.quoteV4),
		Opportunities:    httpapi.NewOpportunityChainHandler(chains, services.opportunities, services.opportunityExecutors),
		Pools:            httpapi.NewPoolsChainHandler(chains, services.pools),
		ContractExecutor: httpapi.NewContractExecutorHandler(contractExecutor),
	})
}

type httpChainServices struct {
	quoteCombined        map[string]*quotecombined.AppService
	quoteV3              map[string]*quoteuniv3.AppService
	quotePancakeV3       map[string]*quotepancakev3.AppService
	quoteQuickSwapV3     map[string]*quotequickswapv3.AppService
	quoteV4              map[string]*quoteuniv4.AppService
	opportunities        map[string]domainarb.OpportunityRepository
	opportunityExecutors map[string]*arbitrageapp.OpportunityExecutor
	pools                map[string]*poolsapp.AppService
}

func newHTTPChainServices(runtimes *runtimeSet, contractExecutor *contractapp.AppService, logger *zap.Logger) ([]httpapi.ChainInfo, httpChainServices) {
	services := httpChainServices{
		quoteCombined:        make(map[string]*quotecombined.AppService),
		quoteV3:              make(map[string]*quoteuniv3.AppService),
		quotePancakeV3:       make(map[string]*quotepancakev3.AppService),
		quoteQuickSwapV3:     make(map[string]*quotequickswapv3.AppService),
		quoteV4:              make(map[string]*quoteuniv4.AppService),
		opportunities:        make(map[string]domainarb.OpportunityRepository),
		opportunityExecutors: make(map[string]*arbitrageapp.OpportunityExecutor),
		pools:                make(map[string]*poolsapp.AppService),
	}
	if runtimes == nil {
		return nil, services
	}

	chains := make([]httpapi.ChainInfo, 0, len(runtimes.chains))
	for i, runtime := range runtimes.chains {
		if runtime == nil || runtime.bundle == nil {
			continue
		}
		key := httpChainKey(runtime.bundle.ChainName)
		if key == "" {
			key = httpChainKey(fmt.Sprintf("chain-%d", runtime.bundle.ChainID))
		}
		chains = append(chains, httpapi.ChainInfo{
			Name:    runtime.bundle.ChainName,
			ChainID: runtime.bundle.ChainID,
			Primary: i == 0,
		})
		services.quoteCombined[key] = newQuoteCombinedAppService(
			runtime.cfg,
			runtime.store,
			runtime.poolRegistry,
			runtime.pancakePoolRegistry,
			runtime.quickSwapPoolRegistry,
			runtime.v4PoolRegistry,
			runtime.balancerPoolRegistry,
			runtime.bundle,
		)
		services.quoteV3[key] = newQuoteV3AppService(runtime.cfg, runtime.store, runtime.poolRegistry, runtime.bundle)
		services.quotePancakeV3[key] = newQuotePancakeV3AppService(runtime.cfg, runtime.store, runtime.pancakePoolRegistry, runtime.bundle)
		services.quoteQuickSwapV3[key] = newQuoteQuickSwapV3AppService(runtime.cfg, runtime.store, runtime.quickSwapPoolRegistry, runtime.bundle)
		services.quoteV4[key] = newQuoteV4AppService(runtime.cfg, runtime.store, runtime.v4PoolRegistry, runtime.bundle)
		services.opportunities[key] = runtime.store.Opportunities
		services.opportunityExecutors[key] = newOpportunityExecutor(runtime.cfg, runtime.store, contractExecutor, runtime.chain, logger)
		services.pools[key] = newPoolsAppService(
			runtime.cfg,
			runtime.store,
			runtime.poolRegistry,
			runtime.pancakePoolRegistry,
			runtime.v4PoolRegistry,
			runtime.balancerPoolRegistry,
			runtime.chain,
		)
	}
	return chains, services
}

func newOpportunityExecutor(
	cfg config.Config,
	store *persistence.Services,
	contractExecutor *contractapp.AppService,
	chain *chaininfra.Services,
	logger *zap.Logger,
) *arbitrageapp.OpportunityExecutor {
	if store == nil || store.Opportunities == nil || contractExecutor == nil || chain == nil {
		return nil
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	livePlan := livePlanConfigFromRuntime(cfg)
	encoder := arbitrageapp.NewLiveCalldataEncoder(livePlan, arbitrageapp.NewRepositoryRoutePoolLoader(
		store.Pools,
		store.PancakePools,
		store.QuickSwapPools,
		store.V4Pools,
		store.BalancerPools,
	))
	builder := arbitrageapp.NewLiveExecutionPlanBuilder(livePlan, encoder)
	return arbitrageapp.NewOpportunityExecutor(
		store.Opportunities,
		builder,
		contractExecutor,
		chain.Client,
		executionConfigFromRuntime(cfg),
		logger.Named("opportunity.execute"),
	)
}

func httpChainKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

type syncLifecycle struct {
	runCtx                context.Context
	cancel                context.CancelFunc
	wg                    sync.WaitGroup
	orchestrator          *syncv3.SyncOrchestrator
	orchestratorPancake   *syncpancakev3.SyncOrchestrator
	orchestratorQuickSwap *syncquickswapv3.SyncOrchestrator
	orchestratorV4        *syncv4.SyncOrchestrator
	orchestratorBalancer  *syncbalancer.SyncOrchestrator
	sharedHead            *syncapp.SharedHeadRunner
	bundle                *runtimeBundle
	chain                 *chaininfra.Services
	store                 *persistence.Services
	cfg                   config.Config
	logger                *zap.Logger
	poolRegistry          *registry.CompositeRegistry
	pancakePoolRegistry   *registry.PancakeCompositeRegistry
	quickSwapPoolRegistry *registry.QuickSwapCompositeRegistry
	v4PoolRegistry        *registry.CompositeV4Registry
	balancerPoolRegistry  *registry.CompositeBalancerRegistry
}

func registerSyncLifecycle(
	lifecycle fx.Lifecycle,
	logger *zap.Logger,
	runtimes *runtimeSet,
) {
	runners := make([]*syncLifecycle, 0, len(runtimes.chains))
	for _, runtime := range runtimes.chains {
		runner := newSyncLifecycle(runtime, logger.Named(runtime.bundle.ChainName))
		runners = append(runners, runner)
	}
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			for _, runner := range runners {
				if err := runner.start(ctx); err != nil {
					return err
				}
			}
			return nil
		},
		OnStop: func(ctx context.Context) error {
			for i := len(runners) - 1; i >= 0; i-- {
				if err := runners[i].stop(ctx); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

func newSyncLifecycle(runtime *chainRuntime, logger *zap.Logger) *syncLifecycle {
	runner := &syncLifecycle{
		bundle:                runtime.bundle,
		chain:                 runtime.chain,
		store:                 runtime.store,
		cfg:                   runtime.cfg,
		logger:                logger,
		poolRegistry:          runtime.poolRegistry,
		pancakePoolRegistry:   runtime.pancakePoolRegistry,
		quickSwapPoolRegistry: runtime.quickSwapPoolRegistry,
		v4PoolRegistry:        runtime.v4PoolRegistry,
		balancerPoolRegistry:  runtime.balancerPoolRegistry,
	}
	handlers := make([]syncapp.NamedHeadHandler, 0, 5)
	if runtime.bundle.Sync != nil {
		runner.orchestrator = runtime.bundle.Sync.NewOrchestrator(runtime.chain.Client)
		handlers = append(handlers, syncapp.NamedHeadHandler{Name: "univ3", Handler: runtime.bundle.Sync.HeadSync})
	}
	if runtime.bundle.SyncPancake != nil {
		runner.orchestratorPancake = runtime.bundle.SyncPancake.NewOrchestrator(runtime.chain.Client)
		handlers = append(handlers, syncapp.NamedHeadHandler{Name: "pancakev3", Handler: runtime.bundle.SyncPancake.HeadSync})
	}
	if runtime.bundle.SyncQuickSwap != nil {
		runner.orchestratorQuickSwap = runtime.bundle.SyncQuickSwap.NewOrchestrator(runtime.chain.Client)
		handlers = append(handlers, syncapp.NamedHeadHandler{Name: "quickswapv3", Handler: runtime.bundle.SyncQuickSwap.HeadSync})
	}
	if runtime.bundle.SyncV4 != nil {
		runner.orchestratorV4 = runtime.bundle.SyncV4.NewOrchestrator(runtime.chain.Client)
		handlers = append(handlers, syncapp.NamedHeadHandler{Name: "univ4", Handler: runtime.bundle.SyncV4.HeadSync})
	}
	if runtime.bundle.SyncBalancer != nil {
		runner.orchestratorBalancer = runtime.bundle.SyncBalancer.NewOrchestrator(runtime.chain.Client)
		handlers = append(handlers, syncapp.NamedHeadHandler{Name: "balancer", Handler: runtime.bundle.SyncBalancer.HeadSync})
	}
	if len(handlers) > 0 && runtime.chain != nil && runtime.chain.HeadSub != nil {
		runner.sharedHead = syncapp.NewSharedHeadRunner(runtime.chain.HeadSub, handlers, logger.Named("shared-head"))
		if runtime.bundle.Arbitrage != nil && runtime.bundle.Arbitrage.Coordinator != nil {
			coordinator := runtime.bundle.Arbitrage.Coordinator
			runner.sharedHead.SetBeforeHead(func(ctx context.Context, head domainchain.BlockHeader) error {
				return coordinator.PrepareHead(ctx, head)
			})
		}
	}
	return runner
}

func (r *syncLifecycle) start(_ context.Context) error {
	runCtx, cancel := context.WithCancel(context.Background())
	r.runCtx = runCtx
	r.cancel = cancel
	if r.bundle != nil && r.bundle.Arbitrage != nil && r.bundle.Arbitrage.Coordinator != nil {
		r.bundle.Arbitrage.Coordinator.SetScanContext(runCtx)
	}

	r.logger.Info("starting pool sync",
		zap.Uint64("chain_id", r.cfg.ChainID),
		zap.String("chain", r.bundle.ChainName),
		zap.String("persistence", r.store.BackendName()),
		zap.Bool("memory_mode", r.cfg.MemoryMode()),
		zap.Bool("univ3", r.cfg.Sync.Univ3.IsActive()),
		zap.Bool("univ3_subgraph", r.cfg.Sync.Univ3.Subgraph.IsEnabled()),
		zap.Int("univ3_pools", len(r.cfg.Sync.Univ3.Pools)),
		zap.Bool("pancakev3", r.cfg.Sync.PancakeV3.IsActive()),
		zap.Bool("pancakev3_subgraph", r.cfg.Sync.PancakeV3.Subgraph.IsEnabled()),
		zap.Int("pancakev3_pools", len(r.cfg.Sync.PancakeV3.Pools)),
		zap.Bool("quickswapv3", r.cfg.Sync.QuickSwapV3.IsActive()),
		zap.Bool("quickswapv3_subgraph", r.cfg.Sync.QuickSwapV3.Subgraph.IsEnabled()),
		zap.Int("quickswapv3_pools", len(r.cfg.Sync.QuickSwapV3.Pools)),
		zap.Bool("univ4", r.cfg.Sync.Univ4.IsActive()),
		zap.Int("univ4_poolmanager_pools", len(r.cfg.Sync.Univ4.PoolManager.Pools)),
		zap.Bool("univ4_subgraph", r.cfg.Sync.Univ4.Subgraph.IsEnabled()),
		zap.Bool("balancer", r.cfg.Sync.Balancer.IsActive()),
		zap.Bool("balancer_subgraph", r.cfg.Sync.Balancer.Subgraph.IsEnabled()),
		zap.Int("balancer_pools", len(r.cfg.Sync.Balancer.Pools)),
	)

	if r.orchestrator != nil || r.orchestratorPancake != nil || r.orchestratorQuickSwap != nil || r.orchestratorV4 != nil || r.orchestratorBalancer != nil || r.sharedHead != nil {
		r.runSync("shared-head", r.runSharedHeadLifecycle)
	}

	if r.bundle != nil && r.bundle.Arbitrage != nil && r.cfg.ArbitrageEnabled() {
		r.runArbitrageRouteWatcher()
	}
	r.runSubgraphDiscoveryWatchers()
	return nil
}

func (r *syncLifecycle) runSubgraphDiscoveryWatchers() {
	if r.bundle.Sync != nil {
		runSubgraphDiscovery(r, "univ3", r.cfg.Sync.Univ3.Subgraph.RefreshInterval, r.cfg.Sync.Univ3.Subgraph.IsEnabled(), r.poolRegistry, r.bundle.Sync.Lifecycle, r.orchestrator)
	}
	if r.bundle.SyncPancake != nil {
		runSubgraphDiscovery(r, "pancakev3", r.cfg.Sync.PancakeV3.Subgraph.RefreshInterval, r.cfg.Sync.PancakeV3.Subgraph.IsEnabled(), r.pancakePoolRegistry, r.bundle.SyncPancake.Lifecycle, r.orchestratorPancake)
	}
	if r.bundle.SyncQuickSwap != nil {
		runSubgraphDiscovery(r, "quickswapv3", r.cfg.Sync.QuickSwapV3.Subgraph.RefreshInterval, r.cfg.Sync.QuickSwapV3.Subgraph.IsEnabled(), r.quickSwapPoolRegistry, r.bundle.SyncQuickSwap.Lifecycle, r.orchestratorQuickSwap)
	}
	if r.bundle.SyncV4 != nil {
		runSubgraphDiscovery(r, "univ4", r.cfg.Sync.Univ4.Subgraph.RefreshInterval, r.cfg.Sync.Univ4.Subgraph.IsEnabled(), r.v4PoolRegistry, r.bundle.SyncV4.Lifecycle, r.orchestratorV4)
	}
	if r.bundle.SyncBalancer != nil {
		runSubgraphDiscovery(r, "balancer", r.cfg.Sync.Balancer.Subgraph.RefreshInterval, r.cfg.Sync.Balancer.Subgraph.IsEnabled(), r.balancerPoolRegistry, r.bundle.SyncBalancer.Lifecycle, r.orchestratorBalancer)
	}
}

type poolLister[PoolID comparable] interface {
	List(context.Context) ([]PoolID, error)
}

type poolOnboarder[PoolID comparable] interface {
	AddPool(context.Context, PoolID) error
}

func runSubgraphDiscovery[PoolID comparable](r *syncLifecycle, name string, interval time.Duration, enabled bool, registry poolLister[PoolID], lifecycle *syncapp.PoolLifecycleService[PoolID], onboarder poolOnboarder[PoolID]) {
	if !enabled || registry == nil || lifecycle == nil || onboarder == nil {
		return
	}
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-r.runCtx.Done():
				return
			case <-ticker.C:
				reconcileSubgraphPools(r, name, interval, registry, lifecycle, onboarder)
			}
		}
	}()
}

func reconcileSubgraphPools[PoolID comparable](r *syncLifecycle, name string, interval time.Duration, registry poolLister[PoolID], lifecycle *syncapp.PoolLifecycleService[PoolID], onboarder poolOnboarder[PoolID]) {
	started := time.Now()
	active := lifecycle.ListActive()
	r.logger.Info("subgraph pool refresh started", zap.String("protocol", name), zap.Duration("refresh_interval", interval), zap.Int("active_pools", len(active)))
	tracked, err := registry.List(r.runCtx)
	if err != nil {
		r.logger.Warn("subgraph pool refresh failed", zap.String("protocol", name), zap.Error(err), zap.Int64("duration_ms", time.Since(started).Milliseconds()))
		return
	}
	activeSet := make(map[PoolID]struct{}, len(active))
	for _, id := range active {
		activeSet[id] = struct{}{}
	}
	added := 0
	for _, id := range tracked {
		if _, ok := activeSet[id]; ok {
			continue
		}
		r.logger.Info("subgraph pool discovered", zap.String("protocol", name), zap.Any("pool", id))
		if err := onboarder.AddPool(r.runCtx, id); err != nil {
			r.logger.Warn("subgraph pool onboarding failed", zap.String("protocol", name), zap.Any("pool", id), zap.Error(err))
			continue
		}
		added++
		r.logger.Info("subgraph pool activated", zap.String("protocol", name), zap.Any("pool", id))
	}
	if added > 0 && r.bundle != nil && r.bundle.Arbitrage != nil {
		if routes, err := r.bundle.Arbitrage.RefreshArbitrageRoutes(r.runCtx); err != nil {
			r.logger.Warn("refresh arbitrage routes after subgraph update failed", zap.String("protocol", name), zap.Error(err))
		} else {
			r.logger.Info("arbitrage routes refreshed after subgraph update", zap.String("protocol", name), zap.Int("new_pools", added), zap.Int("routes", routes))
		}
	}
	r.logger.Info("subgraph pool refresh completed", zap.String("protocol", name), zap.Int("tracked_pools", len(tracked)), zap.Int("new_pools", added), zap.Int64("duration_ms", time.Since(started).Milliseconds()))
}

func (r *syncLifecycle) runSharedHeadLifecycle(ctx context.Context) error {
	if err := r.bootstrapAll(ctx); err != nil {
		return err
	}
	if r.sharedHead == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	if r.chain == nil || r.chain.Client == nil {
		return errors.New("shared head bootstrap requires a blockchain client")
	}
	head, err := r.chain.Client.GetLatestBlockHeader(ctx)
	if err != nil {
		return fmt.Errorf("load initial shared head: %w", err)
	}
	if err := r.sharedHead.HandleHead(ctx, head); err != nil {
		return fmt.Errorf("apply initial shared head: %w", err)
	}
	return r.sharedHead.Run(ctx)
}

type bootstrapTask struct {
	name string
	run  func(context.Context) error
}

func (r *syncLifecycle) bootstrapAll(ctx context.Context) error {
	tasks := make([]bootstrapTask, 0, 5)
	if r.orchestrator != nil {
		tasks = append(tasks, bootstrapTask{name: "univ3", run: r.orchestrator.StartBootstrap})
	}
	if r.orchestratorPancake != nil {
		tasks = append(tasks, bootstrapTask{name: "pancakev3", run: r.orchestratorPancake.StartBootstrap})
	}
	if r.orchestratorQuickSwap != nil {
		tasks = append(tasks, bootstrapTask{name: "quickswapv3", run: r.orchestratorQuickSwap.StartBootstrap})
	}
	if r.orchestratorV4 != nil {
		tasks = append(tasks, bootstrapTask{name: "univ4", run: r.orchestratorV4.StartBootstrap})
	}
	if r.orchestratorBalancer != nil {
		tasks = append(tasks, bootstrapTask{name: "balancer", run: r.orchestratorBalancer.StartBootstrap})
	}
	if len(tasks) == 0 {
		return nil
	}
	return runBootstrapTasks(ctx, tasks)
}

func runBootstrapTasks(ctx context.Context, tasks []bootstrapTask) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(tasks))
	for _, task := range tasks {
		task := task
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					errCh <- fmt.Errorf("%s bootstrap panicked: %v", task.name, recovered)
				}
			}()
			if err := task.run(ctx); err != nil {
				errCh <- fmt.Errorf("%s bootstrap: %w", task.name, err)
			}
		}()
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
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				r.logger.Error("pool sync panicked",
					zap.Uint64("chain_id", r.cfg.ChainID),
					zap.String("sync", name),
					zap.Any("panic", recovered),
					zap.Stack("stack"),
				)
			}
		}()

		if err := run(r.runCtx); err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Error("pool sync stopped", zap.Uint64("chain_id", r.cfg.ChainID), zap.String("sync", name), zap.Error(err))
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

	routes, err := r.bundle.Arbitrage.RefreshArbitrageRoutes(r.runCtx)
	if err != nil {
		r.logger.Warn("refresh arbitrage routes failed", zap.Error(err))
		return false
	}

	r.logger.Info("arbitrage routes refreshed",
		zap.Uint64("chain_id", r.cfg.ChainID),
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
	if r.bundle.SyncQuickSwap != nil && r.bundle.SyncQuickSwap.Readiness != nil {
		protocols = append(protocols, arbitrageProtocolState{
			name: "quickswapv3",
			ready: func() bool {
				return r.bundle.SyncQuickSwap.Readiness.IsSystemReady()
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
	r.logger.Info("pool sync shutdown complete", zap.Uint64("chain_id", r.cfg.ChainID))
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
			zap.String("quote_quickswapv3", "POST /api/v1/quickswapv3/quote"),
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
