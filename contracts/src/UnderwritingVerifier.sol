// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import { ECDSA } from "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";
import { MessageHashUtils } from "@openzeppelin/contracts/utils/cryptography/MessageHashUtils.sol";
import { ITeeMachineRegistry } from "./interfaces/ITeeMachineRegistry.sol";

/// @title UnderwritingVerifier
/// @notice Verifies a TEE-signed credit attestation produced by the TrustLine Flare Compute Extension.
///
/// This contract reconstructs exactly the digest that `tee-node` signs and recovers the signing TEE
/// machine, then confirms with FCC's MachineManager that the signer is a PRODUCTION machine registered
/// to *our* extension. Trust rests entirely on the signature, so `verify` is a pure view — anyone may
/// relay an attestation and a hostile relayer cannot alter a tier.
///
/// Scheme (verified against tee-node v0.0.24 / go-flare-common v1.2.2, see docs/CONTRACTS.md §5):
///
///   innerHash = keccak256( keccak256(Data) || ID || keccak256(SubmissionTag) || Status )
///   digest    = keccak256( abi.encode( bytes32("TEE_ACTION_RESULT"), chainId, innerHash ) )
///   teeId     = ecrecover( toEthSignedMessageHash(digest), signature )
///
/// Note the deliberate asymmetry: innerHash is *packed*, the outer payload is *abi.encode*. Getting
/// this backwards recovers a wrong address silently rather than reverting.
contract UnderwritingVerifier {
    /// @notice Domain prefix the TEE signs under. Mirrors `TEEActionResult` in
    /// go-flare-common/pkg/signing/prefixes.go, whose comment reads "must be aligned with smart contracts".
    // casting to 'bytes32' is safe because the literal is 17 bytes, well under 32, and this
    // right-padded encoding is exactly what Go's mustStringBytes32 produces.
    // forge-lint: disable-next-line(unsafe-typecast)
    bytes32 public constant TEE_ACTION_RESULT_PREFIX = bytes32("TEE_ACTION_RESULT");

    /// @notice Submission tag on the success path. tee-node returns the extension's result under
    /// the "threshold" tag; "end" carries rewarding data, not our payload.
    bytes32 public constant SUBMISSION_TAG_THRESHOLD_HASH = keccak256(bytes("threshold"));

    /// @notice ActionResult.Status meaning success. 0 = error, >=2 = pending.
    uint8 public constant STATUS_SUCCESS = 1;

    /// @notice IMachineManager.TeeStatus value meaning PRODUCTION.
    uint8 public constant TEE_STATUS_PRODUCTION = 2;

    /// @notice FCC MachineManager facet (the FlareTeeManager diamond).
    ITeeMachineRegistry public immutable TEE_MACHINE_REGISTRY;

    /// @notice Extension id this verifier accepts attestations for.
    uint256 public immutable EXTENSION_ID;

    /// @notice The credit attestation the TEE signs. Encoding is fixed — see docs/CONTRACTS.md §4.
    struct CreditAttestation {
        address borrower;
        bytes32 xrplAddressHash;
        uint8 riskTier;
        uint16 maxLTVBips;
        uint64 issuedAt;
        uint64 expiry;
    }

    error InvalidStatus(uint8 status);
    error UnknownTeeMachine(address teeId);
    error WrongExtension(address teeId, uint256 extensionId);
    error TeeMachineNotInProduction(address teeId, uint8 status);
    error InvalidRiskTier(uint8 riskTier);
    error InvalidLTV(uint16 maxLTVBips);
    error ExpiryBeforeIssuance(uint64 issuedAt, uint64 expiry);

    constructor(ITeeMachineRegistry _teeMachineRegistry, uint256 _extensionId) {
        require(address(_teeMachineRegistry) != address(0), "TeeMachineRegistry cannot be zero address");
        require(address(_teeMachineRegistry).code.length > 0, "TeeMachineRegistry has no code");
        require(_extensionId != 0, "Extension ID cannot be zero");
        TEE_MACHINE_REGISTRY = _teeMachineRegistry;
        EXTENSION_ID = _extensionId;
    }

    /// @notice Reconstructs `ActionResult.Hash()` from tee-node.
    /// @dev keccak256(abi.encodePacked(keccak256(data), id, keccak256(tag), status)) — 97 bytes.
    /// Mirrors tee-node/pkg/types/actions.go:62.
    function actionResultHash(bytes calldata _data, bytes32 _id, bytes32 _submissionTagHash, uint8 _status)
        public
        pure
        returns (bytes32)
    {
        return keccak256(abi.encodePacked(keccak256(_data), _id, _submissionTagHash, _status));
    }

    /// @notice Reconstructs the digest the TEE signs, before the EIP-191 wrapper.
    /// @dev keccak256(abi.encode(prefix, chainId, dataHash)) — mirrors go-flare-common
    /// pkg/signing/hash.go:52. Uses block.chainid, so a signature cannot cross networks.
    function signingDigest(bytes32 _innerHash) public view returns (bytes32) {
        return keccak256(abi.encode(TEE_ACTION_RESULT_PREFIX, block.chainid, _innerHash));
    }

    /// @notice Recovers the TEE machine that signed an action result.
    /// @dev tee-node signs via crypto.Sign(accounts.TextHash(hash)), i.e. EIP-191 prefixed.
    /// OZ's ECDSA.recover rejects high-s signatures, matching CheckCanonicalSignature Go-side.
    function recoverTeeId(bytes calldata _data, bytes32 _id, uint8 _status, bytes calldata _signature)
        public
        view
        returns (address)
    {
        bytes32 innerHash = actionResultHash(_data, _id, SUBMISSION_TAG_THRESHOLD_HASH, _status);
        bytes32 digest = signingDigest(innerHash);
        return ECDSA.recover(MessageHashUtils.toEthSignedMessageHash(digest), _signature);
    }

    /// @notice Confirms `_teeId` is a PRODUCTION machine registered to this extension.
    /// @dev MachineManager reverts with TeeNotFound() for unregistered addresses, which is the
    /// common case for a forged signature. try/catch converts that into UnknownTeeMachine.
    function assertRegisteredTeeMachine(address _teeId) public view {
        uint256 extensionId;
        try TEE_MACHINE_REGISTRY.getExtensionId(_teeId) returns (uint256 id) {
            extensionId = id;
        } catch {
            revert UnknownTeeMachine(_teeId);
        }
        if (extensionId != EXTENSION_ID) revert WrongExtension(_teeId, extensionId);

        uint8 status;
        try TEE_MACHINE_REGISTRY.getTeeMachineStatus(_teeId) returns (uint8 s) {
            status = s;
        } catch {
            revert UnknownTeeMachine(_teeId);
        }
        if (status != TEE_STATUS_PRODUCTION) revert TeeMachineNotInProduction(_teeId, status);
    }

    /// @notice Full verification: signature, signer provenance, and payload sanity.
    /// @dev Deliberately does NOT check expiry — freshness is the registry's policy decision, and
    /// keeping it out means an attestation can still be verified for display after it lapses.
    /// @param _data ABI-encoded CreditAttestation (ActionResult.Data).
    /// @param _id Instruction id assigned by the TEE extension registry.
    /// @param _status ActionResult.Status; must be STATUS_SUCCESS.
    /// @param _signature 65-byte TEE signature.
    /// @return attestation The decoded, validated attestation.
    /// @return teeId The TEE machine that signed it.
    function verifyAttestation(bytes calldata _data, bytes32 _id, uint8 _status, bytes calldata _signature)
        external
        view
        returns (CreditAttestation memory attestation, address teeId)
    {
        if (_status != STATUS_SUCCESS) revert InvalidStatus(_status);

        teeId = recoverTeeId(_data, _id, _status, _signature);
        assertRegisteredTeeMachine(teeId);

        attestation = abi.decode(_data, (CreditAttestation));

        if (attestation.riskTier > 3) revert InvalidRiskTier(attestation.riskTier);
        if (attestation.maxLTVBips > 10_000) revert InvalidLTV(attestation.maxLTVBips);
        if (attestation.expiry <= attestation.issuedAt) {
            revert ExpiryBeforeIssuance(attestation.issuedAt, attestation.expiry);
        }
    }
}
