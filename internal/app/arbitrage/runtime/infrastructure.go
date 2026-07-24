package runtime

import (
	"context"
	"fmt"

	contractapp "github.com/brianliu-sysu/uniswapv3/internal/application/contract"
	"github.com/brianliu-sysu/uniswapv3/internal/config"
	chaininfra "github.com/brianliu-sysu/uniswapv3/internal/infrastructure/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence"
)

func newContractExecutorAppService(cfg config.ChainConfig) (*contractapp.AppService, error) {
	broadcaster, err := chaininfra.NewContractExecutorBroadcaster(cfg.BlockchainConfig().MulticallAddress)
	if err != nil {
		return nil, err
	}
	return contractapp.NewAppService(broadcaster), nil
}

type chainStores struct {
	durable         *persistence.Services
	runtime         *persistence.Services
	separateRuntime bool
}

func newChainStores(cfg persistence.Config) (*chainStores, error) {
	durable, err := newChainPersistence(cfg)
	if err != nil {
		return nil, err
	}
	runtime := durable
	separateRuntime := false
	if !cfg.UseMemory {
		runtime = persistence.MemoryServices()
		separateRuntime = true
	}
	return &chainStores{
		durable:         durable,
		runtime:         runtime,
		separateRuntime: separateRuntime,
	}, nil
}

func (s *chainStores) hasSeparateRuntime() bool {
	return s != nil && s.separateRuntime
}

func (s *chainStores) backendName() string {
	if s == nil || s.durable == nil {
		return ""
	}
	return s.durable.BackendName()
}

func (s *chainStores) usesMemory() bool {
	return s != nil && !s.hasSeparateRuntime()
}

func (s *chainStores) Close() {
	if s == nil {
		return
	}
	if s.hasSeparateRuntime() {
		s.runtime.Close()
	}
	if s.durable != nil {
		s.durable.Close()
	}
}

func newChainResources(persistenceCfg persistence.Config, chainCfg config.ChainConfig) (*chainResources, error) {
	stores, err := newChainStores(persistenceCfg)
	if err != nil {
		return nil, fmt.Errorf("create persistence: %w", err)
	}

	blockchain, err := chaininfra.NewServices(chainCfg.BlockchainConfig())
	if err != nil {
		stores.Close()
		return nil, fmt.Errorf("create blockchain services: %w", err)
	}

	protocols, err := newProtocolResources(chainCfg, blockchain)
	if err != nil {
		blockchain.Close()
		stores.Close()
		return nil, fmt.Errorf("create protocol blockchain services: %w", err)
	}

	contractExecutor, err := newContractExecutorAppService(chainCfg)
	if err != nil {
		blockchain.Close()
		stores.Close()
		return nil, fmt.Errorf("create contract executor: %w", err)
	}

	persistenceCtx, cancelPersistence := context.WithCancel(context.Background())
	return &chainResources{
		stores:            stores,
		blockchain:        blockchain,
		protocols:         protocols,
		contractExecutor:  contractExecutor,
		persistenceCtx:    persistenceCtx,
		cancelPersistence: cancelPersistence,
	}, nil
}

func newChainPersistence(cfg persistence.Config) (*persistence.Services, error) {
	if cfg.UseMemory {
		return persistence.MemoryServices(), nil
	}
	return persistence.NewServices(context.Background(), cfg)
}
