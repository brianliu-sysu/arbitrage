package balancersync

import (
	"context"
	"fmt"
	"math/big"
	"sort"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

type BlockApplyService = syncapp.BlockApplyService[marketbalancer.PoolID, marketbalancer.PoolEvent, *marketbalancer.Pool, *blockchain.BalancerCheckpoint]

type ApplyBlockRequest = syncapp.ApplyBlockRequest[marketbalancer.PoolID, marketbalancer.PoolEvent]

type blockApplyProtocol struct {
	registry marketbalancer.PoolRegistry
	reader   PoolBootstrapReader
}

func (p *blockApplyProtocol) Label() string                                { return "balancer" }
func (p *blockApplyProtocol) FormatPoolID(id marketbalancer.PoolID) string { return id.String() }
func (p *blockApplyProtocol) IsNilPool(pool *marketbalancer.Pool) bool     { return pool == nil }
func (p *blockApplyProtocol) IsNilCheckpoint(checkpoint *blockchain.BalancerCheckpoint) bool {
	return checkpoint == nil
}
func (p *blockApplyProtocol) DescribeEvent(event marketbalancer.PoolEvent) syncapp.EventMetadata[marketbalancer.PoolID] {
	return syncapp.EventMetadata[marketbalancer.PoolID]{PoolID: event.Meta.PoolID, TxIndex: event.Meta.TxIndex, LogIndex: event.Meta.LogIndex, BlockNumber: event.Meta.BlockNumber, Kind: event.Kind.String()}
}
func (p *blockApplyProtocol) ApplyEvent(pool *marketbalancer.Pool, event marketbalancer.PoolEvent) error {
	return pool.Apply(event)
}
func (p *blockApplyProtocol) PoolLastBlock(pool *marketbalancer.Pool) uint64 {
	return pool.LastBlockNumber
}
func (p *blockApplyProtocol) SetPoolStatus(pool *marketbalancer.Pool, status market.PoolStatus) {
	pool.Status = status
}
func (p *blockApplyProtocol) NewCheckpoint(id marketbalancer.PoolID, block uint64, hash common.Hash) *blockchain.BalancerCheckpoint {
	return &blockchain.BalancerCheckpoint{PoolID: id, BlockNumber: block, BlockHash: hash}
}
func (p *blockApplyProtocol) EventLogFields(event marketbalancer.PoolEvent) []zap.Field {
	return balancerEventLogFields(event)
}
func (p *blockApplyProtocol) PoolStateLogFields(pool *marketbalancer.Pool, _ marketbalancer.PoolEvent) []zap.Field {
	return balancerPoolStateLogFields(pool)
}
func (p *blockApplyProtocol) AfterApplyPool(ctx context.Context, id marketbalancer.PoolID, pool *marketbalancer.Pool, block uint64) error {
	return anchorBalancerV3Pool(ctx, p.registry, p.reader, id, pool, block)
}

func NewBlockApplyService(
	pools marketbalancer.PoolRepository,
	checkpoints blockchain.BalancerCheckpointRepository,
	snapshots *SnapshotService,
	readiness *ReadinessService,
	registry marketbalancer.PoolRegistry,
	reader PoolBootstrapReader,
	listener ChangedPoolsListener,
) *BlockApplyService {
	if listener == nil {
		listener = NopChangedPoolsListener{}
	}
	return syncapp.NewBlockApplyService[
		marketbalancer.PoolID,
		marketbalancer.PoolEvent,
		*marketbalancer.Pool,
		*blockchain.BalancerCheckpoint,
	](
		syncapp.BlockApplyOptions{SkipPoolAlreadyAtBlock: true},
		syncapp.BlockApplyDeps[marketbalancer.PoolID, *marketbalancer.Pool, *blockchain.BalancerCheckpoint]{
			Pools: pools, Checkpoints: checkpoints, Snapshots: syncapp.SnapshotBlockApplyService(snapshots), Readiness: readiness, Listener: listener,
		},
		&blockApplyProtocol{registry: registry, reader: reader},
	)
}

func anchorBalancerV3Pool(
	ctx context.Context,
	registry marketbalancer.PoolRegistry,
	reader PoolBootstrapReader,
	poolID marketbalancer.PoolID,
	pool *marketbalancer.Pool,
	blockNumber uint64,
) error {
	if registry == nil || reader == nil || pool == nil {
		return nil
	}
	spec, err := registry.GetSpec(ctx, poolID)
	if err != nil {
		return fmt.Errorf("resolve pool spec: %w", err)
	}
	if !spec.VaultVersion.IsV3() {
		return nil
	}
	data, err := reader.ReadBootstrapData(ctx, poolID, spec, blockNumber)
	if err != nil {
		return fmt.Errorf("read v3 pool state: %w", err)
	}
	if data == nil {
		return fmt.Errorf("read v3 pool state: empty response")
	}
	applyBootstrapData(pool, data)
	pool.LastBlockNumber = data.BlockNumber
	return nil
}

func balancerEventLogFields(event marketbalancer.PoolEvent) []zap.Field {
	fields := make([]zap.Field, 0, 8)
	switch event.Kind {
	case marketbalancer.EventKindPoolBalanceChanged:
		if payload := event.PoolBalanceChanged; payload != nil {
			fields = append(fields,
				zap.Strings("tokens", addressStrings(payload.Tokens)),
				zap.Strings("deltas", bigIntStrings(payload.Deltas)),
			)
		}
	case marketbalancer.EventKindSwap:
		if payload := event.Swap; payload != nil {
			fields = append(fields,
				zap.String("tokenIn", payload.TokenIn.Hex()),
				zap.String("tokenOut", payload.TokenOut.Hex()),
				zap.String("amountIn", bigIntString(payload.AmountIn)),
				zap.String("amountOut", bigIntString(payload.AmountOut)),
			)
		}
	case marketbalancer.EventKindSwapFeePercentageChanged:
		if payload := event.SwapFeePercentageChanged; payload != nil {
			fields = append(fields, zap.String("swapFeePercentage", bigIntString(payload.SwapFeePercentage)))
		}
	case marketbalancer.EventKindAmplificationUpdated:
		if payload := event.AmplificationUpdated; payload != nil {
			fields = append(fields, zap.String("amplification", bigIntString(payload.Amplification)))
		}
	case marketbalancer.EventKindLiquidityAdded:
		if payload := event.LiquidityAdded; payload != nil {
			fields = append(fields, zap.Strings("amounts", bigIntStrings(payload.Amounts)))
		}
	case marketbalancer.EventKindLiquidityRemoved:
		if payload := event.LiquidityRemoved; payload != nil {
			fields = append(fields, zap.Strings("amounts", bigIntStrings(payload.Amounts)))
		}
	case marketbalancer.EventKindPoolPausedStateChanged:
		if payload := event.PoolPausedStateChanged; payload != nil {
			fields = append(fields, zap.Bool("paused", payload.Paused))
		}
	}
	return fields
}

func balancerPoolStateLogFields(pool *marketbalancer.Pool) []zap.Field {
	if pool == nil {
		return nil
	}
	return []zap.Field{
		zap.String("status", string(pool.Status)),
		zap.Uint64("lastBlock", pool.LastBlockNumber),
		zap.String("type", string(pool.Type)),
		zap.Bool("paused", pool.Paused),
		zap.Strings("tokens", addressStrings(pool.Tokens)),
		zap.Strings("balances", tokenIntMapStrings(pool.Tokens, pool.Balances)),
		zap.Strings("weights", tokenIntMapStrings(pool.Tokens, pool.Weights)),
		zap.String("amplification", bigIntString(pool.Amplification)),
		zap.String("swapFeePercentage", bigIntString(pool.SwapFeePercentage)),
	}
}

func addressStrings(addresses []common.Address) []string {
	out := make([]string, 0, len(addresses))
	for _, address := range addresses {
		out = append(out, address.Hex())
	}
	return out
}

func bigIntStrings(values []*big.Int) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, bigIntString(value))
	}
	return out
}

func tokenIntMapStrings(tokens []common.Address, values map[common.Address]*big.Int) []string {
	ordered := append([]common.Address(nil), tokens...)
	if len(ordered) == 0 && len(values) > 0 {
		ordered = make([]common.Address, 0, len(values))
		for token := range values {
			ordered = append(ordered, token)
		}
		sort.Slice(ordered, func(i, j int) bool {
			return ordered[i].Hex() < ordered[j].Hex()
		})
	}
	out := make([]string, 0, len(ordered))
	for _, token := range ordered {
		out = append(out, fmt.Sprintf("%s=%s", token.Hex(), bigIntString(values[token])))
	}
	return out
}

func bigIntString(value *big.Int) string {
	if value == nil {
		return ""
	}
	return value.String()
}
