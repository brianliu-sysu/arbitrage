package market

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// PoolRef identifies a pool across protocols.
// V3 pools use a deployed contract address; V4 and Balancer pools use a PoolId (bytes32).
type PoolRef struct {
	Protocol Protocol
	Address  common.Address
	PoolID   common.Hash
}

func PoolRefFromUniswapV3(address common.Address) PoolRef {
	return PoolRef{Protocol: ProtocolUniswapV3, Address: address}
}

// PoolRefFromV3 returns a pool ref for Uniswap V3.
func PoolRefFromV3(address common.Address) PoolRef {
	return PoolRefFromUniswapV3(address)
}

func PoolRefFromPancakeV3(address common.Address) PoolRef {
	return PoolRef{Protocol: ProtocolPancakeV3, Address: address}
}

func PoolRefFromQuickSwapV3(address common.Address) PoolRef {
	return PoolRef{Protocol: ProtocolQuickSwapV3, Address: address}
}

func PoolRefFromV4(poolID common.Hash) PoolRef {
	return PoolRef{Protocol: ProtocolV4, PoolID: poolID}
}

func PoolRefFromBalancer(poolID common.Hash) PoolRef {
	return PoolRef{Protocol: ProtocolBalancer, PoolID: poolID}
}

func (r PoolRef) IsZero() bool {
	return r.Protocol == ProtocolUnknown ||
		(isAddressProtocol(r.Protocol) && r.Address == (common.Address{})) ||
		(isPoolIDProtocol(r.Protocol) && r.PoolID == (common.Hash{}))
}

func isAddressProtocol(protocol Protocol) bool {
	switch protocol {
	case ProtocolUniswapV3, ProtocolPancakeV3, ProtocolQuickSwapV3:
		return true
	default:
		return false
	}
}

func isPoolIDProtocol(protocol Protocol) bool {
	switch protocol {
	case ProtocolV4, ProtocolBalancer:
		return true
	default:
		return false
	}
}

func (r PoolRef) String() string {
	switch r.Protocol {
	case ProtocolUniswapV3:
		return fmt.Sprintf("univ3:%s", r.Address.Hex())
	case ProtocolPancakeV3:
		return fmt.Sprintf("pancakev3:%s", r.Address.Hex())
	case ProtocolQuickSwapV3:
		return fmt.Sprintf("quickswapv3:%s", r.Address.Hex())
	case ProtocolV4:
		return fmt.Sprintf("univ4:%s", r.PoolID.Hex())
	case ProtocolBalancer:
		return fmt.Sprintf("balancer:%s", r.PoolID.Hex())
	default:
		return "unknown"
	}
}
