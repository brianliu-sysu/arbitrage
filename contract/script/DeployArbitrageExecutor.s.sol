// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import { Script, console2 } from "forge-std/Script.sol";
import { ArbitrageExecutor } from "../src/ArbitrageExecutor.sol";

contract DeployArbitrageExecutor is Script {
    function run() external returns (ArbitrageExecutor executor) {
        uint256 deployerPrivateKey = vm.envUint("PRIVATE_KEY");
        address deployer = vm.addr(deployerPrivateKey);
        address owner = vm.envOr("ARBITRAGE_EXECUTOR_OWNER", deployer);

        vm.startBroadcast(deployerPrivateKey);
        executor = new ArbitrageExecutor(owner);
        vm.stopBroadcast();

        console2.log("ArbitrageExecutor deployed at", address(executor));
        console2.log("Owner", owner);
    }
}
