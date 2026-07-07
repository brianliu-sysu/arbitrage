package blockchain

import (
	"context"
	"fmt"
	"math/big"

	domainchain "github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

// BalancerLogFetcher loads Balancer V2/V3 Vault and pool contract logs via eth_getLogs.
type BalancerLogFetcher struct {
	client  *EthClient
	vaultV2 common.Address
	vaultV3 common.Address
}

func NewBalancerLogFetcher(client *EthClient, vaultV2, vaultV3 common.Address) *BalancerLogFetcher {
	return &BalancerLogFetcher{client: client, vaultV2: vaultV2, vaultV3: vaultV3}
}

func (f *BalancerLogFetcher) FetchLogs(ctx context.Context, filter domainchain.BalancerLogFilter) ([]domainchain.RawLog, error) {
	if filter.ToBlock < filter.FromBlock {
		return nil, fmt.Errorf("invalid block range: from %d to %d", filter.FromBlock, filter.ToBlock)
	}
	if len(filter.V2PoolIDs) == 0 && len(filter.V3PoolAddresses) == 0 && len(filter.V2PoolAddresses) == 0 {
		return nil, fmt.Errorf("balancer log filter requires tracked pools")
	}

	raw := make([]domainchain.RawLog, 0)
	if len(filter.V2PoolIDs) > 0 {
		if (f.vaultV2 == common.Address{}) {
			return nil, fmt.Errorf("balancer v2 vault address is required")
		}
		v2VaultLogs, err := f.filterVaultLogs(ctx, f.vaultV2, BalancerVaultV2LogTopics(), poolIDTopics(filter.V2PoolIDs), filter.FromBlock, filter.ToBlock)
		if err != nil {
			return nil, err
		}
		raw = append(raw, v2VaultLogs...)
	}
	if len(filter.V3PoolAddresses) > 0 {
		if (f.vaultV3 == common.Address{}) {
			return nil, fmt.Errorf("balancer v3 vault address is required")
		}
		v3VaultLogs, err := f.filterVaultLogs(ctx, f.vaultV3, BalancerVaultV3LogTopics(), addressTopics(filter.V3PoolAddresses), filter.FromBlock, filter.ToBlock)
		if err != nil {
			return nil, err
		}
		raw = append(raw, v3VaultLogs...)
	}
	if len(filter.V2PoolAddresses) > 0 {
		poolQuery := ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(filter.FromBlock),
			ToBlock:   new(big.Int).SetUint64(filter.ToBlock),
			Addresses: filter.V2PoolAddresses,
			Topics:    [][]common.Hash{BalancerPoolV2LogTopics()},
		}
		poolLogs, err := f.client.FilterLogs(ctx, poolQuery)
		if err != nil {
			return nil, fmt.Errorf("filter balancer v2 pool logs: %w", err)
		}
		raw = append(raw, rawLogsFromEth(poolLogs)...)
	}
	return raw, nil
}

func (f *BalancerLogFetcher) filterVaultLogs(
	ctx context.Context,
	vault common.Address,
	eventTopics []common.Hash,
	poolTopics []common.Hash,
	fromBlock, toBlock uint64,
) ([]domainchain.RawLog, error) {
	query := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(fromBlock),
		ToBlock:   new(big.Int).SetUint64(toBlock),
		Addresses: []common.Address{vault},
		Topics: [][]common.Hash{
			eventTopics,
			poolTopics,
		},
	}
	logs, err := f.client.FilterLogs(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("filter balancer vault logs at %s: %w", vault.Hex(), err)
	}
	return rawLogsFromEth(logs), nil
}

func poolIDTopics(poolIDs []marketbalancer.PoolID) []common.Hash {
	topics := make([]common.Hash, 0, len(poolIDs))
	for _, id := range poolIDs {
		topics = append(topics, id.Hash())
	}
	return topics
}

func addressTopics(addresses []common.Address) []common.Hash {
	topics := make([]common.Hash, 0, len(addresses))
	for _, address := range addresses {
		topics = append(topics, common.BytesToHash(common.LeftPadBytes(address.Bytes(), 32)))
	}
	return topics
}
