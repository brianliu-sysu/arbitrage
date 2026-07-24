package clv3sync

import (
	"context"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

type BootstrapService = syncapp.BootstrapService[common.Address, *marketclv3.Pool, *BootstrapData]

type bootstrapProtocol struct {
	pools    PoolRepository
	newPool  PoolFactory
	reader   PoolBootstrapReader
	snapshot *SnapshotService
}

func NewBootstrapService(
	pools PoolRepository,
	newPool PoolFactory,
	reader PoolBootstrapReader,
	snapshot *SnapshotService,
	staleBlockThreshold uint64,
) *BootstrapService {
	return syncapp.NewBootstrapService(staleBlockThreshold, &bootstrapProtocol{
		pools: pools, newPool: newPool, reader: reader, snapshot: snapshot,
	})
}

func (p *bootstrapProtocol) IsNilPool(pool *marketclv3.Pool) bool { return pool == nil }

func (p *bootstrapProtocol) LoadPool(ctx context.Context, id common.Address) (*marketclv3.Pool, error) {
	return p.pools.Get(ctx, id)
}

func (p *bootstrapProtocol) SavePool(ctx context.Context, pool *marketclv3.Pool) error {
	return p.pools.Save(ctx, pool)
}

func (p *bootstrapProtocol) RestoreSnapshot(ctx context.Context, pool *marketclv3.Pool) error {
	if p.snapshot == nil {
		return nil
	}
	_, err := p.snapshot.RestorePool(ctx, pool)
	return err
}

func (p *bootstrapProtocol) ReadChainData(ctx context.Context, id common.Address, blockNumber uint64) (*BootstrapData, error) {
	return p.reader.ReadBootstrapData(ctx, id, blockNumber)
}

func (p *bootstrapProtocol) NewPoolFromChain(id common.Address, data *BootstrapData) (*marketclv3.Pool, error) {
	return p.newPool(id, data.Token0, data.Token1, data.Fee, data.TickSpacing), nil
}

func (p *bootstrapProtocol) ApplyChainData(pool *marketclv3.Pool, data *BootstrapData, blockNumber uint64) {
	pool.Token0 = data.Token0
	pool.Token1 = data.Token1
	pool.Fee = data.Fee
	pool.TickSpacing = data.TickSpacing
	applyBootstrapData(pool, data)
	pool.LastBlockNumber = blockNumber
}

func (p *bootstrapProtocol) IsInitialized(pool *marketclv3.Pool) bool {
	return pool.State.IsInitialized()
}

func (p *bootstrapProtocol) PoolLastBlock(pool *marketclv3.Pool) uint64 {
	return pool.LastBlockNumber
}

func (p *bootstrapProtocol) SetStatus(pool *marketclv3.Pool, status market.PoolStatus) {
	pool.Status = status
}

func applyBootstrapData(pool *marketclv3.Pool, data *BootstrapData) {
	pool.State = data.State.Clone()
	pool.Ticks = data.Ticks.Clone()
	pool.Bitmap = data.Bitmap.Clone()
}
