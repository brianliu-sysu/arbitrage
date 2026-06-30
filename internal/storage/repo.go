// Package storage 定义持久化 Repository 接口。
package storage

import (
	"context"
	"math/big"
)

// PoolSnapshot 池子状态快照。
type PoolSnapshot struct {
	ChainName    string
	PoolAddress  string
	BlockNumber  uint64
	Tick         int32
	SqrtPriceX96 *big.Int
	Liquidity    *big.Int
	Price0In1    float64
	Token0Symbol string
	Token1Symbol string
	Fee          uint32
	TickData     map[int32]TickLiquiditySnapshot
}

// TickLiquiditySnapshot 单个 tick 流动性快照。
type TickLiquiditySnapshot struct {
	LiquidityNet   *big.Int `json:"liquidityNet"`
	LiquidityGross *big.Int `json:"liquidityGross"`
}

// TokenMetadata 代币元信息。
type TokenMetadata struct {
	ChainName    string
	TokenAddress string
	Symbol       string
	Decimals     int
}

// PoolRepo 池子状态读写。
type PoolRepo interface {
	Save(ctx context.Context, s *PoolSnapshot) error
	SaveHistory(ctx context.Context, s *PoolSnapshot) error
	Load(ctx context.Context, chainName, poolAddress string) (*PoolSnapshot, error)
	LoadAll(ctx context.Context, chainName string) (map[string]*PoolSnapshot, error)
	LoadTokenMetadata(ctx context.Context, chainName, tokenAddress string) (*TokenMetadata, error)
	SaveTokenMetadata(ctx context.Context, meta *TokenMetadata) error
	Close()
}

// SyncRepo 链同步进度（LastProcessedBlock、Reorg checkpoint）。
type SyncRepo interface {
	GetLastProcessedBlock(ctx context.Context, chainName string) (uint64, error)
	SetLastProcessedBlock(ctx context.Context, chainName string, block uint64) error
}

// MaxIncrementalGap 增量同步最大区块间隔，超过则全量重建 tick 地图。
const MaxIncrementalGap = 100
