package runtime

import "sync"

func startSafeGoroutine(wg *sync.WaitGroup, onPanic func(any), run func()) {
	if wg != nil {
		wg.Add(1)
	}
	go func() {
		if wg != nil {
			defer wg.Done()
		}
		defer func() {
			if recovered := recover(); recovered != nil && onPanic != nil {
				onPanic(recovered)
			}
		}()
		run()
	}()
}
