package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	syncbalancer "github.com/brianliu-sysu/uniswapv3/internal/application/sync/balancer"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// BalancerPoolReader loads on-chain Balancer weighted/stable pool state for bootstrap.
type BalancerPoolReader struct {
	client    *EthClient
	multicall *Multicall
	vaultABI  abi.ABI
	poolABI   abi.ABI
}

func NewBalancerPoolReader(client *EthClient, multicall *Multicall) (*BalancerPoolReader, error) {
	vaultABI, err := abi.JSON(strings.NewReader(balancerVaultReaderABI))
	if err != nil {
		return nil, fmt.Errorf("parse balancer vault reader abi: %w", err)
	}
	poolABI, err := abi.JSON(strings.NewReader(balancerPoolReaderABI))
	if err != nil {
		return nil, fmt.Errorf("parse balancer pool reader abi: %w", err)
	}
	return &BalancerPoolReader{
		client:    client,
		multicall: multicall,
		vaultABI:  vaultABI,
		poolABI:   poolABI,
	}, nil
}

func (r *BalancerPoolReader) ReadBootstrapData(
	ctx context.Context,
	poolID marketbalancer.PoolID,
	spec marketbalancer.PoolSpec,
	blockNumber uint64,
) (*syncbalancer.BootstrapData, error) {
	if blockNumber == 0 {
		header, err := r.client.GetLatestBlockHeader(ctx)
		if err != nil {
			return nil, err
		}
		blockNumber = header.Number
	}
	if err := spec.Type.Validate(); err != nil {
		return nil, err
	}
	if (spec.Address == common.Address{}) {
		return nil, fmt.Errorf("balancer pool address is required")
	}
	if (spec.Vault == common.Address{}) {
		return nil, fmt.Errorf("balancer vault address is required")
	}

	tokens, balances, err := r.readVaultTokens(ctx, poolID, spec.Vault, blockNumber)
	if err != nil {
		return nil, err
	}
	swapFee, err := r.readSwapFee(ctx, spec.Address, blockNumber)
	if err != nil {
		return nil, err
	}

	data := &syncbalancer.BootstrapData{
		Spec:              spec,
		Tokens:            tokens,
		Balances:          balances,
		Weights:           make(map[common.Address]*big.Int),
		Amplification:     big.NewInt(0),
		SwapFeePercentage: swapFee,
		BlockNumber:       blockNumber,
	}

	switch spec.Type {
	case marketbalancer.PoolTypeWeighted:
		weights, err := r.readWeights(ctx, spec.Address, tokens, blockNumber)
		if err != nil {
			return nil, err
		}
		data.Weights = weights
	case marketbalancer.PoolTypeStable:
		amp, err := r.readAmplification(ctx, spec.Address, blockNumber)
		if err != nil {
			return nil, err
		}
		data.Amplification = amp
	}
	return data, nil
}

func (r *BalancerPoolReader) readVaultTokens(
	ctx context.Context,
	poolID marketbalancer.PoolID,
	vault common.Address,
	blockNumber uint64,
) ([]common.Address, map[common.Address]*big.Int, error) {
	data, err := r.vaultABI.Pack("getPoolTokens", poolID.Hash())
	if err != nil {
		return nil, nil, fmt.Errorf("pack getPoolTokens: %w", err)
	}
	output, err := r.client.CallContract(ctx, vault, data, blockNumber)
	if err != nil {
		return nil, nil, fmt.Errorf("call getPoolTokens: %w", err)
	}
	values, err := r.vaultABI.Unpack("getPoolTokens", output)
	if err != nil {
		return nil, nil, fmt.Errorf("unpack getPoolTokens: %w", err)
	}
	if len(values) < 2 {
		return nil, nil, fmt.Errorf("getPoolTokens returned %d values", len(values))
	}
	tokens, ok := values[0].([]common.Address)
	if !ok {
		return nil, nil, fmt.Errorf("getPoolTokens tokens has unexpected type %T", values[0])
	}
	rawBalances, ok := values[1].([]*big.Int)
	if !ok {
		return nil, nil, fmt.Errorf("getPoolTokens balances has unexpected type %T", values[1])
	}
	if len(tokens) != len(rawBalances) {
		return nil, nil, fmt.Errorf("getPoolTokens returned %d tokens and %d balances", len(tokens), len(rawBalances))
	}
	balances := make(map[common.Address]*big.Int, len(tokens))
	for i, token := range tokens {
		balances[token] = new(big.Int).Set(rawBalances[i])
	}
	return tokens, balances, nil
}

func (r *BalancerPoolReader) readSwapFee(ctx context.Context, pool common.Address, blockNumber uint64) (*big.Int, error) {
	data, err := r.poolABI.Pack("getSwapFeePercentage")
	if err != nil {
		return nil, fmt.Errorf("pack getSwapFeePercentage: %w", err)
	}
	output, err := r.client.CallContract(ctx, pool, data, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("call getSwapFeePercentage: %w", err)
	}
	values, err := r.poolABI.Unpack("getSwapFeePercentage", output)
	if err != nil {
		return nil, fmt.Errorf("unpack getSwapFeePercentage: %w", err)
	}
	if len(values) < 1 {
		return nil, fmt.Errorf("getSwapFeePercentage returned %d values", len(values))
	}
	return abiUintToBigInt(values[0])
}

func (r *BalancerPoolReader) readWeights(ctx context.Context, pool common.Address, tokens []common.Address, blockNumber uint64) (map[common.Address]*big.Int, error) {
	data, err := r.poolABI.Pack("getNormalizedWeights")
	if err != nil {
		return nil, fmt.Errorf("pack getNormalizedWeights: %w", err)
	}
	output, err := r.client.CallContract(ctx, pool, data, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("call getNormalizedWeights: %w", err)
	}
	values, err := r.poolABI.Unpack("getNormalizedWeights", output)
	if err != nil {
		return nil, fmt.Errorf("unpack getNormalizedWeights: %w", err)
	}
	if len(values) < 1 {
		return nil, fmt.Errorf("getNormalizedWeights returned %d values", len(values))
	}
	rawWeights, ok := values[0].([]*big.Int)
	if !ok {
		return nil, fmt.Errorf("getNormalizedWeights has unexpected type %T", values[0])
	}
	if len(rawWeights) != len(tokens) {
		return nil, fmt.Errorf("getNormalizedWeights returned %d weights for %d tokens", len(rawWeights), len(tokens))
	}
	weights := make(map[common.Address]*big.Int, len(tokens))
	for i, token := range tokens {
		weights[token] = new(big.Int).Set(rawWeights[i])
	}
	return weights, nil
}

func (r *BalancerPoolReader) readAmplification(ctx context.Context, pool common.Address, blockNumber uint64) (*big.Int, error) {
	data, err := r.poolABI.Pack("getAmplificationParameter")
	if err != nil {
		return nil, fmt.Errorf("pack getAmplificationParameter: %w", err)
	}
	output, err := r.client.CallContract(ctx, pool, data, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("call getAmplificationParameter: %w", err)
	}
	values, err := r.poolABI.Unpack("getAmplificationParameter", output)
	if err != nil {
		return nil, fmt.Errorf("unpack getAmplificationParameter: %w", err)
	}
	if len(values) < 1 {
		return nil, fmt.Errorf("getAmplificationParameter returned %d values", len(values))
	}
	return abiUintToBigInt(values[0])
}

const balancerVaultReaderABI = `[
  {"inputs":[{"name":"poolId","type":"bytes32"}],"name":"getPoolTokens","outputs":[{"name":"tokens","type":"address[]"},{"name":"balances","type":"uint256[]"},{"name":"lastChangeBlock","type":"uint256"}],"stateMutability":"view","type":"function"}
]`

const balancerPoolReaderABI = `[
  {"inputs":[],"name":"getSwapFeePercentage","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[],"name":"getNormalizedWeights","outputs":[{"type":"uint256[]"}],"stateMutability":"view","type":"function"},
  {"inputs":[],"name":"getAmplificationParameter","outputs":[{"name":"value","type":"uint256"},{"name":"isUpdating","type":"bool"},{"name":"precision","type":"uint256"}],"stateMutability":"view","type":"function"}
]`
