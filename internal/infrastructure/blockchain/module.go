package blockchain

import (
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
)

// Services bundles blockchain infrastructure adapters for sync wiring.
type Services struct {
	Client     *EthClient
	Multicall  *Multicall
	LogFetcher *LogFetcher
	HeadSub    *HeadSubscriber
	Parser     *ABIParser
	Factory    *FactoryReader
	PoolReader *PoolReader
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

	return &Services{
		Client:     client,
		Multicall:  multicall,
		LogFetcher: NewLogFetcher(client),
		HeadSub:    NewHeadSubscriber(client),
		Parser:     parser,
		Factory:    factory,
		PoolReader: poolReader,
	}, nil
}

func (s *Services) Close() {
	if s.Client != nil {
		s.Client.Close()
	}
}

// SyncDeps returns application sync dependencies backed by this package.
func (s *Services) SyncDeps() syncapp.ServiceDeps {
	return syncapp.ServiceDeps{
		Fetcher:    s.LogFetcher,
		Parser:     s.Parser,
		Blocks:     s.Client,
		Bootstrap:  s.PoolReader,
		Subscriber: s.HeadSub,
		Health:     []syncapp.HealthProbe{s.Client},
	}
}
