package market

import (
	"math/big"
	"testing"
)

func setupBitmapWithTicks(t testing.TB, tickSpacing int32, ticks ...int32) TickBitmap {
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

func TestFlipTickDeletesEmptyWord(t *testing.T) {
	bitmap := setupBitmapWithTicks(t, 1, 42)
	if err := bitmap.FlipTick(42, 1); err != nil {
		t.Fatalf("clear tick: %v", err)
	}
	if len(bitmap.words) != 0 {
		t.Fatalf("expected empty word to be deleted, got %d words", len(bitmap.words))
	}
}

func TestBitmapCloneIsIndependent(t *testing.T) {
	bitmap := setupBitmapWithTicks(t, 1, 1, 65, 129, 193)
	cloned := bitmap.Clone()
	if err := cloned.FlipTick(1, 1); err != nil {
		t.Fatalf("mutate clone: %v", err)
	}
	initialized, err := bitmap.IsInitialized(1, 1)
	if err != nil {
		t.Fatalf("check original: %v", err)
	}
	if !initialized {
		t.Fatal("mutating clone changed original bitmap")
	}
}

func TestBitmapExportImportPreservesAllSegments(t *testing.T) {
	bitmap := setupBitmapWithTicks(t, 1, -256, 0, 63, 64, 127, 128, 191, 192, 255)
	imported := ImportTickBitmap(bitmap.ExportBitmap())
	for _, tick := range []int32{-256, 0, 63, 64, 127, 128, 191, 192, 255} {
		initialized, err := imported.IsInitialized(tick, 1)
		if err != nil {
			t.Fatalf("check imported tick %d: %v", tick, err)
		}
		if !initialized {
			t.Fatalf("expected imported tick %d to be initialized", tick)
		}
	}
}

func TestImportTickBitmapIgnoresEmptyWords(t *testing.T) {
	bitmap := ImportTickBitmap([]PersistedBitmapWord{{WordPos: 1, Word: big.NewInt(0)}})
	if len(bitmap.words) != 0 {
		t.Fatalf("expected empty persisted word to be ignored, got %d words", len(bitmap.words))
	}
}

func TestNextInitializedTickWithinOneWordDoesNotAllocate(t *testing.T) {
	bitmap := setupBitmapWithTicks(t, 1, 1, 65, 129, 193)
	allocations := testing.AllocsPerRun(1000, func() {
		_, _, err := bitmap.NextInitializedTickWithinOneWord(128, 1, false)
		if err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("expected zero allocations per search, got %.2f", allocations)
	}
}

func BenchmarkTickBitmapNextInitializedTickWithinOneWord(b *testing.B) {
	bitmap := setupBitmapWithTicks(b, 1, 1, 65, 129, 193, 255)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := bitmap.NextInitializedTickWithinOneWord(128, 1, i&1 == 0)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTickBitmapFlipTick(b *testing.B) {
	bitmap := setupBitmapWithTicks(b, 1, 129)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := bitmap.FlipTick(129, 1); err != nil {
			b.Fatal(err)
		}
	}
}
