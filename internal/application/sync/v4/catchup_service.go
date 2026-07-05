package syncv4

import (
	"context"
	"fmt"
	"sort"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
)

// CatchupService replays historical PoolManager blocks from checkpoint to a target height.
type CatchupService struct {
	config      Config
	pools       marketv4.PoolRepository
	checkpoints blockchain.V4CheckpointRepository
	fetcher     LogFetcher
	parser      EventParser
	blockApply  *BlockApplyService
	lifecycle   *PoolLifecycleService
	blocks      BlockReader
}

func NewCatchupService(
	config Config,
	pools marketv4.PoolRepository,
	checkpoints blockchain.V4CheckpointRepository,
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
	id        marketv4.PoolID
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

func (s *CatchupService) CatchUpPool(ctx context.Context, poolID marketv4.PoolID, targetBlock uint64) error {
	fromBlock, err := s.poolCatchupStartBlock(ctx, poolID)
	if err != nil {
		return fmt.Errorf("load catchup start for pool %s: %w", poolID, err)
	}
	if fromBlock > targetBlock {
		return nil
	}

	return s.catchUpGroup(ctx, catchupPoolGroup{
		minFromBlock: fromBlock,
		tasks:        []catchupPoolTask{{id: poolID, fromBlock: fromBlock}},
	}, targetBlock)
}

func (s *CatchupService) buildCatchupTasks(ctx context.Context, targetBlock uint64) ([]catchupPoolTask, error) {
	tasks := make([]catchupPoolTask, 0, len(s.lifecycle.ListActive()))
	for _, poolID := range s.lifecycle.ListActive() {
		fromBlock, err := s.poolCatchupStartBlock(ctx, poolID)
		if err != nil {
			return nil, fmt.Errorf("load catchup start for pool %s: %w", poolID, err)
		}
		if fromBlock > targetBlock {
			continue
		}
		tasks = append(tasks, catchupPoolTask{
			id:        poolID,
			fromBlock: fromBlock,
		})
	}

	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].fromBlock != tasks[j].fromBlock {
			return tasks[i].fromBlock < tasks[j].fromBlock
		}
		return tasks[i].id.String() < tasks[j].id.String()
	})
	return tasks, nil
}

func (s *CatchupService) poolCatchupStartBlock(ctx context.Context, poolID marketv4.PoolID) (uint64, error) {
	checkpoint, err := s.checkpoints.Get(ctx, poolID)
	if err != nil {
		return 0, fmt.Errorf("load checkpoint: %w", err)
	}

	pool, err := s.pools.Get(ctx, poolID)
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
	fromBlocks := make([]uint64, len(tasks))
	for i, task := range tasks {
		fromBlocks[i] = task.fromBlock
	}

	indexGroups := syncapp.GroupCatchupFromBlocks(fromBlocks, maxPools, maxBlockSpan)
	groups := make([]catchupPoolGroup, 0, len(indexGroups))
	for _, indexGroup := range indexGroups {
		groupTasks := make([]catchupPoolTask, len(indexGroup.Indices))
		for i, idx := range indexGroup.Indices {
			groupTasks[i] = tasks[idx]
		}
		groups = append(groups, catchupPoolGroup{
			minFromBlock: indexGroup.MinFromBlock,
			tasks:        groupTasks,
		})
	}
	return groups
}

func (s *CatchupService) catchUpGroup(ctx context.Context, group catchupPoolGroup, targetBlock uint64) error {
	fromBlocks := make(map[marketv4.PoolID]uint64, len(group.tasks))
	poolIDs := make([]marketv4.PoolID, 0, len(group.tasks))
	for _, task := range group.tasks {
		fromBlocks[task.id] = task.fromBlock
		poolIDs = append(poolIDs, task.id)
	}

	for start := group.minFromBlock; start <= targetBlock; {
		end := start + s.config.CatchupBatchSize - 1
		if end > targetBlock {
			end = targetBlock
		}
		if err := s.catchUpRange(ctx, poolIDs, fromBlocks, start, end); err != nil {
			return fmt.Errorf("catch up blocks [%d,%d]: %w", start, end, err)
		}
		start = end + 1
	}
	return nil
}

func (s *CatchupService) catchUpRange(
	ctx context.Context,
	pools []marketv4.PoolID,
	fromBlocks map[marketv4.PoolID]uint64,
	fromBlock, toBlock uint64,
) error {
	logs, err := s.fetcher.FetchLogs(ctx, LogFilter{
		PoolIDs:   pools,
		FromBlock: fromBlock,
		ToBlock:   toBlock,
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
	pools []marketv4.PoolID,
	fromBlocks map[marketv4.PoolID]uint64,
	blockNumber uint64,
) []marketv4.PoolID {
	tracked := make([]marketv4.PoolID, 0, len(pools))
	for _, poolID := range pools {
		if fromBlocks[poolID] <= blockNumber {
			tracked = append(tracked, poolID)
		}
	}
	return tracked
}

func groupEventsByBlock(events []marketv4.PoolEvent) map[uint64][]marketv4.PoolEvent {
	grouped := make(map[uint64][]marketv4.PoolEvent)
	for _, event := range events {
		blockNumber := event.Meta.BlockNumber
		grouped[blockNumber] = append(grouped[blockNumber], event)
	}
	return grouped
}
