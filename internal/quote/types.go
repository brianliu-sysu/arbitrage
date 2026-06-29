package quote

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// Result 跨池报价结果。
type Result struct {
	Hops      []Hop
	AmountIn  *big.Int
	AmountOut *big.Int
	TokenIn   common.Address
	TokenOut  common.Address
}

// Hop 报价路径中的一跳。
type Hop struct {
	Pool     common.Address
	TokenIn  common.Address
	TokenOut common.Address
}
