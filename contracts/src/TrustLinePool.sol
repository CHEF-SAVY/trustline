// SPDX-License-Identifier: MIT
pragma solidity 0.8.28;

import { IERC20 } from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import { SafeERC20 } from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import { ReentrancyGuard } from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import { CreditRegistry } from "./CreditRegistry.sol";

/// @title TrustLinePool
/// @notice Minimal lending pool with two borrowing paths.
///
/// - **Standard path** — fixed overcollateralized LTV, matching FAssets norms. Always available.
/// - **Underwritten path** — reads CreditRegistry and lifts the cap to the TEE-attested `maxLTV`
///   while the attestation is unexpired.
///
/// This is the payoff of the whole design: the same collateral unlocks a larger loan because a TEE
/// vouched for the borrower without publishing their history.
///
/// SCOPE: deliberately simplified for the hackathon. No interest accrual, no oracle price feed
/// (collateral and debt are denominated in the same asset), and liquidation is a solvency backstop
/// rather than a competitive keeper market. Called out honestly rather than implied to be complete.
contract TrustLinePool is ReentrancyGuard {
    using SafeERC20 for IERC20;

    /// @notice Asset used for both collateral and borrowing.
    IERC20 public immutable ASSET;

    CreditRegistry public immutable CREDIT_REGISTRY;

    /// @notice LTV in bips available without an attestation. 5000 = 50%, i.e. 2x overcollateralized.
    uint16 public immutable STANDARD_LTV_BIPS;

    /// @notice LTV in bips at which a position may be liquidated. Must exceed any attainable maxLTV,
    /// otherwise a borrower could be liquidated the instant they draw their full allowance.
    uint16 public immutable LIQUIDATION_THRESHOLD_BIPS;

    uint16 public constant BIPS = 10_000;

    mapping(address => uint256) public collateralOf;
    mapping(address => uint256) public debtOf;

    uint256 public totalCollateral;
    uint256 public totalDebt;

    event Deposited(address indexed user, uint256 amount);
    event Withdrawn(address indexed user, uint256 amount);
    event Borrowed(address indexed user, uint256 amount, uint16 appliedLTVBips, bool underwritten);
    event Repaid(address indexed user, uint256 amount);
    event Liquidated(address indexed user, address indexed liquidator, uint256 debtRepaid, uint256 collateralSeized);

    error ZeroAmount();
    error InsufficientCollateral();
    error ExceedsBorrowingPower(uint256 requested, uint256 available);
    error NoDebt();
    error PositionHealthy(uint256 ltvBips, uint16 thresholdBips);
    error InsufficientLiquidity(uint256 requested, uint256 available);
    error RepayExceedsDebt(uint256 amount, uint256 debt);

    constructor(
        IERC20 _asset,
        CreditRegistry _creditRegistry,
        uint16 _standardLtvBips,
        uint16 _liquidationThresholdBips
    ) {
        require(address(_asset) != address(0), "Asset cannot be zero address");
        require(address(_creditRegistry) != address(0), "CreditRegistry cannot be zero address");
        require(_standardLtvBips > 0 && _standardLtvBips < BIPS, "Invalid standard LTV");
        require(_liquidationThresholdBips > _standardLtvBips, "Threshold must exceed standard LTV");
        require(_liquidationThresholdBips <= BIPS, "Threshold too high");

        ASSET = _asset;
        CREDIT_REGISTRY = _creditRegistry;
        STANDARD_LTV_BIPS = _standardLtvBips;
        LIQUIDATION_THRESHOLD_BIPS = _liquidationThresholdBips;
    }

    /// @notice LTV a borrower may use right now, and whether it came from an attestation.
    /// @dev An attested LTV *below* the standard is ignored — underwriting may only ever help. A
    /// borrower who already qualifies for the standard path should never be penalised for having
    /// requested a credit check.
    function effectiveLTVBips(address _user) public view returns (uint16 ltvBips, bool underwritten) {
        uint16 attested = CREDIT_REGISTRY.currentMaxLTVBips(_user);
        if (attested > STANDARD_LTV_BIPS) return (attested, true);
        return (STANDARD_LTV_BIPS, false);
    }

    /// @notice Maximum total debt `_user` may hold, given their collateral and current LTV.
    function maxDebtFor(address _user) public view returns (uint256) {
        (uint16 ltvBips,) = effectiveLTVBips(_user);
        return (collateralOf[_user] * ltvBips) / BIPS;
    }

    /// @notice Additional amount `_user` may borrow right now.
    function availableToBorrow(address _user) public view returns (uint256) {
        uint256 maxDebt = maxDebtFor(_user);
        uint256 debt = debtOf[_user];
        return maxDebt > debt ? maxDebt - debt : 0;
    }

    /// @notice Current LTV of a position in bips; 0 if no debt.
    function currentLTVBips(address _user) public view returns (uint256) {
        uint256 collateral = collateralOf[_user];
        if (collateral == 0) return debtOf[_user] == 0 ? 0 : type(uint256).max;
        return (debtOf[_user] * BIPS) / collateral;
    }

    /// @notice True if the position may be liquidated.
    function isLiquidatable(address _user) public view returns (bool) {
        if (debtOf[_user] == 0) return false;
        return currentLTVBips(_user) >= LIQUIDATION_THRESHOLD_BIPS;
    }

    /// @notice Liquidity available to lend out.
    function availableLiquidity() public view returns (uint256) {
        return ASSET.balanceOf(address(this));
    }

    function deposit(uint256 _amount) external nonReentrant {
        if (_amount == 0) revert ZeroAmount();
        collateralOf[msg.sender] += _amount;
        totalCollateral += _amount;
        ASSET.safeTransferFrom(msg.sender, address(this), _amount);
        emit Deposited(msg.sender, _amount);
    }

    /// @notice Withdraws collateral, provided the position stays within its borrowing power.
    function withdraw(uint256 _amount) external nonReentrant {
        if (_amount == 0) revert ZeroAmount();
        uint256 collateral = collateralOf[msg.sender];
        if (_amount > collateral) revert InsufficientCollateral();

        uint256 remaining = collateral - _amount;
        (uint16 ltvBips,) = effectiveLTVBips(msg.sender);
        uint256 maxDebtAfter = (remaining * ltvBips) / BIPS;
        if (debtOf[msg.sender] > maxDebtAfter) {
            revert ExceedsBorrowingPower(debtOf[msg.sender], maxDebtAfter);
        }
        if (_amount > availableLiquidity()) revert InsufficientLiquidity(_amount, availableLiquidity());

        collateralOf[msg.sender] = remaining;
        totalCollateral -= _amount;
        ASSET.safeTransfer(msg.sender, _amount);
        emit Withdrawn(msg.sender, _amount);
    }

    /// @notice Borrows against collateral, using the attested LTV when one is available.
    function borrow(uint256 _amount) external nonReentrant {
        if (_amount == 0) revert ZeroAmount();

        uint256 available = availableToBorrow(msg.sender);
        if (_amount > available) revert ExceedsBorrowingPower(_amount, available);
        if (_amount > availableLiquidity()) revert InsufficientLiquidity(_amount, availableLiquidity());

        (uint16 ltvBips, bool underwritten) = effectiveLTVBips(msg.sender);

        debtOf[msg.sender] += _amount;
        totalDebt += _amount;
        ASSET.safeTransfer(msg.sender, _amount);
        emit Borrowed(msg.sender, _amount, ltvBips, underwritten);
    }

    function repay(uint256 _amount) external nonReentrant {
        if (_amount == 0) revert ZeroAmount();
        uint256 debt = debtOf[msg.sender];
        if (debt == 0) revert NoDebt();
        if (_amount > debt) revert RepayExceedsDebt(_amount, debt);

        debtOf[msg.sender] = debt - _amount;
        totalDebt -= _amount;
        ASSET.safeTransferFrom(msg.sender, address(this), _amount);
        emit Repaid(msg.sender, _amount);
    }

    /// @notice Liquidates an unhealthy position: repay its debt, seize its collateral.
    /// @dev Full-position liquidation with no bonus — a solvency backstop, not a keeper market.
    /// A position typically becomes liquidatable when an attestation expires and the borrower's
    /// allowance snaps back toward the standard LTV, which is exactly the intended pressure to
    /// re-attest or repay.
    function liquidate(address _user) external nonReentrant {
        uint256 debt = debtOf[_user];
        if (debt == 0) revert NoDebt();
        if (!isLiquidatable(_user)) {
            revert PositionHealthy(currentLTVBips(_user), LIQUIDATION_THRESHOLD_BIPS);
        }

        uint256 collateral = collateralOf[_user];
        debtOf[_user] = 0;
        collateralOf[_user] = 0;
        totalDebt -= debt;
        totalCollateral -= collateral;

        ASSET.safeTransferFrom(msg.sender, address(this), debt);
        ASSET.safeTransfer(msg.sender, collateral);
        emit Liquidated(_user, msg.sender, debt, collateral);
    }
}
