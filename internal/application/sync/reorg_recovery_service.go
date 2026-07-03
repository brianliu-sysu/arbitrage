package syncapp

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

// ReorgRecoveryService rolls back and replays pool state after a chain reorg.
type ReorgRecoveryService struct {
	config      Config
	blocks      BlockReader
	checkpoints blockchain.CheckpointRepository
	pools       market.PoolRepository
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
	pools market.PoolRepository,
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
