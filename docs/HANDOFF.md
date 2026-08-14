# TrustLine — handoff

Written 2026-08-14 for whoever picks this up next (Codex or otherwise). Assumes no prior context.

---

## 1. What this is, in one paragraph

DeFi lending on Flare is 100% overcollateralized — deposit $1,000, borrow $500 — because the
contract has no way to tell whether a borrower is reliable. Their XRPL history could prove it, but
using that on-chain would mean publishing their whole financial life permanently. **TrustLine runs
the credit check inside a TEE** (a hardware enclave the machine's operator cannot see into), and the
enclave signs only a verdict: `{borrower, xrplAddressHash, riskTier, maxLTV, issuedAt, expiry}`. A
pool contract verifies that signature came from a genuine registered enclave and lends against the
tier. Same collateral, 75% instead of 50%, and the transaction history is never published.

Built for the Flare Summer Signal hackathon, Bounty 2 (Confidential Compute).

**Why a TEE and not FDC** — the question judges will ask: FDC's attestation types are
per-transaction, so they cannot summarise an account's history; and `Web2Json` publishes its
response on-chain in the clear, which defeats the entire purpose. The confidentiality *is* the
product.

---

## 2. Current state

**Deployed and live on Coston2 (chain 114).** Extension ID **66285**, owned by the deployer.

| Thing | Address |
|---|---|
| `TrustLineInstructionSender` | `0x33C7E0D2d9da4eF91de1C99Cfd33692e640DfD0E` |
| `UnderwritingVerifier` | `0xB032C85F0dEF5107b7178Ae09d75eCdD5dC3b6e1` |
| `CreditRegistry` | `0xFC7aeDaCcD34AA685d60141BFFE9568FB71f8D9A` |
| `TrustLinePool` | `0xE2A6be036cfFedf83406772D31e745cBA8EA3e58` |
| Pool asset — FXRP (`FTestXRP`), **6 decimals** | `0x0b6A3645c240605887a5532109323A3E12273dc7` |
| `FlareTeeManager` diamond (all TEE facets) | `0x1a9C4A0f9D76c0b1D91d22E24E573a9b377618aE` |
| Deployer | `0x3d582d907c8d73764b261c25c1D7C317Eaaee034` (~92 C2FLR, 10 FXRP) |

**Tests: 50 passing** — 34 Solidity (Foundry) + 16 Go — plus 11 frontend selectors verified.

```bash
cd contracts && forge test          # 34
cd backend  && go test ./...        # 16
cd frontend && npm run check-selectors
```

Go is at `/usr/local/go/bin` and **not on PATH**. Foundry needs `$HOME/.foundry/bin`.

### Works right now
Connect wallet, read position, deposit FXRP, borrow at 50%, repay. All real on-chain.

### Does NOT work yet
`requestCreditCheck` emits an instruction that **nothing answers** — no TEE machine is registered to
extension 66285. The UI will sit in "pending" forever. This is the single biggest remaining gap.

---

## 3. Repo layout and branches

```
contracts/   Foundry. Solidity 0.8.28, EVM cancun. Deps are git submodules —
             clone with --recurse-submodules.
backend/     Go FCE extension (module `trustline-fce`), built on
             flare-foundation/fce-extension-scaffold. Also holds cmd/relayer
             and cmd/gen-fixtures.
frontend/    Dependency-free. No bundler, no npm install, no CDN.
docs/        PLAN.md (architecture + research), CONTRACTS.md (the spec —
             source of truth), RUNBOOK.md (Coston2 deploy + FCC debugging),
             indexer-request.txt.
```

Branch stack, each a PR onto the previous:

```
main  ←  phase-1-contracts  ←  phase-2-fce-backend  ←  phase-3-relayer  ←  phase-4-frontend
```

`main` is scaffold only. **Active work is on `phase-4-frontend`.** GitHub:
https://github.com/CHEF-SAVY/trustline

---

## 4. Things that will waste your day if you don't know them

These were all learned the hard way. Every one cost real time.

### 4.1 The TEE signature scheme has three silent failure modes
`UnderwritingVerifier` reconstructs exactly what `tee-node` signs:

```
innerHash = keccak256( keccak256(Data) ‖ ID ‖ keccak256(SubmissionTag) ‖ Status )
digest    = keccak256( abi.encode( bytes32("TEE_ACTION_RESULT"), chainId, innerHash ) )
teeId     = ecrecover( toEthSignedMessageHash(digest), signature )
```

1. The signature **is EIP-191 prefixed**. Verifying the bare digest recovers a *wrong address
   silently* — no revert.
2. The outer payload is **`abi.encode`**; the inner hash is **packed**. The asymmetry is real.
3. Non-canonical (high-s) signatures must be rejected — use OZ `ECDSA.recover`.

**Do not "simplify" this.** `backend/cmd/gen-fixtures` links the real `tee-node v0.0.24` and
`go-flare-common v1.2.2` packages to generate golden signatures, and the Foundry suite asserts our
Solidity agrees byte-for-byte. If you change the payload, regenerate:

```bash
cd backend && go run ./cmd/gen-fixtures > ../contracts/test/fixtures/tee-signatures.json
```

### 4.2 `forge script` bakes simulated return values into later transactions
It simulates the whole script, then broadcasts the resulting tx list. A value **returned** by an
on-chain call during simulation is frozen into subsequent transactions.

This bit us: `register()` returned `66284` in simulation, another developer claimed that ID before
our broadcast landed, and the verifier deployed with an immutable ID we didn't own. Deployment is
now split into `run()` (register) and `deployConsumers()` (everything else), with the ID read back
from chain in between and asserted against `getExtensionOwner`.

### 4.3 FXRP has 6 decimals, not 18
Assuming 18 scales every amount by 10¹². The contracts are decimal-agnostic; only display and input
parsing care. Frontend uses `CONFIG.poolAssetDecimals`.

### 4.4 Frontend function selectors are hardcoded, and wrong ones fail *silently*
No keccak library is bundled, so selectors in `frontend/app.js` are literals. A wrong selector makes
`eth_call` return `0x` instead of reverting — the UI renders zeros forever. **Every read selector in
the first draft was wrong.** Always run `npm run check-selectors` after touching contract signatures.

### 4.5 Fail-visible, never fail-blank
CSS must not hide content that JS reveals. An early version did, and when the module failed to load
(ES modules are blocked over `file://`) the entire page rendered blank. The pattern now: content is
visible by default, and JS *opts into* hiding by setting `.motion-ready`. Keep this property.

Related: serve the frontend over HTTP, never open `index.html` directly.
```bash
cd frontend && python3 -m http.server 8080
```

### 4.6 FCC facts that contradict the official docs
- **Extension registration is permissionless.** Verified by simulating `register()` from an
  unprivileged address. You do **not** need indexer credentials or Docker to register.
- **Data providers POST instructions directly to your proxy** at `/instruction`. The proxy does NOT
  discover them from the indexer. A 404 from `/action/result` usually means the instruction never
  arrived, not that the proxy is down.
- Machine status: **1 = INITIALIZED, 2 = PRODUCTION**. Only 2 receives dispatches.
- A TEE restart creates a **new identity**; there's no way to restore an old `teeId`.
- The proxy response has **two** signatures. Use `signature` (the TEE's). `proxySignature` is the
  proxy's own and submitting it reverts with `UnknownTeeMachine`.
- `relayer -doctor -tee <teeId>` runs the whole delivery checklist. Use it before debugging anything.

`docs/RUNBOOK.md` has the full version.

---

## 5. NEW DESIGN DIRECTION — read this before touching the frontend

The previous direction (a Flipside Crypto-style light page with heavy scroll-driven motion) is
**dropped**. The user has explicitly moved on from it.

### What the user wants now
- **Dark background as the base** for the whole project — not a light page that inverts.
- **Different design concepts per section.** Each section should have its own visual idea rather
  than the whole page running one uniform system. Variety section-to-section is the point.
- **No heavy motion.** Scroll-scrubbed animation, pinned sections, and elaborate choreography are
  not wanted. Keep interactions simple; subtle transitions are fine, spectacle is not.

### What to keep from the current build
- Monochrome discipline. Black and white carry it; other colours should barely register if at all.
  This was an explicit, repeated instruction.
- **Not too large on screen.** The user pushed back on oversized type. Nothing below 12px either.
- Honest states: the "pending" state must not fake progress — the relay genuinely has no callback.
- Accessibility floors already in place: 44px touch targets, visible labels (not placeholder-only),
  contrast ≥4.5:1, `prefers-reduced-motion` respected, tabular figures for changing numbers.

### What to delete or rewrite
- `frontend/motion.js` — the scroll-scrub engine and the sticky hero stage are no longer wanted.
  The IntersectionObserver reveal logic can stay *if* simple fades are still desired, but the
  `initHeroScrub()` function and `.hero-stage` / `.hero-sticky` CSS should go.
- `frontend/index.html` hero section — built entirely around the scroll stage.
- `frontend/styles.css` — tokens and primitives are reusable; the light-first palette needs
  inverting to dark-first.

### What must survive any rewrite
`frontend/app.html` uses **32 element IDs that `app.js` depends on**. Changing markup is fine;
dropping an ID silently breaks the app. Verify with:

```bash
cd frontend && python3 - <<'PY'
import re
js=open('app.js').read(); html=open('app.html').read()
need=set(re.findall(r'\$\("([A-Za-z0-9_]+)"\)', js)) | set(re.findall(r'showErr\("([A-Za-z0-9_]+)"', js))
have=set(re.findall(r'id="([A-Za-z0-9_]+)"', html))
print("MISSING:", sorted(need-have) or "none")
PY
```

The five app states are driven by `app.js` `show()`: `#notAttested`, `#pending`, `#attested`, plus
`#stateBadge .dot` and `#stateText`.

---

## 6. What to do next, in priority order

1. **Redesign the frontend** per §5. This is what the user asked for next.
2. **Stand up a TEE machine** so attestations actually work. Needs:
   - Flare indexer DB credentials. **Answered by Flare 2026-08-14: no VPN needed, and the
     credentials live in the hackathon channel's pinned message** (not issued per-request). The old
     public `indexer-reader` ones are dead. Paste them into
     `tee-runtime/config/proxy/extension_proxy.coston2.docker.toml` under `[db]`.
   - A **stable** public HTTPS URL — named cloudflared tunnel or reserved ngrok domain. Quick
     tunnels rotate on restart, and the URL is stored on-chain, so providers keep POSTing to the
     dead hostname. This is the most common cause of machines stuck at INITIALIZED.
   - Then: `pre-build.sh` → `start-services.sh --chain coston2` → `post-build.sh`, with
     `SIMULATED_TEE=true` (fine on Coston2, no special hardware needed).
   - Docker, Go, Foundry and jq are already installed and working on this machine.
   - `tee-runtime/` is a prepared working copy of the scaffold with our extension swapped in as its
     `go/` implementation, `config/extension.env` pinned to extension **66285** (so `pre-build.sh`
     must be SKIPPED — running it would register a second extension and orphan ours), and
     `.env.coston2` filled in apart from `EXT_PROXY_URL`. It is gitignored because it holds keys.
3. **Prove the verification path without a live TEE** (a good de-risking step, and faster than 2):
   sign an attestation locally with a known key, deploy a verifier pointed at a mock machine
   registry, and drive submit → verify → borrow-at-75% against real deployed contracts.
4. Demo video and submission write-up.

---

## 7. Honest limitations — keep stating these, don't oversell

- **Sybil resistance is weak.** Someone patient can age accounts and cycle funds to manufacture a
  score. Counterparty diversity raises the cost; it doesn't eliminate it.
- **The borrower↔XRPL link is public** — the address appears in the request event, because the
  enclave has to be told what to score. What stays private is the *evidence* behind the score.
- **The pool is simplified**: no interest accrual, no price oracle, single asset, and liquidation is
  a solvency backstop rather than a keeper market.
- **Single TEE machine**, not a k-of-n quorum.
- Nothing has been through an end-to-end run with a real TEE yet.

---

## 8. Working agreements the user has set

- Flag a scope/time tradeoff once, then **build the full thing** rather than proposing cuts.
- Lead with plain English and what a change *means*, before protocol detail. The user directs the
  project but is not a blockchain-internals specialist.
- Update `STATUS.md` at the end of a session.
- No `Co-Authored-By` trailers in commit messages.
- Don't commit build artifacts, the vendored `.agents/` skill packs, or large media
  (`ui-ux-design/*.mp4` is gitignored — it's 87MB).
- The private key lives in `.env` (gitignored) and should never be pasted into a chat.
