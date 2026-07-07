---
name: arbitrage-architecture
description: >-
  Defines DDD/Clean Architecture conventions, layer boundaries, module layout,
  sync/quote/arbitrage flows, and code-generation rules for this Go arbitrage
  repository. Use when implementing features, refactoring, fixing bugs, adding
  protocols, wiring Fx modules, or making any code change in this project.
---

# Arbitrage Project Architecture

**All code comments must be in English.**

Before any change, read this skill and place code in the correct layer. For full module/file reference, see [reference.md](reference.md).

## Dependency Direction

```
interfaces -> application -> domain
infrastructure -> domain/application interfaces
```

| Layer | Responsibility | Forbidden |
|-------|----------------|-----------|
| **domain** | Models, domain rules, repository interfaces | SQL, Redis, RPC, HTTP, ABI, goroutine orchestration |
| **application** | Use-case orchestration | Direct infra details in business flow |
| **infrastructure** | Technical implementations of interfaces | Domain logic |
| **interfaces** | HTTP/gRPC/CLI I/O adaptation | Domain logic |

`cmd/` and `internal/app/` only bootstrap config, Fx wiring, and lifecycle. No business logic in `main.go`.

## Where to Put Code

| Change type | Location |
|-------------|----------|
| Pool state rules, math, routes | `internal/domain/` |
| Sync/quote/arbitrage workflows | `internal/application/` |
| ETH client, repos, registry, logging | `internal/infrastructure/` |
| HTTP handlers, routers | `internal/interfaces/http/` |
| Fx modules, startup/shutdown | `internal/app/` |
| Config structs/loaders | `internal/config/` |

## Domain Invariants

1. `Pool` is the aggregate root; **`Pool.Apply(event)` is the only entry for market state changes**.
2. `PoolEvent` is a fact from chain logs — it does not save pools or call repositories.
3. Repository **interfaces** live in domain; **implementations** live in infrastructure.
4. `RouteService` finds paths; `QuoteService` prices pools/routes. **Do not make `RouteService` depend on `QuoteService`.** Compose both in application layer.
5. Arbitrage discovery finds/evaluates opportunities — **no tx build/sign/send**.
6. Execution domain describes plans/policies — **no direct RPC/sign/submit**.

## Sync Layer Rules

Sync is **application** orchestration, not domain.

| Service | Role |
|---------|------|
| `SyncOrchestrator` | Startup orchestration only; does not persist pool state |
| `BootstrapService` | Cold start from chain/snapshot |
| `PoolLifecycleService` | Per-pool start/stop/add/remove |
| `CatchupService` | Replay from checkpoint |
| `HeadSyncService` | New-head subscription → block apply |
| `BlockApplyService` | Load pool → `Apply(event)` → save pool/checkpoint |
| `SnapshotService` / `SnapshotScheduler` | Snapshot create/load; block-triggered primary, timer fallback |
| `ReorgRecoveryService` | Detect on new head → rollback → replay |
| `ReadinessService` | System + **pool-level** readiness |

**Triggers:** Reorg detection on new head (not timer-first). Snapshot after `ApplyBlock` (timer only as fallback).

### Multi-protocol pattern (current codebase)

Shared generic code in `internal/application/sync/` (`syncapp` package). Protocol packages are thin wrappers:

- `clv3/` — Uniswap V3 + Pancake V3 (address-based pools)
- `univ3/`, `pancakev3/` — Fx wiring adapters over `clv3/`
- `univ4/` — V4 pool IDs, registry key resolution, extra block-apply filters

When deduplicating across protocols:
1. Extract generic service + hooks into `syncapp`
2. Keep protocol-specific types, checkpoints, logging in wrappers
3. Guard nil fetcher/parser in hook closures (avoid method-value panics in tests)
4. Fx providers need **distinct exported types** per protocol (e.g. separate `AppService` wrappers, not aliases to the same type)

Same pattern applies to:
- `application/quote/clv3` + `univ3`/`pancakev3` wrappers
- `infrastructure/blockchain` CLV3 shared parsers/fetchers
- `infrastructure/persistence/memory` CLV3 shared repos

## Quote Layer Rules

Application quote flow:

```
Handler -> AppService -> PoolRepository + RouteService + QuoteService -> Response
```

- Check readiness before quoting.
- Single-pool vs multi-hop routing handled in `AppService`.
- Combined quotes use `application/quote/combined` + `domain/quote/unified`.

## Change Checklist

Before finishing a change:

- [ ] Code sits in the correct layer; imports respect dependency direction
- [ ] No domain imports from infrastructure/interfaces
- [ ] Pool mutations go through `Pool.Apply(event)` only
- [ ] New persistence behind domain repository interfaces
- [ ] Handlers only adapt I/O; logic stays in application/domain
- [ ] Sync changes preserve reorg/snapshot/readiness semantics
- [ ] Multi-protocol shared code uses hooks/generics, not copy-paste
- [ ] Fx wiring uses unique types for distinct providers
- [ ] Comments in English
- [ ] Run `go test` on touched packages

## Call Chains

**Sync:** `NewHead -> HeadSyncService -> Reorg check -> FetchLogs -> ParseEvents -> ApplyBlock -> Pool.Apply -> Save pool/checkpoint -> optional Snapshot -> arbitrage scan`

**Quote:** `HTTP -> Handler -> QuoteAppService -> Get pool -> FindRoutes -> QuoteRoute -> Response`

## Additional Resources

- Full directory layout, file tables, and original design notes: [reference.md](reference.md)
- Project overview (human-readable): [Readme.md](../../../Readme.md)
