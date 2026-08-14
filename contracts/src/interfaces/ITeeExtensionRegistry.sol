// SPDX-License-Identifier: MIT
pragma solidity >=0.7.6 <0.9;

// TODO: Replace this minimal interface with the full import once flare-smart-contracts-v2
// is published as a package:
//   import { ITeeExtensionRegistry } from "flare-smart-contracts-v2/contracts/userInterfaces/tee/ITeeExtensionRegistry.sol";
interface ITeeExtensionRegistry {
    struct TeeInstructionParams {
        bytes32 opType;
        bytes32 opCommand;
        bytes message;
        address[] cosigners;
        uint64 cosignersThreshold;
        address claimBackAddress;
    }

    function sendInstructions(
        address[] calldata _teeIds,
        TeeInstructionParams calldata _instructionParams
    ) external payable returns (bytes32 _instructionId);

    function nextPublicExtensionId() external view returns (uint256);

    function getTeeExtensionInstructionsSender(uint256 _extensionId)
        external view returns (address);

    /// @notice Registers a new extension and returns its assigned id.
    /// @dev Permissionless on Coston2 — confirmed by simulating the call from an
    /// unprivileged address, which returned an id rather than reverting. This is what lets us
    /// register without running the proxy/indexer stack.
    function register(address _teeExtensionStateVerifier, address _teeExtensionInstructionsSender)
        external returns (uint256 _extensionId);

    /// @notice Repoints an extension's two registered contracts.
    /// @dev Needed to resolve a chicken-and-egg: `register` wants a state verifier address, but
    /// UnderwritingVerifier takes the extension id as an immutable constructor argument. So we
    /// register with a placeholder, then repoint here once the real verifier is deployed.
    function setExtensionContracts(
        uint256 _extensionId,
        address _teeExtensionStateVerifier,
        address _teeExtensionInstructionsSender
    ) external;

    function getTeeExtensionStateVerifier(uint256 _extensionId) external view returns (address);

    function getExtensionOwner(uint256 _extensionId) external view returns (address);
}
