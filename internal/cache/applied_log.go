package cache

import "context"

// AppliedLogCache 用于记录“最近已应用”的池子日志，避免重放阶段重复应用同一事件。
type AppliedLogCache interface {
	// MarkAppliedIfNew 以原子方式记录事件，返回 true 表示首次记录（应继续应用）。
	// 返回 false 表示该事件近期已处理过（可安全跳过）。
	MarkAppliedIfNew(ctx context.Context, chainName, poolAddress string, blockNumber uint64, txHash string, logIndex uint) (bool, error)
	Close() error
}
