package registry

import (
	"context"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

func TestCompositeBalancerRegistryAddVisibleWhenSubgraphDisabled(t *testing.T) {
	id := marketbalancer.PoolID(common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000d1"))
	spec := marketbalancer.PoolSpec{
		Address:      common.HexToAddress("0x00000000000000000000000000000000000000d1"),
		Vault:        common.HexToAddress("0x00000000000000000000000000000000000000d2"),
		Type:         marketbalancer.PoolTypeWeighted,
		VaultVersion: marketbalancer.VaultV2,
	}
	registry, err := NewCompositeBalancerRegistry(config.BalancerSyncConfig{}, common.Address{}, common.Address{})
	if err != nil {
		t.Fatalf("new balancer registry: %v", err)
	}

	if err := registry.Add(context.Background(), id, spec); err != nil {
		t.Fatalf("add pool: %v", err)
	}
	pools, err := registry.List(context.Background())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 1 || pools[0] != id {
		t.Fatalf("expected dynamic balancer pool %s, got %v", id.String(), pools)
	}
	gotSpec, err := registry.GetSpec(context.Background(), id)
	if err != nil {
		t.Fatalf("get spec: %v", err)
	}
	if gotSpec != spec {
		t.Fatalf("expected spec %+v, got %+v", spec, gotSpec)
	}
}
