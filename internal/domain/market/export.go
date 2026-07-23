package market

import "math/big"

// PersistedTick is a portable representation of tick liquidity for storage layers.
type PersistedTick struct {
	Index          int32
	LiquidityGross *big.Int
	LiquidityNet   *big.Int
}

// PersistedBitmapWord is a portable representation of a tick bitmap word.
type PersistedBitmapWord struct {
	WordPos int16
	Word    *big.Int
}

func (tt *TickTable) ExportTicks() []PersistedTick {
	records := make([]PersistedTick, 0, len(tt.ticks))
	for index, tick := range tt.ticks {
		if !tick.IsInitialized() {
			continue
		}
		records = append(records, PersistedTick{
			Index:          index,
			LiquidityGross: cloneInt(tick.LiquidityGross),
			LiquidityNet:   cloneInt(tick.LiquidityNet),
		})
	}
	return records
}

func ImportTickTable(records []PersistedTick) TickTable {
	table := NewTickTable()
	for _, record := range records {
		tick := table.GetOrCreate(record.Index)
		tick.LiquidityGross = cloneInt(record.LiquidityGross)
		tick.LiquidityNet = cloneInt(record.LiquidityNet)
	}
	return table
}

func (tb *TickBitmap) ExportBitmap() []PersistedBitmapWord {
	records := make([]PersistedBitmapWord, 0, len(tb.words))
	for wordPos, word := range tb.words {
		if word == (BitmapWord{}) {
			continue
		}
		records = append(records, PersistedBitmapWord{
			WordPos: wordPos,
			Word:    bitmapWordToBigInt(word),
		})
	}
	return records
}

func ImportTickBitmap(records []PersistedBitmapWord) TickBitmap {
	bitmap := NewTickBitmap()
	for _, record := range records {
		word := bitmapWordFromBigInt(record.Word)
		if word != (BitmapWord{}) {
			bitmap.words[record.WordPos] = word
		}
	}
	return bitmap
}

func bitmapWordToBigInt(word BitmapWord) *big.Int {
	value := new(big.Int)
	for segment := len(word) - 1; segment >= 0; segment-- {
		value.Lsh(value, 64)
		value.Or(value, new(big.Int).SetUint64(word[segment]))
	}
	return value
}

func bitmapWordFromBigInt(value *big.Int) BitmapWord {
	var word BitmapWord
	if value == nil || value.Sign() <= 0 {
		return word
	}
	remainder := new(big.Int).Set(value)
	for segment := range word {
		word[segment] = remainder.Uint64()
		remainder.Rsh(remainder, 64)
	}
	return word
}
