package blockchain

import (
	"context"
	"fmt"
	"math/big"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// LogFetcher loads CLV3 pool logs via eth_getLogs.
type LogFetcher struct {
	client *EthClient
	topics []common.Hash
}

func NewLogFetcher(client *EthClient) *LogFetcher {
	return &LogFetcher{client: client, topics: PoolLogTopics()}
}

func NewPancakeLogFetcher(client *EthClient) *LogFetcher {
	return &LogFetcher{client: client, topics: PancakePoolLogTopics()}
}

func (f *LogFetcher) FetchLogs(ctx context.Context, filter domainchain.CLV3LogFilter) ([]domainchain.RawLog, error) {
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
		Topics:    [][]common.Hash{f.topics},
	}

	logs, err := f.client.FilterLogs(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("filter logs: %w", err)
	}
	return rawLogsFromEth(logs), nil
}

func rawLogsFromEth(logs []types.Log) []domainchain.RawLog {
	rawLogs := make([]domainchain.RawLog, 0, len(logs))
	for _, log := range logs {
		rawLogs = append(rawLogs, domainchain.RawLog{
			Address:     log.Address,
			Topics:      log.Topics,
			Data:        log.Data,
			BlockNumber: log.BlockNumber,
			BlockHash:   log.BlockHash,
			TxIndex:     log.TxIndex,
			LogIndex:    log.Index,
		})
	}
	return rawLogs
}
