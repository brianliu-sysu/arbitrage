package market

import (
	"fmt"
	"math/big"
)

// TickBitmap tracks which compressed ticks are initialized.
type TickBitmap struct {
	words map[int16]*big.Int
}

func NewTickBitmap() TickBitmap {
	return TickBitmap{words: make(map[int16]*big.Int)}
}

func (tb TickBitmap) Clone() TickBitmap {
	cloned := NewTickBitmap()
	for wordPos, word := range tb.words {
		cloned.words[wordPos] = cloneInt(word)
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
	mask := new(big.Int).Lsh(big.NewInt(1), bitPos)
	word, ok := tb.words[wordPos]
	if !ok {
		word = big.NewInt(0)
	}
	tb.words[wordPos] = new(big.Int).Xor(word, mask)
	return nil
}

func (tb TickBitmap) IsInitialized(tick, tickSpacing int32) (bool, error) {
	compressed, err := compressTick(tick, tickSpacing)
	if err != nil {
		return false, err
	}

	wordPos, bitPos := bitmapPosition(compressed)
	word, ok := tb.words[wordPos]
	if !ok {
		return false, nil
	}
	return word.Bit(int(bitPos)) == 1, nil
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

func (tb TickBitmap) wordAt(wordPos int16) *big.Int {
	word, ok := tb.words[wordPos]
	if !ok {
		return big.NewInt(0)
	}
	return word
}

func maskAtOrBelow(bitPos uint) *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bitPos+1), big.NewInt(1))
}

func maskAtOrAbove(bitPos uint) *big.Int {
	fullWord := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	if bitPos == 0 {
		return fullWord
	}
	lowMask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bitPos), big.NewInt(1))
	return new(big.Int).And(new(big.Int).Not(lowMask), fullWord)
}

func mostSignificantBit(v *big.Int) uint {
	if v.Sign() == 0 {
		return 0
	}
	return uint(v.BitLen() - 1)
}

func leastSignificantBit(v *big.Int) uint {
	if v.Sign() == 0 {
		return 0
	}
	for bit := 0; bit < 256; bit++ {
		if v.Bit(bit) == 1 {
			return uint(bit)
		}
	}
	return 0
}

// NextInitializedTick finds the next initialized tick within a single 256-bit bitmap word.
// When lte is true it searches downward (<= tick); otherwise upward (>= tick).
// This mirrors Uniswap V3 TickBitmap.nextInitializedTickWithinOneWord.
func (tb TickBitmap) NextInitializedTick(tick, tickSpacing int32, lte bool) (nextTick int32, initialized bool, err error) {
	compressed, err := compressTickSearch(tick, tickSpacing)
	if err != nil {
		return 0, false, err
	}

	if lte {
		wordPos, bitPos := bitmapPosition(compressed)
		masked := new(big.Int).And(tb.wordAt(wordPos), maskAtOrBelow(bitPos))
		if masked.Sign() != 0 {
			msb := mostSignificantBit(masked)
			nextCompressed := compressed - int32(bitPos-msb)
			return nextCompressed * tickSpacing, true, nil
		}
		nextCompressed := compressed - int32(bitPos)
		return nextCompressed * tickSpacing, false, nil
	}

	nextCompressed := compressed + 1
	wordPos, bitPos := bitmapPosition(nextCompressed)
	masked := new(big.Int).And(tb.wordAt(wordPos), maskAtOrAbove(bitPos))
	if masked.Sign() != 0 {
		lsb := leastSignificantBit(masked)
		nextCompressed = nextCompressed + int32(lsb-bitPos)
		return nextCompressed * tickSpacing, true, nil
	}
	nextCompressed = nextCompressed + int32(255-bitPos)
	return nextCompressed * tickSpacing, false, nil
}
