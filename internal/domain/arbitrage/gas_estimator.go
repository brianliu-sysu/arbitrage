package arbitrage

import (
	"context"
	"math/big"
)

// GasEstimate holds the estimated execution cost of an arbitrage route.
type GasEstimate struct {
	GasLimit uint64
	GasPrice *big.Int
	CostWei  *big.Int
}

// GasEstimator estimates gas usage for arbitrage execution.
type GasEstimator interface {
	Estimate(ctx context.Context, hopCount int) (GasEstimate, error)
}

// StaticGasEstimator applies fixed gas assumptions for domain-side filtering.
type StaticGasEstimator struct {
	BaseGas     uint64
	GasPerHop   uint64
	GasPriceWei *big.Int
}

func NewStaticGasEstimator(baseGas, gasPerHop uint64, gasPriceWei *big.Int) *StaticGasEstimator {
	return &StaticGasEstimator{
		BaseGas:     baseGas,
		GasPerHop:   gasPerHop,
		GasPriceWei: cloneBigInt(gasPriceWei),
	}
}

func (e *StaticGasEstimator) Estimate(_ context.Context, hopCount int) (GasEstimate, error) {
	if hopCount <= 0 {
		hopCount = 1
	}
	gasLimit := e.BaseGas + e.GasPerHop*uint64(hopCount)
	gasPrice := cloneBigInt(e.GasPriceWei)
	costWei := new(big.Int).Mul(big.NewInt(int64(gasLimit)), gasPrice)
	return GasEstimate{
		GasLimit: gasLimit,
		GasPrice: gasPrice,
		CostWei:  costWei,
	}, nil
}
