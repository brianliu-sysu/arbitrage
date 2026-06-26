package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	yaml := `
http_port: 9090
health_check_interval_sec: 60
log_file: "test.log"
max_hops: 3
chains:
  - name: "ethereum"
    ws_endpoint: "wss://example.com/ws"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    max_hops: 3
    bridge_tokens:
      - "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
        sync_from_block: 100
      - pool_address: "0x88e6A0c2dDD26FEEb64F039a2c41296FcB3f5640"
`

	path := writeTempConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if len(cfg.GetChains()) != 1 {
		t.Fatalf("expected 1 chain, got %d", len(cfg.GetChains()))
	}
	if cfg.GetChains()[0].WSEndpoint != "wss://example.com/ws" {
		t.Errorf("WSEndpoint = %q", cfg.GetChains()[0].WSEndpoint)
	}
	if cfg.HTTPPort != 9090 {
		t.Errorf("HTTPPort = %d, want 9090", cfg.HTTPPort)
	}
	if cfg.HealthCheckIntervalSec != 60 {
		t.Errorf("HealthCheckIntervalSec = %d", cfg.HealthCheckIntervalSec)
	}
	if cfg.LogFile != "test.log" {
		t.Errorf("LogFile = %q", cfg.LogFile)
	}
	if cfg.MaxHops != 3 {
		t.Errorf("MaxHops = %d, want 3", cfg.MaxHops)
	}
	if len(cfg.GetChains()[0].BridgeTokens) != 1 {
		t.Errorf("BridgeTokens len = %d, want 1", len(cfg.GetChains()[0].BridgeTokens))
	}
	if len(cfg.GetChains()[0].Pools) != 2 {
		t.Errorf("Pools len = %d, want 2", len(cfg.GetChains()[0].Pools))
	}
	if cfg.GetChains()[0].Pools[0].PoolAddress != "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8" {
		t.Errorf("Pools[0].PoolAddress mismatch")
	}
	if cfg.GetChains()[0].Pools[0].SyncFromBlock != 100 {
		t.Errorf("Pools[0].SyncFromBlock = %d, want 100", cfg.GetChains()[0].Pools[0].SyncFromBlock)
	}
}

func TestLoadDefaults(t *testing.T) {
	yaml := `
chains:
  - name: "ethereum"
    ws_endpoint: "wss://x.com"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`

	path := writeTempConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.HTTPPort != 8080 {
		t.Errorf("HTTPPort default = %d, want 8080", cfg.HTTPPort)
	}
	if cfg.HealthCheckIntervalSec != 30 {
		t.Errorf("HealthCheckIntervalSec default = %d, want 30", cfg.HealthCheckIntervalSec)
	}
	if cfg.MaxHops != 2 {
		t.Errorf("MaxHops default = %d, want 2", cfg.MaxHops)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadNoChains(t *testing.T) {
	yaml := `
	http_port: 8080
	`
	path := writeTempConfig(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for no chains configured")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	path := writeTempConfig(t, "not: [valid: yaml!!!")
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadWithTemplateVars(t *testing.T) {
	t.Setenv("ALCHEMY_KEY", "abc123")
	t.Setenv("CUSTOM_PORT", "9090")

	yaml := `
http_port: {{CUSTOM_PORT}}
chains:
  - name: "ethereum"
    ws_endpoint: "wss://eth-mainnet.g.alchemy.com/v2/{{ALCHEMY_KEY}}"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	path := writeTempConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.GetChains()[0].WSEndpoint != "wss://eth-mainnet.g.alchemy.com/v2/abc123" {
		t.Errorf("WSEndpoint = %q, want with env var substituted", cfg.GetChains()[0].WSEndpoint)
	}
	if cfg.HTTPPort != 9090 {
		t.Errorf("HTTPPort = %d, want 9090", cfg.HTTPPort)
	}
}

func TestLoadMissingEnvVar(t *testing.T) {
	yaml := `
chains:
  - name: "ethereum"
    ws_endpoint: "{{MISSING_VAR_THAT_DOES_NOT_EXIST}}"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	path := writeTempConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	// 缺失的变量保留原文本，不报错
	if cfg.GetChains()[0].WSEndpoint != "{{MISSING_VAR_THAT_DOES_NOT_EXIST}}" {
		t.Errorf("WSEndpoint = %q, want template left as-is", cfg.GetChains()[0].WSEndpoint)
	}
}

func TestLoadMultiVarsInOneLine(t *testing.T) {
	t.Setenv("HOST", "mainnet.infura.io")
	t.Setenv("KEY", "secret123")

	yaml := `
chains:
  - name: "ethereum"
    ws_endpoint: "wss://{{HOST}}/ws/v3/{{KEY}}"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	path := writeTempConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.GetChains()[0].WSEndpoint != "wss://mainnet.infura.io/ws/v3/secret123" {
		t.Errorf("WSEndpoint = %q", cfg.GetChains()[0].WSEndpoint)
	}
}

func TestRPCEndpointAutoDerive(t *testing.T) {
	yaml := `
chains:
  - name: "ethereum"
    ws_endpoint: "wss://eth-mainnet.g.alchemy.com/v2/key123"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	path := writeTempConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.GetChains()[0].RPCEndpoint != "https://eth-mainnet.g.alchemy.com/v2/key123" {
		t.Errorf("RPCEndpoint = %q, want https://...", cfg.GetChains()[0].RPCEndpoint)
	}
}

func TestRPCEndpointExplicit(t *testing.T) {
	yaml := `
chains:
  - name: "ethereum"
    ws_endpoint: "wss://example.com/ws"
    rpc_endpoint: "https://custom-rpc.example.com"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	path := writeTempConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.GetChains()[0].RPCEndpoint != "https://custom-rpc.example.com" {
		t.Errorf("RPCEndpoint = %q, want explicit value", cfg.GetChains()[0].RPCEndpoint)
	}
}

func TestRPCEndpointDeriveWS(t *testing.T) {
	yaml := `
chains:
  - name: "ethereum"
    ws_endpoint: "ws://localhost:8546"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
`
	path := writeTempConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.GetChains()[0].RPCEndpoint != "http://localhost:8546" {
		t.Errorf("RPCEndpoint = %q, want http://localhost:8546", cfg.GetChains()[0].RPCEndpoint)
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}
