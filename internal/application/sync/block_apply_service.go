package syncapp

import (
	"context"
	"fmt"
	"sort"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

// BlockApplyService applies pool events for a single block.
type BlockApplyService struct {
	pools       market.PoolRepository
	checkpoints blockchain.CheckpointRepository
	snapshots   *SnapshotService
	readiness   *ReadinessService
	listener    ChangedPoolsListener
}

func NewBlockApplyService(
	pools market.PoolRepository,
	checkpoints blockchain.CheckpointRepository,
	snapshots *SnapshotService,
	readiness *ReadinessService,
	listener ChangedPoolsListener,
) *BlockApplyService {
	if listener == nil {
		listener = NopChangedPoolsListener{}
	}
	return &BlockApplyService{
		pools:       pools,
		checkpoints: checkpoints,
		snapshots:   snapshots,
		readiness:   readiness,
		listener:    listener,
	}
}

type ApplyBlockRequest struct {
	BlockNumber  uint64
	BlockHash    common.Hash
	Events       []market.PoolEvent
	TrackedPools []common.Address
}

type ApplyBlockResult struct {
	BlockNumber  uint64
	ChangedPools []common.Address
}

func (s *BlockApplyService) ApplyBlock(ctx context.Context, req ApplyBlockRequest) (ApplyBlockResult, error) {
	if req.BlockNumber == 0 {
		return ApplyBlockResult{}, fmt.Errorf("block number must be positive")
	}

	grouped := groupEventsByPool(req.Events)
	changedSet := make(map[common.Address]struct{}, len(grouped))
	changed := make([]common.Address, 0, len(grouped))

	for poolAddress, events := range grouped {
		changedSet[poolAddress] = struct{}{}
		sort.Slice(events, func(i, j int) bool {
			if events[i].Meta.TxIndex != events[j].Meta.TxIndex {
				return events[i].Meta.TxIndex < events[j].Meta.TxIndex
			}
			return events[i].Meta.LogIndex < events[j].Meta.LogIndex
		})

		pool, err := s.pools.Get(ctx, poolAddress)
		if err != nil {
			return ApplyBlockResult{}, fmt.Errorf("load pool %s: %w", poolAddress.Hex(), err)
		}
		if pool == nil {
			return ApplyBlockResult{}, fmt.Errorf("pool %s not found", poolAddress.Hex())
		}

		for _, event := range events {
			if err := pool.Apply(event); err != nil {
				pool.Status = market.PoolStatusError
				_ = s.pools.Save(ctx, pool)
				s.readiness.SetPoolReady(poolAddress, false)
				return ApplyBlockResult{}, fmt.Errorf("apply event on pool %s: %w", poolAddress.Hex(), err)
			}
		}

		if err := s.pools.Save(ctx, pool); err != nil {
			return ApplyBlockResult{}, fmt.Errorf("save pool %s: %w", poolAddress.Hex(), err)
		}
		if err := s.checkpoints.Save(ctx, &blockchain.Checkpoint{
			PoolAddress: poolAddress,
			BlockNumber: req.BlockNumber,
			BlockHash:   req.BlockHash,
		}); err != nil {
			return ApplyBlockResult{}, fmt.Errorf("save checkpoint for pool %s: %w", poolAddress.Hex(), err)
		}

		if s.snapshots != nil {
			if err := s.snapshots.MaybeCreateSnapshot(ctx, pool, req.BlockNumber); err != nil {
				return ApplyBlockResult{}, fmt.Errorf("snapshot pool %s: %w", poolAddress.Hex(), err)
			}
		}

		changed = append(changed, poolAddress)
	}

	for _, poolAddress := range req.TrackedPools {
		if _, ok := changedSet[poolAddress]; ok {
			continue
		}
		if err := s.advanceCheckpoint(ctx, poolAddress, req.BlockNumber, req.BlockHash); err != nil {
			return ApplyBlockResult{}, err
		}
		changed = append(changed, poolAddress)
	}

	sort.Slice(changed, func(i, j int) bool {
		return changed[i].Hex() < changed[j].Hex()
	})

	if len(changed) > 0 {
		if err := s.listener.OnPoolsChanged(ctx, req.BlockNumber, changed); err != nil {
			return ApplyBlockResult{}, fmt.Errorf("notify changed pools: %w", err)
		}
	}

	return ApplyBlockResult{
		BlockNumber:  req.BlockNumber,
		ChangedPools: changed,
	}, nil
}

func (s *BlockApplyService) MarkPoolsReady(ctx context.Context, poolAddresses []common.Address) error {
	for _, poolAddress := range poolAddresses {
		pool, err := s.pools.Get(ctx, poolAddress)
		if err != nil {
			return fmt.Errorf("load pool %s: %w", poolAddress.Hex(), err)
		}
		if pool == nil {
			return fmt.Errorf("pool %s not found", poolAddress.Hex())
		}
		pool.Status = market.PoolStatusReady
		if err := s.pools.Save(ctx, pool); err != nil {
			return fmt.Errorf("save ready pool %s: %w", poolAddress.Hex(), err)
		}
		s.readiness.SetPoolReady(poolAddress, true)
	}
	return nil
}

func groupEventsByPool(events []market.PoolEvent) map[common.Address][]market.PoolEvent {
	grouped := make(map[common.Address][]market.PoolEvent)
	for _, event := range events {
		poolAddress := event.Meta.PoolAddress
		grouped[poolAddress] = append(grouped[poolAddress], event)
	}
	return grouped
}

func (s *BlockApplyService) advanceCheckpoint(
	ctx context.Context,
	poolAddress common.Address,
	blockNumber uint64,
	blockHash common.Hash,
) error {
	pool, err := s.pools.Get(ctx, poolAddress)
	if err != nil {
		return fmt.Errorf("load pool %s: %w", poolAddress.Hex(), err)
	}
	if pool == nil {
		return fmt.Errorf("pool %s not found", poolAddress.Hex())
	}
	if blockNumber > pool.LastBlockNumber {
		pool.LastBlockNumber = blockNumber
	}
	if pool.Status == market.PoolStatusCatchingUp {
		pool.Status = market.PoolStatusSyncing
	}
	if err := s.pools.Save(ctx, pool); err != nil {
		return fmt.Errorf("save pool %s: %w", poolAddress.Hex(), err)
	}
	return s.checkpoints.Save(ctx, &blockchain.Checkpoint{
		PoolAddress: poolAddress,
		BlockNumber: blockNumber,
		BlockHash:   blockHash,
	})
}
