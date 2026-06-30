package blockchain

import (
	"context"
	"fmt"

	"math/big"

	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)
type LogFetcher struct {
	client *Client
}

// NewLogFetcher 创建 LogFetcher。
func NewLogFetcher(client *Client) *LogFetcher {
	return &LogFetcher{client: client}
}

// FetchBlockLogs 获取单个区块内所有跟踪池子的 V3 事件。
func (f *LogFetcher) FetchBlockLogs(ctx context.Context, blockNum uint64, poolAddrs []common.Address) ([]types.Log, error) {
	if f.client == nil {
		return nil, fmt.Errorf("rpc client is nil")
	}
	eth, err := f.client.Eth()
	if err != nil {
		return nil, err
	}
	if len(poolAddrs) == 0 {
		return nil, nil
	}

	block := new(big.Int).SetUint64(blockNum)
	logs, err := eth.FilterLogs(ctx, ethereum.FilterQuery{
		FromBlock: block,
		ToBlock:   block,
		Addresses: poolAddrs,
		Topics: [][]common.Hash{{
			pool.SwapEventSignature,
			pool.MintEventSignature,
			pool.BurnEventSignature,
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("filter logs block %d: %w", blockNum, err)
	}
	return logs, nil
}

// GroupLogsByPool 按池子地址分组日志。
func GroupLogsByPool(logs []types.Log) map[common.Address][]types.Log {
	out := make(map[common.Address][]types.Log)
	for _, lg := range logs {
		if len(lg.Address) == 0 {
			continue
		}
		addr := lg.Address
		out[addr] = append(out[addr], lg)
	}
	return out
}
