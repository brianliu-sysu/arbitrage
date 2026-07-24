package runtime

import (
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	chaininfra "github.com/brianliu-sysu/uniswapv3/internal/infrastructure/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/registry"
	"github.com/ethereum/go-ethereum/common"
)

// protocolResources owns the complete infrastructure and registry state for
// every enabled protocol on one chain.
type protocolResources struct {
	headLogFetcher *chaininfra.HeadLogFetcher
	univ3          *univ3Resources
	pancakeV3      *pancakeV3Resources
	quickSwapV3    *quickSwapV3Resources
	univ4          *univ4Resources
	balancer       *balancerResources
}

type univ3Resources struct {
	blockchain *chaininfra.Univ3Services
	registry   *registry.CompositeRegistry
}

type pancakeV3Resources struct {
	blockchain *chaininfra.PancakeV3Services
	registry   *registry.PancakeCompositeRegistry
}

type quickSwapV3Resources struct {
	blockchain *chaininfra.QuickSwapV3Services
	registry   *registry.QuickSwapCompositeRegistry
}

type univ4Resources struct {
	blockchain *chaininfra.Univ4Services
	registry   *registry.CompositeV4Registry
}

type balancerResources struct {
	blockchain *chaininfra.BalancerServices
	registry   *registry.CompositeBalancerRegistry
}

func newProtocolResources(cfg config.ChainConfig, chain *chaininfra.Services) (protocolResources, error) {
	var result protocolResources
	var topicGroups [][]common.Hash
	if cfg.Sync.Univ3.IsActive() {
		blockchain, err := chaininfra.NewUniv3Services(chain, cfg.Univ3BlockchainConfig())
		if err != nil {
			return protocolResources{}, fmt.Errorf("create univ3 blockchain adapters: %w", err)
		}
		result.univ3 = &univ3Resources{blockchain: blockchain, registry: newPoolRegistry(cfg)}
		topicGroups = append(topicGroups, chaininfra.PoolLogTopics())
	}
	if cfg.Sync.PancakeV3.IsActive() {
		blockchain, err := chaininfra.NewPancakeV3Services(chain)
		if err != nil {
			return protocolResources{}, fmt.Errorf("create pancakev3 blockchain adapters: %w", err)
		}
		result.pancakeV3 = &pancakeV3Resources{blockchain: blockchain, registry: newPancakePoolRegistry(cfg)}
		topicGroups = append(topicGroups, chaininfra.PancakePoolLogTopics())
	}
	if cfg.Sync.QuickSwapV3.IsActive() {
		blockchain, err := chaininfra.NewQuickSwapV3Services(chain)
		if err != nil {
			return protocolResources{}, fmt.Errorf("create quickswapv3 blockchain adapters: %w", err)
		}
		result.quickSwapV3 = &quickSwapV3Resources{blockchain: blockchain, registry: newQuickSwapPoolRegistry(cfg)}
		topicGroups = append(topicGroups, chaininfra.QuickSwapPoolLogTopics())
	}
	if cfg.Sync.Univ4.IsActive() {
		blockchain, err := chaininfra.NewUniv4Services(chain, cfg.Univ4BlockchainConfig())
		if err != nil {
			return protocolResources{}, fmt.Errorf("create univ4 blockchain adapters: %w", err)
		}
		poolRegistry, err := newV4PoolRegistry(cfg)
		if err != nil {
			return protocolResources{}, fmt.Errorf("create univ4 pool registry: %w", err)
		}
		result.univ4 = &univ4Resources{blockchain: blockchain, registry: poolRegistry}
		topicGroups = append(topicGroups, chaininfra.V4PoolLogTopics())
	}
	if cfg.Sync.Balancer.IsActive() {
		blockchain, err := chaininfra.NewBalancerServices(chain, cfg.BalancerBlockchainConfig())
		if err != nil {
			return protocolResources{}, fmt.Errorf("create balancer blockchain adapters: %w", err)
		}
		poolRegistry, err := newBalancerPoolRegistry(cfg)
		if err != nil {
			return protocolResources{}, fmt.Errorf("create balancer pool registry: %w", err)
		}
		result.balancer = &balancerResources{blockchain: blockchain, registry: poolRegistry}
		topicGroups = append(
			topicGroups,
			chaininfra.BalancerVaultV2LogTopics(),
			chaininfra.BalancerVaultV3LogTopics(),
			chaininfra.BalancerPoolV2LogTopics(),
		)
	}
	if len(topicGroups) > 0 {
		result.headLogFetcher = chaininfra.NewHeadLogFetcher(chain.Client, topicGroups...)
	}
	return result, nil
}

func newPoolRegistry(cfg config.ChainConfig) *registry.CompositeRegistry {
	return registry.NewCompositeRegistry(cfg.Sync.Univ3)
}

func newPancakePoolRegistry(cfg config.ChainConfig) *registry.PancakeCompositeRegistry {
	if !cfg.Sync.PancakeV3.IsActive() {
		return nil
	}
	return registry.NewPancakeCompositeRegistry(cfg.Sync.PancakeV3)
}

func newQuickSwapPoolRegistry(cfg config.ChainConfig) *registry.QuickSwapCompositeRegistry {
	if !cfg.Sync.QuickSwapV3.IsActive() {
		return nil
	}
	return registry.NewQuickSwapCompositeRegistry(cfg.Sync.QuickSwapV3)
}

func newV4PoolRegistry(cfg config.ChainConfig) (*registry.CompositeV4Registry, error) {
	if !cfg.Sync.Univ4.IsActive() {
		return nil, nil
	}
	return registry.NewCompositeV4Registry(cfg.Sync.Univ4)
}

func newBalancerPoolRegistry(cfg config.ChainConfig) (*registry.CompositeBalancerRegistry, error) {
	if !cfg.Sync.Balancer.IsActive() {
		return nil, nil
	}
	blockchainCfg := cfg.BalancerBlockchainConfig()
	return registry.NewCompositeBalancerRegistry(cfg.Sync.Balancer, blockchainCfg.VaultAddress, blockchainCfg.VaultV3Address)
}
