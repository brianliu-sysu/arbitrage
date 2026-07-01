package pool

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/core/types"
)

// BlockLogApplier 将单个区块内的日志应用到池子状态（通常由 replay 层实现）。
type BlockLogApplier func(p *State, logs []types.Log) error

// BlockEvent 单区块待消费事件。
type BlockEvent struct {
	BlockNumber uint64
	Logs        []types.Log
}

// BeginLoading 进入加载阶段：Loaded=true，增量事件写入 pending buffer。
func (p *State) BeginLoading() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Loaded = true
}

// CompleteLoading 结束加载阶段：Loaded=false。
func (p *State) CompleteLoading() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Loaded = false
}

// Loading 返回 true 表示池子尚在加载。
func (p *State) Loading() bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Loaded
}

// ApplyBlockEvents 将区块日志写入 pending buffer。
// 仅在加载、drain 或已有 pending 时缓存；否则返回 false，由调用方同步 apply。
func (p *State) ApplyBlockEvents(block uint64, logs []types.Log) bool {
	if p == nil || len(logs) == 0 {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if block <= p.BlockNumber {
		return true
	}
	if !p.Loaded && !p.Draining && len(p.pendingEvents) == 0 {
		return false
	}
	p.pendingEvents = append(p.pendingEvents, BlockEvent{
		BlockNumber: block,
		Logs:        append([]types.Log(nil), logs...),
	})
	return true
}

// ApplyBlockEventsDirect 同步直接 apply（回补历史用），并更新 BlockNumber。
// apply 完成后调用 onApplied（持久化等）。
func (p *State) ApplyBlockEventsDirect(block uint64, logs []types.Log, apply BlockLogApplier) error {
	if p == nil {
		return fmt.Errorf("pool state is nil")
	}
	if apply == nil {
		return fmt.Errorf("block log applier is nil")
	}
	p.mu.RLock()
	if block <= p.BlockNumber {
		p.mu.RUnlock()
		return nil
	}
	p.mu.RUnlock()

	if len(logs) > 0 {
		if err := apply(p, logs); err != nil {
			return err
		}
	}
	p.mu.Lock()
	if block > p.BlockNumber {
		p.BlockNumber = block
	}
	p.mu.Unlock()

	p.fireOnApplied()
	return nil
}

// SetOnApplied 注册 apply 完成回调（持久化等）。
func (p *State) SetOnApplied(fn func(*State)) {
	if p == nil {
		return
	}
	p.eventMu.Lock()
	p.onApplied = fn
	p.eventMu.Unlock()
}

func (p *State) fireOnApplied() {
	p.eventMu.Lock()
	fn := p.onApplied
	p.eventMu.Unlock()
	if fn != nil {
		fn(p)
	}
}

func (p *State) consumeEvent(ev BlockEvent, applier BlockLogApplier) error {
	p.mu.RLock()
	stale := ev.BlockNumber <= p.BlockNumber
	p.mu.RUnlock()
	if stale {
		return nil
	}
	if len(ev.Logs) > 0 {
		if err := applier(p, ev.Logs); err != nil {
			return fmt.Errorf("apply block %d: %w", ev.BlockNumber, err)
		}
	}
	p.mu.Lock()
	if ev.BlockNumber > p.BlockNumber {
		p.BlockNumber = ev.BlockNumber
	}
	p.mu.Unlock()
	return nil
}

// DrainPendingBlockEvents 同步消费 pending buffer，直到没有积压事件。
func (p *State) DrainPendingBlockEvents(ctx context.Context, applier BlockLogApplier) error {
	if p == nil {
		return fmt.Errorf("pool state is nil")
	}
	if applier == nil {
		return fmt.Errorf("block log applier is nil")
	}
	p.mu.Lock()
	p.Loaded = false
	p.Draining = true
	p.mu.Unlock()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		p.mu.Lock()
		if len(p.pendingEvents) == 0 {
			p.Draining = false
			p.mu.Unlock()
			return nil
		}
		ev := p.pendingEvents[0]
		copy(p.pendingEvents, p.pendingEvents[1:])
		p.pendingEvents[len(p.pendingEvents)-1] = BlockEvent{}
		p.pendingEvents = p.pendingEvents[:len(p.pendingEvents)-1]
		p.mu.Unlock()

		if err := p.consumeEvent(ev, applier); err != nil {
			return err
		}
		p.fireOnApplied()
	}
}

// ClearPending 清空 pending buffer（加载失败回滚用）。
func (p *State) ClearPending() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.pendingEvents = nil
	p.mu.Unlock()
}

// PendingLen 返回 pending buffer 中待消费区块数。
func (p *State) PendingLen() int {
	if p == nil {
		return 0
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.pendingEvents)
}
