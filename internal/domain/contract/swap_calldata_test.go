package contract

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestPackWETHWithdrawAmountOffset(t *testing.T) {
	amount := big.NewInt(12345)
	data, err := PackWETHWithdraw(amount)
	if err != nil {
		t.Fatalf("pack withdraw: %v", err)
	}
	if len(data) < WETHWithdrawAmountOffset+32 {
		t.Fatalf("calldata too short: %d", len(data))
	}
	got := new(big.Int).SetBytes(data[WETHWithdrawAmountOffset : WETHWithdrawAmountOffset+32])
	if got.Cmp(amount) != 0 {
		t.Fatalf("expected amount %s at offset, got %s", amount, got)
	}
}

func TestPackExactInputSingleAmountInOffset(t *testing.T) {
	amount := big.NewInt(999)
	data, err := PackExactInputSingle(ExactInputSingleParams{
		TokenIn:          common.HexToAddress("0x1"),
		TokenOut:         common.HexToAddress("0x2"),
		Fee:              big.NewInt(3000),
		Recipient:        common.HexToAddress("0x3"),
		AmountIn:         amount,
		AmountOutMinimum: big.NewInt(0),
	})
	if err != nil {
		t.Fatalf("pack exactInputSingle: %v", err)
	}
	if len(data) < ExactInputSingleAmountInOffset+32 {
		t.Fatalf("calldata too short: %d", len(data))
	}
	got := new(big.Int).SetBytes(data[ExactInputSingleAmountInOffset : ExactInputSingleAmountInOffset+32])
	if got.Cmp(amount) != 0 {
		t.Fatalf("expected amountIn %s at offset %d, got %s", amount, ExactInputSingleAmountInOffset, got)
	}
}

func TestPackPancakeV3ExactInputSingleAmountInOffset(t *testing.T) {
	amount := big.NewInt(999)
	data, err := PackPancakeV3ExactInputSingle(ExactInputSingleParams{
		TokenIn:          common.HexToAddress("0x1"),
		TokenOut:         common.HexToAddress("0x2"),
		Fee:              big.NewInt(500),
		Recipient:        common.HexToAddress("0x3"),
		AmountIn:         amount,
		AmountOutMinimum: big.NewInt(0),
	})
	if err != nil {
		t.Fatalf("pack pancake exactInputSingle: %v", err)
	}
	if got, want := common.Bytes2Hex(data[:4]), "414bf389"; got != want {
		t.Fatalf("expected pancake selector %s, got %s", want, got)
	}
	if len(data) < PancakeV3ExactInputSingleAmountInOffset+32 {
		t.Fatalf("calldata too short: %d", len(data))
	}
	got := new(big.Int).SetBytes(data[PancakeV3ExactInputSingleAmountInOffset : PancakeV3ExactInputSingleAmountInOffset+32])
	if got.Cmp(amount) != 0 {
		t.Fatalf("expected amountIn %s at offset %d, got %s", amount, PancakeV3ExactInputSingleAmountInOffset, got)
	}
}

func TestPackBalancerVaultSwapAmountOffset(t *testing.T) {
	amount := big.NewInt(4242)
	data, err := PackBalancerVaultSwap(BalancerVaultSwapParams{
		PoolID:    common.HexToHash("0x1"),
		AssetIn:   common.HexToAddress("0x2"),
		AssetOut:  common.HexToAddress("0x3"),
		Amount:    amount,
		Sender:    common.HexToAddress("0x4"),
		Recipient: common.HexToAddress("0x4"),
	})
	if err != nil {
		t.Fatalf("pack balancer swap: %v", err)
	}
	if len(data) < BalancerSwapAmountOffset+32 {
		t.Fatalf("calldata too short: %d", len(data))
	}
	got := new(big.Int).SetBytes(data[BalancerSwapAmountOffset : BalancerSwapAmountOffset+32])
	if got.Cmp(amount) != 0 {
		t.Fatalf("expected amount %s at offset, got %s", amount, got)
	}
}

func TestPackBalancerV3RouterSwapExactAmountInOffset(t *testing.T) {
	amount := big.NewInt(7777)
	data, err := PackBalancerV3RouterSwapExactIn(BalancerV3RouterSwapParams{
		Pool:          common.HexToAddress("0x11"),
		TokenIn:       common.HexToAddress("0x22"),
		TokenOut:      common.HexToAddress("0x33"),
		ExactAmountIn: amount,
		MinAmountOut:  big.NewInt(0),
	})
	if err != nil {
		t.Fatalf("pack balancer v3 swap: %v", err)
	}
	if len(data) < BalancerV3SwapExactAmountInOffset+32 {
		t.Fatalf("calldata too short: %d", len(data))
	}
	got := new(big.Int).SetBytes(data[BalancerV3SwapExactAmountInOffset : BalancerV3SwapExactAmountInOffset+32])
	if got.Cmp(amount) != 0 {
		t.Fatalf("expected exactAmountIn %s at offset, got %s", amount, got)
	}
}

func TestPackERC20TransferAmountOffset(t *testing.T) {
	amount := big.NewInt(77)
	data, err := PackERC20Transfer(common.HexToAddress("0xabc"), amount)
	if err != nil {
		t.Fatalf("pack transfer: %v", err)
	}
	got := new(big.Int).SetBytes(data[ERC20TransferAmountOffset : ERC20TransferAmountOffset+32])
	if got.Cmp(amount) != 0 {
		t.Fatalf("expected amount %s at offset, got %s", amount, got)
	}
}

func TestPackUniversalRouterV4ExactInSingle(t *testing.T) {
	amountIn := big.NewInt(123456)
	data, err := PackUniversalRouterV4ExactInSingle(
		V4PoolKeyABI{
			Currency0:   common.HexToAddress("0x1"),
			Currency1:   common.HexToAddress("0x2"),
			Fee:         big.NewInt(3000),
			TickSpacing: big.NewInt(60),
		},
		true,
		amountIn,
		big.NewInt(0),
		big.NewInt(123),
	)
	if err != nil {
		t.Fatalf("pack universal router v4: %v", err)
	}
	if len(data) < 4 {
		t.Fatalf("expected non-empty calldata")
	}
	got := unpackUniversalRouterV4ExactInAmount(t, data)
	if got.Cmp(amountIn) != 0 {
		t.Fatalf("expected v4 amountIn %s, got %s", amountIn, got)
	}
}

func TestPackUniversalRouterV4ExactInSingleUsesOpenDeltaWhenAmountNil(t *testing.T) {
	data, err := PackUniversalRouterV4ExactInSingle(
		V4PoolKeyABI{
			Currency0:   common.HexToAddress("0x1"),
			Currency1:   common.HexToAddress("0x2"),
			Fee:         big.NewInt(3000),
			TickSpacing: big.NewInt(60),
		},
		true,
		nil,
		big.NewInt(0),
		big.NewInt(123),
	)
	if err != nil {
		t.Fatalf("pack universal router v4: %v", err)
	}
	got := unpackUniversalRouterV4ExactInAmount(t, data)
	if got.Cmp(V4ActionOpenDelta) != 0 {
		t.Fatalf("expected v4 amountIn OPEN_DELTA, got %s", got)
	}
}

func TestPackPoolManagerSwapRequiresPositiveAmount(t *testing.T) {
	_, err := PackPoolManagerSwap(
		V4PoolKeyABI{
			Currency0:   common.HexToAddress("0x1"),
			Currency1:   common.HexToAddress("0x2"),
			Fee:         big.NewInt(3000),
			TickSpacing: big.NewInt(60),
		},
		true,
		big.NewInt(0),
		nil,
	)
	if err == nil {
		t.Fatal("expected error for zero amount")
	}
}

func unpackUniversalRouterV4ExactInAmount(t *testing.T, data []byte) *big.Int {
	t.Helper()
	values, err := universalRouterABI.Methods["execute"].Inputs.Unpack(data[4:])
	if err != nil {
		t.Fatalf("unpack universal router execute: %v", err)
	}
	inputs, ok := values[1].([][]byte)
	if !ok || len(inputs) != 1 {
		t.Fatalf("unexpected universal router inputs: %#v", values[1])
	}
	actionValues, err := v4ActionsParamsArgs.Unpack(inputs[0])
	if err != nil {
		t.Fatalf("unpack v4 actions input: %v", err)
	}
	params, ok := actionValues[1].([][]byte)
	if !ok || len(params) != 3 {
		t.Fatalf("unexpected v4 action params: %#v", actionValues[1])
	}
	swapValues, err := exactInSingleArgs.Unpack(params[1])
	if err != nil {
		t.Fatalf("unpack v4 exact-in params: %v", err)
	}
	if len(swapValues) != 1 {
		t.Fatalf("unexpected exact-in values count %d", len(swapValues))
	}
	swap, ok := swapValues[0].(struct {
		PoolKey struct {
			Currency0   common.Address `json:"currency0"`
			Currency1   common.Address `json:"currency1"`
			Fee         *big.Int       `json:"fee"`
			TickSpacing *big.Int       `json:"tickSpacing"`
			Hooks       common.Address `json:"hooks"`
		} `json:"poolKey"`
		ZeroForOne       bool     `json:"zeroForOne"`
		AmountIn         *big.Int `json:"amountIn"`
		AmountOutMinimum *big.Int `json:"amountOutMinimum"`
		HookData         []uint8  `json:"hookData"`
	})
	if !ok {
		t.Fatalf("unexpected exact-in type %T", swapValues[0])
	}
	return swap.AmountIn
}
