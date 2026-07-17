package balancersync

import (
	"context"
	"fmt"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

// PoolLogBinding groups tracked pools by vault version for log fetching.
type PoolLogBinding struct {
	PoolIDByAddress map[common.Address]marketbalancer.PoolID
	V2PoolIDs       []marketbalancer.PoolID
	V2PoolAddresses []common.Address
	V3PoolAddresses []common.Address
}

func bindBalancerPools(
	ctx context.Context,
	registry marketbalancer.PoolRegistry,
	parser EventParser,
	poolIDs []marketbalancer.PoolID,
) (PoolLogBinding, error) {
	if registry == nil {
		return PoolLogBinding{}, fmt.Errorf("balancer registry is not configured")
	}
	binding := PoolLogBinding{
		PoolIDByAddress: make(map[common.Address]marketbalancer.PoolID, len(poolIDs)),
	}
	for _, poolID := range poolIDs {
		spec, err := registry.GetSpec(ctx, poolID)
		if err != nil {
			// Active pools can briefly lose subgraph-cache membership after a refresh
			// if they were never pinned into the mutable registry. Skip them so one
			// missing Balancer pool cannot stall shared-head catch-up for all protocols.
			continue
		}
		if (spec.Address == common.Address{}) {
			continue
		}
		binding.PoolIDByAddress[spec.Address] = poolID
		if spec.VaultVersion.IsV3() {
			binding.V3PoolAddresses = append(binding.V3PoolAddresses, spec.Address)
			continue
		}
		binding.V2PoolIDs = append(binding.V2PoolIDs, poolID)
		binding.V2PoolAddresses = append(binding.V2PoolAddresses, spec.Address)
	}
	if binder, ok := parser.(PoolAddressBinder); ok {
		binder.SetPoolAddressMap(binding.PoolIDByAddress)
	}
	return binding, nil
}

func logFilterFromBinding(binding PoolLogBinding, fromBlock, toBlock uint64) LogFilter {
	return LogFilter{
		V2PoolIDs:       binding.V2PoolIDs,
		V2PoolAddresses: binding.V2PoolAddresses,
		V3PoolAddresses: binding.V3PoolAddresses,
		FromBlock:       fromBlock,
		ToBlock:         toBlock,
	}
}

// bindBalancerPoolAddresses is kept for callers that only need pool contract addresses.
func bindBalancerPoolAddresses(
	ctx context.Context,
	registry marketbalancer.PoolRegistry,
	parser EventParser,
	poolIDs []marketbalancer.PoolID,
) ([]common.Address, error) {
	binding, err := bindBalancerPools(ctx, registry, parser, poolIDs)
	if err != nil {
		return nil, err
	}
	addresses := make([]common.Address, 0, len(binding.V2PoolAddresses)+len(binding.V3PoolAddresses))
	addresses = append(addresses, binding.V2PoolAddresses...)
	addresses = append(addresses, binding.V3PoolAddresses...)
	return addresses, nil
}
