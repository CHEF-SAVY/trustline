// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import { Script } from "forge-std/Script.sol";
import { console2 } from "forge-std/console2.sol";

import { DemoTeeMachineRegistry } from "../src/demo/DemoTeeMachineRegistry.sol";
import { UnderwritingVerifier } from "../src/UnderwritingVerifier.sol";
import { CreditRegistry } from "../src/CreditRegistry.sol";
import { TrustLinePool } from "../src/TrustLinePool.sol";
import { ITeeMachineRegistry } from "../src/interfaces/ITeeMachineRegistry.sol";
import { ITeeExtensionRegistry } from "../src/interfaces/ITeeExtensionRegistry.sol";
import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";

/// @notice Redeploys the consumer stack against `DemoTeeMachineRegistry` so the end-to-end flow can
/// be demonstrated while FCC's `toProduction` is broken upstream. See DemoTeeMachineRegistry for
/// exactly what is relaxed (one status check) and what stays real (everything else).
///
/// The extension registration itself is NOT touched — extension 66285, its instruction sender and
/// the TEE machine on-chain are all reused as-is. This only redeploys the three consumer contracts,
/// which take their dependencies as immutables and so cannot be repointed in place.
///
///   forge script script/DeployDemo.s.sol:DeployDemoScript --rpc-url coston2 --broadcast
contract DeployDemoScript is Script {
    address public constant FLARE_TEE_MANAGER = 0x1a9C4A0f9D76c0b1D91d22E24E573a9b377618aE;
    address public constant FXRP = 0x0b6A3645c240605887a5532109323A3E12273dc7;

    uint16 public constant STANDARD_LTV_BIPS = 5000;
    uint16 public constant LIQUIDATION_THRESHOLD_BIPS = 8500;

    /// @dev `vm.envUint` rejects a bare 64-char hex string; accept both forms.
    function _deployerKey() internal view returns (uint256) {
        try vm.envUint("DEPLOYER_PRIVATE_KEY") returns (uint256 k) {
            return k;
        } catch {
            return vm.parseUint(string.concat("0x", vm.envString("DEPLOYER_PRIVATE_KEY")));
        }
    }

    function run() external {
        uint256 pk = _deployerKey();
        address deployer = vm.addr(pk);
        uint256 extensionId = vm.envUint("TRUSTLINE_EXTENSION_ID");
        address sender = vm.envAddress("TRUSTLINE_SENDER");
        address asset = vm.envOr("TRUSTLINE_POOL_ASSET", FXRP);

        ITeeExtensionRegistry extReg = ITeeExtensionRegistry(FLARE_TEE_MANAGER);

        // Same guards as the real deploy: fail cheaply if env and chain disagree.
        require(extReg.getExtensionOwner(extensionId) == deployer, "deployer does not own extensionId");
        require(
            extReg.getTeeExtensionInstructionsSender(extensionId) == sender,
            "extensionId is not registered to this sender"
        );

        console2.log("deployer   :", deployer);
        console2.log("extensionId:", extensionId);

        vm.startBroadcast(pk);

        DemoTeeMachineRegistry demoReg =
            new DemoTeeMachineRegistry(ITeeMachineRegistry(FLARE_TEE_MANAGER));
        console2.log("DemoTeeMachineRegistry:", address(demoReg));

        UnderwritingVerifier verifier =
            new UnderwritingVerifier(ITeeMachineRegistry(address(demoReg)), extensionId);
        console2.log("UnderwritingVerifier  :", address(verifier));

        // Repoint the extension's state verifier at the new one, so the on-chain wiring stays
        // consistent with what actually verifies attestations.
        extReg.setExtensionContracts(extensionId, address(verifier), sender);

        CreditRegistry registry = new CreditRegistry(verifier);
        console2.log("CreditRegistry        :", address(registry));

        TrustLinePool pool =
            new TrustLinePool(IERC20(asset), registry, STANDARD_LTV_BIPS, LIQUIDATION_THRESHOLD_BIPS);
        console2.log("TrustLinePool         :", address(pool));

        vm.stopBroadcast();

        console2.log("");
        console2.log("--- paste into frontend/config.js ---");
        console2.log("creditRegistry:   ", address(registry));
        console2.log("trustLinePool:    ", address(pool));
        console2.log("instructionSender:", sender);
        console2.log("poolAsset:        ", asset);
    }
}
