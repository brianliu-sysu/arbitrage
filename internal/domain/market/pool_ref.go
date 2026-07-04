package market

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// PoolRef identifies a pool across protocols.
// V3 pools use a deployed contract address; V4 pools use a PoolId (bytes32).
type PoolRef struct {
	Protocol Protocol
	Address  common.Address
	PoolID   common.Hash
}

func PoolRefFromV3(address common.Address) PoolRef {
	return PoolRef{Protocol: ProtocolV3, Address: address}
}

func PoolRefFromV4(poolID common.Hash) PoolRef {
	return PoolRef{Protocol: ProtocolV4, PoolID: poolID}
}

func (r PoolRef) IsZero() bool {
	return r.Protocol == ProtocolUnknown ||
		(r.Protocol == ProtocolV3 && r.Address == (common.Address{})) ||
		(r.Protocol == ProtocolV4 && r.PoolID == (common.Hash{}))
}

func (r PoolRef) String() string {
	switch r.Protocol {
	case ProtocolV3:
		return fmt.Sprintf("v3:%s", r.Address.Hex())
	case ProtocolV4:
		return fmt.Sprintf("v4:%s", r.PoolID.Hex())
	default:
		return "unknown"
	}
}
