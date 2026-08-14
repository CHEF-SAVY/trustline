// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import { ITeeExtensionRegistry } from "./interfaces/ITeeExtensionRegistry.sol";
import { ITeeMachineRegistry } from "./interfaces/ITeeMachineRegistry.sol";

/// @title TrustLineInstructionSender
/// @notice On-chain entry point that asks the TrustLine TEE extension to score a borrower.
///
/// Registered with FCC as the extension's `instructionsSender`. Structure follows
/// fce-extension-scaffold's HelloWorldInstructionSender: the constructor, `setExtensionId()` and
/// `_getExtensionId()` are marked DO NOT MODIFY upstream and are reproduced unchanged.
contract TrustLineInstructionSender {
    /// @notice Operation type for TrustLine actions.
    /// @dev Must be byte-identical to the Go config constant and the handler's routing switch.
    // casting to 'bytes32' is safe because the literal is 9 bytes and this right-padded encoding
    // is exactly what Go's mustStringBytes32 produces.
    // forge-lint: disable-next-line(unsafe-typecast)
    bytes32 public constant OP_TYPE_TRUSTLINE = bytes32("TRUSTLINE");

    /// @notice Command asking the TEE to score a borrower's XRPL history.
    // casting to 'bytes32' is safe because the literal is 14 bytes; see above.
    // forge-lint: disable-next-line(unsafe-typecast)
    bytes32 public constant OP_COMMAND_SCORE_BORROWER = bytes32("SCORE_BORROWER");

    ITeeExtensionRegistry public immutable TEE_EXTENSION_REGISTRY;
    ITeeMachineRegistry public immutable TEE_MACHINE_REGISTRY;

    /// @notice Number of TEE machines to address per request.
    /// @dev One machine is enough for a hackathon MVP. Raising this is the path to a k-of-n quorum,
    /// which would need the verifier to require multiple independent signatures.
    uint256 public constant TEE_MACHINE_COUNT = 1;

    /// @notice First public extension ID; the registry reserves everything below it.
    uint256 private constant FIRST_PUBLIC_EXTENSION_ID = 0x10000; // 65536

    uint256 private _extensionId;

    /// @notice Instruction payload sent to the TEE. Mirrors ScoreRequest in docs/CONTRACTS.md §3.
    struct ScoreRequest {
        address borrower;
        string xrplAddress;
    }

    event CreditCheckRequested(
        address indexed borrower, bytes32 indexed instructionId, string xrplAddress, uint256 fee
    );

    error ExtensionIdNotSet();
    error EmptyXrplAddress();
    error NoTeeMachinesAvailable();

    /// DO NOT MODIFY.
    constructor(ITeeExtensionRegistry _teeExtensionRegistry, ITeeMachineRegistry _teeMachineRegistry) {
        require(address(_teeExtensionRegistry) != address(0), "TeeExtensionRegistry cannot be zero address");
        require(address(_teeMachineRegistry) != address(0), "TeeMachineRegistry cannot be zero address");
        require(address(_teeExtensionRegistry).code.length > 0, "TeeExtensionRegistry has no code");
        require(address(_teeMachineRegistry).code.length > 0, "TeeMachineRegistry has no code");
        TEE_EXTENSION_REGISTRY = _teeExtensionRegistry;
        TEE_MACHINE_REGISTRY = _teeMachineRegistry;
    }

    /// @notice Finds and latches this contract's extension id. Can only be set once.
    /// DO NOT MODIFY this function.
    function setExtensionId() external {
        require(_extensionId == 0, "Extension ID already set.");

        uint256 c = TEE_EXTENSION_REGISTRY.nextPublicExtensionId();
        for (uint256 i = FIRST_PUBLIC_EXTENSION_ID; i < c; ++i) {
            if (TEE_EXTENSION_REGISTRY.getTeeExtensionInstructionsSender(i) == address(this)) {
                _extensionId = i;
                return;
            }
        }
        revert("Extension ID not found.");
    }

    /// @notice This contract's registered extension id.
    function extensionId() external view returns (uint256) {
        return _extensionId;
    }

    /// @notice Requests a confidential credit assessment of `_xrplAddress` for `msg.sender`.
    /// @dev Payable — the registry charges an instruction fee. Unused value is refunded to
    /// `claimBackAddress`, set to the caller.
    ///
    /// The borrower is bound to `msg.sender` rather than being a parameter, so nobody can request an
    /// attestation that lands on someone else's address.
    ///
    /// PRIVACY: `_xrplAddress` is visible in this event — the TEE must be told what to score. What
    /// stays confidential is the transaction history and every derived feature. See CONTRACTS.md §7.
    function requestCreditCheck(string calldata _xrplAddress)
        external
        payable
        returns (bytes32 instructionId)
    {
        if (_extensionId == 0) revert ExtensionIdNotSet();
        if (bytes(_xrplAddress).length == 0) revert EmptyXrplAddress();

        address[] memory teeIds = TEE_MACHINE_REGISTRY.getRandomTeeIds(_extensionId, TEE_MACHINE_COUNT);
        if (teeIds.length == 0) revert NoTeeMachinesAvailable();

        bytes memory message = abi.encode(ScoreRequest({ borrower: msg.sender, xrplAddress: _xrplAddress }));

        ITeeExtensionRegistry.TeeInstructionParams memory params = ITeeExtensionRegistry
            .TeeInstructionParams({
            opType: OP_TYPE_TRUSTLINE,
            opCommand: OP_COMMAND_SCORE_BORROWER,
            message: message,
            cosigners: new address[](0),
            cosignersThreshold: 0,
            claimBackAddress: msg.sender
        });

        instructionId = TEE_EXTENSION_REGISTRY.sendInstructions{ value: msg.value }(teeIds, params);
        emit CreditCheckRequested(msg.sender, instructionId, _xrplAddress, msg.value);
    }
}
