// Command relayer carries TEE-signed attestations from the extension proxy onto the chain.
//
// WHY THIS EXISTS: FCC has no automatic result-to-chain path. tee-node signs an ActionResult and
// ext-proxy serves it at /action/result, but nothing submits it. Without this step a TrustLine
// attestation would never reach CreditRegistry. (Confirmed in fce-extension-scaffold
// docs/architecture.md — the documented flow ends at the proxy.)
//
// Trust rests entirely on the TEE signature, so this relayer is UNTRUSTED infrastructure: it cannot
// alter a tier, forge an attestation, or attribute one to the wrong borrower. Anyone can run it. If
// it disappears, borrowers can submit their own results. That is the point of the design.
//
// Usage:
//
//	relayer -proxy https://<proxy-url> -action <actionId> -registry 0x... -rpc <url>
package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"trustline-fce/pkg/types"
)

// submitAttestationABI is the single CreditRegistry method the relayer needs.
const submitAttestationABI = `[{
  "inputs": [
    {"internalType":"bytes","name":"_data","type":"bytes"},
    {"internalType":"bytes32","name":"_id","type":"bytes32"},
    {"internalType":"uint8","name":"_status","type":"uint8"},
    {"internalType":"bytes","name":"_signature","type":"bytes"}
  ],
  "name": "submitAttestation",
  "outputs": [],
  "stateMutability": "nonpayable",
  "type": "function"
}]`

// proxyResult mirrors tee-node's ActionResponse, served by ext-proxy at
// GET /action/result/{actionID}.
//
// CAREFUL — there are TWO signatures in this response and only one is the right one:
//
//	signature       the TEE machine's, over domain TEE_ACTION_RESULT. THIS is what
//	                UnderwritingVerifier recovers and what establishes trust.
//	proxySignature  the proxy's own, over domain PROXY_ACTION_RESULT
//	                (tee-proxy internal/server/external.go:269). It attests only that this proxy
//	                served the bytes; it says nothing about the TEE. Submitting it on-chain would
//	                recover the proxy's address, which is not a registered TEE machine, and revert
//	                with UnknownTeeMachine.
//
// We decode proxySignature purely so its existence is explicit rather than silently dropped.
type proxyResult struct {
	Result struct {
		ID            common.Hash   `json:"id"`
		SubmissionTag string        `json:"submissionTag"`
		Status        uint8         `json:"status"`
		Log           string        `json:"log"`
		Data          hexutil.Bytes `json:"data"`
	} `json:"result"`
	Signature      hexutil.Bytes `json:"signature"`
	ProxySignature hexutil.Bytes `json:"proxySignature"`
}

// instructionStatus mirrors GET /action/status/{rewardEpochID}/{instructionID}, the endpoint for
// working out how far an instruction got. Decoded loosely because the shape is diagnostic only.
type instructionStatus map[string]any

func main() {
	var (
		proxyURL    = flag.String("proxy", "", "extension proxy base URL")
		actionID    = flag.String("action", "", "action id to fetch")
		registryHex = flag.String("registry", "", "CreditRegistry address")
		rpcURL      = flag.String("rpc", "https://coston2-api.flare.network/ext/C/rpc", "Flare RPC")
		dryRun      = flag.Bool("dry-run", false, "decode and print without submitting")
		timeout     = flag.Duration("timeout", 60*time.Second, "overall timeout")

		statusMode    = flag.Bool("status", false, "query per-instruction voting state instead of relaying")
		epoch         = flag.Uint64("epoch", 0, "reward epoch id (with -status)")
		instructionID = flag.String("instruction", "", "instruction id (with -status)")

		doctor   = flag.Bool("doctor", false, "run the FCC delivery checklist against a teeId")
		teeIDHex = flag.String("tee", "", "teeId to diagnose (with -doctor)")
	)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if *doctor {
		if *teeIDHex == "" {
			log.Fatal("-doctor requires -tee <teeId>")
		}
		if err := runDoctor(ctx, *rpcURL, common.HexToAddress(*teeIDHex), *proxyURL); err != nil {
			log.Fatalf("doctor: %v", err)
		}
		return
	}

	if *statusMode {
		if *proxyURL == "" || *instructionID == "" {
			log.Fatal("-status requires -proxy and -instruction (and usually -epoch)")
		}
		st, err := fetchStatus(ctx, *proxyURL, *epoch, *instructionID)
		if err != nil {
			log.Fatalf("fetching status: %v", err)
		}
		out, _ := json.MarshalIndent(st, "", "  ")
		fmt.Println(string(out))
		return
	}

	if *proxyURL == "" || *actionID == "" {
		log.Fatal("-proxy and -action are required")
	}
	if !*dryRun && *registryHex == "" {
		log.Fatal("-registry is required unless -dry-run")
	}

	res, err := fetchResult(ctx, *proxyURL, *actionID)
	if err != nil {
		log.Fatalf("fetching result: %v", err)
	}

	// Only successful results carry an attestation. 0 is an error, >=2 means still pending.
	if res.Result.Status != 1 {
		log.Fatalf("action status %d (%s) — nothing to submit", res.Result.Status, res.Result.Log)
	}

	att, err := types.DecodeCreditAttestation(res.Result.Data)
	if err != nil {
		log.Fatalf("decoding attestation: %v", err)
	}

	fmt.Printf("attestation for %s\n  tier      %d\n  maxLTV    %d bips\n  issuedAt  %d\n  expiry    %d\n",
		att.Borrower.Hex(), att.RiskTier, att.MaxLTVBips, att.IssuedAt, att.Expiry)

	if att.Expiry <= uint64(time.Now().Unix()) {
		log.Fatalf("attestation already expired at %d; the registry would reject it", att.Expiry)
	}

	if *dryRun {
		fmt.Println("dry run — not submitting")
		return
	}

	txHash, err := submit(ctx, *rpcURL, common.HexToAddress(*registryHex), res)
	if err != nil {
		log.Fatalf("submitting: %v", err)
	}
	fmt.Printf("submitted: %s\n", txHash.Hex())
}

// fetchResult reads a signed action result from the proxy.
//
// The actionID is a PATH parameter, not a query parameter — see tee-proxy
// internal/server/external.go:135, `GET /action/result/{actionID}`. submissionTag is an optional
// query parameter defaulting to "threshold", which is the tag our extension's result carries.
func fetchResult(ctx context.Context, proxyURL, actionID string) (*proxyResult, error) {
	url := fmt.Sprintf("%s/action/result/%s?submissionTag=threshold",
		strings.TrimRight(proxyURL, "/"), actionID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		// A 404 here does NOT mean the proxy is down. For a recent action it usually means the
		// instruction never reached THIS proxy — providers POST instructions directly to
		// /instruction, so delivery depends on the machine being PRODUCTION, having a fresh
		// availability check (<6h), and a stable public HTTPS URL.
		return nil, fmt.Errorf(
			"404 from proxy for action %s.\n"+
				"  This usually means the instruction never reached this proxy, not that the proxy is down.\n"+
				"  Check, in order:\n"+
				"    1. machine status is 2 (PRODUCTION)\n"+
				"    2. availability check is fresh (<6h)\n"+
				"    3. the URL registered on-chain matches this proxy and is stable public HTTPS\n"+
				"    4. GET %s/info responds\n"+
				"    5. relayer -status -epoch <n> -instruction <id> for per-provider voting state",
			actionID, strings.TrimRight(proxyURL, "/"))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy returned status %d", resp.StatusCode)
	}

	var out proxyResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding proxy response: %w", err)
	}
	if len(out.Signature) == 0 {
		return nil, fmt.Errorf("proxy returned no TEE signature — the result may not be signed yet")
	}
	return &out, nil
}

// fetchStatus queries per-instruction voting state, the main tool for locating where an instruction
// stalled. GET /action/status/{rewardEpochID}/{instructionID} (external.go:136).
func fetchStatus(ctx context.Context, proxyURL string, epoch uint64, instructionID string) (instructionStatus, error) {
	url := fmt.Sprintf("%s/action/status/%d/%s", strings.TrimRight(proxyURL, "/"), epoch, instructionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status endpoint returned %d", resp.StatusCode)
	}
	var out instructionStatus
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding status response: %w", err)
	}
	return out, nil
}

func submit(ctx context.Context, rpcURL string, registry common.Address, res *proxyResult) (common.Hash, error) {
	key, err := loadKey()
	if err != nil {
		return common.Hash{}, err
	}

	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return common.Hash{}, fmt.Errorf("dialing rpc: %w", err)
	}
	defer client.Close()

	chainID, err := client.ChainID(ctx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("fetching chain id: %w", err)
	}

	parsed, err := abi.JSON(strings.NewReader(submitAttestationABI))
	if err != nil {
		return common.Hash{}, err
	}
	payload, err := parsed.Pack("submitAttestation",
		[]byte(res.Result.Data), res.Result.ID, res.Result.Status, []byte(res.Signature))
	if err != nil {
		return common.Hash{}, fmt.Errorf("packing call: %w", err)
	}

	auth, err := bind.NewKeyedTransactorWithChainID(key, chainID)
	if err != nil {
		return common.Hash{}, err
	}
	auth.Context = ctx

	contract := bind.NewBoundContract(registry, parsed, client, client, client)
	tx, err := contract.RawTransact(auth, payload)
	if err != nil {
		return common.Hash{}, err
	}
	return tx.Hash(), nil
}

// loadKey reads the relayer key from the environment. The relayer is untrusted — this key only pays
// gas and has no authority over attestation contents.
func loadKey() (*ecdsa.PrivateKey, error) {
	raw := os.Getenv("RELAYER_PRIVATE_KEY")
	if raw == "" {
		raw = os.Getenv("DEPLOYER_PRIVATE_KEY")
	}
	if raw == "" {
		return nil, fmt.Errorf("set RELAYER_PRIVATE_KEY (or DEPLOYER_PRIVATE_KEY)")
	}
	return crypto.HexToECDSA(strings.TrimPrefix(raw, "0x"))
}
