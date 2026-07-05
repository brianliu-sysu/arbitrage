package syncv3

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v3"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

// ReorgRecoveryService rolls back and replays pool state after a chain reorg.
type ReorgRecoveryService struct {
	config      Config
	blocks      BlockReader
	checkpoints blockchain.CheckpointRepository
	pools       marketv3.PoolRepository
	snapshots   *SnapshotService
	fetcher     LogFetcher
	parser      EventParser
	blockApply  *BlockApplyService
	readiness   *ReadinessService
}

func NewReorgRecoveryService(
	config Config,
	blocks BlockReader,
	checkpoints blockchain.CheckpointRepository,
	pools marketv3.PoolRepository,
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

func (s *ReorgRecoveryService) Recover(ctx context.Context, reorg blockchain.Reorg, poolAddresses []common.Address) error {
	for _, poolAddress := range poolAddresses {
		s.readiness.SetPoolReady(poolAddress, false)

		if err := s.snapshots.DeleteAfterBlock(ctx, poolAddress, reorg.CommonAncestor); err != nil {
			return fmt.Errorf("delete snapshots for pool %s: %w", poolAddress.Hex(), err)
		}

		pool, err := s.pools.Get(ctx, poolAddress)
		if err != nil {
			return fmt.Errorf("load pool %s: %w", poolAddress.Hex(), err)
		}
		if pool == nil {
			return fmt.Errorf("pool %s not found", poolAddress.Hex())
		}

		if snapshot, err := s.snapshots.LoadLatest(ctx, poolAddress); err != nil {
			return fmt.Errorf("load snapshot for pool %s: %w", poolAddress.Hex(), err)
		} else if snapshot != nil {
			snapshot.RestoreTo(pool)
		} else {
			pool.LastBlockNumber = reorg.CommonAncestor
		}
		pool.Status = market.PoolStatusSyncing
		if err := s.pools.Save(ctx, pool); err != nil {
			return fmt.Errorf("save pool %s: %w", poolAddress.Hex(), err)
		}

		fromBlock := reorg.CommonAncestor + 1
		toBlock := reorg.RemoteHead.Number
		if fromBlock > toBlock {
			continue
		}

		logs, err := s.fetcher.FetchLogs(ctx, LogFilter{
			PoolAddresses: []common.Address{poolAddress},
			FromBlock:     fromBlock,
			ToBlock:       toBlock,
		})
		if err != nil {
			return fmt.Errorf("fetch replay logs for pool %s: %w", poolAddress.Hex(), err)
		}
		events, err := s.parser.ParsePoolEvents(logs)
		if err != nil {
			return fmt.Errorf("parse replay events for pool %s: %w", poolAddress.Hex(), err)
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
				TrackedPools: []common.Address{poolAddress},
			}); err != nil {
				return fmt.Errorf("replay block %d for pool %s: %w", blockNumber, poolAddress.Hex(), err)
			}
		}

		pool, err = s.pools.Get(ctx, poolAddress)
		if err != nil {
			return fmt.Errorf("reload pool %s: %w", poolAddress.Hex(), err)
		}
		pool.Status = market.PoolStatusReady
		if err := s.pools.Save(ctx, pool); err != nil {
			return fmt.Errorf("save ready pool %s: %w", poolAddress.Hex(), err)
		}
		s.readiness.SetPoolReady(poolAddress, true)
	}
	return nil
}
