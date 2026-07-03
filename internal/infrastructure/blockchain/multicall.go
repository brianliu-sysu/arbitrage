package blockchain

import (
	"context"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const multicallBatchSize = 64

// Multicall batches eth_call requests through Multicall3 aggregate3.
type Multicall struct {
	client  *EthClient
	address common.Address
	abi     abi.ABI
}

func NewMulticall(client *EthClient, address common.Address) (*Multicall, error) {
	parsed, err := abi.JSON(strings.NewReader(multicallABIJSON))
	if err != nil {
		return nil, fmt.Errorf("parse multicall abi: %w", err)
	}
	return &Multicall{
		client:  client,
		address: address,
		abi:     parsed,
	}, nil
}

type MulticallRequest struct {
	Target common.Address
	Data   []byte
}

type MulticallResult struct {
	Success    bool
	ReturnData []byte
}

func (m *Multicall) Aggregate3(ctx context.Context, requests []MulticallRequest, blockNumber uint64) ([]MulticallResult, error) {
	if len(requests) == 0 {
		return nil, nil
	}

	results := make([]MulticallResult, 0, len(requests))
	for start := 0; start < len(requests); start += multicallBatchSize {
		end := start + multicallBatchSize
		if end > len(requests) {
			end = len(requests)
		}
		batchResults, err := m.aggregate3Batch(ctx, requests[start:end], blockNumber)
		if err != nil {
			return nil, err
		}
		results = append(results, batchResults...)
	}
	return results, nil
}

func (m *Multicall) aggregate3Batch(ctx context.Context, requests []MulticallRequest, blockNumber uint64) ([]MulticallResult, error) {
	type call3 struct {
		Target       common.Address
		AllowFailure bool
		CallData     []byte
	}

	calls := make([]call3, len(requests))
	for i, request := range requests {
		calls[i] = call3{
			Target:       request.Target,
			AllowFailure: true,
			CallData:     request.Data,
		}
	}

	data, err := m.abi.Pack("aggregate3", calls)
	if err != nil {
		return nil, fmt.Errorf("pack aggregate3: %w", err)
	}

	output, err := m.client.CallContract(ctx, m.address, data, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("aggregate3 call: %w", err)
	}

	unpacked, err := m.abi.Unpack("aggregate3", output)
	if err != nil {
		return nil, fmt.Errorf("unpack aggregate3: %w", err)
	}
	if len(unpacked) != 1 {
		return nil, fmt.Errorf("unexpected aggregate3 result length %d", len(unpacked))
	}

	rawResults, ok := unpacked[0].([]struct {
		Success    bool    `json:"success"`
		ReturnData []uint8 `json:"returnData"`
	})
	if !ok {
		return nil, fmt.Errorf("unexpected aggregate3 result type %T", unpacked[0])
	}

	results := make([]MulticallResult, len(rawResults))
	for i, result := range rawResults {
		results[i] = MulticallResult{
			Success:    result.Success,
			ReturnData: bytesFromUint8Slice(result.ReturnData),
		}
	}
	return results, nil
}

func bytesFromUint8Slice(values []uint8) []byte {
	if len(values) == 0 {
		return nil
	}
	out := make([]byte, len(values))
	copy(out, values)
	return out
}
