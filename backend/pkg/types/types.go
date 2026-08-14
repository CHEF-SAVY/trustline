// Package types contains the wire types for the TrustLine extension: what the Solidity contract
// sends in, and what the TEE signs on the way out.
//
// The encodings here are load-bearing. ActionResult.Data is hashed and signed by tee-node, and
// UnderwritingVerifier.sol abi.decodes it on-chain. If a field type or order drifts from
// docs/CONTRACTS.md §4, on-chain verification fails — so both layouts are pinned here and asserted
// in tests.
package types

import (
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// ScoreRequest is the ABI-decoded instruction payload sent by TrustLineInstructionSender.
// Mirrors `struct ScoreRequest { address borrower; string xrplAddress; }`.
type ScoreRequest struct {
	Borrower    common.Address `json:"borrower"`
	XrplAddress string         `json:"xrplAddress"`
}

// CreditAttestation is what the TEE signs and the chain verifies.
// Mirrors `struct CreditAttestation` in UnderwritingVerifier.sol — six fixed words.
//
// It carries NO feature values: no age, no volume, no counterparties, no transaction data. The XRPL
// address appears only as a keccak256 commitment. See docs/CONTRACTS.md §7.
type CreditAttestation struct {
	Borrower        common.Address `json:"borrower"`
	XrplAddressHash common.Hash    `json:"xrplAddressHash"`
	RiskTier        uint8          `json:"riskTier"`
	MaxLTVBips      uint16         `json:"maxLtvBips"`
	IssuedAt        uint64         `json:"issuedAt"`
	Expiry          uint64         `json:"expiry"`
}

// ScoreRequestArg describes the ABI layout of ScoreRequest from the Solidity contract.
var ScoreRequestArg abi.Argument

// CreditAttestationArgs describes the ABI layout of CreditAttestation.
//
// Encoded as a flat 6-word argument list rather than a tuple, which is what
// `abi.decode(data, (CreditAttestation))` expects for a struct with only static fields.
var CreditAttestationArgs abi.Arguments

func init() {
	requestTuple, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "borrower", Type: "address"},
		{Name: "xrplAddress", Type: "string"},
	})
	if err != nil {
		panic(err)
	}
	ScoreRequestArg = abi.Argument{Type: requestTuple}

	addressTy, _ := abi.NewType("address", "", nil)
	bytes32Ty, _ := abi.NewType("bytes32", "", nil)
	uint8Ty, _ := abi.NewType("uint8", "", nil)
	uint16Ty, _ := abi.NewType("uint16", "", nil)
	uint64Ty, _ := abi.NewType("uint64", "", nil)

	CreditAttestationArgs = abi.Arguments{
		{Name: "borrower", Type: addressTy},
		{Name: "xrplAddressHash", Type: bytes32Ty},
		{Name: "riskTier", Type: uint8Ty},
		{Name: "maxLTVBips", Type: uint16Ty},
		{Name: "issuedAt", Type: uint64Ty},
		{Name: "expiry", Type: uint64Ty},
	}
}

// Encode ABI-encodes the attestation for ActionResult.Data.
func (a CreditAttestation) Encode() ([]byte, error) {
	return CreditAttestationArgs.Pack(
		a.Borrower,
		a.XrplAddressHash,
		a.RiskTier,
		a.MaxLTVBips,
		a.IssuedAt,
		a.Expiry,
	)
}

// DecodeCreditAttestation reverses Encode. Used by the local harness and the /decode tooling; the
// chain does the equivalent with abi.decode.
func DecodeCreditAttestation(data []byte) (CreditAttestation, error) {
	values, err := CreditAttestationArgs.Unpack(data)
	if err != nil {
		return CreditAttestation{}, err
	}
	xrplHash := values[1].([32]byte)
	return CreditAttestation{
		Borrower:        values[0].(common.Address),
		XrplAddressHash: common.BytesToHash(xrplHash[:]),
		RiskTier:        values[2].(uint8),
		MaxLTVBips:      values[3].(uint16),
		IssuedAt:        values[4].(uint64),
		Expiry:          values[5].(uint64),
	}, nil
}

// State holds the extension's observable state, returned by GET /state.
//
// PRIVACY: aggregate counters only. Never expose per-borrower results, XRPL addresses, or feature
// values here — /state is served through the public proxy.
type State struct {
	AttestationsIssued int    `json:"attestationsIssued"`
	LastIssuedAt       uint64 `json:"lastIssuedAt"`
	// Tier distribution is safe to expose: it aggregates across borrowers and reveals nothing
	// about any individual.
	TierCounts [4]int `json:"tierCounts"`
}

// --- DO NOT MODIFY below this line. ---

// StateResponse is the envelope returned by GET /state.
type StateResponse struct {
	StateVersion common.Hash `json:"stateVersion"`
	State        State       `json:"state"`
}
