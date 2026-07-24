package arbitrageapp

import (
	"context"
	"sync"
)

type scanController struct {
	mu     sync.Mutex
	base   context.Context
	block  uint64
	cancel context.CancelFunc
	done   chan struct{}
}

type scanTask interface {
	Run(context.Context) error
	Complete(error)
}

func (c *scanController) SetContext(ctx context.Context) {
	c.mu.Lock()
	c.base = ctx
	c.mu.Unlock()
}

func (c *scanController) CancelBefore(ctx context.Context, blockNumber uint64) error {
	c.mu.Lock()
	if c.done == nil || c.block >= blockNumber {
		c.mu.Unlock()
		return nil
	}
	cancel, done := c.cancel, c.done
	c.mu.Unlock()
	return cancelAndWait(ctx, cancel, done)
}

func (c *scanController) CancelCurrent(ctx context.Context) error {
	c.mu.Lock()
	cancel, done := c.cancel, c.done
	c.mu.Unlock()
	if done == nil {
		return nil
	}
	return cancelAndWait(ctx, cancel, done)
}

func (c *scanController) WaitForIdle(ctx context.Context) error {
	c.mu.Lock()
	done := c.done
	c.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (c *scanController) Start(
	ctx context.Context,
	blockNumber uint64,
	task scanTask,
) {
	c.mu.Lock()
	if c.done != nil && c.block >= blockNumber {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	if err := c.CancelBefore(ctx, blockNumber); err != nil {
		return
	}

	c.mu.Lock()
	base := c.base
	if base == nil {
		base = context.WithoutCancel(ctx)
	}
	runCtx, cancel := context.WithCancel(base)
	done := make(chan struct{})
	c.block = blockNumber
	c.cancel = cancel
	c.done = done
	c.mu.Unlock()

	go func() {
		err := task.Run(runCtx)
		task.Complete(err)

		c.mu.Lock()
		if c.done == done {
			c.block = 0
			c.cancel = nil
			c.done = nil
		}
		close(done)
		c.mu.Unlock()
	}()
}

func cancelAndWait(ctx context.Context, cancel context.CancelFunc, done <-chan struct{}) error {
	if cancel != nil {
		cancel()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}
