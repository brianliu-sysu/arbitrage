# Uniswap V3 Multi-Chain Quote Service

基于 Go 的 Uniswap V3 多链实时报价服务。支持同时监控多条链上的多个池子，通过 WebSocket 订阅链上事件实时更新状态，对外提供 HTTP API 报价。

## 特性

- **多链支持** — 一条链一个配置节，独立 RPC 端点 + 池子列表，同时监控 Ethereum / Arbitrum / Base / Optimism 等
- **实时状态** — WebSocket 订阅 Swap / Mint / Burn 事件，秒级价格更新
- **WebSocket 断线重连** — 指数退避自动重连，全量或轻量 RPC 快照快速恢复
- **Tick Bitmap 全量重建** — 直接从链上读取 tick bitmap 重建流动性地图，使用 Multicall3 批量查询
- **锚点区块模式** — 全量同步基于固定区块高度获取一致性快照，缓冲重放避免事件丢失
- **跨池报价** — BFS 路径搜索 + 本地模拟报价，支持多跳 routing
- **动态池子发现** — 通过 Uniswap V3 Factory 自动发现 token 的池子并添加到监控
- **Subgraph 自动发现** — 启动时从 Uniswap V3 Subgraph 拉取 Top N 池子，按 TVL/交易量排序自动添加
- **PostgreSQL 持久化** — 池子状态和代币元信息自动保存，启动时从 DB 恢复
- **Redis 缓存** — 代币元信息（symbol / decimals）Redis 缓存，1 小时 TTL，减少 RPC 调用
- **Multicall3 批量查询** — tick bitmap 和 tick 数据批量获取，O(活跃 tick 数) 成本，单次 RPC 最多 800 条
- **Cron 定时健康检查** — 定时对比链上/内存状态，不一致时自动触发全量修复
- **HTTP API** — Gin 框架，RESTful 接口，支持 API Key 鉴权和令牌桶限流
- **Prometheus 指标** — 事件数 / 重连次数 / 报价次数 / 健康修复次数 / API 耗时 / 实时价格
- **OpenTelemetry 链路追踪** — OTLP 导出到 Jaeger / Tempo
- **结构化日志** — JSON 格式（slog），自动包含文件名和行号
- **{{ENV_VAR}} 模板** — 配置文件中敏感信息通过环境变量注入

## 项目结构

```
arbitrage/
├── cmd/
│   ├── arbitrage/
│   │   └── main.go                         # 主入口：加载配置、初始化多链服务、启动 HTTP
│   └── migrate/
│       └── main.go                         # 数据库迁移 CLI 工具
├── internal/
│   ├── cache/
│   │   └── redis.go                        # Redis 代币元信息缓存（1h TTL）
│   ├── config/
│   │   └── config.go                       # YAML 配置加载，{{VAR}} 模板解析
│   ├── httpapi/
│   │   ├── server.go                       # Gin HTTP 服务器（限流/鉴权/指标中间件）
│   │   ├── routes.go                       # API 处理器 + 请求/响应类型
│   │   └── types.go                        # QuoteProvider 接口定义
│   ├── logx/
│   │   └── logx.go                         # 结构化日志接口（slog 实现）
│   ├── metrics/
│   │   └── metrics.go                      # Prometheus 指标定义
│   ├── pool/
│   │   ├── pool.go                         # PoolState — 池子状态 + tick 流动性地图 + 本地报价
│   │   ├── events.go                       # 事件 ABI 解析（Swap / Mint / Burn）
│   │   └── constants.go                    # TickMin / TickMax 常量
│   ├── service/
│   │   ├── service.go                      # PoolQuoteService — 单池编排（缓冲/同步/报价/持久化）
│   │   ├── multi_pool.go                   # MultiPoolService — 多池管理 + 动态发现
│   │   ├── multichain.go                   # MultiChainService — 多链管理
│   │   └── path_finder.go                  # BFS 跨池路径搜索
│   ├── store/
│   │   ├── store.go                        # Storer 接口 + PoolSnapshot + 自动迁移
│   │   └── postgres.go                     # PostgreSQL 实现（pgx）
│   ├── subgraph/
│   │   └── client.go                       # Uniswap V3 Subgraph 查询客户端
│   ├── subscriber/
│   │   └── subscriber.go                   # WebSocket 订阅 + RPC 查询 + Multicall3 + Factory
│   ├── tracing/
│   │   └── tracing.go                      # OpenTelemetry 初始化
│   └── utils/
│       └── safego.go                       # SafeGo — 带 recover 的 goroutine 启动
├── migrations/
│   ├── 001_init.sql                        # 池子状态表 + 历史记录表
│   └── 002_token_metadata.sql              # 代币元信息缓存表
├── config.yaml                             # 配置文件
├── go.mod / go.sum
└── README.md
```

## 快速开始

### 前提条件

- Go 1.21+
- 以太坊 RPC 端点（WebSocket + HTTP，如 Alchemy / Infura）
- （可选）PostgreSQL（持久化）
- （可选）Redis（代币元信息缓存）
- （可选）Jaeger / Grafana Tempo（链路追踪）

### 配置

```yaml
# config.yaml
http_port: 8080
health_check_interval_sec: 30
log_file: "arbitrage.log"
log_level: "info"                             # debug / info / warn / error，默认 info
max_block_gap_for_full_sync: 100              # 全量同步最大区块间隔，默认 100
max_hops: 2                                   # 跨池报价最大跳数（链级可覆盖）
http_rate_limit: 100                          # API 每秒最大请求数，0 不限，默认 100
api_key: ""                                   # API Key（X-API-Key header），空表示不鉴权
db_url: "postgres://user:pass@localhost:5432/arbitrage?sslmode=disable"
redis_url: "redis://localhost:6379/0"         # Redis 连接串，空则禁用缓存
tracing_endpoint: ""                          # OTLP endpoint，空禁用 tracing

chains:
  - name: "ethereum"
    ws_endpoint: "wss://eth-mainnet.g.alchemy.com/v2/{{ALCHEMY_API_KEY}}"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    multicall_address: ""                     # Multicall3 地址，空使用标准部署地址
    quoter_address: ""                        # QuoterV2 地址，空使用默认地址
    max_hops: 2
    base_tokens:                              # 基础代币白名单（跨池报价中间代币 + 自动发现基础代币）
      - "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"   # WETH
      - "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"   # USDC
      - "0xdAC17F958D2ee523a2206206994597C13D831ec7"   # USDT
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
        sync_from_block: 25393441
```

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

认证：如配置了 `api_key`，需传 `X-API-Key` header。
限流：令牌桶算法，默认 100 req/s，返回 429 时请稍后重试。

### 示例

```bash
# 查询 ethereum 链上所有池子
curl http://localhost:8080/api/v1/ethereum/pools

# 单池报价：1000000 USDC → WETH
curl -X POST http://localhost:8080/api/v1/ethereum/pools/0x8ad599.../quote \
  -H "Content-Type: application/json" \
  -d '{"amountIn":"1000000","tokenIn":"0xA0b869..."}'

# 跨池报价：USDC → WBTC
curl -X POST http://localhost:8080/api/v1/ethereum/quote \
  -H "Content-Type: application/json" \
  -d '{"amountIn":"1000000","tokenIn":"0xA0b869...","tokenOut":"0x2260FAC..."}'
```

响应示例：

```json
{
  "amountIn": "1000000",
  "amountOut": "1581234567",
  "chain": "ethereum",
  "tokenIn": "0xA0b869...",
  "tokenOut": "0xC02aaA...",
  "hops": 1,
  "path": [
    {"pool": "0x8ad599...", "tokenIn": "0xA0b869...", "tokenOut": "0xC02aaA..."}
  ]
}
```

## 配置说明

### 全局配置

| 字段 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `http_port` | int | 8080 | HTTP API 监听端口，0 禁用 |
| `health_check_interval_sec` | int | 30 | 健康检查间隔，0 禁用 |
| `log_file` | string | `""` | 日志文件路径，空输出到 stderr |
| `log_level` | string | `info` | 日志级别：debug / info / warn / error |
| `max_block_gap_for_full_sync` | int | 100 | 全量同步最大区块间隔 |
| `max_hops` | int | 2 | 跨池报价最大跳数（链级 max_hops=0 时使用此值） |
| `http_rate_limit` | int | 100 | API 每秒最大请求数，0 不限 |
| `api_key` | string | `""` | API Key（X-API-Key header），空不鉴权 |
| `db_url` | string | `""` | PostgreSQL 连接串，空禁用持久化 |
| `redis_url` | string | `""` | Redis 连接串，空禁用代币元信息缓存 |
| `tracing_endpoint` | string | `""` | OTLP endpoint，空禁用 tracing |

### 链级配置 (`chains[]`)

| 字段 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `name` | string | 必填 | 链标识，用于 HTTP 路由 |
| `ws_endpoint` | string | 必填 | WebSocket RPC（事件订阅） |
| `rpc_endpoint` | string | 从 ws 推导 | HTTP RPC（eth_call），空则 wss→https / ws→http |
| `rpc_failover` | []string | `[]` | RPC 故障转移列表 |
| `factory_address` | string | 必填 | Uniswap V3 Factory 地址（0x 格式） |
| `multicall_address` | string | `0xcA11...CA11` | Multicall3 合约地址，空使用标准部署地址 |
| `quoter_address` | string | `0x61fF...B21e` | QuoterV2 合约地址，空使用默认地址 |
| `base_tokens` | []string | 必填 | 基础代币白名单（跨池报价中间代币 + 自动发现基础代币） |
| `max_hops` | int | 全局值 | 跨池报价跳数，0 使用全局 max_hops |
| `pools` | []object | 必填 | 该链的池子列表 |
| `auto_discover` | object | - | Subgraph 自动发现配置 |

### 池子配置 (`pools[]`)

| 字段 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `pool_address` | string | 必填 | Uniswap V3 Pool 地址（0x 格式） |
| `sync_from_block` | int | 0 | 历史同步起始区块，0 跳过历史同步 |

### `{{ENV_VAR}}` 模板

配置中任何 `{{变量名}}` 会被替换为对应环境变量的值。缺失的变量保留原文本不报错。

```yaml
ws_endpoint: "wss://eth-mainnet.g.alchemy.com/v2/{{ALCHEMY_API_KEY}}"
```

## Subgraph 自动发现

配置 `auto_discover` 后，启动时自动从 Uniswap V3 Subgraph 查询热门池子并添加到监控：

```yaml
chains:
  - name: "ethereum"
    auto_discover:
      enabled: true
      subgraph_url: "https://api.thegraph.com/subgraphs/name/uniswap/uniswap-v3"  # 可选
      min_tvl_usd: 500000       # 最低 TVL $500K
      min_volume_usd: 10000000  # 最低 24h 交易量 $10M
      max_pools: 20             # 最多 20 个池子
      order_by: "volumeUSD"     # 按交易量排序
```

| 字段 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `enabled` | bool | false | 是否启用 |
| `subgraph_url` | string | Uniswap 官方 | 子图 API 端点 |
| `min_tvl_usd` | int | 500,000 | 最低 TVL（美元） |
| `min_volume_usd` | int | 10,000,000 | 最低 24h 交易量（美元） |
| `max_pools` | int | 20 | 最多添加池子数 |
| `order_by` | string | volumeUSD | 排序方式: volumeUSD / totalValueLockedUSD / txCount |

内置过滤条件：`liquidity_gt: "0"`（排除零流动性池子）。

手动配置的 `pools` 与自动发现的池子**合并**，已存在的池子不会被覆盖。

所有主流链都有对应的子图：

| 链 | Subgraph URL |
|------|-------------|
| Ethereum | `https://api.thegraph.com/subgraphs/name/uniswap/uniswap-v3` |
| Arbitrum | `https://api.thegraph.com/subgraphs/name/ianlapham/arbitrum-minimal` |
| Optimism | `https://api.thegraph.com/subgraphs/name/ianlapham/optimism-post-regenesis` |
| Polygon | `https://api.thegraph.com/subgraphs/name/ianlapham/uniswap-v3-polygon` |
| Base | `https://api.studio.thegraph.com/query/48211/uniswap-v3-base/version/latest` |
| Celo | `https://api.thegraph.com/subgraphs/name/jesse-sawa/uniswap-celo` |
| BSC | `https://api.thegraph.com/subgraphs/name/ianlapham/uniswap-v3-bsc` |
| Avalanche | `https://api.thegraph.com/subgraphs/name/lynnshaoyu/uniswap-v3-avax` |

## 监控指标

所有指标通过 `GET /metrics` 暴露（Prometheus 格式）。

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `arbitrage_events_total` | Counter | `pool`, `event` | 收到的事件数 (swap/mint/burn) |
| `arbitrage_ws_reconnects_total` | Counter | `pool` | WebSocket 重连次数 |
| `arbitrage_quotes_total` | Counter | `pool`, `method` | 报价请求数 (local/rpc_simulate) |
| `arbitrage_health_repairs_total` | Counter | `pool` | 健康检查修复次数 |
| `arbitrage_price` | Gauge | `pool` | 现货价格（已调整小数位） |
| `arbitrage_block_number` | Gauge | `pool` | 当前区块高度 |
| `arbitrage_event_latency_seconds` | Histogram | `pool`, `event` | 事件处理延迟 |
| `arbitrage_http_request_duration_seconds` | Histogram | `method`, `path`, `status` | API 请求耗时 |

## 架构概览

```
MultiChainService
├── chain["ethereum"] → MultiPoolService
│   ├── PoolQuoteService(0x8ad5...)  ← WebSocket → RPC
│   ├── PoolQuoteService(0x88e6...)
│   └── PathFinder (BFS routing)
└── chain["arbitrum"] → MultiPoolService
    ├── PoolQuoteService(0xC696...)
    └── PathFinder

                    ┌─ Gin HTTP API ──┐
                    │  /api/v1/:chain │
                    │  + 限流 + 鉴权   │
                    └────────────────┘

                    ┌─ 持久化 ────────┐
                    │  PostgreSQL     │ → 池子状态 + 历史
                    │  Redis          │ → Token 元信息缓存
                    └────────────────┘
```

每条链独立拥有自己的 WebSocket 连接、事件订阅、健康检查定时任务、持久 RPC 客户端连接池。

## 状态同步策略

| 阶段 | 触发条件 | 方法 | RPC 成本 |
|------|---------|------|------|
| 冷启动 | 首次启动 / DB 无数据 | `DoFullSync` — 锚点区块一致性快照 + Tick Bitmap 全量重建 | ~15 次 |
| 热启动 | DB 有数据 | `LoadFromStore` 恢复状态 + `ResolvePoolMetadata` 获取元信息 | ~2 次 |
| WebSocket 重连 | 断线后自动重连 | `DoLightSync`（距上次全量<5min）或 `DoFullSync`（>5min） | 1~15 次 |
| 健康检查 | cron 定时触发 | 对比 sqrtPriceX96 / tick / liquidity → 不一致则 `DoFullSync` | 1~15 次 |
| Swap 事件 | WebSocket 实时推送 | `OnSwap` → 直接更新内存状态 | 0 |
| Mint/Burn 事件 | WebSocket 实时推送 | `UpdateTickFromMint` / `UpdateTickFromBurn` → 更新 tick 流动性地图 | 0 |

### 缓冲重放机制

全量同步期间 WebSocket 事件进入缓冲区，同步完成后按区块高度过滤回放，确保不丢失、不重复。

```
bufferingMode=true → 事件入 eventBuffer
       │
       ▼
  DoFullSync (锚点区块快照)
       │
       ▼
  drainAndReplay (回放 ≥ snapshotStartBlock 的事件)
       │
       ▼
bufferingMode=false → 恢复实时模式
```

## 多链示例

```yaml
chains:
  - name: "ethereum"
    ws_endpoint: "wss://eth-mainnet.g.alchemy.com/v2/{{KEY}}"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    base_tokens:
      - "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"   # WETH
      - "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"   # USDC
      - "0xdAC17F958D2ee523a2206206994597C13D831ec7"   # USDT
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"

  - name: "arbitrum"
    ws_endpoint: "wss://arb-mainnet.g.alchemy.com/v2/{{KEY}}"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    base_tokens:
      - "0x82aF49447D8a07e3bd95BD0d56f35241523fBab1"   # WETH
      - "0xaf88d065e77c8cC2239327C5EDb3A432268e5831"   # USDC
      - "0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9"   # USDT
    pools:
      - pool_address: "0xC6962004f452bE9203591991D15f6b388e09E8D0"

  - name: "base"
    ws_endpoint: "wss://base-mainnet.g.alchemy.com/v2/{{KEY}}"
    factory_address: "0x33128a8fC17869897dcE68Ed026d694621f6FDfD"
    base_tokens:
      - "0x4200000000000000000000000000000000000006"   # WETH
      - "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"   # USDC
    pools:
      - pool_address: "0xd0b53D9277642d899DF5C87A3966A349A798F224"
```

## 数据库迁移

使用内置的迁移工具管理数据库 schema：

```bash
# 执行迁移
export DB_URL="postgres://user:pass@localhost:5432/arbitrage?sslmode=disable"
go run ./cmd/migrate/ -db "$DB_URL" up

# 回滚迁移
go run ./cmd/migrate/ -db "$DB_URL" down
```

迁移文件放在 `migrations/` 目录，使用 goose 格式的 SQL 文件：

| 文件 | 说明 |
|------|------|
| `001_init.sql` | 池子状态快照表（upsert）+ 价格历史时序表 |
| `002_token_metadata.sql` | 代币元信息缓存表（chain + token_address 联合主键） |

启动时 `RunMigrations` 自动确保基础表存在（安全网，幂等执行），正式部署应使用 `migrate` 命令。

## 代币元信息缓存

代币的 symbol 和 decimals 通过三级缓存获取：

1. **Redis**（1 小时 TTL）— 最快，命中后直接返回
2. **PostgreSQL** — Redis 未命中时查询 DB，命中后异步回写 Redis
3. **RPC 链上查询** — DB 也未命中时通过 ERC20 接口查询，结果同时写入 DB 和 Redis

该机制避免每次启动对同一代币重复发起 RPC 查询，大幅降低 RPC 调用量。

## 许可

MIT
