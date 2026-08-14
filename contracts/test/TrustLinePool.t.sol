// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import { Test } from "forge-std/Test.sol";
import { ERC20 } from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { TrustLinePool } from "../src/TrustLinePool.sol";
import { CreditRegistry } from "../src/CreditRegistry.sol";
import { UnderwritingVerifier } from "../src/UnderwritingVerifier.sol";
import { ITeeMachineRegistry } from "../src/interfaces/ITeeMachineRegistry.sol";
import { MockMachineRegistry } from "./UnderwritingVerifier.t.sol";

contract MockAsset is ERC20 {
    constructor() ERC20("Mock FXRP", "mFXRP") { }

    function mint(address to, uint256 amount) external {
        _mint(to, amount);
    }
}

contract TrustLinePoolTest is Test {
    uint256 internal constant EXTENSION_ID = 66_000;
    uint16 internal constant STANDARD_LTV = 5000; // 50%
    uint16 internal constant LIQ_THRESHOLD = 8500; // 85%
    uint256 internal constant COLLATERAL = 1000e18;

    MockAsset internal asset;
    TrustLinePool internal pool;
    CreditRegistry internal registry;
    UnderwritingVerifier internal verifier;
    MockMachineRegistry internal machines;

    // tier3 fixture: borrower 0x1111..., maxLTV 7500 bips
    bytes internal data;
    bytes32 internal id;
    uint8 internal status;
    bytes internal signature;
    address internal teeId;
    address internal borrower;
    uint64 internal issuedAt;
    uint64 internal expiry;

    address internal lp = makeAddr("lp");
    address internal plainUser = makeAddr("plainUser");

    function setUp() public {
        vm.chainId(114);
        machines = new MockMachineRegistry();
        verifier = new UnderwritingVerifier(ITeeMachineRegistry(address(machines)), EXTENSION_ID);
        registry = new CreditRegistry(verifier);
        asset = new MockAsset();
        pool = new TrustLinePool(IERC20(address(asset)), registry, STANDARD_LTV, LIQ_THRESHOLD);

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
        vm.warp(issuedAt + 1);

        // Seed liquidity and collateral.
        asset.mint(lp, 1_000_000e18);
        vm.startPrank(lp);
        asset.approve(address(pool), type(uint256).max);
        pool.deposit(500_000e18);
        vm.stopPrank();

        _fund(borrower);
        _fund(plainUser);
    }

    function _fund(address who) internal {
        asset.mint(who, COLLATERAL * 2);
        vm.startPrank(who);
        asset.approve(address(pool), type(uint256).max);
        pool.deposit(COLLATERAL);
        vm.stopPrank();
    }

    function _attest() internal {
        registry.submitAttestation(data, id, status, signature);
    }

    // --- the core value proposition ----------------------------------------

    /// @dev The headline claim: same collateral, more borrowing power, because a TEE vouched for the
    /// borrower without publishing their history.
    function test_attestationIncreasesBorrowingPower() public {
        uint256 before = pool.availableToBorrow(borrower);
        assertEq(before, (COLLATERAL * STANDARD_LTV) / 10_000, "standard path = 50%");

        _attest();

        uint256 afterAttest = pool.availableToBorrow(borrower);
        assertEq(afterAttest, (COLLATERAL * 7500) / 10_000, "underwritten path = 75%");
        assertGt(afterAttest, before, "attestation must increase borrowing power");
    }

    function test_borrow_underwrittenPathEmitsUnderwrittenFlag() public {
        _attest();
        uint256 amount = (COLLATERAL * 7000) / 10_000; // 70%, above the standard cap

        vm.expectEmit(true, true, true, true);
        emit TrustLinePool.Borrowed(borrower, amount, 7500, true);

        vm.prank(borrower);
        pool.borrow(amount);
        assertEq(pool.debtOf(borrower), amount);
    }

    function test_borrow_standardPathForUnattestedUser() public {
        uint256 amount = (COLLATERAL * 5000) / 10_000;
        vm.expectEmit(true, true, true, true);
        emit TrustLinePool.Borrowed(plainUser, amount, STANDARD_LTV, false);

        vm.prank(plainUser);
        pool.borrow(amount);
    }

    function test_borrow_revertsAboveStandardLTVWithoutAttestation() public {
        uint256 tooMuch = (COLLATERAL * 6000) / 10_000;
        uint256 available = (COLLATERAL * STANDARD_LTV) / 10_000;

        vm.expectRevert(
            abi.encodeWithSelector(TrustLinePool.ExceedsBorrowingPower.selector, tooMuch, available)
        );
        vm.prank(plainUser);
        pool.borrow(tooMuch);
    }

    function test_borrow_revertsAboveAttestedLTV() public {
        _attest();
        uint256 tooMuch = (COLLATERAL * 8000) / 10_000;
        uint256 available = (COLLATERAL * 7500) / 10_000;
        vm.expectRevert(
            abi.encodeWithSelector(TrustLinePool.ExceedsBorrowingPower.selector, tooMuch, available)
        );
        vm.prank(borrower);
        pool.borrow(tooMuch);
    }

    // --- expiry behaviour ---------------------------------------------------

    /// @dev When an attestation lapses, borrowing power must snap back to the standard path.
    function test_expiredAttestationRevertsToStandardLTV() public {
        _attest();
        assertEq(pool.availableToBorrow(borrower), (COLLATERAL * 7500) / 10_000);

        vm.warp(uint256(expiry) + 1);
        assertEq(
            pool.availableToBorrow(borrower),
            (COLLATERAL * STANDARD_LTV) / 10_000,
            "expired attestation must fall back to standard LTV"
        );
    }

    /// @dev A borrower who drew on an attested line and then let it lapse keeps the debt but loses
    /// the headroom. They are underwater relative to the standard path but NOT yet liquidatable,
    /// because the liquidation threshold sits above any attainable LTV. This is the intended
    /// pressure to re-attest, and it is the subtlest interaction in the pool.
    function test_expiredAttestation_leavesDebtOutstandingButNotLiquidatable() public {
        _attest();
        uint256 amount = (COLLATERAL * 7000) / 10_000; // 70%
        vm.prank(borrower);
        pool.borrow(amount);

        vm.warp(uint256(expiry) + 1);

        assertEq(pool.debtOf(borrower), amount, "debt survives expiry");
        assertEq(pool.availableToBorrow(borrower), 0, "no headroom left");
        assertEq(pool.currentLTVBips(borrower), 7000, "LTV unchanged by expiry");
        assertFalse(pool.isLiquidatable(borrower), "70% < 85% threshold");
    }

    // --- repay / withdraw ---------------------------------------------------

    function test_repay_reducesDebt() public {
        _attest();
        uint256 amount = (COLLATERAL * 6000) / 10_000;
        vm.startPrank(borrower);
        pool.borrow(amount);
        pool.repay(amount / 2);
        vm.stopPrank();
        assertEq(pool.debtOf(borrower), amount - amount / 2);
    }

    function test_withdraw_revertsIfItWouldUndercollateralise() public {
        vm.startPrank(plainUser);
        pool.borrow((COLLATERAL * 5000) / 10_000); // max out standard path
        vm.expectRevert();
        pool.withdraw(COLLATERAL / 2);
        vm.stopPrank();
    }

    function test_withdraw_succeedsWhenNoDebt() public {
        vm.prank(plainUser);
        pool.withdraw(COLLATERAL);
        assertEq(pool.collateralOf(plainUser), 0);
    }

    // --- liquidation --------------------------------------------------------

    function test_liquidate_revertsOnHealthyPosition() public {
        _attest();
        vm.prank(borrower);
        pool.borrow((COLLATERAL * 7000) / 10_000);

        vm.expectRevert(
            abi.encodeWithSelector(TrustLinePool.PositionHealthy.selector, uint256(7000), LIQ_THRESHOLD)
        );
        vm.prank(lp);
        pool.liquidate(borrower);
    }

    /// @dev Exercises the real liquidation path.
    ///
    /// In the main pool the threshold (85%) sits above any attainable LTV (75%), which is correct —
    /// a borrower must never be liquidatable the moment they draw their full allowance. So to reach
    /// an unhealthy position at all we need a pool configured with a tighter threshold. That is a
    /// legitimate deployment (the constructor only requires threshold > standardLTV), and it lets us
    /// test `liquidate()` for real rather than asserting around it.
    function test_liquidate_seizesCollateralAndClearsDebt() public {
        TrustLinePool tightPool = new TrustLinePool(IERC20(address(asset)), registry, STANDARD_LTV, 7000);
        _attest();

        asset.mint(lp, 1_000_000e18);
        vm.startPrank(lp);
        asset.approve(address(tightPool), type(uint256).max);
        tightPool.deposit(500_000e18);
        vm.stopPrank();

        asset.mint(borrower, COLLATERAL);
        vm.startPrank(borrower);
        asset.approve(address(tightPool), type(uint256).max);
        tightPool.deposit(COLLATERAL);
        uint256 debt = (COLLATERAL * 7500) / 10_000; // 75% > 70% threshold
        tightPool.borrow(debt);
        vm.stopPrank();

        assertEq(tightPool.currentLTVBips(borrower), 7500, "position at 75%");
        assertTrue(tightPool.isLiquidatable(borrower), "75% >= 70% threshold");

        address liquidator = makeAddr("liquidator");
        asset.mint(liquidator, debt);
        uint256 liquidatorBefore = asset.balanceOf(liquidator);

        vm.startPrank(liquidator);
        asset.approve(address(tightPool), type(uint256).max);
        vm.expectEmit(true, true, true, true);
        emit TrustLinePool.Liquidated(borrower, liquidator, debt, COLLATERAL);
        tightPool.liquidate(borrower);
        vm.stopPrank();

        assertEq(tightPool.debtOf(borrower), 0, "debt cleared");
        assertEq(tightPool.collateralOf(borrower), 0, "collateral seized");
        assertFalse(tightPool.isLiquidatable(borrower), "no longer liquidatable");
        // Liquidator paid `debt` and received `COLLATERAL`; net gain is the overcollateralization.
        assertEq(
            asset.balanceOf(liquidator),
            liquidatorBefore - debt + COLLATERAL,
            "liquidator nets the collateral surplus"
        );
    }

    function test_liquidate_revertsWhenNoDebt() public {
        vm.expectRevert(TrustLinePool.NoDebt.selector);
        vm.prank(lp);
        pool.liquidate(plainUser);
    }

    // --- invariants ---------------------------------------------------------

    /// @dev No borrower may ever exceed their effective LTV, whatever sequence they attempt.
    function testFuzz_neverExceedsEffectiveLTV(uint256 borrowAmount, bool attested) public {
        if (attested) _attest();
        uint256 available = pool.availableToBorrow(borrower);
        borrowAmount = bound(borrowAmount, 1, COLLATERAL * 2);

        vm.prank(borrower);
        if (borrowAmount > available) {
            vm.expectRevert();
            pool.borrow(borrowAmount);
        } else {
            pool.borrow(borrowAmount);
            (uint16 ltv,) = pool.effectiveLTVBips(borrower);
            assertLe(pool.currentLTVBips(borrower), ltv, "LTV invariant");
        }
    }
}
