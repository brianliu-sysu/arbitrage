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

func TestLoadFlashLoanFeeConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
persistence:
  memory: true
arbitrage:
  flash_loan:
    balancer_fee_ppm: "1"
    univ4_fee_ppm: "2"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Arbitrage.FlashLoan.BalancerFee().String() != "1" {
		t.Fatalf("expected balancer fee 1, got %s", cfg.Arbitrage.FlashLoan.BalancerFee())
	}
	if cfg.Arbitrage.FlashLoan.Univ4Fee().String() != "2" {
		t.Fatalf("expected univ4 fee 2, got %s", cfg.Arbitrage.FlashLoan.Univ4Fee())
	}
}

func TestLoadMultiChainConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
persistence:
  memory: true
blockchain:
  multicall_address: "0xcA11bde05977b3631167028862bE2a173976CA11"
chains:
  - name: ethereum
    chain_id: 1
    rpc:
      url: "https://eth.example"
      ws_url: "wss://eth.example"
    sync:
      univ3:
        enabled: true
        pools:
          - address: "0x88e6A0c2dDD26FEEb64F039a2c41296Fb728693B"
            fee: 500
    arbitrage:
      triangle:
        enabled: true
        start_tokens:
          - "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
        min_net_profit_wei: "100"
  - name: base
    chain_id: 8453
    rpc:
      url: "https://base.example"
      ws_url: "wss://base.example"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	chains := cfg.NormalizedChains()
	if len(chains) != 2 {
		t.Fatalf("expected 2 chains, got %d", len(chains))
	}
	if chains[0].Name != "ethereum" || chains[0].ChainID != 1 || chains[0].RPC.URL != "https://eth.example" {
		t.Fatalf("unexpected first chain: %+v", chains[0])
	}
	if !chains[0].Sync.Univ3.IsActive() || !chains[0].Arbitrage.Triangle.Enabled {
		t.Fatalf("expected chain-local sync and arbitrage config, got sync=%+v arbitrage=%+v", chains[0].Sync.Univ3, chains[0].Arbitrage.Triangle)
	}
	if chains[1].Name != "base" || chains[1].ChainID != 8453 || chains[1].RPC.WSURL != "wss://base.example" {
		t.Fatalf("unexpected second chain: %+v", chains[1])
	}
	if chains[1].Blockchain.MulticallAddress != "0xcA11bde05977b3631167028862bE2a173976CA11" {
		t.Fatalf("expected inherited multicall address, got %s", chains[1].Blockchain.MulticallAddress)
	}
}

func TestNormalizedChainsSkipsDisabledChains(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
persistence:
  memory: true
chains:
  - name: ethereum
    enabled: true
    chain_id: 1
    rpc:
      url: "https://eth.example"
      ws_url: "wss://eth.example"
  - name: polygon
    enabled: false
    chain_id: 137
    rpc:
      url: "https://polygon.example"
      ws_url: "wss://polygon.example"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	chains := cfg.NormalizedChains()
	if len(chains) != 1 {
		t.Fatalf("expected 1 enabled chain, got %d", len(chains))
	}
	if chains[0].Name != "ethereum" || chains[0].ChainID != 1 {
		t.Fatalf("unexpected enabled chain: %+v", chains[0])
	}
}

func TestLoadFailsWhenAllChainsDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
persistence:
  memory: true
chains:
  - name: ethereum
    enabled: false
    chain_id: 1
    rpc:
      url: "https://eth.example"
      ws_url: "wss://eth.example"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := config.Load(path); err == nil {
		t.Fatal("expected error when all chains are disabled")
	}
}

func TestExampleConfigFilesLoad(t *testing.T) {
	for _, path := range []string{
		filepath.Join("..", "..", "config.yaml"),
		filepath.Join("..", "..", "configs", "config.yaml"),
	} {
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		chains := cfg.NormalizedChains()
		if len(chains) != 2 {
			t.Fatalf("expected two configured chains in %s, got %d", path, len(chains))
		}
		if chains[0].Sync.Univ3.Enabled == false {
			t.Fatalf("expected chain-local sync config in %s", path)
		}
		if chains[1].Name != "polygon" || chains[1].ChainID != 137 {
			t.Fatalf("expected polygon as second configured chain in %s, got %+v", path, chains[1])
		}
		if !chains[1].Sync.Univ3.IsActive() {
			t.Fatalf("expected polygon univ3 sync active in %s, got %+v", path, chains[1].Sync.Univ3)
		}
		if chains[1].Blockchain.PoolManagerAddress != "0x67366782805870060151383f4bbff9dab53e5cd6" {
			t.Fatalf("expected polygon univ4 pool manager in %s, got %s", path, chains[1].Blockchain.PoolManagerAddress)
		}
		if chains[1].Sync.QuickSwapV3.Subgraph.OrderBy != "volume24h" {
			t.Fatalf("expected polygon quickswapv3 config in %s, got %+v", path, chains[1].Sync.QuickSwapV3)
		}
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
