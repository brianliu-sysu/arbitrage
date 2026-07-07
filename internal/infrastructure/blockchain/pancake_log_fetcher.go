package blockchain

import (
	"context"
	"fmt"
	"math/big"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	clv3sync "github.com/brianliu-sysu/uniswapv3/internal/application/sync/clv3"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

// PancakeLogFetcher loads PancakeSwap V3 pool logs via eth_getLogs.
type PancakeLogFetcher struct {
	client *EthClient
}

func NewPancakeLogFetcher(client *EthClient) *PancakeLogFetcher {
	return &PancakeLogFetcher{client: client}
}

func (f *PancakeLogFetcher) FetchLogs(ctx context.Context, filter clv3sync.LogFilter) ([]syncapp.RawLog, error) {
	if filter.ToBlock < filter.FromBlock {
		return nil, fmt.Errorf("invalid block range: from %d to %d", filter.FromBlock, filter.ToBlock)
	}
	if len(filter.PoolAddresses) == 0 {
		return nil, fmt.Errorf("pool addresses are required")
	}

	query := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(filter.FromBlock),
		ToBlock:   new(big.Int).SetUint64(filter.ToBlock),
		Addresses: filter.PoolAddresses,
		Topics:    [][]common.Hash{PancakePoolLogTopics()},
	}

	logs, err := f.client.FilterLogs(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("filter logs: %w", err)
	}

	rawLogs := make([]syncapp.RawLog, 0, len(logs))
	for _, log := range logs {
		rawLogs = append(rawLogs, syncapp.RawLog{
			Address:     log.Address,
			Topics:      log.Topics,
			Data:        log.Data,
			BlockNumber: log.BlockNumber,
			BlockHash:   log.BlockHash,
			TxIndex:     log.TxIndex,
			LogIndex:    log.Index,
		})
	}
	return rawLogs, nil
}
