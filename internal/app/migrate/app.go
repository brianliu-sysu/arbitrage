package app

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/migrate"
)

const defaultMigrationsDir = "migrations"

// Params holds runtime options for the migrate command.
type Params struct {
	ConfigPath    string
	MigrationsDir string
	DatabaseURL   string
}

// ParseFlags parses standard CLI flags for the migrate binary.
func ParseFlags() Params {
	configPath := flag.String("config", config.DefaultPath, "path to config yaml")
	migrationsDir := flag.String("migrations", defaultMigrationsDir, "path to migrations directory")
	databaseURL := flag.String("database-url", "", "optional database url override")
	flag.Parse()

	return Params{
		ConfigPath:    *configPath,
		MigrationsDir: *migrationsDir,
		DatabaseURL:   *databaseURL,
	}
}

// Run loads config and applies pending database migrations.
func Run(params Params) error {
	cfg, err := config.Load(params.ConfigPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	databaseURL := cfg.DatabaseURL()
	if params.DatabaseURL != "" {
		databaseURL = params.DatabaseURL
	}
	if cfg.MemoryMode() {
		return fmt.Errorf("database migrations are not available in persistence.memory mode")
	}
	if databaseURL == "" {
		return fmt.Errorf("database url is required; set persistence.database.url in config or -database-url")
	}

	migrationsDir := params.MigrationsDir
	if migrationsDir == "" {
		migrationsDir = defaultMigrationsDir
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := migrate.Run(ctx, databaseURL, migrationsDir); err != nil {
		return err
	}

	fmt.Println("migrations completed")
	return nil
}
