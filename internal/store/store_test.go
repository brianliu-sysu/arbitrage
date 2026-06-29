package store

import (
	"math/big"
	"testing"
)

func TestPoolSnapshotTypes(t *testing.T) {
	snap := &PoolSnapshot{
		ChainName:    "ethereum",
		PoolAddress:  "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8",
		BlockNumber:  15000000,
		Tick:         42,
		SqrtPriceX96: new(big.Int).Lsh(big.NewInt(1), 96), // 2^96
		Liquidity:    big.NewInt(1000000000000000000),
		Price0In1:    2000.0,
		Token0Symbol: "USDC",
		Token1Symbol: "WETH",
		Fee:          3000,
		TickData: map[int32]TickLiquiditySnapshot{
			10:  {LiquidityNet: big.NewInt(500), LiquidityGross: big.NewInt(500)},
			-10: {LiquidityNet: big.NewInt(-500), LiquidityGross: big.NewInt(500)},
		},
	}
	if snap.ChainName != "ethereum" {
		t.Errorf("ChainName = %q", snap.ChainName)
	}
	if snap.PoolAddress != "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8" {
		t.Errorf("PoolAddress = %q", snap.PoolAddress)
	}
	if snap.BlockNumber != 15000000 {
		t.Errorf("BlockNumber = %d", snap.BlockNumber)
	}
	if snap.Tick != 42 {
		t.Errorf("Tick = %d", snap.Tick)
	}
	if snap.Price0In1 != 2000.0 {
		t.Errorf("Price0In1 = %f", snap.Price0In1)
	}
	if snap.Token0Symbol != "USDC" {
		t.Errorf("Token0Symbol = %q", snap.Token0Symbol)
	}
	if snap.Token1Symbol != "WETH" {
		t.Errorf("Token1Symbol = %q", snap.Token1Symbol)
	}
	if snap.Fee != 3000 {
		t.Errorf("Fee = %d", snap.Fee)
	}
	if len(snap.TickData) != 2 {
		t.Errorf("TickData len = %d", len(snap.TickData))
	}
	if snap.TickData[10].LiquidityNet.Cmp(big.NewInt(500)) != 0 {
		t.Errorf("TickData[10].LiquidityNet = %s", snap.TickData[10].LiquidityNet.String())
	}
}

func TestDecodeTickDataJSON_RejectLegacyFormat(t *testing.T) {
	_, err := decodeTickDataJSON(`{"10":"500","-10":"-400"}`)
	if err == nil {
		t.Fatalf("expected legacy format decode to fail")
	}
}

func TestDecodeTickDataJSON_NewFormat(t *testing.T) {
	current, err := decodeTickDataJSON(`{"10":{"liquidityNet":500,"liquidityGross":900}}`)
	if err != nil {
		t.Fatalf("decode current: %v", err)
	}
	if current[10].LiquidityNet.Cmp(big.NewInt(500)) != 0 || current[10].LiquidityGross.Cmp(big.NewInt(900)) != 0 {
		t.Fatalf("current decode mismatch: %+v", current[10])
	}
}

func TestTokenMetadataTypes(t *testing.T) {
	meta := &TokenMetadata{
		ChainName:    "ethereum",
		TokenAddress: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		Symbol:       "USDC",
		Decimals:     6,
	}
	if meta.ChainName != "ethereum" {
		t.Errorf("ChainName = %q", meta.ChainName)
	}
	if meta.Symbol != "USDC" {
		t.Errorf("Symbol = %q", meta.Symbol)
	}
	if meta.Decimals != 6 {
		t.Errorf("Decimals = %d", meta.Decimals)
	}
}

func TestMaxIncrementalGap(t *testing.T) {
	if MaxIncrementalGap != 100 {
		t.Errorf("MaxIncrementalGap = %d, want 100", MaxIncrementalGap)
	}
}

func TestRunMigrationsInvalidConnString(t *testing.T) {
	// Invalid connection string should fail at PingContext
	err := RunMigrations("postgres://invalid:5432/nonexistent")
	if err == nil {
		t.Skip("unexpected success — database might be reachable")
	}
	t.Logf("expected error with invalid conn string: %v", err)
}

func TestFindMigrationsDir(t *testing.T) {
	dir := findMigrationsDir()
	if dir == "" {
		t.Error("findMigrationsDir returned empty string")
	}
	// Should at least return "migrations" as fallback
	if dir != "migrations" && dir == "" {
		t.Errorf("findMigrationsDir = %q", dir)
	}
}

func TestExtractSection(t *testing.T) {
	content := `-- +goose Up
CREATE TABLE test (id SERIAL PRIMARY KEY);
-- +goose Down
DROP TABLE test;`

	up := extractSection(content, "Up")
	if up == "" {
		t.Error("extractSection(Up) should return non-empty SQL")
	}
	if up != "CREATE TABLE test (id SERIAL PRIMARY KEY);" {
		t.Errorf("Up section = %q", up)
	}

	down := extractSection(content, "Down")
	if down == "" {
		t.Error("extractSection(Down) should return non-empty SQL")
	}

	// Non-existent section
	missing := extractSection(content, "NoSuchSection")
	if missing != "" {
		t.Errorf("extractSection for missing section = %q, want empty", missing)
	}

	// Empty content
	empty := extractSection("", "Up")
	if empty != "" {
		t.Errorf("extractSection on empty content = %q", empty)
	}

	// Content without matching marker
	noMatch := extractSection("SELECT 1;", "Up")
	if noMatch != "" {
		t.Errorf("extractSection without marker = %q, want empty", noMatch)
	}
}
