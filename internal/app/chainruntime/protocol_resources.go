package chainruntime

import (
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	chaininfra "github.com/brianliu-sysu/uniswapv3/internal/infrastructure/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/registry"
	"github.com/ethereum/go-ethereum/common"
)

// protocolResources owns protocol-specific infrastructure and registry state.
type protocolResources struct {
	modules []protocolResource
}

type protocolResource interface {
	protocolResource()
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

func (*univ3Resources) protocolResource()       {}
func (*pancakeV3Resources) protocolResource()   {}
func (*quickSwapV3Resources) protocolResource() {}
func (*univ4Resources) protocolResource()       {}
func (*balancerResources) protocolResource()    {}

func findProtocolResource[T protocolResource](resources protocolResources) T {
	var zero T
	for _, module := range resources.modules {
		if typed, ok := module.(T); ok {
			return typed
		}
	}
	return zero
}

func (r protocolResources) univ3() *univ3Resources {
	return findProtocolResource[*univ3Resources](r)
}

func (r protocolResources) pancakeV3() *pancakeV3Resources {
	return findProtocolResource[*pancakeV3Resources](r)
}

func (r protocolResources) quickSwapV3() *quickSwapV3Resources {
	return findProtocolResource[*quickSwapV3Resources](r)
}

func (r protocolResources) univ4() *univ4Resources {
	return findProtocolResource[*univ4Resources](r)
}

func (r protocolResources) balancer() *balancerResources {
	return findProtocolResource[*balancerResources](r)
}

func newProtocolResources(cfg config.ChainConfig, chain *chaininfra.Services) (protocolResources, *chaininfra.HeadLogFetcher, error) {
	result := protocolResources{modules: make([]protocolResource, 0, 5)}
	var topicGroups [][]common.Hash
	if cfg.Sync.Univ3.IsActive() {
		blockchain, err := chaininfra.NewUniv3Services(chain, cfg.Univ3BlockchainConfig())
		if err != nil {
			return protocolResources{}, nil, fmt.Errorf("create univ3 blockchain adapters: %w", err)
		}
		result.modules = append(result.modules, &univ3Resources{blockchain: blockchain, registry: newPoolRegistry(cfg)})
		topicGroups = append(topicGroups, chaininfra.PoolLogTopics())
	}
	if cfg.Sync.PancakeV3.IsActive() {
		blockchain, err := chaininfra.NewPancakeV3Services(chain)
		if err != nil {
			return protocolResources{}, nil, fmt.Errorf("create pancakev3 blockchain adapters: %w", err)
		}
		result.modules = append(result.modules, &pancakeV3Resources{blockchain: blockchain, registry: newPancakePoolRegistry(cfg)})
		topicGroups = append(topicGroups, chaininfra.PancakePoolLogTopics())
	}
	if cfg.Sync.QuickSwapV3.IsActive() {
		blockchain, err := chaininfra.NewQuickSwapV3Services(chain)
		if err != nil {
			return protocolResources{}, nil, fmt.Errorf("create quickswapv3 blockchain adapters: %w", err)
		}
		result.modules = append(result.modules, &quickSwapV3Resources{blockchain: blockchain, registry: newQuickSwapPoolRegistry(cfg)})
		topicGroups = append(topicGroups, chaininfra.QuickSwapPoolLogTopics())
	}
	if cfg.Sync.Univ4.IsActive() {
		blockchain, err := chaininfra.NewUniv4Services(chain, cfg.Univ4BlockchainConfig())
		if err != nil {
			return protocolResources{}, nil, fmt.Errorf("create univ4 blockchain adapters: %w", err)
		}
		poolRegistry, err := newV4PoolRegistry(cfg)
		if err != nil {
			return protocolResources{}, nil, fmt.Errorf("create univ4 pool registry: %w", err)
		}
		result.modules = append(result.modules, &univ4Resources{blockchain: blockchain, registry: poolRegistry})
		topicGroups = append(topicGroups, chaininfra.V4PoolLogTopics())
	}
	if cfg.Sync.Balancer.IsActive() {
		blockchain, err := chaininfra.NewBalancerServices(chain, cfg.BalancerBlockchainConfig())
		if err != nil {
			return protocolResources{}, nil, fmt.Errorf("create balancer blockchain adapters: %w", err)
		}
		poolRegistry, err := newBalancerPoolRegistry(cfg)
		if err != nil {
			return protocolResources{}, nil, fmt.Errorf("create balancer pool registry: %w", err)
		}
		result.modules = append(result.modules, &balancerResources{blockchain: blockchain, registry: poolRegistry})
		topicGroups = append(
			topicGroups,
			chaininfra.BalancerVaultV2LogTopics(),
			chaininfra.BalancerVaultV3LogTopics(),
			chaininfra.BalancerPoolV2LogTopics(),
		)
	}
	var headLogFetcher *chaininfra.HeadLogFetcher
	if len(topicGroups) > 0 {
		headLogFetcher = chaininfra.NewHeadLogFetcher(chain.Client, topicGroups...)
	}
	return result, headLogFetcher, nil
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
