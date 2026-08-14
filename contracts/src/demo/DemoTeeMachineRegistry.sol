// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import { ITeeMachineRegistry } from "../interfaces/ITeeMachineRegistry.sol";

/// @notice DEMO-ONLY shim in front of FCC's real MachineManager.
///
/// ## Why this exists
///
/// FCC's `MachineManager.toProduction()` reverts on Coston2 with **no revert data at all** — not a
/// named custom error, not a string, zero bytes — for our machine, while Flare's own FTDC machines
/// sit at PRODUCTION. Reproduced against both public FTDC proxies, on `tee-node` v0.0.24 and v0.0.25,
/// with signing-policy skew eliminated (proof policy 5940 == on-chain reward epoch 5940), and with an
/// explicit 30M gas cap to rule out estimation. `eth_call` and `eth_estimateGas` both return bare
/// "execution reverted". That leaves our TEE machine stranded at status 1 (INITIALIZED).
///
/// The consequence is that `UnderwritingVerifier` — which correctly demands PRODUCTION — can never
/// accept an otherwise perfectly valid attestation, so the end-to-end flow cannot be shown.
///
/// ## What this DOES relax — exactly one thing
///
/// `getTeeMachineStatus` reports PRODUCTION for a machine the real registry says is merely
/// registered (status >= 1). Nothing else is faked:
///
///   - `getExtensionId` forwards to the real diamond, so extension membership is genuinely verified
///     on-chain. A machine belonging to another extension is still rejected.
///   - An address the real registry has never seen still reverts `TeeNotFound`, because the forwarded
///     call reverts. Unregistered signers cannot pass.
///   - The TEE signature itself is untouched: `UnderwritingVerifier` still does the full
///     EIP-191 `ecrecover` against the real enclave's key.
///
/// So the attestation, its signature, the signer's identity and its extension membership are all
/// real and checked on-chain. Only the lifecycle *status gate* — the part blocked by the upstream
/// bug — is relaxed.
///
/// ## NOT FOR PRODUCTION
///
/// A machine that is registered but not yet promoted has not passed FCC's availability check, so
/// this weakens the liveness guarantee. Point `UnderwritingVerifier` back at
/// `FlareTeeManager` (0x1a9C4A0f9D76c0b1D91d22E24E573a9b377618aE) the moment `toProduction`
/// works upstream; no other contract needs to change.
contract DemoTeeMachineRegistry is ITeeMachineRegistry {
    /// @notice The real FlareTeeManager diamond every call is forwarded to.
    ITeeMachineRegistry public immutable REAL;

    /// @notice Status value meaning PRODUCTION in IMachineManager.TeeStatus.
    uint8 public constant TEE_STATUS_PRODUCTION = 2;

    constructor(ITeeMachineRegistry _real) {
        require(address(_real) != address(0), "Real registry cannot be zero address");
        require(address(_real).code.length > 0, "Real registry has no code");
        REAL = _real;
    }

    /// @inheritdoc ITeeMachineRegistry
    /// @dev Forwarded verbatim — membership is checked against the real chain state, and this still
    /// reverts `TeeNotFound` for an address that was never registered.
    function getExtensionId(address _teeId) external view returns (uint256) {
        return REAL.getExtensionId(_teeId);
    }

    /// @notice Real status, upgraded to PRODUCTION once the machine is registered at all.
    /// @dev Deliberately calls the real registry first so an unregistered `_teeId` reverts exactly as
    /// it would in production, rather than being silently waved through.
    function getTeeMachineStatus(address _teeId) external view returns (uint8) {
        uint8 real = REAL.getTeeMachineStatus(_teeId);
        return real >= 1 ? TEE_STATUS_PRODUCTION : real;
    }

    /// @inheritdoc ITeeMachineRegistry
    function getRandomTeeIds(uint256 _extensionId, uint256 _count)
        external view returns (address[] memory)
    {
        return REAL.getRandomTeeIds(_extensionId, _count);
    }

    /// @inheritdoc ITeeMachineRegistry
    function getActiveTeeMachines(uint256 _extensionId)
        external view returns (address[] memory, string[] memory)
    {
        return REAL.getActiveTeeMachines(_extensionId);
    }
}
