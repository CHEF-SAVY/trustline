# Indexer access request

> **CORRECTION (2026-08-14).** An earlier version of this file said the indexer is how `ext-proxy`
> "picks up instructions." **That was wrong**, and it matters because it points debugging in
> completely the wrong direction.
>
> **Data providers POST the cosigned instruction DIRECTLY to your proxy at `POST /instruction`.**
> The proxy does not discover instructions by reading the indexer. Verified in `tee-proxy v0.0.18`
> (`internal/server/external.go:134`).
>
> What the proxy actually uses the DB for (`tee-proxy` sources):
> - `internal/proxy/proxy.go:52` — `WaitCIndexerToSync`, a startup gate
> - `internal/service/machinepath` — watching for signed machine path lists
> - `internal/liveness/liveness.go:88` — readiness (`c-chain indexer delay`)
>
> So credentials are still needed to **run** a proxy (it panics if the indexer never syncs), but they
> are **not** the reason an instruction fails to arrive. If instructions aren't landing, run
> `go run ./cmd/relayer -doctor -tee <teeId>` instead — the cause is almost always machine status or
> a stale on-chain URL.

**Also note:** the indexer-reader credentials printed in older docs are dead. Use the ones from the
hackathon channel's pinned message. Expected indexer lag is effectively zero — check
`GET :6661/ready` (200 = current; 503 with `c-chain indexer delay` = genuinely behind). Do not infer
lag from gaps in the logs table: the hackathon DB only indexes selected contracts and topics.

---

## Draft message

> **Subject: Coston2 indexer DB credentials for an FCE — Summer Signal Bounty 2**
>
> Hi — I'm building a Flare Compute Extension for the Summer Signal hackathon (Bounty 2, Confidential
> Compute Apps) and need current indexer DB credentials so `ext-proxy` can pass its startup sync and
> readiness checks on Coston2. I understand the ones in the older docs are dead.
>
> **What I'm building — TrustLine:** an underwriting layer for XRPFi/FAssets lending. XRPFi markets
> today are fully overcollateralized because there's no way to assess a borrower without exposing
> their history. TrustLine runs the assessment *inside the TEE*: the extension reads a borrower's
> XRPL transaction history, computes a risk tier and a max LTV with a transparent rule-based
> function, and returns only a signed `{borrower, riskTier, maxLTV, expiry}` tuple. A pool contract
> verifies the TEE signature on-chain and lends against the tier. Raw history never touches the chain
> and is never seen by any operator.
>
> This is a genuine fit for FCC rather than FDC: FDC's attestation types are per-transaction, and
> Web2Json would publish the response on-chain in the clear, defeating the privacy goal entirely.
>
> Stack is current: `FlareTeeManager 0x1a9C4A0f9D76c0b1D91d22E24E573a9b377618aE`, scaffold `main`,
> `tee-node v0.0.24`, Coston2 only, `SIMULATED_TEE=true`.
>
> Thanks very much — the hackathon closes today.
>
> [your name / GitHub handle / contact]

---

## While waiting

Not a blocker for most of the build. `docs/testing.md` in the scaffold confirms the **unit** and
**conformance** layers run with no chain, no indexer and no Docker — which is how all 50 of our tests
pass today. Contracts also deploy and test on Coston2 independently.
