// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import { Ownable } from "@openzeppelin/contracts/access/Ownable.sol";
import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { SafeERC20 } from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {
    IERC20 as BalancerIERC20
} from "balancer-v2-monorepo/pkg/interfaces/contracts/solidity-utils/openzeppelin/IERC20.sol";
import { IFlashLoanRecipient } from "balancer-v2-monorepo/pkg/interfaces/contracts/vault/IFlashLoanRecipient.sol";
import { IVault } from "balancer-v2-monorepo/pkg/interfaces/contracts/vault/IVault.sol";
import { IUniswapV3Pool } from "v3-core/contracts/interfaces/IUniswapV3Pool.sol";
import { IUniswapV3FlashCallback } from "v3-core/contracts/interfaces/callback/IUniswapV3FlashCallback.sol";
import { IPoolManager } from "v4-core/src/interfaces/IPoolManager.sol";
import { IUnlockCallback } from "v4-core/src/interfaces/callback/IUnlockCallback.sol";
import { TransientStateLibrary } from "v4-core/src/libraries/TransientStateLibrary.sol";
import { Currency, CurrencyLibrary } from "v4-core/src/types/Currency.sol";
import { CurrencySettler } from "./libraries/CurrencySettler.sol";

interface IWrappedNative {
    function withdraw(uint256 amount) external;
}

/// @notice Executes a discovered arbitrage atomically with flash liquidity.
/// @dev Swap calls are intentionally generic so the off-chain searcher can route through V3/V4/Balancer routers.
contract ArbitrageExecutor is Ownable, IFlashLoanRecipient, IUniswapV3FlashCallback, IUnlockCallback {
    using SafeERC20 for IERC20;
    using CurrencyLibrary for Currency;
    using CurrencySettler for Currency;
    using TransientStateLibrary for IPoolManager;

    enum FlashLoanProtocol {
        Balancer,
        UniswapV3,
        UniswapV4
    }

    enum FillSource {
        None,
        ERC20Balance,
        NativeBalance
    }

    struct FlashLoan {
        FlashLoanProtocol protocol;
        address lender;
        address token;
        uint256 amount;
        bool borrowToken0;
    }

    struct RouterCall {
        address routerAddress;
        uint256 value;
        bytes data;
        /// @dev Explicit source for dynamic amount fill. Native assets use address(0) internally.
        FillSource fillSource;
        address fillToken;
        bool patchAmount;
        bool amountAsCallValue;
        uint256 fillOffset;
    }

    struct ExecutionPlan {
        FlashLoan loan;
        RouterCall[] routers;
        /// @dev Currencies that may hold open PoolManager deltas after swaps (must include the loan token).
        address[] settleCurrencies;
        address profitToken;
        uint256 minProfit;
        uint256 deadline;
    }

    mapping(address => bool) public operators;
    address public profitRecipient;
    bool private executionActive;
    bytes32 private activeExecutionHash;

    /// @dev Conventional native-ETH sentinel used by routers / aggregators.
    address internal constant NATIVE_TOKEN = 0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE;

    error NotOperator();
    error DeadlineExpired();
    error InvalidAddress();
    error InvalidCallback();
    error InvalidFillToken();
    error InvalidFillOffset(uint256 index, uint256 offset, uint256 dataLength);
    error InsufficientRepayBalance(address token, uint256 balance, uint256 required);
    error NativeTransferFailed();
    error SwapFailed();
    error SwapCallFailed(uint256 index, address router, bytes reason);
    error ProfitTooLow(uint256 profit, uint256 minProfit);
    error InvalidCoinbasePayment();

    event OperatorSet(address indexed operator, bool enabled);
    event ProfitRecipientSet(address indexed recipient);
    event ArbitrageExecuted(
        address indexed caller,
        FlashLoanProtocol indexed protocol,
        address indexed profitToken,
        uint256 profit,
        address recipient
    );
    event CoinbasePaid(address indexed coinbase, uint256 amount);

    modifier onlyOperator() {
        if (msg.sender != owner() && !operators[msg.sender]) revert NotOperator();
        _;
    }

    constructor(address initialOwner) Ownable(_validateOwner(initialOwner)) {
        profitRecipient = initialOwner;
    }

    receive() external payable { }

    function setOperator(address operator, bool enabled) external onlyOwner {
        if (operator == address(0)) revert InvalidAddress();
        operators[operator] = enabled;
        emit OperatorSet(operator, enabled);
    }

    function setProfitRecipient(address recipient) external onlyOwner {
        if (recipient == address(0)) revert InvalidAddress();
        profitRecipient = recipient;
        emit ProfitRecipientSet(recipient);
    }

    function approveToken(address token, address spender, uint256 amount) external onlyOperator {
        if (token == address(0) || spender == address(0)) revert InvalidAddress();
        IERC20(token).forceApprove(spender, amount);
    }

    function execute(ExecutionPlan calldata plan) external onlyOperator returns (uint256 profit) {
        return _execute(plan, 0, address(0));
    }

    function execute(ExecutionPlan calldata plan, uint16 coinbasePaymentBps, address wrappedNativeToken)
        external
        onlyOperator
        returns (uint256 profit)
    {
        return _execute(plan, coinbasePaymentBps, wrappedNativeToken);
    }

    function _execute(ExecutionPlan calldata plan, uint16 coinbasePaymentBps, address wrappedNativeToken)
        private
        returns (uint256 profit)
    {
        // Only on-chain freshness; operator is trusted to supply a well-formed plan.
        if (plan.deadline != 0 && block.timestamp > plan.deadline) revert DeadlineExpired();

        uint256 initialProfitBalance = _balanceOf(plan.profitToken, address(this));
        uint256[] memory initialFillBalances = _snapshotFillBalances(plan.routers);
        bytes memory data = abi.encode(msg.sender, plan, initialProfitBalance, initialFillBalances);

        _activateExecution(data);

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
        } else {
            IPoolManager(plan.loan.lender).unlock(data);
        }

        _clearActiveExecution();
        profit = _settleProfit(msg.sender, plan, initialProfitBalance, coinbasePaymentBps, wrappedNativeToken);
    }

    function receiveFlashLoan(
        BalancerIERC20[] memory,
        uint256[] memory,
        uint256[] memory feeAmounts,
        bytes memory userData
    ) external override {
        (, ExecutionPlan memory plan,, uint256[] memory initialFillBalances) =
            abi.decode(userData, (address, ExecutionPlan, uint256, uint256[]));

        if (
            plan.loan.protocol != FlashLoanProtocol.Balancer || msg.sender != plan.loan.lender
                || !_isActiveExecutionHash(keccak256(userData))
        ) {
            revert InvalidCallback();
        }

        _executeSwaps(plan.routers, initialFillBalances);
        _repayERC20(plan.loan.token, plan.loan.lender, plan.loan.amount + feeAmounts[0]);
    }

    function uniswapV3FlashCallback(uint256 fee0, uint256 fee1, bytes calldata data) external override {
        (, ExecutionPlan memory plan,, uint256[] memory initialFillBalances) =
            abi.decode(data, (address, ExecutionPlan, uint256, uint256[]));

        if (
            plan.loan.protocol != FlashLoanProtocol.UniswapV3 || msg.sender != plan.loan.lender
                || !_isActiveExecutionHash(keccak256(data))
        ) {
            revert InvalidCallback();
        }

        uint256 fee = plan.loan.borrowToken0 ? fee0 : fee1;
        _executeSwaps(plan.routers, initialFillBalances);
        _repayERC20(plan.loan.token, plan.loan.lender, plan.loan.amount + fee);
    }

    function unlockCallback(bytes calldata data) external override returns (bytes memory) {
        (, ExecutionPlan memory plan,, uint256[] memory initialFillBalances) =
            abi.decode(data, (address, ExecutionPlan, uint256, uint256[]));

        if (
            plan.loan.protocol != FlashLoanProtocol.UniswapV4 || msg.sender != plan.loan.lender
                || !_isActiveExecutionHash(keccak256(data))
        ) {
            revert InvalidCallback();
        }

        IPoolManager manager = IPoolManager(plan.loan.lender);
        Currency loanCurrency = Currency.wrap(plan.loan.token);

        // Borrow creates a negative currency delta that must be cleared before unlock returns.
        manager.take(loanCurrency, address(this), plan.loan.amount);
        _executeSwaps(plan.routers, initialFillBalances);
        _clearUniswapV4Deltas(manager, plan.loan.token, plan.settleCurrencies);

        return "";
    }

    function _validateOwner(address initialOwner) private pure returns (address) {
        if (initialOwner == address(0)) revert InvalidAddress();
        return initialOwner;
    }

    function _profitRecipient() private view returns (address) {
        return profitRecipient == address(0) ? owner() : profitRecipient;
    }

    function _activateExecution(bytes memory data) private {
        activeExecutionHash = keccak256(data);
        executionActive = true;
    }

    function _clearActiveExecution() private {
        executionActive = false;
        delete activeExecutionHash;
    }

    function _isActiveExecutionHash(bytes32 dataHash) private view returns (bool) {
        return executionActive && activeExecutionHash == dataHash;
    }

    function _snapshotFillBalances(RouterCall[] calldata routers) private view returns (uint256[] memory balances) {
        balances = new uint256[](routers.length);
        for (uint256 i = 0; i < routers.length; i++) {
            if (routers[i].fillSource == FillSource.None) continue;
            balances[i] = _fillBalance(routers[i]);
        }
    }

    function _executeSwaps(RouterCall[] memory routers, uint256[] memory initialFillBalances) private {
        if (routers.length != initialFillBalances.length) revert InvalidCallback();
        for (uint256 i = 0; i < routers.length; i++) {
            _executeSwap(i, routers[i], initialFillBalances[i]);
        }
    }

    function _executeSwap(uint256 index, RouterCall memory router, uint256 initialFillBalance) private {
        bytes memory midData = router.data;
        uint256 callValue = router.value;

        if (router.fillSource != FillSource.None) {
            uint256 currentBalance = _fillBalance(router);
            uint256 actualAmount = currentBalance > initialFillBalance ? currentBalance - initialFillBalance : 0;
            if (router.patchAmount) {
                if (router.fillOffset > midData.length || midData.length - router.fillOffset < 32) {
                    revert InvalidFillOffset(index, router.fillOffset, midData.length);
                }
                uint256 offset = router.fillOffset;
                assembly ("memory-safe") {
                    // midData layout: length at midData, payload starts at midData + 32.
                    mstore(add(add(midData, 32), offset), actualAmount)
                }
            }
            if (router.amountAsCallValue) {
                callValue = actualAmount;
            }
        }

        (bool ok, bytes memory reason) = router.routerAddress.call{ value: callValue }(midData);
        if (!ok) revert SwapCallFailed(index, router.routerAddress, reason);
    }

    function _fillBalance(RouterCall memory router) private view returns (uint256) {
        if (router.fillSource == FillSource.NativeBalance) {
            return address(this).balance;
        }
        if (router.fillSource == FillSource.ERC20Balance) {
            if (router.fillToken == address(0) || router.fillToken == NATIVE_TOKEN) revert InvalidFillToken();
            return IERC20(router.fillToken).balanceOf(address(this));
        }
        return 0;
    }

    function _repayERC20(address token, address lender, uint256 required) private {
        uint256 balance = IERC20(token).balanceOf(address(this));
        if (balance < required) revert InsufficientRepayBalance(token, balance, required);
        IERC20(token).safeTransfer(lender, required);
    }

    /// @dev Clears open deltas with CurrencySettler. Operator must list every currency that may be nonzero.
    function _clearUniswapV4Deltas(IPoolManager manager, address loanToken, address[] memory settleCurrencies) private {
        if (settleCurrencies.length == 0) {
            _clearUniswapV4Delta(manager, Currency.wrap(loanToken));
            return;
        }

        for (uint256 i = 0; i < settleCurrencies.length; i++) {
            _clearUniswapV4Delta(manager, Currency.wrap(settleCurrencies[i]));
        }
    }

    function _clearUniswapV4Delta(IPoolManager manager, Currency currency) private {
        int256 delta = manager.currencyDelta(address(this), currency);
        if (delta < 0) {
            currency.settle(manager, address(this), uint256(-delta), false);
        } else if (delta > 0) {
            currency.take(manager, address(this), uint256(delta), false);
        }
    }

    function _settleProfit(
        address caller,
        ExecutionPlan memory plan,
        uint256 initialProfitBalance,
        uint16 coinbasePaymentBps,
        address wrappedNativeToken
    )
        private
        returns (uint256 profit)
    {
        uint256 finalBalance = _balanceOf(plan.profitToken, address(this));
        if (finalBalance < initialProfitBalance) revert ProfitTooLow(0, plan.minProfit);
        uint256 grossProfit = finalBalance - initialProfitBalance;
        uint256 coinbasePayment = _payCoinbase(plan.profitToken, grossProfit, coinbasePaymentBps, wrappedNativeToken);
        profit = grossProfit - coinbasePayment;
        if (profit < plan.minProfit) revert ProfitTooLow(profit, plan.minProfit);

        address recipient = _profitRecipient();
        if (profit > 0) {
            _transferAsset(plan.profitToken, recipient, profit);
        }

        emit ArbitrageExecuted(caller, plan.loan.protocol, plan.profitToken, profit, recipient);
    }

    function _payCoinbase(address profitToken, uint256 grossProfit, uint16 paymentBps, address wrappedNativeToken)
        private
        returns (uint256 payment)
    {
        if (paymentBps == 0 || grossProfit == 0) return 0;
        if (paymentBps > 10_000) revert InvalidCoinbasePayment();
        if (profitToken != address(0) && profitToken != wrappedNativeToken) {
            revert InvalidCoinbasePayment();
        }

        payment = grossProfit * paymentBps / 10_000;
        if (payment == 0) return 0;
        if (profitToken != address(0)) IWrappedNative(wrappedNativeToken).withdraw(payment);
        (bool success,) = payable(block.coinbase).call{ value: payment }("");
        if (!success) revert NativeTransferFailed();
        emit CoinbasePaid(block.coinbase, payment);
    }

    function _balanceOf(address token, address account) private view returns (uint256) {
        if (token == address(0)) {
            return account.balance;
        }
        return IERC20(token).balanceOf(account);
    }

    function _transferAsset(address token, address recipient, uint256 amount) private {
        if (token == address(0)) {
            (bool success,) = payable(recipient).call{ value: amount }("");
            if (!success) revert NativeTransferFailed();
            return;
        }
        IERC20(token).safeTransfer(recipient, amount);
    }
}
