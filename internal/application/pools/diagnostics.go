package poolsapp

import (
	"context"
	"fmt"
	"math/big"
	"sort"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// DiagnosticsRequest selects a tracked pool to inspect.
type DiagnosticsRequest struct {
	PoolType       string
	PoolAddress    common.Address
	PoolID         marketuniv4.PoolID
	BalancerPoolID marketbalancer.PoolID
}

// StateSnapshot summarizes pool base state for diagnostics.
type StateSnapshot struct {
	BlockNumber       uint64            `json:"blockNumber,omitempty"`
	LastBlockNumber   uint64            `json:"lastBlockNumber,omitempty"`
	BlockLag          uint64            `json:"blockLag,omitempty"`
	Status            string            `json:"status,omitempty"`
	SqrtPriceX96      string            `json:"sqrtPriceX96,omitempty"`
	Tick              int32             `json:"tick,omitempty"`
	Liquidity         string            `json:"liquidity,omitempty"`
	Price             PriceInfo         `json:"price,omitempty"`
	Tokens            []TokenInfo       `json:"tokens,omitempty"`
	Balances          map[string]string `json:"balances,omitempty"`
	Weights           map[string]string `json:"weights,omitempty"`
	Amplification     string            `json:"amplification,omitempty"`
	SwapFeePercentage string            `json:"swapFeePercentage,omitempty"`
}

// StateDiff compares local and chain snapshots.
type StateDiff struct {
	SqrtPriceX96Match bool `json:"sqrtPriceX96Match"`
	TickMatch         bool `json:"tickMatch"`
	LiquidityMatch    bool `json:"liquidityMatch"`
}

// BalancerStateDiff compares Balancer pool state fields.
type BalancerStateDiff struct {
	TokensMatch            bool `json:"tokensMatch"`
	BalancesMatch          bool `json:"balancesMatch"`
	WeightsMatch           bool `json:"weightsMatch"`
	AmplificationMatch     bool `json:"amplificationMatch"`
	SwapFeePercentageMatch bool `json:"swapFeePercentageMatch"`
}

// DiagnosticsResponse compares synced pool state against on-chain data.
type DiagnosticsResponse struct {
	PoolType     string             `json:"poolType"`
	PoolAddress  string             `json:"poolAddress,omitempty"`
	PoolID       string             `json:"poolId,omitempty"`
	BalancerType string             `json:"balancerType,omitempty"`
	Token0       TokenInfo          `json:"token0"`
	Token1       TokenInfo          `json:"token1"`
	Tokens       []TokenInfo        `json:"tokens,omitempty"`
	Fee          uint32             `json:"fee"`
	ChainHead    uint64             `json:"chainHeadBlock"`
	Local        StateSnapshot      `json:"local"`
	Chain        StateSnapshot      `json:"chain"`
	Diff         StateDiff          `json:"diff"`
	BalancerDiff *BalancerStateDiff `json:"balancerDiff,omitempty"`
}

// DiagnosticsListResponse lists pools whose local state does not match chain state.
type DiagnosticsListResponse struct {
	ChainHead uint64                `json:"chainHeadBlock"`
	Count     int                   `json:"count"`
	Items     []DiagnosticsResponse `json:"items"`
}

// Diagnostics compares local pool state with on-chain base state.
func (s *AppService) Diagnostics(ctx context.Context, req DiagnosticsRequest) (*DiagnosticsResponse, error) {
	if s == nil {
		return nil, fmt.Errorf("pools service is nil")
	}
	if s.chain == nil || s.chain.Head == nil {
		return nil, fmt.Errorf("chain readers are not configured")
	}

	head, err := s.chain.Head.LatestBlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("latest block: %w", err)
	}

	switch req.PoolType {
	case PoolTypeUniv4:
		return s.diagnosticsV4(ctx, req.PoolID, head)
	case PoolTypeUniv3:
		return s.diagnosticsCLV3(ctx, PoolTypeUniv3, "univ3", req.PoolAddress, head, univ3CLV3Pools(s), s.chain.V3)
	case PoolTypePancakeV3:
		return s.diagnosticsCLV3(ctx, PoolTypePancakeV3, "pancakev3", req.PoolAddress, head, pancakeCLV3Pools(s), s.chain.Pancake)
	case PoolTypeBalancer:
		return s.diagnosticsBalancer(ctx, req.BalancerPoolID, head)
	default:
		return nil, fmt.Errorf("unsupported poolType %q", req.PoolType)
	}
}

// DiagnosticsAll returns tracked pools at chain head whose local slot0 state differs from chain.
func (s *AppService) DiagnosticsAll(ctx context.Context) (*DiagnosticsListResponse, error) {
	if s == nil {
		return nil, fmt.Errorf("pools service is nil")
	}
	if s.chain == nil || s.chain.Head == nil {
		return nil, fmt.Errorf("chain readers are not configured")
	}

	head, err := s.chain.Head.LatestBlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("latest block: %w", err)
	}

	items := make([]DiagnosticsResponse, 0)
	if err := appendMismatchingCLV3Pools(ctx, s, univ3CLV3Source(s), head, &items); err != nil {
		return nil, err
	}
	if err := appendMismatchingCLV3Pools(ctx, s, pancakeCLV3Source(s), head, &items); err != nil {
		return nil, err
	}
	if err := s.appendMismatchingV4(ctx, head, &items); err != nil {
		return nil, err
	}
	if err := s.appendMismatchingBalancer(ctx, head, &items); err != nil {
		return nil, err
	}

	sortDiagnostics(items)
	return &DiagnosticsListResponse{
		ChainHead: head,
		Count:     len(items),
		Items:     items,
	}, nil
}

func (s *AppService) appendMismatchingV4(ctx context.Context, head uint64, items *[]DiagnosticsResponse) error {
	if s.v4Registry == nil || s.v4Pools == nil || s.chain.V4 == nil {
		return nil
	}
	poolIDs, err := s.v4Registry.List(ctx)
	if err != nil {
		return fmt.Errorf("list univ4 pools: %w", err)
	}

	type localPool struct {
		id   marketuniv4.PoolID
		pool *marketuniv4.Pool
	}
	locals := make([]localPool, 0, len(poolIDs))
	atHead := make([]marketuniv4.PoolID, 0, len(poolIDs))
	tokenAddresses := make([]common.Address, 0, len(poolIDs)*2)

	for _, poolID := range poolIDs {
		pool, err := s.v4Pools.Get(ctx, poolID)
		if err != nil || pool == nil {
			continue
		}
		locals = append(locals, localPool{id: poolID, pool: pool})
		if pool.LastBlockNumber != head {
			continue
		}
		atHead = append(atHead, poolID)
		tokenAddresses = append(tokenAddresses, pool.Key.Currency0, pool.Key.Currency1)
	}

	chainStates, err := readManyV4BaseStates(ctx, s.chain.V4, atHead, head)
	if err != nil {
		return fmt.Errorf("read univ4 chain states: %w", err)
	}

	tokenMeta, err := s.resolveTokenMetadata(ctx, tokenAddresses...)
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
		token0 := enrichToken(tokenInfoFromAddress(local.pool.Key.Currency0), tokenMeta)
		token1 := enrichToken(tokenInfoFromAddress(local.pool.Key.Currency1), tokenMeta)
		resp := buildV4DiagnosticsResponse(
			local.id,
			token0,
			token1,
			local.pool.Key.Fee,
			local.pool.State.SqrtPriceX96,
			local.pool.State.Tick,
			local.pool.State.Liquidity,
			local.pool.LastBlockNumber,
			string(local.pool.Status),
			head,
			chainState,
		)
		if isMismatchingAtHead(resp) {
			*items = append(*items, *resp)
		}
	}
	return nil
}

func isMismatchingAtHead(resp *DiagnosticsResponse) bool {
	if resp == nil || resp.Local.BlockLag > 0 {
		return false
	}
	if resp.PoolType == PoolTypeBalancer && resp.BalancerDiff != nil {
		return !balancerStateConsistent(*resp.BalancerDiff)
	}
	return !stateConsistent(resp.Diff)
}

func stateConsistent(diff StateDiff) bool {
	return diff.SqrtPriceX96Match && diff.TickMatch && diff.LiquidityMatch
}

func balancerStateConsistent(diff BalancerStateDiff) bool {
	return diff.TokensMatch &&
		diff.BalancesMatch &&
		diff.WeightsMatch &&
		diff.AmplificationMatch &&
		diff.SwapFeePercentageMatch
}

func sortDiagnostics(items []DiagnosticsResponse) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].PoolType != items[j].PoolType {
			return items[i].PoolType < items[j].PoolType
		}
		left := items[i].PoolAddress
		if left == "" {
			left = items[i].PoolID
		}
		right := items[j].PoolAddress
		if right == "" {
			right = items[j].PoolID
		}
		return left < right
	})
}

func (s *AppService) diagnosticsV4(ctx context.Context, poolID marketuniv4.PoolID, head uint64) (*DiagnosticsResponse, error) {
	if poolID == (marketuniv4.PoolID{}) {
		return nil, fmt.Errorf("poolId is required for univ4 diagnostics")
	}
	if s.v4Pools == nil {
		return nil, fmt.Errorf("v4 pool repository is not configured")
	}
	if s.chain.V4 == nil {
		return nil, fmt.Errorf("v4 chain reader is not configured")
	}

	pool, err := s.v4Pools.Get(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("load v4 pool: %w", err)
	}
	if pool == nil {
		return nil, fmt.Errorf("v4 pool %s not found", poolID.String())
	}

	token0, token1, err := s.enrichPair(ctx, pool.Key.Currency0, pool.Key.Currency1)
	if err != nil {
		return nil, err
	}

	chainBlock := head
	if pool.LastBlockNumber > 0 && pool.LastBlockNumber <= head {
		chainBlock = pool.LastBlockNumber
	}

	chainState, err := s.chain.V4.ReadV4BaseState(ctx, poolID, chainBlock)
	if err != nil {
		return nil, fmt.Errorf("read chain v4 state: %w", err)
	}

	local := snapshotFromState(pool.State.SqrtPriceX96, pool.State.Tick, pool.State.Liquidity, pool.LastBlockNumber, string(pool.Status), token0.Decimal, token1.Decimal)
	chain := snapshotFromState(chainState.SqrtPriceX96, chainState.Tick, chainState.Liquidity, chainBlock, "", token0.Decimal, token1.Decimal)
	local.BlockLag = blockLag(head, pool.LastBlockNumber)

	return &DiagnosticsResponse{
		PoolType:  PoolTypeUniv4,
		PoolID:    poolID.String(),
		Token0:    token0,
		Token1:    token1,
		Fee:       pool.Key.Fee,
		ChainHead: head,
		Local:     local,
		Chain:     chain,
		Diff:      diffSnapshots(local, chain),
	}, nil
}

func buildV3DiagnosticsResponse(
	poolType string,
	poolAddress common.Address,
	token0, token1 TokenInfo,
	fee uint32,
	sqrtPrice *big.Int,
	tick int32,
	liquidity *big.Int,
	lastBlock uint64,
	status string,
	head uint64,
	chainState *BaseState,
) *DiagnosticsResponse {
	local := snapshotFromState(sqrtPrice, tick, liquidity, lastBlock, status, token0.Decimal, token1.Decimal)
	chain := snapshotFromState(chainState.SqrtPriceX96, chainState.Tick, chainState.Liquidity, head, "", token0.Decimal, token1.Decimal)
	local.BlockLag = blockLag(head, lastBlock)

	return &DiagnosticsResponse{
		PoolType:    poolType,
		PoolAddress: poolAddress.Hex(),
		Token0:      token0,
		Token1:      token1,
		Fee:         fee,
		ChainHead:   head,
		Local:       local,
		Chain:       chain,
		Diff:        diffSnapshots(local, chain),
	}
}

func buildV4DiagnosticsResponse(
	poolID marketuniv4.PoolID,
	token0, token1 TokenInfo,
	fee uint32,
	sqrtPrice *big.Int,
	tick int32,
	liquidity *big.Int,
	lastBlock uint64,
	status string,
	head uint64,
	chainState *BaseState,
) *DiagnosticsResponse {
	local := snapshotFromState(sqrtPrice, tick, liquidity, lastBlock, status, token0.Decimal, token1.Decimal)
	chain := snapshotFromState(chainState.SqrtPriceX96, chainState.Tick, chainState.Liquidity, head, "", token0.Decimal, token1.Decimal)
	local.BlockLag = blockLag(head, lastBlock)

	return &DiagnosticsResponse{
		PoolType:  PoolTypeUniv4,
		PoolID:    poolID.String(),
		Token0:    token0,
		Token1:    token1,
		Fee:       fee,
		ChainHead: head,
		Local:     local,
		Chain:     chain,
		Diff:      diffSnapshots(local, chain),
	}
}

func readManyV3BaseStates(
	ctx context.Context,
	reader V3BaseStateReader,
	poolAddresses []common.Address,
	blockNumber uint64,
) (map[common.Address]*BaseState, error) {
	if len(poolAddresses) == 0 {
		return map[common.Address]*BaseState{}, nil
	}
	if batch, ok := reader.(V3BaseStateBatchReader); ok {
		return batch.ReadManyV3BaseStates(ctx, poolAddresses, blockNumber)
	}

	out := make(map[common.Address]*BaseState, len(poolAddresses))
	for _, address := range poolAddresses {
		state, err := reader.ReadV3BaseState(ctx, address, blockNumber)
		if err != nil {
			continue
		}
		out[address] = state
	}
	return out, nil
}

func readManyV4BaseStates(
	ctx context.Context,
	reader V4BaseStateReader,
	poolIDs []marketuniv4.PoolID,
	blockNumber uint64,
) (map[marketuniv4.PoolID]*BaseState, error) {
	if len(poolIDs) == 0 {
		return map[marketuniv4.PoolID]*BaseState{}, nil
	}
	if batch, ok := reader.(V4BaseStateBatchReader); ok {
		return batch.ReadManyV4BaseStates(ctx, poolIDs, blockNumber)
	}

	out := make(map[marketuniv4.PoolID]*BaseState, len(poolIDs))
	for _, poolID := range poolIDs {
		state, err := reader.ReadV4BaseState(ctx, poolID, blockNumber)
		if err != nil {
			continue
		}
		out[poolID] = state
	}
	return out, nil
}

func (s *AppService) resolveTokenMetadata(ctx context.Context, addresses ...common.Address) (map[common.Address]*asset.Token, error) {
	if s.tokens == nil || len(addresses) == 0 {
		return map[common.Address]*asset.Token{}, nil
	}
	unique := uniqueAddresses(addresses)
	return s.tokens.Resolve(ctx, unique)
}

func uniqueAddresses(addresses []common.Address) []common.Address {
	seen := make(map[common.Address]struct{}, len(addresses))
	unique := make([]common.Address, 0, len(addresses))
	for _, address := range addresses {
		if address == (common.Address{}) {
			continue
		}
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		unique = append(unique, address)
	}
	return unique
}

func (s *AppService) buildV3Diagnostics(
	ctx context.Context,
	poolType string,
	poolAddress, token0Addr, token1Addr common.Address,
	fee uint32,
	sqrtPrice *big.Int,
	tick int32,
	liquidity *big.Int,
	lastBlock uint64,
	status string,
	head uint64,
	reader V3BaseStateReader,
) (*DiagnosticsResponse, error) {
	token0, token1, err := s.enrichPair(ctx, token0Addr, token1Addr)
	if err != nil {
		return nil, err
	}

	chainState, err := reader.ReadV3BaseState(ctx, poolAddress, head)
	if err != nil {
		return nil, fmt.Errorf("read chain v3 state: %w", err)
	}

	return buildV3DiagnosticsResponse(
		poolType,
		poolAddress,
		token0,
		token1,
		fee,
		sqrtPrice,
		tick,
		liquidity,
		lastBlock,
		status,
		head,
		chainState,
	), nil
}

func (s *AppService) enrichPair(ctx context.Context, token0Addr, token1Addr common.Address) (TokenInfo, TokenInfo, error) {
	tokenMeta := map[common.Address]*asset.Token{}
	if s.tokens != nil {
		var err error
		tokenMeta, err = s.tokens.Resolve(ctx, []common.Address{token0Addr, token1Addr})
		if err != nil {
			return TokenInfo{}, TokenInfo{}, err
		}
	}
	token0 := enrichToken(tokenInfoFromAddress(token0Addr), tokenMeta)
	token1 := enrichToken(tokenInfoFromAddress(token1Addr), tokenMeta)
	return token0, token1, nil
}

func snapshotFromState(sqrtPrice *big.Int, tick int32, liquidity *big.Int, blockNumber uint64, status string, decimals0, decimals1 uint8) StateSnapshot {
	snap := StateSnapshot{
		Tick:            tick,
		LastBlockNumber: blockNumber,
		Price:           impliedPrice(sqrtPrice, decimals0, decimals1),
	}
	if status != "" {
		snap.Status = status
	}
	if sqrtPrice != nil {
		snap.SqrtPriceX96 = sqrtPrice.String()
	}
	if liquidity != nil {
		snap.Liquidity = liquidity.String()
	}
	if blockNumber > 0 {
		snap.BlockNumber = blockNumber
	}
	return snap
}

func blockLag(head, lastBlock uint64) uint64 {
	if head >= lastBlock {
		return head - lastBlock
	}
	return 0
}

func diffSnapshots(local, chain StateSnapshot) StateDiff {
	return StateDiff{
		SqrtPriceX96Match: local.SqrtPriceX96 == chain.SqrtPriceX96,
		TickMatch:         local.Tick == chain.Tick,
		LiquidityMatch:    local.Liquidity == chain.Liquidity,
	}
}
