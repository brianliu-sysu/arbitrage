package blockchain

import "fmt"

// ChainID identifies an EVM chain. It must be carried by persisted chain data
// when running multiple independent chain runtimes.
type ChainID uint64

func (id ChainID) IsZero() bool {
	return id == 0
}

func (id ChainID) String() string {
	return fmt.Sprintf("%d", uint64(id))
}
