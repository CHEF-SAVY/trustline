# TrustLine — Confidential Underwriting for XRPFi Lending on Flare

Flare's XRPFi/FAssets lending markets are 100% overcollateralized because no underwriting layer exists.
TrustLine adds one: a Flare Compute Extension (FCE) reads a borrower's XRPL transaction history inside a
TEE, scores it with a transparent rule-based function, and signs a minimal attestation
(`{borrower, riskTier, maxLTV, expiry}`) that a lending pool contract can verify and act on — so a pool can
offer under-collateralized loans without the borrower's raw transaction data ever touching the chain or
any operator. Built for the Flare Summer Signal hackathon, Bounty 2: Confidential Compute Apps.

## Live on Coston2

**Demo:** https://trustline-flare.vercel.app — connect a wallet on Coston2 and deposit, borrow
and repay against the real deployed pool. The UI reads the chain directly; there is no backend API
behind it.

| Contract | Address |
|---|---|
| `TrustLineInstructionSender` | `0x33C7E0D2d9da4eF91de1C99Cfd33692e640DfD0E` |
| `UnderwritingVerifier` | `0x4520b699700945A0Fcd1AECac79099aEBe466C89` |
| `CreditRegistry` | `0x539296b1A1210A7a6aEC99E2d311d0a89F350f69` |
| `TrustLinePool` | `0x41aC977D774cB86EC7f9b3125776C50e97bd9CE6` |
| `DemoTeeMachineRegistry` (see below) | `0x6209AcbaEa55ccCD874F58aA4B5eE889128bD75B` |
| Pool asset — FXRP (`FTestXRP`), **6 decimals** | `0x0b6A3645c240605887a5532109323A3E12273dc7` |
| FCC extension | **66285**, TEE machine `0xD173b2e8ce371ded0384e3a426566aEf1E45346b` |

### Known limitation — the credit check does not complete on testnet

Our TEE machine is registered on-chain under extension 66285 but is stranded at status `1`
(INITIALIZED). FCC's `MachineManager.toProduction()` reverts with **no revert data at all** — not one
of its ~70 named errors, which would each return a selector. Ruled out: stale proofs, a missing
signing policy, duplicate attestation requests, `tee-node` v0.0.24 vs v0.0.25, reward-epoch skew
(retried with proof policy and on-chain epoch both at 5940), and gas (fails with an explicit 30M cap
on both `eth_call` and `eth_estimateGas`). Flare's own FTDC machines *are* at PRODUCTION, so the
mechanism works and ours is specifically rejected.

Only PRODUCTION machines receive dispatches, so `requestCreditCheck` emits an instruction that
nothing answers, and the app's pending state says so plainly rather than faking progress.

`DemoTeeMachineRegistry` exists so the underwritten path can still be shown. It relaxes **exactly
one** thing — a registered machine is reported as PRODUCTION. Extension membership is still forwarded
to the real diamond, unregistered signers still revert, and the TEE signature is still verified in
full by `ecrecover`. It is **not for production**: point `UnderwritingVerifier` back at
`FlareTeeManager` (`0x1a9C4A0f9D76c0b1D91d22E24E573a9b377618aE`) once `toProduction` works upstream.
No other contract changes.

## Repo layout

| Path | What lives here |
|---|---|
| `contracts/` | Foundry project. `UnderwritingVerifier.sol`, `CreditRegistry.sol`, `TrustLinePool.sol` and the TEE registry interface (Phase 1). |
| `backend/` | Go FCE service, shaped after `fce-extension-scaffold`. XRPL fetch → scoring → TEE signing (Phase 2). |
| `frontend/` | Wallet UI against the locked ABI (Phase 3). Dependency-free static site — `index.html` is the landing page, `live/` is the credit desk. Deploys to Vercel as-is. |
| `tee-runtime/` | Working copy of `fce-extension-scaffold` with our extension as its `go/` implementation. Gitignored — holds keys. Runs the enclave + proxy in Docker. |
| `docs/CONTRACTS.md` | Source of truth for every function signature, event, and the attestation byte layout. |
| `STATUS.md` | Current build stage. **Read this first every session.** |
| `CLAUDE.md` | Persistent project context for Claude Code. |

## Build order (strict gate)

Stage 0 scaffolding → Phase 1 contracts → Phase 2 FCE backend → Phase 3 frontend.
Each stage defines the data shapes the next one builds against, so they don't get reordered.
See `STATUS.md` for where things currently stand.

## Prerequisites

- **Foundry** 1.7+ — `forge --version`
- **Go** 1.25+ — `go version`
- **Node** 22+ — `node -v`

> On this machine Go is installed at `/usr/local/go/bin` but is not on `PATH`.
> Add `export PATH="$PATH:/usr/local/go/bin"` to your shell profile, or prefix commands with the full path.

## Quick start

```bash
cp .env.example .env                 # throwaway Coston2 key only

cd contracts && forge test           # 34 tests
cd ../backend && go test ./...       # 16 tests
cd ../frontend && npm run check-selectors
```

## How it works

```
borrower → TrustLineInstructionSender.requestCreditCheck(xrplAddress)   [on-chain]
         → data providers relay the instruction to a TEE machine
         → our Go extension: fetch XRPL history → score → CreditAttestation
         → tee-node signs it; ext-proxy serves it at /action/result
         → ANYONE submits it: CreditRegistry.submitAttestation(...)     [on-chain]
              └─ UnderwritingVerifier: ecrecover + registry provenance check
         → TrustLinePool lends up to the attested LTV instead of the standard one
```

That second-to-last step is ours. FCC signs results and serves them from the proxy, but nothing
carries them on-chain — see `docs/PLAN.md` §2. Because trust rests on the TEE signature rather than
the sender, the submit step is permissionless: anyone can relay, and no relayer can alter a tier.

**Why a TEE and not FDC?** FDC's attestation types are per-transaction, so they cannot summarise an
account's history; and `Web2Json` publishes its response on-chain in the clear, which would defeat
the entire point. The confidentiality is the product.

## The load-bearing test

`backend/cmd/gen-fixtures` links the real `tee-node` and `go-flare-common` packages, encodes an
attestation with the same encoder the extension uses in production, and signs it through Flare's own
signing path. `contracts/test/UnderwritingVerifier.t.sol` then asserts our Solidity recovers the same
signer and decodes identical fields.

This matters because the scheme has three ways to fail *silently* — the signature is EIP-191
prefixed, the outer payload is `abi.encode` while the inner hash is packed, and non-canonical
signatures must be rejected. Get any of them wrong and `ecrecover` returns a plausible-looking wrong
address instead of reverting. Regenerate with:

```bash
cd backend && go run ./cmd/gen-fixtures > ../contracts/test/fixtures/tee-signatures.json
```

## Network

Everything targets **Coston2** (Flare testnet), chain ID `114`,
RPC `https://coston2-api.flare.network/ext/C/rpc`, faucet at https://faucet.flare.network/coston2.
`FlareContractRegistry` is at `0xaD67FE66660Fb8dFE9d6b1b4240d8650e30F6019` on every Flare network.

## Privacy invariant

Raw borrower data — XRPL history and any computed features — must never leave the TEE boundary, and must
never be logged or persisted outside it. Only the final signed
`{borrower, riskTier, maxLTV, expiry}` tuple is relayed out.
