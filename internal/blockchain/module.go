package blockchain

import "go.uber.org/fx"

// Module 提供区块链访问层依赖（按需注入 Client / LogFetcher）。
var Module = fx.Module(
	"blockchain",
)

// Multicall 批量 RPC 调用见 pool_client.go。
