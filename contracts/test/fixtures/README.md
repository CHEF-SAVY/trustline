# Test fixtures

`tee-signatures.json` is **generated**, not hand-written. Regenerate with:

```bash
cd backend && go run ./cmd/gen-fixtures > ../contracts/test/fixtures/tee-signatures.json
```

## About the `privateKey` fields

These are **throwaway test keys, deliberately committed.** They exist only so the Foundry suite can
verify our Solidity recovers the same signer that Flare's Go code produced. They hold no funds, are
not used on any network, and control nothing.

They are published on purpose: without them the cross-language signature test could not be
reproduced by anyone cloning this repo, which is the whole point of that test.

**Never put a key with funds here.** Deployment keys belong in `.env`, which is gitignored.
