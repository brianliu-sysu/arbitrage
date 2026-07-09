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

## Setup

After cloning this repository, install Foundry libraries declared in `.gitmodules`:

```bash
cd contract
forge install
```

## Build

```bash
cd contract
forge build
```

## Deploy

Set `PRIVATE_KEY` for the deployer. `ARBITRAGE_EXECUTOR_OWNER` is optional and defaults to the deployer address.

```bash
cd contract
PRIVATE_KEY=0x... forge script script/DeployArbitrageExecutor.s.sol:DeployArbitrageExecutor \
  --rpc-url "$RPC_URL" \
  --broadcast \
  --verify
```
