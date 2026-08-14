// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import { UnderwritingVerifier } from "./UnderwritingVerifier.sol";

/// @title CreditRegistry
/// @notice Stores the latest valid TEE-signed credit attestation per borrower.
///
/// This contract closes the gap in FCC's flow: tee-node signs a result and ext-proxy serves it at
/// /action/result, but nothing carries it on-chain. `submitAttestation` is that missing step, and it
/// is deliberately **permissionless** — trust rests on the TEE signature, so it does not matter who
/// pays the gas. A borrower, a keeper, or the protocol can all relay, and none of them can alter a
/// tier without invalidating the signature.
contract CreditRegistry {
    /// @notice A stored attestation plus the provenance of how it arrived.
    struct StoredAttestation {
        uint8 riskTier;
        uint16 maxLTVBips;
        uint64 issuedAt;
        uint64 expiry;
        bytes32 xrplAddressHash;
        address teeId;
        bytes32 instructionId;
    }

    UnderwritingVerifier public immutable VERIFIER;

    /// @notice Latest attestation per borrower.
    mapping(address => StoredAttestation) internal _attestations;

    /// @notice Instruction ids already consumed, to stop replay of a valid signature.
    mapping(bytes32 => bool) public consumedInstructionIds;

    event CreditAttested(
        address indexed borrower,
        uint8 riskTier,
        uint256 maxLTV,
        uint256 expiry,
        address indexed teeId,
        bytes32 indexed instructionId
    );

    error AttestationReplayed(bytes32 instructionId);
    error AttestationExpired(uint64 expiry, uint256 nowTs);
    error AttestationNotNewer(uint64 storedIssuedAt, uint64 incomingIssuedAt);

    constructor(UnderwritingVerifier _verifier) {
        require(address(_verifier) != address(0), "Verifier cannot be zero address");
        VERIFIER = _verifier;
    }

    /// @notice Verifies a TEE-signed attestation and records it. Callable by anyone.
    /// @param _data ABI-encoded CreditAttestation, exactly as returned in ActionResult.Data.
    /// @param _id Instruction id from the TEE extension registry.
    /// @param _status ActionResult.Status; must be 1 (success).
    /// @param _signature 65-byte TEE signature.
    function submitAttestation(bytes calldata _data, bytes32 _id, uint8 _status, bytes calldata _signature)
        external
    {
        // Reverts on a bad signature, an unregistered/non-production signer, or a malformed payload.
        (UnderwritingVerifier.CreditAttestation memory att, address teeId) =
            VERIFIER.verifyAttestation(_data, _id, _status, _signature);

        // Each instruction may be settled once. Without this a valid signature could be re-submitted
        // to resurrect an old tier after a newer, worse one has landed.
        if (consumedInstructionIds[_id]) revert AttestationReplayed(_id);

        // Refuse attestations that are already stale on arrival.
        if (att.expiry <= block.timestamp) revert AttestationExpired(att.expiry, block.timestamp);

        // Monotonicity: never let an older attestation overwrite a newer one. Instructions can be
        // relayed out of order, and without this a borrower could hold back a stale favourable
        // attestation and replay it after their credit worsens.
        StoredAttestation storage existing = _attestations[att.borrower];
        if (existing.issuedAt != 0 && att.issuedAt <= existing.issuedAt) {
            revert AttestationNotNewer(existing.issuedAt, att.issuedAt);
        }

        consumedInstructionIds[_id] = true;
        _attestations[att.borrower] = StoredAttestation({
            riskTier: att.riskTier,
            maxLTVBips: att.maxLTVBips,
            issuedAt: att.issuedAt,
            expiry: att.expiry,
            xrplAddressHash: att.xrplAddressHash,
            teeId: teeId,
            instructionId: _id
        });

        emit CreditAttested(att.borrower, att.riskTier, att.maxLTVBips, att.expiry, teeId, _id);
    }

    /// @notice Full stored attestation, whether or not it is still valid.
    function getAttestation(address _borrower) external view returns (StoredAttestation memory) {
        return _attestations[_borrower];
    }

    /// @notice True if `_borrower` has an unexpired attestation.
    function hasValidAttestation(address _borrower) public view returns (bool) {
        StoredAttestation storage a = _attestations[_borrower];
        return a.issuedAt != 0 && a.expiry > block.timestamp;
    }

    /// @notice Underwritten max LTV in bips, or 0 if there is no valid attestation.
    /// @dev Returning 0 rather than reverting lets the pool fall back to its standard
    /// overcollateralized path without a try/catch.
    function currentMaxLTVBips(address _borrower) external view returns (uint16) {
        if (!hasValidAttestation(_borrower)) return 0;
        return _attestations[_borrower].maxLTVBips;
    }

    /// @notice Current risk tier, or 0 if there is no valid attestation.
    function currentRiskTier(address _borrower) external view returns (uint8) {
        if (!hasValidAttestation(_borrower)) return 0;
        return _attestations[_borrower].riskTier;
    }
}
