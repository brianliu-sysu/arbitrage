package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/migrate"
)

func main() {
	configPath := flag.String("config", config.DefaultPath, "path to config yaml")
	migrationsDir := flag.String("migrations", "migrations", "path to migrations directory")
	databaseURL := flag.String("database-url", "", "optional database url override")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		exitErr(fmt.Errorf("load config: %w", err))
	}

	url := cfg.Database.URL
	if *databaseURL != "" {
		url = *databaseURL
	}
	if url == "" {
		exitErr(fmt.Errorf("database url is required; set database.url in config or -database-url"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := migrate.Run(ctx, url, *migrationsDir); err != nil {
		exitErr(err)
	}

	fmt.Println("migrations completed")
}

func exitErr(err error) {
	fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
	os.Exit(1)
}
