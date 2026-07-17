package balancersync

import (
	"context"
	"testing"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

func TestBindBalancerPoolsSkipsMissingRegistrySpecs(t *testing.T) {
	knownID := marketbalancer.PoolID(common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000a1"))
	missingID := marketbalancer.PoolID(common.HexToHash("0x0000000000000000000000001ea5870f7c037930ce1d5d8d9317c670e89e13e3"))
	address := common.HexToAddress("0x00000000000000000000000000000000000000a1")

	registry := testBalancerRegistry{
		specs: map[marketbalancer.PoolID]marketbalancer.PoolSpec{
			knownID: {
				Address:      address,
				VaultVersion: marketbalancer.VaultV3,
			},
		},
	}

	binding, err := bindBalancerPools(context.Background(), registry, nil, []marketbalancer.PoolID{knownID, missingID})
	if err != nil {
		t.Fatalf("bind balancer pools: %v", err)
	}
	if got := binding.PoolIDByAddress[address]; got != knownID {
		t.Fatalf("expected known pool binding, got %s", got)
	}
	if len(binding.V3PoolAddresses) != 1 || binding.V3PoolAddresses[0] != address {
		t.Fatalf("unexpected v3 addresses %#v", binding.V3PoolAddresses)
	}
	if len(binding.V2PoolIDs) != 0 {
		t.Fatalf("expected no v2 pools, got %#v", binding.V2PoolIDs)
	}
}
