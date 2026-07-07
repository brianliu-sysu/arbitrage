package blockchain

import (
	"math/big"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// RawLog is a decoded-free chain log entry fetched from RPC.
type RawLog struct {
	Address     common.Address
	Topics      []common.Hash
	Data        []byte
	BlockNumber uint64
	BlockHash   common.Hash
	TxIndex     uint
	LogIndex    uint
}

// CLV3LogFilter selects logs for tracked V3-style pools within a block range.
type CLV3LogFilter struct {
	PoolAddresses []common.Address
	FromBlock     uint64
	ToBlock       uint64
}

// V4LogFilter selects PoolManager logs for tracked V4 pools within a block range.
type V4LogFilter struct {
	PoolIDs   []marketv4.PoolID
	FromBlock uint64
	ToBlock   uint64
}

// BalancerLogFilter selects Vault/pool logs for tracked Balancer pools within a block range.
type BalancerLogFilter struct {
	V2PoolIDs       []marketbalancer.PoolID
	V2PoolAddresses []common.Address
	V3PoolAddresses []common.Address
	FromBlock       uint64
	ToBlock         uint64
}

// BasePoolState is on-chain slot0 and liquidity without tick data.
type BasePoolState struct {
	SqrtPriceX96 *big.Int
	Tick         int32
	Liquidity    *big.Int
}
