package protocol

import (
	"context"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// BlockApplyPoolRepository persists the pool state used during block application.
type BlockApplyPoolRepository[PoolID comparable, Pool any] interface {
	Get(context.Context, PoolID) (Pool, error)
	Save(context.Context, Pool) error
	AdvanceSyncProgressMany(context.Context, []PoolID, uint64) error
}

type BlockApplyCheckpointRepository[PoolID comparable, Checkpoint any] interface {
	Get(context.Context, PoolID) (Checkpoint, error)
	Save(context.Context, Checkpoint) error
	SaveMany(context.Context, []Checkpoint) error
	Delete(context.Context, PoolID) error
}

type BlockApplySnapshotService[PoolID comparable, Pool any] interface {
	DeleteAfterBlock(context.Context, PoolID, uint64) error
	MaybeSnapshot(context.Context, Pool, uint64) error
}

type snapshotBlockApplyAdapter[PoolID comparable, Pool, Snapshot any] struct {
	service *SnapshotService[PoolID, Pool, Snapshot]
}

func (a *snapshotBlockApplyAdapter[PoolID, Pool, Snapshot]) DeleteAfterBlock(ctx context.Context, id PoolID, block uint64) error {
	return a.service.DeleteAfterBlock(ctx, id, block)
}

func (a *snapshotBlockApplyAdapter[PoolID, Pool, Snapshot]) MaybeSnapshot(ctx context.Context, pool *Pool, block uint64) error {
	return a.service.MaybeCreateSnapshot(ctx, pool, block)
}

func SnapshotBlockApplyService[PoolID comparable, Pool, Snapshot any](
	service *SnapshotService[PoolID, Pool, Snapshot],
) BlockApplySnapshotService[PoolID, *Pool] {
	if service == nil {
		return nil
	}
	return &snapshotBlockApplyAdapter[PoolID, Pool, Snapshot]{service: service}
}

type PoolReadiness[PoolID comparable] interface {
	SetPoolReady(PoolID, bool)
	IsPoolReady(PoolID) bool
}

type BlockApplyDeps[PoolID comparable, Pool, Checkpoint any] struct {
	Pools       BlockApplyPoolRepository[PoolID, Pool]
	Checkpoints BlockApplyCheckpointRepository[PoolID, Checkpoint]
	Snapshots   BlockApplySnapshotService[PoolID, Pool]
	Readiness   PoolReadiness[PoolID]
	Listener    PoolsChangedNotifier[PoolID]
}

type EventMetadata[PoolID comparable] struct {
	PoolID      PoolID
	TxIndex     uint
	LogIndex    uint
	BlockNumber uint64
	Kind        string
}

type ProtocolDescriptor[PoolID comparable] interface {
	Label() string
	FormatPoolID(PoolID) string
}

type BlockEventAdapter[PoolID comparable, Event any] interface {
	DescribeEvent(Event) EventMetadata[PoolID]
}

type BlockApplyPoolAdapter[Pool, Event any] interface {
	IsNilPool(Pool) bool
	ApplyEvent(Pool, Event) error
	PoolLastBlock(Pool) uint64
	SetPoolStatus(Pool, market.PoolStatus)
}

type BlockApplyCheckpointAdapter[PoolID comparable, Checkpoint any] interface {
	IsNilCheckpoint(Checkpoint) bool
	NewCheckpoint(PoolID, uint64, common.Hash) Checkpoint
}

type BlockApplyProtocol[PoolID comparable, Event, Pool, Checkpoint any] interface {
	ProtocolDescriptor[PoolID]
	BlockEventAdapter[PoolID, Event]
	BlockApplyPoolAdapter[Pool, Event]
	BlockApplyCheckpointAdapter[PoolID, Checkpoint]
}

type EventLogEnricher[Event any] interface {
	EventLogFields(Event) []zap.Field
}

type PoolStateLogEnricher[Pool, Event any] interface {
	PoolStateLogFields(Pool, Event) []zap.Field
}

type AfterPoolApply[PoolID comparable, Pool any] interface {
	AfterApplyPool(context.Context, PoolID, Pool, uint64) error
}
