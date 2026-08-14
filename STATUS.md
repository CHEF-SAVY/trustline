# STATUS

**Branch `main`: Stage 0 — repository scaffolding.**

Structure, toolchain config, and pre-build research only. No business logic lives on `main`; each
build phase lands via a pull request from its own branch.

| Phase | Branch | State |
|---|---|---|
| 0 — Scaffolding | `main` | ✅ this branch |
| 1 — Smart contracts | `phase-1-contracts` | ✅ built, awaiting review |
| 2 — Go FCE backend | `phase-2-fce-backend` | ✅ built, awaiting review |
| 3 — Relayer / integration | `phase-3-relayer` | ✅ built, awaiting review |
| 4 — Frontend | `phase-4-frontend` | 🚧 first pass; active work |

Read `docs/PLAN.md` before touching any phase — it records five assumptions from the original build
plan that turned out to be wrong once checked against the live chain, and the architecture that
replaced them.

## Toolchain

| Tool | Version |
|---|---|
| Foundry | 1.7.1 |
| Go | 1.25.3 |
| Node | 22.x |

Dependencies in `contracts/lib/` are git submodules — clone with `--recurse-submodules`, or run
`git submodule update --init --recursive` afterwards.

## Not yet done

- Nothing is deployed to Coston2. That needs a funded testnet key (faucet.flare.network/coston2).
- The live instruction-relay leg needs permissioned Flare indexer credentials; see
  `docs/INDEXER_ACCESS_REQUEST.md` on the phase-3 branch.
