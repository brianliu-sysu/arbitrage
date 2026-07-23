package market

import (
	"fmt"
	"math/bits"
)

type BitmapWord [4]uint64

// TickBitmap tracks which compressed ticks are initialized.
type TickBitmap struct {
	words map[int16]BitmapWord
}

func NewTickBitmap() TickBitmap {
	return TickBitmap{words: make(map[int16]BitmapWord)}
}

func (tb *TickBitmap) Clone() TickBitmap {
	cloned := NewTickBitmap()
	for wordPos, word := range tb.words {
		cloned.words[wordPos] = word
	}
	return cloned
}

func compressTick(tick, tickSpacing int32) (int32, error) {
	if tickSpacing <= 0 {
		return 0, fmt.Errorf("invalid tick spacing %d", tickSpacing)
	}
	if err := validateTick(tick); err != nil {
		return 0, err
	}
	if tick%tickSpacing != 0 {
		return 0, fmt.Errorf("tick %d is not aligned to spacing %d", tick, tickSpacing)
	}
	return tick / tickSpacing, nil
}

func bitmapPosition(compressed int32) (int16, uint) {
	wordPos := int16(compressed >> 8)
	bitPos := uint((compressed%256 + 256) % 256)
	return wordPos, bitPos
}

func (tb *TickBitmap) FlipTick(tick, tickSpacing int32) error {
	compressed, err := compressTick(tick, tickSpacing)
	if err != nil {
		return err
	}

	wordPos, bitPos := bitmapPosition(compressed)
	word := tb.words[wordPos]
	word[bitPos/64] ^= uint64(1) << (bitPos % 64)
	if word == (BitmapWord{}) {
		delete(tb.words, wordPos)
	} else {
		tb.words[wordPos] = word
	}
	return nil
}

func (tb *TickBitmap) IsInitialized(tick, tickSpacing int32) (bool, error) {
	compressed, err := compressTick(tick, tickSpacing)
	if err != nil {
		return false, err
	}

	wordPos, bitPos := bitmapPosition(compressed)
	word, ok := tb.words[wordPos]
	if !ok {
		return false, nil
	}
	return word[bitPos/64]&(uint64(1)<<(bitPos%64)) != 0, nil
}

// compressTickSearch rounds tick toward negative infinity before dividing by spacing,
// matching Uniswap V3 search semantics for ticks that may not be spacing-aligned.
func compressTickSearch(tick, tickSpacing int32) (int32, error) {
	if tickSpacing <= 0 {
		return 0, fmt.Errorf("invalid tick spacing %d", tickSpacing)
	}
	if err := validateTick(tick); err != nil {
		return 0, err
	}

	compressed := tick / tickSpacing
	if tick < 0 && tick%tickSpacing != 0 {
		compressed--
	}
	return compressed, nil
}

func mostSignificantBitAtOrBelow(word BitmapWord, bitPos uint) (uint, bool) {
	segment := int(bitPos / 64)
	offset := bitPos % 64
	candidate := word[segment]
	if offset < 63 {
		candidate &= (uint64(1) << (offset + 1)) - 1
	}
	if candidate != 0 {
		return uint(segment*64 + 63 - bits.LeadingZeros64(candidate)), true
	}
	for segment--; segment >= 0; segment-- {
		if word[segment] != 0 {
			return uint(segment*64 + 63 - bits.LeadingZeros64(word[segment])), true
		}
	}
	return 0, false
}

func leastSignificantBitAtOrAbove(word BitmapWord, bitPos uint) (uint, bool) {
	segment := int(bitPos / 64)
	offset := bitPos % 64
	candidate := word[segment] & (^uint64(0) << offset)
	if candidate != 0 {
		return uint(segment*64 + bits.TrailingZeros64(candidate)), true
	}
	for segment++; segment < len(word); segment++ {
		if word[segment] != 0 {
			return uint(segment*64 + bits.TrailingZeros64(word[segment])), true
		}
	}
	return 0, false
}

// NextInitializedTickWithinOneWord finds the next initialized tick within one 256-bit word.
// lte=true searches <= the compressed tick; lte=false searches > the compressed tick.
func (tb *TickBitmap) NextInitializedTickWithinOneWord(tick, tickSpacing int32, lte bool) (nextTick int32, initialized bool, err error) {
	compressed, err := compressTickSearch(tick, tickSpacing)
	if err != nil {
		return 0, false, err
	}

	if lte {
		wordPos, bitPos := bitmapPosition(compressed)
		if msb, found := mostSignificantBitAtOrBelow(tb.words[wordPos], bitPos); found {
			nextCompressed := compressed - int32(bitPos-msb)
			return nextCompressed * tickSpacing, true, nil
		}
		nextCompressed := compressed - int32(bitPos)
		return nextCompressed * tickSpacing, false, nil
	}

	nextCompressed := compressed + 1
	wordPos, bitPos := bitmapPosition(nextCompressed)
	if lsb, found := leastSignificantBitAtOrAbove(tb.words[wordPos], bitPos); found {
		nextCompressed += int32(lsb - bitPos)
		return nextCompressed * tickSpacing, true, nil
	}
	nextCompressed += int32(255 - bitPos)
	return nextCompressed * tickSpacing, false, nil
}

// NextInitializedTick is kept for compatibility.
func (tb *TickBitmap) NextInitializedTick(tick, tickSpacing int32, lte bool) (nextTick int32, initialized bool, err error) {
	return tb.NextInitializedTickWithinOneWord(tick, tickSpacing, lte)
}
