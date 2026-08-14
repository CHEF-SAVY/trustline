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

/// @notice Deploys the whole TrustLine stack to Coston2 in one transaction batch.
///
/// The ordering is forced by a circular dependency in FCC's registration flow:
///
///   - `ExtensionManager.register(stateVerifier, instructionsSender)` assigns the extension id,
///     so it needs both contract addresses up front.
///   - `UnderwritingVerifier` takes that extension id as an **immutable** constructor argument,
///     so it cannot exist before registration.
///
/// Resolved by registering with the instruction sender standing in as a placeholder state
/// verifier, then repointing with `setExtensionContracts` once the real verifier exists.
///
/// Registration is permissionless on Coston2 — verified by simulating `register` from an
/// unprivileged address, which returned an id instead of reverting. That means this script needs
/// no indexer credentials and no Docker stack; those are only required to run a TEE machine that
/// can actually answer instructions.
contract DeployScript is Script {
    /// @notice FlareTeeManager diamond on Coston2 — every TEE facet sits behind this one address.
    /// (The pre-2026-07-22 deployment at 0x004224fa…5d41F is dead; using it gives FunctionNotFound.)
    address public constant FLARE_TEE_MANAGER = 0x1a9C4A0f9D76c0b1D91d22E24E573a9b377618aE;

    /// @notice FXRP on Coston2, resolved via FlareContractRegistry -> AssetManagerFXRP -> fAsset().
    /// Symbol FTestXRP, and note it has **6 decimals**, not 18.
    address public constant FXRP = 0x0b6A3645c240605887a5532109323A3E12273dc7;

    /// @notice 50% — the standard overcollateralized path, mirroring FAssets norms.
    uint16 public constant STANDARD_LTV_BIPS = 5000;

    /// @notice 85% — must sit above the highest attainable attested LTV (7500), so a borrower is
    /// never liquidatable the instant they draw their full allowance.
    uint16 public constant LIQUIDATION_THRESHOLD_BIPS = 8500;

    /// @notice Reads the deployer key, accepting it with or without a `0x` prefix.
    /// @dev `vm.envUint` rejects a bare 64-char hex string, which is exactly how most wallets
    /// export a key. Normalising here avoids asking anyone to hand-edit a secret file to add two
    /// characters — a step that invites copy-paste mistakes with a private key.
    function _deployerKey() internal view returns (uint256) {
        string memory raw = vm.envString("DEPLOYER_PRIVATE_KEY");
        bytes memory b = bytes(raw);
        require(b.length > 0, "DEPLOYER_PRIVATE_KEY is empty");

        bool hasPrefix = b.length > 1 && b[0] == "0" && (b[1] == "x" || b[1] == "X");
        return vm.parseUint(hasPrefix ? raw : string.concat("0x", raw));
    }

    /// @notice Phase B. Deploys the verifier, registry and pool against an extension id that has
    /// already been registered and verified on-chain.
    ///
    /// This is split from `run()` because of a trap that only bites on a live, contended chain:
    /// `forge script` simulates the whole script, then broadcasts the resulting transaction list.
    /// Return values from calls like `register()` are taken from the SIMULATION, not from real
    /// execution. On our first deploy another developer registered the id our simulation had
    /// predicted, in the gap between simulating and broadcasting — so the verifier was built with
    /// an immutable id we did not own, and `setExtensionContracts` reverted.
    ///
    /// Passing the id in from the environment, after reading it back from the chain, removes the
    /// race entirely.
    function deployConsumers() external {
        uint256 pk = _deployerKey();
        address deployer = vm.addr(pk);
        uint256 extensionId = vm.envUint("TRUSTLINE_EXTENSION_ID");
        address sender = vm.envAddress("TRUSTLINE_SENDER");
        address asset = vm.envOr("TRUSTLINE_POOL_ASSET", FXRP);

        ITeeExtensionRegistry extReg = ITeeExtensionRegistry(FLARE_TEE_MANAGER);
        ITeeMachineRegistry machReg = ITeeMachineRegistry(FLARE_TEE_MANAGER);

        // Fail loudly and cheaply if the environment disagrees with the chain.
        require(extReg.getExtensionOwner(extensionId) == deployer, "deployer does not own extensionId");
        require(
            extReg.getTeeExtensionInstructionsSender(extensionId) == sender,
            "extensionId is not registered to this sender"
        );

        console2.log("extensionId (verified on-chain):", extensionId);

        vm.startBroadcast(pk);

        UnderwritingVerifier verifier = new UnderwritingVerifier(machReg, extensionId);
        console2.log("UnderwritingVerifier:", address(verifier));

        extReg.setExtensionContracts(extensionId, address(verifier), sender);

        CreditRegistry registry = new CreditRegistry(verifier);
        console2.log("CreditRegistry:", address(registry));

        TrustLinePool pool =
            new TrustLinePool(IERC20(asset), registry, STANDARD_LTV_BIPS, LIQUIDATION_THRESHOLD_BIPS);
        console2.log("TrustLinePool:", address(pool));

        vm.stopBroadcast();

        console2.log("");
        console2.log("--- paste into frontend/config.js ---");
        console2.log("creditRegistry:   ", address(registry));
        console2.log("trustLinePool:    ", address(pool));
        console2.log("instructionSender:", sender);
        console2.log("poolAsset:        ", asset);
        console2.log("extensionId:      ", extensionId);
    }

    /// @notice Phase A. Deploys the instruction sender and registers the extension.
    /// @dev Stop after this, read the assigned id off-chain, then run `deployConsumers()`.
    function run() external {
        uint256 pk = _deployerKey();
        address deployer = vm.addr(pk);

        // Allow overriding the pool asset; default to real FXRP.
        address asset = vm.envOr("TRUSTLINE_POOL_ASSET", FXRP);

        console2.log("deployer:", deployer);
        console2.log("balance :", deployer.balance);
        require(deployer.balance > 0.05 ether, "deployer has no gas");

        ITeeExtensionRegistry extReg = ITeeExtensionRegistry(FLARE_TEE_MANAGER);
        ITeeMachineRegistry machReg = ITeeMachineRegistry(FLARE_TEE_MANAGER);

        vm.startBroadcast(pk);

        // 1. The on-chain entry point. Needs only the registry addresses.
        TrustLineInstructionSender sender = new TrustLineInstructionSender(extReg, machReg);
        console2.log("TrustLineInstructionSender:", address(sender));

        // 2. Register the extension. The sender doubles as a placeholder state verifier so that
        //    register() has two non-zero addresses to store; step 5 corrects it.
        uint256 extensionId = extReg.register(address(sender), address(sender));
        console2.log("extensionId:", extensionId);

        // 3. Latch the id into the sender. Scans the registry for its own address, so it must run
        //    after registration.
        sender.setExtensionId();
        require(sender.extensionId() == extensionId, "extension id mismatch");

        // 4. Now the id is known, the verifier can be built against it.
        UnderwritingVerifier verifier = new UnderwritingVerifier(machReg, extensionId);
        console2.log("UnderwritingVerifier:", address(verifier));

        // 5. Repoint the extension's state verifier at the real contract.
        extReg.setExtensionContracts(extensionId, address(verifier), address(sender));

        // 6. The consumer contracts.
        CreditRegistry registry = new CreditRegistry(verifier);
        console2.log("CreditRegistry:", address(registry));

        TrustLinePool pool =
            new TrustLinePool(IERC20(asset), registry, STANDARD_LTV_BIPS, LIQUIDATION_THRESHOLD_BIPS);
        console2.log("TrustLinePool:", address(pool));

        vm.stopBroadcast();

        console2.log("");
        console2.log("--- paste into frontend/config.js ---");
        console2.log("creditRegistry:   ", address(registry));
        console2.log("trustLinePool:    ", address(pool));
        console2.log("instructionSender:", address(sender));
        console2.log("poolAsset:        ", asset);
        console2.log("extensionId:      ", extensionId);
    }
}
