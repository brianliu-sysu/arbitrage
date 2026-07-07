package balancersync

import (
	"context"
	"fmt"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

func bindBalancerPoolAddresses(
	ctx context.Context,
	registry marketbalancer.PoolRegistry,
	parser EventParser,
	poolIDs []marketbalancer.PoolID,
) ([]common.Address, error) {
	if registry == nil {
		return nil, fmt.Errorf("balancer registry is not configured")
	}
	poolIDByAddress := make(map[common.Address]marketbalancer.PoolID, len(poolIDs))
	addresses := make([]common.Address, 0, len(poolIDs))
	for _, poolID := range poolIDs {
		spec, err := registry.GetSpec(ctx, poolID)
		if err != nil {
			return nil, fmt.Errorf("resolve pool spec %s: %w", poolID.String(), err)
		}
		if (spec.Address == common.Address{}) {
			continue
		}
		poolIDByAddress[spec.Address] = poolID
		addresses = append(addresses, spec.Address)
	}
	if binder, ok := parser.(PoolAddressBinder); ok {
		binder.SetPoolAddressMap(poolIDByAddress)
	}
	return addresses, nil
}
