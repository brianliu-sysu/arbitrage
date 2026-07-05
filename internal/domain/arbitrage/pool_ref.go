package arbitrage

import (
	"fmt"

	quoteunified "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/unified"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
)

// PoolRef identifies a pool in either Uniswap V3 or V4.
type PoolRef struct {
	Version quoteunified.PoolVersion
	V3      common.Address
	V4      marketv4.PoolID
}

func PoolRefFromV3(address common.Address) PoolRef {
	return PoolRef{Version: quoteunified.PoolVersionV3, V3: address}
}

func PoolRefFromV4(id marketv4.PoolID) PoolRef {
	return PoolRef{Version: quoteunified.PoolVersionV4, V4: id}
}

func PoolRefFromHop(hop quoteunified.RouteHop) PoolRef {
	switch hop.Version {
	case quoteunified.PoolVersionV3:
		return PoolRefFromV3(hop.PoolV3)
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

// PrimaryAddress returns the V3 pool address when available.
func (p PoolRef) PrimaryAddress() common.Address {
	if p.Version == quoteunified.PoolVersionV3 {
		return p.V3
	}
	return common.Address{}
}

func poolRefFromKey(key string) (PoolRef, error) {
	if len(key) < 4 {
		return PoolRef{}, fmt.Errorf("invalid pool ref key %q", key)
	}
	switch key[:3] {
	case "v3:":
		addr := common.HexToAddress(key[3:])
		if addr == (common.Address{}) {
			return PoolRef{}, fmt.Errorf("invalid v3 pool ref key %q", key)
		}
		return PoolRefFromV3(addr), nil
	case "v4:":
		hash := common.HexToHash(key[3:])
		if hash == (common.Hash{}) {
			return PoolRef{}, fmt.Errorf("invalid v4 pool ref key %q", key)
		}
		return PoolRefFromV4(marketv4.PoolID(hash)), nil
	default:
		return PoolRef{}, fmt.Errorf("unsupported pool ref key %q", key)
	}
}
