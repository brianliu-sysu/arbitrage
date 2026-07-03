package blockchain

import (
	"context"
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

// EthClient wraps go-ethereum RPC access.
type EthClient struct {
	url    string
	client *ethclient.Client
	mu     sync.Mutex
}

func NewEthClient(cfg Config) (*EthClient, error) {
	if strings.TrimSpace(cfg.RPCURL) == "" {
		return nil, fmt.Errorf("rpc url is required")
	}
	client, err := ethclient.Dial(cfg.RPCURL)
	if err != nil {
		return nil, fmt.Errorf("dial rpc: %w", err)
	}
	return &EthClient{url: cfg.RPCURL, client: client}, nil
}

func (c *EthClient) Client() *ethclient.Client {
	return c.client
}

func (c *EthClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		c.client.Close()
		c.client = nil
	}
}

func (c *EthClient) Name() string {
	return "rpc"
}

func (c *EthClient) Ping(ctx context.Context) error {
	_, err := c.client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("rpc ping: %w", err)
	}
	return nil
}

func (c *EthClient) GetBlockHeader(ctx context.Context, blockNumber uint64) (domainchain.BlockHeader, error) {
	header, err := c.client.HeaderByNumber(ctx, new(big.Int).SetUint64(blockNumber))
	if err != nil {
		return domainchain.BlockHeader{}, fmt.Errorf("header by number %d: %w", blockNumber, err)
	}
	return headerToDomain(header), nil
}

func (c *EthClient) GetLatestBlockHeader(ctx context.Context) (domainchain.BlockHeader, error) {
	header, err := c.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return domainchain.BlockHeader{}, fmt.Errorf("latest header: %w", err)
	}
	return headerToDomain(header), nil
}

func (c *EthClient) CallContract(ctx context.Context, to common.Address, data []byte, blockNumber uint64) ([]byte, error) {
	var block *big.Int
	if blockNumber > 0 {
		block = new(big.Int).SetUint64(blockNumber)
	}
	return c.client.CallContract(ctx, ethereum.CallMsg{
		To:   &to,
		Data: data,
	}, block)
}

func (c *EthClient) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	return c.client.FilterLogs(ctx, query)
}

func (c *EthClient) SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error) {
	return c.client.SubscribeNewHead(ctx, ch)
}

func headerToDomain(header *types.Header) domainchain.BlockHeader {
	return domainchain.BlockHeader{
		Number:     header.Number.Uint64(),
		Hash:       header.Hash(),
		ParentHash: header.ParentHash,
	}
}
