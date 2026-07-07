# Arbitrage Architecture Reference

Detailed layout derived from `Readme.md`, aligned with the current repository.

## Top-Level Layout

```
arbitrage/
├── cmd/arbitrage/main.go      # config + Fx + lifecycle only
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

## internal/domain/

### domain/asset/
Token metadata: `token.go`, `token_repository.go`

### domain/blockchain/
Chain primitives: `block.go`, `block_header.go`, `block_range.go`, `checkpoint.go`, `chain.go`, `reorg.go`, `checkpoint_repository.go`. No RPC here.

### domain/market/
Core market model. Shared + protocol packages:

- Shared: `pool_state.go`, `pool_status.go`, `tick.go`, `tick_table.go`, `tick_bitmap.go`, `liquidity.go`, ...
- `clv3/` — concentrated-liquidity pool model
- `univ3/`, `pancakev3/` — protocol-specific pool wrappers
- `univ4/` — V4 pool ID/key model

Key rules:
- `Pool` aggregate root; `Pool.Apply(event)` mutates state
- `PoolEvent` is immutable chain fact
- `pool_repository.go`, `snapshot_repository.go`, `registry.go` — interfaces only

### domain/quote/
- `shared/` — quote modes, shared result types
- `clv3/` — CLV3 math, routes, quote engine
- `univ3/`, `pancakev3/`, `univ4/` — protocol quote services
- `unified/` — cross-protocol quote facade

Math files (`swap_math.go`, `sqrt_price_math.go`, etc.) are pure computation.

### domain/arbitrage/
Opportunity discovery: `opportunity.go`, `strategy.go`, `evaluator.go`, `optimizer.go`, `gas_estimator.go`, `dependency_graph.go`, `opportunity_repository.go`. No tx execution.

### domain/execution/
Execution models only: plans, transactions, bundles, nonce, gas/slippage policies. No RPC/sign/send.

## internal/application/

### application/pools/
Pool listing and on-chain diagnostics (`clv3_pools.go`, `clv3_adapters.go` for shared CLV3 logic).

### application/quote/
- `clv3/` — shared CLV3 quote AppService
- `univ3/`, `pancakev3/` — thin wrappers with **distinct** `AppService` types for Fx
- `univ4/` — V4 quote AppService
- `combined/` — unified multi-protocol quoting
- `quote_request.go` — shared request aliases

### application/sync/
Generic (`syncapp` package root):
- `orchestrator.go`, `snapshot_service.go`, `snapshot_scheduler.go`, `pool_lifecycle.go`
- `catchup_service.go`, `head_sync_service.go`
- `bootstrap_service.go`, `block_apply_service.go`, `reorg_recovery_service.go`
- `readiness.go`, `reorg_detect.go`, `reorg_restore.go`, helpers

Protocol wrappers:
- `clv3/` — CLV3 sync services (used by univ3 + pancakev3)
- `univ3/`, `pancakev3/` — Fx adapter packages
- `univ4/` — V4-specific hooks (tracked pool filter, registry key bootstrap)

### application/arbitrage/
Scan, opportunity generation, publish to listeners/HTTP/execution.

### application/execution/
Build, simulate, sign, submit, nonce/retry policies.

### application/asset/
Token metadata use cases.

## internal/infrastructure/

### infrastructure/blockchain/
`eth_client.go`, `log_fetcher.go`, `head_subscriber.go`, `multicall.go`, `abi_parser.go`, `clv3_pool_parser.go`, `abi_helpers.go`, pool readers, V4 fetchers/parsers.

### infrastructure/persistence/
- `postgres/` — production repos
- `memory/` — in-memory repos; `clv3_pool_repository.go`, `clv3_snapshot_repository.go` shared by univ3/pancake

### infrastructure/registry/
`config_registry.go`, `postgres_registry.go`, `subgraph_registry.go`, composite registries per protocol.

### infrastructure/logging/
Zap logger setup.

### infrastructure/cache/, infrastructure/execution/
As described in Readme (Redis cache, Flashbots/builder/RPC senders).

## internal/interfaces/

### interfaces/http/
`router.go`, quote handlers (combined/v3/pancake/v4), opportunities, pools, health. Parse request → call AppService → JSON.

### interfaces/grpc/, interfaces/cli/
gRPC and CLI entry points per Readme.

## internal/config/
`config.go` — RPC, DB, pools, snapshot, gas, logging.

## internal/app/
`arbitrage/` — Fx module: wires sync, quote, HTTP, arbitrage lifecycle.

## migrations/, configs/, scripts/
SQL migrations, `config.yaml`, dev/ops scripts.

## Code Generation Rules (from Readme)

1. domain — models, rules, interfaces only
2. application — use-case orchestration only
3. infrastructure — external tech implementations only
4. interfaces — I/O adaptation only
5. `Pool.Apply(event)` is the sole market state mutation entry
6. `PoolEvent` does not call repositories or save pools
7. Repository interfaces in domain; implementations in infrastructure
8. `SyncOrchestrator` orchestrates; does not own pool state
9. Readiness supports pool-level status
10. Snapshot primary trigger after ApplyBlock; timer is fallback
11. Reorg primary trigger in NewHead flow; timer is fallback
12. Arbitrage discovery does not send transactions
13. Execution engine does not discover opportunities

## Sync Flow (detailed)

```
NewHead
  -> HeadSyncService
  -> Reorg check
  -> LogFetcher.FetchLogs
  -> ABIParser.ParsePoolEvents
  -> BlockApplyService.ApplyBlock
  -> PoolRepository.Get
  -> Pool.Apply(event)
  -> PoolRepository.Save
  -> CheckpointRepository.Save
  -> SnapshotPolicy (if needed)
  -> Arbitrage scan
```

## Quote Flow (detailed)

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
