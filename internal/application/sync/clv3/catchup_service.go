package clv3sync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

type CatchupService = syncapp.CatchupService[common.Address, marketclv3.PoolEvent]

type catchupProtocol struct {
	pools       PoolRepository
	checkpoints blockchain.CheckpointRepository
	fetcher     LogFetcher
	parser      EventParser
	blockApply  *BlockApplyService
}

func (p *catchupProtocol) FormatPoolID(poolID common.Address) string {
	return poolID.Hex()
}

func (p *catchupProtocol) EventBlockNumber(event marketclv3.PoolEvent) uint64 {
	return event.Meta.BlockNumber
}

func (p *catchupProtocol) LoadCatchupStart(ctx context.Context, poolID common.Address) (uint64, error) {
	return loadCLV3CatchupStartBlock(ctx, p.checkpoints, p.pools, poolID)
}

func (p *catchupProtocol) FetchCatchupEvents(
	ctx context.Context,
	poolIDs []common.Address,
	fromBlock uint64,
	toBlock uint64,
) (syncapp.CatchupEventBatch[marketclv3.PoolEvent], error) {
	var zero syncapp.CatchupEventBatch[marketclv3.PoolEvent]
	if p.fetcher == nil {
		return zero, fmt.Errorf("log fetcher is not configured")
	}
	if p.parser == nil {
		return zero, fmt.Errorf("event parser is not configured")
	}
	logs, err := p.fetcher.FetchLogs(ctx, LogFilter{
		PoolAddresses: poolIDs,
		FromBlock:     fromBlock,
		ToBlock:       toBlock,
	})
	if err != nil {
		return zero, err
	}
	events, err := p.parser.ParsePoolEvents(logs)
	if err != nil {
		return zero, fmt.Errorf("parse events: %w", err)
	}
	return syncapp.CatchupEventBatch[marketclv3.PoolEvent]{
		Events:      events,
		BlockHashes: syncapp.BlockHashesFromLogs(logs),
	}, nil
}

func (p *catchupProtocol) ApplyCatchupBlock(
	ctx context.Context,
	req syncapp.ApplyBlockRequest[common.Address, marketclv3.PoolEvent],
) error {
	if p.blockApply == nil {
		return fmt.Errorf("block apply service is not configured")
	}
	_, err := p.blockApply.ApplyBlock(ctx, req)
	return err
}

func NewCatchupService(
	config Config,
	pools PoolRepository,
	checkpoints blockchain.CheckpointRepository,
	fetcher LogFetcher,
	parser EventParser,
	blockApply *BlockApplyService,
	lifecycle *PoolLifecycleService,
	blocks BlockReader,
) *CatchupService {
	return syncapp.NewCatchupService(
		config,
		lifecycle,
		blocks,
		&catchupProtocol{
			pools:       pools,
			checkpoints: checkpoints,
			fetcher:     fetcher,
			parser:      parser,
			blockApply:  blockApply,
		},
	)
}

func loadCLV3CatchupStartBlock(
	ctx context.Context,
	checkpoints blockchain.CheckpointRepository,
	pools PoolRepository,
	poolAddress common.Address,
) (uint64, error) {
	checkpoint, err := checkpoints.Get(ctx, poolAddress)
	if err != nil {
		return 0, fmt.Errorf("load checkpoint: %w", err)
	}

	pool, err := pools.Get(ctx, poolAddress)
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
