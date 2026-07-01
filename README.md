# Uniswap V3 Multi-Chain Quote Service

基于 Go 的生产级 Uniswap V3 多链报价引擎。采用**领域驱动分层**与 **Uber Fx 依赖注入**，池子 tick 快照由独立工具 `cmd/snapshot` 一次性写入 PostgreSQL，`cmd/arbitrage` 仅加载 **READY** 池子并通过 **BlockSync** 按区块增量同步，对外提供 HTTP API 报价与跨池路由。

## 特性

- **多链支持** — 每条链独立 RPC、BlockSync、池子列表，可同时监控 Ethereum / Arbitrum / Base / Optimism 等
- **Snapshot 与增量分离** — `cmd/snapshot` 链上全量同步 tick 地图；`cmd/arbitrage` 只做增量，不在运行时全量重建
- **`snapshot_status` 生命周期** — `INITIALIZING` / `READY` / `FAILED` / `DISABLED`，arbitrage 只跟踪 READY，并定期轮询 DB 动态增删池子
- **BlockSync 增量同步** — `newHeads → eth_getLogs → eventCh → replay.ApplyBlock → PostgreSQL`
- **Per-pool Event Consumer** — 每个池子独立 `chan BlockEvent`（容量 100）+ 后台协程顺序消费；动态加池回补期间 `Loaded=true` 暂停消费，回补用 `ApplyBlockEventsDirect` 同步 apply
- **Pool 状态机** — `pool/replay` 统一处理 Swap / Mint / Burn，历史回补与实时增量共用同一套逻辑
- **跨池报价** — `router` BFS 路径搜索 + `arbitrage` 最优路径选择 + 本地模拟报价
- **多 DEX 扩展** — `ProcessorRegistry` 按协议注册 BlockProcessor（当前内置 Uniswap V3）
- **动态池子发现** — Factory 自动发现 token 池子；Subgraph 用于 snapshot / 配置收集池子列表
- **PostgreSQL 持久化** — Repository 模式（PoolRepo / SyncRepo），池子快照 + 链级同步进度
- **Redis 缓存** — 代币元信息（symbol / decimals）与已应用日志 dedup
- **Cron 健康检查** — 对比链上 slot0 与内存状态，不一致时告警（由 BlockSync 增量修复，不触发运行时全量同步）
- **HTTP API** — Gin RESTful 接口，API Key 鉴权 + 令牌桶限流
- **Prometheus / OpenTelemetry** — 指标与 OTLP 链路追踪
- **Uber Fx DI** — 基础设施模块自动装配，领域服务在 bootstrap 中按链初始化

## 项目结构

按**领域（Domain）**组织，而非 RPC / DB / Pool 横向拆分：

```
arbitrage/
├── cmd/
│   ├── arbitrage/main.go                   # 长期运行：报价 + BlockSync 增量
│   ├── snapshot/main.go                    # 一次性：链上全量快照 → pool_states
│   └── migrate/main.go                     # 数据库迁移 CLI
├── internal/
│   ├── app/
│   │   ├── app.go                          # Fx 根模块（arbitrage）
│   │   ├── bootstrap/chains.go             # 启动：READY 快照恢复 + BlockSync
│   │   └── snapshot/                       # Fx 模块（snapshot 工具）
│   ├── api/                                # HTTP 层
│   ├── blockchain/                         # BlockSync、eth_getLogs、Processor
│   ├── pool/
│   │   ├── pool.go / state.go              # 运行时 State + 本地报价
│   │   ├── loading.go                      # eventCh 缓冲、ApplyBlockEventsDirect、Event Consumer
│   │   ├── cache.go
│   │   ├── events.go
│   │   ├── snapshot/
│   │   │   ├── loader.go                   # DB 恢复（READY 池子）
│   │   │   ├── runner.go                   # snapshot 任务编排
│   │   │   └── scanner.go                  # 链上 tick bitmap 扫描
│   │   └── replay/                         # ApplyBlock / Swap / Mint / Burn
│   ├── storage/
│   │   ├── repo.go                         # PoolRepo / SyncRepo 接口
│   │   ├── snapshot_status.go              # INITIALIZING / READY / FAILED / DISABLED
│   │   └── postgres/
│   ├── router/ / arbitrage/ / quote/
│   ├── service/                            # MultiChain / MultiPool / 单池服务
│   ├── store/                              # legacy Storer 适配 storage
│   ├── cache/ / config/ / logx/ / metrics/ / tracing/
│   └── subgraph/
├── migrations/
│   ├── 001_init.sql                        # pool_states + history
│   ├── 002_token_metadata.sql
│   ├── 003_chain_sync_state.sql            # LastProcessedBlock
│   └── 004_snapshot_status.sql             # snapshot_status 列
├── config.yaml
└── README.md
```

## 架构概览

```
                    Uber Fx
                       │
         bootstrap.RegisterChains
                       │
    ┌──────────────────┼──────────────────┐
    │                  │                  │
 READY 快照        BlockSync          MultiPoolService
 (Loader)       (CatchUp + Run)    (报价 / 状态轮询 / 健康检查)
    │                  │                  │
    └────────► pool.Cache ◄────────────────┘
                       │
              UniswapV3BlockProcessor
                       │
    ProcessBlock → ApplyBlockEvents → eventCh
                       │                    │
              BackfillPool → ApplyBlockEventsDirect（回补）
                       │
              Event Consumer → replay.ApplyBlock → onApplied → PostgreSQL
                       │
         router.PathFinder → arbitrage.CrossQuoter → api (HTTP)
```

### 两阶段部署

| 阶段 | 命令 | 职责 |
|------|------|------|
| 1. 快照 | `cmd/snapshot` | 读取 `pools` + `auto_discover`，链上 `DoFullSync`，写入 `pool_states`，标记 `snapshot_status=READY` |
| 2. 运行 | `cmd/arbitrage` | 只加载 `READY` 池子，BlockSync 增量同步，HTTP 报价 |

### BlockSync 流水线（常态）

```
SubscribeNewHead
      ↓
LastBlock + 1 .. head
      ↓
eth_getLogs (按区块)
      ↓
按 Pool 分组 → ApplyBlockEvents → eventCh
      ↓
Event Consumer（Loaded=false 时）→ replay.ApplyBlock
      ↓
onApplied → PostgreSQL（pool_states + history）
      ↓
更新 LastProcessedBlock（链级游标）
```

`BlockProcessor` 接口使后续增加 Uniswap V4、Aerodrome 等协议时只需新增实现，`BlockSync` 本身无需修改。

### 动态加池 handoff

新池加入或 `snapshot_status` 变为 READY 时：

```
BeginLoading (Loaded=true, consumer 暂停)
      ↓
notifyPoolAddresses → BlockSync 开始 getLogs（live 事件进 eventCh）
      ↓
BackfillPool → ApplyBlockEventsDirect（同步回补 + 持久化）
      ↓
CompleteLoading (Loaded=false) → consumer 消费 eventCh 积压（旧 block 丢弃）
      ↓
WaitEventQueueEmpty
```

### Fx 模块依赖

```
config → storage.postgres → store(adapter) → cache → pool.Cache
  → service → api
  → bootstrap.RegisterChains（BlockSync + 链服务 + 状态轮询）
```

## 快速开始

### 前提条件

- Go 1.21+
- 以太坊 RPC 端点（WebSocket + HTTP，如 Alchemy / Infura）
- **PostgreSQL**（必需）— 池子快照、同步进度、`snapshot_status`
- （可选）Redis — 代币元信息缓存与 applied-log dedup
- （可选）Jaeger / Grafana Tempo — 链路追踪

### 1. 配置

```yaml
# config.yaml
http_port: 8080
health_check_interval_sec: 30
pool_status_poll_interval_sec: 30   # arbitrage 轮询 snapshot_status 增删池子
log_file: "arbitrage.log"
log_level: "info"
max_block_gap_for_full_sync: 100
max_hops: 2
http_rate_limit: 100
api_key: ""
db_url: "postgres://user:pass@localhost:5432/arbitrage?sslmode=disable"
redis_url: "redis://localhost:6379/0"
tracing_endpoint: ""

chains:
  - name: "ethereum"
    ws_endpoint: "wss://eth-mainnet.g.alchemy.com/v2/{{ALCHEMY_API_KEY}}"
    rpc_endpoint: "https://eth-mainnet.g.alchemy.com/v2/{{ALCHEMY_API_KEY}}"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    base_tokens:
      - "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"   # WETH
      - "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"   # USDC
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
        sync_from_block: 25393441
    auto_discover:
      enabled: true
      min_tvl_usd: 500000
      max_pools: 20
```

`ws_endpoint` 用于 BlockSync 的 `SubscribeNewHead`；HTTP RPC 用于 `eth_getLogs`、snapshot 全量同步、健康检查等。

配置中 `{{ENV_VAR}}` 会替换为环境变量值。

### 2. 数据库迁移

```bash
export ARBITRAGE_DB_URL="postgres://user:pass@localhost:5432/arbitrage?sslmode=disable"
go run ./cmd/migrate/ -db "$ARBITRAGE_DB_URL" up
```

### 3. 运行 Snapshot（首次或新增池子）

```bash
export ALCHEMY_API_KEY="your-key"
go run ./cmd/snapshot/ -config config.yaml
# 仅指定链：
go run ./cmd/snapshot/ -config config.yaml -chain ethereum
```

Snapshot 会跳过 `READY` / `DISABLED` 池子，对其余池子执行链上全量同步并写入 `pool_states`。

### 4. 运行 Arbitrage

```bash
go run ./cmd/arbitrage/ -config config.yaml
```

arbitrage 启动时从 DB 加载 `snapshot_status=READY` 的池子，CatchUp 至链头后进入实时 BlockSync；后台轮询 DB，READY 新增则加池，非 READY 则移除。

## HTTP API

所有 API 按链分组：`/api/v1/:chain/...`

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | 健康检查 |
| `GET` | `/metrics` | Prometheus 指标 |
| `GET` | `/api/v1/:chain/pools` | 列出该链所有池子 |
| `GET` | `/api/v1/:chain/pools/:address` | 单个池子详情 |
| `GET` | `/api/v1/:chain/pools/:address/price` | 现货价格 |
| `POST` | `/api/v1/:chain/pools/:address/quote` | 单池报价 |
| `POST` | `/api/v1/:chain/quote` | 跨池报价 |

认证：配置了 `api_key` 时需传 `X-API-Key` header。  
限流：令牌桶，默认 100 req/s。

### 示例

```bash
curl http://localhost:8080/api/v1/ethereum/pools

curl -X POST http://localhost:8080/api/v1/ethereum/pools/0x8ad599.../quote \
  -H "Content-Type: application/json" \
  -d '{"amountIn":"1000000","tokenIn":"0xA0b869..."}'

curl -X POST http://localhost:8080/api/v1/ethereum/quote \
  -H "Content-Type: application/json" \
  -d '{"amountIn":"1000000","tokenIn":"0xA0b869...","tokenOut":"0x2260FAC..."}'
```

## 配置说明

### 全局配置

| 字段 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `http_port` | int | 8080 | HTTP 监听端口，0 禁用 |
| `health_check_interval_sec` | int | 30 | 健康检查间隔，0 禁用 |
| `pool_status_poll_interval_sec` | int | 30 | `snapshot_status` 轮询间隔，0 默认 30 |
| `log_file` | string | `""` | 日志文件，空则 stderr |
| `log_level` | string | `info` | debug / info / warn / error |
| `max_block_gap_for_full_sync` | int | 100 | snapshot 全量同步区块间隔阈值（snapshot 工具使用） |
| `max_hops` | int | 2 | 跨池最大跳数 |
| `http_rate_limit` | int | 100 | API 限流，0 不限 |
| `api_key` | string | `""` | X-API-Key，空不鉴权 |
| `db_url` | string | `""` | PostgreSQL，snapshot / arbitrage 均需要 |
| `redis_url` | string | `""` | Redis，空禁用 |
| `tracing_endpoint` | string | `""` | OTLP endpoint，空禁用 |

### 链级配置 (`chains[]`)

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | string | 链标识，HTTP 路由与 DB key |
| `ws_endpoint` | string | WebSocket（BlockSync newHeads） |
| `rpc_endpoint` | string | HTTP RPC，空则从 ws 推导 |
| `factory_address` | string | Uniswap V3 Factory |
| `multicall_address` | string | Multicall3，空用标准地址 |
| `quoter_address` | string | QuoterV2，空用默认 |
| `base_tokens` | []string | 跨池中间代币白名单 |
| `max_hops` | int | 跨池跳数，0 用全局值 |
| `pools` | []object | 监控池子列表（snapshot + arbitrage 共用） |
| `auto_discover` | object | Subgraph 自动发现（snapshot 收集 + 可选运行时发现） |

### 池子配置 (`pools[]`)

| 字段 | 类型 | 说明 |
|------|------|------|
| `pool_address` | string | Pool 合约地址 |
| `sync_from_block` | int | 历史起始区块（snapshot 全量同步起点） |

## 状态同步策略

### `snapshot_status` 状态机

| 状态 | 含义 | arbitrage 行为 |
|------|------|----------------|
| `INITIALIZING` | snapshot 进行中 | 不加载 |
| `READY` | 快照完成 | 加载并 BlockSync 增量 |
| `FAILED` | snapshot 失败 | 不加载 |
| `DISABLED` | 人工禁用 | 不加载；snapshot 跳过 |

手动禁用示例：

```sql
UPDATE pool_states SET snapshot_status = 'DISABLED'
WHERE chain_name = 'ethereum' AND pool_address = '0x8ad599...';
```

### 各阶段职责

| 阶段 | 触发 | 执行方 | 说明 |
|------|------|--------|------|
| 全量 tick 快照 | 首次 / 新增池 | `cmd/snapshot` | `DoFullSync` → `pool_states`，`snapshot_status=READY` |
| 冷启动恢复 | arbitrage 启动 | `snapshot.Loader.RestoreAllReady` | 只加载 READY 快照到内存 |
| 链级 CatchUp | arbitrage 启动后 | `BlockSync.CatchUpFrom` | eth_getLogs 按区块 replay（链级游标） |
| 实时增量 | newHeads | `BlockSync.Run` → `ProcessBlock` | 写入 eventCh，consumer 顺序 apply + 持久化 |
| 动态加池回补 | READY 新增 / 手动 AddPool | `BackfillPool` + handoff | Direct apply 回补 + consumer 消费积压 |
| 健康检查 | Cron | `runHealthCheck` | 对比 slot0，仅告警；无快照时提示运行 snapshot |

**arbitrage 不在运行时执行链上全量同步。** 若池子无 tick 快照，会报错并要求先运行 `cmd/snapshot`。

### 两级游标

| 游标 | 存储 | 含义 |
|------|------|------|
| 链级 `last_processed_block` | `chain_sync_state` | BlockSync 已扫过的最高区块（getLogs 协调用） |
| 池级 `block_number` | `pool_states` | 该池子状态真实高度（报价真相） |

## Snapshot 工具

```bash
# 全部链
go run ./cmd/snapshot/ -config config.yaml

# 指定链
go run ./cmd/snapshot/ -config config.yaml -chain ethereum
```

流程：

1. 合并配置 `pools` 与 `auto_discover` 得到目标池子列表
2. 每条链一次查询 `ListSnapshotStatuses`，跳过 `READY` / `DISABLED`
3. 标记 `INITIALIZING` → 链上 `DoFullSync` → 写入 `pool_states` → 标记 `READY`（失败则 `FAILED`）

## Subgraph 自动发现

```yaml
chains:
  - name: "ethereum"
    auto_discover:
      enabled: true
      subgraph_url: "https://gateway.thegraph.com/api/{{KEY}}/subgraphs/id/..."
      min_tvl_usd: 500000
      min_volume_usd: 10000000
      max_pools: 20
      order_by: "totalValueLockedUSD"
```

- **snapshot**：收集待快照池子列表
- **arbitrage**：启动时只加载 DB 中 READY 池；运行时通过 `pool_status_poll_interval_sec` 轮询 DB 增删

## 数据库迁移

```bash
go run ./cmd/migrate/ -db "$DB_URL" up
go run ./cmd/migrate/ -db "$DB_URL" down
```

| 文件 | 说明 |
|------|------|
| `001_init.sql` | `pool_states` 快照 + `pool_states_history` 历史 |
| `002_token_metadata.sql` | 代币 symbol / decimals 缓存 |
| `003_chain_sync_state.sql` | 链级 `last_processed_block`（BlockSync checkpoint） |
| `004_snapshot_status.sql` | `snapshot_status` 列 + 索引 |

启动时 `storage/postgres` 模块自动执行未应用的迁移（幂等安全网）。

## 监控指标

`GET /metrics` 暴露 Prometheus 指标：

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `arbitrage_events_total` | Counter | 事件数 (swap/mint/burn) |
| `arbitrage_ws_reconnects_total` | Counter | WS 重连 |
| `arbitrage_quotes_total` | Counter | 报价请求数 |
| `arbitrage_health_repairs_total` | Counter | 健康检查修复次数 |
| `arbitrage_price` | Gauge | 现货价格 |
| `arbitrage_block_number` | Gauge | 池子当前区块 |
| `arbitrage_http_request_duration_seconds` | Histogram | API 耗时 |

## 多链示例

```yaml
chains:
  - name: "ethereum"
    ws_endpoint: "wss://eth-mainnet.g.alchemy.com/v2/{{KEY}}"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"

  - name: "arbitrum"
    ws_endpoint: "wss://arb-mainnet.g.alchemy.com/v2/{{KEY}}"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    pools:
      - pool_address: "0xC6962004f452bE9203591991D15f6b388e09E8D0"
```

每条链需分别运行 snapshot，再启动 arbitrage。

## 代币元信息缓存

1. **Redis**（1h TTL）
2. **PostgreSQL** `token_metadata`
3. **RPC** ERC20 查询，结果回写 DB + Redis

## 扩展指南

| 目标 | 做法 |
|------|------|
| 新增 DEX 协议 | 实现 `BlockProcessor` + `ProcessorRegistry.Register` |
| 新增链 | 在 `config.yaml` 增加 `chains[]`，运行 snapshot 后启动 arbitrage |
| 禁用池子 | DB 设置 `snapshot_status=DISABLED` |
| 自定义 HTTP | 扩展 `internal/api/routes.go` |
| 套利检测 | 在 `internal/arbitrage/` 扩展逻辑，复用 `pool.Cache` + `router` |

## 许可

MIT
