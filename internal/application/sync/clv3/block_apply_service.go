package clv3sync

import (
	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

type BlockApplyService = syncapp.BlockApplyService[common.Address, marketclv3.PoolEvent, *marketclv3.Pool, *blockchain.Checkpoint]
type ApplyBlockRequest = syncapp.ApplyBlockRequest[common.Address, marketclv3.PoolEvent]
type ApplyBlockResult = syncapp.ApplyBlockResult[common.Address]

type blockApplyProtocol struct{}

func (p *blockApplyProtocol) Label() string                         { return "clv3" }
func (p *blockApplyProtocol) FormatPoolID(id common.Address) string { return id.Hex() }
func (p *blockApplyProtocol) IsNilPool(pool *marketclv3.Pool) bool  { return pool == nil }
func (p *blockApplyProtocol) IsNilCheckpoint(checkpoint *blockchain.Checkpoint) bool {
	return checkpoint == nil
}
func (p *blockApplyProtocol) DescribeEvent(event marketclv3.PoolEvent) syncapp.EventMetadata[common.Address] {
	return syncapp.EventMetadata[common.Address]{PoolID: event.Meta.PoolAddress, TxIndex: event.Meta.TxIndex, LogIndex: event.Meta.LogIndex, BlockNumber: event.Meta.BlockNumber, Kind: event.Kind.String()}
}
func (p *blockApplyProtocol) ApplyEvent(pool *marketclv3.Pool, event marketclv3.PoolEvent) error {
	return pool.Apply(event)
}
func (p *blockApplyProtocol) PoolLastBlock(pool *marketclv3.Pool) uint64 { return pool.LastBlockNumber }
func (p *blockApplyProtocol) SetPoolStatus(pool *marketclv3.Pool, status market.PoolStatus) {
	pool.Status = status
}
func (p *blockApplyProtocol) NewCheckpoint(id common.Address, block uint64, hash common.Hash) *blockchain.Checkpoint {
	return &blockchain.Checkpoint{PoolAddress: id, BlockNumber: block, BlockHash: hash}
}

func NewBlockApplyService(pools PoolRepository, checkpoints blockchain.CheckpointRepository, snapshots *SnapshotService, readiness *ReadinessService, listener ChangedPoolsListener) *BlockApplyService {
	if listener == nil {
		listener = NopChangedPoolsListener{}
	}
	return syncapp.NewBlockApplyService[
		common.Address,
		marketclv3.PoolEvent,
		*marketclv3.Pool,
		*blockchain.Checkpoint,
	](
		syncapp.BlockApplyOptions{},
		syncapp.BlockApplyDeps[common.Address, *marketclv3.Pool, *blockchain.Checkpoint]{
			Pools: pools, Checkpoints: checkpoints, Snapshots: syncapp.SnapshotBlockApplyService(snapshots), Readiness: readiness, Listener: listener,
		},
		&blockApplyProtocol{},
	)
}
