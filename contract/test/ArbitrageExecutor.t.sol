// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import { Test } from "forge-std/Test.sol";
import { ERC20 } from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import {
    IERC20 as BalancerIERC20
} from "balancer-v2-monorepo/pkg/interfaces/contracts/solidity-utils/openzeppelin/IERC20.sol";
import { IFlashLoanRecipient } from "balancer-v2-monorepo/pkg/interfaces/contracts/vault/IFlashLoanRecipient.sol";
import { ArbitrageExecutor } from "../src/ArbitrageExecutor.sol";

contract MockERC20 is ERC20 {
    constructor(string memory name_, string memory symbol_) ERC20(name_, symbol_) { }

    function mint(address to, uint256 amount) external {
        _mint(to, amount);
    }
}

contract MockBalancerVault {
    function flashLoan(
        IFlashLoanRecipient recipient,
        BalancerIERC20[] calldata tokens,
        uint256[] calldata amounts,
        bytes calldata userData
    ) external {
        require(tokens.length == 1 && amounts.length == 1, "bad flash loan");
        MockERC20(address(tokens[0])).mint(address(recipient), amounts[0]);

        uint256[] memory fees = new uint256[](1);
        recipient.receiveFlashLoan(tokens, amounts, fees, userData);

        require(tokens[0].balanceOf(address(this)) == amounts[0], "flash loan not repaid");
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

    function testNonOperatorCannotExecute() external {
        ArbitrageExecutor.ExecutionPlan memory plan = _balancerPlan(25 ether, 1 ether);

        vm.expectRevert(ArbitrageExecutor.NotOperator.selector);
        executor.execute(plan);
    }

    function testExecuteBalancerFlashLoanTransfersProfit() external {
        uint256 minProfit = 20 ether;
        uint256 profit = 25 ether;
        ArbitrageExecutor.ExecutionPlan memory plan = _balancerPlan(profit, minProfit);

        vm.prank(operator);
        uint256 returnedProfit = executor.execute(plan);

        assertEq(returnedProfit, profit);
        assertEq(token.balanceOf(recipient), profit);
        assertEq(token.balanceOf(address(vault)), plan.loan.amount);
    }

    function testExecuteApprovesAllowanceTargetWithSafeERC20() external {
        uint256 pullAmount = 10 ether;
        uint256 mintedAmount = 35 ether;

        ArbitrageExecutor.SwapCall[] memory swaps = new ArbitrageExecutor.SwapCall[](1);
        swaps[0] = ArbitrageExecutor.SwapCall({
            target: address(swapTarget),
            allowanceTarget: address(swapTarget),
            tokenIn: address(token),
            amountIn: pullAmount,
            value: 0,
            data: abi.encodeCall(MockSwapTarget.pullAndMint, (token, pullAmount, mintedAmount))
        });

        ArbitrageExecutor.ExecutionPlan memory plan = ArbitrageExecutor.ExecutionPlan({
            loan: ArbitrageExecutor.FlashLoan({
                protocol: ArbitrageExecutor.FlashLoanProtocol.Balancer,
                lender: address(vault),
                token: address(token),
                amount: 100 ether,
                borrowToken0: true
            }),
            swaps: swaps,
            profitToken: address(token),
            minProfit: 20 ether,
            recipient: recipient,
            deadline: block.timestamp + 1
        });

        vm.prank(operator);
        uint256 returnedProfit = executor.execute(plan);

        assertEq(returnedProfit, 25 ether);
        assertEq(token.balanceOf(recipient), 25 ether);
        assertEq(token.balanceOf(address(swapTarget)), pullAmount);
        assertEq(token.allowance(address(executor), address(swapTarget)), type(uint256).max);
    }

    function testExecuteRevertsWhenProfitTooLow() external {
        ArbitrageExecutor.ExecutionPlan memory plan = _balancerPlan(1 ether, 2 ether);

        vm.prank(operator);
        vm.expectRevert(abi.encodeWithSelector(ArbitrageExecutor.ProfitTooLow.selector, 1 ether, 2 ether));
        executor.execute(plan);
    }

    function _balancerPlan(uint256 mintedProfit, uint256 minProfit)
        private
        view
        returns (ArbitrageExecutor.ExecutionPlan memory)
    {
        ArbitrageExecutor.SwapCall[] memory swaps = new ArbitrageExecutor.SwapCall[](1);
        swaps[0] = ArbitrageExecutor.SwapCall({
            target: address(swapTarget),
            allowanceTarget: address(0),
            tokenIn: address(0),
            amountIn: 0,
            value: 0,
            data: abi.encodeCall(MockSwapTarget.mintProfit, (token, address(executor), mintedProfit))
        });

        return ArbitrageExecutor.ExecutionPlan({
            loan: ArbitrageExecutor.FlashLoan({
                protocol: ArbitrageExecutor.FlashLoanProtocol.Balancer,
                lender: address(vault),
                token: address(token),
                amount: 100 ether,
                borrowToken0: true
            }),
            swaps: swaps,
            profitToken: address(token),
            minProfit: minProfit,
            recipient: recipient,
            deadline: block.timestamp + 1
        });
    }
}
