package blockchain

import (
	"context"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// FactoryReader resolves Uniswap V3 pool addresses from the factory contract.
type FactoryReader struct {
	client  *EthClient
	address common.Address
	abi     abi.ABI
}

func NewFactoryReader(client *EthClient, factoryAddress common.Address) (*FactoryReader, error) {
	parsed, err := abi.JSON(strings.NewReader(factoryABIJSON))
	if err != nil {
		return nil, fmt.Errorf("parse factory abi: %w", err)
	}
	return &FactoryReader{
		client:  client,
		address: factoryAddress,
		abi:     parsed,
	}, nil
}

func (r *FactoryReader) GetPool(ctx context.Context, token0, token1 common.Address, fee uint32, blockNumber uint64) (common.Address, error) {
	a, b := sortTokens(token0, token1)
	data, err := r.abi.Pack("getPool", a, b, fee)
	if err != nil {
		return common.Address{}, fmt.Errorf("pack getPool: %w", err)
	}
	output, err := r.client.CallContract(ctx, r.address, data, blockNumber)
	if err != nil {
		return common.Address{}, fmt.Errorf("call getPool: %w", err)
	}
	values, err := r.abi.Unpack("getPool", output)
	if err != nil {
		return common.Address{}, fmt.Errorf("unpack getPool: %w", err)
	}
	if len(values) != 1 {
		return common.Address{}, fmt.Errorf("getPool returned %d values", len(values))
	}
	pool, ok := values[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("unexpected getPool type %T", values[0])
	}
	return pool, nil
}

func sortTokens(token0, token1 common.Address) (common.Address, common.Address) {
	if token0.Hex() > token1.Hex() {
		return token1, token0
	}
	return token0, token1
}
