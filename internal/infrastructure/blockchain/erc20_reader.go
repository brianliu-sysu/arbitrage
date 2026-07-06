package blockchain

import (
	"context"
	"fmt"
	"strings"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// ERC20Reader loads ERC20 metadata from chain via multicall.
type ERC20Reader struct {
	multicall *Multicall
	abi       abi.ABI
}

func NewERC20Reader(multicall *Multicall) (*ERC20Reader, error) {
	parsed, err := abi.JSON(strings.NewReader(erc20ABIJSON))
	if err != nil {
		return nil, fmt.Errorf("parse erc20 abi: %w", err)
	}
	return &ERC20Reader{
		multicall: multicall,
		abi:       parsed,
	}, nil
}

// FetchMany loads symbol and decimals for the given token addresses.
func (r *ERC20Reader) FetchMany(ctx context.Context, addresses []common.Address) (map[common.Address]*asset.Token, error) {
	if r == nil || len(addresses) == 0 {
		return map[common.Address]*asset.Token{}, nil
	}

	erc20Addresses := make([]common.Address, 0, len(addresses))
	for _, address := range addresses {
		if asset.IsNativeETH(address) {
			continue
		}
		erc20Addresses = append(erc20Addresses, address)
	}
	if len(erc20Addresses) == 0 {
		return map[common.Address]*asset.Token{}, nil
	}

	symbolData, err := r.abi.Pack("symbol")
	if err != nil {
		return nil, fmt.Errorf("pack symbol: %w", err)
	}
	decimalData, err := r.abi.Pack("decimals")
	if err != nil {
		return nil, fmt.Errorf("pack decimals: %w", err)
	}

	requests := make([]MulticallRequest, 0, len(erc20Addresses)*2)
	for _, address := range erc20Addresses {
		requests = append(requests,
			MulticallRequest{Target: address, Data: symbolData},
			MulticallRequest{Target: address, Data: decimalData},
		)
	}

	results, err := r.multicall.Aggregate3(ctx, requests, 0)
	if err != nil {
		return nil, err
	}

	out := make(map[common.Address]*asset.Token, len(erc20Addresses))
	for i, address := range erc20Addresses {
		symbolResult := results[i*2]
		decimalResult := results[i*2+1]

		token := &asset.Token{Address: address}
		if symbolResult.Success {
			if symbol, ok := unpackERC20Symbol(r.abi, symbolResult.ReturnData); ok {
				token.Symbol = symbol
			}
		}
		if decimalResult.Success {
			if decimals, ok := unpackERC20Decimals(r.abi, decimalResult.ReturnData); ok {
				token.Decimal = decimals
			}
		}
		out[address] = token
	}
	return out, nil
}

func unpackERC20Symbol(tokenABI abi.ABI, data []byte) (string, bool) {
	values, err := tokenABI.Unpack("symbol", data)
	if err != nil {
		return "", false
	}
	if len(values) == 0 {
		return "", false
	}
	switch v := values[0].(type) {
	case string:
		return strings.TrimSpace(v), true
	case [32]byte:
		return strings.TrimRight(string(v[:]), "\x00"), true
	default:
		return "", false
	}
}

func unpackERC20Decimals(tokenABI abi.ABI, data []byte) (uint8, bool) {
	values, err := tokenABI.Unpack("decimals", data)
	if err != nil || len(values) == 0 {
		return 0, false
	}
	switch v := values[0].(type) {
	case uint8:
		return v, true
	default:
		return 0, false
	}
}
