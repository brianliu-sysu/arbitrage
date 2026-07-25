package runtime

import (
	"context"
	"fmt"
	"sync"

	arbitrageapp "github.com/brianliu-sysu/uniswapv3/internal/application/arbitrage"
	contractapp "github.com/brianliu-sysu/uniswapv3/internal/application/contract"
	"github.com/brianliu-sysu/uniswapv3/internal/application/headpipeline"
	"github.com/brianliu-sysu/uniswapv3/internal/application/marketpipeline"
	"github.com/brianliu-sysu/uniswapv3/internal/application/marketstore"
	"github.com/brianliu-sysu/uniswapv3/internal/config"
	chaininfra "github.com/brianliu-sysu/uniswapv3/internal/infrastructure/blockchain"
	"go.uber.org/zap"
)

type chainRuntime struct {
	cfg             config.ChainConfig
	resources       *chainResources
	protocols       *protocolServices
	Arbitrage       *arbitrageapp.Services
	MarketStore     *marketstore.Store
	headCoordinator *headpipeline.Coordinator
	persistenceWG   sync.WaitGroup
}

// chainResources owns the infrastructure and registries for exactly one chain.
type chainResources struct {
	stores            *chainStores
	blockchain        *chaininfra.Services
	protocols         protocolResources
	contractExecutor  *contractapp.AppService
	persistenceCtx    context.Context
	cancelPersistence context.CancelFunc
}

func (r *chainResources) Close() {
	if r == nil {
		return
	}
	if r.blockchain != nil {
		r.blockchain.Close()
	}
	if r.cancelPersistence != nil {
		r.cancelPersistence()
	}
	if r.stores != nil {
		r.stores.Close()
	}
}

type runtimeSet struct {
	chains []*chainRuntime
}

func newChainRuntime(
	cfg config.ChainConfig,
	logger *zap.Logger,
	resources *chainResources,
) (*chainRuntime, error) {
	if resources == nil {
		return nil, fmt.Errorf("chain resources are not configured")
	}
	protocols, err := newProtocolServices(cfg, resources.stores.runtime, resources.blockchain, resources.protocols)
	if err != nil {
		return nil, err
	}
	marketView := newMarketStore(resources.stores.runtime, protocols, logger)
	arbitrageServices := newArbitrageServices(
		cfg,
		logger,
		resources.stores.durable,
		resources.blockchain,
		marketView,
		resources.contractExecutor,
	)
	marketCoordinator := marketpipeline.NewCoordinator(enabledMarketProtocols(cfg), marketView, logger.Named("market-pipeline"))
	headCoordinator := headpipeline.NewCoordinator(marketCoordinator, arbitrageServices.NewScanScheduler())
	protocols.bindMarketReports(marketCoordinator, logger)

	if cfg.ArbitrageEnabled() {
		arbitrageServices.LogDiagnostics(context.Background(), logger, "startup")
	} else {
		logger.Info("arbitrage discovery disabled")
	}

	runtime := &chainRuntime{
		cfg:             cfg,
		resources:       resources,
		protocols:       protocols,
		Arbitrage:       arbitrageServices,
		MarketStore:     marketView,
		headCoordinator: headCoordinator,
	}
	configureMarketPublishObservers(runtime, logger)
	return runtime, nil
}

func newRuntimeSet(
	cfg config.Config,
	logger *zap.Logger,
) (*runtimeSet, error) {
	normalized := cfg.NormalizedChains()
	if len(normalized) > 1 && !cfg.MemoryMode() {
		return nil, fmt.Errorf("multi-chain runtime currently requires persistence.memory=true until postgres repositories are chain_id scoped")
	}

	set := &runtimeSet{chains: make([]*chainRuntime, 0, len(normalized))}
	persistenceCfg := cfg.PersistenceConfig()
	for _, chain := range normalized {
		resources, err := newChainResources(persistenceCfg, chain)
		if err != nil {
			set.Close()
			return nil, fmt.Errorf("chain %s (%d) resources: %w", chain.Name, chain.ChainID, err)
		}
		runtime, err := newChainRuntime(
			chain,
			logger.Named(chain.Name),
			resources,
		)
		if err != nil {
			resources.Close()
			set.Close()
			return nil, fmt.Errorf("chain %s (%d) runtime: %w", chain.Name, chain.ChainID, err)
		}
		set.chains = append(set.chains, runtime)
	}
	return set, nil
}

func (s *runtimeSet) Close() {
	if s == nil {
		return
	}
	for i := len(s.chains) - 1; i >= 0; i-- {
		chain := s.chains[i]
		if chain == nil {
			continue
		}
		chain.resources.Close()
	}
}
