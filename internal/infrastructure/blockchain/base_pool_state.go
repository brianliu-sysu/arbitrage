package blockchain

import "math/big"

// BasePoolState is on-chain slot0 and liquidity without tick data.
type BasePoolState struct {
	SqrtPriceX96 *big.Int
	Tick         int32
	Liquidity    *big.Int
}
