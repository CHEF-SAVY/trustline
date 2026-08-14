# STATUS

**Active branch: `phase-4-frontend`. TrustLine is deployed on Coston2.**

| Phase | State |
|---|---|
| Smart contracts | ✅ 34 Foundry tests passing; deployed |
| Go FCE backend | ✅ 16 Go tests passing |
| Relayer / integration | ✅ built; live TEE delivery still pending |
| Frontend | ✅ dark-first redesign implemented; live contract reads and writes wired |

## Live deployment

- Network: Coston2, chain ID 114
- Extension: `66285`
- Instruction sender: `0x33C7E0D2d9da4eF91de1C99Cfd33692e640DfD0E`
- Credit registry: `0xFC7aeDaCcD34AA685d60141BFFE9568FB71f8D9A`
- Pool: `0xE2A6be036cfFedf83406772D31e745cBA8EA3e58`
- Asset: FXRP (`FTestXRP`), 6 decimals

## Current frontend

The landing page and credit desk now use a dark-first, monochrome system with a separate visual
concept for each section. The old sticky scroll stage and scrubbed hero animation have been
removed. Interaction is limited to simple one-shot reveals and state transitions, with
`prefers-reduced-motion` and fail-visible behavior preserved.

Wallet connection, position reads, deposit, borrow, and repay are wired to the live deployment.
All 11 hardcoded selectors are verified against the ABI, and every DOM ID required by `app.js`
is present.

## Biggest remaining gap

Credit-check requests emit successfully, but no TEE machine is registered to extension 66285, so
an attestation remains pending indefinitely. The prepared `tee-runtime/` still needs current Flare
indexer credentials and a stable public HTTPS proxy URL before the end-to-end path can run.

See `docs/HANDOFF.md` and `docs/RUNBOOK.md` for the detailed deployment state and TEE bring-up steps.
