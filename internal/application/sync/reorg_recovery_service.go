package syncapp

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

// ReorgRecoveryHooks configures protocol-specific reorg recovery behavior.
type ReorgRecoveryHooks[PoolID comparable, Event any, Pool any] struct {
	FormatPoolID       func(PoolID) string
	DeleteSnapshotsAfter func(context.Context, PoolID, uint64) error
	LoadPool           func(context.Context, PoolID) (Pool, error)
	SavePool           func(context.Context, Pool) error
	IsNilPool           func(Pool) bool
	RestorePoolState   func(context.Context, Pool, PoolID, uint64) (uint64, error)
	SetPoolStatus      func(Pool, market.PoolStatus)
	SetPoolReady       func(PoolID, bool)
	FetchReplayLogs    func(context.Context, PoolID, uint64, uint64) ([]RawLog, error)
	ParseEvents        func([]RawLog) ([]Event, error)
	EventBlockNumber   func(Event) uint64
	ApplyBlock         func(context.Context, uint64, common.Hash, []Event, []PoolID) error
}

// ReorgRecoveryService rolls back and replays pool state after a chain reorg.
type ReorgRecoveryService[PoolID comparable, Event any, Pool any] struct {
	reorgMaxDepth uint64
	blocks        BlockReader
	hooks         ReorgRecoveryHooks[PoolID, Event, Pool]
}

// NewReorgRecoveryService builds a reorg recovery service with protocol hooks.
func NewReorgRecoveryService[PoolID comparable, Event any, Pool any](
	reorgMaxDepth uint64,
	blocks BlockReader,
	hooks ReorgRecoveryHooks[PoolID, Event, Pool],
) *ReorgRecoveryService[PoolID, Event, Pool] {
	return &ReorgRecoveryService[PoolID, Event, Pool]{
		reorgMaxDepth: reorgMaxDepth,
		blocks:        blocks,
		hooks:         hooks,
	}
}

// DetectReorg compares local and remote heads and returns a reorg when found.
func (s *ReorgRecoveryService[PoolID, Event, Pool]) DetectReorg(
	ctx context.Context,
	localHead, remoteHead blockchain.BlockHeader,
) (*blockchain.Reorg, error) {
	return DetectReorg(ctx, s.blocks, s.reorgMaxDepth, localHead, remoteHead)
}

// Recover rolls back affected pools to the common ancestor and replays blocks.
func (s *ReorgRecoveryService[PoolID, Event, Pool]) Recover(ctx context.Context, reorg blockchain.Reorg, poolIDs []PoolID) error {
	for _, poolID := range poolIDs {
		s.hooks.SetPoolReady(poolID, false)

		if err := s.hooks.DeleteSnapshotsAfter(ctx, poolID, reorg.CommonAncestor); err != nil {
			return fmt.Errorf("delete snapshots for pool %s: %w", s.hooks.FormatPoolID(poolID), err)
		}

		pool, err := s.hooks.LoadPool(ctx, poolID)
		if err != nil {
			return fmt.Errorf("load pool %s: %w", s.hooks.FormatPoolID(poolID), err)
		}
		if s.hooks.IsNilPool(pool) {
			return fmt.Errorf("pool %s not found", s.hooks.FormatPoolID(poolID))
		}

		fromBlock, err := s.hooks.RestorePoolState(ctx, pool, poolID, reorg.CommonAncestor)
		if err != nil {
			return fmt.Errorf("restore pool %s: %w", s.hooks.FormatPoolID(poolID), err)
		}

		s.hooks.SetPoolStatus(pool, market.PoolStatusSyncing)
		if err := s.hooks.SavePool(ctx, pool); err != nil {
			return fmt.Errorf("save pool %s: %w", s.hooks.FormatPoolID(poolID), err)
		}

		toBlock := reorg.RemoteHead.Number
		if fromBlock > toBlock {
			continue
		}

		logs, err := s.hooks.FetchReplayLogs(ctx, poolID, fromBlock, toBlock)
		if err != nil {
			return fmt.Errorf("fetch replay logs for pool %s: %w", s.hooks.FormatPoolID(poolID), err)
		}
		events, err := s.hooks.ParseEvents(logs)
		if err != nil {
			return fmt.Errorf("parse replay events for pool %s: %w", s.hooks.FormatPoolID(poolID), err)
		}

		eventsByBlock := GroupEventsByBlock(events, s.hooks.EventBlockNumber)
		for blockNumber := fromBlock; blockNumber <= toBlock; blockNumber++ {
			header, err := s.blocks.GetBlockHeader(ctx, blockNumber)
			if err != nil {
				return fmt.Errorf("load block header %d: %w", blockNumber, err)
			}
			if err := s.hooks.ApplyBlock(ctx, blockNumber, header.Hash, eventsByBlock[blockNumber], []PoolID{poolID}); err != nil {
				return fmt.Errorf("replay block %d for pool %s: %w", blockNumber, s.hooks.FormatPoolID(poolID), err)
			}
		}

		pool, err = s.hooks.LoadPool(ctx, poolID)
		if err != nil {
			return fmt.Errorf("reload pool %s: %w", s.hooks.FormatPoolID(poolID), err)
		}
		s.hooks.SetPoolStatus(pool, market.PoolStatusReady)
		if err := s.hooks.SavePool(ctx, pool); err != nil {
			return fmt.Errorf("save ready pool %s: %w", s.hooks.FormatPoolID(poolID), err)
		}
		s.hooks.SetPoolReady(poolID, true)
	}
	return nil
}
