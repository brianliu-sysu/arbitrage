package unified

import (
	"fmt"
	"math/big"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	"github.com/ethereum/go-ethereum/common"
)

// WETHBridgeEdges returns synthetic pool edges connecting native ETH and WETH.
func WETHBridgeEdges() []PoolEdge {
	weth := asset.MainnetWETH
	native := common.Address{}
	return []PoolEdge{
		{Version: PoolVersionUnwrapWETH, Token0: weth, Token1: native},
		{Version: PoolVersionWrapWETH, Token0: native, Token1: weth},
	}
}

// IsWETHBridgeVersion reports whether version is a WETH wrap or unwrap hop.
func IsWETHBridgeVersion(version PoolVersion) bool {
	return version == PoolVersionWrapWETH || version == PoolVersionUnwrapWETH
}

// ValidateWETHBridgeHop checks token direction for a wrap or unwrap hop.
func ValidateWETHBridgeHop(hop RouteHop) error {
	weth := asset.MainnetWETH
	native := common.Address{}
	switch hop.Version {
	case PoolVersionUnwrapWETH:
		if hop.TokenIn != weth || hop.TokenOut != native {
			return fmt.Errorf("unwrap hop must be WETH -> native ETH")
		}
	case PoolVersionWrapWETH:
		if hop.TokenIn != native || hop.TokenOut != weth {
			return fmt.Errorf("wrap hop must be native ETH -> WETH")
		}
	default:
		return fmt.Errorf("not a WETH bridge hop")
	}
	return nil
}

// QuoteWETHBridge quotes a 1:1 wrap or unwrap hop.
func QuoteWETHBridge(hop RouteHop, amountIn *big.Int) (quoteshared.QuoteResult, error) {
	if amountIn == nil || amountIn.Sign() <= 0 {
		return quoteshared.QuoteResult{}, fmt.Errorf("amountIn must be positive")
	}
	if err := ValidateWETHBridgeHop(hop); err != nil {
		return quoteshared.QuoteResult{}, err
	}
	return quoteshared.NewQuoteResult(amountIn, new(big.Int).Set(amountIn), big.NewInt(0), nil, 0), nil
}
