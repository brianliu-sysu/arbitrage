package snapshot

import (
	"context"
	"fmt"
	"math/big"

	"github.com/brianliu-sysu/arbitrage/internal/pool"
	"github.com/brianliu-sysu/arbitrage/internal/blockchain"
	"github.com/ethereum/go-ethereum/common"
)

// BitmapReader 读取链上 tick bitmap 与 tick 数据。
type BitmapReader interface {
	FetchTickSpacing() (int32, error)
	FetchTickBitmapBatch(wordPositions []int16) (map[int16]*big.Int, error)
	FetchTickInfoBatch(ticks []int32) (map[int32]*blockchain.TickData, error)
}

// Scanner 首次从链上扫描 tick bitmap 并填充池子 tick 地图。
type Scanner struct {
	reader BitmapReader
}

// NewScanner 创建链上扫描器。
func NewScanner(reader BitmapReader) *Scanner {
	return &Scanner{reader: reader}
}

// ScanTicks 扫描活跃 tick 并写入 pool.State（全量重建 tick 地图）。
func (s *Scanner) ScanTicks(ctx context.Context, p *pool.State) error {
	if s.reader == nil || p == nil {
		return fmt.Errorf("scanner or pool is nil")
	}
	_ = ctx

	tickSpacing, err := s.reader.FetchTickSpacing()
	if err != nil {
		return fmt.Errorf("fetch tick spacing: %w", err)
	}
	p.TickSpacing = tickSpacing

	const wordRange int32 = 512
	words := make([]int16, 0, wordRange*2)
	for w := int16(-512); w < 512; w++ {
		words = append(words, w)
	}

	bitmaps, err := s.reader.FetchTickBitmapBatch(words)
	if err != nil {
		return fmt.Errorf("fetch tick bitmap batch: %w", err)
	}

	ticks := collectInitializedTicks(words, tickSpacing, bitmaps)
	if len(ticks) == 0 {
		return nil
	}

	tickData, err := s.reader.FetchTickInfoBatch(ticks)
	if err != nil {
		return fmt.Errorf("fetch tick info batch: %w", err)
	}

	newTicks := make(map[int32]*pool.TickLiquidity)
	for tick, td := range tickData {
		if td == nil || td.LiquidityNet == nil || td.LiquidityNet.Sign() == 0 {
			continue
		}
		gross := td.LiquidityGross
		if gross == nil || gross.Sign() < 0 {
			continue
		}
		newTicks[tick] = &pool.TickLiquidity{
			LiquidityNet:   new(big.Int).Set(td.LiquidityNet),
			LiquidityGross: new(big.Int).Set(gross),
		}
	}
	p.ReplaceTicks(newTicks)
	return nil
}

func collectInitializedTicks(words []int16, tickSpacing int32, bitmaps map[int16]*big.Int) []int32 {
	var ticks []int32
	for _, wordPos := range words {
		word := bitmaps[wordPos]
		if word == nil || word.Sign() == 0 {
			continue
		}
		for bit := 0; bit < 256; bit++ {
			if word.Bit(bit) == 0 {
				continue
			}
			compressed := int32(wordPos)*256 + int32(bit)
			ticks = append(ticks, compressed*tickSpacing)
		}
	}
	return ticks
}

// EnsureMetadata 通过 RPC 填充 token0/token1/fee（若尚未设置）。
func EnsureMetadata(p *pool.State, token0, token1 common.Address, fee uint32) {
	if p == nil {
		return
	}
	if p.Token0 == (common.Address{}) {
		p.SetTokens(token0, token1, fee)
	}
}
