package syncv4

import (
	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

type BlockApplyService = syncapp.BlockApplyService[marketv4.PoolID, marketv4.PoolEvent, *marketv4.Pool, *blockchain.V4Checkpoint]
type ApplyBlockRequest = syncapp.ApplyBlockRequest[marketv4.PoolID, marketv4.PoolEvent]

type blockApplyProtocol struct{}

func (p *blockApplyProtocol) Label() string                          { return "univ4" }
func (p *blockApplyProtocol) FormatPoolID(id marketv4.PoolID) string { return id.String() }
func (p *blockApplyProtocol) IsNilPool(pool *marketv4.Pool) bool     { return pool == nil }
func (p *blockApplyProtocol) IsNilCheckpoint(checkpoint *blockchain.V4Checkpoint) bool {
	return checkpoint == nil
}
func (p *blockApplyProtocol) DescribeEvent(event marketv4.PoolEvent) syncapp.EventMetadata[marketv4.PoolID] {
	return syncapp.EventMetadata[marketv4.PoolID]{PoolID: event.Meta.PoolID, TxIndex: event.Meta.TxIndex, LogIndex: event.Meta.LogIndex, BlockNumber: event.Meta.BlockNumber, Kind: event.Kind.String()}
}
func (p *blockApplyProtocol) ApplyEvent(pool *marketv4.Pool, event marketv4.PoolEvent) error {
	return pool.Apply(event)
}
func (p *blockApplyProtocol) PoolLastBlock(pool *marketv4.Pool) uint64 { return pool.LastBlockNumber }
func (p *blockApplyProtocol) SetPoolStatus(pool *marketv4.Pool, status market.PoolStatus) {
	pool.Status = status
}
func (p *blockApplyProtocol) NewCheckpoint(id marketv4.PoolID, block uint64, hash common.Hash) *blockchain.V4Checkpoint {
	return &blockchain.V4Checkpoint{PoolID: id, BlockNumber: block, BlockHash: hash}
}
func (p *blockApplyProtocol) EventLogFields(event marketv4.PoolEvent) []zap.Field {
	return v4EventLogFields(event)
}
func (p *blockApplyProtocol) PoolStateLogFields(pool *marketv4.Pool, _ marketv4.PoolEvent) []zap.Field {
	return syncapp.PoolStateLogFields(pool.State, pool.LastBlockNumber, pool.Status)
}

func NewBlockApplyService(pools marketv4.PoolRepository, checkpoints blockchain.V4CheckpointRepository, snapshots *SnapshotService, readiness *ReadinessService, listener ChangedPoolsListener) *BlockApplyService {
	if listener == nil {
		listener = NopChangedPoolsListener{}
	}
	return syncapp.NewBlockApplyService[
		marketv4.PoolID,
		marketv4.PoolEvent,
		*marketv4.Pool,
		*blockchain.V4Checkpoint,
	](
		syncapp.BlockApplyOptions{FilterUntrackedEvents: true, SkipPoolAlreadyAtBlock: true},
		syncapp.BlockApplyDeps[marketv4.PoolID, *marketv4.Pool, *blockchain.V4Checkpoint]{
			Pools: pools, Checkpoints: checkpoints, Snapshots: syncapp.SnapshotBlockApplyService(snapshots), Readiness: readiness, Listener: listener,
		},
		&blockApplyProtocol{},
	)
}
