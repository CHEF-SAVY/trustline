// SPDX-License-Identifier: MIT
pragma solidity >=0.7.6 <0.9;

/// @notice Minimal view of FCC's MachineManagerFacet, exposed on the FlareTeeManager diamond
/// (Coston2: 0x1a9C4A0f9D76c0b1D91d22E24E573a9b377618aE).
///
/// Base copied from fce-extension-scaffold/contracts/interfaces/ITeeMachineRegistry.sol and extended
/// with the two lookups UnderwritingVerifier needs. Signatures checked against the MachineManager ABI
/// in go-flare-common v1.2.2 (pkg/contracts/tee/machinemanager).
///
/// TODO: replace with the upstream import once flare-smart-contracts-v2 ships as a package.
interface ITeeMachineRegistry {
    /// @notice Random subset of TEE machines registered to an extension. Used when sending instructions.
    function getRandomTeeIds(uint256 _extensionId, uint256 _count)
        external view returns (address[] memory);

    /// @notice Extension a TEE machine is registered to.
    /// @dev REVERTS with TeeNotFound() (0xceb05b68) if `_teeId` was never registered — confirmed
    /// empirically on Coston2. Callers must use try/catch.
    function getExtensionId(address _teeId) external view returns (uint256);

    /// @notice Lifecycle status of a TEE machine, as enum IMachineManager.TeeStatus.
    /// @dev 2 == PRODUCTION (verified against four live Coston2 machines). Anything else is not
    /// trustworthy. REVERTS with TeeNotFound() for unregistered addresses — use try/catch.
    function getTeeMachineStatus(address _teeId) external view returns (uint8);

    /// @notice Active machines for an extension, with their proxy URLs. Used by the frontend to
    /// locate a proxy to poll for results.
    function getActiveTeeMachines(uint256 _extensionId)
        external view returns (address[] memory _teeIds, string[] memory _urls);
}
