package quote

import (
	"fmt"
	"math/big"

	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/ethereum/go-ethereum/common"
)

// Quoter 基于本地 pool.State 的报价器（Quote / Arbitrage 共享）。
type Quoter struct {
	cache *pool.Cache
}

// NewQuoter 创建报价器。
func NewQuoter(cache *pool.Cache) *Quoter {
	return &Quoter{cache: cache}
}

// ExactInput 对指定池子执行 exact input 报价。
func (q *Quoter) ExactInput(poolAddr common.Address, amountIn *big.Int, tokenIn common.Address) (*big.Int, error) {
	if q.cache == nil {
		return nil, fmt.Errorf("pool cache is nil")
	}
	state, ok := q.cache.Get(poolAddr)
	if !ok {
		return nil, fmt.Errorf("pool %s not found", poolAddr.Hex())
	}
	return state.QuoteExactInput(amountIn, tokenIn)
}
