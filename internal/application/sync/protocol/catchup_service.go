package protocol

import (
	"context"
	"fmt"
	"sort"

	"github.com/ethereum/go-ethereum/common"
)

// CatchupTask is a single pool catchup job from a start block.
type CatchupTask[PoolID comparable] struct {
	ID        PoolID
	FromBlock uint64
}

// CatchupTaskGroup batches catchup tasks with a shared minimum start block.
type CatchupTaskGroup[PoolID comparable] struct {
	MinFromBlock uint64
	Tasks        []CatchupTask[PoolID]
}

// GroupCatchupTasks groups sorted catchup tasks for batched replay.
func GroupCatchupTasks[PoolID comparable](tasks []CatchupTask[PoolID], maxPools, maxBlockSpan uint64) []CatchupTaskGroup[PoolID] {
	fromBlocks := make([]uint64, len(tasks))
	for i, task := range tasks {
		fromBlocks[i] = task.FromBlock
	}

	indexGroups := GroupCatchupFromBlocks(fromBlocks, maxPools, maxBlockSpan)
	groups := make([]CatchupTaskGroup[PoolID], 0, len(indexGroups))
	for _, indexGroup := range indexGroups {
		groupTasks := make([]CatchupTask[PoolID], len(indexGroup.Indices))
		for i, idx := range indexGroup.Indices {
			groupTasks[i] = tasks[idx]
		}
		groups = append(groups, CatchupTaskGroup[PoolID]{
			MinFromBlock: indexGroup.MinFromBlock,
			Tasks:        groupTasks,
		})
	}
	return groups
}

// TrackedPoolsForBlock returns pools that should be synced at blockNumber.
func TrackedPoolsForBlock[PoolID comparable](
	pools []PoolID,
	fromBlocks map[PoolID]uint64,
	blockNumber uint64,
) []PoolID {
	tracked := make([]PoolID, 0, len(pools))
	for _, poolID := range pools {
		if fromBlocks[poolID] <= blockNumber {
			tracked = append(tracked, poolID)
		}
	}
	return tracked
}

// CatchupEventBatch contains parsed events and hashes observed while fetching them.
type CatchupEventBatch[Event any] struct {
	Events      []Event
	BlockHashes map[uint64]common.Hash
}

// CatchupProtocol defines the protocol-specific capabilities required by catchup.
type CatchupProtocol[PoolID comparable, Event any] interface {
	FormatPoolID(PoolID) string
	EventBlockNumber(Event) uint64
	LoadCatchupStart(context.Context, PoolID) (uint64, error)
	FetchCatchupEvents(context.Context, []PoolID, uint64, uint64) (CatchupEventBatch[Event], error)
	ApplyCatchupBlock(context.Context, ApplyBlockRequest[PoolID, Event]) error
}

// CatchupService replays historical blocks from checkpoint to a target height.
type CatchupService[PoolID comparable, Event any] struct {
	config    Config
	lifecycle *PoolLifecycleService[PoolID]
	blocks    BlockReader
	protocol  CatchupProtocol[PoolID, Event]
}

func NewCatchupService[PoolID comparable, Event any](
	config Config,
	lifecycle *PoolLifecycleService[PoolID],
	blocks BlockReader,
	protocol CatchupProtocol[PoolID, Event],
) *CatchupService[PoolID, Event] {
	return &CatchupService[PoolID, Event]{
		config:    config,
		lifecycle: lifecycle,
		blocks:    blocks,
		protocol:  protocol,
	}
}

func (s *CatchupService[PoolID, Event]) CatchUpAll(ctx context.Context, targetBlock uint64) error {
	tasks, err := s.buildCatchupTasks(ctx, targetBlock)
	if err != nil {
		return err
	}

	groups := GroupCatchupTasks(
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

func (s *CatchupService[PoolID, Event]) CatchUpPool(ctx context.Context, poolID PoolID, targetBlock uint64) error {
	fromBlock, err := s.protocol.LoadCatchupStart(ctx, poolID)
	if err != nil {
		return fmt.Errorf("load catchup start for pool %s: %w", s.protocol.FormatPoolID(poolID), err)
	}
	if fromBlock > targetBlock {
		return nil
	}

	return s.catchUpGroup(ctx, CatchupTaskGroup[PoolID]{
		MinFromBlock: fromBlock,
		Tasks:        []CatchupTask[PoolID]{{ID: poolID, FromBlock: fromBlock}},
	}, targetBlock)
}

func (s *CatchupService[PoolID, Event]) buildCatchupTasks(ctx context.Context, targetBlock uint64) ([]CatchupTask[PoolID], error) {
	tasks := make([]CatchupTask[PoolID], 0, len(s.lifecycle.ListActive()))
	for _, poolID := range s.lifecycle.ListActive() {
		fromBlock, err := s.protocol.LoadCatchupStart(ctx, poolID)
		if err != nil {
			return nil, fmt.Errorf("load catchup start for pool %s: %w", s.protocol.FormatPoolID(poolID), err)
		}
		if fromBlock > targetBlock {
			continue
		}
		tasks = append(tasks, CatchupTask[PoolID]{
			ID:        poolID,
			FromBlock: fromBlock,
		})
	}

	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].FromBlock != tasks[j].FromBlock {
			return tasks[i].FromBlock < tasks[j].FromBlock
		}
		return s.protocol.FormatPoolID(tasks[i].ID) < s.protocol.FormatPoolID(tasks[j].ID)
	})
	return tasks, nil
}

func (s *CatchupService[PoolID, Event]) catchUpGroup(ctx context.Context, group CatchupTaskGroup[PoolID], targetBlock uint64) error {
	fromBlocks := make(map[PoolID]uint64, len(group.Tasks))
	poolIDs := make([]PoolID, 0, len(group.Tasks))
	for _, task := range group.Tasks {
		fromBlocks[task.ID] = task.FromBlock
		poolIDs = append(poolIDs, task.ID)
	}

	for start := group.MinFromBlock; start <= targetBlock; {
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

func (s *CatchupService[PoolID, Event]) catchUpRange(
	ctx context.Context,
	pools []PoolID,
	fromBlocks map[PoolID]uint64,
	fromBlock, toBlock uint64,
) error {
	batch, err := s.protocol.FetchCatchupEvents(ctx, pools, fromBlock, toBlock)
	if err != nil {
		return fmt.Errorf("fetch catchup events: %w", err)
	}

	blockHashes := batch.BlockHashes
	eventsByBlock := make(map[uint64][]Event)
	for _, event := range batch.Events {
		blockNumber := s.protocol.EventBlockNumber(event)
		eventsByBlock[blockNumber] = append(eventsByBlock[blockNumber], event)
	}

	missingHeaderBlocks := make([]uint64, 0)
	for blockNumber := fromBlock; blockNumber <= toBlock; blockNumber++ {
		if len(TrackedPoolsForBlock(pools, fromBlocks, blockNumber)) == 0 {
			continue
		}
		if _, ok := blockHashes[blockNumber]; !ok {
			missingHeaderBlocks = append(missingHeaderBlocks, blockNumber)
		}
	}
	if len(missingHeaderBlocks) > 0 {
		fetched, err := FetchBlockHeaders(ctx, s.blocks, missingHeaderBlocks, s.config.CatchupHeaderConcurrency)
		if err != nil {
			return err
		}
		for blockNumber, blockHash := range fetched {
			blockHashes[blockNumber] = blockHash
		}
	}

	for blockNumber := fromBlock; blockNumber <= toBlock; blockNumber++ {
		trackedPools := TrackedPoolsForBlock(pools, fromBlocks, blockNumber)
		if len(trackedPools) == 0 {
			continue
		}

		blockHash, ok := blockHashes[blockNumber]
		if !ok {
			return fmt.Errorf("missing block hash for block %d", blockNumber)
		}
		if err := s.protocol.ApplyCatchupBlock(ctx, ApplyBlockRequest[PoolID, Event]{
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
