package blockchain

import (
	"context"
)

// BlockProcessor 处理单个区块的同步流水线。
// 内部完成：eth_getLogs → 按 Pool 分组 → ApplyBlock → 事务提交。
type BlockProcessor interface {
	ProcessBlock(ctx context.Context, block uint64) error
}
