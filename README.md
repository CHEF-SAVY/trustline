# TrustLine — Confidential Underwriting for XRPFi Lending on Flare

Flare's XRPFi/FAssets lending markets are 100% overcollateralized because no underwriting layer exists.
TrustLine adds one: a Flare Compute Extension (FCE) reads a borrower's XRPL transaction history inside a
TEE, scores it with a transparent rule-based function, and signs a minimal attestation
(`{borrower, riskTier, maxLTV, expiry}`) that a lending pool contract can verify and act on — so a pool can
offer under-collateralized loans without the borrower's raw transaction data ever touching the chain or
any operator. Built for the Flare Summer Signal hackathon, Bounty 2: Confidential Compute Apps.

## Repo layout

| Path | What lives here |
|---|---|
| `contracts/` | Foundry project. `UnderwritingVerifier.sol`, `CreditRegistry.sol`, `TrustLinePool.sol` and the TEE registry interface (Phase 1). |
| `backend/` | Go FCE service, shaped after `fce-extension-scaffold`. XRPL fetch → scoring → TEE signing (Phase 2). |
| `frontend/` | Wallet UI against the locked ABI (Phase 3). |
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
