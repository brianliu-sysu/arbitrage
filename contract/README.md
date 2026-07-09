# Arbitrage Executor Contracts

Foundry subproject for the on-chain leg of discovered arbitrage opportunities.

## Layout

- `src/ArbitrageExecutor.sol`: owner/operator controlled executor.
- `foundry.toml`: Foundry configuration.

## Execution Model

The backend submits an `ExecutionPlan` containing:

- flash loan protocol: Balancer, Uniswap V3, or Uniswap V4 PoolManager flash accounting
- loan token and amount
- ordered swap calls, each with `target`, `calldata`, optional ETH value, and allowance data
- profit token, minimum profit, and recipient

The contract borrows, executes swaps in order, repays the loan plus fee, and reverts if final profit is below `minProfit`.

## Build

```bash
cd contract
forge build
```
