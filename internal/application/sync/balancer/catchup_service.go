package balancersync

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
)

type CatchupService = syncapp.CatchupService[marketbalancer.PoolID, marketbalancer.PoolEvent]

type catchupProtocol struct {
	pools       marketbalancer.PoolRepository
	checkpoints blockchain.BalancerCheckpointRepository
	registry    marketbalancer.PoolRegistry
	fetcher     LogFetcher
	parser      EventParser
	blockApply  *BlockApplyService
}

func (p *catchupProtocol) FormatPoolID(poolID marketbalancer.PoolID) string {
	return poolID.String()
}

func (p *catchupProtocol) EventBlockNumber(event marketbalancer.PoolEvent) uint64 {
	return event.Meta.BlockNumber
}

func (p *catchupProtocol) LoadCatchupStart(ctx context.Context, poolID marketbalancer.PoolID) (uint64, error) {
	return loadBalancerCatchupStartBlock(ctx, p.checkpoints, p.pools, poolID)
}

func (p *catchupProtocol) FetchCatchupEvents(
	ctx context.Context,
	poolIDs []marketbalancer.PoolID,
	fromBlock uint64,
	toBlock uint64,
) (syncapp.CatchupEventBatch[marketbalancer.PoolEvent], error) {
	var zero syncapp.CatchupEventBatch[marketbalancer.PoolEvent]
	if p.fetcher == nil {
		return zero, fmt.Errorf("log fetcher is not configured")
	}
	if p.parser == nil {
		return zero, fmt.Errorf("event parser is not configured")
	}
	binding, err := bindBalancerPools(ctx, p.registry, p.parser, poolIDs)
	if err != nil {
		return zero, err
	}
	logs, err := p.fetcher.FetchLogs(ctx, logFilterFromBinding(binding, fromBlock, toBlock))
	if err != nil {
		return zero, err
	}
	events, err := p.parser.ParsePoolEvents(logs)
	if err != nil {
		return zero, fmt.Errorf("parse events: %w", err)
	}
	return syncapp.CatchupEventBatch[marketbalancer.PoolEvent]{
		Events:      events,
		BlockHashes: syncapp.BlockHashesFromLogs(logs),
	}, nil
}

func (p *catchupProtocol) ApplyCatchupBlock(
	ctx context.Context,
	req syncapp.ApplyBlockRequest[marketbalancer.PoolID, marketbalancer.PoolEvent],
) error {
	if p.blockApply == nil {
		return fmt.Errorf("block apply service is not configured")
	}
	_, err := p.blockApply.ApplyBlock(ctx, req)
	return err
}

func NewCatchupService(
	config Config,
	pools marketbalancer.PoolRepository,
	checkpoints blockchain.BalancerCheckpointRepository,
	registry marketbalancer.PoolRegistry,
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
			registry:    registry,
			fetcher:     fetcher,
			parser:      parser,
			blockApply:  blockApply,
		},
	)
}

func loadBalancerCatchupStartBlock(
	ctx context.Context,
	checkpoints blockchain.BalancerCheckpointRepository,
	pools marketbalancer.PoolRepository,
	poolID marketbalancer.PoolID,
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
