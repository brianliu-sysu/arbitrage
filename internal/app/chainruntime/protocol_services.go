package chainruntime

import (
	"fmt"
	"strings"

	"go.uber.org/zap"

	arbitrageapp "github.com/brianliu-sysu/uniswapv3/internal/application/arbitrage"
	contractapp "github.com/brianliu-sysu/uniswapv3/internal/application/contract"
	"github.com/brianliu-sysu/uniswapv3/internal/application/marketpipeline"
	"github.com/brianliu-sysu/uniswapv3/internal/application/marketstore"
	syncbalancer "github.com/brianliu-sysu/uniswapv3/internal/application/sync/balancer"
	synccontract "github.com/brianliu-sysu/uniswapv3/internal/application/sync/contract"
	syncpancakev3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/pancakev3"
	syncquickswapv3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/quickswapv3"
	syncv3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/univ3"
	syncv4 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/univ4"
	"github.com/brianliu-sysu/uniswapv3/internal/config"
	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	quotebalancerdomain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/balancer"
	quotepancakev3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/pancakev3"
	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	quoteuniv3domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ3"
	quoteuniv4domain "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/univ4"
	chaininfra "github.com/brianliu-sysu/uniswapv3/internal/infrastructure/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence"
	"github.com/ethereum/go-ethereum/common"
)

type protocolServices struct {
	modules []protocolModule
}

type protocolModule interface {
	Name() string
	Bootstrapper() protocolBootstrapper
	BlockPreparer() synccontract.BlockPreparer
	BindMarketReports(marketpipeline.ReportReceiver, *zap.Logger)
	StartDiscovery(*syncLifecycle, config.ChainConfig)
}

type univ3ProtocolModule struct {
	services  *syncv3.Services
	resources *univ3Resources
}
type pancakeProtocolModule struct {
	services  *syncpancakev3.Services
	resources *pancakeV3Resources
}
type quickSwapProtocolModule struct {
	services  *syncquickswapv3.Services
	resources *quickSwapV3Resources
}
type univ4ProtocolModule struct {
	services  *syncv4.Services
	resources *univ4Resources
}
type balancerProtocolModule struct {
	services  *syncbalancer.Services
	resources *balancerResources
}

func (m *univ3ProtocolModule) Name() string     { return "univ3" }
func (m *pancakeProtocolModule) Name() string   { return "pancakev3" }
func (m *quickSwapProtocolModule) Name() string { return "quickswapv3" }
func (m *univ4ProtocolModule) Name() string     { return "univ4" }
func (m *balancerProtocolModule) Name() string  { return "balancer" }

func (m *univ3ProtocolModule) Bootstrapper() protocolBootstrapper     { return m.services.Lifecycle }
func (m *pancakeProtocolModule) Bootstrapper() protocolBootstrapper   { return m.services.Lifecycle }
func (m *quickSwapProtocolModule) Bootstrapper() protocolBootstrapper { return m.services.Lifecycle }
func (m *univ4ProtocolModule) Bootstrapper() protocolBootstrapper     { return m.services.Lifecycle }
func (m *balancerProtocolModule) Bootstrapper() protocolBootstrapper  { return m.services.Lifecycle }

func (m *univ3ProtocolModule) BlockPreparer() synccontract.BlockPreparer {
	return m.services.Lifecycle.BlockPreparer()
}
func (m *pancakeProtocolModule) BlockPreparer() synccontract.BlockPreparer {
	return m.services.Lifecycle.BlockPreparer()
}
func (m *quickSwapProtocolModule) BlockPreparer() synccontract.BlockPreparer {
	return m.services.Lifecycle.BlockPreparer()
}
func (m *univ4ProtocolModule) BlockPreparer() synccontract.BlockPreparer {
	return m.services.Lifecycle.BlockPreparer()
}
func (m *balancerProtocolModule) BlockPreparer() synccontract.BlockPreparer {
	return m.services.Lifecycle.BlockPreparer()
}

func (m *univ3ProtocolModule) BindMarketReports(receiver marketpipeline.ReportReceiver, logger *zap.Logger) {
	m.services.SetListener(&marketpipeline.Univ3PoolListener{Receiver: receiver})
	m.services.SetLogger(logger.Named("sync.clv3"))
}
func (m *pancakeProtocolModule) BindMarketReports(receiver marketpipeline.ReportReceiver, logger *zap.Logger) {
	m.services.SetListener(&marketpipeline.PancakeV3PoolListener{Receiver: receiver})
	m.services.SetLogger(logger.Named("sync.pancakev3"))
}
func (m *quickSwapProtocolModule) BindMarketReports(receiver marketpipeline.ReportReceiver, logger *zap.Logger) {
	m.services.SetListener(&marketpipeline.QuickSwapV3PoolListener{Receiver: receiver})
	m.services.SetLogger(logger.Named("sync.quickswapv3"))
}
func (m *univ4ProtocolModule) BindMarketReports(receiver marketpipeline.ReportReceiver, logger *zap.Logger) {
	m.services.SetListener(&marketpipeline.Univ4PoolListener{Receiver: receiver})
	m.services.SetLogger(logger.Named("sync.univ4"))
}
func (m *balancerProtocolModule) BindMarketReports(receiver marketpipeline.ReportReceiver, logger *zap.Logger) {
	m.services.SetListener(&marketpipeline.BalancerPoolListener{Receiver: receiver})
	m.services.SetLogger(logger.Named("sync.balancer"))
}

func (m *univ3ProtocolModule) StartDiscovery(r *syncLifecycle, cfg config.ChainConfig) {
	if m.resources != nil {
		runSubgraphDiscovery(r, m.Name(), cfg.Sync.Univ3.Subgraph.RefreshInterval, cfg.Sync.Univ3.Subgraph.IsEnabled(), m.resources.registry, m.services.Lifecycle)
	}
}
func (m *pancakeProtocolModule) StartDiscovery(r *syncLifecycle, cfg config.ChainConfig) {
	if m.resources != nil {
		runSubgraphDiscovery(r, m.Name(), cfg.Sync.PancakeV3.Subgraph.RefreshInterval, cfg.Sync.PancakeV3.Subgraph.IsEnabled(), m.resources.registry, m.services.Lifecycle)
	}
}
func (m *quickSwapProtocolModule) StartDiscovery(r *syncLifecycle, cfg config.ChainConfig) {
	if m.resources != nil {
		runSubgraphDiscovery(r, m.Name(), cfg.Sync.QuickSwapV3.Subgraph.RefreshInterval, cfg.Sync.QuickSwapV3.Subgraph.IsEnabled(), m.resources.registry, m.services.Lifecycle)
	}
}
func (m *univ4ProtocolModule) StartDiscovery(r *syncLifecycle, cfg config.ChainConfig) {
	if m.resources != nil {
		runSubgraphDiscovery(r, m.Name(), cfg.Sync.Univ4.Subgraph.RefreshInterval, cfg.Sync.Univ4.Subgraph.IsEnabled(), m.resources.registry, m.services.Lifecycle)
	}
}
func (m *balancerProtocolModule) StartDiscovery(r *syncLifecycle, cfg config.ChainConfig) {
	if m.resources != nil {
		runSubgraphDiscovery(r, m.Name(), cfg.Sync.Balancer.Subgraph.RefreshInterval, cfg.Sync.Balancer.Subgraph.IsEnabled(), m.resources.registry, m.services.Lifecycle)
	}
}

func (s *protocolServices) univ3Services() *syncv3.Services {
	for _, module := range s.modules {
		if typed, ok := module.(*univ3ProtocolModule); ok {
			return typed.services
		}
	}
	return nil
}

func (s *protocolServices) pancakeServices() *syncpancakev3.Services {
	for _, module := range s.modules {
		if typed, ok := module.(*pancakeProtocolModule); ok {
			return typed.services
		}
	}
	return nil
}

func (s *protocolServices) quickSwapServices() *syncquickswapv3.Services {
	for _, module := range s.modules {
		if typed, ok := module.(*quickSwapProtocolModule); ok {
			return typed.services
		}
	}
	return nil
}

func (s *protocolServices) univ4Services() *syncv4.Services {
	for _, module := range s.modules {
		if typed, ok := module.(*univ4ProtocolModule); ok {
			return typed.services
		}
	}
	return nil
}

func (s *protocolServices) balancerServices() *syncbalancer.Services {
	for _, module := range s.modules {
		if typed, ok := module.(*balancerProtocolModule); ok {
			return typed.services
		}
	}
	return nil
}

func newProtocolServices(
	cfg config.ChainConfig,
	store *persistence.Services,
	chain *chaininfra.Services,
	resources protocolResources,
) (*protocolServices, error) {
	protocols := &protocolServices{modules: make([]protocolModule, 0, 5)}
	univ3Resources := resources.univ3()
	univ3 := newUniv3Protocol(cfg, store, chain, univ3Resources)
	if univ3 != nil {
		protocols.modules = append(protocols.modules, &univ3ProtocolModule{services: univ3, resources: univ3Resources})
	}
	pancakeResources := resources.pancakeV3()
	pancake, err := newPancakeProtocol(cfg, store, chain, pancakeResources)
	if err != nil {
		return nil, err
	}
	if pancake != nil {
		protocols.modules = append(protocols.modules, &pancakeProtocolModule{services: pancake, resources: pancakeResources})
	}
	quickSwapResources := resources.quickSwapV3()
	quickSwap, err := newQuickSwapProtocol(cfg, store, chain, quickSwapResources)
	if err != nil {
		return nil, err
	}
	if quickSwap != nil {
		protocols.modules = append(protocols.modules, &quickSwapProtocolModule{services: quickSwap, resources: quickSwapResources})
	}
	univ4Resources := resources.univ4()
	univ4, err := newUniv4Protocol(cfg, store, chain, univ4Resources)
	if err != nil {
		return nil, err
	}
	if univ4 != nil {
		protocols.modules = append(protocols.modules, &univ4ProtocolModule{services: univ4, resources: univ4Resources})
	}
	balancerResources := resources.balancer()
	balancer, err := newBalancerProtocol(cfg, store, chain, balancerResources)
	if err != nil {
		return nil, err
	}
	if balancer != nil {
		protocols.modules = append(protocols.modules, &balancerProtocolModule{services: balancer, resources: balancerResources})
	}
	return protocols, nil
}

func newUniv3Protocol(
	cfg config.ChainConfig,
	store *persistence.Services,
	chain *chaininfra.Services,
	resources *univ3Resources,
) *syncv3.Services {
	if !cfg.Sync.Univ3.IsActive() {
		return nil
	}
	if resources == nil {
		return nil
	}
	adapters := resources.blockchain
	deps := syncv3.ServiceDeps{
		Config:      cfg.SyncConfig(),
		Pools:       store.Pools,
		Snapshots:   store.Snapshots,
		Checkpoints: store.Checkpoints,
		Registry:    resources.registry,
		Fetcher:     adapters.LogFetcher,
		Parser:      adapters.Parser,
		Blocks:      chain.Client,
		Bootstrap:   adapters.PoolReader,
		Listener:    syncv3.NopChangedPoolsListener{},
	}
	return syncv3.NewServices(deps)
}

func newPancakeProtocol(
	cfg config.ChainConfig,
	store *persistence.Services,
	chain *chaininfra.Services,
	resources *pancakeV3Resources,
) (*syncpancakev3.Services, error) {
	if !cfg.Sync.PancakeV3.IsActive() {
		return nil, nil
	}
	if resources == nil {
		return nil, fmt.Errorf("pancake resources are not configured")
	}
	adapters := resources.blockchain
	deps := syncpancakev3.ServiceDeps{
		Config:      cfg.SyncConfig(),
		Pools:       store.PancakePools,
		Snapshots:   store.PancakeSnapshots,
		Checkpoints: store.PancakeCheckpoints,
		Registry:    resources.registry,
		Fetcher:     adapters.LogFetcher,
		Parser:      adapters.Parser,
		Blocks:      chain.Client,
		Bootstrap:   adapters.PoolReader,
		Listener:    syncpancakev3.NopChangedPoolsListener{},
	}
	return syncpancakev3.NewServices(deps), nil
}

func newQuickSwapProtocol(
	cfg config.ChainConfig,
	store *persistence.Services,
	chain *chaininfra.Services,
	resources *quickSwapV3Resources,
) (*syncquickswapv3.Services, error) {
	if !cfg.Sync.QuickSwapV3.IsActive() {
		return nil, nil
	}
	if resources == nil {
		return nil, fmt.Errorf("quickswap resources are not configured")
	}
	adapters := resources.blockchain
	deps := syncquickswapv3.ServiceDeps{
		Config:      cfg.SyncConfig(),
		Pools:       store.QuickSwapPools,
		Snapshots:   store.QuickSwapSnapshots,
		Checkpoints: store.QuickSwapCheckpoints,
		Registry:    resources.registry,
		Fetcher:     adapters.LogFetcher,
		Parser:      adapters.Parser,
		Blocks:      chain.Client,
		Bootstrap:   adapters.PoolReader,
		Listener:    syncquickswapv3.NopChangedPoolsListener{},
	}
	return syncquickswapv3.NewServices(deps), nil
}

func newUniv4Protocol(
	cfg config.ChainConfig,
	store *persistence.Services,
	chain *chaininfra.Services,
	resources *univ4Resources,
) (*syncv4.Services, error) {
	if !cfg.Sync.Univ4.IsActive() {
		return nil, nil
	}
	if resources == nil {
		return nil, fmt.Errorf("univ4 resources are not configured")
	}
	adapters := resources.blockchain
	deps := syncv4.ServiceDeps{
		Config:      cfg.SyncConfig(),
		Pools:       store.V4Pools,
		Snapshots:   store.V4Snapshots,
		Checkpoints: store.V4Checkpoints,
		Registry:    resources.registry,
		Fetcher:     adapters.LogFetcher,
		Parser:      adapters.Parser,
		Blocks:      chain.Client,
		Bootstrap:   adapters.PoolReader,
		Listener:    syncv4.NopChangedPoolsListener{},
	}
	return syncv4.NewServices(deps), nil
}

func newBalancerProtocol(
	cfg config.ChainConfig,
	store *persistence.Services,
	chain *chaininfra.Services,
	resources *balancerResources,
) (*syncbalancer.Services, error) {
	if !cfg.Sync.Balancer.IsActive() {
		return nil, nil
	}
	if resources == nil {
		return nil, fmt.Errorf("balancer resources are not configured")
	}
	adapters := resources.blockchain
	deps := syncbalancer.ServiceDeps{
		Config:      cfg.SyncConfig(),
		Pools:       store.BalancerPools,
		Snapshots:   store.BalancerSnapshots,
		Checkpoints: store.BalancerCheckpoints,
		Registry:    resources.registry,
		Fetcher:     adapters.LogFetcher,
		Parser:      adapters.Parser,
		Blocks:      chain.Client,
		Bootstrap:   adapters.PoolReader,
		Listener:    syncbalancer.NopChangedPoolsListener{},
	}
	return syncbalancer.NewServices(deps), nil
}

func newMarketStore(store *persistence.Services, protocols *protocolServices, logger *zap.Logger) *marketstore.Store {
	var univ3Active marketstore.Registry[common.Address]
	var pancakeActive marketstore.Registry[common.Address]
	var quickSwapActive marketstore.Registry[common.Address]
	var univ4Active marketstore.Registry[marketv4.PoolID]
	var balancerActive marketstore.Registry[marketbalancer.PoolID]
	if services := protocols.univ3Services(); services != nil {
		univ3Active = services.Lifecycle
	}
	if services := protocols.pancakeServices(); services != nil {
		pancakeActive = services.Lifecycle
	}
	if services := protocols.quickSwapServices(); services != nil {
		quickSwapActive = services.Lifecycle
	}
	if services := protocols.univ4Services(); services != nil {
		univ4Active = services.Lifecycle
	}
	if services := protocols.balancerServices(); services != nil {
		balancerActive = services.Lifecycle
	}
	view := marketstore.NewStore(marketstore.Sources{
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
	view.SetLogger(logger.Named("market-view"))
	return view
}

func newArbitrageServices(
	cfg config.ChainConfig,
	logger *zap.Logger,
	durableStore *persistence.Services,
	chain *chaininfra.Services,
	marketView *marketstore.Store,
	contractExecutor *contractapp.AppService,
) *arbitrageapp.Services {
	triangleCfg := cfg.Arbitrage.Triangle
	spreadCfg := cfg.Arbitrage.Spread
	triangleEnabled := cfg.TriangleArbitrageEnabled()
	spreadEnabled := cfg.SpreadArbitrageEnabled()
	configuredStartTokens := triangleCfg.StartTokenAddresses()
	spreadStartTokens := spreadCfg.StartTokenAddresses()
	minNetProfit := triangleCfg.MinNetProfit()
	spreadMinNetProfit := spreadCfg.MinNetProfit()
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
	executionCfg := executionConfigFromRuntime(cfg)
	livePlan := livePlanConfigFromRuntime(cfg)
	coinbasePaymentBPS := uint16(0)
	if strings.TrimSpace(executionCfg.FlashbotsRPCURL) != "" && executionCfg.FlashbotsPaymentBPS > 0 {
		coinbasePaymentBPS = uint16(executionCfg.FlashbotsPaymentBPS)
	}
	publishing := arbitrageapp.PublishingDeps{Repository: durableStore.Opportunities}
	if executionCfg.Enabled {
		encoder := arbitrageapp.NewLiveCalldataEncoder(
			livePlan,
			arbitrageapp.NewCommittedMarketRoutePoolLoader(marketView),
		)
		builder := arbitrageapp.NewLiveExecutionPlanBuilder(livePlan, encoder)
		publishing.Publishers = append(publishing.Publishers,
			arbitrageapp.NewExecutionPublisher(
				executionCfg,
				builder,
				contractExecutor,
				durableStore.Opportunities,
				chain.Client,
				logger,
			),
		)
		publishing.PoolGraphUpdaters = append(publishing.PoolGraphUpdaters, builder)
	}

	return arbitrageapp.NewServices(arbitrageapp.ServiceDeps{
		Logger: logger,
		Quotes: quoteunified.NewQuoteService(
			quoteuniv3domain.NewQuoteService(),
			quotepancakev3domain.NewQuoteService(),
			quoteuniv4domain.NewQuoteService(),
			quotebalancerdomain.NewQuoteService(),
		),
		Market: marketView,
		Routing: arbitrageapp.RoutingConfig{
			TriangleEnabled:       triangleEnabled,
			SpreadEnabled:         spreadEnabled,
			ConfiguredStartTokens: configuredStartTokens,
			SpreadStartTokens:     spreadStartTokens,
			MinNetProfitWei:       minNetProfit,
			SpreadMinNetProfitWei: spreadMinNetProfit,
		},
		Opportunity: arbitrageapp.OpportunityConfig{
			MinAmount:           optimizerMinAmount,
			MaxAmount:           optimizerMaxAmount,
			OptimizerIterations: optimizerIterations,
			WrappedNative:       livePlan.WETH,
			CoinbasePaymentBPS:  coinbasePaymentBPS,
			FlashLoanOptions: []domainarb.FlashLoanOption{
				{Protocol: domainarb.FlashLoanProtocolBalancer, FeePPM: cfg.Arbitrage.FlashLoan.BalancerFee()},
				{Protocol: domainarb.FlashLoanProtocolUniv4, FeePPM: cfg.Arbitrage.FlashLoan.Univ4Fee()},
			},
		},
		Publishing: publishing,
	})
}

func enabledMarketProtocols(cfg config.ChainConfig) []marketpipeline.Protocol {
	protocols := make([]marketpipeline.Protocol, 0, 5)
	if cfg.Sync.Univ3.IsActive() {
		protocols = append(protocols, marketpipeline.ProtocolUniv3)
	}
	if cfg.Sync.PancakeV3.IsActive() {
		protocols = append(protocols, marketpipeline.ProtocolPancakeV3)
	}
	if cfg.Sync.QuickSwapV3.IsActive() {
		protocols = append(protocols, marketpipeline.ProtocolQuickSwapV3)
	}
	if cfg.Sync.Univ4.IsActive() {
		protocols = append(protocols, marketpipeline.ProtocolUniv4)
	}
	if cfg.Sync.Balancer.IsActive() {
		protocols = append(protocols, marketpipeline.ProtocolBalancer)
	}
	return protocols
}

func executionConfigFromRuntime(cfg config.ChainConfig) arbitrageapp.ExecutionConfig {
	execution := cfg.Arbitrage.Execution
	return arbitrageapp.ExecutionConfig{
		Enabled:               execution.Enabled,
		RPCURL:                execution.ResolvedRPCURL(),
		PrivateKey:            execution.PrivateKey,
		Executor:              execution.Executor(),
		FlashbotsRPCURL:       execution.FlashbotsRPCURL,
		FlashbotsPaymentBPS:   execution.FlashbotsPaymentBPS,
		SettlementSlippageBPS: execution.SettlementSlippageBPS,
		WrappedNativeToken:    execution.WETH(),
		GasLimit:              execution.GasLimit,
		GasPriceWei:           execution.GasPrice(),
		SkipEstimate:          execution.SkipEstimate,
		BroadcastToken:        execution.BroadcastToken,
		MaxOpportunityAge:     maxOpportunityAge(execution.MaxOpportunityAge),
		AllowedRouters:        execution.AllowedRouterAddresses(),
		AllowedSpenders:       execution.AllowedSpenderAddresses(),
	}
}

func livePlanConfigFromRuntime(cfg config.ChainConfig) arbitrageapp.LivePlanConfig {
	balancerCfg := cfg.BalancerBlockchainConfig()
	univ4Cfg := cfg.Univ4BlockchainConfig()
	execution := cfg.Arbitrage.Execution
	return arbitrageapp.LivePlanConfig{
		RequireWETHProfit:     false,
		CoinbasePaymentBPS:    0,
		SettlementSlippageBPS: 0,
		WETH:                  execution.WETH(),
		BalancerVault:         balancerCfg.VaultAddress,
		BalancerVaultV3:       balancerCfg.VaultV3Address,
		BalancerRouterV3:      execution.BalancerRouterV3Address(),
		PoolManager:           univ4Cfg.PoolManagerAddress,
		SwapRouterV3:          execution.SwapRouterV3Address(),
		SwapRouterPancakeV3:   execution.SwapRouterPancakeV3Address(),
		UniversalRouter:       execution.UniversalRouterAddress(),
		Executor:              execution.Executor(),
	}
}

func maxOpportunityAge(configured uint64) uint64 {
	if configured == 0 {
		return 3
	}
	return configured
}

func (s *protocolServices) bindMarketReports(receiver marketpipeline.ReportReceiver, logger *zap.Logger) {
	for _, module := range s.modules {
		module.BindMarketReports(receiver, logger)
	}
}
