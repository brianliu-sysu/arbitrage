package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const balancerBootstrapCallsPerPool = 3

// BalancerBootstrapInput identifies a pool to read during batched bootstrap.
type BalancerBootstrapInput = marketbalancer.BootstrapInput

// BalancerPoolReader loads on-chain Balancer weighted/stable pool state for bootstrap.
type BalancerPoolReader struct {
	client     *EthClient
	multicall  *Multicall
	vaultV2ABI abi.ABI
	vaultV3ABI abi.ABI
	poolABI    abi.ABI
	poolV3ABI  abi.ABI
}

func NewBalancerPoolReader(client *EthClient, multicall *Multicall) (*BalancerPoolReader, error) {
	vaultV2ABI, err := abi.JSON(strings.NewReader(balancerVaultV2ReaderABI))
	if err != nil {
		return nil, fmt.Errorf("parse balancer vault v2 reader abi: %w", err)
	}
	vaultV3ABI, err := abi.JSON(strings.NewReader(balancerVaultV3ReaderABI))
	if err != nil {
		return nil, fmt.Errorf("parse balancer vault v3 reader abi: %w", err)
	}
	poolABI, err := abi.JSON(strings.NewReader(balancerPoolReaderABI))
	if err != nil {
		return nil, fmt.Errorf("parse balancer pool reader abi: %w", err)
	}
	poolV3ABI, err := abi.JSON(strings.NewReader(balancerPoolV3ReaderABI))
	if err != nil {
		return nil, fmt.Errorf("parse balancer pool v3 reader abi: %w", err)
	}
	return &BalancerPoolReader{
		client:     client,
		multicall:  multicall,
		vaultV2ABI: vaultV2ABI,
		vaultV3ABI: vaultV3ABI,
		poolABI:    poolABI,
		poolV3ABI:  poolV3ABI,
	}, nil
}

func (r *BalancerPoolReader) ReadBootstrapData(
	ctx context.Context,
	poolID marketbalancer.PoolID,
	spec marketbalancer.PoolSpec,
	blockNumber uint64,
) (*marketbalancer.BootstrapData, error) {
	if r.multicall != nil {
		results, err := r.ReadManyBootstrapData(ctx, []BalancerBootstrapInput{{PoolID: poolID, Spec: spec}}, blockNumber)
		if err != nil {
			return nil, err
		}
		data, ok := results[poolID]
		if !ok || data == nil {
			return nil, fmt.Errorf("read bootstrap data for pool %s", poolID)
		}
		return data, nil
	}
	return r.readBootstrapDataDirect(ctx, poolID, spec, blockNumber)
}

// ReadManyBootstrapData loads bootstrap state for many pools using batched multicall requests.
func (r *BalancerPoolReader) ReadManyBootstrapData(
	ctx context.Context,
	inputs []BalancerBootstrapInput,
	blockNumber uint64,
) (map[marketbalancer.PoolID]*marketbalancer.BootstrapData, error) {
	if len(inputs) == 0 {
		return map[marketbalancer.PoolID]*marketbalancer.BootstrapData{}, nil
	}
	if r.multicall == nil {
		out := make(map[marketbalancer.PoolID]*marketbalancer.BootstrapData, len(inputs))
		for _, input := range inputs {
			data, err := r.readBootstrapDataDirect(ctx, input.PoolID, input.Spec, blockNumber)
			if err != nil {
				return nil, err
			}
			out[input.PoolID] = data
		}
		return out, nil
	}

	blockNumber, err := r.client.ResolveBlockNumber(ctx, blockNumber)
	if err != nil {
		return nil, err
	}

	swapFeeV3Data, err := r.poolV3ABI.Pack("getStaticSwapFeePercentage")
	if err != nil {
		return nil, fmt.Errorf("pack getStaticSwapFeePercentage: %w", err)
	}
	swapFeeV2Data, err := r.poolABI.Pack("getSwapFeePercentage")
	if err != nil {
		return nil, fmt.Errorf("pack getSwapFeePercentage: %w", err)
	}
	weightsData, err := r.poolABI.Pack("getNormalizedWeights")
	if err != nil {
		return nil, fmt.Errorf("pack getNormalizedWeights: %w", err)
	}
	ampData, err := r.poolABI.Pack("getAmplificationParameter")
	if err != nil {
		return nil, fmt.Errorf("pack getAmplificationParameter: %w", err)
	}

	requests := make([]MulticallRequest, 0, len(inputs)*balancerBootstrapCallsPerPool)
	for _, input := range inputs {
		if err := validateBootstrapInput(input); err != nil {
			return nil, err
		}
		if input.Spec.VaultVersion.IsV3() {
			vaultData, err := r.vaultV3ABI.Pack("getPoolTokenInfo", input.Spec.Address)
			if err != nil {
				return nil, fmt.Errorf("pack getPoolTokenInfo: %w", err)
			}
			requests = append(requests, MulticallRequest{Target: input.Spec.Vault, Data: vaultData})
			requests = append(requests, MulticallRequest{Target: input.Spec.Address, Data: swapFeeV3Data})
		} else {
			vaultData, err := r.vaultV2ABI.Pack("getPoolTokens", input.PoolID.Hash())
			if err != nil {
				return nil, fmt.Errorf("pack getPoolTokens: %w", err)
			}
			requests = append(requests, MulticallRequest{Target: input.Spec.Vault, Data: vaultData})
			requests = append(requests, MulticallRequest{Target: input.Spec.Address, Data: swapFeeV2Data})
		}
		switch input.Spec.Type {
		case marketbalancer.PoolTypeWeighted:
			requests = append(requests, MulticallRequest{Target: input.Spec.Address, Data: weightsData})
		case marketbalancer.PoolTypeStable:
			requests = append(requests, MulticallRequest{Target: input.Spec.Address, Data: ampData})
		default:
			return nil, fmt.Errorf("unsupported balancer pool type %q", input.Spec.Type)
		}
	}

	results, err := r.multicall.Aggregate3WithDirectFallback(ctx, requests, blockNumber)
	if err != nil {
		return nil, err
	}
	if len(results) != len(requests) {
		return nil, fmt.Errorf("expected %d multicall results, got %d", len(requests), len(results))
	}

	out := make(map[marketbalancer.PoolID]*marketbalancer.BootstrapData, len(inputs))
	for i, input := range inputs {
		base := i * balancerBootstrapCallsPerPool
		tokens, balances, err := r.unpackVaultTokens(ctx, input, requests[base], results[base], blockNumber)
		if err != nil {
			return nil, fmt.Errorf("pool %s: %w", input.PoolID, err)
		}
		swapFee, err := r.unpackSwapFee(ctx, input.Spec, requests[base+1], results[base+1], blockNumber)
		if err != nil {
			return nil, fmt.Errorf("pool %s: %w", input.PoolID, err)
		}

		data := &marketbalancer.BootstrapData{
			Spec:              input.Spec,
			Tokens:            tokens,
			Balances:          balances,
			Weights:           make(map[common.Address]*big.Int),
			Amplification:     big.NewInt(0),
			SwapFeePercentage: swapFee,
			BlockNumber:       blockNumber,
		}
		switch input.Spec.Type {
		case marketbalancer.PoolTypeWeighted:
			weights, err := r.unpackWeights(ctx, tokens, requests[base+2], results[base+2], blockNumber)
			if err != nil {
				return nil, fmt.Errorf("pool %s: %w", input.PoolID, err)
			}
			data.Weights = weights
		case marketbalancer.PoolTypeStable:
			amp, err := r.unpackAmplification(ctx, requests[base+2], results[base+2], blockNumber)
			if err != nil {
				return nil, fmt.Errorf("pool %s: %w", input.PoolID, err)
			}
			data.Amplification = amp
		}
		out[input.PoolID] = data
	}
	return out, nil
}

func validateBootstrapInput(input BalancerBootstrapInput) error {
	if err := input.Spec.Type.Validate(); err != nil {
		return err
	}
	if (input.Spec.Address == common.Address{}) {
		return fmt.Errorf("balancer pool address is required")
	}
	if (input.Spec.Vault == common.Address{}) {
		return fmt.Errorf("balancer vault address is required")
	}
	return nil
}

func (r *BalancerPoolReader) readBootstrapDataDirect(
	ctx context.Context,
	poolID marketbalancer.PoolID,
	spec marketbalancer.PoolSpec,
	blockNumber uint64,
) (*marketbalancer.BootstrapData, error) {
	blockNumber, err := r.client.ResolveBlockNumber(ctx, blockNumber)
	if err != nil {
		return nil, err
	}
	if err := validateBootstrapInput(BalancerBootstrapInput{PoolID: poolID, Spec: spec}); err != nil {
		return nil, err
	}

	var (
		tokens   []common.Address
		balances map[common.Address]*big.Int
	)
	if spec.VaultVersion.IsV3() {
		tokens, balances, err = r.readVaultV3Tokens(ctx, spec.Address, spec.Vault, blockNumber)
	} else {
		tokens, balances, err = r.readVaultV2Tokens(ctx, poolID, spec.Vault, blockNumber)
	}
	if err != nil {
		return nil, err
	}
	swapFee, err := r.readSwapFee(ctx, spec.Address, spec.VaultVersion, blockNumber)
	if err != nil {
		return nil, err
	}

	data := &marketbalancer.BootstrapData{
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

func (r *BalancerPoolReader) unpackVaultTokens(
	ctx context.Context,
	input BalancerBootstrapInput,
	request MulticallRequest,
	result MulticallResult,
	blockNumber uint64,
) ([]common.Address, map[common.Address]*big.Int, error) {
	returnData, err := multicallReturnDataOrDirect(ctx, r.client, request.Target, request.Data, blockNumber, result)
	if err != nil {
		return nil, nil, err
	}
	if input.Spec.VaultVersion.IsV3() {
		return unpackVaultV3Tokens(r.vaultV3ABI, returnData)
	}
	return unpackVaultV2Tokens(r.vaultV2ABI, returnData)
}

func (r *BalancerPoolReader) unpackSwapFee(
	ctx context.Context,
	spec marketbalancer.PoolSpec,
	request MulticallRequest,
	result MulticallResult,
	blockNumber uint64,
) (*big.Int, error) {
	if spec.VaultVersion.IsV3() {
		fee, err := r.unpackPoolUint(ctx, r.poolV3ABI, "getStaticSwapFeePercentage", request, result, blockNumber)
		if err == nil {
			return fee, nil
		}
	}
	return r.unpackPoolUint(ctx, r.poolABI, "getSwapFeePercentage", request, result, blockNumber)
}

func (r *BalancerPoolReader) unpackWeights(
	ctx context.Context,
	tokens []common.Address,
	request MulticallRequest,
	result MulticallResult,
	blockNumber uint64,
) (map[common.Address]*big.Int, error) {
	returnData, err := multicallReturnDataOrDirect(ctx, r.client, request.Target, request.Data, blockNumber, result)
	if err != nil {
		return nil, err
	}
	values, err := r.poolABI.Unpack("getNormalizedWeights", returnData)
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

func (r *BalancerPoolReader) unpackAmplification(
	ctx context.Context,
	request MulticallRequest,
	result MulticallResult,
	blockNumber uint64,
) (*big.Int, error) {
	return r.unpackPoolUint(ctx, r.poolABI, "getAmplificationParameter", request, result, blockNumber)
}

func (r *BalancerPoolReader) unpackPoolUint(
	ctx context.Context,
	contractABI abi.ABI,
	method string,
	request MulticallRequest,
	result MulticallResult,
	blockNumber uint64,
) (*big.Int, error) {
	returnData, err := multicallReturnDataOrDirect(ctx, r.client, request.Target, request.Data, blockNumber, result)
	if err != nil {
		return nil, err
	}
	values, err := contractABI.Unpack(method, returnData)
	if err != nil {
		return nil, fmt.Errorf("unpack %s: %w", method, err)
	}
	if len(values) < 1 {
		return nil, fmt.Errorf("%s returned %d values", method, len(values))
	}
	return abiUintToBigInt(values[0])
}

func unpackVaultV2Tokens(vaultABI abi.ABI, returnData []byte) ([]common.Address, map[common.Address]*big.Int, error) {
	values, err := vaultABI.Unpack("getPoolTokens", returnData)
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

func unpackVaultV3Tokens(vaultABI abi.ABI, returnData []byte) ([]common.Address, map[common.Address]*big.Int, error) {
	values, err := vaultABI.Unpack("getPoolTokenInfo", returnData)
	if err != nil {
		return nil, nil, fmt.Errorf("unpack getPoolTokenInfo: %w", err)
	}
	if len(values) < 3 {
		return nil, nil, fmt.Errorf("getPoolTokenInfo returned %d values", len(values))
	}
	tokens, ok := values[0].([]common.Address)
	if !ok {
		return nil, nil, fmt.Errorf("getPoolTokenInfo tokens has unexpected type %T", values[0])
	}
	rawBalances, ok := values[2].([]*big.Int)
	if !ok {
		return nil, nil, fmt.Errorf("getPoolTokenInfo balancesRaw has unexpected type %T", values[2])
	}
	if len(tokens) != len(rawBalances) {
		return nil, nil, fmt.Errorf("getPoolTokenInfo returned %d tokens and %d balances", len(tokens), len(rawBalances))
	}
	balances := make(map[common.Address]*big.Int, len(tokens))
	for i, token := range tokens {
		balances[token] = new(big.Int).Set(rawBalances[i])
	}
	return tokens, balances, nil
}

func (r *BalancerPoolReader) readVaultV2Tokens(
	ctx context.Context,
	poolID marketbalancer.PoolID,
	vault common.Address,
	blockNumber uint64,
) ([]common.Address, map[common.Address]*big.Int, error) {
	data, err := r.vaultV2ABI.Pack("getPoolTokens", poolID.Hash())
	if err != nil {
		return nil, nil, fmt.Errorf("pack getPoolTokens: %w", err)
	}
	output, err := r.client.CallContract(ctx, vault, data, blockNumber)
	if err != nil {
		return nil, nil, fmt.Errorf("call getPoolTokens: %w", err)
	}
	return unpackVaultV2Tokens(r.vaultV2ABI, output)
}

func (r *BalancerPoolReader) readVaultV3Tokens(
	ctx context.Context,
	pool common.Address,
	vault common.Address,
	blockNumber uint64,
) ([]common.Address, map[common.Address]*big.Int, error) {
	data, err := r.vaultV3ABI.Pack("getPoolTokenInfo", pool)
	if err != nil {
		return nil, nil, fmt.Errorf("pack getPoolTokenInfo: %w", err)
	}
	output, err := r.client.CallContract(ctx, vault, data, blockNumber)
	if err != nil {
		return nil, nil, fmt.Errorf("call getPoolTokenInfo: %w", err)
	}
	return unpackVaultV3Tokens(r.vaultV3ABI, output)
}

func (r *BalancerPoolReader) readSwapFee(ctx context.Context, pool common.Address, version marketbalancer.VaultVersion, blockNumber uint64) (*big.Int, error) {
	if version.IsV3() {
		fee, err := r.callPoolUint(ctx, pool, r.poolV3ABI, "getStaticSwapFeePercentage", blockNumber)
		if err == nil {
			return fee, nil
		}
	}
	return r.callPoolUint(ctx, pool, r.poolABI, "getSwapFeePercentage", blockNumber)
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
	request := MulticallRequest{Target: pool, Data: data}
	result := MulticallResult{Success: true, ReturnData: output}
	return r.unpackWeights(ctx, tokens, request, result, blockNumber)
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
	request := MulticallRequest{Target: pool, Data: data}
	result := MulticallResult{Success: true, ReturnData: output}
	return r.unpackAmplification(ctx, request, result, blockNumber)
}

func (r *BalancerPoolReader) callPoolUint(ctx context.Context, pool common.Address, contractABI abi.ABI, method string, blockNumber uint64) (*big.Int, error) {
	data, err := contractABI.Pack(method)
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", method, err)
	}
	output, err := r.client.CallContract(ctx, pool, data, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", method, err)
	}
	values, err := contractABI.Unpack(method, output)
	if err != nil {
		return nil, fmt.Errorf("unpack %s: %w", method, err)
	}
	if len(values) < 1 {
		return nil, fmt.Errorf("%s returned %d values", method, len(values))
	}
	return abiUintToBigInt(values[0])
}

const balancerVaultV2ReaderABI = `[
  {"inputs":[{"name":"poolId","type":"bytes32"}],"name":"getPoolTokens","outputs":[{"name":"tokens","type":"address[]"},{"name":"balances","type":"uint256[]"},{"name":"lastChangeBlock","type":"uint256"}],"stateMutability":"view","type":"function"}
]`

const balancerVaultV3ReaderABI = `[
  {"inputs":[{"name":"pool","type":"address"}],"name":"getPoolTokenInfo","outputs":[{"name":"tokens","type":"address[]"},{"name":"tokenInfo","type":"tuple[]","components":[{"name":"tokenType","type":"uint8"},{"name":"rateProvider","type":"address"},{"name":"paysYieldFees","type":"bool"}]},{"name":"balancesRaw","type":"uint256[]"},{"name":"lastBalancesLiveScaled18","type":"uint256[]"}],"stateMutability":"view","type":"function"}
]`

const balancerPoolReaderABI = `[
  {"inputs":[],"name":"getSwapFeePercentage","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[],"name":"getNormalizedWeights","outputs":[{"type":"uint256[]"}],"stateMutability":"view","type":"function"},
  {"inputs":[],"name":"getAmplificationParameter","outputs":[{"name":"value","type":"uint256"},{"name":"isUpdating","type":"bool"},{"name":"precision","type":"uint256"}],"stateMutability":"view","type":"function"}
]`

const balancerPoolV3ReaderABI = `[
  {"inputs":[],"name":"getStaticSwapFeePercentage","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"}
]`
