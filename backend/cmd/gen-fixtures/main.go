// Command gen-fixtures produces the golden signature fixtures the Solidity test suite asserts against.
//
// This closes the loop across both languages:
//
//	types.CreditAttestation.Encode()   (our Go encoder — the real one the extension uses)
//	  → tee-node ActionResult.Hash()   (Flare's Go code)
//	  → go-flare-common signing.Payload.Hash()  (Flare's Go code)
//	  → utils.Sign()                   (Flare's Go code, EIP-191)
//	  → contracts/test/fixtures/tee-signatures.json
//	  → UnderwritingVerifier.sol asserts it recovers the same signer and decodes the same fields.
//
// Because the payload comes from the same Encode() the extension calls at runtime, a drift between
// the Go struct and the Solidity struct fails the Foundry suite locally instead of silently failing
// verification on Coston2.
//
// Regenerate with:
//
//	go run ./cmd/gen-fixtures > ../contracts/test/fixtures/tee-signatures.json
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"trustline-fce/pkg/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	csigning "github.com/flare-foundation/go-flare-common/pkg/signing"
	teetypes "github.com/flare-foundation/tee-node/pkg/types"
	teeutils "github.com/flare-foundation/tee-node/pkg/utils"
)

type fixture struct {
	Name            string `json:"name"`
	PrivateKey      string `json:"privateKey"`
	ExpectedTeeID   string `json:"expectedTeeId"`
	ChainID         uint64 `json:"chainId"`
	Data            string `json:"data"`
	ID              string `json:"id"`
	SubmissionTag   string `json:"submissionTag"`
	Status          uint8  `json:"status"`
	InnerHash       string `json:"innerHash"`
	Digest          string `json:"digest"`
	Signature       string `json:"signature"`
	Borrower        string `json:"borrower"`
	XrplAddressHash string `json:"xrplAddressHash"`
	RiskTier        uint8  `json:"riskTier"`
	MaxLTVBips      uint16 `json:"maxLtvBips"`
	IssuedAt        uint64 `json:"issuedAt"`
	Expiry          uint64 `json:"expiry"`
}

func build(name, privHex string, chainID uint64, att types.CreditAttestation, id common.Hash, status uint8) fixture {
	priv, err := crypto.HexToECDSA(privHex)
	if err != nil {
		panic(err)
	}

	// The real encoder the extension uses at runtime.
	data, err := att.Encode()
	if err != nil {
		panic(err)
	}

	ar := &teetypes.ActionResult{
		ID:            id,
		SubmissionTag: teetypes.Threshold,
		Status:        status,
		Data:          data,
	}
	innerHash := ar.Hash()

	digest, err := csigning.NewPayload(csigning.TEEActionResult, chainID, common.BytesToHash(innerHash)).Hash()
	if err != nil {
		panic(err)
	}
	sig, err := teeutils.Sign(digest[:], priv)
	if err != nil {
		panic(err)
	}
	// go-ethereum yields v in {0,1}; Solidity's ecrecover expects {27,28}.
	if sig[64] < 27 {
		sig[64] += 27
	}

	return fixture{
		Name:            name,
		PrivateKey:      "0x" + privHex,
		ExpectedTeeID:   crypto.PubkeyToAddress(priv.PublicKey).Hex(),
		ChainID:         chainID,
		Data:            hexutil.Encode(data),
		ID:              id.Hex(),
		SubmissionTag:   string(teetypes.Threshold),
		Status:          status,
		InnerHash:       hexutil.Encode(innerHash),
		Digest:          hexutil.Encode(digest[:]),
		Signature:       hexutil.Encode(sig),
		Borrower:        att.Borrower.Hex(),
		XrplAddressHash: att.XrplAddressHash.Hex(),
		RiskTier:        att.RiskTier,
		MaxLTVBips:      att.MaxLTVBips,
		IssuedAt:        att.IssuedAt,
		Expiry:          att.Expiry,
	}
}

func main() {
	const coston2 = 114
	xrplHash := crypto.Keccak256Hash([]byte("rPEPPER7kfTD9w2To4CQk6UCfuHM9c6GDY"))

	fixtures := []fixture{
		build("tier3_coston2",
			"4c0883a69102937d6231471b5dbb6204fe512961708279a0e1b0f0d6f0f0a001", coston2,
			types.CreditAttestation{
				Borrower:        common.HexToAddress("0x1111111111111111111111111111111111111111"),
				XrplAddressHash: xrplHash,
				RiskTier:        3,
				MaxLTVBips:      7500,
				IssuedAt:        1_760_000_000,
				Expiry:          1_760_086_400,
			},
			common.HexToHash("0xabc0000000000000000000000000000000000000000000000000000000000001"), 1),

		build("tier0_coston2",
			"4c0883a69102937d6231471b5dbb6204fe512961708279a0e1b0f0d6f0f0a002", coston2,
			types.CreditAttestation{
				Borrower:        common.HexToAddress("0x2222222222222222222222222222222222222222"),
				XrplAddressHash: xrplHash,
				RiskTier:        0,
				MaxLTVBips:      0,
				IssuedAt:        1_760_000_000,
				Expiry:          1_760_086_400,
			},
			common.HexToHash("0xabc0000000000000000000000000000000000000000000000000000000000002"), 1),
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(fixtures); err != nil {
		panic(err)
	}
	fmt.Fprintln(os.Stderr, "generated", len(fixtures), "fixtures")
}
