# TrustLine — Implementation Plan

Written 2026-08-14, after installing the Flare skill packs, reading the FCC/FDC docs, cloning
`fce-extension-scaffold`, and querying the live Coston2 TEE registry.

---

## 0. Two things that need a decision before anything else

### 0.1 The deadline is today
Both `CLAUDE.md` and the build plan state submissions close **Aug 14 2026**. That is today's date.
The original 4-phase plan is a multi-day plan. This document therefore prioritises a **demo-critical
path** (§3) and marks everything else as cut-scope (§7). Confirm the real deadline — if there are
genuinely more days, sections 6–7 get promoted back in.

### 0.2 Indexer credentials — needed to RUN a proxy, but not the delivery path

> **CORRECTED 2026-08-14.** An earlier draft of this section called the indexer "the long pole" and
> said `ext-proxy` uses it to pick up instructions. That was wrong, and wrong in a way that points
> debugging in the opposite direction from the real cause.

**Data providers POST cosigned instructions directly to your proxy** at `POST /instruction`
(`tee-proxy v0.0.18`, `internal/server/external.go:134`). The proxy uses the indexer DB only for its
startup sync gate, machine-path watching, and liveness/readiness.

So credentials are still required to *run* a proxy — it panics if the indexer never syncs — but if an
instruction fails to arrive, the indexer is almost never the cause. The real causes, in order, are
machine status, a stale on-chain URL, and version drift. `docs/RUNBOOK.md` covers all of them, and
`relayer -doctor` checks them automatically.

Note also: the indexer-reader credentials in the older docs are **dead**; use the hackathon channel's
pinned ones. Expected lag is effectively zero — `GET :6661/ready` is authoritative (200 = current,
503 with `c-chain indexer delay` = genuinely behind). Gaps in the logs table prove nothing, since the
hackathon DB indexes only selected contracts and topics.

Mitigation, confirmed from the scaffold's `docs/testing.md`: the **unit** and **conformance** layers
run with *no chain, no indexer, no Docker*. That is how all 50 tests pass today.

---

## 1. What the research changed

The build plan was written from the docs' overview pages. Reading the source and the live chain
corrected five assumptions. These are not cosmetic — three of them change the contract set.

| # | Build plan assumed | Reality | Consequence |
|---|---|---|---|
| 1 | FCC is "pre-production", we simulate the TEE | **FCC is live on Coston2.** `nextPublicExtensionId` = 66251 (≈715 extensions registered), 443 active TEE machines, several on ngrok/Codespaces URLs — i.e. developer-registered | We can run a *real* TEE extension, not a simulation. Much stronger submission. |
| 2 | `TeeExtensionRegistry` and `TeeMachineRegistry` are separate contracts; interfaces must be hand-written | Both are **facets of one EIP-2535 diamond**, `FlareTeeManager` @ `0x1a9C4A0f9D76c0b1D91d22E24E573a9b377618aE`. Interfaces already ship in `fce-extension-scaffold/contracts/interfaces/` | Delete the "hand-write ITeeExtensionRegistry" task. Copy the scaffold's interfaces. |
| 3 | The FCE "pushes the signed result back through the proxy for on-chain relay" | **Results never reach the chain by themselves.** `tee-node` signs the response and the proxy serves it at `/action/result`. Nothing submits it on-chain | **We must build the submit step ourselves.** This is the single biggest gap. See §2. |
| 4 | An extension registers one contract | `ExtensionManager.register(address _teeExtensionStateVerifier, address _teeExtensionInstructionsSender)` — **two** contracts | Our contract set maps onto the two registered roles. |
| 5 | FDC could supply XRPL history "more rigorously" | FDC attestation types are **per-transaction** (`XRPPayment`, `Payment`), not per-account-history. `Web2Json` could hit an XRPL API but publishes the response **on-chain in the clear** | FDC is the wrong tool *and* would break the privacy invariant. This is a feature: it's the crisp answer to "why not just use FDC?" |

Also worth knowing:
- OPType/OPCommand strings must be byte-identical in **three** places — Solidity `bytes32("...")`, the Go
  config constants, and the handler's routing switch. Mismatch = "unsupported op type". Avoid the `F_` prefix (reserved).
- TEE machine identity **is an EVM address** (`getRandomTeeIds` returns `address[]`), which is exactly
  what makes on-chain `ecrecover` verification work.
- `VerificationFacet` is about *TEE attestation and availability*, **not** verifying arbitrary payloads.
  It gives us no `verifySignature(...)` — we write that ourselves. See §2.

---

## 2. The architecture, corrected

The crux is getting a TEE-signed attestation on-chain. Since nothing does it for us, we add a
**permissionless submit step**. This is safe precisely because trust rests on the *signature*, not on
the sender — anyone may relay, and a wrong relayer cannot forge a tier.

```
1. Borrower  → TrustLineInstructionSender.requestCreditCheck(xrplAddress)      [on-chain tx]
                 └─ ITeeExtensionRegistry.sendInstructions(teeIds, params)
2. Data providers relay the instruction (needs ≥50% signature weight)
3. ext-proxy → TEE machine → our Go handler, POST /action
4. Inside the TEE:  fetch XRPL history → score → build attestation
5. tee-node signs the response (ECDSA secp256k1, bound to CHAIN_ID)
6. Result served at  ext-proxy /action/result                      ← leaves the chain's view
7. ANYONE submits it: CreditRegistry.submitAttestation(payload, signature)      [on-chain tx]
                 └─ UnderwritingVerifier.verifyAttestation(payload, signature)
                      ├─ ecrecover(hash, sig) → teeId
                      ├─ MachineManager.getExtensionId(teeId) == OUR_EXTENSION_ID
                      ├─ MachineManager.getTeeMachineStatus(teeId) is active
                      └─ decode {borrower, riskTier, maxLTV, expiry}, reject if expired
8. TrustLinePool reads CreditRegistry → allows borrowing up to maxLTV
```

Verification calls (all to the one diamond address):
- `getExtensionId(address _teeId) returns (uint256)` — is this machine *ours*?
- `getTeeMachineStatus(address _teeId) returns (uint8)` — active, not banned/paused?
- `getActiveTeeMachines(uint256 _extensionId) returns (address[], string[])` — for the frontend to find a proxy URL.

### 2.1 The exact signing scheme — RESOLVED (was the highest-risk unknown)

Traced through `tee-node@v0.0.24` → `go-flare-common@v1.2.2`. The result is **fully reconstructible
in Solidity**, and `prefixes.go` carries the comment *"must be aligned with smart contracts"* —
on-chain verification is a designed-for path, not something we're bolting on.

```
innerHash = keccak256( keccak256(Data) ‖ ID ‖ keccak256(SubmissionTag) ‖ Status )   // ActionResult.Hash()
digest    = keccak256( abi.encode( bytes32("TEE_ACTION_RESULT"), uint256(chainId), innerHash ) )
signature = secp256k1.Sign( EIP-191( digest ) )
teeId     = ecrecover( EIP-191(digest), signature )
```

Sources: `tee-node/pkg/types/actions.go:62` (`ActionResult.Hash`), `tee-node/internal/router/utils.go:25`
(`SignResult`), `go-flare-common/pkg/signing/hash.go:52` (`Payload.Hash`),
`go-flare-common/pkg/signing/prefixes.go:18` (`TEEActionResult`), `tee-node/pkg/utils/crypto.go:27` (`Sign`).

**Three details that would otherwise have cost hours of debugging:**

1. **The signature IS EIP-191 prefixed.** `utils.Sign` calls `crypto.Sign(accounts.TextHash(msgHash), …)`,
   i.e. `"\x19Ethereum Signed Message:\n32" ‖ digest`. Solidity must apply
   `MessageHashUtils.toEthSignedMessageHash(digest)` **before** `ecrecover`. Verifying the bare digest
   will silently recover the wrong address.
2. **`Payload` is `abi.encode`d, not `encodePacked`** — a 3-word tuple `(bytes32, uint256, bytes32)`.
3. **Non-canonical signatures are rejected** (`CheckCanonicalSignature`). Mirror this on-chain by using
   OpenZeppelin's `ECDSA.recover`, which enforces low-s and rejects `s` malleability.

**Plan correction:** do **not** invent a bespoke EIP-712 domain, as earlier drafts of this document
proposed. Flare's `prefix ‖ chainId ‖ dataHash` scheme is already domain-separated and chain-bound,
and it is what the TEE actually signs. Matching it is mandatory, not a stylistic choice.

**Consequence for the payload:** the borrower/tier/LTV/expiry tuple lives inside `ActionResult.Data`,
which is hashed as an opaque blob. So the on-chain path is: verify the signature over the outer
structure, then `abi.decode` `Data` into our struct. Replay defence — `chainId` is already bound by the
digest; `ID` (the instruction id) is bound by `innerHash`; we add `expiry` inside `Data` and store the
last-seen instruction id per borrower in `CreditRegistry` to reject re-submission and out-of-order writes.

### Contract set (revised)

| Contract | Role |
|---|---|
| `TrustLineInstructionSender.sol` | On-chain entry point. Registered as the extension's `instructionsSender`. Holds `OP_TYPE_TRUSTLINE` / `OP_COMMAND_SCORE_BORROWER`. Keep the scaffold's `constructor`, `setExtensionId()`, `_getExtensionId()` **unmodified** — the docs mark them DO NOT MODIFY. |
| `UnderwritingVerifier.sol` | Pure verification: EIP-712 digest, `ecrecover`, registry membership + status check, decode. Registered as the extension's `stateVerifier`. |
| `CreditRegistry.sol` | `submitAttestation(...)` (permissionless), stores latest per borrower, rejects stale/expired/replayed, emits `CreditAttested`. |
| `TrustLinePool.sol` | Standard overcollateralised path + underwritten path reading `CreditRegistry`. Emits `Borrowed`, `Repaid`, `Liquidated`. |
| `interfaces/ITeeExtensionRegistry.sol`, `interfaces/ITeeMachineRegistry.sol` | **Copied from the scaffold**, extended with `getExtensionId` / `getTeeMachineStatus`. |

---

## 3. Critical path (the submission-defining work)

Ordered so each step unblocks the next. Steps A and B run in parallel with everything.

| # | Step | Depends on | Notes |
|---|---|---|---|
| **A** | **Request indexer credentials from Flare** | — | Do this first. Blocks E2E only. |
| **B** | Clone `fce-extension-scaffold` into `backend/` | — | Replaces the hand-rolled Go skeleton from Stage 0. Do not invent a different shape. |
| **C** | Lock the attestation payload + EIP-712 struct in `docs/CONTRACTS.md` | — | **Single source of truth. Both C and D depend on this — write it before either.** |
| **D** | Contracts: interfaces → `UnderwritingVerifier` → `CreditRegistry` → `TrustLineInstructionSender` → `TrustLinePool` | C | Foundry tests as you go; fuzz the LTV math and expiry. |
| **E** | Go handler: OPType/OPCommand routing, XRPL fetch, scoring, response encoding | B, C | Match the payload from C byte-for-byte — tee-node hashes and signs it. |
| **F** | Conformance + unit tests green | E | **No chain, no indexer needed.** This is the correctness proof we can produce today. |
| **G** | Deploy contracts to Coston2, register extension, `setExtensionId()` | D | Needs a funded Coston2 key (faucet). |
| **H** | Wire the submit step: read `/action/result`, submit on-chain | F, G | The gap from §2. A small Go/TS relayer script is enough. |
| **I** | E2E on Coston2 | G, H, **A** | Blocked on credentials. |
| **J** | Frontend: the five states | G | Can be built against a deployed contract with a manually-submitted attestation, without I. |
| **K** | Demo video + README | — | Judged artifact. Do not leave to the last minute. |

**If credentials never arrive:** F + G + J still give a demonstrable submission — real contracts on
Coston2, a real signed attestation verified on-chain (submitted by our relayer from a locally-run
extension), with only the data-provider relay leg simulated. Document that boundary honestly rather
than implying a full live run.

---

## 4. The scoring function

Must be defensible to judges, so: transparent, bounded, documented. No ML.

Inputs, all derivable from XRPL account data:
- **Account age** — from the account's first ledger.
- **Transaction count** — total, and share that are `Payment`.
- **Payment volume** — total drops sent/received.
- **Counterparty diversity** — number of distinct addresses transacted with (resists self-wash).

Map to a 0–3 tier with published thresholds, then to `maxLTV`:

| Tier | Meaning | maxLTV |
|---|---|---|
| 0 | No/thin history | fall back to the standard overcollateralised LTV |
| 1 | Thin but real | modest improvement |
| 2 | Established | meaningful improvement |
| 3 | Strong, diverse, aged | best available |

Exact weights and cutoffs get fixed in code and mirrored into `CONTRACTS.md`. **Every input must be
computed inside the TEE and discarded** — only the tier and LTV leave. Sybil resistance is the honest
weak point (an attacker can age cheap accounts); state that limitation openly rather than overclaiming.

---

## 5. Privacy invariant — how it is actually enforced

Not a comment in the code; a set of checkable rules:
1. The XRPL fetch happens **only** inside the extension handler running in the TEE.
2. Nothing derived from raw history is written to the response, to `/state`, or to logs. `/state`
   exposes aggregate counters only.
3. The signed payload carries exactly `{borrower, riskTier, maxLTV, expiry, chainId, instructionId, nonce}`.
4. No feature values, no XRPL address, no transaction hashes on-chain. Note: putting the raw XRPL
   r-address on-chain would partially defeat the purpose — prefer a hash/commitment if the link must exist.
5. A test asserts the response struct has no extra fields.

---

## 6. Deferred (only if the deadline is not today)

- FDC `XRPPayment` cross-check of a single declared payment, as a second evidence source.
- Real liquidation mechanics and interest accrual in the pool.
- Multi-TEE quorum (require *k* signatures) instead of a single machine.
- Reproducible build / code-hash registration (`REPRODUCIBILITY.md`) for a genuinely verifiable image.

## 7. Explicitly cut for a same-day submission

- The full FAssets integration (mirror the LTV conventions; don't integrate the protocol).
- Anything needing `SIMULATED_TEE=false` real confidential hardware.
- A polished multi-page frontend — one page with five honest states beats five half-built pages.

---

## 8. Open questions to resolve while building

1. ~~Is the response signed over raw bytes or a reconstructible hash?~~ **RESOLVED — see §2.1.**
   Fully reconstructible on-chain; note the EIP-191 prefix.
2. ~~Does `getTeeMachineStatus` return an enum where 0 is valid?~~ **RESOLVED:** `1 = INITIALIZED`,
   `2 = PRODUCTION`. Only 2 receives dispatches and only 2 is accepted by the verifier.
3. Instruction fee: `sendInstructions` is `payable` — determine the minimum so `requestCreditCheck`
   forwards enough.
4. Does `claimBackAddress` need to be the borrower, or can the protocol absorb it?
