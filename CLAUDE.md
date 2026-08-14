# TrustLine — Confidential Underwriting for XRPFi Lending on Flare

Built for the Flare Summer Signal hackathon — Bounty 2: Confidential Compute Apps (deadline Aug 14 2026).

## What this is
Flare's XRPFi/FAssets lending markets are 100% overcollateralized — no underwriting layer exists.
TrustLine adds one: a Flare Compute Extension (FCE) computes a borrower's creditworthiness from their
XRPL transaction history inside a TEE, and signs a minimal attestation (risk tier + max LTV + expiry)
that a lending pool contract can trust and act on — without ever exposing the borrower's raw transaction
data on-chain or to any operator.

## Build stages — strict gate, do not skip ahead
- **Stage 0: Repo scaffolding** — structure only, no business logic
- **Phase 1: Smart contracts** — UnderwritingVerifier, CreditRegistry, TrustLinePool
- **Phase 2: Backend (Go FCE)** — TEE scoring service
- **Phase 3: Frontend** — only after Phase 1 + Phase 2 are deployed and CONTRACTS.md is locked

Check `STATUS.md` at the start of every session for the current stage. Never write code for a later
stage before the current one is confirmed done — if asked to, flag it instead of proceeding.

## Research & reference resources
Core FCC docs (read in this order before touching contracts or backend code):
- https://dev.flare.network/fcc/overview — architecture, FCE concept, TEE machine/proxy split
- https://dev.flare.network/fcc/guides — developer guides + example extensions
- https://dev.flare.network/fcc/troubleshooting
- https://dev.flare.network/fdc/overview and /fdc/getting-started — attestation types (Web2Json,
  EVMTransaction, Payment), Merkle proof flow
- https://dev.flare.network/fassets/overview — collateral/LTV conventions to stay consistent with
- https://dev.flare.network/network/overview and /network/getting-started — chain IDs, RPCs,
  FlareContractRegistry address (0xaD67FE66660Fb8dFE9d6b1b4240d8650e30F6019, same on all networks)
- https://dev.flare.network/network/guides/hardhat-foundry-starter-kit
- Full doc index for agent ingestion: https://dev.flare.network/llms.txt (any page also at `<url>.md`)

Reference repos (github.com/flare-foundation):
- `fce-extension-scaffold` — base template for the FCE (clone as the backend starting point)
- `fce-sign` — TEE signing pattern reference
- `fce-weather-api` — second reference for the POST /action handler shape (multi-language examples)
- `tee-node`, `tee-proxy`, `tee-relay-client` — FCC infra, read for context only, not run locally
- `flare-foundry-starter` — contracts scaffold (Foundry, chosen over Hardhat for faster Solidity
  testing/fuzzing)
- `flare-foundry-periphery-package` — Solidity interfaces for FTSO/FDC/registry integration
- `fassets` — FAsset protocol contracts, for LTV/collateral conventions to mirror
- `flare-ai-skills` — install before Phase 1:
  ```
  npx skills add https://github.com/flare-foundation/flare-ai-skills --skill flare-general
  npx skills add https://github.com/flare-foundation/flare-ai-skills --skill flare-fdc
  npx skills add https://github.com/flare-foundation/flare-ai-skills --skill flare-fassets
  ```

## Verified architecture facts (researched 2026-08-14 — these override the original build plan)
Read `docs/PLAN.md` for the full reasoning. Short version:
- **FCC is LIVE on Coston2**, not pre-production. 443 active TEE machines, ~715 registered extensions.
  Do not describe the TEE as simulated unless referring to `SIMULATED_TEE=true` local runs.
- **All TEE registries are facets of ONE EIP-2535 diamond**: `FlareTeeManager` at
  `0x1a9C4A0f9D76c0b1D91d22E24E573a9b377618aE` (Coston2). `ITeeExtensionRegistry` and
  `ITeeMachineRegistry` both point at this same address. Copy the interfaces from
  `fce-extension-scaffold/contracts/interfaces/` — do NOT hand-write them.
- **TEE results do NOT reach the chain by themselves.** tee-node signs the response; ext-proxy serves it
  at `/action/result`. A permissionless submit tx must carry it on-chain, where `UnderwritingVerifier`
  does `ecrecover` + a `MachineManager.getExtensionId`/`getTeeMachineStatus` membership check.
  TEE machine identity IS an EVM address, which is what makes this work.
- **An extension registers TWO contracts**:
  `ExtensionManager.register(address stateVerifier, address instructionsSender)`.
- **FDC cannot supply XRPL account history** — its types are per-transaction, and `Web2Json` publishes
  responses on-chain in the clear, which would break the privacy invariant. The TEE is the right tool.
- **OPType/OPCommand strings must be byte-identical in three places**: Solidity `bytes32("...")`, the Go
  config constants, and the handler routing switch. Avoid the reserved `F_` prefix.
- Unit and conformance tests need **no chain, no indexer, no Docker**. Only E2E needs them.
- Blocker: ext-proxy needs permissioned Flare indexer DB credentials.

## Standards
- Solidity: match `flare-foundry-periphery-package` interface conventions. Use FlareContractRegistry
  for cross-protocol lookups instead of hardcoding addresses. Target EVM version cancun. No contract is
  "done" without a full Foundry test suite.
- Go (FCE service): follow the directory structure and 4-step POST /action handler pattern from
  `fce-extension-scaffold`/`fce-weather-api`. Don't invent a different service shape.
- Privacy invariant: raw borrower data (XRPL history, computed features) must never leave the TEE
  boundary or be logged/persisted outside it. Only the final signed {address, riskTier, maxLTV, expiry}
  tuple is ever relayed out.
- Deploy target for everything: Coston2 (chain ID 114, RPC coston2-api.flare.network/ext/C/rpc, faucet
  at faucet.flare.network/coston2).
- FCC is still pre-production — anywhere the build simulates a piece of real TEE infra, document that
  clearly in code comments and in STATUS.md rather than presenting it as fully live.

## Phase 1 — Smart Contracts (spec)
- `UnderwritingVerifier.sol` — verifies a TEE-signed attestation against the registered TEE identity
  (mirrors FCC's FDC attestation pattern). `verifyAttestation(bytes calldata attestation) returns
  (address borrower, uint8 riskTier, uint256 maxLTV, uint256 expiry)`.
- `CreditRegistry.sol` — stores the latest valid attestation per address post-verification. Emits
  `CreditAttested(address indexed borrower, uint8 riskTier, uint256 maxLTV, uint256 expiry)`. Rejects
  stale/expired attestations.
- `TrustLinePool.sol` — standard fixed-LTV overcollateralized path (matches FAssets norms) plus an
  underwritten path that reads CreditRegistry and allows borrowing up to maxLTV while unexpired. Emits
  `Borrowed`, `Repaid`, `Liquidated`.
- `TrustLineInstructionSender.sol` — on-chain entry point, registered as the extension's
  `instructionsSender`. Keep the scaffold's `constructor`, `setExtensionId()` and `_getExtensionId()`
  unmodified (marked DO NOT MODIFY upstream).
- Interfaces: copy `ITeeExtensionRegistry.sol` / `ITeeMachineRegistry.sol` from
  `fce-extension-scaffold/contracts/interfaces/` and extend with `getExtensionId(address)` and
  `getTeeMachineStatus(address)`. The periphery package has no TEE interfaces — the scaffold does.
- Deliverables: full test suite, Coston2 deployment script, `docs/CONTRACTS.md` documenting every
  function signature and event.

## Phase 2 — Backend / FCE (spec)
Built on `fce-extension-scaffold`, Go, following the POST /action 4-step pattern:
1. Receive an instruction event (borrower address) relayed from `CreditRegistry`.
2. Inside the TEE: pull XRPL transaction history (direct public API call for the MVP; note where a real
   FDC Web2Json/EVMTransaction flow would replace this in production) — account age, payment volume,
   transaction count.
3. Rule-based scoring (documented weights/thresholds, no ML/black box) → riskTier (0–3) + maxLTV.
4. Sign `{borrower, riskTier, maxLTV, expiry}` with the TEE identity key, following the `fce-sign`
   pattern.
5. Push the signed result through the TEE proxy pattern for on-chain relay.
- Deliverables: Go service matching scaffold structure, documented scoring function, a types-server
  /decode entry, a local test harness simulating the full flow without a live TEE (document what's
  simulated vs real), and an updated `docs/CONTRACTS.md` with the exact attestation byte layout.

## Phase 3 — Frontend (spec)
Only start once Phase 1 and Phase 2 are deployed and `docs/CONTRACTS.md` is locked. Build states for:
not attested (connect XRPL address, request credit check), pending (honest wait state, no fake
progress), attested (risk tier, max LTV, expiry countdown, borrow comparison vs standard path), expired
(re-attest CTA), and borrow/repay/liquidation flows on TrustLinePool. Use Wagmi/viem via
`@flarenetwork/flare-wagmi-periphery-package` for typed contract calls against the locked ABI.

## Local environment notes
- Go 1.25.3 is installed at `/usr/local/go/bin` but is NOT on PATH. Use the absolute path or
  `export PATH="$PATH:/usr/local/go/bin"`.
- Foundry 1.7.1 uses Soldeer under the hood for `forge install`; dependencies land in `contracts/lib/`
  and remappings are declared explicitly in `contracts/foundry.toml`, not in a `remappings.txt`.

## Working agreement
- Update `STATUS.md` at the end of every session with current stage and what's left.
- Stop and summarize after each stage instead of silently continuing into the next one.
- If a stage's assumptions turn out wrong once you're inside it, flag it before improvising a fix.
