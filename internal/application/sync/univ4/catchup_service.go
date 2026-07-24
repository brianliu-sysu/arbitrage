package syncv4

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
)

type CatchupService = syncapp.CatchupService[marketv4.PoolID, marketv4.PoolEvent]

type catchupProtocol struct {
	pools       marketv4.PoolRepository
	checkpoints blockchain.V4CheckpointRepository
	fetcher     LogFetcher
	parser      EventParser
	blockApply  *BlockApplyService
}

func (p *catchupProtocol) FormatPoolID(poolID marketv4.PoolID) string {
	return poolID.String()
}

func (p *catchupProtocol) EventBlockNumber(event marketv4.PoolEvent) uint64 {
	return event.Meta.BlockNumber
}

func (p *catchupProtocol) LoadCatchupStart(ctx context.Context, poolID marketv4.PoolID) (uint64, error) {
	return loadV4CatchupStartBlock(ctx, p.checkpoints, p.pools, poolID)
}

func (p *catchupProtocol) FetchCatchupEvents(
	ctx context.Context,
	poolIDs []marketv4.PoolID,
	fromBlock uint64,
	toBlock uint64,
) (syncapp.CatchupEventBatch[marketv4.PoolEvent], error) {
	var zero syncapp.CatchupEventBatch[marketv4.PoolEvent]
	if p.fetcher == nil {
		return zero, fmt.Errorf("log fetcher is not configured")
	}
	if p.parser == nil {
		return zero, fmt.Errorf("event parser is not configured")
	}
	logs, err := p.fetcher.FetchLogs(ctx, LogFilter{
		PoolIDs:   poolIDs,
		FromBlock: fromBlock,
		ToBlock:   toBlock,
	})
	if err != nil {
		return zero, err
	}
	events, err := p.parser.ParsePoolEvents(logs)
	if err != nil {
		return zero, fmt.Errorf("parse events: %w", err)
	}
	return syncapp.CatchupEventBatch[marketv4.PoolEvent]{
		Events:      events,
		BlockHashes: syncapp.BlockHashesFromLogs(logs),
	}, nil
}

func (p *catchupProtocol) ApplyCatchupBlock(
	ctx context.Context,
	req syncapp.ApplyBlockRequest[marketv4.PoolID, marketv4.PoolEvent],
) error {
	if p.blockApply == nil {
		return fmt.Errorf("block apply service is not configured")
	}
	_, err := p.blockApply.ApplyBlock(ctx, req)
	return err
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

func loadV4CatchupStartBlock(
	ctx context.Context,
	checkpoints blockchain.V4CheckpointRepository,
	pools marketv4.PoolRepository,
	poolID marketv4.PoolID,
) (uint64, error) {
	checkpoint, err := checkpoints.Get(ctx, poolID)
	if err != nil {
		return 0, fmt.Errorf("load checkpoint: %w", err)
	}

	pool, err := pools.Get(ctx, poolID)
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
