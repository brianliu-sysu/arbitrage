# Uniswap V3 Multi-Chain Quote Service

基于 Go 的 Uniswap V3 多链实时报价服务。支持同时监控多条链上的多个池子，通过 WebSocket 订阅链上事件实时更新状态，对外提供 HTTP API 报价。

## 特性

- **多链支持** — 一条链一个配置节，独立 RPC 端点 + 池子列表，同时监控 Ethereum / Arbitrum / Base / Optimism 等
- **实时状态** — WebSocket 订阅 Swap / Mint / Burn 事件，秒级价格更新
- **WebSocket 断线重连** — 指数退避自动重连，轻量 RPC 快照快速恢复
- **Tick Bitmap 全量重建** — 直接从链上读取 tick bitmap 重建流动性地图，O(活跃 tick 数) 成本
- **跨池报价** — BFS 路径搜索 + eth_call 链上模拟，支持多跳 routing
- **动态池子发现** — 通过 Uniswap V3 Factory 自动发现 token 的池子并添加到监控
- **Subgraph 自动发现** — 启动时从 Uniswap V3 Subgraph 拉取 Top N 池子，按 TVL/交易量排序自动添加
- **PostgreSQL 持久化** — Swap 事件自动保存，启动时从 DB 恢复，gap 小则跳过 tick 重建
- **HTTP API** — Gin 框架，RESTful 接口，按链分组路由
- **Prometheus 指标** — 事件数 / 重连次数 / 报价次数 / 健康修复次数 / API 耗时 / 实时价格
- **OpenTelemetry 链路追踪** — OTLP 导出到 Jaeger / Tempo
- **结构化日志** — JSON 格式，自动包含文件名和行号
- **{{ENV_VAR}} 模板** — 配置文件中敏感信息通过环境变量注入

## 项目结构

```
arbitrage/
├── cmd/
│   ├── arbitrage/
│   │   └── main.go
│   └── migrate/
│       └── main.go                        # 入口，多链启动，信号处理
├── internal/
│   ├── config/
│   │   └── config.go                  # YAML 配置加载，{{VAR}} 模板解析
│   ├── httpapi/
│   │   ├── server.go                  # Gin HTTP 服务器，路由注册
│   │   ├── routes.go                  # API 处理器 + 请求/响应类型
│   │   └── types.go                   # QuoteProvider 接口定义
│   ├── logx/
│   │   └── logx.go                    # 结构化日志接口（slog 实现）
│   ├── metrics/
│   │   └── metrics.go                 # Prometheus 指标定义
│   ├── pool/
│   │   ├── pool.go                    # PoolState — 池子状态 + tick 流动性地图
│   │   ├── events.go                  # 事件 ABI 解析（Swap / Mint / Burn）
│   │   └── constants.go               # MinSqrtRatio / MaxSqrtRatio
│   ├── service/
│   │   ├── service.go                 # PoolQuoteService — 单池编排
│   │   ├── multi_pool.go              # MultiPoolService — 多池管理 + 动态发现
│   │   ├── multichain.go              # MultiChainService — 多链管理
│   │   └── path_finder.go             # BFS 跨池路径搜索
│   ├── store/
│   │   ├── store.go                   # Storer 接口 + PoolSnapshot
│   │   └── postgres.go                # PostgreSQL 实现（pgx）
│   ├── subscriber/
│   │   └── subscriber.go              # WebSocket 订阅 + RPC 查询 + Factory 交互
│   └── tracing/
│       └── tracing.go                 # OpenTelemetry 初始化
├── config.yaml                         # 配置文件
├── go.mod / go.sum
└── README.md
```

## 快速开始

### 前提条件

- Go 1.21+
- 以太坊 RPC 端点（WebSocket + HTTP，如 Alchemy / Infura）
- (可选) PostgreSQL（持久化）
- (可选) Jaeger / Grafana Tempo（链路追踪）

### 配置

```yaml
# config.yaml
http_port: 8080
health_check_interval_sec: 30
log_file: "arbitrage.log"
max_block_gap_for_full_sync: 1000
max_hops: 2
db_url: "postgres://user:pass@localhost:5432/arbitrage?sslmode=disable"
tracing_endpoint: ""

chains:
  - name: "ethereum"
    ws_endpoint: "wss://eth-mainnet.g.alchemy.com/v2/{{ALCHEMY_API_KEY}}"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    max_hops: 2
    bridge_tokens: ["0xA0b869...", "0xC02aaA...", "0xdAC17F..."]
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
| `log_file` | string | `""` | 日志文件，空为 stderr |
| `max_block_gap_for_full_sync` | int | 1000 | 启动时 gap 超过此值才做全量 tick 重建 |
| `max_hops` | int | 2 | 跨池报价最大跳数（链级可覆盖） |
| `db_url` | string | `""` | PostgreSQL 连接串，空禁用持久化 |
| `tracing_endpoint` | string | `""` | OTLP endpoint，空禁用 tracing |

### 链级配置 (`chains[]`)

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | string | 链标识，用于 HTTP 路由 |
| `ws_endpoint` | string | WebSocket RPC（事件订阅） |
| `rpc_endpoint` | string | HTTP RPC（eth_call），空则从 ws 推导 |
| `factory_address` | string | Uniswap V3 Factory 地址 |
| `weth` / `usdc` / `usdt` | string | 基础代币地址（用于动态池子发现） |
| `max_hops` | int | 跨池报价跳数，0 用全局值 |
| `bridge_tokens` | []string | 跨池报价中间代币白名单 |
| `pools` | []object | 该链的池子列表 |
| `auto_discover` | object | Subgraph 自动发现配置（enabled/subgraph_url/min_tvl_usd/max_pools/order_by） |

### `{{ENV_VAR}}` 模板

配置中任何 `{{变量名}}` 会被替换为对应环境变量的值。缺失的变量不会报错，保留原文本。

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

额外的内置过滤条件：`liquidity_gt: "0"`（排除零流动性池子）。

手动配置的 `pools` 与自动发现的池子**合并**，已存在的手动池子不会被覆盖。
标准池子发现和全过程同步适合全部池子，全量同步会被串行化以避免 RPC 限流。

所有主流 L2 都有对应的子图：

| 链 | Subgraph URL |
|------|-------------|
| Ethereum | `https://api.thegraph.com/subgraphs/name/uniswap/uniswap-v3` |
| Arbitrum | `https://api.thegraph.com/subgraphs/name/ianlapham/arbitrum-minimal` |
| Optimism | `https://api.thegraph.com/subgraphs/name/ianlapham/optimism-post-regenesis` |
| Polygon | `https://api.thegraph.com/subgraphs/name/ianlapham/uniswap-v3-polygon` |
| Base | `https://api.studio.thegraph.com/query/48211/uniswap-v3-base/version/latest` |


## 监控指标

所有指标通过 `GET /metrics` 暴露（Prometheus 格式）。

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `arbitrage_events_total` | Counter | `pool`, `event` | 收到的事件数 (swap/mint/burn) |
| `arbitrage_ws_reconnects_total` | Counter | `pool` | WebSocket 重连次数 |
| `arbitrage_quotes_total` | Counter | `pool`, `method` | 报价请求数 |
| `arbitrage_health_repairs_total` | Counter | `pool` | 健康检查修复次数 |
| `arbitrage_price` | Gauge | `pool` | 现货价格（已调整小数位） |
| `arbitrage_block_number` | Gauge | `pool` | 当前区块高度 |
| `arbitrage_http_request_duration_seconds` | Histogram | `method`, `path`, `status` | API 请求耗时 |

## 架构概览

```
MultiChainService
├── chain["ethereum"] → MultiPoolService
│   ├── PoolQuoteService(0x8ad5...)  ← WebSocket → Alchemy
│   ├── PoolQuoteService(0x88e6...)
│   └── PathFinder (BFS routing)
└── chain["arbitrum"] → MultiPoolService
    ├── PoolQuoteService(0xC696...)
    └── PathFinder

                    ┌─ Gin HTTP API ──┐
                    │  /api/v1/:chain │
                    └────────────────┘
```

每条链独立拥有自己的 WebSocket 连接、事件订阅、健康检查、RPC 客户端池。

## 状态同步策略

| 阶段 | 触发条件 | 方法 | 成本 |
|------|---------|------|------|
| 冷启动 | 首次启动 / DB 无数据 | `DoFullSync` — Tick Bitmap 全量重建 | ~15 次 RPC |
| 热启动 | DB 有数据，gap ≤ 阈值 | 仅 RPC 快照（slot0 + liquidity） | 1 次 RPC |
| 热启动 | DB 有数据，gap > 阈值 | `DoFullSync` — Tick Bitmap 重建 | ~15 次 RPC |
| WebSocket 重连 | 断线后自动重连 | `DoLightSync` — RPC 快照 | 1 次 RPC |
| 距上次全量 > 5min | WS 重连 + 超时 | `DoFullSync` — Tick Bitmap 重建 | ~15 次 RPC |
| 健康检查 | 每 N 秒 | 对比 sqrtPriceX96 / tick / liquidity → 不一致则 `DoFullSync` | 1 次 RPC |
| Swap 事件 | 实时推送 | `OnSwap` → 直接更新状态 | 0 RPC |

## 多链示例

```yaml
chains:
  - name: "ethereum"
    ws_endpoint: "wss://eth-mainnet.g.alchemy.com/v2/{{KEY}}"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
    usdc: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
    usdt: "0xdAC17F958D2ee523a2206206994597C13D831ec7"
    pools:
      - pool_address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"

  - name: "arbitrum"
    ws_endpoint: "wss://arb-mainnet.g.alchemy.com/v2/{{KEY}}"
    factory_address: "0x1F98431c8aD98523631AE4a59f267346ea31F984"
    weth: "0x82aF49447D8a07e3bd95BD0d56f35241523fBab1"
    usdc: "0xaf88d065e77c8cC2239327C5EDb3A432268e5831"
    usdt: "0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9"
    pools:
      - pool_address: "0xC6962004f452bE9203591991D15f6b388e09E8D0"

  - name: "base"
    ws_endpoint: "wss://base-mainnet.g.alchemy.com/v2/{{KEY}}"
    factory_address: "0x33128a8fC17869897dcE68Ed026d694621f6FDfD"
    weth: "0x4200000000000000000000000000000000000006"
    usdc: "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
    usdt: "0x0000000000000000000000000000000000000000"  # Base 无原生 USDT
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

# 或使用 Makefile
make migrate-up DB_URL="$DB_URL"
```

迁移文件放在 `migrations/` 目录，使用 goose 格式的 SQL 文件：

| 文件 | 说明 |
|------|------|
| `001_create_pool_states.sql` | 池子状态快照表（upsert） |
| `002_create_pool_states_history.sql` | 价格历史时序表 |

迁移工具会自动创建 `goose_db_version` 表追踪已应用的迁移，幂等执行。


## 许可

MIT
