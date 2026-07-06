package syncv4

import (
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"go.uber.org/zap"
)

func v4EventLogFields(event marketv4.PoolEvent) []zap.Field {
	switch event.Kind {
	case marketv4.EventKindInitialize:
		if event.Initialize == nil {
			return nil
		}
		fields := []zap.Field{zap.Int32("tick", event.Initialize.Tick)}
		if event.Initialize.SqrtPriceX96 != nil {
			fields = append(fields, zap.String("sqrtPriceX96", event.Initialize.SqrtPriceX96.String()))
		}
		return fields
	case marketv4.EventKindSwap:
		if event.Swap == nil {
			return nil
		}
		fields := []zap.Field{
			zap.Int32("tick", event.Swap.Tick),
			zap.Uint32("fee", event.Swap.Fee),
		}
		if event.Swap.SqrtPriceX96 != nil {
			fields = append(fields, zap.String("sqrtPriceX96", event.Swap.SqrtPriceX96.String()))
		}
		if event.Swap.Liquidity != nil {
			fields = append(fields, zap.String("liquidity", event.Swap.Liquidity.String()))
		}
		if event.Swap.Amount0 != nil {
			fields = append(fields, zap.String("amount0", event.Swap.Amount0.String()))
		}
		if event.Swap.Amount1 != nil {
			fields = append(fields, zap.String("amount1", event.Swap.Amount1.String()))
		}
		return fields
	case marketv4.EventKindModifyLiquidity:
		if event.ModifyLiquidity == nil {
			return nil
		}
		fields := []zap.Field{
			zap.Int32("tickLower", event.ModifyLiquidity.TickLower),
			zap.Int32("tickUpper", event.ModifyLiquidity.TickUpper),
		}
		if event.ModifyLiquidity.LiquidityDelta != nil {
			fields = append(fields, zap.String("liquidityDelta", event.ModifyLiquidity.LiquidityDelta.String()))
		}
		return fields
	default:
		return nil
	}
}
