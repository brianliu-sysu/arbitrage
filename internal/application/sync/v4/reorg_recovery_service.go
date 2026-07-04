package syncv4

import (
	"context"
	"fmt"

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
	if localHead.Number == 0 || remoteHead.Number == 0 {
		return nil, nil
	}
	if remoteHead.Number == localHead.Number+1 && remoteHead.ParentHash == localHead.Hash {
		return nil, nil
	}
	if localHead.Hash == remoteHead.Hash {
		return nil, nil
	}

	ancestor, err := s.findCommonAncestor(ctx, localHead, remoteHead)
	if err != nil {
		return nil, err
	}
	if ancestor >= localHead.Number {
		return nil, nil
	}

	reorg := blockchain.NewReorg(remoteHead.Number, localHead, remoteHead, ancestor)
	return &reorg, nil
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

func (s *ReorgRecoveryService) findCommonAncestor(
	ctx context.Context,
	localHead, remoteHead blockchain.BlockHeader,
) (uint64, error) {
	localBlock := localHead
	remoteBlock := remoteHead

	for depth := uint64(0); depth <= s.config.ReorgMaxDepth; depth++ {
		if localBlock.Number == 0 || remoteBlock.Number == 0 {
			break
		}

		if localBlock.Number == remoteBlock.Number {
			if localBlock.Hash == remoteBlock.Hash {
				return localBlock.Number, nil
			}
			var err error
			localBlock, remoteBlock, err = s.stepBack(ctx, localBlock, remoteBlock)
			if err != nil {
				return 0, err
			}
			continue
		}

		if localBlock.Number > remoteBlock.Number {
			header, err := s.blocks.GetBlockHeader(ctx, localBlock.Number-1)
			if err != nil {
				return 0, fmt.Errorf("load local block %d: %w", localBlock.Number-1, err)
			}
			localBlock = header
			continue
		}

		header, err := s.blocks.GetBlockHeader(ctx, remoteBlock.Number-1)
		if err != nil {
			return 0, fmt.Errorf("load remote block %d: %w", remoteBlock.Number-1, err)
		}
		remoteBlock = header
	}
	return 0, fmt.Errorf("common ancestor not found within depth %d", s.config.ReorgMaxDepth)
}

func (s *ReorgRecoveryService) stepBack(
	ctx context.Context,
	localBlock, remoteBlock blockchain.BlockHeader,
) (blockchain.BlockHeader, blockchain.BlockHeader, error) {
	if localBlock.Number == 0 || remoteBlock.Number == 0 {
		return localBlock, remoteBlock, nil
	}

	nextLocal, err := s.blocks.GetBlockHeader(ctx, localBlock.Number-1)
	if err != nil {
		return blockchain.BlockHeader{}, blockchain.BlockHeader{}, fmt.Errorf("load local block %d: %w", localBlock.Number-1, err)
	}
	nextRemote, err := s.blocks.GetBlockHeader(ctx, remoteBlock.Number-1)
	if err != nil {
		return blockchain.BlockHeader{}, blockchain.BlockHeader{}, fmt.Errorf("load remote block %d: %w", remoteBlock.Number-1, err)
	}
	return nextLocal, nextRemote, nil
}
