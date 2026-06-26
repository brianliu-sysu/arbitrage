package pool

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// ---------------------------------------------------------------------------
// Uniswap V3 Pool 事件签名哈希（Keccak256）
// ---------------------------------------------------------------------------

var (
	SwapEventSignature = crypto.Keccak256Hash([]byte("Swap(address,address,int256,int256,uint160,uint128,int24)"))
	MintEventSignature = crypto.Keccak256Hash([]byte("Mint(address,address,int24,int24,uint128,uint256,uint256)"))
	BurnEventSignature = crypto.Keccak256Hash([]byte("Burn(address,int24,int24,uint128,uint256,uint256)"))
)

// ---------------------------------------------------------------------------
// 事件结构体
// ---------------------------------------------------------------------------

type SwapEvent struct {
	Sender       common.Address
	Recipient    common.Address
	Amount0      *big.Int
	Amount1      *big.Int
	SqrtPriceX96 *big.Int
	Liquidity    *big.Int
	Tick         int32
	Raw          types.Log
}

type MintEvent struct {
	Sender    common.Address
	Owner     common.Address
	TickLower int32
	TickUpper int32
	Amount    *big.Int
	Amount0   *big.Int
	Amount1   *big.Int
	Raw       types.Log
}

type BurnEvent struct {
	Owner     common.Address
	TickLower int32
	TickUpper int32
	Amount    *big.Int
	Amount0   *big.Int
	Amount1   *big.Int
	Raw       types.Log
}

// ---------------------------------------------------------------------------
// 事件解析
// ---------------------------------------------------------------------------

// mustNewType 创建 ABI 类型，失败则 panic（类型字符串在编译时已知）。
func MustNewType(kind, internalType string, components []abi.ArgumentMarshaling) abi.Type {
	t, err := abi.NewType(kind, internalType, components)
	if err != nil {
		panic(fmt.Sprintf("abi.NewType(%q): %v", kind, err))
	}
	return t
}

// topicToInt24 将 indexed int24 topic 转为 int32。
// indexed int24 值以 big-endian 存储在 topic 的低 3 字节中。
// sign-extend: 如果最高位 (bit 23) 为 1，则为负数。
func topicToInt24(h common.Hash) int32 {
	b := h[:]
	// big-endian: byte 29 = bits 23..16, byte 30 = bits 15..8, byte 31 = bits 7..0
	v := uint32(b[29])<<16 | uint32(b[30])<<8 | uint32(b[31])
	if v >= 0x800000 { // bit 23 set → negative
		return -int32(0x1000000 - v)
	}
	return int32(v)
}

// ParseSwapEvent 从 log 中解析 Swap 事件。
//
// indexed: sender (topic1), recipient (topic2)
// data: amount0, amount1, sqrtPriceX96, liquidity, tick
func ParseSwapEvent(log types.Log) (*SwapEvent, error) {
	if len(log.Topics) < 3 {
		return nil, fmt.Errorf("insufficient topics for Swap event: got %d, want >=3", len(log.Topics))
	}

	int256 := MustNewType("int256", "", nil)
	uint160 := MustNewType("uint160", "", nil)
	uint128 := MustNewType("uint128", "", nil)
	int24 := MustNewType("int24", "", nil)

	args := abi.Arguments{
		{Type: int256},
		{Type: int256},
		{Type: uint160},
		{Type: uint128},
		{Type: int24},
	}

	unpacked, err := args.Unpack(log.Data)
	if err != nil {
		return nil, fmt.Errorf("unpack swap data: %w", err)
	}

	return &SwapEvent{
		Sender:       common.BytesToAddress(log.Topics[1].Bytes()),
		Recipient:    common.BytesToAddress(log.Topics[2].Bytes()),
		Amount0:      unpacked[0].(*big.Int),
		Amount1:      unpacked[1].(*big.Int),
		SqrtPriceX96: unpacked[2].(*big.Int),
		Liquidity:    unpacked[3].(*big.Int),
		Tick:         int32(unpacked[4].(*big.Int).Int64()),
		Raw:          log,
	}, nil
}

// ParseMintEvent 从 log 中解析 Mint 事件。
//
// indexed: owner (topic1), tickLower (topic2), tickUpper (topic3)
// data: sender, amount, amount0, amount1
func ParseMintEvent(log types.Log) (*MintEvent, error) {
	if len(log.Topics) < 4 {
		return nil, fmt.Errorf("insufficient topics for Mint event: got %d, want >=4", len(log.Topics))
	}

	addressType := MustNewType("address", "", nil)
	uint128 := MustNewType("uint128", "", nil)
	uint256 := MustNewType("uint256", "", nil)

	args := abi.Arguments{
		{Type: addressType},
		{Type: uint128},
		{Type: uint256},
		{Type: uint256},
	}

	unpacked, err := args.Unpack(log.Data)
	if err != nil {
		return nil, fmt.Errorf("unpack mint data: %w", err)
	}

	return &MintEvent{
		Sender:    unpacked[0].(common.Address),
		Owner:     common.BytesToAddress(log.Topics[1].Bytes()),
		TickLower: topicToInt24(log.Topics[2]),
		TickUpper: topicToInt24(log.Topics[3]),
		Amount:    unpacked[1].(*big.Int),
		Amount0:   unpacked[2].(*big.Int),
		Amount1:   unpacked[3].(*big.Int),
		Raw:       log,
	}, nil
}

// ParseBurnEvent 从 log 中解析 Burn 事件。
//
// indexed: owner (topic1), tickLower (topic2), tickUpper (topic3)
// data: amount, amount0, amount1
func ParseBurnEvent(log types.Log) (*BurnEvent, error) {
	if len(log.Topics) < 4 {
		return nil, fmt.Errorf("insufficient topics for Burn event: got %d, want >=4", len(log.Topics))
	}

	uint128 := MustNewType("uint128", "", nil)
	uint256 := MustNewType("uint256", "", nil)

	args := abi.Arguments{
		{Type: uint128},
		{Type: uint256},
		{Type: uint256},
	}

	unpacked, err := args.Unpack(log.Data)
	if err != nil {
		return nil, fmt.Errorf("unpack burn data: %w", err)
	}

	return &BurnEvent{
		Owner:     common.BytesToAddress(log.Topics[1].Bytes()),
		TickLower: topicToInt24(log.Topics[2]),
		TickUpper: topicToInt24(log.Topics[3]),
		Amount:    unpacked[0].(*big.Int),
		Amount0:   unpacked[1].(*big.Int),
		Amount1:   unpacked[2].(*big.Int),
		Raw:       log,
	}, nil
}

