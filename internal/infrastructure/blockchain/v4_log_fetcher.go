package blockchain

import (
	"context"
	"fmt"
	"math/big"

	syncv4 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/v4"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

// V4LogFetcher loads PoolManager logs via eth_getLogs.
type V4LogFetcher struct {
	client      *EthClient
	poolManager common.Address
}

func NewV4LogFetcher(client *EthClient, poolManager common.Address) *V4LogFetcher {
	return &V4LogFetcher{client: client, poolManager: poolManager}
}

func (f *V4LogFetcher) FetchLogs(ctx context.Context, filter syncv4.LogFilter) ([]syncv4.RawLog, error) {
	if filter.ToBlock < filter.FromBlock {
		return nil, fmt.Errorf("invalid block range: from %d to %d", filter.FromBlock, filter.ToBlock)
	}
	if len(filter.PoolIDs) == 0 {
		return nil, fmt.Errorf("pool ids are required")
	}
	if (f.poolManager == common.Address{}) {
		return nil, fmt.Errorf("pool manager address is required")
	}

	poolTopics := make([]common.Hash, 0, len(filter.PoolIDs))
	for _, id := range filter.PoolIDs {
		poolTopics = append(poolTopics, id.Hash())
	}

	query := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(filter.FromBlock),
		ToBlock:   new(big.Int).SetUint64(filter.ToBlock),
		Addresses: []common.Address{f.poolManager},
		Topics: [][]common.Hash{
			V4PoolLogTopics(),
			poolTopics,
		},
	}

	logs, err := f.client.FilterLogs(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("filter logs: %w", err)
	}

	rawLogs := make([]syncv4.RawLog, 0, len(logs))
	for _, log := range logs {
		rawLogs = append(rawLogs, syncv4.RawLog{
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
