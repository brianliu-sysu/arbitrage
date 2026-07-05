package syncv4

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
)

// ReorgRecoveryService rolls back and replays V4 pool state after a chain reorg.
type ReorgRecoveryService struct {
	config      Config
	blocks      BlockReader
	checkpoints blockchain.V4CheckpointRepository
	pools       marketv4.PoolRepository
	snapshots   *SnapshotService
	fetcher     LogFetcher
	parser      EventParser
	blockApply  *BlockApplyService
	readiness   *ReadinessService
}

func NewReorgRecoveryService(
	config Config,
	blocks BlockReader,
	checkpoints blockchain.V4CheckpointRepository,
	pools marketv4.PoolRepository,
	snapshots *SnapshotService,
	fetcher LogFetcher,
	parser EventParser,
	blockApply *BlockApplyService,
	readiness *ReadinessService,
) *ReorgRecoveryService {
	return &ReorgRecoveryService{
		config:      config,
		blocks:      blocks,
		checkpoints: checkpoints,
		pools:       pools,
		snapshots:   snapshots,
		fetcher:     fetcher,
		parser:      parser,
		blockApply:  blockApply,
		readiness:   readiness,
	}
}

func (s *ReorgRecoveryService) DetectReorg(ctx context.Context, localHead, remoteHead blockchain.BlockHeader) (*blockchain.Reorg, error) {
	return syncapp.DetectReorg(ctx, s.blocks, s.config.ReorgMaxDepth, localHead, remoteHead)
}

func (s *ReorgRecoveryService) Recover(ctx context.Context, reorg blockchain.Reorg, poolIDs []marketv4.PoolID) error {
	for _, poolID := range poolIDs {
		s.readiness.SetPoolReady(poolID, false)

		if err := s.snapshots.DeleteAfterBlock(ctx, poolID, reorg.CommonAncestor); err != nil {
			return fmt.Errorf("delete snapshots for pool %s: %w", poolID, err)
		}

		pool, err := s.pools.Get(ctx, poolID)
		if err != nil {
			return fmt.Errorf("load pool %s: %w", poolID, err)
		}
		if pool == nil {
			return fmt.Errorf("pool %s not found", poolID)
		}

		if snapshot, err := s.snapshots.LoadLatest(ctx, poolID); err != nil {
			return fmt.Errorf("load snapshot for pool %s: %w", poolID, err)
		} else if snapshot != nil {
			snapshot.RestoreTo(pool)
		} else {
			pool.LastBlockNumber = reorg.CommonAncestor
		}
		pool.Status = market.PoolStatusSyncing
		if err := s.pools.Save(ctx, pool); err != nil {
			return fmt.Errorf("save pool %s: %w", poolID, err)
		}

		fromBlock := reorg.CommonAncestor + 1
		toBlock := reorg.RemoteHead.Number
		if fromBlock > toBlock {
			continue
		}

		logs, err := s.fetcher.FetchLogs(ctx, LogFilter{
			PoolIDs:   []marketv4.PoolID{poolID},
			FromBlock: fromBlock,
			ToBlock:   toBlock,
		})
		if err != nil {
			return fmt.Errorf("fetch replay logs for pool %s: %w", poolID, err)
		}
		events, err := s.parser.ParsePoolEvents(logs)
		if err != nil {
			return fmt.Errorf("parse replay events for pool %s: %w", poolID, err)
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
				TrackedPools: []marketv4.PoolID{poolID},
			}); err != nil {
				return fmt.Errorf("replay block %d for pool %s: %w", blockNumber, poolID, err)
			}
		}

		pool, err = s.pools.Get(ctx, poolID)
		if err != nil {
			return fmt.Errorf("reload pool %s: %w", poolID, err)
		}
		pool.Status = market.PoolStatusReady
		if err := s.pools.Save(ctx, pool); err != nil {
			return fmt.Errorf("save ready pool %s: %w", poolID, err)
		}
		s.readiness.SetPoolReady(poolID, true)
	}
	return nil
}
