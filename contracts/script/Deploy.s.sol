// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import { Script } from "forge-std/Script.sol";
import { console2 } from "forge-std/console2.sol";
import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { UnderwritingVerifier } from "../src/UnderwritingVerifier.sol";
import { CreditRegistry } from "../src/CreditRegistry.sol";
import { TrustLinePool } from "../src/TrustLinePool.sol";
import { TrustLineInstructionSender } from "../src/TrustLineInstructionSender.sol";
import { ITeeExtensionRegistry } from "../src/interfaces/ITeeExtensionRegistry.sol";
import { ITeeMachineRegistry } from "../src/interfaces/ITeeMachineRegistry.sol";

/// @notice Deploys TrustLine to Coston2.
///
/// Ordering is forced by a chicken-and-egg in FCC's registration flow:
///   1. Deploy TrustLineInstructionSender (needs only the diamond address).
///   2. Register the extension off-chain via the scaffold tooling, passing this sender — that call
///      assigns the extensionId.
///   3. Call `setExtensionId()` on the sender to latch it.
///   4. Deploy UnderwritingVerifier with that id, then CreditRegistry and TrustLinePool.
///
/// So this script runs in two passes. Pass 1 (`deploySender`) before registration, pass 2 (`run`)
/// after, once TRUSTLINE_EXTENSION_ID is known.
contract DeployScript is Script {
    /// @notice FlareTeeManager diamond on Coston2 — every TEE facet lives behind this address.
    address public constant FLARE_TEE_MANAGER = 0x1a9C4A0f9D76c0b1D91d22E24E573a9b377618aE;

    /// @notice 50% — the standard overcollateralized path, mirroring FAssets norms.
    uint16 public constant STANDARD_LTV_BIPS = 5000;

    /// @notice 85% — must exceed the highest attainable attested LTV (7500) so a borrower is never
    /// liquidatable the moment they draw their full allowance.
    uint16 public constant LIQUIDATION_THRESHOLD_BIPS = 8500;

    /// @notice Pass 1: deploy the instruction sender so it can be registered as an FCC extension.
    function deploySender() external returns (TrustLineInstructionSender sender) {
        uint256 pk = vm.envUint("DEPLOYER_PRIVATE_KEY");
        vm.startBroadcast(pk);
        sender = new TrustLineInstructionSender(
            ITeeExtensionRegistry(FLARE_TEE_MANAGER), ITeeMachineRegistry(FLARE_TEE_MANAGER)
        );
        vm.stopBroadcast();

        console2.log("TrustLineInstructionSender:", address(sender));
        console2.log("Next: register this address as an FCE, then call setExtensionId().");
    }

    /// @notice Pass 2: deploy the verifier, registry and pool against a known extension id.
    function run() external returns (UnderwritingVerifier verifier, CreditRegistry registry, TrustLinePool pool) {
        uint256 pk = vm.envUint("DEPLOYER_PRIVATE_KEY");
        uint256 extensionId = vm.envUint("TRUSTLINE_EXTENSION_ID");
        address asset = vm.envAddress("TRUSTLINE_POOL_ASSET");

        vm.startBroadcast(pk);
        verifier = new UnderwritingVerifier(ITeeMachineRegistry(FLARE_TEE_MANAGER), extensionId);
        registry = new CreditRegistry(verifier);
        pool = new TrustLinePool(IERC20(asset), registry, STANDARD_LTV_BIPS, LIQUIDATION_THRESHOLD_BIPS);
        vm.stopBroadcast();

        console2.log("UnderwritingVerifier:", address(verifier));
        console2.log("CreditRegistry:      ", address(registry));
        console2.log("TrustLinePool:       ", address(pool));
        console2.log("extensionId:         ", extensionId);
    }
}
