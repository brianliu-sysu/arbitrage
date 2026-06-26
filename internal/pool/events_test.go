package pool

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// buildSwapLog 构建一个有效的 Swap 事件 log 用于测试。
func buildSwapLog(t *testing.T, sender, recipient common.Address, amount0, amount1, sqrtPriceX96, liquidity, tick *big.Int) types.Log {
	t.Helper()

	int256, _ := abi.NewType("int256", "", nil)
	uint160, _ := abi.NewType("uint160", "", nil)
	uint128, _ := abi.NewType("uint128", "", nil)
	int24, _ := abi.NewType("int24", "", nil)

	args := abi.Arguments{
		{Type: int256},
		{Type: int256},
		{Type: uint160},
		{Type: uint128},
		{Type: int24},
	}

	data, err := args.Pack(amount0, amount1, sqrtPriceX96, liquidity, tick)
	if err != nil {
		t.Fatalf("pack Swap data: %v", err)
	}

	return types.Log{
		Topics: []common.Hash{
			SwapEventSignature,
			common.BytesToHash(sender.Bytes()),
			common.BytesToHash(recipient.Bytes()),
		},
		Data: data,
	}
}

// int24ToTopic 将 int24 值编码为 indexed topic hash。
// Solidity 将 int24 sign-extend 到 256 位（32 字节）。
func int24ToTopic(v int32) common.Hash {
	var b [32]byte
	if v >= 0 {
		b[31] = byte(v & 0xFF)
		b[30] = byte((v >> 8) & 0xFF)
		b[29] = byte((v >> 16) & 0xFF)
	} else {
		// 使用 256 位无符号表示: 2^256 + v
		u := big.NewInt(0).Add(
			new(big.Int).Lsh(big.NewInt(1), 256),
			big.NewInt(int64(v)),
		)
		raw := u.Bytes()
		copy(b[32-len(raw):], raw)
	}
	return common.BytesToHash(b[:])
}

// buildMintLog 构建 Mint 事件 log。
func buildMintLog(t *testing.T, owner common.Address, tickLower, tickUpper int32, amount, amount0, amount1 *big.Int) types.Log {
	t.Helper()

	addressType, _ := abi.NewType("address", "", nil)
	uint128, _ := abi.NewType("uint128", "", nil)
	uint256, _ := abi.NewType("uint256", "", nil)

	args := abi.Arguments{
		{Type: addressType},
		{Type: uint128},
		{Type: uint256},
		{Type: uint256},
	}

	sender := common.HexToAddress("0x0000000000000000000000000000000000000001")
	data, err := args.Pack(sender, amount, amount0, amount1)
	if err != nil {
		t.Fatalf("pack Mint data: %v", err)
	}

	return types.Log{
		Topics: []common.Hash{
			MintEventSignature,
			common.BytesToHash(owner.Bytes()),
			int24ToTopic(tickLower),
			int24ToTopic(tickUpper),
		},
		Data: data,
	}
}

// buildBurnLog 构建 Burn 事件 log。
func buildBurnLog(t *testing.T, owner common.Address, tickLower, tickUpper int32, amount, amount0, amount1 *big.Int) types.Log {
	t.Helper()

	uint128, _ := abi.NewType("uint128", "", nil)
	uint256, _ := abi.NewType("uint256", "", nil)

	args := abi.Arguments{
		{Type: uint128},
		{Type: uint256},
		{Type: uint256},
	}

	data, err := args.Pack(amount, amount0, amount1)
	if err != nil {
		t.Fatalf("pack Burn data: %v", err)
	}

	return types.Log{
		Topics: []common.Hash{
			BurnEventSignature,
			common.BytesToHash(owner.Bytes()),
			int24ToTopic(tickLower),
			int24ToTopic(tickUpper),
		},
		Data: data,
	}
}

func TestParseSwapEvent(t *testing.T) {
	sender := common.HexToAddress("0x1111111111111111111111111111111111111111")
	recipient := common.HexToAddress("0x2222222222222222222222222222222222222222")
	amount0 := big.NewInt(-1000000)
	amount1 := big.NewInt(500000000000000000)
	sqrtPriceX96 := new(big.Int).Lsh(big.NewInt(1), 96) // 2^96
	liquidity := big.NewInt(1000000000000000000)
	tick := big.NewInt(100)

	log := buildSwapLog(t, sender, recipient, amount0, amount1, sqrtPriceX96, liquidity, tick)

	event, err := ParseSwapEvent(log)
	if err != nil {
		t.Fatalf("ParseSwapEvent error: %v", err)
	}

	if event.Sender != sender {
		t.Errorf("Sender = %s, want %s", event.Sender.Hex(), sender.Hex())
	}
	if event.Recipient != recipient {
		t.Errorf("Recipient = %s, want %s", event.Recipient.Hex(), recipient.Hex())
	}
	if event.Amount0.Cmp(amount0) != 0 {
		t.Errorf("Amount0 = %s, want %s", event.Amount0.String(), amount0.String())
	}
	if event.Amount1.Cmp(amount1) != 0 {
		t.Errorf("Amount1 = %s, want %s", event.Amount1.String(), amount1.String())
	}
	if event.SqrtPriceX96.Cmp(sqrtPriceX96) != 0 {
		t.Errorf("SqrtPriceX96 mismatch")
	}
	if event.Liquidity.Cmp(liquidity) != 0 {
		t.Errorf("Liquidity mismatch")
	}
	if event.Tick != 100 {
		t.Errorf("Tick = %d, want 100", event.Tick)
	}
}

func TestParseSwapEventNegativeTick(t *testing.T) {
	sender := common.HexToAddress("0x1111111111111111111111111111111111111111")
	recipient := common.HexToAddress("0x2222222222222222222222222222222222222222")
	amount0 := big.NewInt(500000000)
	amount1 := big.NewInt(-2000000)
	sqrtPriceX96 := new(big.Int).Lsh(big.NewInt(1), 96)
	liquidity := big.NewInt(500000000000000000)
	tick := big.NewInt(-887272) // MinTick

	log := buildSwapLog(t, sender, recipient, amount0, amount1, sqrtPriceX96, liquidity, tick)

	event, err := ParseSwapEvent(log)
	if err != nil {
		t.Fatalf("ParseSwapEvent error: %v", err)
	}
	if event.Tick != -887272 {
		t.Errorf("Tick = %d, want -887272", event.Tick)
	}
}

func TestParseSwapEventInsufficientTopics(t *testing.T) {
	log := types.Log{
		Topics: []common.Hash{SwapEventSignature},
	}
	_, err := ParseSwapEvent(log)
	if err == nil {
		t.Error("expected error for insufficient topics")
	}
}

func TestParseMintEvent(t *testing.T) {
	owner := common.HexToAddress("0x3333333333333333333333333333333333333333")
	tickLower := int32(-200)
	tickUpper := int32(200)
	amount := big.NewInt(1000000)
	amount0 := big.NewInt(500000000)
	amount1 := big.NewInt(0)

	log := buildMintLog(t, owner, tickLower, tickUpper, amount, amount0, amount1)
	event, err := ParseMintEvent(log)
	if err != nil {
		t.Fatalf("ParseMintEvent error: %v", err)
	}

	if event.Owner != owner {
		t.Errorf("Owner = %s, want %s", event.Owner.Hex(), owner.Hex())
	}
	if event.TickLower != tickLower {
		t.Errorf("TickLower = %d, want %d", event.TickLower, tickLower)
	}
	if event.TickUpper != tickUpper {
		t.Errorf("TickUpper = %d, want %d", event.TickUpper, tickUpper)
	}
	if event.Amount.Cmp(amount) != 0 {
		t.Errorf("Amount mismatch")
	}
	if event.Amount0.Cmp(amount0) != 0 {
		t.Errorf("Amount0 mismatch")
	}
	if event.Amount1.Cmp(amount1) != 0 {
		t.Errorf("Amount1 mismatch")
	}
}

func TestParseMintEventInsufficientTopics(t *testing.T) {
	log := types.Log{
		Topics: []common.Hash{MintEventSignature},
	}
	_, err := ParseMintEvent(log)
	if err == nil {
		t.Error("expected error for insufficient topics")
	}
}

func TestParseBurnEvent(t *testing.T) {
	owner := common.HexToAddress("0x4444444444444444444444444444444444444444")
	tickLower := int32(-100)
	tickUpper := int32(100)
	amount := big.NewInt(500000)
	amount0 := big.NewInt(0)
	amount1 := big.NewInt(200000000)

	log := buildBurnLog(t, owner, tickLower, tickUpper, amount, amount0, amount1)
	event, err := ParseBurnEvent(log)
	if err != nil {
		t.Fatalf("ParseBurnEvent error: %v", err)
	}

	if event.Owner != owner {
		t.Errorf("Owner = %s, want %s", event.Owner.Hex(), owner.Hex())
	}
	if event.TickLower != tickLower {
		t.Errorf("TickLower = %d, want %d", event.TickLower, tickLower)
	}
	if event.TickUpper != tickUpper {
		t.Errorf("TickUpper = %d, want %d", event.TickUpper, tickUpper)
	}
	if event.Amount.Cmp(amount) != 0 {
		t.Errorf("Amount mismatch")
	}
	if event.Amount0.Cmp(amount0) != 0 {
		t.Errorf("Amount0 mismatch")
	}
	if event.Amount1.Cmp(amount1) != 0 {
		t.Errorf("Amount1 mismatch")
	}
}

func TestParseBurnEventInsufficientTopics(t *testing.T) {
	log := types.Log{
		Topics: []common.Hash{BurnEventSignature},
	}
	_, err := ParseBurnEvent(log)
	if err == nil {
		t.Error("expected error for insufficient topics")
	}
}

func TestEventSignatures(t *testing.T) {
	// 验证事件签名不为空
	if SwapEventSignature == (common.Hash{}) {
		t.Error("SwapEventSignature is zero")
	}
	if MintEventSignature == (common.Hash{}) {
		t.Error("MintEventSignature is zero")
	}
	if BurnEventSignature == (common.Hash{}) {
		t.Error("BurnEventSignature is zero")
	}

	// 验证签名唯一性
	if SwapEventSignature == MintEventSignature {
		t.Error("Swap and Mint signatures should differ")
	}
	if SwapEventSignature == BurnEventSignature {
		t.Error("Swap and Burn signatures should differ")
	}

	// 验证签名与已知值匹配
	expectedSwapSig := crypto.Keccak256Hash([]byte("Swap(address,address,int256,int256,uint160,uint128,int24)"))
	if SwapEventSignature != expectedSwapSig {
		t.Errorf("SwapEventSignature mismatch")
	}

	expectedMintSig := crypto.Keccak256Hash([]byte("Mint(address,address,int24,int24,uint128,uint256,uint256)"))
	if MintEventSignature != expectedMintSig {
		t.Errorf("MintEventSignature mismatch")
	}

	expectedBurnSig := crypto.Keccak256Hash([]byte("Burn(address,int24,int24,uint128,uint256,uint256)"))
	if BurnEventSignature != expectedBurnSig {
		t.Errorf("BurnEventSignature mismatch")
	}
}
