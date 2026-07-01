package api

import (
	"math/big"

	"github.com/brianliu-sysu/arbitrage/internal/arbitrage"
	"github.com/brianliu-sysu/arbitrage/internal/quote"
	"github.com/ethereum/go-ethereum/common"
)

// QuoteProvider 报价服务接口（多链版本）。
type QuoteProvider interface {
	GetAllPoolInfo() []map[string]interface{}
	GetPrice(chain string, poolAddr common.Address) (price0In1, price1In0 float64, tick int32, ok bool)
	QuoteExactInput(chain string, poolAddr common.Address, amountIn *big.Int, tokenIn common.Address) (*big.Int, error)
	CrossQuote(chain string, amountIn *big.Int, tokenIn, tokenOut common.Address) (*quote.Result, error)
	TriangleOpportunities(chain string) ([]arbitrage.TriangleOpportunity, error)
}
