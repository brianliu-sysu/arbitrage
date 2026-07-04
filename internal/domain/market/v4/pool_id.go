package v4

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// PoolID is the keccak256 hash of a PoolKey, used as the on-chain pool identifier.
type PoolID common.Hash

func (id PoolID) Hash() common.Hash {
	return common.Hash(id)
}

func (id PoolID) String() string {
	return id.Hash().Hex()
}

// ComputePoolID derives the on-chain PoolId from a PoolKey.
func ComputePoolID(key PoolKey) (PoolID, error) {
	addressType, err := abi.NewType("address", "", nil)
	if err != nil {
		return PoolID{}, fmt.Errorf("address type: %w", err)
	}
	uint24Type, err := abi.NewType("uint24", "", nil)
	if err != nil {
		return PoolID{}, fmt.Errorf("uint24 type: %w", err)
	}
	int24Type, err := abi.NewType("int24", "", nil)
	if err != nil {
		return PoolID{}, fmt.Errorf("int24 type: %w", err)
	}

	args := abi.Arguments{
		{Type: addressType},
		{Type: addressType},
		{Type: uint24Type},
		{Type: int24Type},
		{Type: addressType},
	}

	encoded, err := args.Pack(
		key.Currency0,
		key.Currency1,
		new(big.Int).SetUint64(uint64(key.Fee)),
		int32ToABIInt24(key.TickSpacing),
		key.Hooks,
	)
	if err != nil {
		return PoolID{}, fmt.Errorf("pack pool key: %w", err)
	}

	return PoolID(crypto.Keccak256Hash(encoded)), nil
}

func int32ToABIInt24(value int32) *big.Int {
	return big.NewInt(int64(value))
}
