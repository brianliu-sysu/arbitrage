# Uniswap V3 Multi-Chain Quote Service

基于 Go 的生产级 Uniswap V3 多链报价引擎。采用**领域驱动分层**与 **Uber Fx 依赖注入**，池子状态长期保存在 PostgreSQL，启动时从 Snapshot 恢复，后续通过 **BlockSync** 按区块增量同步，对外提供 HTTP API 报价与跨池路由。

## 特性

- **多链支持** — 每条链独立 RPC、BlockSync、池子列表，可同时监控 Ethereum / Arbitrum / Base / Optimism 等
- **BlockSync 增量同步** — `newHeads → eth_getLogs → ApplyBlock → PostgreSQL`，按区块顺序更新，无 per-pool WS 重复应用
- **Snapshot 冷启动** — 启动时从 PostgreSQL 恢复池子状态，CatchUp 补块至链头后进入实时同步
- **Pool 状态机** — `pool/replay` 统一处理 Swap / Mint / Burn，历史补块与实时同步共用同一套逻辑
- **Tick Bitmap 全量重建** — 健康检查 / 全量修复时从链上读取 tick bitmap，Multicall3 批量查询
- **跨池报价** — `router` BFS 路径搜索 + `arbitrage` 最优路径选择 + 本地模拟报价
- **多 DEX 扩展** — `ProcessorRegistry` 按协议注册 BlockProcessor（当前内置 Uniswap V3）
- **动态池子发现** — Factory 自动发现 token 池子；Subgraph 启动时拉取 Top N 池子
- **PostgreSQL 持久化** — Repository 模式（PoolRepo / SyncRepo），池子快照 + 链级同步进度
- **Redis 缓存** — 代币元信息（symbol / decimals）与已应用日志 dedup
- **Cron 健康检查** — 对比链上/内存状态，不一致时触发全量/轻量修复
- **HTTP API** — Gin RESTful 接口，API Key 鉴权 + 令牌桶限流
- **Prometheus / OpenTelemetry** — 指标与 OTLP 链路追踪
- **Uber Fx DI** — 基础设施模块自动装配，领域服务在 bootstrap 中按链初始化

## 项目结构

按**领域（Domain）**组织，而非 RPC / DB / Pool 横向拆分：

```
arbitrage/
├── cmd/
│   ├── arbitrage/main.go                   # 入口：app.New().Run()
│   └── migrate/main.go                     # 数据库迁移 CLI
├── internal/
│   ├── app/
│   │   ├── app.go                          # Fx 根模块装配
│   │   └── bootstrap/chains.go             # 启动：Snapshot 恢复 + BlockSync
│   ├── api/                                # HTTP 层
│   │   ├── server.go                       # Gin 服务器（限流/鉴权/指标）
│   │   ├── routes.go                       # REST 处理器
│   │   └── types.go                        # QuoteProvider 接口
│   ├── blockchain/                         # 区块链访问层
│   │   ├── client.go                       # HTTP RPC ethclient
│   │   ├── subscriber.go                   # WS SubscribeNewHead
│   │   ├── log_fetcher.go                  # eth_getLogs
│   │   ├── block_sync.go                   # BlockSync 协调器
│   │   ├── processor.go                    # BlockProcessor 接口
│   │   ├── uniswap_v3_processor.go         # V3 区块处理器
│   │   ├── processor_registry.go           # 多 DEX Processor 注册表
│   │   └── pool_client.go                  # RPC + Multicall3 + Factory + Quoter
│   ├── pool/                               # 池子领域
│   │   ├── pool.go / state.go              # 运行时 State + 本地报价
│   │   ├── cache.go                        # sync.Map 池子缓存
│   │   ├── events.go                       # Swap / Mint / Burn 解析
│   │   ├── snapshot/                       # loader（DB 恢复）+ scanner（链上 bitmap）
│   │   └── replay/                         # ApplyBlock / ApplySwap / Mint / Burn
│   ├── storage/
│   │   ├── repo.go                         # PoolRepo / SyncRepo 接口
│   │   └── postgres/                       # pool_repo / sync_repo / tick_repo
│   ├── router/                             # 跨池路径 BFS 搜索
│   ├── arbitrage/                          # 跨池最优报价
│   ├── quote/                              # 报价类型 + Quoter
│   ├── service/                            # 编排层（MultiChain / MultiPool / 单池服务）
│   ├── store/                              # legacy Storer 适配 storage
│   ├── cache/                              # Redis 代币元信息 + applied-log dedup
│   ├── config/ / logx/ / metrics/ / tracing/
│   ├── subgraph/                           # Subgraph 自动发现
│   └── subscriber/                         # deprecated 别名 → blockchain
├── migrations/
│   ├── 001_init.sql                        # pool_states + history
│   ├── 002_token_metadata.sql              # 代币元信息
│   └── 003_chain_sync_state.sql            # LastProcessedBlock
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
 Snapshot          BlockSync          MultiPoolService
 (DB 恢复)      (CatchUp + Run)         (报价 / 健康检查)
    │                  │                  │
    └────────► pool.Cache ◄────────────────┘
                       │
              UniswapV3BlockProcessor
                       │
         eth_getLogs → replay.ApplyBlock
                       │
              PostgreSQL (PoolRepo + SyncRepo)
                       │
         router.PathFinder → arbitrage.CrossQuoter → api (HTTP)
```

### BlockSync 流水线

```
SubscribeNewHead
      ↓
LastBlock + 1 .. head
      ↓
eth_getLogs (按区块)
      ↓
按 Pool 分组
      ↓
replay.ApplyBlock
      ↓
PostgreSQL 事务提交
      ↓
更新 LastProcessedBlock
```

`BlockProcessor` 接口使后续增加 Uniswap V4、Aerodrome 等协议时只需新增实现，`BlockSync` 本身无需修改。

### Fx 模块依赖

```
config → storage.postgres → store(adapter) → cache → pool.Cache
  → service → api
  → bootstrap.RegisterChains（BlockSync + 链服务）
```

## 快速开始

### 前提条件

- Go 1.21+
- 以太坊 RPC 端点（WebSocket + HTTP，如 Alchemy / Infura）
- （推荐）PostgreSQL — 池子状态与同步进度持久化
- （可选）Redis — 代币元信息缓存与 applied-log dedup
- （可选）Jaeger / Grafana Tempo — 链路追踪

### 配置

```yaml
# config.yaml
http_port: 8080
health_check_interval_sec: 30
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
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    multicall_address: ""
    quoter_address: ""
    max_hops: 2
    base_tokens:
      - "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"   # WETH
      - "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"   # USDC
      - "0xdAC17F958D2ee523a2206206994597C13D831ec7"   # USDT
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
        sync_from_block: 25393441
```

`ws_endpoint` 用于 BlockSync 的 `SubscribeNewHead`；HTTP RPC 用于 `eth_getLogs`、健康检查、全量修复等（可从 ws 地址推导）。

### 运行

```bash
export ALCHEMY_API_KEY="your-key"
go run ./cmd/arbitrage/ -config config.yaml
```

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
| `log_file` | string | `""` | 日志文件，空则 stderr |
| `log_level` | string | `info` | debug / info / warn / error |
| `max_block_gap_for_full_sync` | int | 100 | 全量 tick 重建区块间隔阈值 |
| `max_hops` | int | 2 | 跨池最大跳数 |
| `http_rate_limit` | int | 100 | API 限流，0 不限 |
| `api_key` | string | `""` | X-API-Key，空不鉴权 |
| `db_url` | string | `""` | PostgreSQL，空禁用 |
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
| `pools` | []object | 监控池子列表 |
| `auto_discover` | object | Subgraph 自动发现 |

### 池子配置 (`pools[]`)

| 字段 | 类型 | 说明 |
|------|------|------|
| `pool_address` | string | Pool 合约地址 |
| `sync_from_block` | int | 历史起始区块（配合 Snapshot / CatchUp） |

配置中 `{{ENV_VAR}}` 会替换为环境变量值。

## 状态同步策略

| 阶段 | 触发 | 方法 | 说明 |
|------|------|------|------|
| 首次无数据 | 添加池子 | `EnsureInitialState` → `DoFullSync` | 链上 tick bitmap + slot0，**仅执行一次** |
| 已有 DB 快照 | 启动 | `snapshot.Loader` 恢复 | 跳过全量，直接进入增量 |
| CatchUp | 启动后 | `BlockSync.CatchUpFrom(last+1, head)` | eth_getLogs 按区块 replay |
| 实时增量 | newHeads | `BlockSync.Run` → `ProcessBlock` | SubscribeNewHead → eth_getLogs(block) → ApplyBlock |
| 健康检查 | Cron | 对比 slot0 | 仅未初始化时补全量；已初始化只告警，由 BlockSync 修复 |

增量路径（常态）：

```
SubscribeNewHead
      ↓
eth_getLogs(block, block)
      ↓
replay.ApplyBlock()
      ↓
PostgreSQL + LastProcessedBlock
```

全量同步**不会**在健康检查、WS 重连时重复触发；只有 `BlockNumber==0` 或 `tick 地图为空` 时才会执行。

## Subgraph 自动发现

```yaml
chains:
  - name: "ethereum"
    auto_discover:
      enabled: true
      min_tvl_usd: 500000
      min_volume_usd: 10000000
      max_pools: 20
      order_by: "volumeUSD"
```

手动 `pools` 与自动发现池子合并，已存在地址跳过。新增池子后自动更新 BlockProcessor 跟踪列表。

## 数据库迁移

```bash
export DB_URL="postgres://user:pass@localhost:5432/arbitrage?sslmode=disable"
go run ./cmd/migrate/ -db "$DB_URL" up
go run ./cmd/migrate/ -db "$DB_URL" down
```

| 文件 | 说明 |
|------|------|
| `001_init.sql` | `pool_states` 快照 + `pool_states_history` 历史 |
| `002_token_metadata.sql` | 代币 symbol / decimals 缓存 |
| `003_chain_sync_state.sql` | 链级 `last_processed_block`（BlockSync checkpoint） |

启动时 `storage/postgres` 模块自动执行未应用的迁移（幂等安全网）。

## 监控指标

`GET /metrics` 暴露 Prometheus 指标：

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `arbitrage_events_total` | Counter | 事件数 (swap/mint/burn) |
| `arbitrage_ws_reconnects_total` | Counter | WS 重连（BlockSync / 遗留客户端） |
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
    base_tokens:
      - "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
      - "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"

  - name: "arbitrum"
    ws_endpoint: "wss://arb-mainnet.g.alchemy.com/v2/{{KEY}}"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    pools:
      - pool_address: "0xC6962004f452bE9203591991D15f6b388e09E8D0"
```

## 代币元信息缓存

1. **Redis**（1h TTL）
2. **PostgreSQL** `token_metadata`
3. **RPC** ERC20 查询，结果回写 DB + Redis

## 扩展指南

| 目标 | 做法 |
|------|------|
| 新增 DEX 协议 | 实现 `BlockProcessor` + `ProcessorRegistry.Register` |
| 新增链 | 在 `config.yaml` 增加 `chains[]` 条目 |
| 自定义 HTTP | 扩展 `internal/api/routes.go` |
| 套利检测 | 在 `internal/arbitrage/` 扩展逻辑，复用 `pool.Cache` + `router` |

## 许可

MIT
