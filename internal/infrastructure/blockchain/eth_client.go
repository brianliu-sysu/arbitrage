package blockchain

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ErrClientClosed is returned when RPC methods are called after the client is closed.
var ErrClientClosed = errors.New("rpc client closed")

// EthClient wraps go-ethereum RPC access.
type EthClient struct {
	url      string
	wsURL    string
	client   *ethclient.Client
	wsClient *ethclient.Client
	mu       sync.Mutex
}

func NewEthClient(cfg Config) (*EthClient, error) {
	if strings.TrimSpace(cfg.RPCURL) == "" {
		return nil, fmt.Errorf("rpc url is required")
	}
	client, err := ethclient.Dial(cfg.RPCURL)
	if err != nil {
		return nil, fmt.Errorf("dial rpc: %w", err)
	}

	ethClient := &EthClient{
		url:    cfg.RPCURL,
		wsURL:  cfg.WSURL,
		client: client,
	}
	if strings.TrimSpace(cfg.WSURL) == "" {
		return ethClient, nil
	}

	wsClient, err := ethclient.Dial(cfg.WSURL)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("dial ws rpc: %w", err)
	}
	ethClient.wsClient = wsClient
	return ethClient, nil
}

func (c *EthClient) Client() *ethclient.Client {
	return c.client
}

func (c *EthClient) rpcClient() (*ethclient.Client, error) {
	if c == nil || c.client == nil {
		return nil, ErrClientClosed
	}
	return c.client, nil
}

func (c *EthClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.wsClient != nil {
		c.wsClient.Close()
		c.wsClient = nil
	}
	if c.client != nil {
		c.client.Close()
		c.client = nil
	}
}

func (c *EthClient) Name() string {
	return "rpc"
}

func (c *EthClient) Ping(ctx context.Context) error {
	rpcClient, err := c.rpcClient()
	if err != nil {
		return err
	}
	if _, err := rpcClient.BlockNumber(ctx); err != nil {
		return fmt.Errorf("rpc ping: %w", err)
	}
	return nil
}

func (c *EthClient) GetBlockHeader(ctx context.Context, blockNumber uint64) (domainchain.BlockHeader, error) {
	rpcClient, err := c.rpcClient()
	if err != nil {
		return domainchain.BlockHeader{}, err
	}
	header, err := rpcClient.HeaderByNumber(ctx, new(big.Int).SetUint64(blockNumber))
	if err != nil {
		return domainchain.BlockHeader{}, fmt.Errorf("header by number %d: %w", blockNumber, err)
	}
	return headerToDomain(header), nil
}

func (c *EthClient) GetLatestBlockHeader(ctx context.Context) (domainchain.BlockHeader, error) {
	rpcClient, err := c.rpcClient()
	if err != nil {
		return domainchain.BlockHeader{}, err
	}
	header, err := rpcClient.HeaderByNumber(ctx, nil)
	if err != nil {
		return domainchain.BlockHeader{}, fmt.Errorf("latest header: %w", err)
	}
	return headerToDomain(header), nil
}

func (c *EthClient) CallContract(ctx context.Context, to common.Address, data []byte, blockNumber uint64) ([]byte, error) {
	return callContractWithRetry(ctx, c, to, data, blockNumber)
}

func (c *EthClient) CodeAt(ctx context.Context, address common.Address, blockNumber uint64) ([]byte, error) {
	return codeAtWithRetry(ctx, c, address, blockNumber)
}

func (c *EthClient) ChainID(ctx context.Context) (*big.Int, error) {
	rpcClient, err := c.rpcClient()
	if err != nil {
		return nil, err
	}
	return rpcClient.ChainID(ctx)
}

func (c *EthClient) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	rpcClient, err := c.rpcClient()
	if err != nil {
		return nil, err
	}
	return rpcClient.FilterLogs(ctx, query)
}

func (c *EthClient) SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error) {
	if c.wsClient == nil {
		return nil, fmt.Errorf("websocket rpc is required for head subscription; set rpc.ws_url in config")
	}
	return c.wsClient.SubscribeNewHead(ctx, ch)
}

func headerToDomain(header *types.Header) domainchain.BlockHeader {
	return domainchain.BlockHeader{
		Number:     header.Number.Uint64(),
		Hash:       header.Hash(),
		ParentHash: header.ParentHash,
	}
}
