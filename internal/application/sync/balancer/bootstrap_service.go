package balancersync

import (
	"context"
	"fmt"
	"math/big"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

// BootstrapService cold-starts a Balancer pool from chain state or snapshot.
type BootstrapService struct {
	inner *syncapp.BootstrapService[marketbalancer.PoolID, *marketbalancer.Pool, *BootstrapData]
}

type bootstrapProtocol struct {
	pools    marketbalancer.PoolRepository
	registry marketbalancer.PoolRegistry
	reader   PoolBootstrapReader
	snapshot *SnapshotService
}

func NewBootstrapService(
	pools marketbalancer.PoolRepository,
	registry marketbalancer.PoolRegistry,
	reader PoolBootstrapReader,
	snapshot *SnapshotService,
	staleBlockThreshold uint64,
) *BootstrapService {
	protocol := &bootstrapProtocol{
		pools: pools, registry: registry, reader: reader, snapshot: snapshot,
	}
	return &BootstrapService{
		inner: syncapp.NewBootstrapService(staleBlockThreshold, protocol),
	}
}

func (s *BootstrapService) Bootstrap(ctx context.Context, poolID marketbalancer.PoolID, blockNumber uint64) (*marketbalancer.Pool, error) {
	return s.inner.Bootstrap(ctx, poolID, blockNumber)
}

func (s *BootstrapService) BootstrapAll(ctx context.Context, poolIDs []marketbalancer.PoolID, blockNumber uint64) error {
	return s.inner.BootstrapAll(ctx, poolIDs, blockNumber)
}

func (p *bootstrapProtocol) IsNilPool(pool *marketbalancer.Pool) bool { return pool == nil }

func (p *bootstrapProtocol) LoadPool(ctx context.Context, id marketbalancer.PoolID) (*marketbalancer.Pool, error) {
	return p.pools.Get(ctx, id)
}

func (p *bootstrapProtocol) SavePool(ctx context.Context, pool *marketbalancer.Pool) error {
	return p.pools.Save(ctx, pool)
}

func (p *bootstrapProtocol) RestoreSnapshot(ctx context.Context, pool *marketbalancer.Pool) error {
	if p.snapshot == nil {
		return nil
	}
	_, err := p.snapshot.RestorePool(ctx, pool)
	return err
}

func (p *bootstrapProtocol) ReadChainData(ctx context.Context, id marketbalancer.PoolID, blockNumber uint64) (*BootstrapData, error) {
	spec, err := p.registry.GetSpec(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("resolve pool spec: %w", err)
	}
	data, err := p.reader.ReadBootstrapData(ctx, id, spec, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("read bootstrap data: %w", err)
	}
	return data, nil
}

func (p *bootstrapProtocol) ReadChainDataForMany(
	ctx context.Context,
	poolIDs []marketbalancer.PoolID,
	blockNumber uint64,
) (map[marketbalancer.PoolID]*BootstrapData, error) {
	if p.reader == nil {
		return nil, fmt.Errorf("balancer bootstrap reader is not configured")
	}
	inputs := make([]BootstrapInput, 0, len(poolIDs))
	for _, poolID := range poolIDs {
		spec, err := p.registry.GetSpec(ctx, poolID)
		if err != nil {
			return nil, fmt.Errorf("resolve pool spec for %s: %w", poolID, err)
		}
		inputs = append(inputs, BootstrapInput{PoolID: poolID, Spec: spec})
	}
	results, err := p.reader.ReadManyBootstrapData(ctx, inputs, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("read bootstrap data: %w", err)
	}
	return results, nil
}

func (p *bootstrapProtocol) NewPoolFromChain(id marketbalancer.PoolID, data *BootstrapData) (*marketbalancer.Pool, error) {
	return marketbalancer.NewPool(id, data.Spec.Address, data.Spec.Vault, data.Spec.Type, data.Tokens)
}

func (p *bootstrapProtocol) ApplyChainData(pool *marketbalancer.Pool, data *BootstrapData, _ uint64) {
	applyBootstrapData(pool, data)
	pool.LastBlockNumber = data.BlockNumber
}

func (p *bootstrapProtocol) IsInitialized(pool *marketbalancer.Pool) bool {
	return pool.IsInitialized()
}

func (p *bootstrapProtocol) PoolLastBlock(pool *marketbalancer.Pool) uint64 {
	return pool.LastBlockNumber
}

func (p *bootstrapProtocol) SetStatus(pool *marketbalancer.Pool, status market.PoolStatus) {
	pool.Status = status
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
