package syncv4

import (
	"context"
	"fmt"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
)

// BootstrapService cold-starts a V4 pool from chain state or snapshot.
type BootstrapService struct {
	inner *syncapp.BootstrapService[marketv4.PoolID, *marketv4.Pool, *BootstrapData]
}

type bootstrapProtocol struct {
	pools    marketv4.PoolRepository
	registry marketv4.PoolRegistry
	reader   PoolBootstrapReader
	snapshot *SnapshotService
}

func NewBootstrapService(
	pools marketv4.PoolRepository,
	registry marketv4.PoolRegistry,
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

func (s *BootstrapService) Bootstrap(ctx context.Context, poolID marketv4.PoolID, blockNumber uint64) (*marketv4.Pool, error) {
	return s.inner.Bootstrap(ctx, poolID, blockNumber)
}

func (p *bootstrapProtocol) IsNilPool(pool *marketv4.Pool) bool { return pool == nil }

func (p *bootstrapProtocol) LoadPool(ctx context.Context, id marketv4.PoolID) (*marketv4.Pool, error) {
	return p.pools.Get(ctx, id)
}

func (p *bootstrapProtocol) SavePool(ctx context.Context, pool *marketv4.Pool) error {
	return p.pools.Save(ctx, pool)
}

func (p *bootstrapProtocol) RestoreSnapshot(ctx context.Context, pool *marketv4.Pool) error {
	if p.snapshot == nil {
		return nil
	}
	_, err := p.snapshot.RestorePool(ctx, pool)
	return err
}

func (p *bootstrapProtocol) ReadChainData(ctx context.Context, id marketv4.PoolID, blockNumber uint64) (*BootstrapData, error) {
	key, err := p.registry.GetKey(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("resolve pool key: %w", err)
	}
	data, err := p.reader.ReadBootstrapData(ctx, id, key, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("read bootstrap data: %w", err)
	}
	return data, nil
}

func (p *bootstrapProtocol) NewPoolFromChain(id marketv4.PoolID, data *BootstrapData) (*marketv4.Pool, error) {
	return marketv4.NewPool(id, data.Key), nil
}

func (p *bootstrapProtocol) ApplyChainData(pool *marketv4.Pool, data *BootstrapData, _ uint64) {
	pool.Key = data.Key
	applyBootstrapData(pool, data)
	pool.LastBlockNumber = data.BlockNumber
}

func (p *bootstrapProtocol) IsInitialized(pool *marketv4.Pool) bool {
	return pool.State.IsInitialized()
}

func (p *bootstrapProtocol) PoolLastBlock(pool *marketv4.Pool) uint64 {
	return pool.LastBlockNumber
}

func (p *bootstrapProtocol) SetStatus(pool *marketv4.Pool, status market.PoolStatus) {
	pool.Status = status
}

func applyBootstrapData(pool *marketv4.Pool, data *BootstrapData) {
	pool.State = data.State.Clone()
	pool.Ticks = data.Ticks.Clone()
	pool.Bitmap = data.Bitmap.Clone()
}
