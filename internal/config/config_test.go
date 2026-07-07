package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
)

func TestLoadSyncV3Config(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
persistence:
  memory: false
  database:
    url: postgres://localhost/univ3
sync:
  univ3:
    enabled: true
    pools:
      - address: "0x88e6A0c2dDD26FEEb64F039a2c41296Fb728693B"
        fee: 500
    subgraph:
      enabled: true
      endpoint: "https://example.com/subgraph"
      first: 50
      fee_tiers: [500, 3000]
  univ4:
    enabled: false
    poolmanager:
      pools: []
    subgraph:
      enabled: false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.StaticPoolAddresses()) != 1 {
		t.Fatalf("expected 1 static pool")
	}
	if !cfg.Sync.Univ3.IsActive() {
		t.Fatal("expected univ3 sync active")
	}
	if !cfg.SubgraphPoolSource().IsEnabled() {
		t.Fatal("expected subgraph source enabled")
	}
	if cfg.SubgraphPoolSource().First != 50 {
		t.Fatalf("expected subgraph first=50, got %d", cfg.SubgraphPoolSource().First)
	}
}

func TestLoadMemoryModeConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
persistence:
  memory: true
sync:
  univ3:
    enabled: true
    pools:
      - address: "0x88e6A0c2dDD26FEEb64F039a2c41296Fb728693B"
        fee: 500
  univ4:
    enabled: false
    poolmanager:
      pools: []
    subgraph:
      enabled: false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.MemoryMode() {
		t.Fatal("expected memory mode enabled")
	}

	pCfg := cfg.PersistenceConfig()
	if !pCfg.UseMemory {
		t.Fatal("expected persistence config to use memory")
	}
	if pCfg.DatabaseURL != "" {
		t.Fatalf("expected empty database url in memory mode, got %q", pCfg.DatabaseURL)
	}
}

func TestLogConfigResolvedPaths(t *testing.T) {
	cfg := config.LogConfig{
		File:      "logs/arbitrage.log",
		ErrorFile: "logs/arbitrage.error.log",
	}

	outputPaths := cfg.ResolvedOutputPaths()
	if len(outputPaths) != 2 || outputPaths[0] != "stdout" || outputPaths[1] != "logs/arbitrage.log" {
		t.Fatalf("unexpected output paths: %#v", outputPaths)
	}

	errorPaths := cfg.ResolvedErrorOutputPaths()
	if len(errorPaths) != 2 || errorPaths[0] != "stderr" || errorPaths[1] != "logs/arbitrage.error.log" {
		t.Fatalf("unexpected error output paths: %#v", errorPaths)
	}
}

func TestValidateRejectsShortV4PoolManagerAddress(t *testing.T) {
	cfg := config.Default()
	cfg.Persistence.Memory = true
	cfg.Sync.Univ3.Enabled = false
	cfg.Sync.Univ3.Subgraph.Enabled = false
	cfg.Sync.Univ4.Enabled = true
	cfg.Sync.Univ4.Subgraph.Enabled = true
	cfg.Sync.Univ4.Subgraph.Endpoint = "https://example.com/subgraph"
	cfg.Blockchain.PoolManagerAddress = "0x000000000004444c5dc75cb35838093bd135961"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected invalid pool manager address error")
	}
}
