package persistence

import (
	"context"
	"fmt"
	"os"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/arbitrage"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/asset"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
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
	Pools                marketv3.PoolRepository
	Snapshots            marketv3.SnapshotRepository
	Checkpoints          blockchain.CheckpointRepository
	PancakePools         marketpancake.PoolRepository
	PancakeSnapshots     marketpancake.SnapshotRepository
	PancakeCheckpoints   blockchain.CheckpointRepository
	QuickSwapPools       marketquick.PoolRepository
	QuickSwapSnapshots   marketquick.SnapshotRepository
	QuickSwapCheckpoints blockchain.CheckpointRepository
	Opportunities        arbitrage.OpportunityRepository
	V4Pools              marketv4.PoolRepository
	V4Snapshots          marketv4.SnapshotRepository
	V4Checkpoints        blockchain.V4CheckpointRepository
	BalancerPools        marketbalancer.PoolRepository
	BalancerSnapshots    marketbalancer.SnapshotRepository
	BalancerCheckpoints  blockchain.BalancerCheckpointRepository
	Tokens               asset.TokenRepository
	Postgres             *postgres.DB

	memory *memoryBundle
}

type memoryBundle struct {
	pools                *memory.PoolRepository
	snapshots            *memory.SnapshotRepository
	checkpoints          *memory.CheckpointRepository
	pancakePools         *memory.PancakePoolRepository
	pancakeSnapshots     *memory.PancakeSnapshotRepository
	pancakeCheckpoints   *memory.CheckpointRepository
	quickSwapPools       *memory.QuickSwapPoolRepository
	quickSwapSnapshots   *memory.QuickSwapSnapshotRepository
	quickSwapCheckpoints *memory.CheckpointRepository
	opportunities        *memory.OpportunityRepository
	v4Pools              *memory.V4PoolRepository
	v4Snapshots          *memory.V4SnapshotRepository
	v4Checkpoints        *memory.V4CheckpointRepository
	balancerPools        *memory.BalancerPoolRepository
	balancerSnapshots    *memory.BalancerSnapshotRepository
	balancerCheckpoints  *memory.BalancerCheckpointRepository
	tokens               *memory.TokenRepository
}

// MemoryServices returns in-memory repositories for local development and tests.
func MemoryServices() *Services {
	bundle := &memoryBundle{
		pools:                memory.NewPoolRepository(),
		snapshots:            memory.NewSnapshotRepository(),
		checkpoints:          memory.NewCheckpointRepository(),
		pancakePools:         memory.NewPancakePoolRepository(),
		pancakeSnapshots:     memory.NewPancakeSnapshotRepository(),
		pancakeCheckpoints:   memory.NewCheckpointRepository(),
		quickSwapPools:       memory.NewQuickSwapPoolRepository(),
		quickSwapSnapshots:   memory.NewQuickSwapSnapshotRepository(),
		quickSwapCheckpoints: memory.NewCheckpointRepository(),
		opportunities:        memory.NewOpportunityRepository(),
		v4Pools:              memory.NewV4PoolRepository(),
		v4Snapshots:          memory.NewV4SnapshotRepository(),
		v4Checkpoints:        memory.NewV4CheckpointRepository(),
		balancerPools:        memory.NewBalancerPoolRepository(),
		balancerSnapshots:    memory.NewBalancerSnapshotRepository(),
		balancerCheckpoints:  memory.NewBalancerCheckpointRepository(),
		tokens:               memory.NewTokenRepository(),
	}
	return &Services{
		Pools:                bundle.pools,
		Snapshots:            bundle.snapshots,
		Checkpoints:          bundle.checkpoints,
		PancakePools:         bundle.pancakePools,
		PancakeSnapshots:     bundle.pancakeSnapshots,
		PancakeCheckpoints:   bundle.pancakeCheckpoints,
		QuickSwapPools:       bundle.quickSwapPools,
		QuickSwapSnapshots:   bundle.quickSwapSnapshots,
		QuickSwapCheckpoints: bundle.quickSwapCheckpoints,
		Opportunities:        bundle.opportunities,
		V4Pools:              bundle.v4Pools,
		V4Snapshots:          bundle.v4Snapshots,
		V4Checkpoints:        bundle.v4Checkpoints,
		BalancerPools:        bundle.balancerPools,
		BalancerSnapshots:    bundle.balancerSnapshots,
		BalancerCheckpoints:  bundle.balancerCheckpoints,
		Tokens:               bundle.tokens,
		memory:               bundle,
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
		Pools:                postgres.NewPoolRepository(db),
		Snapshots:            postgres.NewSnapshotRepository(db),
		Checkpoints:          postgres.NewCheckpointRepository(db),
		PancakePools:         postgres.NewPancakePoolRepository(db),
		PancakeSnapshots:     postgres.NewPancakeSnapshotRepository(db),
		PancakeCheckpoints:   postgres.NewPancakeCheckpointRepository(db),
		QuickSwapPools:       postgres.NewQuickSwapPoolRepository(db),
		QuickSwapSnapshots:   postgres.NewQuickSwapSnapshotRepository(db),
		QuickSwapCheckpoints: postgres.NewQuickSwapCheckpointRepository(db),
		Opportunities:        postgres.NewOpportunityRepository(db),
		V4Pools:              postgres.NewV4PoolRepository(db),
		V4Snapshots:          postgres.NewV4SnapshotRepository(db),
		V4Checkpoints:        postgres.NewV4CheckpointRepository(db),
		BalancerPools:        postgres.NewBalancerPoolRepository(db),
		BalancerSnapshots:    postgres.NewBalancerSnapshotRepository(db),
		BalancerCheckpoints:  postgres.NewBalancerCheckpointRepository(db),
		Tokens:               postgres.NewTokenRepository(db),
		Postgres:             db,
	}, nil
}

func (s *Services) Close() {
	if s.Postgres != nil {
		s.Postgres.Close()
	}
}

// ConfigFromEnv loads persistence config from environment variables.
func ConfigFromEnv() Config {
	useMemory := os.Getenv("USE_MEMORY_DB") == "true" || os.Getenv("USE_MEMORY_DB") == "1"
	return Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		UseMemory:   useMemory,
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
