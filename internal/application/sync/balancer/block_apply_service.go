package balancersync

import (
	"context"
	"fmt"
	"math/big"
	"sort"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

type BlockApplyService = syncapp.BlockApplyService[marketbalancer.PoolID, marketbalancer.PoolEvent, *marketbalancer.Pool, *blockchain.BalancerCheckpoint]

type ApplyBlockRequest = syncapp.ApplyBlockRequest[marketbalancer.PoolID, marketbalancer.PoolEvent]
type ApplyBlockResult = syncapp.ApplyBlockResult[marketbalancer.PoolID]

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
	return syncapp.NewBlockApplyService(
		syncapp.BlockApplyOptions{SkipPoolAlreadyAtBlock: true},
		syncapp.BlockApplyHooks[marketbalancer.PoolID, marketbalancer.PoolEvent, *marketbalancer.Pool, *blockchain.BalancerCheckpoint]{
			FormatPoolID: func(poolID marketbalancer.PoolID) string { return poolID.String() },
			LessPoolID:   func(a, b marketbalancer.PoolID) bool { return a.String() < b.String() },
			IsNilPool:    func(pool *marketbalancer.Pool) bool { return pool == nil },
			LoadPool:     pools.Get,
			SavePool:     pools.Save,
			AdvanceIdlePools: func(ctx context.Context, poolIDs []marketbalancer.PoolID, blockNumber uint64) error {
				return pools.AdvanceSyncProgressMany(ctx, poolIDs, blockNumber)
			},
			EventPoolID:         func(event marketbalancer.PoolEvent) marketbalancer.PoolID { return event.Meta.PoolID },
			EventTxIndex:        func(event marketbalancer.PoolEvent) uint { return event.Meta.TxIndex },
			EventLogIndex:       func(event marketbalancer.PoolEvent) uint { return event.Meta.LogIndex },
			EventBlockNumber:    func(event marketbalancer.PoolEvent) uint64 { return event.Meta.BlockNumber },
			EventKind:           func(event marketbalancer.PoolEvent) string { return event.Kind.String() },
			ProtocolLabel:       "balancer",
			ExtraEventLogFields: balancerEventLogFields,
			PoolStateAfterApplyLogFields: func(pool *marketbalancer.Pool, _ marketbalancer.PoolEvent, skipped bool) []zap.Field {
				if skipped {
					return nil
				}
				return balancerPoolStateLogFields(pool)
			},
			ApplyEvent: func(pool *marketbalancer.Pool, event marketbalancer.PoolEvent) error { return pool.Apply(event) },
			AfterApplyPool: func(ctx context.Context, poolID marketbalancer.PoolID, pool *marketbalancer.Pool, blockNumber uint64) error {
				return anchorBalancerV3Pool(ctx, registry, reader, poolID, pool, blockNumber)
			},
			PoolLastBlock: func(pool *marketbalancer.Pool) uint64 { return pool.LastBlockNumber },
			SetPoolStatus: func(pool *marketbalancer.Pool, status market.PoolStatus) { pool.Status = status },
			IsPoolAlreadyAtBlock: func(pool *marketbalancer.Pool, blockNumber uint64) bool {
				return pool.LastBlockNumber >= blockNumber
			},
			SetPoolReady: readiness.SetPoolReady,
			MaybeSnapshot: func(ctx context.Context, pool *marketbalancer.Pool, blockNumber uint64) error {
				if snapshots == nil {
					return nil
				}
				return snapshots.MaybeCreateSnapshot(ctx, pool, blockNumber)
			},
			NewCheckpoint: func(poolID marketbalancer.PoolID, blockNumber uint64, blockHash common.Hash) *blockchain.BalancerCheckpoint {
				return &blockchain.BalancerCheckpoint{
					PoolID:      poolID,
					BlockNumber: blockNumber,
					BlockHash:   blockHash,
				}
			},
			SaveCheckpoints: func(ctx context.Context, pending []*blockchain.BalancerCheckpoint) error {
				return checkpoints.SaveMany(ctx, pending)
			},
			NotifyPoolsChanged: listener.OnPoolsChanged,
			SetPoolReadyForStatus: func(ctx context.Context, poolID marketbalancer.PoolID) error {
				pool, err := pools.Get(ctx, poolID)
				if err != nil {
					return fmt.Errorf("load pool %s: %w", poolID.String(), err)
				}
				if pool == nil {
					return fmt.Errorf("pool %s not found", poolID.String())
				}
				pool.Status = market.PoolStatusReady
				if err := pools.Save(ctx, pool); err != nil {
					return fmt.Errorf("save ready pool %s: %w", poolID.String(), err)
				}
				return nil
			},
		},
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
