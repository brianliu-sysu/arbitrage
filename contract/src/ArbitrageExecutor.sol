// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import { Ownable } from "@openzeppelin/contracts/access/Ownable.sol";
import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { SafeERC20 } from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import { ReentrancyGuard } from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import {
    IERC20 as BalancerIERC20
} from "balancer-v2-monorepo/pkg/interfaces/contracts/solidity-utils/openzeppelin/IERC20.sol";
import { IFlashLoanRecipient } from "balancer-v2-monorepo/pkg/interfaces/contracts/vault/IFlashLoanRecipient.sol";
import { IVault } from "balancer-v2-monorepo/pkg/interfaces/contracts/vault/IVault.sol";
import { IUniswapV3Pool } from "v3-core/contracts/interfaces/IUniswapV3Pool.sol";
import { IUniswapV3FlashCallback } from "v3-core/contracts/interfaces/callback/IUniswapV3FlashCallback.sol";
import { IPoolManager } from "v4-core/src/interfaces/IPoolManager.sol";
import { IUnlockCallback } from "v4-core/src/interfaces/callback/IUnlockCallback.sol";
import { Currency } from "v4-core/src/types/Currency.sol";

/// @notice Executes a discovered arbitrage atomically with flash liquidity.
/// @dev Swap calls are intentionally generic so the off-chain searcher can route through V3/V4/Balancer routers.
contract ArbitrageExecutor is Ownable, ReentrancyGuard, IFlashLoanRecipient, IUniswapV3FlashCallback, IUnlockCallback {
    using SafeERC20 for IERC20;

    enum FlashLoanProtocol {
        Balancer,
        UniswapV3,
        UniswapV4
    }

    struct FlashLoan {
        FlashLoanProtocol protocol;
        address lender;
        address token;
        uint256 amount;
        bool borrowToken0;
    }

    struct SwapCall {
        address target;
        address allowanceTarget;
        address tokenIn;
        uint256 amountIn;
        uint256 value;
        bytes data;
    }

    struct ExecutionPlan {
        FlashLoan loan;
        SwapCall[] swaps;
        address profitToken;
        uint256 minProfit;
        address recipient;
        uint256 deadline;
    }

    mapping(address => bool) public operators;

    uint256 private callbackProfit;

    error NotOperator();
    error DeadlineExpired();
    error InvalidAddress();
    error InvalidCallback();
    error InvalidFlashLoan();
    error SwapFailed(uint256 index, address target, bytes reason);
    error ProfitTooLow(uint256 profit, uint256 minProfit);

    event OperatorSet(address indexed operator, bool enabled);
    event ArbitrageExecuted(
        address indexed caller,
        FlashLoanProtocol indexed protocol,
        address indexed profitToken,
        uint256 profit,
        address recipient
    );

    modifier onlyOperator() {
        if (msg.sender != owner() && !operators[msg.sender]) revert NotOperator();
        _;
    }

    constructor(address initialOwner) Ownable(_validateOwner(initialOwner)) { }

    receive() external payable { }

    function setOperator(address operator, bool enabled) external onlyOwner {
        if (operator == address(0)) revert InvalidAddress();
        operators[operator] = enabled;
        emit OperatorSet(operator, enabled);
    }

    function execute(ExecutionPlan calldata plan) external onlyOperator nonReentrant returns (uint256 profit) {
        _validatePlan(plan);

        uint256 initialProfitBalance = _balanceOf(plan.profitToken, address(this));
        callbackProfit = 0;
        bytes memory data = abi.encode(msg.sender, plan, initialProfitBalance);

        if (plan.loan.protocol == FlashLoanProtocol.Balancer) {
            BalancerIERC20[] memory tokens = new BalancerIERC20[](1);
            uint256[] memory amounts = new uint256[](1);
            tokens[0] = BalancerIERC20(plan.loan.token);
            amounts[0] = plan.loan.amount;
            IVault(plan.loan.lender).flashLoan(this, tokens, amounts, data);
        } else if (plan.loan.protocol == FlashLoanProtocol.UniswapV3) {
            uint256 amount0 = plan.loan.borrowToken0 ? plan.loan.amount : 0;
            uint256 amount1 = plan.loan.borrowToken0 ? 0 : plan.loan.amount;
            IUniswapV3Pool(plan.loan.lender).flash(address(this), amount0, amount1, data);
        } else if (plan.loan.protocol == FlashLoanProtocol.UniswapV4) {
            IPoolManager(plan.loan.lender).unlock(data);
        } else {
            revert InvalidFlashLoan();
        }

        profit = callbackProfit;
        callbackProfit = 0;
    }

    function receiveFlashLoan(
        BalancerIERC20[] memory tokens,
        uint256[] memory amounts,
        uint256[] memory feeAmounts,
        bytes memory userData
    ) external override {
        (address caller, ExecutionPlan memory plan, uint256 initialProfitBalance) =
            abi.decode(userData, (address, ExecutionPlan, uint256));

        if (plan.loan.protocol != FlashLoanProtocol.Balancer || msg.sender != plan.loan.lender) {
            revert InvalidCallback();
        }
        if (tokens.length != 1 || amounts.length != 1 || feeAmounts.length != 1) revert InvalidFlashLoan();
        if (address(tokens[0]) != plan.loan.token || amounts[0] != plan.loan.amount) revert InvalidFlashLoan();

        _executeSwaps(plan.swaps);
        IERC20(plan.loan.token).safeTransfer(plan.loan.lender, plan.loan.amount + feeAmounts[0]);
        _settleProfit(caller, plan, initialProfitBalance);
    }

    function uniswapV3FlashCallback(uint256 fee0, uint256 fee1, bytes calldata data) external override {
        (address caller, ExecutionPlan memory plan, uint256 initialProfitBalance) =
            abi.decode(data, (address, ExecutionPlan, uint256));

        if (plan.loan.protocol != FlashLoanProtocol.UniswapV3 || msg.sender != plan.loan.lender) {
            revert InvalidCallback();
        }

        uint256 fee = plan.loan.borrowToken0 ? fee0 : fee1;
        _executeSwaps(plan.swaps);
        IERC20(plan.loan.token).safeTransfer(plan.loan.lender, plan.loan.amount + fee);
        _settleProfit(caller, plan, initialProfitBalance);
    }

    function unlockCallback(bytes calldata data) external override returns (bytes memory) {
        (address caller, ExecutionPlan memory plan, uint256 initialProfitBalance) =
            abi.decode(data, (address, ExecutionPlan, uint256));

        if (plan.loan.protocol != FlashLoanProtocol.UniswapV4 || msg.sender != plan.loan.lender) {
            revert InvalidCallback();
        }

        IPoolManager manager = IPoolManager(plan.loan.lender);
        Currency currency = Currency.wrap(plan.loan.token);

        manager.take(currency, address(this), plan.loan.amount);
        _executeSwaps(plan.swaps);

        manager.sync(currency);
        IERC20(plan.loan.token).safeTransfer(plan.loan.lender, plan.loan.amount);
        manager.settle();

        uint256 profit = _settleProfit(caller, plan, initialProfitBalance);
        return abi.encode(profit);
    }

    function _validatePlan(ExecutionPlan calldata plan) private view {
        if (plan.deadline != 0 && block.timestamp > plan.deadline) revert DeadlineExpired();
        if (plan.loan.lender == address(0) || plan.loan.token == address(0)) revert InvalidAddress();
        if (plan.loan.amount == 0) revert InvalidFlashLoan();
        if (plan.profitToken == address(0) || plan.recipient == address(0)) revert InvalidAddress();
    }

    function _validateOwner(address initialOwner) private pure returns (address) {
        if (initialOwner == address(0)) revert InvalidAddress();
        return initialOwner;
    }

    function _executeSwaps(SwapCall[] memory swaps) private {
        for (uint256 i = 0; i < swaps.length; i++) {
            SwapCall memory swapCall = swaps[i];
            if (swapCall.target == address(0)) revert InvalidAddress();
            if (swapCall.allowanceTarget != address(0) && swapCall.tokenIn != address(0) && swapCall.amountIn > 0) {
                _approveIfNeeded(IERC20(swapCall.tokenIn), swapCall.allowanceTarget, swapCall.amountIn);
            }

            (bool ok, bytes memory reason) = swapCall.target.call{ value: swapCall.value }(swapCall.data);
            if (!ok) revert SwapFailed(i, swapCall.target, reason);
        }
    }

    function _settleProfit(address caller, ExecutionPlan memory plan, uint256 initialProfitBalance)
        private
        returns (uint256 profit)
    {
        uint256 finalBalance = _balanceOf(plan.profitToken, address(this));
        if (finalBalance < initialProfitBalance) revert ProfitTooLow(0, plan.minProfit);
        profit = finalBalance - initialProfitBalance;
        if (profit < plan.minProfit) revert ProfitTooLow(profit, plan.minProfit);

        if (profit > 0) {
            IERC20(plan.profitToken).safeTransfer(plan.recipient, profit);
        }

        callbackProfit = profit;
        emit ArbitrageExecuted(caller, plan.loan.protocol, plan.profitToken, profit, plan.recipient);
    }

    function _approveIfNeeded(IERC20 token, address spender, uint256 amount) private {
        if (token.allowance(address(this), spender) >= amount) return;
        token.forceApprove(spender, type(uint256).max);
    }

    function _balanceOf(address token, address account) private view returns (uint256) {
        return IERC20(token).balanceOf(account);
    }
}
