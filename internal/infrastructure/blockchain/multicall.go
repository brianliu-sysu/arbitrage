package blockchain

import (
	"context"
	"fmt"
	"reflect"
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
	if len(output) == 0 {
		return nil, fmt.Errorf("aggregate3 returned empty data (check multicall address and RPC endpoint)")
	}

	unpacked, err := m.abi.Unpack("aggregate3", output)
	if err != nil {
		return nil, fmt.Errorf("unpack aggregate3: %w", err)
	}
	if len(unpacked) != 1 {
		return nil, fmt.Errorf("unexpected aggregate3 result length %d", len(unpacked))
	}

	rawResults, err := decodeAggregate3Results(unpacked[0])
	if err != nil {
		return nil, err
	}

	results := make([]MulticallResult, len(rawResults))
	for i, result := range rawResults {
		results[i] = MulticallResult{
			Success:    result.Success,
			ReturnData: result.ReturnData,
		}
	}
	return results, nil
}

type aggregate3Result struct {
	Success    bool
	ReturnData []byte
}

func decodeAggregate3Results(raw interface{}) ([]aggregate3Result, error) {
	value := reflect.ValueOf(raw)
	if value.Kind() != reflect.Slice {
		return nil, fmt.Errorf("unexpected aggregate3 result type %T", raw)
	}

	results := make([]aggregate3Result, value.Len())
	for i := 0; i < value.Len(); i++ {
		item := value.Index(i)
		if item.Kind() == reflect.Interface {
			item = item.Elem()
		}
		if item.Kind() != reflect.Struct {
			return nil, fmt.Errorf("unexpected aggregate3 item type %s", item.Kind())
		}

		successField := item.FieldByName("Success")
		returnField := item.FieldByName("ReturnData")
		if !successField.IsValid() || !returnField.IsValid() {
			return nil, fmt.Errorf("unexpected aggregate3 item struct %s", item.Type())
		}
		if successField.Kind() != reflect.Bool {
			return nil, fmt.Errorf("unexpected aggregate3 success type %s", successField.Kind())
		}
		if returnField.Kind() != reflect.Slice || returnField.Type().Elem().Kind() != reflect.Uint8 {
			return nil, fmt.Errorf("unexpected aggregate3 returnData type %s", returnField.Type())
		}

		returnData := make([]byte, returnField.Len())
		if returnField.Len() > 0 {
			reflect.Copy(reflect.ValueOf(returnData), returnField)
		}
		results[i] = aggregate3Result{
			Success:    successField.Bool(),
			ReturnData: returnData,
		}
	}
	return results, nil
}
