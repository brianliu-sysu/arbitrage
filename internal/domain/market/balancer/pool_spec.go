package balancer

import "github.com/ethereum/go-ethereum/common"

// PoolSpec contains immutable registry metadata needed to bootstrap a Balancer pool.
type PoolSpec struct {
	Address      common.Address
	Vault        common.Address
	Type         PoolType
	VaultVersion VaultVersion
}
