package registry

import (
	"fmt"
	"sort"
	"strings"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

type balancerPoolEntry struct {
	id   marketbalancer.PoolID
	spec marketbalancer.PoolSpec
}

func parseStaticBalancerPools(pools []config.StaticBalancerPoolConfig, defaultVault common.Address) ([]balancerPoolEntry, error) {
	entries := make([]balancerPoolEntry, 0, len(pools))
	for i, pool := range pools {
		entry, err := balancerPoolEntryFromConfig(pool, defaultVault, i)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func balancerPoolEntryFromConfig(pool config.StaticBalancerPoolConfig, defaultVault common.Address, index int) (balancerPoolEntry, error) {
	poolType := marketbalancer.PoolType(strings.ToLower(strings.TrimSpace(pool.Type)))
	if err := poolType.Validate(); err != nil {
		return balancerPoolEntry{}, fmt.Errorf("sync.balancer.pools[%d]: %w", index, err)
	}
	vault := common.HexToAddress(pool.Vault)
	if (vault == common.Address{}) {
		vault = defaultVault
	}
	entry := balancerPoolEntry{
		id: marketbalancer.PoolID(common.HexToHash(pool.ID)),
		spec: marketbalancer.PoolSpec{
			Address: common.HexToAddress(pool.Address),
			Vault:   vault,
			Type:    poolType,
		},
	}
	return entry, nil
}

func sortBalancerPoolEntries(entries []balancerPoolEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].id.String() < entries[j].id.String()
	})
}
