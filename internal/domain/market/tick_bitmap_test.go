package market

import (
	"testing"
)

func setupBitmapWithTicks(t *testing.T, tickSpacing int32, ticks ...int32) TickBitmap {
	t.Helper()
	bitmap := NewTickBitmap()
	for _, tick := range ticks {
		if err := bitmap.FlipTick(tick, tickSpacing); err != nil {
			t.Fatalf("flip tick %d: %v", tick, err)
		}
	}
	return bitmap
}

func TestNextInitializedTickLTEAtCurrent(t *testing.T) {
	const spacing int32 = 60
	bitmap := setupBitmapWithTicks(t, spacing, -120, 0, 120)

	next, initialized, err := bitmap.NextInitializedTick(0, spacing, true)
	if err != nil {
		t.Fatalf("next initialized tick: %v", err)
	}
	if !initialized || next != 0 {
		t.Fatalf("expected initialized tick 0, got tick=%d initialized=%v", next, initialized)
	}
}

func TestNextInitializedTickLTEFindsLower(t *testing.T) {
	const spacing int32 = 60
	bitmap := setupBitmapWithTicks(t, spacing, -120, 0, 120)

	next, initialized, err := bitmap.NextInitializedTick(-1, spacing, true)
	if err != nil {
		t.Fatalf("next initialized tick: %v", err)
	}
	if !initialized || next != -120 {
		t.Fatalf("expected initialized tick -120, got tick=%d initialized=%v", next, initialized)
	}
}

func TestNextInitializedTickGTEAtCurrent(t *testing.T) {
	const spacing int32 = 60
	bitmap := setupBitmapWithTicks(t, spacing, -120, 0, 120)

	// lte=false searches from compressed+1, so tick 0 is found when searching from -1.
	next, initialized, err := bitmap.NextInitializedTick(-1, spacing, false)
	if err != nil {
		t.Fatalf("next initialized tick: %v", err)
	}
	if !initialized || next != 0 {
		t.Fatalf("expected initialized tick 0, got tick=%d initialized=%v", next, initialized)
	}
}

func TestNextInitializedTickGTEFindsUpper(t *testing.T) {
	const spacing int32 = 60
	bitmap := setupBitmapWithTicks(t, spacing, -120, 0, 120)

	next, initialized, err := bitmap.NextInitializedTick(1, spacing, false)
	if err != nil {
		t.Fatalf("next initialized tick: %v", err)
	}
	if !initialized || next != 120 {
		t.Fatalf("expected initialized tick 120, got tick=%d initialized=%v", next, initialized)
	}
}

func TestNextInitializedTickNoInitializedInWord(t *testing.T) {
	const spacing int32 = 60
	bitmap := setupBitmapWithTicks(t, spacing, -120)

	next, initialized, err := bitmap.NextInitializedTick(0, spacing, true)
	if err != nil {
		t.Fatalf("next initialized tick: %v", err)
	}
	if initialized {
		t.Fatalf("expected no initialized tick in word, got %d", next)
	}
	if next != 0 {
		t.Fatalf("expected boundary tick 0, got %d", next)
	}
}

func TestNextInitializedTickUnalignedSearchTick(t *testing.T) {
	const spacing int32 = 60
	bitmap := setupBitmapWithTicks(t, spacing, -120, 0)

	next, initialized, err := bitmap.NextInitializedTick(-30, spacing, true)
	if err != nil {
		t.Fatalf("next initialized tick: %v", err)
	}
	if !initialized || next != -120 {
		t.Fatalf("expected initialized tick -120 for unaligned search, got tick=%d initialized=%v", next, initialized)
	}
}
