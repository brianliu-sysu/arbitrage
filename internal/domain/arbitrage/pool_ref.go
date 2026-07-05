package arbitrage

import (
	"fmt"
	"strings"

	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// PoolRef identifies a pool in Uniswap V3, PancakeSwap V3, or V4.
type PoolRef struct {
	Version   quoteunified.PoolVersion
	V3        common.Address
	PancakeV3 common.Address
	V4        marketv4.PoolID
}

func PoolRefFromV3(address common.Address) PoolRef {
	return PoolRef{Version: quoteunified.PoolVersionV3, V3: address}
}

func PoolRefFromPancakeV3(address common.Address) PoolRef {
	return PoolRef{Version: quoteunified.PoolVersionPancakeV3, PancakeV3: address}
}

func PoolRefFromV4(id marketv4.PoolID) PoolRef {
	return PoolRef{Version: quoteunified.PoolVersionV4, V4: id}
}

func PoolRefFromHop(hop quoteunified.RouteHop) PoolRef {
	switch hop.Version {
	case quoteunified.PoolVersionV3:
		return PoolRefFromV3(hop.PoolV3)
	case quoteunified.PoolVersionPancakeV3:
		return PoolRefFromPancakeV3(hop.PoolPancakeV3)
	case quoteunified.PoolVersionV4:
		return PoolRefFromV4(hop.PoolV4)
	default:
		return PoolRef{}
	}
}

// Key returns a stable string key for graph indexing.
func (p PoolRef) Key() string {
	switch p.Version {
	case quoteunified.PoolVersionV3:
		return "v3:" + p.V3.Hex()
	case quoteunified.PoolVersionPancakeV3:
		return "pancakev3:" + p.PancakeV3.Hex()
	case quoteunified.PoolVersionV4:
		return "v4:" + p.V4.String()
	default:
		return ""
	}
}

func (p PoolRef) String() string {
	if key := p.Key(); key != "" {
		return key
	}
	return "unknown"
}

// PrimaryAddress returns the pool address when the ref is address-based.
func (p PoolRef) PrimaryAddress() common.Address {
	switch p.Version {
	case quoteunified.PoolVersionV3:
		return p.V3
	case quoteunified.PoolVersionPancakeV3:
		return p.PancakeV3
	default:
		return common.Address{}
	}
}

func poolRefFromKey(key string) (PoolRef, error) {
	switch {
	case strings.HasPrefix(key, "pancakev3:"):
		addr := common.HexToAddress(key[len("pancakev3:"):])
		if addr == (common.Address{}) {
			return PoolRef{}, fmt.Errorf("invalid pancakev3 pool ref key %q", key)
		}
		return PoolRefFromPancakeV3(addr), nil
	case strings.HasPrefix(key, "v3:"):
		addr := common.HexToAddress(key[len("v3:"):])
		if addr == (common.Address{}) {
			return PoolRef{}, fmt.Errorf("invalid v3 pool ref key %q", key)
		}
		return PoolRefFromV3(addr), nil
	case strings.HasPrefix(key, "v4:"):
		hash := common.HexToHash(key[len("v4:"):])
		if hash == (common.Hash{}) {
			return PoolRef{}, fmt.Errorf("invalid v4 pool ref key %q", key)
		}
		return PoolRefFromV4(marketv4.PoolID(hash)), nil
	default:
		return PoolRef{}, fmt.Errorf("unsupported pool ref key %q", key)
	}
}
