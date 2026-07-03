package syncapp

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

// CatchupService replays historical blocks from checkpoint to a target height.
type CatchupService struct {
	config      Config
	pools       market.PoolRepository
	checkpoints blockchain.CheckpointRepository
	fetcher     LogFetcher
	parser      EventParser
	blockApply  *BlockApplyService
	lifecycle   *PoolLifecycleService
	blocks      BlockReader
}

func NewCatchupService(
	config Config,
	pools market.PoolRepository,
	checkpoints blockchain.CheckpointRepository,
	fetcher LogFetcher,
	parser EventParser,
	blockApply *BlockApplyService,
	lifecycle *PoolLifecycleService,
	blocks BlockReader,
) *CatchupService {
	return &CatchupService{
		config:      config,
		pools:       pools,
		checkpoints: checkpoints,
		fetcher:     fetcher,
		parser:      parser,
		blockApply:  blockApply,
		lifecycle:   lifecycle,
		blocks:      blocks,
	}
}

func (s *CatchupService) CatchUpAll(ctx context.Context, targetBlock uint64) error {
	for _, poolAddress := range s.lifecycle.ListActive() {
		if err := s.CatchUpPool(ctx, poolAddress, targetBlock); err != nil {
			return err
		}
	}
	return nil
}

func (s *CatchupService) CatchUpPool(ctx context.Context, poolAddress common.Address, targetBlock uint64) error {
	checkpoint, err := s.checkpoints.Get(ctx, poolAddress)
	if err != nil {
		return fmt.Errorf("load checkpoint: %w", err)
	}

	pool, err := s.pools.Get(ctx, poolAddress)
	if err != nil {
		return fmt.Errorf("load pool: %w", err)
	}

	var checkpointBlock uint64
	if checkpoint != nil {
		checkpointBlock = checkpoint.BlockNumber
	}
	var poolLastBlock uint64
	if pool != nil {
		poolLastBlock = pool.LastBlockNumber
	}

	fromBlock := catchupStartBlock(checkpointBlock, poolLastBlock)
	if fromBlock > targetBlock {
		return nil
	}

	for start := fromBlock; start <= targetBlock; {
		end := start + s.config.CatchupBatchSize - 1
		if end > targetBlock {
			end = targetBlock
		}
		if err := s.catchUpRange(ctx, []common.Address{poolAddress}, start, end); err != nil {
			return fmt.Errorf("catch up pool %s blocks [%d,%d]: %w", poolAddress.Hex(), start, end, err)
		}
		start = end + 1
	}
	return nil
}

func (s *CatchupService) catchUpRange(ctx context.Context, pools []common.Address, fromBlock, toBlock uint64) error {
	logs, err := s.fetcher.FetchLogs(ctx, LogFilter{
		PoolAddresses: pools,
		FromBlock:     fromBlock,
		ToBlock:       toBlock,
	})
	if err != nil {
		return fmt.Errorf("fetch logs: %w", err)
	}

	events, err := s.parser.ParsePoolEvents(logs)
	if err != nil {
		return fmt.Errorf("parse events: %w", err)
	}

	eventsByBlock := groupEventsByBlock(events)
	for blockNumber := fromBlock; blockNumber <= toBlock; blockNumber++ {
		header, err := s.blocks.GetBlockHeader(ctx, blockNumber)
		if err != nil {
			return fmt.Errorf("load block header %d: %w", blockNumber, err)
		}
		if _, err := s.blockApply.ApplyBlock(ctx, ApplyBlockRequest{
			BlockNumber:  blockNumber,
			BlockHash:    header.Hash,
			Events:       eventsByBlock[blockNumber],
			TrackedPools: pools,
		}); err != nil {
			return fmt.Errorf("apply block %d: %w", blockNumber, err)
		}
	}
	return nil
}

func groupEventsByBlock(events []market.PoolEvent) map[uint64][]market.PoolEvent {
	grouped := make(map[uint64][]market.PoolEvent)
	for _, event := range events {
		blockNumber := event.Meta.BlockNumber
		grouped[blockNumber] = append(grouped[blockNumber], event)
	}
	return grouped
}

func catchupStartBlock(checkpointBlock, poolLastBlock uint64) uint64 {
	fromBlock := uint64(1)
	if checkpointBlock > 0 {
		fromBlock = checkpointBlock + 1
	}
	if poolLastBlock > 0 && poolLastBlock+1 > fromBlock {
		fromBlock = poolLastBlock + 1
	}
	return fromBlock
}
