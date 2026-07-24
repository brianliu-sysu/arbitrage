package blockchain

import (
	"context"
	"testing"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

func TestBalancerPoolReaderReadBootstrapDataV3(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live balancer v3 bootstrap in short mode")
	}

	client, err := NewEthClient(Config{
		RPCURL:           "https://eth-mainnet.g.alchemy.com/v2/7NCmH4mP28eUd1BkVLMA8",
		MulticallAddress: common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"),
	})
	if err != nil {
		t.Fatalf("new eth client: %v", err)
	}
	defer client.Close()

	multicall, err := NewMulticall(client, common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"))
	if err != nil {
		t.Fatalf("new multicall: %v", err)
	}

	reader, err := NewBalancerPoolReader(client, multicall)
	if err != nil {
		t.Fatalf("new balancer pool reader: %v", err)
	}

	poolAddress := common.HexToAddress("0xd9005569c381d57506baefb69f90d1bb52a023b9")
	spec := marketbalancer.PoolSpec{
		Address:      poolAddress,
		Vault:        common.HexToAddress("0xbA1333333333a1BA1108E8412f11850A5C319bA9"),
		Type:         marketbalancer.PoolTypeStable,
		VaultVersion: marketbalancer.VaultV3,
	}
	poolID := marketbalancer.PoolID(common.HexToHash(poolAddress.Hex()))

	data, err := reader.ReadBootstrapData(context.Background(), poolID, spec, 0)
	if err != nil {
		t.Fatalf("read bootstrap data: %v", err)
	}
	if len(data.Tokens) < 2 {
		t.Fatalf("expected at least 2 tokens, got %d", len(data.Tokens))
	}
	if data.SwapFeePercentage == nil || data.SwapFeePercentage.Sign() <= 0 {
		t.Fatalf("expected positive swap fee, got %v", data.SwapFeePercentage)
	}
	if data.Amplification == nil || data.Amplification.Sign() <= 0 {
		t.Fatalf("expected positive amplification, got %v", data.Amplification)
	}
}

func TestBalancerPoolReaderReadManyBootstrapDataV3(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live balancer v3 batch bootstrap in short mode")
	}

	client, err := NewEthClient(Config{
		RPCURL:           "https://eth-mainnet.g.alchemy.com/v2/7NCmH4mP28eUd1BkVLMA8",
		MulticallAddress: common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"),
	})
	if err != nil {
		t.Fatalf("new eth client: %v", err)
	}
	defer client.Close()

	multicall, err := NewMulticall(client, common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"))
	if err != nil {
		t.Fatalf("new multicall: %v", err)
	}
	reader, err := NewBalancerPoolReader(client, multicall)
	if err != nil {
		t.Fatalf("new balancer pool reader: %v", err)
	}

	vault := common.HexToAddress("0xbA1333333333a1BA1108E8412f11850A5C319bA9")
	stableAddr := common.HexToAddress("0xd9005569c381d57506baefb69f90d1bb52a023b9")
	weightedAddr := common.HexToAddress("0x01b3f3aabff34e266a98e771438320df98d447dd")
	inputs := []BalancerBootstrapInput{
		{
			PoolID: marketbalancer.PoolID(common.HexToHash(stableAddr.Hex())),
			Spec: marketbalancer.PoolSpec{
				Address: stableAddr, Vault: vault,
				Type: marketbalancer.PoolTypeStable, VaultVersion: marketbalancer.VaultV3,
			},
		},
		{
			PoolID: marketbalancer.PoolID(common.HexToHash(weightedAddr.Hex())),
			Spec: marketbalancer.PoolSpec{
				Address: weightedAddr, Vault: vault,
				Type: marketbalancer.PoolTypeWeighted, VaultVersion: marketbalancer.VaultV3,
			},
		},
	}

	results, err := reader.ReadManyBootstrapData(context.Background(), inputs, 0)
	if err != nil {
		t.Fatalf("read many bootstrap data: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 pools, got %d", len(results))
	}
	for _, input := range inputs {
		data, ok := results[input.PoolID]
		if !ok || data == nil {
			t.Fatalf("missing bootstrap data for %s", input.PoolID)
		}
		if len(data.Tokens) < 2 {
			t.Fatalf("pool %s expected at least 2 tokens", input.PoolID)
		}
	}
}
