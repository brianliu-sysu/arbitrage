package balancersync

import (
	"context"
	"fmt"
	"math/big"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// BootstrapService cold-starts a Balancer pool from chain state or snapshot.
type BootstrapService struct {
	inner    *syncapp.BootstrapService[marketbalancer.PoolID, *marketbalancer.Pool, *BootstrapData]
	logger   *zap.Logger
	registry marketbalancer.PoolRegistry
	reader   PoolBootstrapReader
}

func NewBootstrapService(
	pools marketbalancer.PoolRepository,
	registry marketbalancer.PoolRegistry,
	reader PoolBootstrapReader,
	snapshot *SnapshotService,
	staleBlockThreshold uint64,
) *BootstrapService {
	svc := &BootstrapService{
		logger:   zap.NewNop(),
		registry: registry,
		reader:   reader,
	}
	svc.inner = syncapp.NewBootstrapService(staleBlockThreshold, syncapp.BootstrapHooks[marketbalancer.PoolID, *marketbalancer.Pool, *BootstrapData]{
		IsNilPool: func(pool *marketbalancer.Pool) bool { return pool == nil },
		IsNilData: func(data *BootstrapData) bool { return data == nil },
		LoadPool:  pools.Get,
		SavePool:  pools.Save,
		RestoreSnapshot: func(ctx context.Context, pool *marketbalancer.Pool) error {
			if snapshot == nil {
				return nil
			}
			_, err := snapshot.RestorePool(ctx, pool)
			return err
		},
		ReadChainData: func(ctx context.Context, poolID marketbalancer.PoolID, blockNumber uint64) (*BootstrapData, error) {
			spec, err := registry.GetSpec(ctx, poolID)
			if err != nil {
				return nil, fmt.Errorf("resolve pool spec: %w", err)
			}
			data, err := reader.ReadBootstrapData(ctx, poolID, spec, blockNumber)
			if err != nil {
				return nil, fmt.Errorf("read bootstrap data: %w", err)
			}
			return data, nil
		},
		ReadChainDataForMany: func(ctx context.Context, poolIDs []marketbalancer.PoolID, blockNumber uint64) (map[marketbalancer.PoolID]*BootstrapData, error) {
			if reader == nil {
				return nil, fmt.Errorf("balancer bootstrap reader is not configured")
			}
			inputs := make([]BootstrapInput, 0, len(poolIDs))
			for _, poolID := range poolIDs {
				spec, err := registry.GetSpec(ctx, poolID)
				if err != nil {
					return nil, fmt.Errorf("resolve pool spec for %s: %w", poolID, err)
				}
				inputs = append(inputs, BootstrapInput{PoolID: poolID, Spec: spec})
			}
			results, err := reader.ReadManyBootstrapData(ctx, inputs, blockNumber)
			if err != nil {
				return nil, fmt.Errorf("read bootstrap data: %w", err)
			}
			return results, nil
		},
		NewPoolFromChain: func(poolID marketbalancer.PoolID, data *BootstrapData) (*marketbalancer.Pool, error) {
			return marketbalancer.NewPool(poolID, data.Spec.Address, data.Spec.Vault, data.Spec.Type, data.Tokens)
		},
		UpdatePoolFromChain: applyBootstrapData,
		IsInitialized:       func(pool *marketbalancer.Pool) bool { return pool.IsInitialized() },
		PoolLastBlock:       func(pool *marketbalancer.Pool) uint64 { return pool.LastBlockNumber },
		SetStatus:           func(pool *marketbalancer.Pool, status market.PoolStatus) { pool.Status = status },
		SetLastBlockOnChainBootstrap: func(pool *marketbalancer.Pool, data *BootstrapData, _ uint64) {
			pool.LastBlockNumber = data.BlockNumber
		},
		OnChainBootstrap: func(poolID marketbalancer.PoolID, data *BootstrapData) {
			svc.logChainBootstrap(poolID, data)
		},
	})
	return svc
}

// SetLogger configures bootstrap logging.
func (s *BootstrapService) SetLogger(logger *zap.Logger) {
	if logger == nil {
		logger = zap.NewNop()
	}
	s.logger = logger
}

func (s *BootstrapService) Bootstrap(ctx context.Context, poolID marketbalancer.PoolID, blockNumber uint64) (*marketbalancer.Pool, error) {
	return s.inner.Bootstrap(ctx, poolID, blockNumber)
}

// BootstrapAll cold-starts every tracked pool using batched chain reads.
func (s *BootstrapService) BootstrapAll(ctx context.Context, poolIDs []marketbalancer.PoolID, blockNumber uint64) error {
	return s.inner.BootstrapAll(ctx, poolIDs, blockNumber)
}

func (s *BootstrapService) logChainBootstrap(poolID marketbalancer.PoolID, data *BootstrapData) {
	if s == nil || s.logger == nil || data == nil {
		return
	}
	s.logger.Info("chain bootstrap",
		zap.String("protocol", "balancer"),
		zap.String("pool", poolID.String()),
		zap.String("type", string(data.Spec.Type)),
		zap.Int("tokens", len(data.Tokens)),
		zap.Uint64("block", data.BlockNumber),
	)
}

func applyBootstrapData(pool *marketbalancer.Pool, data *BootstrapData) {
	pool.Address = data.Spec.Address
	pool.Vault = data.Spec.Vault
	pool.Type = data.Spec.Type
	pool.Tokens = cloneAddresses(data.Tokens)
	pool.Balances = cloneIntMap(data.Balances)
	pool.Weights = cloneIntMap(data.Weights)
	pool.Amplification = cloneInt(data.Amplification)
	pool.SwapFeePercentage = cloneInt(data.SwapFeePercentage)
}

func cloneInt(value *big.Int) *big.Int {
	if value == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(value)
}

func cloneAddresses(values []common.Address) []common.Address {
	out := make([]common.Address, len(values))
	copy(out, values)
	return out
}

func cloneIntMap(values map[common.Address]*big.Int) map[common.Address]*big.Int {
	out := make(map[common.Address]*big.Int, len(values))
	for token, value := range values {
		out[token] = cloneInt(value)
	}
	return out
}
