package runtime

import (
	"fmt"
	"strings"

	arbitrageapp "github.com/brianliu-sysu/uniswapv3/internal/application/arbitrage"
	assetapp "github.com/brianliu-sysu/uniswapv3/internal/application/asset"
	contractapp "github.com/brianliu-sysu/uniswapv3/internal/application/contract"
	poolsapp "github.com/brianliu-sysu/uniswapv3/internal/application/pools"
	quotecombined "github.com/brianliu-sysu/uniswapv3/internal/application/quote/combined"
	quotepancakev3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/pancakev3"
	quotequickswapv3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/quickswapv3"
	quoteuniv3 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/univ3"
	quoteuniv4 "github.com/brianliu-sysu/uniswapv3/internal/application/quote/univ4"
	domainarb "github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
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
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/registry"
	httpapi "github.com/brianliu-sysu/uniswapv3/internal/interfaces/http"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func newQuoteV3AppService(
	runtime *chainRuntime,
) *quoteuniv3.AppService {
	cfg := runtime.cfg
	services := runtime.protocols.univ3Services()
	if !cfg.Sync.Univ3.IsActive() || services == nil {
		return nil
	}
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}
	return quoteuniv3.NewAppService(
		runtime.MarketStore.Univ3Repository(),
		runtime.MarketStore.Univ3Registry(),
		quoteuniv3domain.NewQuoteService(),
		runtime.MarketStore.Univ3Readiness(),
		maxHops,
	)
}

func newQuotePancakeV3AppService(
	runtime *chainRuntime,
) *quotepancakev3.AppService {
	cfg := runtime.cfg
	services := runtime.protocols.pancakeServices()
	if !cfg.Sync.PancakeV3.IsActive() || services == nil || runtime.resources.protocols.pancakeV3 == nil {
		return nil
	}
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}
	return quotepancakev3.NewAppService(
		runtime.MarketStore.PancakeRepository(),
		runtime.MarketStore.PancakeRegistry(),
		quotepancakev3domain.NewQuoteService(),
		runtime.MarketStore.PancakeReadiness(),
		maxHops,
	)
}

func newQuoteQuickSwapV3AppService(
	runtime *chainRuntime,
) *quotequickswapv3.AppService {
	cfg := runtime.cfg
	services := runtime.protocols.quickSwapServices()
	if !cfg.Sync.QuickSwapV3.IsActive() || services == nil || runtime.resources.protocols.quickSwapV3 == nil {
		return nil
	}
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}
	return quotequickswapv3.NewAppService(
		runtime.MarketStore.QuickSwapRepository(),
		runtime.MarketStore.QuickSwapRegistry(),
		quotequickswapv3domain.NewQuoteService(),
		runtime.MarketStore.QuickSwapReadiness(),
		maxHops,
	)
}

func newQuoteV4AppService(
	runtime *chainRuntime,
) *quoteuniv4.AppService {
	cfg := runtime.cfg
	services := runtime.protocols.univ4Services()
	if services == nil || runtime.resources.protocols.univ4 == nil {
		return nil
	}
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}
	return quoteuniv4.NewAppService(
		runtime.MarketStore.Univ4Repository(),
		runtime.MarketStore.Univ4Registry(),
		quoteuniv4domain.NewQuoteService(),
		runtime.MarketStore.Univ4Readiness(),
		maxHops,
	)
}

func newQuoteCombinedAppService(
	runtime *chainRuntime,
) *quotecombined.AppService {
	cfg := runtime.cfg
	maxHops := cfg.Quote.MaxHops
	if maxHops <= 0 {
		maxHops = 3
	}
	protocols := []quotecombined.ProtocolAdapter{
		quotecombined.NewUniv3ProtocolAdapter(runtime.MarketStore.Univ3Repository(), runtime.MarketStore.Univ3Registry(), runtime.MarketStore.Univ3Readiness()),
		quotecombined.NewPancakeV3ProtocolAdapter(runtime.MarketStore.PancakeRepository(), runtime.MarketStore.PancakeRegistry(), runtime.MarketStore.PancakeReadiness()),
		quotecombined.NewQuickSwapV3ProtocolAdapter(runtime.MarketStore.QuickSwapRepository(), runtime.MarketStore.QuickSwapRegistry(), runtime.MarketStore.QuickSwapReadiness()),
		quotecombined.NewUniv4ProtocolAdapter(runtime.MarketStore.Univ4Repository(), runtime.MarketStore.Univ4Registry(), runtime.MarketStore.Univ4Readiness()),
		quotecombined.NewBalancerProtocolAdapter(
			runtime.MarketStore.BalancerRepository(),
			runtime.MarketStore.BalancerRegistry(),
			runtime.MarketStore.BalancerReadiness(),
		),
	}
	return quotecombined.NewAppService(
		protocols,
		quoteunified.NewQuoteService(
			quoteuniv3domain.NewQuoteService(),
			quotepancakev3domain.NewQuoteService(),
			quoteuniv4domain.NewQuoteService(),
			quotebalancerdomain.NewQuoteService(),
		),
		runtime.MarketStore,
		maxHops,
	)
}

func newPoolsAppService(
	store *persistence.Services,
	chain *chaininfra.Services,
	resources protocolResources,
) *poolsapp.AppService {
	var poolRegistry *registry.CompositeRegistry
	var pancakePoolRegistry *registry.PancakeCompositeRegistry
	var v4PoolRegistry *registry.CompositeV4Registry
	var balancerPoolRegistry *registry.CompositeBalancerRegistry
	if resources.univ3 != nil {
		poolRegistry = resources.univ3.registry
	}
	if resources.pancakeV3 != nil {
		pancakePoolRegistry = resources.pancakeV3.registry
	}
	if resources.univ4 != nil {
		v4PoolRegistry = resources.univ4.registry
	}
	if resources.balancer != nil {
		balancerPoolRegistry = resources.balancer.registry
	}
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
	var v3Reader *chaininfra.PoolReader
	var pancakeReader *chaininfra.PoolReader
	var v4Reader *chaininfra.V4PoolReader
	var balancerReader *chaininfra.BalancerPoolReader
	if resources.univ3 != nil {
		v3Reader = resources.univ3.blockchain.PoolReader
	}
	if resources.pancakeV3 != nil {
		pancakeReader = resources.pancakeV3.blockchain.PoolReader
	}
	if resources.univ4 != nil {
		v4Reader = resources.univ4.blockchain.PoolReader
	}
	if resources.balancer != nil {
		balancerReader = resources.balancer.blockchain.PoolReader
	}
	headReader := chaininfra.NewPoolsHeadReader(chain.Client)
	univ3Reader := chaininfra.NewCLV3ChainReader(v3Reader)
	pancakeV3Reader := chaininfra.NewCLV3ChainReader(pancakeReader)
	univ4Reader := chaininfra.NewV4ChainReader(v4Reader)
	balancerStateReader := chaininfra.NewBalancerChainReader(balancerReader)
	return poolsapp.NewAppService(poolsapp.ServiceDeps{
		Protocols: []poolsapp.ProtocolAdapter{
			poolsapp.NewUniv3Adapter(store.Pools, poolRegistry, univ3Reader),
			poolsapp.NewPancakeV3Adapter(store.PancakePools, pancakeRegistry, pancakeV3Reader),
			poolsapp.NewUniv4Adapter(store.V4Pools, v4Registry, univ4Reader),
			poolsapp.NewBalancerAdapter(store.BalancerPools, balancerRegistry, balancerStateReader),
		},
		Tokens: tokenService,
		Head:   headReader,
	})
}

func newHTTPRouter(runtimes *runtimeSet, logger *zap.Logger) *gin.Engine {
	chains, services := newHTTPChainServices(runtimes, logger)
	return httpapi.NewRouter(httpapi.Handlers{
		Health:           httpapi.NewHealthHandler(),
		QuoteCombined:    httpapi.NewQuoteCombinedChainHandler(chains, services.quoteCombined),
		QuoteV3:          httpapi.NewQuoteV3ChainHandler(chains, services.quoteV3),
		QuotePancakeV3:   httpapi.NewQuotePancakeV3ChainHandler(chains, services.quotePancakeV3),
		QuoteQuickSwapV3: httpapi.NewQuoteQuickSwapV3ChainHandler(chains, services.quoteQuickSwapV3),
		QuoteV4:          httpapi.NewQuoteV4ChainHandler(chains, services.quoteV4),
		Opportunities:    httpapi.NewOpportunityChainHandler(chains, services.opportunities, services.opportunityExecutors),
		Pools:            httpapi.NewPoolsChainHandler(chains, services.pools),
		ContractExecutor: httpapi.NewContractExecutorChainHandler(chains, services.contractExecutors),
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
	contractExecutors    map[string]*contractapp.AppService
	pools                map[string]*poolsapp.AppService
}

func newHTTPChainServices(runtimes *runtimeSet, logger *zap.Logger) ([]httpapi.ChainInfo, httpChainServices) {
	services := httpChainServices{
		quoteCombined:        make(map[string]*quotecombined.AppService),
		quoteV3:              make(map[string]*quoteuniv3.AppService),
		quotePancakeV3:       make(map[string]*quotepancakev3.AppService),
		quoteQuickSwapV3:     make(map[string]*quotequickswapv3.AppService),
		quoteV4:              make(map[string]*quoteuniv4.AppService),
		opportunities:        make(map[string]domainarb.OpportunityRepository),
		opportunityExecutors: make(map[string]*arbitrageapp.OpportunityExecutor),
		contractExecutors:    make(map[string]*contractapp.AppService),
		pools:                make(map[string]*poolsapp.AppService),
	}
	if runtimes == nil {
		return nil, services
	}

	chains := make([]httpapi.ChainInfo, 0, len(runtimes.chains))
	for i, runtime := range runtimes.chains {
		if runtime == nil {
			continue
		}
		chainName := runtime.cfg.Name
		key := httpChainKey(chainName)
		if key == "" {
			key = httpChainKey(fmt.Sprintf("chain-%d", runtime.cfg.ChainID))
		}
		chains = append(chains, httpapi.ChainInfo{
			Name:    chainName,
			ChainID: runtime.cfg.ChainID,
			Primary: i == 0,
		})
		services.quoteCombined[key] = newQuoteCombinedAppService(runtime)
		services.quoteV3[key] = newQuoteV3AppService(runtime)
		services.quotePancakeV3[key] = newQuotePancakeV3AppService(runtime)
		services.quoteQuickSwapV3[key] = newQuoteQuickSwapV3AppService(runtime)
		services.quoteV4[key] = newQuoteV4AppService(runtime)
		services.opportunities[key] = runtime.resources.stores.durable.Opportunities
		services.opportunityExecutors[key] = newOpportunityExecutor(runtime, logger)
		services.contractExecutors[key] = runtime.resources.contractExecutor
		services.pools[key] = newPoolsAppService(
			runtime.resources.stores.durable,
			runtime.resources.blockchain,
			runtime.resources.protocols,
		)
	}
	return chains, services
}

func newOpportunityExecutor(
	runtime *chainRuntime,
	logger *zap.Logger,
) *arbitrageapp.OpportunityExecutor {
	if runtime == nil || runtime.resources == nil {
		return nil
	}
	cfg := runtime.cfg
	store := runtime.resources.stores.durable
	chain := runtime.resources.blockchain
	contractExecutor := runtime.resources.contractExecutor
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
	if runtime.Arbitrage != nil {
		runtime.Arbitrage.RegisterPoolGraphUpdater(builder)
	}
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
