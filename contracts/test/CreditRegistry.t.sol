// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import { Test } from "forge-std/Test.sol";
import { CreditRegistry } from "../src/CreditRegistry.sol";
import { UnderwritingVerifier } from "../src/UnderwritingVerifier.sol";
import { ITeeMachineRegistry } from "../src/interfaces/ITeeMachineRegistry.sol";
import { MockMachineRegistry } from "./UnderwritingVerifier.t.sol";

/// @notice Registry tests driven by real TEE-signed fixtures, so the signature path is exercised
/// end-to-end rather than stubbed.
contract CreditRegistryTest is Test {
    uint256 internal constant EXTENSION_ID = 66_000;
    uint256 internal constant COSTON2_CHAIN_ID = 114;

    CreditRegistry internal registryContract;
    UnderwritingVerifier internal verifier;
    MockMachineRegistry internal machines;

    // tier3 fixture, signed by the real Go implementation.
    bytes internal data;
    bytes32 internal id;
    uint8 internal status;
    bytes internal signature;
    address internal teeId;
    address internal borrower;
    uint64 internal issuedAt;
    uint64 internal expiry;

    function setUp() public {
        vm.chainId(COSTON2_CHAIN_ID);
        machines = new MockMachineRegistry();
        verifier = new UnderwritingVerifier(ITeeMachineRegistry(address(machines)), EXTENSION_ID);
        registryContract = new CreditRegistry(verifier);

        string memory json = vm.readFile("test/fixtures/tee-signatures.json");
        string memory b = "$[?(@.name == 'tier3_coston2')]";
        data = vm.parseJsonBytes(json, string.concat(b, ".data"));
        id = vm.parseJsonBytes32(json, string.concat(b, ".id"));
        status = uint8(vm.parseJsonUint(json, string.concat(b, ".status")));
        signature = vm.parseJsonBytes(json, string.concat(b, ".signature"));
        teeId = vm.parseJsonAddress(json, string.concat(b, ".expectedTeeId"));
        borrower = vm.parseJsonAddress(json, string.concat(b, ".borrower"));
        issuedAt = uint64(vm.parseJsonUint(json, string.concat(b, ".issuedAt")));
        expiry = uint64(vm.parseJsonUint(json, string.concat(b, ".expiry")));

        machines.setMachine(teeId, EXTENSION_ID, verifier.TEE_STATUS_PRODUCTION());
        // Fixtures are dated; sit between issuance and expiry.
        vm.warp(issuedAt + 1);
    }

    function test_submitAttestation_storesAndEmits() public {
        vm.expectEmit(true, true, true, true);
        emit CreditRegistry.CreditAttested(borrower, 3, 7500, expiry, teeId, id);
        registryContract.submitAttestation(data, id, status, signature);

        CreditRegistry.StoredAttestation memory a = registryContract.getAttestation(borrower);
        assertEq(a.riskTier, 3, "riskTier");
        assertEq(a.maxLTVBips, 7500, "maxLTVBips");
        assertEq(a.teeId, teeId, "teeId");
        assertEq(a.instructionId, id, "instructionId");
        assertTrue(registryContract.hasValidAttestation(borrower), "should be valid");
        assertEq(registryContract.currentMaxLTVBips(borrower), 7500, "maxLTV accessor");
    }

    /// @dev The whole point of the permissionless design: a third party can pay the gas and the
    /// attestation still binds to the borrower in the signed payload, not to msg.sender.
    function test_submitAttestation_isPermissionless() public {
        address randomRelayer = address(0xBEEF);
        vm.prank(randomRelayer);
        registryContract.submitAttestation(data, id, status, signature);

        assertTrue(registryContract.hasValidAttestation(borrower), "borrower from payload, not sender");
        assertFalse(registryContract.hasValidAttestation(randomRelayer), "relayer gets no credit line");
    }

    function test_submitAttestation_revertsOnReplay() public {
        registryContract.submitAttestation(data, id, status, signature);
        vm.expectRevert(abi.encodeWithSelector(CreditRegistry.AttestationReplayed.selector, id));
        registryContract.submitAttestation(data, id, status, signature);
    }

    function test_submitAttestation_revertsWhenAlreadyExpired() public {
        vm.warp(uint256(expiry) + 1);
        vm.expectRevert(
            abi.encodeWithSelector(CreditRegistry.AttestationExpired.selector, expiry, uint256(expiry) + 1)
        );
        registryContract.submitAttestation(data, id, status, signature);
    }

    function test_hasValidAttestation_falseAfterExpiry() public {
        registryContract.submitAttestation(data, id, status, signature);
        assertTrue(registryContract.hasValidAttestation(borrower), "valid before expiry");

        vm.warp(uint256(expiry) + 1);
        assertFalse(registryContract.hasValidAttestation(borrower), "expired");
        assertEq(registryContract.currentMaxLTVBips(borrower), 0, "expired LTV must read 0");
        assertEq(registryContract.currentRiskTier(borrower), 0, "expired tier must read 0");
    }

    function test_unattestedBorrowerReadsZero() public view {
        address stranger = address(0xDEAD);
        assertFalse(registryContract.hasValidAttestation(stranger));
        assertEq(registryContract.currentMaxLTVBips(stranger), 0);
    }

    /// @dev A forged signature must not create a credit line.
    function test_submitAttestation_revertsForUnregisteredSigner() public {
        MockMachineRegistry empty = new MockMachineRegistry();
        UnderwritingVerifier v2 = new UnderwritingVerifier(ITeeMachineRegistry(address(empty)), EXTENSION_ID);
        CreditRegistry r2 = new CreditRegistry(v2);

        vm.expectRevert(abi.encodeWithSelector(UnderwritingVerifier.UnknownTeeMachine.selector, teeId));
        r2.submitAttestation(data, id, status, signature);
    }
}
