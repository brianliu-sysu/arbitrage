package asset

import "github.com/ethereum/go-ethereum/common"

type Token struct {
	Address common.Address
	Decimal uint8
	Symbol  string
}
