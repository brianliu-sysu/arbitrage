package blockchain

import (
	"context"
	"fmt"
	"math/big"

	syncbalancer "github.com/brianliu-sysu/uniswapv3/internal/application/sync/balancer"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

// BalancerLogFetcher loads Balancer Vault logs via eth_getLogs.
type BalancerLogFetcher struct {
	client *EthClient
	vault  common.Address
}

func NewBalancerLogFetcher(client *EthClient, vault common.Address) *BalancerLogFetcher {
	return &BalancerLogFetcher{client: client, vault: vault}
}

func (f *BalancerLogFetcher) FetchLogs(ctx context.Context, filter syncbalancer.LogFilter) ([]syncbalancer.RawLog, error) {
	if filter.ToBlock < filter.FromBlock {
		return nil, fmt.Errorf("invalid block range: from %d to %d", filter.FromBlock, filter.ToBlock)
	}
	if len(filter.PoolIDs) == 0 {
		return nil, fmt.Errorf("pool ids are required")
	}
	if (f.vault == common.Address{}) {
		return nil, fmt.Errorf("balancer vault address is required")
	}

	poolTopics := make([]common.Hash, 0, len(filter.PoolIDs))
	for _, id := range filter.PoolIDs {
		poolTopics = append(poolTopics, id.Hash())
	}

	vaultQuery := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(filter.FromBlock),
		ToBlock:   new(big.Int).SetUint64(filter.ToBlock),
		Addresses: []common.Address{f.vault},
		Topics: [][]common.Hash{
			BalancerVaultLogTopics(),
			poolTopics,
		},
	}

	logs, err := f.client.FilterLogs(ctx, vaultQuery)
	if err != nil {
		return nil, fmt.Errorf("filter balancer vault logs: %w", err)
	}
	raw := rawLogsFromEth(logs)

	if len(filter.PoolAddresses) > 0 {
		poolQuery := ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(filter.FromBlock),
			ToBlock:   new(big.Int).SetUint64(filter.ToBlock),
			Addresses: filter.PoolAddresses,
			Topics:    [][]common.Hash{BalancerPoolLogTopics()},
		}
		poolLogs, err := f.client.FilterLogs(ctx, poolQuery)
		if err != nil {
			return nil, fmt.Errorf("filter balancer pool logs: %w", err)
		}
		raw = append(raw, rawLogsFromEth(poolLogs)...)
	}
	return raw, nil
}
