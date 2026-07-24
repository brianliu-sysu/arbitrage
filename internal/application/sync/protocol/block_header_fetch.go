package protocol

import (
	"context"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

func blockHashesFromLogs(logs []RawLog) map[uint64]common.Hash {
	hashes := make(map[uint64]common.Hash)
	for _, log := range logs {
		if log.BlockHash == (common.Hash{}) {
			continue
		}
		hashes[log.BlockNumber] = log.BlockHash
	}
	return hashes
}

func fetchBlockHeaders(
	ctx context.Context,
	reader BlockReader,
	blockNumbers []uint64,
	concurrency int,
) (map[uint64]common.Hash, error) {
	if len(blockNumbers) == 0 {
		return map[uint64]common.Hash{}, nil
	}
	if concurrency <= 0 {
		concurrency = 16
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(map[uint64]common.Hash, len(blockNumbers))
	var mu sync.Mutex
	var wg sync.WaitGroup
	errCh := make(chan error, 1)
	sem := make(chan struct{}, concurrency)

	for _, blockNumber := range blockNumbers {
		wg.Add(1)
		go func(blockNumber uint64) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			header, err := reader.GetBlockHeader(ctx, blockNumber)
			if err != nil {
				select {
				case errCh <- fmt.Errorf("load block header %d: %w", blockNumber, err):
					cancel()
				default:
				}
				return
			}

			mu.Lock()
			results[blockNumber] = header.Hash
			mu.Unlock()
		}(blockNumber)
	}

	wg.Wait()

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	if len(results) != len(blockNumbers) {
		return nil, fmt.Errorf("expected %d block headers, fetched %d", len(blockNumbers), len(results))
	}
	return results, nil
}

// BlockHashesFromLogs extracts block hashes from raw logs.
func BlockHashesFromLogs(logs []RawLog) map[uint64]common.Hash {
	return blockHashesFromLogs(logs)
}

// FetchBlockHeaders loads block hashes for the given block numbers.
func FetchBlockHeaders(
	ctx context.Context,
	reader BlockReader,
	blockNumbers []uint64,
	concurrency int,
) (map[uint64]common.Hash, error) {
	return fetchBlockHeaders(ctx, reader, blockNumbers, concurrency)
}

// CatchupStartBlock returns the first block to replay from checkpoint and pool progress.
func CatchupStartBlock(checkpointBlock, poolLastBlock uint64) uint64 {
	fromBlock := uint64(1)
	if checkpointBlock > 0 {
		fromBlock = checkpointBlock + 1
	}
	if poolLastBlock > 0 && poolLastBlock+1 > fromBlock {
		fromBlock = poolLastBlock + 1
	}
	return fromBlock
}
