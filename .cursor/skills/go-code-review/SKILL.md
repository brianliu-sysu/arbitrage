---
name: go-code-review
description: >-
  Reviews Go code for simplicity, readability, naming, packages, APIs, errors,
  and concurrency using project Go style principles. Use when reviewing pull
  requests, diffs, local changes, or when the user asks for a code review,
  refactor feedback, or style check.
---

# Go Code Review

Review Go changes against the principles below. For this repository, also apply [arbitrage-architecture](../arbitrage-architecture/SKILL.md) (layer boundaries, domain invariants, sync/quote rules).

## Review Workflow

1. Read the diff end-to-end; note intent before nitpicks.
2. Walk each section below; flag violations with file/line references.
3. Prefer actionable fixes over vague advice.
4. Separate **must fix** (correctness, misuse, leaks, architecture breaks) from **should fix** (style, clarity) from **consider** (optional polish).
5. Acknowledge what is done well when relevant.

## Output Format

```markdown
## Summary
[1–3 sentences: overall assessment]

## Must fix
- `path:line` — issue and suggested fix

## Should fix
- `path:line` — issue and suggested fix

## Consider
- `path:line` — optional improvement

## Checklist
- [ ] Guiding principles
- [ ] Identifiers
- [ ] Comments
- [ ] Package design
- [ ] Project structure
- [ ] API design
- [ ] Error handling
- [ ] Concurrency
- [ ] Project architecture (if applicable)
```

---

## 1. Guiding principles

### 1.1. Simplicity

- Prefer the smallest change that solves the problem.
- Question abstractions that serve one call site, indirection without boundary benefit, and speculative generality.
- **Flag:** unnecessary interfaces, wrapper types, or config knobs with no current consumer.

### 1.2. Readability

- Code should read top-to-bottom; a reader should not need to jump across many files for one behavior.
- Prefer explicit control flow over cleverness.
- **Flag:** dense one-liners, hidden side effects, magic numbers without named constants.

### 1.3. Productivity

- Favor conventions already used in the package/repo over personal style.
- Reuse existing helpers and patterns before adding parallel implementations.
- **Flag:** duplicate logic that an existing shared module already covers; churn that does not advance the stated goal.

---

## 2. Identifiers

### 2.1. Choose identifiers for clarity, not brevity

- Names should answer *what* something is in domain terms.
- **Flag:** `ctx`, `tmp`, `data`, `res` when a domain name (`poolID`, `checkpoint`, `ancestorBlock`) fits.

### 2.2. Identifier length

- Length should match scope and importance: short in tiny scopes, descriptive at package/API boundaries.
- **Flag:** single-letter names outside idiomatic loops (`i`, `r`, `w`) or math (`x`, `y`).

### 2.3. Don't name your variables for their types

- Avoid `userMap`, `poolSlice`, `addrStr` — the type already says that.
- **Flag:** `stringList`, `errorChan`, `configMap`.

### 2.4. Use a consistent naming style

- Follow Go conventions: `MixedCaps`, acronyms consistent (`URL`, `ID`, `HTTP`), no `snake_case` in Go identifiers.
- Match surrounding package style for receivers (`s`, `r`, `svc` — pick what the package already uses).

### 2.5. Use a consistent declaration style

- Prefer one declaration style per file/package (`var (` blocks vs inline `:=`).
- Group related declarations; don't mix styles without reason.

### 2.6. Be a team player

- Don't rename exported symbols or reshuffle APIs without need.
- New names should fit existing vocabulary in the package (`Pool`, `Checkpoint`, `Bootstrap`, not synonyms).

---

## 3. Comments

### 3.1. Comments on variables and constants should describe their contents not their purpose

- Good: `// maximum reorg depth in blocks`
- Bad: `// used by reorg recovery` (purpose belongs on the function/type using it)
- **Flag:** comments that restate the identifier or explain *why it's here* instead of *what it holds*.

### 3.2. Always document public symbols

- Every exported func, type, const, and var needs a doc comment starting with the symbol name.
- Comments must be **English** in this repository.
- **Flag:** missing godoc on new exports; comments that only say "TODO" or repeat the signature.

---

## 4. Package Design

### 4.1. A good package starts with its name

- Package name = what it provides (`syncapp`, `quoteuniv3`, `httpapi`).
- Import path + package name should make call sites read naturally.

### 4.2. Avoid package names like base, common, or util

- **Flag:** new `common`, `util`, `helpers`, `misc` packages — merge into a domain-named package or the caller's package.

### 4.3. Return early rather than nesting deeply

- Guard clauses first; avoid `if { if { if {`.
- **Flag:** else branches that could be early returns; error handling buried inside nested blocks.

### 4.4. Make the zero value useful

- Prefer structs where `var s Service` or `&T{}` is safe without constructor side effects.
- **Flag:** constructors that only zero fields; types that panic if not initialized when zero value could work.

### 4.5. Avoid package level state

- No mutable package globals; inject dependencies via constructors/Fx.
- **Flag:** `var defaultX`, init-time mutable maps, package-level `sync.Once` caches without clear lifecycle.

---

## 5. Project Structure

### 5.1. Consider fewer, larger packages

- Prefer cohesive packages over many tiny files/packages split by mechanical concern.
- **Flag:** one-function packages; fragmentation that forces circular import workarounds.

### 5.2. Keep package main small as small as possible

- `main` / `cmd/*`: flags, wiring, lifecycle only.
- **Flag:** business logic, SQL, HTTP handlers, or domain rules in `main.go`.

---

## 6. API Design

### 6.1. Design APIs that are hard to misuse.

- Use distinct types for distinct concepts (`PoolID` vs `common.Address`).
- Narrow function signatures; avoid boolean trap parameters when an enum or option struct is clearer.
- **Flag:** Fx providers returning the same concrete type for different concepts; functions where argument order is easy to swap.

### 6.2. Design APIs for their default use case

- Defaults should be safe and obvious; options for advanced cases.
- **Flag:** requiring every caller to pass zero values or boilerplate for the common path.

### 6.3. Let functions define the behaviour they requires

- If a function needs context, logger, or clock — take it as a parameter or receiver field, don't reach for globals.
- **Flag:** hidden dependencies via package globals or `init()`.

---

## 7. Error handling

### 7.1. Eliminate error handling by eliminating errors

- Refactor so invalid states are unrepresentable; validate at boundaries.
- Use helpers like `syncapp.GroupEventsByBlock` instead of repeating error-prone boilerplate.
- **Flag:** error branches that exist only because types are too loose.

### 7.2. Only handle an error once

- Wrap with context at the boundary where you handle it: `fmt.Errorf("load pool %s: %w", id, err)`.
- Don't log and return the same error; don't swallow errors silently (`_ = err`).
- **Flag:** duplicate log+return; `return err` without context at application boundaries.

---

## 8. Concurrency

### 8.1. Keep yourself busy or do the work yourself

- Don't spawn goroutines to hide sequential work; parallelize only when it pays off.
- **Flag:** goroutines that immediately block on one channel operation with no overlap.

### 8.2. Leave concurrency to the caller

- Libraries/services should not impose goroutine policy unless that is their job (e.g. head subscriber).
- **Flag:** `go func()` inside low-level helpers where the caller cannot control lifecycle.

### 8.3. Never start a goroutine without knowing when it will stop

- Every goroutine needs a cancel/shutdown path (`context.Context`, `stop` channel, `sync.WaitGroup`, Fx `OnStop`).
- **Flag:** fire-and-forget goroutines; missing wait on shutdown; subscribers without unsubscribe.

---

## Project-Specific Checks (this repo)

When reviewing changes here, also verify:

- [ ] Correct layer (`domain` / `application` / `infrastructure` / `interfaces`)
- [ ] Pool mutations only via `Pool.Apply(event)`
- [ ] Repository interface in domain, implementation in infrastructure
- [ ] Sync: reorg on new head; snapshot after apply; pool-level readiness
- [ ] Multi-protocol shared code via `syncapp` / `clv3` hooks, not duplicated services
- [ ] Distinct Fx provider types per protocol where needed
- [ ] Nil-safe hook closures for optional deps in tests

See [arbitrage-architecture](../arbitrage-architecture/SKILL.md) for details.

## Severity Guide

| Level | Examples |
|-------|----------|
| **Must fix** | Data races, goroutine leaks, wrong layer import, double error handling with log noise, broken Fx wiring, state mutation bypassing `Pool.Apply` |
| **Should fix** | Missing godoc on exports, `util` package, type-named variables, deep nesting, APIs easy to misuse |
| **Consider** | Naming polish, minor simplification, comment wording |
