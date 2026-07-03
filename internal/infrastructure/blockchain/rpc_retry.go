package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

const (
	rpcRetryAttempts = 4
	rpcRetryBackoff  = 250 * time.Millisecond
)

func blockNumberOrLatest(blockNumber uint64) *big.Int {
	if blockNumber == 0 {
		return nil
	}
	return new(big.Int).SetUint64(blockNumber)
}

func callContractWithRetry(
	ctx context.Context,
	client *EthClient,
	to common.Address,
	data []byte,
	blockNumber uint64,
) ([]byte, error) {
	blocks := callBlockCandidates(blockNumber)
	var lastErr error

	for attempt := 0; attempt < rpcRetryAttempts; attempt++ {
		for _, block := range blocks {
			output, err := client.client.CallContract(ctx, ethereum.CallMsg{
				To:   &to,
				Data: data,
			}, block)
			if err != nil {
				lastErr = err
				continue
			}
			if len(output) > 0 {
				return output, nil
			}
			lastErr = fmt.Errorf("empty response at block %s", blockLabel(block))
		}
		if attempt+1 < rpcRetryAttempts {
			if err := sleepWithContext(ctx, rpcRetryBackoff*time.Duration(attempt+1)); err != nil {
				return nil, err
			}
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("empty response")
	}
	return nil, fmt.Errorf("call contract after %d attempts: %w", rpcRetryAttempts, lastErr)
}

func codeAtWithRetry(
	ctx context.Context,
	client *EthClient,
	address common.Address,
	blockNumber uint64,
) ([]byte, error) {
	blocks := callBlockCandidates(blockNumber)
	var lastErr error

	for attempt := 0; attempt < rpcRetryAttempts; attempt++ {
		for _, block := range blocks {
			code, err := client.client.CodeAt(ctx, address, block)
			if err != nil {
				lastErr = err
				continue
			}
			if len(code) > 0 {
				return code, nil
			}
			lastErr = fmt.Errorf("empty bytecode at block %s", blockLabel(block))
		}
		if attempt+1 < rpcRetryAttempts {
			if err := sleepWithContext(ctx, rpcRetryBackoff*time.Duration(attempt+1)); err != nil {
				return nil, err
			}
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("empty bytecode")
	}
	return nil, fmt.Errorf("code at after %d attempts: %w", rpcRetryAttempts, lastErr)
}

func callBlockCandidates(blockNumber uint64) []*big.Int {
	if blockNumber == 0 {
		return []*big.Int{nil}
	}
	latest := blockNumberOrLatest(blockNumber)
	if blockNumber <= 1 {
		return []*big.Int{latest, nil}
	}
	return []*big.Int{
		latest,
		new(big.Int).SetUint64(blockNumber - 1),
		nil,
	}
}

func blockLabel(block *big.Int) string {
	if block == nil {
		return "latest"
	}
	return block.String()
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
