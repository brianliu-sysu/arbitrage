package utils

import (
	"sync"
	"testing"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/logx"
)

func TestSafeGo_NormalExecution(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	done := make(chan struct{})

	SafeGo(logx.Nop(), func() {
		defer wg.Done()
		close(done)
	})

	wg.Wait()
	select {
	case <-done:
		// expected
	default:
		t.Error("SafeGo did not execute the function")
	}
}

func TestSafeGo_PanicRecovery(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	recovered := make(chan struct{})

	// 这个 goroutine 等待 SafeGo 完成（含 recover）后通知主测试
	SafeGo(logx.Nop(), func() {
		defer wg.Done()
		panic("test panic in SafeGo")
	})

	SafeGo(logx.Nop(), func() {
		wg.Wait()
		close(recovered)
	})

	select {
	case <-recovered:
		// panic was recovered, SafeGo did not crash the test
	case <-time.After(2 * time.Second):
		t.Fatal("SafeGo with panic did not recover within timeout")
	}
}
