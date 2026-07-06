package poolsapp

import (
	"context"
	"fmt"
	"math/big"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// DiagnosticsRequest selects a tracked pool to inspect.
type DiagnosticsRequest struct {
	PoolType    string
	PoolAddress common.Address
	PoolID      marketuniv4.PoolID
}

// StateSnapshot summarizes pool base state for diagnostics.
type StateSnapshot struct {
	BlockNumber     uint64    `json:"blockNumber,omitempty"`
	LastBlockNumber uint64    `json:"lastBlockNumber,omitempty"`
	BlockLag        uint64    `json:"blockLag,omitempty"`
	Status          string    `json:"status,omitempty"`
	SqrtPriceX96    string    `json:"sqrtPriceX96,omitempty"`
	Tick            int32     `json:"tick,omitempty"`
	Liquidity       string    `json:"liquidity,omitempty"`
	Price           PriceInfo `json:"price,omitempty"`
}

// StateDiff compares local and chain snapshots.
type StateDiff struct {
	SqrtPriceX96Match bool `json:"sqrtPriceX96Match"`
	TickMatch         bool `json:"tickMatch"`
	LiquidityMatch    bool `json:"liquidityMatch"`
}

// DiagnosticsResponse compares synced pool state against on-chain data.
type DiagnosticsResponse struct {
	PoolType    string        `json:"poolType"`
	PoolAddress string        `json:"poolAddress,omitempty"`
	PoolID      string        `json:"poolId,omitempty"`
	Token0      TokenInfo     `json:"token0"`
	Token1      TokenInfo     `json:"token1"`
	Fee         uint32        `json:"fee"`
	ChainHead   uint64        `json:"chainHeadBlock"`
	Local       StateSnapshot `json:"local"`
	Chain       StateSnapshot `json:"chain"`
	Diff        StateDiff     `json:"diff"`
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
		return s.diagnosticsUniv3(ctx, req.PoolAddress, head)
	case PoolTypePancakeV3:
		return s.diagnosticsPancake(ctx, req.PoolAddress, head)
	default:
		return nil, fmt.Errorf("unsupported poolType %q", req.PoolType)
	}
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

	chainState, err := s.chain.V4.ReadV4BaseState(ctx, poolID, head)
	if err != nil {
		return nil, fmt.Errorf("read chain v4 state: %w", err)
	}

	local := snapshotFromState(pool.State.SqrtPriceX96, pool.State.Tick, pool.State.Liquidity, pool.LastBlockNumber, string(pool.Status), token0.Decimal, token1.Decimal)
	chain := snapshotFromState(chainState.SqrtPriceX96, chainState.Tick, chainState.Liquidity, head, "", token0.Decimal, token1.Decimal)
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

func (s *AppService) diagnosticsUniv3(ctx context.Context, poolAddress common.Address, head uint64) (*DiagnosticsResponse, error) {
	if poolAddress == (common.Address{}) {
		return nil, fmt.Errorf("poolAddress is required for univ3 diagnostics")
	}
	if s.univ3Pools == nil {
		return nil, fmt.Errorf("univ3 pool repository is not configured")
	}
	if s.chain.V3 == nil {
		return nil, fmt.Errorf("univ3 chain reader is not configured")
	}

	pool, err := s.univ3Pools.Get(ctx, poolAddress)
	if err != nil {
		return nil, fmt.Errorf("load univ3 pool: %w", err)
	}
	if pool == nil {
		return nil, fmt.Errorf("univ3 pool %s not found", poolAddress.Hex())
	}

	return s.buildV3Diagnostics(ctx, PoolTypeUniv3, poolAddress, pool.Token0, pool.Token1, pool.Fee, pool.State.SqrtPriceX96, pool.State.Tick, pool.State.Liquidity, pool.LastBlockNumber, string(pool.Status), head, s.chain.V3)
}

func (s *AppService) diagnosticsPancake(ctx context.Context, poolAddress common.Address, head uint64) (*DiagnosticsResponse, error) {
	if poolAddress == (common.Address{}) {
		return nil, fmt.Errorf("poolAddress is required for pancakev3 diagnostics")
	}
	if s.pancakePools == nil {
		return nil, fmt.Errorf("pancakev3 pool repository is not configured")
	}
	if s.chain.Pancake == nil {
		return nil, fmt.Errorf("pancakev3 chain reader is not configured")
	}

	pool, err := s.pancakePools.Get(ctx, poolAddress)
	if err != nil {
		return nil, fmt.Errorf("load pancakev3 pool: %w", err)
	}
	if pool == nil {
		return nil, fmt.Errorf("pancakev3 pool %s not found", poolAddress.Hex())
	}

	return s.buildV3Diagnostics(ctx, PoolTypePancakeV3, poolAddress, pool.Token0, pool.Token1, pool.Fee, pool.State.SqrtPriceX96, pool.State.Tick, pool.State.Liquidity, pool.LastBlockNumber, string(pool.Status), head, s.chain.Pancake)
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
	}, nil
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
