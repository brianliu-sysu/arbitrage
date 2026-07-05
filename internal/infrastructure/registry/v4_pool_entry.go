package registry

import (
	"fmt"
	"sort"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

type v4PoolEntry struct {
	id  marketv4.PoolID
	key marketv4.PoolKey
}

func parseStaticV4Pools(pools []config.StaticV4PoolConfig) ([]v4PoolEntry, error) {
	entries := make([]v4PoolEntry, 0, len(pools))
	for i, pool := range pools {
		entry, err := poolEntryFromConfig(pool, i)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func poolEntryFromConfig(pool config.StaticV4PoolConfig, index int) (v4PoolEntry, error) {
	key := marketv4.PoolKey{
		Currency0:   common.HexToAddress(pool.Currency0),
		Currency1:   common.HexToAddress(pool.Currency1),
		Fee:         pool.Fee,
		TickSpacing: pool.TickSpacing,
		Hooks:       common.HexToAddress(pool.Hooks),
	}
	id, err := marketv4.ComputePoolID(key)
	if err != nil {
		return v4PoolEntry{}, fmt.Errorf("sync.v4.poolmanager.pools[%d]: %w", index, err)
	}
	return v4PoolEntry{id: id, key: key}, nil
}

func sortV4PoolEntries(entries []v4PoolEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].id.String() < entries[j].id.String()
	})
}
