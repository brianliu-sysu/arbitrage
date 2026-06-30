package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/config"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/storage"
	"go.uber.org/fx"
)

// Module 提供 PostgreSQL Repository 依赖。
var Module = fx.Module(
	"storage.postgres",
	fx.Provide(newRepositories),
	fx.Invoke(registerStorageLifecycle),
)

type Repositories struct {
	Pool storage.PoolRepo
	Sync storage.SyncRepo
}

type repoRuntime struct {
	cfg  *config.AppConfig
	log  logx.Logger
	pool storage.PoolRepo
}

func newRepositories(cfg *config.AppConfig, logger logx.Logger) (*Repositories, *repoRuntime, error) {
	if cfg.DBURL == "" {
		noop := NewNoopPoolRepo()
		return &Repositories{
			Pool: noop,
			Sync: NewNoopSyncRepo(),
		}, &repoRuntime{cfg: cfg, log: logger, pool: noop}, nil
	}

	store, err := NewStore(context.Background(), cfg.DBURL)
	if err != nil {
		return nil, nil, fmt.Errorf("connect postgres: %w", err)
	}
	return &Repositories{
		Pool: store,
		Sync: NewSyncRepo(store),
	}, &repoRuntime{cfg: cfg, log: logger, pool: store}, nil
}

func registerStorageLifecycle(lc fx.Lifecycle, rt *repoRuntime) {
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			if rt.cfg.DBURL == "" {
				rt.log.Info("storage disabled")
				return nil
			}
			if err := RunMigrations(rt.cfg.DBURL); err != nil {
				return fmt.Errorf("run migrations: %w", err)
			}
			rt.log.Info("storage connected and migrated")
			return nil
		},
		OnStop: func(context.Context) error {
			if rt.pool != nil {
				rt.pool.Close()
			}
			return nil
		},
	})
}

// RunMigrations 执行 migrations 目录下的 SQL。
func RunMigrations(connString string) error {
	return runMigrations(connString)
}

func runMigrations(connString string) error {
	db, err := openSQL(connString)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS goose_db_version (
		id SERIAL PRIMARY KEY,
		version_id BIGINT NOT NULL,
		is_applied BOOLEAN NOT NULL DEFAULT true,
		tstamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		return fmt.Errorf("create version table: %w", err)
	}

	migrationsDir := findMigrationsDir()
	files, _ := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	sort.Strings(files)

	for _, f := range files {
		name := filepath.Base(f)
		var version int64
		fmt.Sscanf(name, "%d_", &version)

		var count int
		err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM goose_db_version WHERE version_id = $1 AND is_applied = TRUE`,
			version,
		).Scan(&count)
		if err == nil && count > 0 {
			continue
		}

		data, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}

		upSQL := extractSection(string(data), "Up")
		if upSQL == "" {
			continue
		}

		if _, err := db.ExecContext(ctx, upSQL); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}

		if _, err := db.ExecContext(ctx,
			`INSERT INTO goose_db_version (version_id, is_applied) VALUES ($1, TRUE)`,
			version,
		); err != nil {
			return fmt.Errorf("record %s: %w", name, err)
		}
	}
	return nil
}

func findMigrationsDir() string {
	candidates := []string{"migrations", "../migrations", "../../migrations"}
	if cwd, err := os.Getwd(); err == nil {
		for range 3 {
			cwd = filepath.Dir(cwd)
			candidates = append(candidates, filepath.Join(cwd, "migrations"))
		}
	}
	for _, dir := range candidates {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir
		}
	}
	return "migrations"
}

func extractSection(content, section string) string {
	marker := "-- +goose " + section
	idx := strings.Index(content, marker)
	if idx < 0 {
		return ""
	}
	start := strings.Index(content[idx:], "\n") + idx + 1
	remaining := content[start:]
	nextMarker := strings.Index(remaining, "\n-- +goose ")
	if nextMarker >= 0 {
		remaining = remaining[:nextMarker]
	}
	return strings.TrimSpace(remaining)
}
