// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import { Test } from "forge-std/Test.sol";
import { ERC20 } from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import {
    IERC20 as BalancerIERC20
} from "balancer-v2-monorepo/pkg/interfaces/contracts/solidity-utils/openzeppelin/IERC20.sol";
import { IFlashLoanRecipient } from "balancer-v2-monorepo/pkg/interfaces/contracts/vault/IFlashLoanRecipient.sol";
import { Currency } from "v4-core/src/types/Currency.sol";
import { ArbitrageExecutor } from "../src/ArbitrageExecutor.sol";

contract MockERC20 is ERC20 {
    constructor(string memory name_, string memory symbol_) ERC20(name_, symbol_) { }

    function mint(address to, uint256 amount) external {
        _mint(to, amount);
    }
}

contract MockBalancerVault {
    uint256 public feeAmount;

    function setFeeAmount(uint256 feeAmount_) external {
        feeAmount = feeAmount_;
    }

    function flashLoan(
        IFlashLoanRecipient recipient,
        BalancerIERC20[] calldata tokens,
        uint256[] calldata amounts,
        bytes calldata userData
    ) external {
        require(tokens.length == 1 && amounts.length == 1, "bad flash loan");
        MockERC20(address(tokens[0])).mint(address(recipient), amounts[0]);

        uint256[] memory fees = new uint256[](1);
        fees[0] = feeAmount;
        recipient.receiveFlashLoan(tokens, amounts, fees, userData);

        require(tokens[0].balanceOf(address(this)) == amounts[0] + feeAmount, "flash loan not repaid");
    }
}

/// @dev Minimal PoolManager stand-in that mirrors flash accounting + CurrencySettler repay.
contract MockPoolManager {
    address public unlockedBy;
    Currency public syncedCurrency;
    uint256 public syncedReserves;
    bool public currencySynced;
    uint256 public nonzeroDeltaCount;
    mapping(bytes32 => int256) internal _deltas;

    error NotUnlocked();
    error CurrencyNotSettled();
    error InsufficientBalance();

    receive() external payable { }

    function unlock(bytes calldata data) external returns (bytes memory result) {
        unlockedBy = msg.sender;
        result = ArbitrageExecutor(payable(msg.sender)).unlockCallback(data);
        if (nonzeroDeltaCount != 0) revert CurrencyNotSettled();
        unlockedBy = address(0);
    }

    function take(Currency currency, address to, uint256 amount) external {
        if (unlockedBy == address(0)) revert NotUnlocked();
        _accountDelta(msg.sender, currency, -int256(amount));

        if (Currency.unwrap(currency) == address(0)) {
            if (address(this).balance < amount) revert InsufficientBalance();
            (bool ok,) = to.call{ value: amount }("");
            require(ok, "native take failed");
            return;
        }

        MockERC20(Currency.unwrap(currency)).mint(to, amount);
    }

    function sync(Currency currency) external {
        syncedCurrency = currency;
        currencySynced = true;
        if (Currency.unwrap(currency) == address(0)) {
            syncedReserves = 0;
        } else {
            syncedReserves = MockERC20(Currency.unwrap(currency)).balanceOf(address(this));
        }
    }

    function settle() external payable returns (uint256 paid) {
        if (unlockedBy == address(0)) revert NotUnlocked();

        Currency currency = syncedCurrency;
        if (!currencySynced || Currency.unwrap(currency) == address(0)) {
            paid = msg.value;
            currency = Currency.wrap(address(0));
        } else {
            require(msg.value == 0, "nonzero native value");
            uint256 reservesNow = MockERC20(Currency.unwrap(currency)).balanceOf(address(this));
            paid = reservesNow - syncedReserves;
            currencySynced = false;
        }

        _accountDelta(msg.sender, currency, int256(paid));
    }

    /// @dev Used by TransientStateLibrary.currencyDelta via the same keccak key as v4-core.
    function exttload(bytes32 slot) external view returns (bytes32) {
        return bytes32(uint256(_deltas[slot]));
    }

    function deltaOf(address target, address token) external view returns (int256) {
        return _deltas[_deltaKey(target, Currency.wrap(token))];
    }

    /// @dev Test helper: create an extra open delta as if a V4 swap/hook left debt or credit.
    function forceDelta(address target, Currency currency, int256 delta) external {
        _accountDelta(target, currency, delta);
        if (delta < 0 && Currency.unwrap(currency) != address(0)) {
            // Leave the caller holding tokens so they can repay the forced debt.
            MockERC20(Currency.unwrap(currency)).mint(target, uint256(-delta));
        }
    }

    function _accountDelta(address target, Currency currency, int256 delta) internal {
        if (delta == 0) return;
        bytes32 key = _deltaKey(target, currency);
        int256 previous = _deltas[key];
        int256 next = previous + delta;
        _deltas[key] = next;
        if (next == 0) {
            unchecked {
                nonzeroDeltaCount--;
            }
        } else if (previous == 0) {
            unchecked {
                nonzeroDeltaCount++;
            }
        }
    }

    function _deltaKey(address target, Currency currency) internal pure returns (bytes32 key) {
        assembly ("memory-safe") {
            mstore(0, and(target, 0xffffffffffffffffffffffffffffffffffffffff))
            mstore(32, and(currency, 0xffffffffffffffffffffffffffffffffffffffff))
            key := keccak256(0, 64)
        }
    }
}

contract MockSwapTarget {
    function mintProfit(MockERC20 token, address to, uint256 amount) external {
        token.mint(to, amount);
    }

    function pullAndMint(MockERC20 token, uint256 pullAmount, uint256 mintAmount) external {
        require(token.transferFrom(msg.sender, address(this), pullAmount), "pull failed");
        token.mint(msg.sender, mintAmount);
    }

    function pullDynamicAndMint(MockERC20 tokenIn, MockERC20 tokenOut, uint256 pullAmount, uint256 mintAmount)
        external
    {
        require(tokenIn.transferFrom(msg.sender, address(this), pullAmount), "pull failed");
        tokenOut.mint(msg.sender, mintAmount);
    }

    function spendETHAndMint(uint256 amount, MockERC20 tokenOut, uint256 mintAmount) external payable {
        require(msg.value == amount, "bad value");
        tokenOut.mint(msg.sender, mintAmount);
    }

    /// @dev Mimics WETH.deposit(): consumes msg.value only (no amount in calldata).
    function deposit() external payable {
        require(msg.value > 0, "zero deposit");
    }

    function sendETH(address payable to, uint256 amount) external {
        (bool ok,) = to.call{ value: amount }("");
        require(ok, "send eth failed");
    }

    function forcePoolManagerDelta(MockPoolManager manager, address target, address token, int256 delta) external {
        manager.forceDelta(target, Currency.wrap(token), delta);
    }
}

contract ArbitrageExecutorTest is Test {
    ArbitrageExecutor private executor;
    MockERC20 private token;
    MockBalancerVault private vault;
    MockSwapTarget private swapTarget;

    address private owner = address(0xA11CE);
    address private operator = address(0xB0B);
    address private recipient = address(0xCAFE);

    function setUp() external {
        executor = new ArbitrageExecutor(owner);
        token = new MockERC20("Mock Token", "MOCK");
        vault = new MockBalancerVault();
        swapTarget = new MockSwapTarget();

        vm.prank(owner);
        executor.setOperator(operator, true);
    }

    function testOwnerCanSetOperator() external {
        address newOperator = address(0xD00D);

        vm.prank(owner);
        executor.setOperator(newOperator, true);

        assertTrue(executor.operators(newOperator));
    }

    function testOwnerCanSetProfitRecipient() external {
        address newRecipient = address(0xBEEF);

        vm.prank(owner);
        executor.setProfitRecipient(newRecipient);

        assertEq(executor.profitRecipient(), newRecipient);
    }

    function testNonOperatorCannotExecute() external {
        ArbitrageExecutor.ExecutionPlan memory plan = _balancerPlan(25 ether, 1 ether);

        vm.expectRevert(ArbitrageExecutor.NotOperator.selector);
        executor.execute(plan);
    }

    function testExecuteBalancerFlashLoanTransfersProfit() external {
        uint256 minProfit = 20 ether;
        uint256 profit = 25 ether;
        _setProfitRecipient();
        ArbitrageExecutor.ExecutionPlan memory plan = _balancerPlan(profit, minProfit);

        vm.prank(operator);
        uint256 returnedProfit = executor.execute(plan);

        assertEq(returnedProfit, profit);
        assertEq(token.balanceOf(recipient), profit);
        assertEq(token.balanceOf(address(vault)), plan.loan.amount);
    }

    function testExecuteTransfersNativeProfit() external {
        uint256 nativeProfit = 2 ether;
        _setProfitRecipient();
        vm.deal(address(swapTarget), nativeProfit);

        ArbitrageExecutor.RouterCall[] memory routers = new ArbitrageExecutor.RouterCall[](1);
        routers[0] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(MockSwapTarget.sendETH, (payable(address(executor)), nativeProfit)),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });

        ArbitrageExecutor.ExecutionPlan memory plan = ArbitrageExecutor.ExecutionPlan({
            loan: ArbitrageExecutor.FlashLoan({
                protocol: ArbitrageExecutor.FlashLoanProtocol.Balancer,
                lender: address(vault),
                token: address(token),
                amount: 100 ether,
                borrowToken0: true
            }),
            routers: routers,
            settleCurrencies: new address[](0),
            profitToken: address(0),
            minProfit: nativeProfit,
            deadline: block.timestamp + 1
        });

        vm.prank(operator);
        uint256 returnedProfit = executor.execute(plan);

        assertEq(returnedProfit, nativeProfit);
        assertEq(recipient.balance, nativeProfit);
        assertEq(address(executor).balance, 0);
    }

    function testRejectsDirectBalancerCallbackWithoutActiveExecution() external {
        ArbitrageExecutor.ExecutionPlan memory plan = _balancerPlan(25 ether, 1 ether);
        uint256[] memory initialFillBalances = new uint256[](plan.routers.length);
        bytes memory userData = abi.encode(operator, plan, uint256(0), initialFillBalances);
        BalancerIERC20[] memory tokens = new BalancerIERC20[](1);
        tokens[0] = BalancerIERC20(address(token));
        uint256[] memory amounts = new uint256[](1);
        amounts[0] = plan.loan.amount;
        uint256[] memory fees = new uint256[](1);

        vm.prank(address(vault));
        vm.expectRevert(ArbitrageExecutor.InvalidCallback.selector);
        executor.receiveFlashLoan(tokens, amounts, fees, userData);
    }

    function testExecuteDefaultsProfitRecipientToOwner() external {
        uint256 profit = 25 ether;
        ArbitrageExecutor.ExecutionPlan memory plan = _balancerPlan(profit, 1 ether);

        vm.prank(operator);
        uint256 returnedProfit = executor.execute(plan);

        assertEq(returnedProfit, profit);
        assertEq(token.balanceOf(owner), profit);
        assertEq(token.balanceOf(recipient), 0);
    }

    function testExecuteRouterCallTransfersProfit() external {
        uint256 mintedAmount = 35 ether;
        _setProfitRecipient();

        ArbitrageExecutor.RouterCall[] memory routers = new ArbitrageExecutor.RouterCall[](1);
        routers[0] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(MockSwapTarget.mintProfit, (token, address(executor), mintedAmount)),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });

        ArbitrageExecutor.ExecutionPlan memory plan = ArbitrageExecutor.ExecutionPlan({
            loan: ArbitrageExecutor.FlashLoan({
                protocol: ArbitrageExecutor.FlashLoanProtocol.Balancer,
                lender: address(vault),
                token: address(token),
                amount: 100 ether,
                borrowToken0: true
            }),
            routers: routers,
            settleCurrencies: new address[](0),
            profitToken: address(token),
            minProfit: 20 ether,
            deadline: block.timestamp + 1
        });

        vm.prank(operator);
        uint256 returnedProfit = executor.execute(plan);

        assertEq(returnedProfit, mintedAmount);
        assertEq(token.balanceOf(recipient), mintedAmount);
    }

    function testExecuteRevertsWhenProfitTooLow() external {
        ArbitrageExecutor.ExecutionPlan memory plan = _balancerPlan(1 ether, 2 ether);

        vm.prank(operator);
        vm.expectRevert(abi.encodeWithSelector(ArbitrageExecutor.ProfitTooLow.selector, 1 ether, 2 ether));
        executor.execute(plan);
    }

    function testExecuteRevertsWithInsufficientRepayBalance() external {
        uint256 loanAmount = 100 ether;
        ArbitrageExecutor.RouterCall[] memory routers = new ArbitrageExecutor.RouterCall[](1);
        routers[0] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(MockSwapTarget.pullAndMint, (token, loanAmount, uint256(0))),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });

        vm.prank(address(executor));
        token.approve(address(swapTarget), type(uint256).max);

        ArbitrageExecutor.ExecutionPlan memory plan = ArbitrageExecutor.ExecutionPlan({
            loan: ArbitrageExecutor.FlashLoan({
                protocol: ArbitrageExecutor.FlashLoanProtocol.Balancer,
                lender: address(vault),
                token: address(token),
                amount: loanAmount,
                borrowToken0: true
            }),
            routers: routers,
            settleCurrencies: new address[](0),
            profitToken: address(token),
            minProfit: 0,
            deadline: block.timestamp + 1
        });

        vm.prank(operator);
        vm.expectRevert(
            abi.encodeWithSelector(
                ArbitrageExecutor.InsufficientRepayBalance.selector, address(token), uint256(0), loanAmount
            )
        );
        executor.execute(plan);
    }

    function testExecuteMultipleRouterCallsTransfersProfit() external {
        MockERC20 outputToken = new MockERC20("Output Token", "OUT");
        uint256 intermediateAmount = 40 ether;
        uint256 profit = 25 ether;
        _setProfitRecipient();

        ArbitrageExecutor.RouterCall[] memory routers = new ArbitrageExecutor.RouterCall[](2);
        routers[0] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(MockSwapTarget.mintProfit, (outputToken, address(executor), intermediateAmount)),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });
        routers[1] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(MockSwapTarget.mintProfit, (token, address(executor), profit)),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });

        ArbitrageExecutor.ExecutionPlan memory plan = ArbitrageExecutor.ExecutionPlan({
            loan: ArbitrageExecutor.FlashLoan({
                protocol: ArbitrageExecutor.FlashLoanProtocol.Balancer,
                lender: address(vault),
                token: address(token),
                amount: 100 ether,
                borrowToken0: true
            }),
            routers: routers,
            settleCurrencies: new address[](0),
            profitToken: address(token),
            minProfit: profit,
            deadline: block.timestamp + 1
        });

        vm.prank(operator);
        uint256 returnedProfit = executor.execute(plan);

        assertEq(returnedProfit, profit);
        assertEq(token.balanceOf(recipient), profit);
        assertEq(outputToken.balanceOf(address(executor)), intermediateAmount);
    }

    function testExecuteFillsAmountFromLiveTokenBalance() external {
        MockERC20 midToken = new MockERC20("Mid Token", "MID");
        uint256 intermediateAmount = 40 ether;
        uint256 historicalAmount = 7 ether;
        uint256 profit = 25 ether;
        _setProfitRecipient();
        midToken.mint(address(executor), historicalAmount);

        // pullDynamicAndMint(tokenIn, tokenOut, pullAmount, mintAmount):
        // selector(4) + tokenIn(32) + tokenOut(32) + pullAmount@68.
        bytes memory secondCall =
            abi.encodeCall(MockSwapTarget.pullDynamicAndMint, (midToken, token, uint256(0), profit));
        uint256 fillOffset = 68;

        ArbitrageExecutor.RouterCall[] memory routers = new ArbitrageExecutor.RouterCall[](2);
        routers[0] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(MockSwapTarget.mintProfit, (midToken, address(executor), intermediateAmount)),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });
        routers[1] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: secondCall,
            fillSource: ArbitrageExecutor.FillSource.ERC20Balance,
            fillToken: address(midToken),
            patchAmount: true,
            amountAsCallValue: false,
            fillOffset: fillOffset
        });

        vm.prank(address(executor));
        midToken.approve(address(swapTarget), type(uint256).max);

        ArbitrageExecutor.ExecutionPlan memory plan = ArbitrageExecutor.ExecutionPlan({
            loan: ArbitrageExecutor.FlashLoan({
                protocol: ArbitrageExecutor.FlashLoanProtocol.Balancer,
                lender: address(vault),
                token: address(token),
                amount: 100 ether,
                borrowToken0: true
            }),
            routers: routers,
            settleCurrencies: new address[](0),
            profitToken: address(token),
            minProfit: profit,
            deadline: block.timestamp + 1
        });

        vm.prank(operator);
        uint256 returnedProfit = executor.execute(plan);

        assertEq(returnedProfit, profit);
        assertEq(token.balanceOf(recipient), profit);
        assertEq(midToken.balanceOf(address(executor)), historicalAmount);
        assertEq(midToken.balanceOf(address(swapTarget)), intermediateAmount);
    }

    function testExecuteRevertsWhenFillOffsetIsOutOfRange() external {
        MockERC20 midToken = new MockERC20("Mid Token", "MID");
        uint256 intermediateAmount = 40 ether;
        uint256 profit = 25 ether;

        bytes memory secondCall =
            abi.encodeCall(MockSwapTarget.pullDynamicAndMint, (midToken, token, uint256(0), profit));

        ArbitrageExecutor.RouterCall[] memory routers = new ArbitrageExecutor.RouterCall[](2);
        routers[0] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(MockSwapTarget.mintProfit, (midToken, address(executor), intermediateAmount)),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });
        routers[1] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: secondCall,
            fillSource: ArbitrageExecutor.FillSource.ERC20Balance,
            fillToken: address(midToken),
            patchAmount: true,
            amountAsCallValue: false,
            fillOffset: secondCall.length
        });

        ArbitrageExecutor.ExecutionPlan memory plan = ArbitrageExecutor.ExecutionPlan({
            loan: ArbitrageExecutor.FlashLoan({
                protocol: ArbitrageExecutor.FlashLoanProtocol.Balancer,
                lender: address(vault),
                token: address(token),
                amount: 100 ether,
                borrowToken0: true
            }),
            routers: routers,
            settleCurrencies: new address[](0),
            profitToken: address(token),
            minProfit: profit,
            deadline: block.timestamp + 1
        });

        vm.prank(operator);
        vm.expectRevert(
            abi.encodeWithSelector(
                ArbitrageExecutor.InvalidFillOffset.selector, uint256(1), secondCall.length, secondCall.length
            )
        );
        executor.execute(plan);
    }

    function testExecuteFillsAmountFromNativeETHBalance() external {
        uint256 ethAmount = 5 ether;
        uint256 historicalETH = 2 ether;
        uint256 profit = 25 ether;
        _setProfitRecipient();
        vm.deal(address(executor), historicalETH);
        vm.deal(address(swapTarget), ethAmount);

        // spendETHAndMint(amount, tokenOut, mintAmount): selector(4) + amount@4.
        bytes memory callData = abi.encodeCall(MockSwapTarget.spendETHAndMint, (uint256(0), token, profit));

        ArbitrageExecutor.RouterCall[] memory routers = new ArbitrageExecutor.RouterCall[](2);
        routers[0] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(MockSwapTarget.sendETH, (payable(address(executor)), ethAmount)),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });
        routers[1] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: callData,
            fillSource: ArbitrageExecutor.FillSource.NativeBalance,
            fillToken: address(0),
            patchAmount: true,
            amountAsCallValue: true,
            fillOffset: 4
        });

        ArbitrageExecutor.ExecutionPlan memory plan = ArbitrageExecutor.ExecutionPlan({
            loan: ArbitrageExecutor.FlashLoan({
                protocol: ArbitrageExecutor.FlashLoanProtocol.Balancer,
                lender: address(vault),
                token: address(token),
                amount: 100 ether,
                borrowToken0: true
            }),
            routers: routers,
            settleCurrencies: new address[](0),
            profitToken: address(token),
            minProfit: profit,
            deadline: block.timestamp + 1
        });

        vm.prank(operator);
        uint256 returnedProfit = executor.execute(plan);

        assertEq(returnedProfit, profit);
        assertEq(token.balanceOf(recipient), profit);
        assertEq(address(executor).balance, historicalETH);
        assertEq(address(swapTarget).balance, ethAmount);
    }

    function testExecuteFillsNativeValueForSelectorOnlyCall() external {
        uint256 ethAmount = 3 ether;
        uint256 profit = 25 ether;
        _setProfitRecipient();
        vm.deal(address(swapTarget), ethAmount);

        // deposit() is selector-only; fill must override msg.value without patching calldata.
        bytes memory callData = abi.encodeCall(MockSwapTarget.deposit, ());

        ArbitrageExecutor.RouterCall[] memory routers = new ArbitrageExecutor.RouterCall[](3);
        routers[0] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(MockSwapTarget.sendETH, (payable(address(executor)), ethAmount)),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });
        routers[1] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: callData,
            fillSource: ArbitrageExecutor.FillSource.NativeBalance,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: true,
            fillOffset: 0
        });
        routers[2] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(MockSwapTarget.mintProfit, (token, address(executor), profit)),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });

        ArbitrageExecutor.ExecutionPlan memory plan = ArbitrageExecutor.ExecutionPlan({
            loan: ArbitrageExecutor.FlashLoan({
                protocol: ArbitrageExecutor.FlashLoanProtocol.Balancer,
                lender: address(vault),
                token: address(token),
                amount: 100 ether,
                borrowToken0: true
            }),
            routers: routers,
            settleCurrencies: new address[](0),
            profitToken: address(token),
            minProfit: profit,
            deadline: block.timestamp + 1
        });

        vm.prank(operator);
        uint256 returnedProfit = executor.execute(plan);

        assertEq(returnedProfit, profit);
        assertEq(address(executor).balance, 0);
        assertEq(address(swapTarget).balance, ethAmount);
    }

    function testExecuteFillsNativeValueWithoutPatchingRouterCalldata() external {
        uint256 ethAmount = 3 ether;
        uint256 profit = 25 ether;
        _setProfitRecipient();
        vm.deal(address(swapTarget), ethAmount);

        // fillOffset=0 for native means "override msg.value only"; calldata keeps its encoded amount.
        bytes memory callData = abi.encodeCall(MockSwapTarget.spendETHAndMint, (ethAmount, token, profit));

        ArbitrageExecutor.RouterCall[] memory routers = new ArbitrageExecutor.RouterCall[](2);
        routers[0] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(MockSwapTarget.sendETH, (payable(address(executor)), ethAmount)),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });
        routers[1] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: callData,
            fillSource: ArbitrageExecutor.FillSource.NativeBalance,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: true,
            fillOffset: 0
        });

        ArbitrageExecutor.ExecutionPlan memory plan = ArbitrageExecutor.ExecutionPlan({
            loan: ArbitrageExecutor.FlashLoan({
                protocol: ArbitrageExecutor.FlashLoanProtocol.Balancer,
                lender: address(vault),
                token: address(token),
                amount: 100 ether,
                borrowToken0: true
            }),
            routers: routers,
            settleCurrencies: new address[](0),
            profitToken: address(token),
            minProfit: profit,
            deadline: block.timestamp + 1
        });

        vm.prank(operator);
        uint256 returnedProfit = executor.execute(plan);

        assertEq(returnedProfit, profit);
        assertEq(address(executor).balance, 0);
        assertEq(address(swapTarget).balance, ethAmount);
        assertEq(token.balanceOf(recipient), profit);
    }

    function testProfitExcludesRepaidFlashLoanPrincipalAndFee() external {
        vault.setFeeAmount(3 ether);
        _setProfitRecipient();
        ArbitrageExecutor.ExecutionPlan memory plan = _balancerPlan(28 ether, 25 ether);

        vm.prank(operator);
        uint256 returnedProfit = executor.execute(plan);

        assertEq(returnedProfit, 25 ether);
        assertEq(token.balanceOf(recipient), 25 ether);
        assertEq(token.balanceOf(address(vault)), 103 ether);
    }

    function testExecuteUniswapV4FlashLoanRepaysAndTransfersProfit() external {
        MockPoolManager manager = new MockPoolManager();
        uint256 loanAmount = 100 ether;
        uint256 profit = 25 ether;
        _setProfitRecipient();

        ArbitrageExecutor.RouterCall[] memory routers = new ArbitrageExecutor.RouterCall[](1);
        routers[0] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(MockSwapTarget.mintProfit, (token, address(executor), profit)),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });

        address[] memory settleCurrencies = new address[](1);
        settleCurrencies[0] = address(token);

        ArbitrageExecutor.ExecutionPlan memory plan = ArbitrageExecutor.ExecutionPlan({
            loan: ArbitrageExecutor.FlashLoan({
                protocol: ArbitrageExecutor.FlashLoanProtocol.UniswapV4,
                lender: address(manager),
                token: address(token),
                amount: loanAmount,
                borrowToken0: true
            }),
            routers: routers,
            settleCurrencies: settleCurrencies,
            profitToken: address(token),
            minProfit: profit,
            deadline: block.timestamp + 1
        });

        vm.prank(operator);
        uint256 returnedProfit = executor.execute(plan);

        assertEq(returnedProfit, profit);
        assertEq(token.balanceOf(recipient), profit);
        assertEq(token.balanceOf(address(manager)), loanAmount);
        assertEq(manager.deltaOf(address(executor), address(token)), 0);
    }

    function testExecuteUniswapV4NativeFlashLoanRepaysWithValue() external {
        MockPoolManager manager = new MockPoolManager();
        uint256 loanAmount = 50 ether;
        uint256 profit = 7 ether;
        _setProfitRecipient();
        vm.deal(address(manager), loanAmount);

        ArbitrageExecutor.RouterCall[] memory routers = new ArbitrageExecutor.RouterCall[](1);
        routers[0] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(MockSwapTarget.mintProfit, (token, address(executor), profit)),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });

        address[] memory settleCurrencies = new address[](1);
        settleCurrencies[0] = address(0);

        ArbitrageExecutor.ExecutionPlan memory plan = ArbitrageExecutor.ExecutionPlan({
            loan: ArbitrageExecutor.FlashLoan({
                protocol: ArbitrageExecutor.FlashLoanProtocol.UniswapV4,
                lender: address(manager),
                token: address(0),
                amount: loanAmount,
                borrowToken0: true
            }),
            routers: routers,
            settleCurrencies: settleCurrencies,
            profitToken: address(token),
            minProfit: profit,
            deadline: block.timestamp + 1
        });

        vm.prank(operator);
        uint256 returnedProfit = executor.execute(plan);

        assertEq(returnedProfit, profit);
        assertEq(token.balanceOf(recipient), profit);
        assertEq(address(manager).balance, loanAmount);
        assertEq(address(executor).balance, 0);
        assertEq(manager.deltaOf(address(executor), address(0)), 0);
    }

    function testExecuteUniswapV4ClearsExtraCurrencyDeltas() external {
        MockPoolManager manager = new MockPoolManager();
        MockERC20 other = new MockERC20("Other", "OTH");
        uint256 loanAmount = 100 ether;
        uint256 extraDebt = 5 ether;
        uint256 profit = 25 ether;
        _setProfitRecipient();

        ArbitrageExecutor.RouterCall[] memory routers = new ArbitrageExecutor.RouterCall[](2);
        routers[0] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(
                MockSwapTarget.forcePoolManagerDelta, (manager, address(executor), address(other), -int256(extraDebt))
            ),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });
        routers[1] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(MockSwapTarget.mintProfit, (token, address(executor), profit)),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });

        address[] memory settleCurrencies = new address[](2);
        settleCurrencies[0] = address(token);
        settleCurrencies[1] = address(other);

        ArbitrageExecutor.ExecutionPlan memory plan = ArbitrageExecutor.ExecutionPlan({
            loan: ArbitrageExecutor.FlashLoan({
                protocol: ArbitrageExecutor.FlashLoanProtocol.UniswapV4,
                lender: address(manager),
                token: address(token),
                amount: loanAmount,
                borrowToken0: true
            }),
            routers: routers,
            settleCurrencies: settleCurrencies,
            profitToken: address(token),
            minProfit: profit,
            deadline: block.timestamp + 1
        });

        vm.prank(operator);
        uint256 returnedProfit = executor.execute(plan);

        assertEq(returnedProfit, profit);
        assertEq(manager.deltaOf(address(executor), address(token)), 0);
        assertEq(manager.deltaOf(address(executor), address(other)), 0);
        assertEq(other.balanceOf(address(manager)), extraDebt);
    }

    function testExecuteUniswapV4RevertsWhenExtraDeltaNotListed() external {
        MockPoolManager manager = new MockPoolManager();
        MockERC20 other = new MockERC20("Other", "OTH");
        uint256 loanAmount = 100 ether;

        ArbitrageExecutor.RouterCall[] memory routers = new ArbitrageExecutor.RouterCall[](1);
        routers[0] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(
                MockSwapTarget.forcePoolManagerDelta, (manager, address(executor), address(other), -int256(5 ether))
            ),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });

        // Only list the loan token; the extra currency delta remains open.
        address[] memory settleCurrencies = new address[](1);
        settleCurrencies[0] = address(token);

        ArbitrageExecutor.ExecutionPlan memory plan = ArbitrageExecutor.ExecutionPlan({
            loan: ArbitrageExecutor.FlashLoan({
                protocol: ArbitrageExecutor.FlashLoanProtocol.UniswapV4,
                lender: address(manager),
                token: address(token),
                amount: loanAmount,
                borrowToken0: true
            }),
            routers: routers,
            settleCurrencies: settleCurrencies,
            profitToken: address(token),
            minProfit: 0,
            deadline: block.timestamp + 1
        });

        vm.prank(operator);
        vm.expectRevert(MockPoolManager.CurrencyNotSettled.selector);
        executor.execute(plan);
    }

    function _balancerPlan(uint256 mintedProfit, uint256 minProfit)
        private
        view
        returns (ArbitrageExecutor.ExecutionPlan memory)
    {
        ArbitrageExecutor.RouterCall[] memory routers = new ArbitrageExecutor.RouterCall[](1);
        routers[0] = ArbitrageExecutor.RouterCall({
            routerAddress: address(swapTarget),
            value: 0,
            data: abi.encodeCall(MockSwapTarget.mintProfit, (token, address(executor), mintedProfit)),
            fillSource: ArbitrageExecutor.FillSource.None,
            fillToken: address(0),
            patchAmount: false,
            amountAsCallValue: false,
            fillOffset: 0
        });

        return ArbitrageExecutor.ExecutionPlan({
            loan: ArbitrageExecutor.FlashLoan({
                protocol: ArbitrageExecutor.FlashLoanProtocol.Balancer,
                lender: address(vault),
                token: address(token),
                amount: 100 ether,
                borrowToken0: true
            }),
            routers: routers,
            settleCurrencies: new address[](0),
            profitToken: address(token),
            minProfit: minProfit,
            deadline: block.timestamp + 1
        });
    }

    function _setProfitRecipient() private {
        vm.prank(owner);
        executor.setProfitRecipient(recipient);
    }
}
