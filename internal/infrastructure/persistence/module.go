package persistence

import (
	"context"
	"fmt"
	"os"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
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
	Pools         market.PoolRepository
	Snapshots     market.SnapshotRepository
	Checkpoints   blockchain.CheckpointRepository
	Opportunities arbitrage.OpportunityRepository
	Postgres      *postgres.DB

	memory *memoryBundle
}

type memoryBundle struct {
	pools         *memory.PoolRepository
	snapshots     *memory.SnapshotRepository
	checkpoints   *memory.CheckpointRepository
	opportunities *memory.OpportunityRepository
}

// MemoryServices returns in-memory repositories for local development and tests.
func MemoryServices() *Services {
	bundle := &memoryBundle{
		pools:         memory.NewPoolRepository(),
		snapshots:     memory.NewSnapshotRepository(),
		checkpoints:   memory.NewCheckpointRepository(),
		opportunities: memory.NewOpportunityRepository(),
	}
	return &Services{
		Pools:         bundle.pools,
		Snapshots:     bundle.snapshots,
		Checkpoints:   bundle.checkpoints,
		Opportunities: bundle.opportunities,
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
		Postgres:      db,
	}, nil
}

// SyncDeps returns repositories for sync application wiring.
func (s *Services) SyncDeps() syncapp.ServiceDeps {
	deps := syncapp.ServiceDeps{
		Pools:       s.Pools,
		Snapshots:   s.Snapshots,
		Checkpoints: s.Checkpoints,
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
