package runtime

import (
	"sync"
	"testing"
)

func TestStartSafeGoroutineRecoversPanicAndCompletesWaitGroup(t *testing.T) {
	var wg sync.WaitGroup
	recovered := make(chan any, 1)

	startSafeGoroutine(&wg, func(value any) {
		recovered <- value
	}, func() {
		panic("boom")
	})

	wg.Wait()
	select {
	case value := <-recovered:
		if value != "boom" {
			t.Fatalf("unexpected recovered value: %v", value)
		}
	default:
		t.Fatal("expected panic to be recovered")
	}
}
