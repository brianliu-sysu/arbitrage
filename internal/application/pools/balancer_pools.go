package poolsapp

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

type BalancerAdapter struct {
	pools    marketbalancer.PoolRepository
	registry marketbalancer.PoolRegistry
	reader   BalancerStateReader
}

func NewBalancerAdapter(
	pools marketbalancer.PoolRepository,
	registry marketbalancer.PoolRegistry,
	reader BalancerStateReader,
) *BalancerAdapter {
	return &BalancerAdapter{pools: pools, registry: registry, reader: reader}
}

func (a *BalancerAdapter) Type() string { return PoolTypeBalancer }

func (a *BalancerAdapter) List(ctx context.Context) ([]PoolInfo, error) {
	pools := make([]PoolInfo, 0)
	if a.registry == nil || a.pools == nil {
		return pools, nil
	}

	poolIDs, err := a.registry.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list balancer pools: %w", err)
	}
	for _, poolID := range poolIDs {
		pool, err := a.pools.Get(ctx, poolID)
		if err != nil {
			return nil, fmt.Errorf("load balancer pool %s: %w", poolID.String(), err)
		}
		if pool == nil {
			continue
		}
		tokens := make([]TokenInfo, 0, len(pool.Tokens))
		for _, token := range pool.Tokens {
			tokens = append(tokens, tokenInfoFromAddress(token))
		}
		info := PoolInfo{
			PoolID: poolID.String(), PoolAddress: pool.Address.Hex(),
			PoolType: PoolTypeBalancer, BalancerType: string(pool.Type), Tokens: tokens,
		}
		if len(tokens) > 0 {
			info.Token0 = tokens[0]
		}
		if len(tokens) > 1 {
			info.Token1 = tokens[1]
		}
		pools = append(pools, info)
	}
	return pools, nil
}

func (a *BalancerAdapter) Diagnostics(ctx context.Context, req DiagnosticsRequest, head uint64, resolve TokenMetadataResolver) (*DiagnosticsResponse, error) {
	return a.diagnostics(ctx, req.BalancerPoolID, head, resolve)
}

func (a *BalancerAdapter) AppendMismatches(ctx context.Context, head uint64, resolve TokenMetadataResolver, items *[]DiagnosticsResponse) error {
	return a.appendMismatches(ctx, head, resolve, items)
}

func balancerTokenAddresses(pools []PoolInfo) []common.Address {
	addresses := make([]common.Address, 0)
	for _, pool := range pools {
		if pool.PoolType != PoolTypeBalancer {
			continue
		}
		for _, token := range pool.Tokens {
			if token.Address != "" {
				addresses = append(addresses, common.HexToAddress(token.Address))
			}
		}
	}
	return addresses
}

func (a *BalancerAdapter) diagnostics(ctx context.Context, poolID marketbalancer.PoolID, head uint64, resolve TokenMetadataResolver) (*DiagnosticsResponse, error) {
	if poolID == (marketbalancer.PoolID{}) {
		return nil, fmt.Errorf("poolId is required for balancer diagnostics")
	}
	if a.pools == nil {
		return nil, fmt.Errorf("balancer pool repository is not configured")
	}
	if a.registry == nil {
		return nil, fmt.Errorf("balancer pool registry is not configured")
	}
	if a.reader == nil {
		return nil, fmt.Errorf("balancer chain reader is not configured")
	}

	pool, err := a.pools.Get(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("load balancer pool: %w", err)
	}
	if pool == nil {
		return nil, fmt.Errorf("balancer pool %s not found", poolID.String())
	}
	spec, err := a.registry.GetSpec(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("load balancer pool spec: %w", err)
	}

	chainBlock := head
	if pool.LastBlockNumber > 0 && pool.LastBlockNumber <= head {
		chainBlock = pool.LastBlockNumber
	}
	chainState, err := a.reader.ReadBalancerState(ctx, poolID, spec, chainBlock)
	if err != nil {
		return nil, fmt.Errorf("read chain balancer state: %w", err)
	}
	if chainState == nil {
		return nil, fmt.Errorf("read chain balancer state: empty response for pool %s", poolID.String())
	}

	tokenMeta, err := resolve(ctx, pool.Tokens...)
	if err != nil {
		return nil, err
	}
	tokenInfos := enrichBalancerTokenSlice(pool.Tokens, tokenMeta)
	return buildBalancerDiagnosticsResponse(poolID, pool, tokenInfos, tokenMeta, head, chainState), nil
}

func (a *BalancerAdapter) appendMismatches(ctx context.Context, head uint64, resolve TokenMetadataResolver, items *[]DiagnosticsResponse) error {
	if a.registry == nil || a.pools == nil || a.reader == nil {
		return nil
	}
	poolIDs, err := a.registry.List(ctx)
	if err != nil {
		return fmt.Errorf("list balancer pools: %w", err)
	}

	type localPool struct {
		id   marketbalancer.PoolID
		spec marketbalancer.PoolSpec
		pool *marketbalancer.Pool
	}
	locals := make([]localPool, 0, len(poolIDs))
	inputs := make([]marketbalancer.BootstrapInput, 0, len(poolIDs))
	tokenAddresses := make([]common.Address, 0)

	for _, poolID := range poolIDs {
		pool, err := a.pools.Get(ctx, poolID)
		if err != nil || pool == nil {
			continue
		}
		spec, err := a.registry.GetSpec(ctx, poolID)
		if err != nil {
			return fmt.Errorf("load balancer pool spec %s: %w", poolID.String(), err)
		}
		locals = append(locals, localPool{id: poolID, spec: spec, pool: pool})
		if pool.LastBlockNumber != head {
			continue
		}
		inputs = append(inputs, marketbalancer.BootstrapInput{PoolID: poolID, Spec: spec})
		tokenAddresses = append(tokenAddresses, pool.Tokens...)
	}

	chainStates, err := readManyBalancerStates(ctx, a.reader, inputs, head)
	if err != nil {
		return fmt.Errorf("read balancer chain states: %w", err)
	}

	tokenMeta, err := resolve(ctx, tokenAddresses...)
	if err != nil {
		return err
	}
	for _, local := range locals {
		if local.pool.LastBlockNumber != head {
			continue
		}
		chainState := chainStates[local.id]
		if chainState == nil {
			continue
		}
		tokenInfos := enrichBalancerTokenSlice(local.pool.Tokens, tokenMeta)
		resp := buildBalancerDiagnosticsResponse(local.id, local.pool, tokenInfos, tokenMeta, head, chainState)
		if isMismatchingAtHead(resp) {
			*items = append(*items, *resp)
		}
	}
	return nil
}

func buildBalancerDiagnosticsResponse(
	poolID marketbalancer.PoolID,
	pool *marketbalancer.Pool,
	tokenInfos []TokenInfo,
	tokenMeta map[common.Address]*asset.Token,
	head uint64,
	chainState *marketbalancer.BootstrapData,
) *DiagnosticsResponse {
	local := snapshotFromBalancerState(tokenInfos, pool.Balances, pool.Weights, pool.Amplification, pool.SwapFeePercentage, pool.LastBlockNumber, string(pool.Status))
	chainTokens := enrichBalancerTokenSlice(chainState.Tokens, tokenMeta)
	chain := snapshotFromBalancerState(chainTokens, chainState.Balances, chainState.Weights, chainState.Amplification, chainState.SwapFeePercentage, chainState.BlockNumber, "")
	local.BlockLag = blockLag(head, pool.LastBlockNumber)

	resp := &DiagnosticsResponse{
		PoolType:     PoolTypeBalancer,
		PoolID:       poolID.String(),
		PoolAddress:  pool.Address.Hex(),
		BalancerType: string(pool.Type),
		Tokens:       tokenInfos,
		ChainHead:    head,
		Local:        local,
		Chain:        chain,
		Diff:         StateDiff{SqrtPriceX96Match: true, TickMatch: true, LiquidityMatch: true},
		BalancerDiff: diffBalancerSnapshots(local, chain),
	}
	if len(tokenInfos) > 0 {
		resp.Token0 = tokenInfos[0]
	}
	if len(tokenInfos) > 1 {
		resp.Token1 = tokenInfos[1]
	}
	return resp
}

func enrichBalancerTokenSlice(tokens []common.Address, tokenMeta map[common.Address]*asset.Token) []TokenInfo {
	out := make([]TokenInfo, 0, len(tokens))
	for _, token := range tokens {
		out = append(out, enrichToken(tokenInfoFromAddress(token), tokenMeta))
	}
	return out
}

func snapshotFromBalancerState(
	tokens []TokenInfo,
	balances map[common.Address]*big.Int,
	weights map[common.Address]*big.Int,
	amplification *big.Int,
	swapFeePercentage *big.Int,
	blockNumber uint64,
	status string,
) StateSnapshot {
	snap := StateSnapshot{
		LastBlockNumber: blockNumber,
		Tokens:          tokens,
		Balances:        stringifyTokenIntMap(balances),
		Weights:         stringifyTokenIntMap(weights),
	}
	if amplification != nil {
		snap.Amplification = amplification.String()
	}
	if swapFeePercentage != nil {
		snap.SwapFeePercentage = swapFeePercentage.String()
	}
	if status != "" {
		snap.Status = status
	}
	if blockNumber > 0 {
		snap.BlockNumber = blockNumber
	}
	return snap
}

func diffBalancerSnapshots(local, chain StateSnapshot) *BalancerStateDiff {
	return &BalancerStateDiff{
		TokensMatch:            tokenInfoAddresses(local.Tokens) == tokenInfoAddresses(chain.Tokens),
		BalancesMatch:          stringMapEqual(local.Balances, chain.Balances),
		WeightsMatch:           stringMapEqual(local.Weights, chain.Weights),
		AmplificationMatch:     local.Amplification == chain.Amplification,
		SwapFeePercentageMatch: local.SwapFeePercentage == chain.SwapFeePercentage,
	}
}

func readManyBalancerStates(
	ctx context.Context,
	reader BalancerStateReader,
	inputs []marketbalancer.BootstrapInput,
	blockNumber uint64,
) (map[marketbalancer.PoolID]*marketbalancer.BootstrapData, error) {
	if len(inputs) == 0 {
		return map[marketbalancer.PoolID]*marketbalancer.BootstrapData{}, nil
	}
	if batch, ok := reader.(BalancerStateBatchReader); ok {
		return batch.ReadManyBalancerStates(ctx, inputs, blockNumber)
	}

	out := make(map[marketbalancer.PoolID]*marketbalancer.BootstrapData, len(inputs))
	for _, input := range inputs {
		state, err := reader.ReadBalancerState(ctx, input.PoolID, input.Spec, blockNumber)
		if err != nil {
			continue
		}
		out[input.PoolID] = state
	}
	return out, nil
}

func stringifyTokenIntMap(values map[common.Address]*big.Int) map[string]string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]common.Address, 0, len(values))
	for token := range values {
		keys = append(keys, token)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].Hex() < keys[j].Hex()
	})
	out := make(map[string]string, len(values))
	for _, token := range keys {
		value := values[token]
		if value == nil {
			out[token.Hex()] = ""
			continue
		}
		out[token.Hex()] = value.String()
	}
	return out
}

func tokenInfoAddresses(tokens []TokenInfo) string {
	if len(tokens) == 0 {
		return ""
	}
	addresses := make([]string, 0, len(tokens))
	for _, token := range tokens {
		addresses = append(addresses, common.HexToAddress(token.Address).Hex())
	}
	return strings.Join(addresses, ",")
}

func stringMapEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftValue := range left {
		if right[key] != leftValue {
			return false
		}
	}
	return true
}
