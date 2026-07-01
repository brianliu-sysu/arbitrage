package postgres

import (
	"context"

	"github.com/brianliu-sysu/arbitrage/internal/storage"
)

// NoopPoolRepo 无数据库时的空 PoolRepo。
type NoopPoolRepo struct{}

func NewNoopPoolRepo() *NoopPoolRepo { return &NoopPoolRepo{} }

func (NoopPoolRepo) Save(context.Context, *storage.PoolSnapshot) error          { return nil }
func (NoopPoolRepo) SaveHistory(context.Context, *storage.PoolSnapshot) error    { return nil }
func (NoopPoolRepo) Load(context.Context, string, string) (*storage.PoolSnapshot, error) {
	return nil, nil
}
func (NoopPoolRepo) LoadAll(context.Context, string) (map[string]*storage.PoolSnapshot, error) {
	return nil, nil
}
func (NoopPoolRepo) LoadAllByStatus(context.Context, string, storage.SnapshotStatus) (map[string]*storage.PoolSnapshot, error) {
	return nil, nil
}
func (NoopPoolRepo) ListSnapshotStatuses(context.Context, string) (map[string]storage.SnapshotStatus, error) {
	return nil, nil
}
func (NoopPoolRepo) SetSnapshotStatus(context.Context, string, string, storage.SnapshotStatus) error {
	return nil
}
func (NoopPoolRepo) LoadTokenMetadata(context.Context, string, string) (*storage.TokenMetadata, error) {
	return nil, nil
}
func (NoopPoolRepo) SaveTokenMetadata(context.Context, *storage.TokenMetadata) error { return nil }
func (NoopPoolRepo) Close()                                                          {}

// NoopSyncRepo 无数据库时的空 SyncRepo。
type NoopSyncRepo struct{}

func NewNoopSyncRepo() *NoopSyncRepo { return &NoopSyncRepo{} }

func (NoopSyncRepo) GetLastProcessedBlock(context.Context, string) (uint64, error) { return 0, nil }
func (NoopSyncRepo) SetLastProcessedBlock(context.Context, string, uint64) error { return nil }
