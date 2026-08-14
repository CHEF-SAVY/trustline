# CONTRACTS.md — source of truth

Locked 2026-08-14. Every value below was verified against `tee-node@v0.0.24`,
`go-flare-common@v1.2.2` and the live Coston2 chain. If an implementation disagrees with this file,
the implementation is wrong.

---

## 1. Network constants (Coston2)

| Item | Value |
|---|---|
| Chain ID | `114` |
| RPC | `https://coston2-api.flare.network/ext/C/rpc` |
| `FlareTeeManager` (EIP-2535 diamond — every TEE facet) | `0x1a9C4A0f9D76c0b1D91d22E24E573a9b377618aE` |
| `FlareContractRegistry` | `0xaD67FE66660Fb8dFE9d6b1b4240d8650e30F6019` |
| First public extension id | `0x10000` (65536) |
| `AssetManagerFXRP` | `0xc1Ca88b937d0b528842F95d5731ffB586f4fbDFA` |
| **FXRP token** (`FTestXRP`) — pool asset | `0x0b6A3645c240605887a5532109323A3E12273dc7` |
| **FXRP decimals** | **6, not 18** |

> FXRP was resolved at runtime, not hardcoded from a doc:
> `FlareContractRegistry.getContractAddressByName("AssetManagerFXRP")` → `.fAsset()`.
> Re-resolve per network rather than copying the address.
>
> **The 6 decimals matter.** Anything assuming 18 is off by a factor of 10^12 — a UI would show
> `0.000001` for 1 FXRP, and "borrow 1" would request a trillion units. The contracts are
> decimal-agnostic (plain `uint256` amounts and bips ratios), so this only affects display and input
> parsing: `frontend/config.js` carries `poolAssetDecimals`.

`ITeeExtensionRegistry` and `ITeeMachineRegistry` are **both** this one diamond address.

---

## 2. Operation constants

Byte-identical in three places: Solidity `bytes32`, Go config, Go handler routing. `F_` prefix is reserved.

| Constant | Value |
|---|---|
| `OP_TYPE_TRUSTLINE` | `bytes32("TRUSTLINE")` |
| `OP_COMMAND_SCORE_BORROWER` | `bytes32("SCORE_BORROWER")` |

---

## 3. Instruction message (chain → TEE)

`ITeeExtensionRegistry.TeeInstructionParams.message` = `abi.encode(ScoreRequest)`:

```solidity
struct ScoreRequest {
    address borrower;      // EVM address the attestation will be bound to
    string  xrplAddress;   // XRPL r-address to score
}
```

> The XRPL address is visible in the instruction event. That is unavoidable — the TEE must be told
> what to score. What stays private is the *transaction history and every derived feature*. §7.

---

## 4. Attestation payload (TEE → chain) — LOCKED

`ActionResult.Data` = `abi.encode(CreditAttestation)`:

```solidity
struct CreditAttestation {
    address borrower;         // who the credit line belongs to
    bytes32 xrplAddressHash;  // keccak256(xrplAddress) — commitment, never the raw address
    uint8   riskTier;         // 0..3
    uint16  maxLTVBips;       // basis points, 10000 = 100%
    uint64  issuedAt;         // unix seconds, TEE clock
    uint64  expiry;           // unix seconds; registry rejects at/after this
}
```

Fixed 6-word encoding. **No feature values, no transaction data, no raw XRPL address.**

---

## 5. Signature scheme — how the TEE signs, how we verify

Verified: `tee-node/pkg/types/actions.go:62`, `tee-node/internal/router/utils.go:25`,
`go-flare-common/pkg/signing/hash.go:52`, `prefixes.go:18`, `tee-node/pkg/utils/crypto.go:27`.

```
innerHash = keccak256( keccak256(Data) ‖ ID ‖ keccak256(SubmissionTag) ‖ Status )
digest    = keccak256( abi.encode( bytes32("TEE_ACTION_RESULT"), uint256(114), innerHash ) )
ethSigned = keccak256( "\x19Ethereum Signed Message:\n32" ‖ digest )
teeId     = ecrecover( ethSigned, signature )
```

Where, for our success path:
- `ID` — `bytes32` instruction id assigned by the registry
- `SubmissionTag` — the ASCII string `"threshold"` (values: `threshold` | `end` | `submit`)
- `Status` — `uint8`; `1` = success (`0` = error, `>=2` = pending). **Only accept `1`.**

**Non-negotiable details** (each one silently breaks verification if missed):
1. The signature **is EIP-191 prefixed**. Apply `MessageHashUtils.toEthSignedMessageHash` before `ecrecover`.
2. The `Payload` is **`abi.encode`**, a 3-word tuple — not `encodePacked`.
3. `ActionResult.Hash()` inner concatenation **is** packed (`keccak256(abi.encodePacked(...))`), 97 bytes:
   `32 (dataHash) + 32 (id) + 32 (tagHash) + 1 (status)`. Note the asymmetry with (2) — this is the
   easiest place to get it wrong.
4. Reject non-canonical (high-s) signatures — use OpenZeppelin `ECDSA.recover`, which enforces this.

---

## 6. Verifying the signer is a genuine TEE machine

Against the diamond (`IMachineManager`):

| Call | Check |
|---|---|
| `getExtensionId(address _teeId) → uint256` | must equal our registered `extensionId` |
| `getTeeMachineStatus(address _teeId) → uint8` | must equal `2` (PRODUCTION) |

**`getTeeMachineStatus` reverts with `TeeNotFound()` (`0xceb05b68`) for unregistered addresses** —
empirically confirmed on Coston2. A forged signature recovers a random address and therefore reverts.
Wrap both calls in `try/catch` and surface `UnknownTeeMachine()` rather than bubbling the raw revert.

Status `2` = PRODUCTION confirmed across four live machines; `ban`/`pause`/`toProduction` imply other
values. Treat "not 2" as invalid.

---

## 7. Privacy invariant

On-chain, per attestation, we reveal only: borrower address, a *hash* of the XRPL address, a tier
(0–3), an LTV, and two timestamps. Never revealed: transaction history, counterparties, balances,
volumes, account age, or any intermediate feature. `/state` exposes aggregate counters only.

Residual, disclosed honestly: the XRPL address appears in the **request** event (§3), so the
borrower↔XRPL link is public. The *creditworthiness evidence* is what stays confidential. Closing that
last gap needs a commit-reveal or encrypted instruction payload — out of scope today, noted as future work.

---

## 8. Deployed addresses (Coston2)

_Filled in at deployment._

| Contract | Address |
|---|---|
| `UnderwritingVerifier` | — |
| `CreditRegistry` | — |
| `TrustLineInstructionSender` | — |
| `TrustLinePool` | — |
| Extension ID | — |

## 9. Function signatures and events

### TrustLineInstructionSender
| Member | Signature |
|---|---|
| `requestCreditCheck` | `requestCreditCheck(string) payable returns (bytes32)` — `0x24793e2a` |
| `setExtensionId` | `setExtensionId()` — DO NOT MODIFY |
| `extensionId` | `extensionId() view returns (uint256)` |
| event | `CreditCheckRequested(address indexed borrower, bytes32 indexed instructionId, string xrplAddress, uint256 fee)` |

### UnderwritingVerifier
| Member | Signature |
|---|---|
| `verifyAttestation` | `verifyAttestation(bytes,bytes32,uint8,bytes) view returns (CreditAttestation, address)` |
| `recoverTeeId` | `recoverTeeId(bytes,bytes32,uint8,bytes) view returns (address)` |
| `actionResultHash` | `actionResultHash(bytes,bytes32,bytes32,uint8) pure returns (bytes32)` |
| `signingDigest` | `signingDigest(bytes32) view returns (bytes32)` |
| errors | `InvalidStatus(uint8)`, `UnknownTeeMachine(address)`, `WrongExtension(address,uint256)`, `TeeMachineNotInProduction(address,uint8)`, `InvalidRiskTier(uint8)`, `InvalidLTV(uint16)`, `ExpiryBeforeIssuance(uint64,uint64)` |

### CreditRegistry
| Member | Signature |
|---|---|
| `submitAttestation` | `submitAttestation(bytes,bytes32,uint8,bytes)` — permissionless |
| `getAttestation` | `getAttestation(address) view returns (StoredAttestation)` — `0xf9b71797` |
| `hasValidAttestation` | `hasValidAttestation(address) view returns (bool)` — `0x6aae21db` |
| `currentMaxLTVBips` | `currentMaxLTVBips(address) view returns (uint16)` — `0xc9e52a96` |
| `currentRiskTier` | `currentRiskTier(address) view returns (uint8)` — `0x2b2708c9` |
| event | `CreditAttested(address indexed borrower, uint8 riskTier, uint256 maxLTV, uint256 expiry, address indexed teeId, bytes32 indexed instructionId)` |
| errors | `AttestationReplayed(bytes32)`, `AttestationExpired(uint64,uint256)`, `AttestationNotNewer(uint64,uint64)` |

`StoredAttestation` returns 7 words in this order:
`riskTier, maxLTVBips, issuedAt, expiry, xrplAddressHash, teeId, instructionId`.

### TrustLinePool
| Member | Signature |
|---|---|
| `deposit` / `withdraw` | `deposit(uint256)` — `0xb6b55f25`, `withdraw(uint256)` — `0x2e1a7d4d` |
| `borrow` / `repay` | `borrow(uint256)` — `0xc5ebeaec`, `repay(uint256)` — `0x371fd8e6` |
| `liquidate` | `liquidate(address)` |
| `effectiveLTVBips` | `effectiveLTVBips(address) view returns (uint16, bool underwritten)` |
| `availableToBorrow` | `availableToBorrow(address) view returns (uint256)` — `0x2c38199e` |
| `currentLTVBips` | `currentLTVBips(address) view returns (uint256)` — `0xfd1d1538` |
| `collateralOf` / `debtOf` | `0x1aefb107` / `0xd283e75f` |
| events | `Deposited`, `Withdrawn`, `Borrowed(address indexed,uint256,uint16,bool)`, `Repaid`, `Liquidated` |

Selectors above are verified in CI by `frontend/check-selectors.mjs`.
