package pool

import (
	"math/big"

	"github.com/brianliu-sysu/arbitrage/internal/utils"
	"github.com/ethereum/go-ethereum/common"
)

// Model 描述 Uniswap V3 池子的领域模型（持久化与内存共享）。
type Model struct {
	Address      common.Address
	Token0       common.Address
	Token1       common.Address
	Fee          uint32
	TickSpacing  int32
	Token0Symbol string
	Token1Symbol string

	SqrtPriceX96 *big.Int
	Tick         int32
	Liquidity    *big.Int
	Ticks        map[int32]*TickLiquidity
	Bitmap       map[uint16]utils.Word256
	BlockNumber  uint64
}

// SnapshotFromState 从运行时 State 构造领域模型快照。
func SnapshotFromState(s *State) *Model {
	if s == nil {
		return nil
	}
	copy := s.GetStateCopy()
	return &Model{
		Address:      copy.Address,
		Token0:       copy.Token0,
		Token1:       copy.Token1,
		Fee:          copy.Fee,
		TickSpacing:  copy.TickSpacing,
		Token0Symbol: copy.Token0Symbol,
		Token1Symbol: copy.Token1Symbol,
		SqrtPriceX96: copy.SqrtPriceX96,
		Tick:         copy.Tick,
		Liquidity:    copy.Liquidity,
		Ticks:        copy.Ticks,
		Bitmap:       copy.Bitmap,
		BlockNumber:  copy.BlockNumber,
	}
}
