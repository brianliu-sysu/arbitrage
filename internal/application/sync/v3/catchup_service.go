package syncv3

import (
	"context"
	"fmt"
	"sort"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v3"
	"github.com/ethereum/go-ethereum/common"
)

// CatchupService replays historical blocks from checkpoint to a target height.
type CatchupService struct {
	config      Config
	pools       marketv3.PoolRepository
	checkpoints blockchain.CheckpointRepository
	fetcher     LogFetcher
	parser      EventParser
	blockApply  *BlockApplyService
	lifecycle   *PoolLifecycleService
	blocks      BlockReader
}

func NewCatchupService(
	config Config,
	pools marketv3.PoolRepository,
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

type catchupPoolTask struct {
	address   common.Address
	fromBlock uint64
}

type catchupPoolGroup struct {
	minFromBlock uint64
	tasks        []catchupPoolTask
}

func (s *CatchupService) CatchUpAll(ctx context.Context, targetBlock uint64) error {
	tasks, err := s.buildCatchupTasks(ctx, targetBlock)
	if err != nil {
		return err
	}

	groups := groupCatchupPools(
		tasks,
		s.config.CatchupPoolGroupSize,
		s.config.CatchupBlockSpan,
	)
	for _, group := range groups {
		if err := s.catchUpGroup(ctx, group, targetBlock); err != nil {
			return err
		}
	}
	return nil
}

func (s *CatchupService) CatchUpPool(ctx context.Context, poolAddress common.Address, targetBlock uint64) error {
	fromBlock, err := s.poolCatchupStartBlock(ctx, poolAddress)
	if err != nil {
		return fmt.Errorf("load catchup start for pool %s: %w", poolAddress.Hex(), err)
	}
	if fromBlock > targetBlock {
		return nil
	}

	return s.catchUpGroup(ctx, catchupPoolGroup{
		minFromBlock: fromBlock,
		tasks:        []catchupPoolTask{{address: poolAddress, fromBlock: fromBlock}},
	}, targetBlock)
}

func (s *CatchupService) buildCatchupTasks(ctx context.Context, targetBlock uint64) ([]catchupPoolTask, error) {
	tasks := make([]catchupPoolTask, 0, len(s.lifecycle.ListActive()))
	for _, poolAddress := range s.lifecycle.ListActive() {
		fromBlock, err := s.poolCatchupStartBlock(ctx, poolAddress)
		if err != nil {
			return nil, fmt.Errorf("load catchup start for pool %s: %w", poolAddress.Hex(), err)
		}
		if fromBlock > targetBlock {
			continue
		}
		tasks = append(tasks, catchupPoolTask{
			address:   poolAddress,
			fromBlock: fromBlock,
		})
	}

	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].fromBlock != tasks[j].fromBlock {
			return tasks[i].fromBlock < tasks[j].fromBlock
		}
		return tasks[i].address.Hex() < tasks[j].address.Hex()
	})
	return tasks, nil
}

func (s *CatchupService) poolCatchupStartBlock(ctx context.Context, poolAddress common.Address) (uint64, error) {
	checkpoint, err := s.checkpoints.Get(ctx, poolAddress)
	if err != nil {
		return 0, fmt.Errorf("load checkpoint: %w", err)
	}

	pool, err := s.pools.Get(ctx, poolAddress)
	if err != nil {
		return 0, fmt.Errorf("load pool: %w", err)
	}

	var checkpointBlock uint64
	if checkpoint != nil {
		checkpointBlock = checkpoint.BlockNumber
	}
	var poolLastBlock uint64
	if pool != nil {
		poolLastBlock = pool.LastBlockNumber
	}
	return syncapp.CatchupStartBlock(checkpointBlock, poolLastBlock), nil
}

func groupCatchupPools(tasks []catchupPoolTask, maxPools, maxBlockSpan uint64) []catchupPoolGroup {
	if len(tasks) == 0 {
		return nil
	}
	if maxPools == 0 {
		maxPools = 100
	}
	if maxBlockSpan == 0 {
		maxBlockSpan = 100
	}

	groups := make([]catchupPoolGroup, 0, len(tasks))
	current := catchupPoolGroup{
		minFromBlock: tasks[0].fromBlock,
		tasks:        []catchupPoolTask{tasks[0]},
	}

	for i := 1; i < len(tasks); i++ {
		task := tasks[i]
		span := task.fromBlock - current.minFromBlock
		if uint64(len(current.tasks)) >= maxPools || span > maxBlockSpan {
			groups = append(groups, current)
			current = catchupPoolGroup{
				minFromBlock: task.fromBlock,
				tasks:        []catchupPoolTask{task},
			}
			continue
		}
		current.tasks = append(current.tasks, task)
	}
	groups = append(groups, current)
	return groups
}

func (s *CatchupService) catchUpGroup(ctx context.Context, group catchupPoolGroup, targetBlock uint64) error {
	fromBlocks := make(map[common.Address]uint64, len(group.tasks))
	poolAddresses := make([]common.Address, 0, len(group.tasks))
	for _, task := range group.tasks {
		fromBlocks[task.address] = task.fromBlock
		poolAddresses = append(poolAddresses, task.address)
	}

	for start := group.minFromBlock; start <= targetBlock; {
		end := start + s.config.CatchupBatchSize - 1
		if end > targetBlock {
			end = targetBlock
		}
		if err := s.catchUpRange(ctx, poolAddresses, fromBlocks, start, end); err != nil {
			return fmt.Errorf("catch up blocks [%d,%d]: %w", start, end, err)
		}
		start = end + 1
	}
	return nil
}

func (s *CatchupService) catchUpRange(
	ctx context.Context,
	pools []common.Address,
	fromBlocks map[common.Address]uint64,
	fromBlock, toBlock uint64,
) error {
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

	blockHashes := syncapp.BlockHashesFromLogs(logs)
	eventsByBlock := groupEventsByBlock(events)

	missingHeaderBlocks := make([]uint64, 0)
	for blockNumber := fromBlock; blockNumber <= toBlock; blockNumber++ {
		if len(trackedPoolsForBlock(pools, fromBlocks, blockNumber)) == 0 {
			continue
		}
		if _, ok := blockHashes[blockNumber]; !ok {
			missingHeaderBlocks = append(missingHeaderBlocks, blockNumber)
		}
	}
	if len(missingHeaderBlocks) > 0 {
		fetched, err := syncapp.FetchBlockHeaders(ctx, s.blocks, missingHeaderBlocks, s.config.CatchupHeaderConcurrency)
		if err != nil {
			return err
		}
		for blockNumber, blockHash := range fetched {
			blockHashes[blockNumber] = blockHash
		}
	}

	for blockNumber := fromBlock; blockNumber <= toBlock; blockNumber++ {
		trackedPools := trackedPoolsForBlock(pools, fromBlocks, blockNumber)
		if len(trackedPools) == 0 {
			continue
		}

		blockHash, ok := blockHashes[blockNumber]
		if !ok {
			return fmt.Errorf("missing block hash for block %d", blockNumber)
		}
		if _, err := s.blockApply.ApplyBlock(ctx, ApplyBlockRequest{
			BlockNumber:      blockNumber,
			BlockHash:        blockHash,
			Events:           eventsByBlock[blockNumber],
			TrackedPools:     trackedPools,
			SuppressListener: true,
		}); err != nil {
			return fmt.Errorf("apply block %d: %w", blockNumber, err)
		}
	}
	return nil
}

func trackedPoolsForBlock(
	pools []common.Address,
	fromBlocks map[common.Address]uint64,
	blockNumber uint64,
) []common.Address {
	tracked := make([]common.Address, 0, len(pools))
	for _, poolAddress := range pools {
		if fromBlocks[poolAddress] <= blockNumber {
			tracked = append(tracked, poolAddress)
		}
	}
	return tracked
}

func groupEventsByBlock(events []marketv3.PoolEvent) map[uint64][]marketv3.PoolEvent {
	grouped := make(map[uint64][]marketv3.PoolEvent)
	for _, event := range events {
		blockNumber := event.Meta.BlockNumber
		grouped[blockNumber] = append(grouped[blockNumber], event)
	}
	return grouped
}
