# Uniswap V3 Arbitrage System

本项目是一个基于 Go 的 Uniswap V3 本地缓存报价与套利系统，采用 DDD + Clean Architecture 分层。

**所有代码注释使用英文。**

## Architecture Principles

整体依赖方向必须保持：

```
interfaces -> application -> domain
infrastructure -> domain/application interface
```

分层约束：

| Layer | Responsibility | Constraints |
|-------|----------------|-------------|
| **domain** | 核心业务模型、领域规则、仓储接口 | 不能依赖 application、infrastructure、interfaces；不能出现 SQL、Redis、RPC、HTTP、ABI、goroutine 编排代码 |
| **application** | 用例编排 | 可以依赖 domain 对象和 domain 接口 |
| **infrastructure** | 技术实现 | 实现 domain/application 定义的接口 |
| **interfaces** | HTTP / gRPC / CLI 入口 | 只做输入输出适配，不写领域逻辑 |

## Directory Structure

```
arbitrage/
├── cmd/
├── internal/
│   ├── domain/
│   ├── application/
│   ├── infrastructure/
│   ├── interfaces/
│   ├── config/
│   └── app/
├── migrations/
├── configs/
├── scripts/
├── go.mod
└── Makefile
```

---

## cmd/

程序启动入口。

```
cmd/arbitrage/main.go
```

只做三件事：

1. 加载配置
2. 初始化 fx / wire 依赖注入
3. 启动应用生命周期

**不要在 `main.go` 里写业务逻辑。**

---

## internal/domain/

领域层，放核心业务模型、领域服务、仓储接口。

**不能出现：**

- SQL
- Redis
- RPC
- HTTP
- ABI
- JSON-RPC
- Ethereum client
- goroutine 启动编排

### domain/asset/

Token 资产元数据。

| File | Description |
|------|-------------|
| `token.go` | Token 实体 |
| `token_repository.go` | Token 仓储接口 |

```go
type Token struct {
    Address  common.Address
    Symbol   string
    Decimals uint8
}
```

`TokenRepository` 只定义接口，实现放 infrastructure。

### domain/blockchain/

区块链基础领域对象。

| File | Description |
|------|-------------|
| `block.go` | Block |
| `block_header.go` | BlockHeader |
| `block_range.go` | BlockRange |
| `checkpoint.go` | Checkpoint |
| `chain.go` | Chain |
| `reorg.go` | Reorg |
| `checkpoint_repository.go` | CheckpointRepository interface |

- `Checkpoint` — 本地同步到哪个区块
- `Reorg` — 链重组领域对象

**这里不放 RPC 调用。**

### domain/market/

市场状态领域，是核心领域之一。

| File | Description |
|------|-------------|
| `pool.go` | Pool 聚合根 |
| `pool_state.go` | 池子当前状态 |
| `pool_status.go` | Pool 生命周期状态 |
| `tick.go` | Tick |
| `tick_table.go` | Tick 集合 |
| `tick_bitmap.go` | TickBitmap |
| `pool_event.go` | Swap / Mint / Burn / Initialize 事件 |
| `snapshot.go` | 池子快照 |
| `snapshot_repository.go` | SnapshotRepository 接口 |
| `pool_repository.go` | PoolRepository 接口 |
| `registry.go` | PoolRegistry 接口 |

**核心规则：**

- `Pool` 是聚合根
- `Pool.Apply(event)` 负责根据 Swap / Mint / Burn 修改池子状态
- `PoolEvent` 只是链上事件事实，不负责修改 Pool
- Repository 只定义接口

### domain/quote/

报价领域。

| File | Description |
|------|-------------|
| `quote_service.go` | QuoteService |
| `quote_result.go` | QuoteResult |
| `route.go` | Route |
| `route_service.go` | RouteService / PathFinder |
| `swap_math.go` | SwapMath |
| `sqrt_price_math.go` | SqrtPriceMath |
| `liquidity_math.go` | LiquidityMath |
| `tick_math.go` | TickMath |
| `full_math.go` | FullMath |

**职责：**

- `Route` — 只表示一条路径
- `RouteService` — 只负责找路径
- `QuoteService` — 负责对 Pool 或 Route 报价
- `SwapMath` 等数学服务 — 只做纯计算

**不要让 `RouteService` 直接依赖 `QuoteService`。** 应用层负责组合：

```
QuoteAppService -> RouteService
QuoteAppService -> QuoteService
```

### domain/arbitrage/

套利发现领域（M6）。

| File | Description |
|------|-------------|
| `opportunity.go` | Opportunity |
| `strategy.go` | Strategy |
| `evaluator.go` | Evaluator |
| `optimizer.go` | Optimizer |
| `gas_estimator.go` | GasEstimator interface |
| `dependency_graph.go` | DependencyGraph |
| `opportunity_repository.go` | OpportunityRepository interface |

**职责：**

- 只负责发现机会、评估利润、筛选机会
- **不**构建交易
- **不**签名
- **不**发送交易

### domain/execution/

套利执行领域（M7）。

| File | Description |
|------|-------------|
| `execution_plan.go` | ExecutionPlan |
| `transaction.go` | Transaction |
| `bundle.go` | Bundle |
| `nonce.go` | Nonce |
| `gas_policy.go` | GasPolicy |
| `slippage_policy.go` | SlippagePolicy |
| `execution_result.go` | ExecutionResult |

**职责：**

- 只描述执行相关领域对象
- **不**直接调用 RPC
- **不**直接签名
- **不**直接发送交易

---

## internal/application/

应用层，放用例编排。application 可以调用 domain 对象和 domain 接口。

### application/quote/

报价用例。

| File | Description |
|------|-------------|
| `quote_app_service.go` | 报价用例编排 |
| `quote_request.go` | QuoteRequest |
| `quote_response.go` | QuoteResponse |

**职责：**

1. 接收 `QuoteRequest`
2. 加载 Pool
3. 调用 `RouteService` 找路径
4. 调用 `QuoteService` 报价
5. 返回 `QuoteResponse`

### application/sync/

同步流程编排，是系统启动和运行的核心。

| File | Description |
|------|-------------|
| `sync_orchestrator.go` | 全局同步启动编排 |
| `bootstrap_service.go` | 冷启动初始化 Pool（slot0、liquidity、tick、bitmap） |
| `pool_lifecycle_service.go` | 单个 Pool 的启动、停止、动态新增、动态移除 |
| `catchup_service.go` | 从 checkpoint 追历史区块 |
| `head_sync_service.go` | 订阅新区块，触发 block apply |
| `block_apply_service.go` | 应用一个区块内的 PoolEvent |
| `snapshot_service.go` | 创建和加载 snapshot |
| `snapshot_scheduler.go` | 定时兜底 snapshot |
| `reorg_recovery_service.go` | 链重组恢复 |
| `readiness_service.go` | 判断系统或 Pool 是否可以对外报价 |
| `health_service.go` | 检查 RPC、DB、Redis 等依赖是否存活 |

**职责划分：**

| Service | Responsibility |
|---------|----------------|
| `SyncOrchestrator` | 全局同步启动编排 |
| `BootstrapService` | 冷启动初始化 Pool，包括读取 slot0、liquidity、tick、bitmap |
| `PoolLifecycleService` | 单个 Pool 的启动、停止、动态新增、动态移除 |
| `CatchupService` | 从 checkpoint 追历史区块 |
| `HeadSyncService` | 订阅新区块，并触发 block apply |
| `BlockApplyService` | 应用一个区块内的 PoolEvent：加载 Pool → `pool.Apply(event)` → 保存 Pool → 保存 checkpoint |
| `SnapshotService` | 创建和加载 snapshot |
| `SnapshotScheduler` | 定时兜底 snapshot；主触发应放在 ApplyBlock 后按 block 间隔触发 |
| `ReorgRecoveryService` | 链重组恢复：检测共同祖先 → 回滚 snapshot → replay 日志 → 恢复 ready |
| `ReadinessService` | 判断系统或 Pool 是否可以对外报价；建议支持 pool-level readiness |
| `HealthService` | 检查 RPC、DB、Redis 等依赖是否存活 |

**关键原则：**

- Sync 是 application 流程，不是 domain
- `ReorgRecoveryService` 不应该用定时任务作为主方案；Reorg 应该在 NewHead 事件中检测
- Snapshot 可以使用 block 触发 + 定时兜底

### application/arbitrage/

套利发现用例。

| File | Description |
|------|-------------|
| `scan_service.go` | 根据 changedPools 找到受影响路径 |
| `opportunity_service.go` | 调用 Evaluator、Optimizer、GasEstimator 生成 Opportunity |
| `publish_service.go` | 发布套利机会给日志、HTTP、WebSocket 或执行层 |

### application/execution/

执行用例。

| File | Description |
|------|-------------|
| `execution_service.go` | 接收 Opportunity，编排执行流程 |
| `simulator.go` | 提交前模拟交易 |
| `tx_builder.go` | 根据 Opportunity 构建 calldata / transaction |
| `signer.go` | 签名交易 |
| `submitter.go` | 发送交易或 bundle |
| `nonce_manager.go` | 管理 nonce |
| `retry_policy.go` | 处理失败重试 |

---

## internal/infrastructure/

基础设施层，放技术实现。

### infrastructure/blockchain/

链上读取实现。

| File | Description |
|------|-------------|
| `eth_client.go` | Ethereum client 封装 |
| `log_fetcher.go` | eth_getLogs |
| `head_subscriber.go` | SubscribeNewHead |
| `multicall.go` | Multicall |
| `abi_parser.go` | ABI decode |
| `factory_reader.go` | UniswapV3Factory 读取 |

实现 application 需要的链上接口。

### infrastructure/persistence/

持久化实现。

```
persistence/
├── postgres/
│   ├── pool_repository.go
│   ├── snapshot_repository.go
│   ├── checkpoint_repository.go
│   └── opportunity_repository.go
└── memory/
    └── pool_repository.go
```

- domain 里定义 Repository interface
- infrastructure 里实现 Repository

### infrastructure/registry/

PoolRegistry 的不同实现。

| File | Description |
|------|-------------|
| `config_registry.go` | 配置文件驱动 |
| `postgres_registry.go` | 数据库驱动 |
| `subgraph_registry.go` | Subgraph 驱动 |

告诉系统当前需要同步哪些 Pool；支持启动加载和运行时动态新增/移除 Pool。

### infrastructure/cache/

| File | Description |
|------|-------------|
| `redis.go` | Redis 缓存 |

用于：Pool 状态缓存、Quote 缓存、Readiness 状态缓存、临时计算结果。

### infrastructure/execution/

执行基础设施。

| File | Description |
|------|-------------|
| `flashbots_client.go` | Flashbots bundle submit |
| `builder_client.go` | Builder relay submit |
| `rpc_sender.go` | 普通 RPC 交易发送 |
| `mev_contract.go` | MEV 合约调用封装 |

---

## internal/interfaces/

外部入口层。

### interfaces/http/

| File | Description |
|------|-------------|
| `quote_handler.go` | 报价 HTTP 入口 |
| `opportunity_handler.go` | 套利机会 HTTP 入口 |
| `readiness_handler.go` | Readiness HTTP 入口 |
| `health_handler.go` | Health HTTP 入口 |
| `router.go` | 路由注册 |

HTTP 请求解析 → 调用 application service → 返回 JSON response。**不要在 handler 里写领域逻辑。**

### interfaces/grpc/

| File | Description |
|------|-------------|
| `quote_server.go` | gRPC 入口 |

### interfaces/cli/

| File | Description |
|------|-------------|
| `sync.go` | 同步 CLI |
| `snapshot.go` | Snapshot CLI |

CLI 工具入口，例如：手动触发 snapshot、手动 replay block range、查看 checkpoint。

---

## internal/config/

配置加载。

| File | Description |
|------|-------------|
| `config.go` | 配置结构体与加载逻辑 |
| `module.go` | fx 配置模块 |

包含：RPC URL、DB URL、Redis URL、Pool 配置、Snapshot 策略、Gas 策略、日志配置。

---

## internal/app/

应用装配层。

| File | Description |
|------|-------------|
| `fx.go` | fx 应用入口 |
| `modules.go` | 各层 module 注册 |
| `lifecycle.go` | 生命周期管理 |

职责：

- 注册 fx module
- 连接 domain / application / infrastructure / interfaces
- 启动 SyncOrchestrator
- 启动 HTTP server
- 处理 graceful shutdown

---

## migrations/

数据库迁移 SQL。建议包括：

- `pools`
- `pool_state`
- `ticks`
- `tick_bitmap`
- `snapshots`
- `checkpoints`
- `opportunities`
- `executions`

---

## configs/

```
configs/config.yaml
```

---

## scripts/

开发和运维脚本，例如：

- 启动本地 postgres
- 初始化数据库
- 压测 quote API
- 拉取指定 pool 日志

---

## Code Generation Rules

AI 写代码时必须遵守：

1. **domain** — 只写业务模型、领域规则、接口
2. **application** — 只写用例编排
3. **infrastructure** — 只写外部技术实现
4. **interfaces** — 只写输入输出适配
5. `Pool.Apply(event)` 是市场状态变化唯一入口
6. `PoolEvent` 只是事实，不调用 Repository，不保存 Pool
7. Repository 接口在 domain，实现放 infrastructure
8. `SyncOrchestrator` 不保存 Pool 状态，只编排流程
9. Readiness 必须支持 Pool 级别状态
10. Snapshot 主触发在 ApplyBlock 后，定时任务只兜底
11. Reorg 主触发在 NewHead 流程中，定时检查只兜底
12. Arbitrage Discovery 不发交易
13. Execution Engine 不找套利机会

---

## Call Chains

### Sync Flow

```
NewHead
  -> HeadSyncService
  -> Reorg 检查
  -> LogFetcher.FetchLogs
  -> ABIParser.ParsePoolEvents
  -> BlockApplyService.ApplyBlock
  -> PoolRepository.Get
  -> Pool.Apply(event)
  -> PoolRepository.Save
  -> CheckpointRepository.Save
  -> SnapshotPolicy 判断是否需要 Snapshot
  -> Arbitrage Scan
```

### Quote Flow

```
HTTP /quote
  -> QuoteHandler
  -> QuoteAppService
  -> PoolRepository.Get
  -> RouteService.FindRoutes
  -> QuoteService.QuoteRoute
  -> SwapMath
  -> QuoteResponse
```
