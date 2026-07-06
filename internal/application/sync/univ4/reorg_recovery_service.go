package syncv4

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
)

// ReorgRecoveryService rolls back and replays V4 pool state after a chain reorg.
type ReorgRecoveryService struct {
	config      Config
	blocks      BlockReader
	checkpoints blockchain.V4CheckpointRepository
	pools       marketv4.PoolRepository
	registry    marketv4.PoolRegistry
	bootstrap   PoolBootstrapReader
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
	registry marketv4.PoolRegistry,
	bootstrap PoolBootstrapReader,
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
		registry:    registry,
		bootstrap:   bootstrap,
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

		fromBlock, err := s.restorePoolState(ctx, pool, poolID, reorg.CommonAncestor)
		if err != nil {
			return fmt.Errorf("restore pool %s: %w", poolID, err)
		}

		pool.Status = market.PoolStatusSyncing
		if err := s.pools.Save(ctx, pool); err != nil {
			return fmt.Errorf("save pool %s: %w", poolID, err)
		}

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

func (s *ReorgRecoveryService) restorePoolState(ctx context.Context, pool *marketv4.Pool, poolID marketv4.PoolID, ancestor uint64) (uint64, error) {
	snapshot, err := s.snapshots.LoadAtOrBefore(ctx, poolID, ancestor)
	if err != nil {
		return 0, err
	}
	if snapshot != nil {
		snapshot.RestoreTo(pool)
		pool.LastBlockNumber = snapshot.BlockNumber
		return syncapp.ReorgReplayFromBlock(snapshot.BlockNumber, ancestor, true), nil
	}

	if s.bootstrap != nil && s.registry != nil {
		key, err := s.registry.GetKey(ctx, poolID)
		if err != nil {
			return 0, fmt.Errorf("resolve pool key: %w", err)
		}
		data, err := s.bootstrap.ReadBootstrapData(ctx, poolID, key, ancestor)
		if err != nil {
			return 0, fmt.Errorf("read chain bootstrap data: %w", err)
		}
		pool.Key = data.Key
		applyBootstrapData(pool, data)
		pool.LastBlockNumber = ancestor
		return syncapp.ReorgReplayFromBlock(0, ancestor, false), nil
	}

	pool.LastBlockNumber = ancestor
	return syncapp.ReorgReplayFromBlock(0, ancestor, false), nil
}
