# Uniswap V3 Arbitrage System

基于 Go 的链上 DEX 池子同步、本地报价与三角套利发现系统。支持 **Uniswap V3**、**PancakeSwap V3**、**Uniswap V4**，在本地维护池子状态（slot0、liquidity、tick、bitmap），对外提供 HTTP 报价与套利机会查询。

> 架构分层、目录约定与代码生成规则见 Cursor skill：`.cursor/skills/arbitrage-architecture/`

## 功能概览

### 池子同步

- 订阅以太坊新区块（WebSocket），实时应用 Swap / Mint / Burn / Initialize 等事件
- 冷启动时从链上读取 bootstrap 数据，或从 snapshot 恢复
- Catch-up 回放历史区块，批量拉取日志并追平进度
- 链重组（reorg）检测与自动恢复：回滚 snapshot、重放日志
- 按区块间隔创建 snapshot，定时任务兜底
- Pool 级 readiness：仅当池子同步就绪后才参与报价

### 多协议支持

| 协议 | 配置项 | 说明 |
|------|--------|------|
| Uniswap V3 | `sync.univ3` | 按 pool 地址跟踪 |
| PancakeSwap V3 | `sync.pancakev3` | 按 pool 地址跟踪 |
| Uniswap V4 | `sync.univ4` | 按 PoolId 跟踪，需 PoolManager / StateView |

各协议可独立启用。池子来源支持配置文件静态列表 + Subgraph 动态发现（按 TVL、24h 成交量等筛选）。

### 报价（Quote）

- **单协议报价**：在已同步的池子状态上计算 exact-input / exact-output
- **多跳路由**：在跟踪池子构成的 token 图上搜索路径（默认最多 3 hop），返回最优路径及候选路径报价
- **跨协议报价**：`POST /api/v1/quote` 可跨 Uniswap V3、Pancake V3、Uniswap V4 选路

报价依赖本地缓存的池子状态，不直接调用链上 swap 模拟。

### 三角套利发现

- 每个区块同步完成后，根据变更池子扫描受影响路径
- 支持配置起始 token（如 USDC、WETH），搜索 A→B→C→A 闭环
- 评估毛利润、gas 成本与净利润，过滤低于阈值的机会
- 机会持久化后可通过 HTTP 查询（**仅发现，不自动发交易**）

### 池子诊断

- 列出当前跟踪的全部池子及元数据
- 对比本地状态与链上读数，辅助排查同步偏差（支持按协议、地址/PoolId 查询）

## 快速开始

### 依赖

- Go 1.22+
- 以太坊 RPC（HTTP + WebSocket）
- PostgreSQL（生产模式）或内存模式（开发）

### 构建与运行

```bash
# 构建
make build

# 运行（默认读取 configs/config.yaml）
make run

# 或指定配置
./bin/arbitrage -config config.yaml
```

### 数据库迁移

```bash
make migrate
```

### 测试

```bash
make test
```

## 配置说明

主配置文件：`config.yaml`（或通过 `-config` 指定）。

| 区块 | 作用 |
|------|------|
| `rpc` | HTTP / WebSocket 节点地址 |
| `persistence` | `memory: true` 使用内存；否则连接 Postgres / Redis |
| `blockchain` | Factory、Multicall、V4 PoolManager / StateView 合约地址 |
| `sync` | 各协议启用开关、池子列表、Subgraph、catch-up / snapshot / reorg 参数 |
| `http` | API 监听地址 |
| `quote` | 最大跳数 `max_hops` |
| `arbitrage.triangle` | 三角套利开关、起始 token、最小净利润、优化器参数 |
| `log` | 日志级别、格式、文件路径 |

示例见仓库内 `config.yaml`。

## HTTP API

默认地址：`http://localhost:8080`（由 `http.addr` 配置）。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health`, `/api/v1/health` | 健康检查 |
| POST | `/api/v1/quote` | 跨协议报价（V3 + Pancake V3 + V4） |
| POST | `/api/v1/univ3/quote` | Uniswap V3 报价 |
| POST | `/api/v1/pancakev3/quote` | PancakeSwap V3 报价 |
| POST | `/api/v1/univ4/quote` | Uniswap V4 报价 |
| GET | `/api/v1/pools` | 列出跟踪池子 |
| GET | `/api/v1/pools/diagnostics` | 池子同步诊断（支持 query 参数筛选） |
| GET | `/api/v1/opportunities` | 查询已发现的套利机会（`limit` 参数） |

### 报价请求示例

```bash
curl -X POST http://localhost:8080/api/v1/univ3/quote \
  -H 'Content-Type: application/json' \
  -d '{
    "tokenIn": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
    "tokenOut": "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2",
    "amountIn": "1000000"
  }'
```

可选字段：`amountOut`（exact-output）、`poolAddress` / `poolId`（指定单池报价）。

## 项目结构（简览）

```
cmd/arbitrage/     主程序入口
cmd/migrate/       数据库迁移
internal/
  domain/          领域模型与规则
  application/     同步、报价、套利用例
  infrastructure/  链上读取、持久化、注册表
  interfaces/http/ REST API
  app/             Fx 依赖装配
migrations/        SQL 迁移
configs/           配置模板
```

完整模块说明见 `.cursor/skills/arbitrage-architecture/reference.md`。

## 开发说明

- 代码注释使用英文
- 架构与分层约束由 `arbitrage-architecture` skill 维护，修改代码前请遵循该 skill
- 构建产物输出到 `bin/`，已加入 `.gitignore`
