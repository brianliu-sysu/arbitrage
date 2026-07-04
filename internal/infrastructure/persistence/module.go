package persistence

import (
	"context"
	"fmt"
	"os"

	syncv3 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/v3"
	syncv4 "github.com/brianliu-sysu/uniswapv3/internal/application/sync/v4"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v3"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/memory"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/postgres"
)

// Config selects the persistence backend.
type Config struct {
	DatabaseURL string
	UseMemory   bool
}

// Services bundles repository implementations.
type Services struct {
	Pools         marketv3.PoolRepository
	Snapshots     marketv3.SnapshotRepository
	Checkpoints   blockchain.CheckpointRepository
	Opportunities arbitrage.OpportunityRepository
	V4Pools       marketv4.PoolRepository
	V4Snapshots   marketv4.SnapshotRepository
	V4Checkpoints blockchain.V4CheckpointRepository
	Postgres      *postgres.DB

	memory *memoryBundle
}

type memoryBundle struct {
	pools         *memory.PoolRepository
	snapshots     *memory.SnapshotRepository
	checkpoints   *memory.CheckpointRepository
	opportunities *memory.OpportunityRepository
	v4Pools       *memory.V4PoolRepository
	v4Snapshots   *memory.V4SnapshotRepository
	v4Checkpoints *memory.V4CheckpointRepository
}

// MemoryServices returns in-memory repositories for local development and tests.
func MemoryServices() *Services {
	bundle := &memoryBundle{
		pools:         memory.NewPoolRepository(),
		snapshots:     memory.NewSnapshotRepository(),
		checkpoints:   memory.NewCheckpointRepository(),
		opportunities: memory.NewOpportunityRepository(),
		v4Pools:       memory.NewV4PoolRepository(),
		v4Snapshots:   memory.NewV4SnapshotRepository(),
		v4Checkpoints: memory.NewV4CheckpointRepository(),
	}
	return &Services{
		Pools:         bundle.pools,
		Snapshots:     bundle.snapshots,
		Checkpoints:   bundle.checkpoints,
		Opportunities: bundle.opportunities,
		V4Pools:       bundle.v4Pools,
		V4Snapshots:   bundle.v4Snapshots,
		V4Checkpoints: bundle.v4Checkpoints,
		memory:        bundle,
	}
}

// NewServices creates persistence repositories from config.
func NewServices(ctx context.Context, cfg Config) (*Services, error) {
	if cfg.UseMemory || cfg.DatabaseURL == "" {
		return MemoryServices(), nil
	}

	db, err := postgres.NewDB(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	return &Services{
		Pools:         postgres.NewPoolRepository(db),
		Snapshots:     postgres.NewSnapshotRepository(db),
		Checkpoints:   postgres.NewCheckpointRepository(db),
		Opportunities: postgres.NewOpportunityRepository(db),
		V4Pools:       memory.NewV4PoolRepository(),
		V4Snapshots:   memory.NewV4SnapshotRepository(),
		V4Checkpoints: memory.NewV4CheckpointRepository(),
		Postgres:      db,
	}, nil
}

// SyncDeps returns repositories for sync application wiring.
func (s *Services) SyncDeps() syncv3.ServiceDeps {
	deps := syncv3.ServiceDeps{
		Pools:       s.Pools,
		Snapshots:   s.Snapshots,
		Checkpoints: s.Checkpoints,
	}
	if s.Postgres != nil {
		deps.Health = append(deps.Health, s.Postgres)
	}
	return deps
}

// SyncV4Deps returns repositories for V4 sync application wiring.
func (s *Services) SyncV4Deps() syncv4.ServiceDeps {
	deps := syncv4.ServiceDeps{
		Pools:       s.V4Pools,
		Snapshots:   s.V4Snapshots,
		Checkpoints: s.V4Checkpoints,
	}
	if s.Postgres != nil {
		deps.Health = append(deps.Health, s.Postgres)
	}
	return deps
}

func (s *Services) Close() {
	if s.Postgres != nil {
		s.Postgres.Close()
	}
}

// ConfigFromEnv loads persistence config from environment variables.
func ConfigFromEnv() Config {
	return Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		UseMemory:   os.Getenv("USE_MEMORY_DB") == "true",
	}
}

// Validate ensures required repositories are available.
func (s *Services) Validate() error {
	if s.Pools == nil || s.Snapshots == nil || s.Checkpoints == nil {
		return fmt.Errorf("persistence repositories are not configured")
	}
	return nil
}

// BackendName returns the active persistence backend label.
func (s *Services) BackendName() string {
	if s.Postgres != nil {
		return "postgres"
	}
	return "memory"
}
