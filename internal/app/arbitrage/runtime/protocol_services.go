package runtime

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	arbitrageapp "github.com/brianliu-sysu/uniswapv3/internal/application/arbitrage"
	contractapp "github.com/brianliu-sysu/uniswapv3/internal/application/contract"
	"github.com/brianliu-sysu/uniswapv3/internal/application/marketstore"
	quotecombined "github.com/brianliu-sysu/uniswapv3/internal/application/quote/combined"
	syncbalancer "github.com/brianliu-sysu/uniswapv3/internal/application/sync/balancer"
	syncpancakev3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/pancakev3"
	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
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
	HeadHandler() syncapp.BlockHandler
	IsReady() bool
	BindArbitrage(*arbitrageapp.Services, *zap.Logger)
	StartDiscovery(*syncLifecycle, config.ChainConfig, protocolResources)
}

type univ3ProtocolModule struct{ services *syncv3.Services }
type pancakeProtocolModule struct{ services *syncpancakev3.Services }
type quickSwapProtocolModule struct{ services *syncquickswapv3.Services }
type univ4ProtocolModule struct{ services *syncv4.Services }
type balancerProtocolModule struct{ services *syncbalancer.Services }

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

func (m *univ3ProtocolModule) HeadHandler() syncapp.BlockHandler {
	return m.services.Lifecycle.BlockHandler
}
func (m *pancakeProtocolModule) HeadHandler() syncapp.BlockHandler {
	return m.services.Lifecycle.BlockHandler
}
func (m *quickSwapProtocolModule) HeadHandler() syncapp.BlockHandler {
	return m.services.Lifecycle.BlockHandler
}
func (m *univ4ProtocolModule) HeadHandler() syncapp.BlockHandler {
	return m.services.Lifecycle.BlockHandler
}
func (m *balancerProtocolModule) HeadHandler() syncapp.BlockHandler {
	return m.services.Lifecycle.BlockHandler
}

func (m *univ3ProtocolModule) IsReady() bool {
	return m.services.Lifecycle.Readiness != nil && m.services.Lifecycle.Readiness.IsSystemReady()
}
func (m *pancakeProtocolModule) IsReady() bool {
	return m.services.Lifecycle.Readiness != nil && m.services.Lifecycle.Readiness.IsSystemReady()
}
func (m *quickSwapProtocolModule) IsReady() bool {
	return m.services.Lifecycle.Readiness != nil && m.services.Lifecycle.Readiness.IsSystemReady()
}
func (m *univ4ProtocolModule) IsReady() bool {
	return m.services.Lifecycle.Readiness != nil && m.services.Lifecycle.Readiness.IsSystemReady()
}
func (m *balancerProtocolModule) IsReady() bool {
	return m.services.Lifecycle.Readiness != nil && m.services.Lifecycle.Readiness.IsSystemReady()
}

func (m *univ3ProtocolModule) BindArbitrage(services *arbitrageapp.Services, logger *zap.Logger) {
	m.services.SetListener(services)
	m.services.SetLogger(logger.Named("sync.clv3"))
}
func (m *pancakeProtocolModule) BindArbitrage(services *arbitrageapp.Services, logger *zap.Logger) {
	m.services.SetListener(arbitrageapp.PancakePoolListener{Services: services})
	m.services.SetLogger(logger.Named("sync.pancakev3"))
}
func (m *quickSwapProtocolModule) BindArbitrage(services *arbitrageapp.Services, logger *zap.Logger) {
	m.services.SetListener(arbitrageapp.QuickSwapPoolListener{Services: services})
	m.services.SetLogger(logger.Named("sync.quickswapv3"))
}
func (m *univ4ProtocolModule) BindArbitrage(services *arbitrageapp.Services, logger *zap.Logger) {
	m.services.SetListener(arbitrageapp.V4PoolListener{Services: services})
	m.services.SetLogger(logger.Named("sync.univ4"))
}
func (m *balancerProtocolModule) BindArbitrage(services *arbitrageapp.Services, logger *zap.Logger) {
	m.services.SetListener(arbitrageapp.BalancerPoolListener{Services: services})
	m.services.SetLogger(logger.Named("sync.balancer"))
}

func (m *univ3ProtocolModule) StartDiscovery(r *syncLifecycle, cfg config.ChainConfig, resources protocolResources) {
	if resources.univ3 != nil {
		runSubgraphDiscovery(r, m.Name(), cfg.Sync.Univ3.Subgraph.RefreshInterval, cfg.Sync.Univ3.Subgraph.IsEnabled(), resources.univ3.registry, m.services.Lifecycle.Pools, m.services.Lifecycle)
	}
}
func (m *pancakeProtocolModule) StartDiscovery(r *syncLifecycle, cfg config.ChainConfig, resources protocolResources) {
	if resources.pancakeV3 != nil {
		runSubgraphDiscovery(r, m.Name(), cfg.Sync.PancakeV3.Subgraph.RefreshInterval, cfg.Sync.PancakeV3.Subgraph.IsEnabled(), resources.pancakeV3.registry, m.services.Lifecycle.Pools, m.services.Lifecycle)
	}
}
func (m *quickSwapProtocolModule) StartDiscovery(r *syncLifecycle, cfg config.ChainConfig, resources protocolResources) {
	if resources.quickSwapV3 != nil {
		runSubgraphDiscovery(r, m.Name(), cfg.Sync.QuickSwapV3.Subgraph.RefreshInterval, cfg.Sync.QuickSwapV3.Subgraph.IsEnabled(), resources.quickSwapV3.registry, m.services.Lifecycle.Pools, m.services.Lifecycle)
	}
}
func (m *univ4ProtocolModule) StartDiscovery(r *syncLifecycle, cfg config.ChainConfig, resources protocolResources) {
	if resources.univ4 != nil {
		runSubgraphDiscovery(r, m.Name(), cfg.Sync.Univ4.Subgraph.RefreshInterval, cfg.Sync.Univ4.Subgraph.IsEnabled(), resources.univ4.registry, m.services.Lifecycle.Pools, m.services.Lifecycle)
	}
}
func (m *balancerProtocolModule) StartDiscovery(r *syncLifecycle, cfg config.ChainConfig, resources protocolResources) {
	if resources.balancer != nil {
		runSubgraphDiscovery(r, m.Name(), cfg.Sync.Balancer.Subgraph.RefreshInterval, cfg.Sync.Balancer.Subgraph.IsEnabled(), resources.balancer.registry, m.services.Lifecycle.Pools, m.services.Lifecycle)
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

func newProtocolServices(
	cfg config.ChainConfig,
	store *persistence.Services,
	chain *chaininfra.Services,
	resources protocolResources,
) (*protocolServices, error) {
	protocols := &protocolServices{modules: make([]protocolModule, 0, 5)}
	univ3 := newUniv3Protocol(cfg, store, chain, resources.univ3)
	if univ3 != nil {
		protocols.modules = append(protocols.modules, &univ3ProtocolModule{services: univ3})
	}
	pancake, err := newPancakeProtocol(cfg, store, chain, resources.pancakeV3)
	if err != nil {
		return nil, err
	}
	if pancake != nil {
		protocols.modules = append(protocols.modules, &pancakeProtocolModule{services: pancake})
	}
	quickSwap, err := newQuickSwapProtocol(cfg, store, chain, resources.quickSwapV3)
	if err != nil {
		return nil, err
	}
	if quickSwap != nil {
		protocols.modules = append(protocols.modules, &quickSwapProtocolModule{services: quickSwap})
	}
	univ4, err := newUniv4Protocol(cfg, store, chain, resources.univ4)
	if err != nil {
		return nil, err
	}
	if univ4 != nil {
		protocols.modules = append(protocols.modules, &univ4ProtocolModule{services: univ4})
	}
	balancer, err := newBalancerProtocol(cfg, store, chain, resources.balancer)
	if err != nil {
		return nil, err
	}
	if balancer != nil {
		protocols.modules = append(protocols.modules, &balancerProtocolModule{services: balancer})
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

func (s *protocolServices) readiness() *quotecombined.SyncReadiness {
	readiness := &quotecombined.SyncReadiness{}
	if services := s.univ3Services(); services != nil {
		readiness.V3 = services.Lifecycle.Readiness
	}
	if services := s.pancakeServices(); services != nil {
		readiness.Pancake = services.Lifecycle.Readiness
	}
	if services := s.quickSwapServices(); services != nil {
		readiness.QuickSwap = services.Lifecycle.Readiness
	}
	if services := s.univ4Services(); services != nil {
		readiness.V4 = services.Lifecycle.Readiness
	}
	if services := s.balancerServices(); services != nil {
		readiness.Balancer = services.Lifecycle.Readiness
	}
	return readiness
}

func newMarketStore(store *persistence.Services, protocols *protocolServices, logger *zap.Logger) *marketstore.Store {
	var univ3Active *syncapp.PoolLifecycleService[common.Address]
	var pancakeActive *syncapp.PoolLifecycleService[common.Address]
	var quickSwapActive *syncapp.PoolLifecycleService[common.Address]
	var univ4Active *syncapp.PoolLifecycleService[marketv4.PoolID]
	var balancerActive *syncapp.PoolLifecycleService[marketbalancer.PoolID]
	if services := protocols.univ3Services(); services != nil {
		univ3Active = services.Lifecycle.Pools
	}
	if services := protocols.pancakeServices(); services != nil {
		pancakeActive = services.Lifecycle.Pools
	}
	if services := protocols.quickSwapServices(); services != nil {
		quickSwapActive = services.Lifecycle.Pools
	}
	if services := protocols.univ4Services(); services != nil {
		univ4Active = services.Lifecycle.Pools
	}
	if services := protocols.balancerServices(); services != nil {
		balancerActive = services.Lifecycle.Pools
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
	runtimeStore *persistence.Services,
	durableStore *persistence.Services,
	chain *chaininfra.Services,
	resources protocolResources,
	protocols *protocolServices,
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
	deps := arbitrageapp.ServiceDeps{
		Logger:         logger,
		Pools:          runtimeStore.Pools,
		PancakePools:   runtimeStore.PancakePools,
		QuickSwapPools: runtimeStore.QuickSwapPools,
		V4Pools:        runtimeStore.V4Pools,
		BalancerPools:  runtimeStore.BalancerPools,
		Quotes: quoteunified.NewQuoteService(
			quoteuniv3domain.NewQuoteService(),
			quotepancakev3domain.NewQuoteService(),
			quoteuniv4domain.NewQuoteService(),
			quotebalancerdomain.NewQuoteService(),
		),
		Readiness:             protocols.readiness(),
		Repository:            durableStore.Opportunities,
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
		MarketStore:               marketView,
		MarketVersion:             marketView,
		OpportunityPools:          marketView.Univ3Repository(),
		OpportunityPancakePools:   marketView.PancakeRepository(),
		OpportunityQuickSwapPools: marketView.QuickSwapRepository(),
		OpportunityV4Pools:        marketView.Univ4Repository(),
		OpportunityBalancerPools:  marketView.BalancerRepository(),
	}
	if resources.univ3 != nil {
		deps.Registry = resources.univ3.registry
	}
	if resources.pancakeV3 != nil {
		deps.PancakeRegistry = resources.pancakeV3.registry.AsPoolRegistry()
	}
	if resources.quickSwapV3 != nil {
		deps.QuickSwapRegistry = resources.quickSwapV3.registry.AsPoolRegistry()
	}
	if resources.univ4 != nil {
		deps.V4Registry = resources.univ4.registry.AsPoolRegistry()
	}
	if resources.balancer != nil {
		deps.BalancerRegistry = resources.balancer.registry.AsPoolRegistry()
	}
	return arbitrageapp.NewServices(deps)
}

func enabledSyncProtocols(cfg config.ChainConfig) []arbitrageapp.SyncProtocol {
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

func (s *protocolServices) bindArbitrage(arbitrage *arbitrageapp.Services, logger *zap.Logger) {
	for _, module := range s.modules {
		module.BindArbitrage(arbitrage, logger)
	}
}
