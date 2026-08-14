# Deployment runbook — Coston2

Written against the **post-redeploy** FCC stack (Coston2 FCC was redeployed; the old deployment died
22 Jul 2026). Every value here was verified on-chain on 2026-08-14.

## 0. Preflight — are you on the live stack?

| Thing | Correct value | How it fails if wrong |
|---|---|---|
| `FlareTeeManager` | `0x1a9C4A0f9D76c0b1D91d22E24E573a9b377618aE` | Old `0x004224fa…5d41F` gives `FunctionNotFound`, "only reward offers manager", reverting `register()` |
| `tee-node` | ≥ v0.0.22 — we pin **v0.0.24** | Older: every data-provider vote is rejected, so the queue stays empty forever |
| `tee-proxy` | v0.0.18 (scaffold-pinned) | — |
| Scaffold | latest `main` | Guides lag the redeploy |

**Do not mix independently-updated versions** of `tee-node` / `tee-proxy` / `go-flare-common`. Use
whatever the scaffold's `main` pins. We inherit its `go.mod`, so this is already correct.

Verified for this repo:

```
$ cast call 0x1a9C…18aE "nextPublicExtensionId()(uint256)" --rpc-url coston2
66275          # was 66251 earlier the same day — registrations are actively succeeding
$ grep tee-node backend/go.mod
github.com/flare-foundation/tee-node v0.0.24     # >= v0.0.22 ✅
```

## 1. The delivery model (read this before debugging anything)

```
dispatch tx on-chain
   → data providers cosign
   → providers POST directly to YOUR proxy:  POST <registered-url>/instruction   (:6664)
   → tee-node → your extension  POST /action
   → result served at  GET /action/result/{actionID}
```

**Your proxy does not pull instructions from the indexer.** Providers push to the URL stored
**on-chain**. Everything about delivery therefore depends on that on-chain record being right.

Ports: `6664` external inside the container, mapped to `6674` on the host
(`docker-compose.yaml`: `${EXT_PROXY_EXTERNAL_BIND:-0.0.0.0:6674}:6664`); `6661` internal
(`/healthy`, `/startup`, `/ready`).

## 2. URL: use a stable hostname

The URL is stored **on-chain**. If it changes, providers keep POSTing to the dead one and nothing
ever arrives — machines stuck at `INITIALIZED` usually have a dead hostname on-chain.

- ❌ `trycloudflare.com` quick tunnels, free ngrok — hostname rotates on restart
- ✅ a **named** cloudflared tunnel, or a **reserved** ngrok domain, or any stable public HTTPS host

If the tunnel does rotate: update `EXT_PROXY_URL` and **re-run post-build**.
Provider source IPs cannot be allowlisted — providers are independent operators. HTTP/1.1 vs HTTP/2
does not matter.

## 3. Deploy

```bash
# 3a. Deploy the instruction sender
cd contracts
forge script script/Deploy.s.sol:DeployScript --sig 'deploySender()' \
  --rpc-url coston2 --broadcast

# 3b. Register the extension (fresh EXTENSION_ID — the redeploy may have wiped registrations)
./scripts/pre-build.sh
./scripts/start-services.sh --chain coston2
./scripts/post-build.sh
# register-tee uses -command rRap  (capital R forces a FRESH challenge)

# 3c. Latch the extension id, then deploy the rest
cast send <sender> "setExtensionId()" --rpc-url coston2 --private-key $DEPLOYER_PRIVATE_KEY
TRUSTLINE_EXTENSION_ID=<id> TRUSTLINE_POOL_ASSET=<erc20> \
  forge script script/Deploy.s.sol:DeployScript --rpc-url coston2 --broadcast
```

`SIMULATED_TEE=true` is fine on Coston2 for judging. GCP Confidential Space is **not** required.
A simulated TEE reaches PRODUCTION in seconds on a current stack — if yours doesn't, it's client-side.

Record every address in `docs/CONTRACTS.md` §8 and `frontend/config.js`.

## 4. Verify before blaming the network

```bash
cd backend && go run ./cmd/relayer -doctor -tee <teeId> -proxy <your-url>
```

Checks, in the order things actually fail: live diamond → registered → status `2` → availability
window → on-chain URL matches what you serve → `GET /info` responds.

Status values: **1 = INITIALIZED**, **2 = PRODUCTION**. Only 2 receives dispatches, and only 2 is
accepted by `UnderwritingVerifier`.

Manual equivalents:

```bash
cast call 0x1a9C…18aE "getTeeMachine(address)((address,address,string))" <teeId> --rpc-url coston2
cast call 0x1a9C…18aE "getTeeMachineStatus(address)(uint8)" <teeId> --rpc-url coston2
```

## 5. Relay the result

```bash
go run ./cmd/relayer -proxy <url> -action <actionId> -dry-run          # decode only
go run ./cmd/relayer -proxy <url> -action <actionId> -registry <addr>  # submit on-chain
go run ./cmd/relayer -status -proxy <url> -epoch <n> -instruction <id> # voting state
```

Endpoint is `GET /action/result/{actionID}` — **path** parameter, plus optional
`?submissionTag=` defaulting to `threshold`.

⚠️ The response contains **two** signatures. Use `signature` (the TEE's, domain `TEE_ACTION_RESULT`).
`proxySignature` is the proxy's own (domain `PROXY_ACTION_RESULT`) and submitting it would recover
the proxy's address and revert with `UnknownTeeMachine`.

## 6. Interpreting a 404 from the proxy

A 404 does **not** mean the proxy is down. For a recent action it usually means the instruction never
reached that proxy. Delivered actions instead show signatures accumulating while providers vote.
Very old 404s are hard to interpret either way.

Flare's own Coston2 FTDC proxies, for comparison:
`https://tee-proxy-coston2-1.flare.rocks` (primary), `https://tee-proxy-coston2-2.flare.rocks`.

## 7. One machine per endpoint

The registry permits several machines on one URL; **don't**. Keep one active machine per endpoint —
each dispatch selects a single machine, so a stale one registered under the same extension produces
*apparently random* failures.

**A restart creates a NEW TEE identity.** The key is not persisted, in simulated or production mode.
Recovery: restart → new identity → re-register → reach PRODUCTION → **pause the stale identity**.
There is no supported way to restore an old `teeId`.

> **Consequence for TrustLine, worth knowing before it bites:** `UnderwritingVerifier` requires the
> signing machine to be PRODUCTION *at submit time*. So if the TEE restarts and you pause the old
> identity, any attestation it signed that hasn't been submitted yet becomes unsubmittable, even
> though it was legitimate when produced. Attestations are cheap to reissue, so the mitigation is
> simply: **relay promptly, and re-request after a restart.** We keep the strict check because
> failing closed on an unrecognised signer is the property the whole design rests on.

## 8. What you cannot see

Whether each individual provider attempted delivery and what HTTP response it got — that lives in
provider logs. If everything above is green and instructions still never land, report: extension ID,
teeId, dispatch tx, registered URL, machine status, and `/action/status` output.
