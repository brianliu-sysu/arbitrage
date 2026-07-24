package blockchain

import "fmt"

// Services owns chain-wide infrastructure shared by every enabled protocol.
type Services struct {
	Client    *EthClient
	Multicall *Multicall
	HeadSub   *HeadSubscriber
	ERC20     *ERC20Reader
}

type Univ3Services struct {
	LogFetcher *LogFetcher
	Parser     *ABIParser
	Factory    *FactoryReader
	PoolReader *PoolReader
}

type PancakeV3Services struct {
	LogFetcher *LogFetcher
	Parser     *PancakeABIParser
	PoolReader *PoolReader
}

type QuickSwapV3Services struct {
	LogFetcher *LogFetcher
	Parser     *QuickSwapABIParser
	PoolReader *PoolReader
}

type Univ4Services struct {
	LogFetcher *V4LogFetcher
	Parser     *V4ABIParser
	PoolReader *V4PoolReader
}

type BalancerServices struct {
	LogFetcher *BalancerLogFetcher
	Parser     *BalancerABIParser
	PoolReader *BalancerPoolReader
}

func NewServices(cfg Config) (*Services, error) {
	client, err := NewEthClient(cfg)
	if err != nil {
		return nil, err
	}
	closeOnError := func(err error) (*Services, error) {
		client.Close()
		return nil, err
	}

	multicall, err := NewMulticall(client, cfg.MulticallAddress)
	if err != nil {
		return closeOnError(fmt.Errorf("create multicall: %w", err))
	}
	erc20, err := NewERC20Reader(multicall)
	if err != nil {
		return closeOnError(fmt.Errorf("create erc20 reader: %w", err))
	}
	return &Services{
		Client:    client,
		Multicall: multicall,
		HeadSub:   NewHeadSubscriber(client),
		ERC20:     erc20,
	}, nil
}

func NewUniv3Services(chain *Services, cfg Univ3Config) (*Univ3Services, error) {
	parser, err := NewABIParser()
	if err != nil {
		return nil, fmt.Errorf("create univ3 abi parser: %w", err)
	}
	factory, err := NewFactoryReader(chain.Client, cfg.FactoryAddress)
	if err != nil {
		return nil, fmt.Errorf("create univ3 factory reader: %w", err)
	}
	poolReader, err := NewPoolReader(chain.Client, chain.Multicall)
	if err != nil {
		return nil, fmt.Errorf("create univ3 pool reader: %w", err)
	}
	return &Univ3Services{
		LogFetcher: NewLogFetcher(chain.Client),
		Parser:     parser,
		Factory:    factory,
		PoolReader: poolReader,
	}, nil
}

func NewPancakeV3Services(chain *Services) (*PancakeV3Services, error) {
	parser, err := NewPancakeABIParser()
	if err != nil {
		return nil, fmt.Errorf("create pancakev3 abi parser: %w", err)
	}
	poolReader, err := NewPancakePoolReader(chain.Client, chain.Multicall)
	if err != nil {
		return nil, fmt.Errorf("create pancakev3 pool reader: %w", err)
	}
	return &PancakeV3Services{
		LogFetcher: NewPancakeLogFetcher(chain.Client),
		Parser:     parser,
		PoolReader: poolReader,
	}, nil
}

func NewQuickSwapV3Services(chain *Services) (*QuickSwapV3Services, error) {
	parser, err := NewQuickSwapABIParser()
	if err != nil {
		return nil, fmt.Errorf("create quickswapv3 abi parser: %w", err)
	}
	poolReader, err := NewQuickSwapPoolReader(chain.Client, chain.Multicall)
	if err != nil {
		return nil, fmt.Errorf("create quickswapv3 pool reader: %w", err)
	}
	return &QuickSwapV3Services{
		LogFetcher: NewQuickSwapLogFetcher(chain.Client),
		Parser:     parser,
		PoolReader: poolReader,
	}, nil
}

func NewUniv4Services(chain *Services, cfg Univ4Config) (*Univ4Services, error) {
	parser, err := NewV4ABIParser()
	if err != nil {
		return nil, fmt.Errorf("create univ4 abi parser: %w", err)
	}
	poolReader, err := NewV4PoolReader(chain.Client, chain.Multicall, cfg.StateViewAddress)
	if err != nil {
		return nil, fmt.Errorf("create univ4 pool reader: %w", err)
	}
	return &Univ4Services{
		LogFetcher: NewV4LogFetcher(chain.Client, cfg.PoolManagerAddress),
		Parser:     parser,
		PoolReader: poolReader,
	}, nil
}

func NewBalancerServices(chain *Services, cfg BalancerConfig) (*BalancerServices, error) {
	parser, err := NewBalancerABIParser()
	if err != nil {
		return nil, fmt.Errorf("create balancer abi parser: %w", err)
	}
	poolReader, err := NewBalancerPoolReader(chain.Client, chain.Multicall)
	if err != nil {
		return nil, fmt.Errorf("create balancer pool reader: %w", err)
	}
	return &BalancerServices{
		LogFetcher: NewBalancerLogFetcher(chain.Client, cfg.VaultAddress, cfg.VaultV3Address),
		Parser:     parser,
		PoolReader: poolReader,
	}, nil
}

func (s *Services) Close() {
	if s != nil && s.Client != nil {
		s.Client.Close()
	}
}
