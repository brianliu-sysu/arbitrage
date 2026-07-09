// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

interface IERC20 {
    function balanceOf(address account) external view returns (uint256);
    function allowance(address owner, address spender) external view returns (uint256);
    function approve(address spender, uint256 amount) external returns (bool);
    function transfer(address to, uint256 amount) external returns (bool);
}

interface IBalancerVault {
    function flashLoan(
        IFlashLoanRecipient recipient,
        IERC20[] calldata tokens,
        uint256[] calldata amounts,
        bytes calldata userData
    ) external;
}

interface IFlashLoanRecipient {
    function receiveFlashLoan(
        IERC20[] calldata tokens,
        uint256[] calldata amounts,
        uint256[] calldata feeAmounts,
        bytes calldata userData
    ) external;
}

interface IUniswapV3Pool {
    function flash(address recipient, uint256 amount0, uint256 amount1, bytes calldata data) external;
}

interface IUniswapV4PoolManager {
    function unlock(bytes calldata data) external returns (bytes memory);
    function take(address currency, address to, uint256 amount) external;
    function sync(address currency) external;
    function settle() external payable returns (uint256 paid);
}

interface IUnlockCallback {
    function unlockCallback(bytes calldata data) external returns (bytes memory);
}

/// @notice Executes a discovered arbitrage atomically with flash liquidity.
/// @dev Swap calls are intentionally generic so the off-chain searcher can route through V3/V4/Balancer routers.
contract ArbitrageExecutor is IFlashLoanRecipient, IUnlockCallback {
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

    address public owner;
    mapping(address => bool) public operators;

    uint256 private locked = 1;
    uint256 private callbackProfit;

    error NotOwner();
    error NotOperator();
    error Reentered();
    error DeadlineExpired();
    error InvalidAddress();
    error InvalidCallback();
    error InvalidFlashLoan();
    error SwapFailed(uint256 index, address target, bytes reason);
    error TransferFailed(address token, address to, uint256 amount);
    error ApproveFailed(address token, address spender, uint256 amount);
    error ProfitTooLow(uint256 profit, uint256 minProfit);

    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);
    event OperatorSet(address indexed operator, bool enabled);
    event ArbitrageExecuted(
        address indexed caller,
        FlashLoanProtocol indexed protocol,
        address indexed profitToken,
        uint256 profit,
        address recipient
    );

    modifier onlyOwner() {
        if (msg.sender != owner) revert NotOwner();
        _;
    }

    modifier onlyOperator() {
        if (msg.sender != owner && !operators[msg.sender]) revert NotOperator();
        _;
    }

    modifier nonReentrant() {
        if (locked != 1) revert Reentered();
        locked = 2;
        _;
        locked = 1;
    }

    constructor(address initialOwner) {
        if (initialOwner == address(0)) revert InvalidAddress();
        owner = initialOwner;
        emit OwnershipTransferred(address(0), initialOwner);
    }

    receive() external payable {}

    function transferOwnership(address newOwner) external onlyOwner {
        if (newOwner == address(0)) revert InvalidAddress();
        emit OwnershipTransferred(owner, newOwner);
        owner = newOwner;
    }

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
            IERC20[] memory tokens = new IERC20[](1);
            uint256[] memory amounts = new uint256[](1);
            tokens[0] = IERC20(plan.loan.token);
            amounts[0] = plan.loan.amount;
            IBalancerVault(plan.loan.lender).flashLoan(this, tokens, amounts, data);
        } else if (plan.loan.protocol == FlashLoanProtocol.UniswapV3) {
            uint256 amount0 = plan.loan.borrowToken0 ? plan.loan.amount : 0;
            uint256 amount1 = plan.loan.borrowToken0 ? 0 : plan.loan.amount;
            IUniswapV3Pool(plan.loan.lender).flash(address(this), amount0, amount1, data);
        } else if (plan.loan.protocol == FlashLoanProtocol.UniswapV4) {
            IUniswapV4PoolManager(plan.loan.lender).unlock(data);
        } else {
            revert InvalidFlashLoan();
        }

        profit = callbackProfit;
        callbackProfit = 0;
    }

    function receiveFlashLoan(
        IERC20[] calldata tokens,
        uint256[] calldata amounts,
        uint256[] calldata feeAmounts,
        bytes calldata userData
    ) external override {
        (address caller, ExecutionPlan memory plan, uint256 initialProfitBalance) =
            abi.decode(userData, (address, ExecutionPlan, uint256));

        if (plan.loan.protocol != FlashLoanProtocol.Balancer || msg.sender != plan.loan.lender) {
            revert InvalidCallback();
        }
        if (tokens.length != 1 || amounts.length != 1 || feeAmounts.length != 1) revert InvalidFlashLoan();
        if (address(tokens[0]) != plan.loan.token || amounts[0] != plan.loan.amount) revert InvalidFlashLoan();

        _executeSwaps(plan.swaps);
        _transferToken(plan.loan.token, plan.loan.lender, plan.loan.amount + feeAmounts[0]);
        _settleProfit(caller, plan, initialProfitBalance);
    }

    function uniswapV3FlashCallback(uint256 fee0, uint256 fee1, bytes calldata data) external {
        (address caller, ExecutionPlan memory plan, uint256 initialProfitBalance) =
            abi.decode(data, (address, ExecutionPlan, uint256));

        if (plan.loan.protocol != FlashLoanProtocol.UniswapV3 || msg.sender != plan.loan.lender) {
            revert InvalidCallback();
        }

        uint256 fee = plan.loan.borrowToken0 ? fee0 : fee1;
        _executeSwaps(plan.swaps);
        _transferToken(plan.loan.token, plan.loan.lender, plan.loan.amount + fee);
        _settleProfit(caller, plan, initialProfitBalance);
    }

    function unlockCallback(bytes calldata data) external override returns (bytes memory) {
        (address caller, ExecutionPlan memory plan, uint256 initialProfitBalance) =
            abi.decode(data, (address, ExecutionPlan, uint256));

        if (plan.loan.protocol != FlashLoanProtocol.UniswapV4 || msg.sender != plan.loan.lender) {
            revert InvalidCallback();
        }

        IUniswapV4PoolManager manager = IUniswapV4PoolManager(plan.loan.lender);
        manager.take(plan.loan.token, address(this), plan.loan.amount);
        _executeSwaps(plan.swaps);

        manager.sync(plan.loan.token);
        _transferToken(plan.loan.token, plan.loan.lender, plan.loan.amount);
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

    function _executeSwaps(SwapCall[] memory swaps) private {
        for (uint256 i = 0; i < swaps.length; i++) {
            SwapCall memory swapCall = swaps[i];
            if (swapCall.target == address(0)) revert InvalidAddress();
            if (swapCall.allowanceTarget != address(0) && swapCall.tokenIn != address(0) && swapCall.amountIn > 0) {
                _approveIfNeeded(swapCall.tokenIn, swapCall.allowanceTarget, swapCall.amountIn);
            }

            (bool ok, bytes memory reason) = swapCall.target.call{ value: swapCall.value }(swapCall.data);
            if (!ok) revert SwapFailed(i, swapCall.target, reason);
        }
    }

    function _settleProfit(
        address caller,
        ExecutionPlan memory plan,
        uint256 initialProfitBalance
    ) private returns (uint256 profit) {
        uint256 finalBalance = _balanceOf(plan.profitToken, address(this));
        if (finalBalance < initialProfitBalance) revert ProfitTooLow(0, plan.minProfit);
        profit = finalBalance - initialProfitBalance;
        if (profit < plan.minProfit) revert ProfitTooLow(profit, plan.minProfit);

        if (profit > 0) {
            _transferToken(plan.profitToken, plan.recipient, profit);
        }

        callbackProfit = profit;
        emit ArbitrageExecuted(caller, plan.loan.protocol, plan.profitToken, profit, plan.recipient);
    }

    function _approveIfNeeded(address token, address spender, uint256 amount) private {
        if (IERC20(token).allowance(address(this), spender) >= amount) return;
        _safeApprove(token, spender, 0);
        _safeApprove(token, spender, type(uint256).max);
    }

    function _safeApprove(address token, address spender, uint256 amount) private {
        (bool ok, bytes memory data) = token.call(abi.encodeCall(IERC20.approve, (spender, amount)));
        if (!ok || (data.length > 0 && !abi.decode(data, (bool)))) {
            revert ApproveFailed(token, spender, amount);
        }
    }

    function _transferToken(address token, address to, uint256 amount) private {
        (bool ok, bytes memory data) = token.call(abi.encodeCall(IERC20.transfer, (to, amount)));
        if (!ok || (data.length > 0 && !abi.decode(data, (bool)))) {
            revert TransferFailed(token, to, amount);
        }
    }

    function _balanceOf(address token, address account) private view returns (uint256) {
        return IERC20(token).balanceOf(account);
    }
}
