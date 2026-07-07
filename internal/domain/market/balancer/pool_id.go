package balancer

import "github.com/ethereum/go-ethereum/common"

// PoolID identifies a Balancer pool in the Vault.
type PoolID common.Hash

func (id PoolID) Hash() common.Hash {
	return common.Hash(id)
}

func (id PoolID) String() string {
	return id.Hash().Hex()
}
