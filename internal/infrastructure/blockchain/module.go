package blockchain

import (
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	syncbalancer "github.com/brianliu-sysu/uniswapv3/internal/application/sync/balancer"
	syncpancakev3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/pancakev3"
	syncv3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/univ3"
	syncv4 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// Services bundles blockchain infrastructure adapters for sync wiring.
type Services struct {
	Client             *EthClient
	Multicall          *Multicall
	LogFetcher         *LogFetcher
	PancakeLogFetcher  *LogFetcher
	HeadSub            *HeadSubscriber
	Parser             *ABIParser
	PancakeParser      *PancakeABIParser
	Factory            *FactoryReader
	PoolReader         *PoolReader
	PancakePoolReader  *PoolReader
	V4LogFetcher       *V4LogFetcher
	V4Parser           *V4ABIParser
	V4PoolReader       *V4PoolReader
	BalancerLogFetcher *BalancerLogFetcher
	BalancerParser     *BalancerABIParser
	BalancerPoolReader *BalancerPoolReader
	ERC20              *ERC20Reader
}

func NewServices(cfg Config) (*Services, error) {
	client, err := NewEthClient(cfg)
	if err != nil {
		return nil, err
	}

	multicall, err := NewMulticall(client, cfg.MulticallAddress)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("create multicall: %w", err)
	}

	parser, err := NewABIParser()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("create abi parser: %w", err)
	}

	pancakeParser, err := NewPancakeABIParser()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("create pancake abi parser: %w", err)
	}

	factory, err := NewFactoryReader(client, cfg.FactoryAddress)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("create factory reader: %w", err)
	}

	poolReader, err := NewPoolReader(client, multicall)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("create pool reader: %w", err)
	}

	pancakePoolReader, err := NewPancakePoolReader(client, multicall)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("create pancake pool reader: %w", err)
	}

	v4Parser, err := NewV4ABIParser()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("create v4 abi parser: %w", err)
	}

	balancerParser, err := NewBalancerABIParser()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("create balancer abi parser: %w", err)
	}

	balancerPoolReader, err := NewBalancerPoolReader(client, multicall)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("create balancer pool reader: %w", err)
	}

	var v4PoolReader *V4PoolReader
	if (cfg.StateViewAddress != common.Address{}) {
		v4PoolReader, err = NewV4PoolReader(client, multicall, cfg.StateViewAddress)
		if err != nil {
			client.Close()
			return nil, fmt.Errorf("create v4 pool reader: %w", err)
		}
	}

	erc20Reader, err := NewERC20Reader(multicall)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("create erc20 reader: %w", err)
	}

	return &Services{
		Client:             client,
		Multicall:          multicall,
		LogFetcher:         NewLogFetcher(client),
		PancakeLogFetcher:  NewPancakeLogFetcher(client),
		HeadSub:            NewHeadSubscriber(client),
		Parser:             parser,
		PancakeParser:      pancakeParser,
		Factory:            factory,
		PoolReader:         poolReader,
		PancakePoolReader:  pancakePoolReader,
		V4LogFetcher:       NewV4LogFetcher(client, cfg.PoolManagerAddress),
		V4Parser:           v4Parser,
		V4PoolReader:       v4PoolReader,
		BalancerLogFetcher: NewBalancerLogFetcher(client, cfg.BalancerVaultAddress),
		BalancerParser:     balancerParser,
		BalancerPoolReader: balancerPoolReader,
		ERC20:              erc20Reader,
	}, nil
}

func (s *Services) Close() {
	if s.Client != nil {
		s.Client.Close()
	}
}

// SyncDeps returns application sync dependencies backed by this package.
func (s *Services) SyncDeps() syncv3.ServiceDeps {
	return syncv3.ServiceDeps{
		Fetcher:    s.LogFetcher,
		Parser:     s.Parser,
		Blocks:     s.Client,
		Bootstrap:  s.PoolReader,
		Subscriber: s.HeadSub,
		Health:     []syncapp.HealthProbe{s.Client},
	}
}

// SyncPancakeV3Deps returns application PancakeSwap V3 sync dependencies backed by this package.
func (s *Services) SyncPancakeV3Deps() syncpancakev3.ServiceDeps {
	return syncpancakev3.ServiceDeps{
		Fetcher:    s.PancakeLogFetcher,
		Parser:     s.PancakeParser,
		Blocks:     s.Client,
		Bootstrap:  s.PancakePoolReader,
		Subscriber: s.HeadSub,
		Health:     []syncapp.HealthProbe{s.Client},
	}
}

// SyncV4Deps returns application V4 sync dependencies backed by this package.
func (s *Services) SyncV4Deps() syncv4.ServiceDeps {
	return syncv4.ServiceDeps{
		Fetcher:    s.V4LogFetcher,
		Parser:     s.V4Parser,
		Blocks:     s.Client,
		Bootstrap:  s.V4PoolReader,
		Subscriber: s.HeadSub,
		Health:     []syncapp.HealthProbe{s.Client},
	}
}

// SyncBalancerDeps returns application Balancer sync dependencies backed by this package.
func (s *Services) SyncBalancerDeps() syncbalancer.ServiceDeps {
	return syncbalancer.ServiceDeps{
		Fetcher:    s.BalancerLogFetcher,
		Parser:     s.BalancerParser,
		Blocks:     s.Client,
		Bootstrap:  s.BalancerPoolReader,
		Subscriber: s.HeadSub,
		Health:     []syncapp.HealthProbe{s.Client},
	}
}
