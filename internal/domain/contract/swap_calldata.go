package contract

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const nativeETHSentinel = "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"

// NativeETHSentinel is the conventional native-ETH address used by routers / ArbitrageExecutor fillToken.
var NativeETHSentinel = common.HexToAddress(nativeETHSentinel)

const wethABIJSON = `[
  {"inputs":[],"name":"deposit","outputs":[],"stateMutability":"payable","type":"function"},
  {"inputs":[{"internalType":"uint256","name":"wad","type":"uint256"}],"name":"withdraw","outputs":[],"stateMutability":"nonpayable","type":"function"}
]`

const swapRouter02ABIJSON = `[
  {
    "inputs":[{
      "components":[
        {"internalType":"address","name":"tokenIn","type":"address"},
        {"internalType":"address","name":"tokenOut","type":"address"},
        {"internalType":"uint24","name":"fee","type":"uint24"},
        {"internalType":"address","name":"recipient","type":"address"},
        {"internalType":"uint256","name":"amountIn","type":"uint256"},
        {"internalType":"uint256","name":"amountOutMinimum","type":"uint256"},
        {"internalType":"uint160","name":"sqrtPriceLimitX96","type":"uint160"}
      ],
      "internalType":"struct IV3SwapRouter.ExactInputSingleParams",
      "name":"params",
      "type":"tuple"
    }],
    "name":"exactInputSingle",
    "outputs":[{"internalType":"uint256","name":"amountOut","type":"uint256"}],
    "stateMutability":"payable",
    "type":"function"
  }
]`

const erc20TransferABIJSON = `[
  {"inputs":[{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"transfer","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"}
]`

const balancerVaultSwapABIJSON = `[
  {
    "inputs":[
      {
        "components":[
          {"internalType":"bytes32","name":"poolId","type":"bytes32"},
          {"internalType":"enum IVault.SwapKind","name":"kind","type":"uint8"},
          {"internalType":"address","name":"assetIn","type":"address"},
          {"internalType":"address","name":"assetOut","type":"address"},
          {"internalType":"uint256","name":"amount","type":"uint256"},
          {"internalType":"bytes","name":"userData","type":"bytes"}
        ],
        "internalType":"struct IVault.SingleSwap",
        "name":"singleSwap",
        "type":"tuple"
      },
      {
        "components":[
          {"internalType":"address","name":"sender","type":"address"},
          {"internalType":"bool","name":"fromInternalBalance","type":"bool"},
          {"internalType":"address payable","name":"recipient","type":"address"},
          {"internalType":"bool","name":"toInternalBalance","type":"bool"}
        ],
        "internalType":"struct IVault.FundManagement",
        "name":"funds",
        "type":"tuple"
      },
      {"internalType":"uint256","name":"limit","type":"uint256"},
      {"internalType":"uint256","name":"deadline","type":"uint256"}
    ],
    "name":"swap",
    "outputs":[{"internalType":"uint256","name":"amountCalculated","type":"uint256"}],
    "stateMutability":"payable",
    "type":"function"
  }
]`

const balancerV3RouterSwapABIJSON = `[
  {
    "inputs":[
      {"internalType":"address","name":"pool","type":"address"},
      {"internalType":"contract IERC20","name":"tokenIn","type":"address"},
      {"internalType":"contract IERC20","name":"tokenOut","type":"address"},
      {"internalType":"uint256","name":"exactAmountIn","type":"uint256"},
      {"internalType":"uint256","name":"minAmountOut","type":"uint256"},
      {"internalType":"uint256","name":"deadline","type":"uint256"},
      {"internalType":"bool","name":"wethIsEth","type":"bool"},
      {"internalType":"bytes","name":"userData","type":"bytes"}
    ],
    "name":"swapSingleTokenExactIn",
    "outputs":[{"internalType":"uint256","name":"amountOut","type":"uint256"}],
    "stateMutability":"payable",
    "type":"function"
  }
]`

const universalRouterExecuteABIJSON = `[
  {
    "inputs":[
      {"internalType":"bytes","name":"commands","type":"bytes"},
      {"internalType":"bytes[]","name":"inputs","type":"bytes[]"},
      {"internalType":"uint256","name":"deadline","type":"uint256"}
    ],
    "name":"execute",
    "outputs":[],
    "stateMutability":"payable",
    "type":"function"
  }
]`

const poolManagerSwapABIJSON = `[
  {
    "inputs":[
      {
        "components":[
          {"internalType":"address","name":"currency0","type":"address"},
          {"internalType":"address","name":"currency1","type":"address"},
          {"internalType":"uint24","name":"fee","type":"uint24"},
          {"internalType":"int24","name":"tickSpacing","type":"int24"},
          {"internalType":"address","name":"hooks","type":"address"}
        ],
        "internalType":"struct PoolKey",
        "name":"key",
        "type":"tuple"
      },
      {
        "components":[
          {"internalType":"bool","name":"zeroForOne","type":"bool"},
          {"internalType":"int256","name":"amountSpecified","type":"int256"},
          {"internalType":"uint160","name":"sqrtPriceLimitX96","type":"uint160"}
        ],
        "internalType":"struct SwapParams",
        "name":"params",
        "type":"tuple"
      },
      {"internalType":"bytes","name":"hookData","type":"bytes"}
    ],
    "name":"swap",
    "outputs":[{"internalType":"int256","name":"swapDelta","type":"int256"}],
    "stateMutability":"nonpayable",
    "type":"function"
  }
]`

const (
	UniversalRouterCommandV4Swap = byte(0x10)
	V4ActionSwapExactInSingle    = byte(0x06)
	V4ActionSettle               = byte(0x0b)
	V4ActionTakeAll              = byte(0x0f)
)

// V4ActionOpenDelta is ActionConstants.OPEN_DELTA. V4 exact-in uses it to spend
// the open input-currency credit created by a preceding SETTLE action.
var V4ActionOpenDelta = big.NewInt(0)

// V4ActionContractBalance is ActionConstants.CONTRACT_BALANCE (1 << 255).
var V4ActionContractBalance = new(big.Int).Lsh(big.NewInt(1), 255)

var (
	V4SqrtPriceLimitZeroForOne, _ = new(big.Int).SetString("4295128740", 10)
	V4SqrtPriceLimitOneForZero, _ = new(big.Int).SetString("1461446703485210103287273052203988822378723970341", 10)
)

var (
	wethABI             abi.ABI
	swapRouter02ABI     abi.ABI
	erc20TransferABI    abi.ABI
	balancerVaultABI    abi.ABI
	balancerV3RouterABI abi.ABI
	universalRouterABI  abi.ABI
	poolManagerSwapABI  abi.ABI
	exactInSingleArgs   abi.Arguments
	settleArgs          abi.Arguments
	takeAllArgs         abi.Arguments
	v4ActionsParamsArgs abi.Arguments
)

func init() {
	var err error
	wethABI, err = abi.JSON(strings.NewReader(wethABIJSON))
	if err != nil {
		panic(fmt.Sprintf("parse weth abi: %v", err))
	}
	swapRouter02ABI, err = abi.JSON(strings.NewReader(swapRouter02ABIJSON))
	if err != nil {
		panic(fmt.Sprintf("parse swap router02 abi: %v", err))
	}
	erc20TransferABI, err = abi.JSON(strings.NewReader(erc20TransferABIJSON))
	if err != nil {
		panic(fmt.Sprintf("parse erc20 transfer abi: %v", err))
	}
	balancerVaultABI, err = abi.JSON(strings.NewReader(balancerVaultSwapABIJSON))
	if err != nil {
		panic(fmt.Sprintf("parse balancer vault abi: %v", err))
	}
	balancerV3RouterABI, err = abi.JSON(strings.NewReader(balancerV3RouterSwapABIJSON))
	if err != nil {
		panic(fmt.Sprintf("parse balancer v3 router abi: %v", err))
	}
	universalRouterABI, err = abi.JSON(strings.NewReader(universalRouterExecuteABIJSON))
	if err != nil {
		panic(fmt.Sprintf("parse universal router abi: %v", err))
	}
	poolManagerSwapABI, err = abi.JSON(strings.NewReader(poolManagerSwapABIJSON))
	if err != nil {
		panic(fmt.Sprintf("parse pool manager swap abi: %v", err))
	}

	addressTy, err := abi.NewType("address", "", nil)
	if err != nil {
		panic(err)
	}
	uint256Ty, err := abi.NewType("uint256", "", nil)
	if err != nil {
		panic(err)
	}
	boolTy, err := abi.NewType("bool", "", nil)
	if err != nil {
		panic(err)
	}
	bytesTy, err := abi.NewType("bytes", "", nil)
	if err != nil {
		panic(err)
	}
	bytesArrTy, err := abi.NewType("bytes[]", "", nil)
	if err != nil {
		panic(err)
	}
	exactInSingleTy, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "poolKey", Type: "tuple", Components: []abi.ArgumentMarshaling{
			{Name: "currency0", Type: "address"},
			{Name: "currency1", Type: "address"},
			{Name: "fee", Type: "uint24"},
			{Name: "tickSpacing", Type: "int24"},
			{Name: "hooks", Type: "address"},
		}},
		{Name: "zeroForOne", Type: "bool"},
		{Name: "amountIn", Type: "uint128"},
		{Name: "amountOutMinimum", Type: "uint128"},
		{Name: "hookData", Type: "bytes"},
	})
	if err != nil {
		panic(err)
	}

	exactInSingleArgs = abi.Arguments{{Type: exactInSingleTy}}
	settleArgs = abi.Arguments{{Type: addressTy}, {Type: uint256Ty}, {Type: boolTy}}
	takeAllArgs = abi.Arguments{{Type: addressTy}, {Type: uint256Ty}}
	v4ActionsParamsArgs = abi.Arguments{{Type: bytesTy}, {Type: bytesArrTy}}
}

func PackWETHDeposit() ([]byte, error) {
	return wethABI.Pack("deposit")
}

func PackWETHWithdraw(amount *big.Int) ([]byte, error) {
	if amount == nil {
		amount = big.NewInt(0)
	}
	return wethABI.Pack("withdraw", amount)
}

const WETHWithdrawAmountOffset = 4

type ExactInputSingleParams struct {
	TokenIn           common.Address
	TokenOut          common.Address
	Fee               *big.Int
	Recipient         common.Address
	AmountIn          *big.Int
	AmountOutMinimum  *big.Int
	SqrtPriceLimitX96 *big.Int
}

func PackExactInputSingle(params ExactInputSingleParams) ([]byte, error) {
	if params.Fee == nil {
		params.Fee = big.NewInt(0)
	}
	if params.AmountIn == nil {
		params.AmountIn = big.NewInt(0)
	}
	if params.AmountOutMinimum == nil {
		params.AmountOutMinimum = big.NewInt(0)
	}
	if params.SqrtPriceLimitX96 == nil {
		params.SqrtPriceLimitX96 = big.NewInt(0)
	}
	return swapRouter02ABI.Pack("exactInputSingle", struct {
		TokenIn           common.Address
		TokenOut          common.Address
		Fee               *big.Int
		Recipient         common.Address
		AmountIn          *big.Int
		AmountOutMinimum  *big.Int
		SqrtPriceLimitX96 *big.Int
	}{
		TokenIn:           params.TokenIn,
		TokenOut:          params.TokenOut,
		Fee:               params.Fee,
		Recipient:         params.Recipient,
		AmountIn:          params.AmountIn,
		AmountOutMinimum:  params.AmountOutMinimum,
		SqrtPriceLimitX96: params.SqrtPriceLimitX96,
	})
}

const ExactInputSingleAmountInOffset = 4 + 32*4

func PackERC20Transfer(to common.Address, amount *big.Int) ([]byte, error) {
	if amount == nil {
		amount = big.NewInt(0)
	}
	return erc20TransferABI.Pack("transfer", to, amount)
}

const ERC20TransferAmountOffset = 4 + 32

const BalancerSwapKindGivenIn uint8 = 0

type BalancerVaultSwapParams struct {
	PoolID    common.Hash
	AssetIn   common.Address
	AssetOut  common.Address
	Amount    *big.Int
	Sender    common.Address
	Recipient common.Address
	Limit     *big.Int
	Deadline  *big.Int
}

func PackBalancerVaultSwap(params BalancerVaultSwapParams) ([]byte, error) {
	if params.Amount == nil {
		params.Amount = big.NewInt(0)
	}
	if params.Limit == nil {
		params.Limit = big.NewInt(0)
	}
	if params.Deadline == nil {
		params.Deadline = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	}
	singleSwap := struct {
		PoolID   [32]byte       `abi:"poolId"`
		Kind     uint8          `abi:"kind"`
		AssetIn  common.Address `abi:"assetIn"`
		AssetOut common.Address `abi:"assetOut"`
		Amount   *big.Int       `abi:"amount"`
		UserData []byte         `abi:"userData"`
	}{
		PoolID:   params.PoolID,
		Kind:     BalancerSwapKindGivenIn,
		AssetIn:  params.AssetIn,
		AssetOut: params.AssetOut,
		Amount:   params.Amount,
		UserData: []byte{},
	}
	funds := struct {
		Sender              common.Address `abi:"sender"`
		FromInternalBalance bool           `abi:"fromInternalBalance"`
		Recipient           common.Address `abi:"recipient"`
		ToInternalBalance   bool           `abi:"toInternalBalance"`
	}{
		Sender:    params.Sender,
		Recipient: params.Recipient,
	}
	return balancerVaultABI.Pack("swap", singleSwap, funds, params.Limit, params.Deadline)
}

// BalancerSwapAmountOffset is the byte offset of SingleSwap.amount for empty userData.
// Layout: selector + (SingleSwap offset, FundManagement x4, limit, deadline) + SingleSwap static prefix.
const BalancerSwapAmountOffset = 4 + 7*32 + 4*32

type BalancerV3RouterSwapParams struct {
	Pool          common.Address
	TokenIn       common.Address
	TokenOut      common.Address
	ExactAmountIn *big.Int
	MinAmountOut  *big.Int
	Deadline      *big.Int
	WethIsEth     bool
	UserData      []byte
}

func PackBalancerV3RouterSwapExactIn(params BalancerV3RouterSwapParams) ([]byte, error) {
	if params.Pool == (common.Address{}) {
		return nil, fmt.Errorf("balancer v3 pool address is required")
	}
	if params.ExactAmountIn == nil {
		params.ExactAmountIn = big.NewInt(0)
	}
	if params.MinAmountOut == nil {
		params.MinAmountOut = big.NewInt(0)
	}
	if params.Deadline == nil {
		params.Deadline = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	}
	if params.UserData == nil {
		params.UserData = []byte{}
	}
	return balancerV3RouterABI.Pack(
		"swapSingleTokenExactIn",
		params.Pool,
		params.TokenIn,
		params.TokenOut,
		params.ExactAmountIn,
		params.MinAmountOut,
		params.Deadline,
		params.WethIsEth,
		params.UserData,
	)
}

// BalancerV3SwapExactAmountInOffset is the byte offset of exactAmountIn for empty userData.
// Layout: selector + pool + tokenIn + tokenOut + exactAmountIn + ...
const BalancerV3SwapExactAmountInOffset = 4 + 3*32

type V4PoolKeyABI struct {
	Currency0   common.Address `abi:"currency0"`
	Currency1   common.Address `abi:"currency1"`
	Fee         *big.Int       `abi:"fee"`
	TickSpacing *big.Int       `abi:"tickSpacing"`
	Hooks       common.Address `abi:"hooks"`
}

func PackUniversalRouterV4ExactInSingle(
	poolKey V4PoolKeyABI,
	zeroForOne bool,
	amountIn *big.Int,
	amountOutMinimum *big.Int,
	deadline *big.Int,
) ([]byte, error) {
	if poolKey.Fee == nil {
		poolKey.Fee = big.NewInt(0)
	}
	if poolKey.TickSpacing == nil {
		poolKey.TickSpacing = big.NewInt(0)
	}
	if amountOutMinimum == nil {
		amountOutMinimum = big.NewInt(0)
	}
	if amountIn == nil {
		amountIn = new(big.Int).Set(V4ActionOpenDelta)
	}
	if amountIn.Sign() < 0 {
		return nil, fmt.Errorf("v4 exact input amountIn must be non-negative")
	}
	if deadline == nil {
		deadline = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	}

	inputCurrency := poolKey.Currency0
	outputCurrency := poolKey.Currency1
	if !zeroForOne {
		inputCurrency = poolKey.Currency1
		outputCurrency = poolKey.Currency0
	}

	swapParams, err := exactInSingleArgs.Pack(struct {
		PoolKey          V4PoolKeyABI `abi:"poolKey"`
		ZeroForOne       bool         `abi:"zeroForOne"`
		AmountIn         *big.Int     `abi:"amountIn"`
		AmountOutMinimum *big.Int     `abi:"amountOutMinimum"`
		HookData         []byte       `abi:"hookData"`
	}{
		PoolKey:          poolKey,
		ZeroForOne:       zeroForOne,
		AmountIn:         amountIn,
		AmountOutMinimum: amountOutMinimum,
		HookData:         []byte{},
	})
	if err != nil {
		return nil, fmt.Errorf("pack v4 exact input single: %w", err)
	}
	settleParams, err := settleArgs.Pack(inputCurrency, V4ActionContractBalance, false)
	if err != nil {
		return nil, fmt.Errorf("pack v4 settle: %w", err)
	}
	takeParams, err := takeAllArgs.Pack(outputCurrency, big.NewInt(0))
	if err != nil {
		return nil, fmt.Errorf("pack v4 take all: %w", err)
	}

	actions := []byte{V4ActionSettle, V4ActionSwapExactInSingle, V4ActionTakeAll}
	params := [][]byte{settleParams, swapParams, takeParams}
	input, err := v4ActionsParamsArgs.Pack(actions, params)
	if err != nil {
		return nil, fmt.Errorf("pack v4 actions input: %w", err)
	}
	return universalRouterABI.Pack("execute", []byte{UniversalRouterCommandV4Swap}, [][]byte{input}, deadline)
}

func PackPoolManagerSwap(poolKey V4PoolKeyABI, zeroForOne bool, amountIn *big.Int, hookData []byte) ([]byte, error) {
	if poolKey.Fee == nil {
		poolKey.Fee = big.NewInt(0)
	}
	if poolKey.TickSpacing == nil {
		poolKey.TickSpacing = big.NewInt(0)
	}
	if amountIn == nil || amountIn.Sign() <= 0 {
		return nil, fmt.Errorf("pool manager swap requires a positive known amountIn")
	}
	if hookData == nil {
		hookData = []byte{}
	}
	limit := V4SqrtPriceLimitOneForZero
	if zeroForOne {
		limit = V4SqrtPriceLimitZeroForOne
	}
	key := struct {
		Currency0   common.Address `abi:"currency0"`
		Currency1   common.Address `abi:"currency1"`
		Fee         *big.Int       `abi:"fee"`
		TickSpacing *big.Int       `abi:"tickSpacing"`
		Hooks       common.Address `abi:"hooks"`
	}{
		Currency0:   poolKey.Currency0,
		Currency1:   poolKey.Currency1,
		Fee:         poolKey.Fee,
		TickSpacing: poolKey.TickSpacing,
		Hooks:       poolKey.Hooks,
	}
	params := struct {
		ZeroForOne        bool     `abi:"zeroForOne"`
		AmountSpecified   *big.Int `abi:"amountSpecified"`
		SqrtPriceLimitX96 *big.Int `abi:"sqrtPriceLimitX96"`
	}{
		ZeroForOne:        zeroForOne,
		AmountSpecified:   new(big.Int).Neg(new(big.Int).Set(amountIn)),
		SqrtPriceLimitX96: limit,
	}
	return poolManagerSwapABI.Pack("swap", key, params, hookData)
}
