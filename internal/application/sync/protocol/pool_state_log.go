package protocol

import (
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"go.uber.org/zap"
)

// PoolStateLogFields returns zap fields for concentrated-liquidity pool slot0 state.
func PoolStateLogFields(state market.PoolState, lastBlock uint64, status market.PoolStatus) []zap.Field {
	fields := []zap.Field{
		zap.Uint64("lastBlockNumber", lastBlock),
		zap.String("status", string(status)),
		zap.Int32("tick", state.Tick),
	}
	if state.SqrtPriceX96 != nil {
		fields = append(fields, zap.String("sqrtPriceX96", state.SqrtPriceX96.String()))
	}
	if state.Liquidity != nil {
		fields = append(fields, zap.String("liquidity", state.Liquidity.String()))
	}
	return fields
}
