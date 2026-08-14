// Package config contains configuration values and defaults used by the extension.
package config

import (
	"os"
	"strconv"
	"time"
)

const (
	Version = "0.1.0"

	// OPTypeTrustLine and OPCommandScoreBorrower MUST be byte-identical to the Solidity constants
	// in TrustLineInstructionSender.sol (OP_TYPE_TRUSTLINE / OP_COMMAND_SCORE_BORROWER) and to the
	// routing switch in internal/extension. A mismatch surfaces as "unsupported op type" at runtime
	// rather than at build time, so treat these as a three-way contract. The F_ prefix is reserved
	// by Flare and must not be used.
	OPTypeTrustLine       = "TRUSTLINE"
	OPCommandScoreBorrower = "SCORE_BORROWER"

	TimeoutShutdown = 5 * time.Second
)

// Defaults.
var (
	ExtensionPort = 8080
	SignPort      = 9090

	// XRPLAPIURL is the XRPL JSON-RPC endpoint queried from inside the TEE.
	// Defaults to the public testnet cluster; override for mainnet history.
	XRPLAPIURL = "https://s.altnet.rippletest.net:51234/"

	// AttestationTTL is how long an issued attestation stays valid. Short enough that a borrower's
	// credit is re-checked regularly, long enough to be usable. Drives CreditAttestation.expiry.
	AttestationTTL = 24 * time.Hour

	// XRPLRequestTimeout bounds the outbound call so a slow XRPL node cannot stall the TEE.
	XRPLRequestTimeout = 15 * time.Second
)

// Environment variables override defaults.
func init() {
	if v := os.Getenv("EXTENSION_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			ExtensionPort = n
		}
	}
	if v := os.Getenv("SIGN_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			SignPort = n
		}
	}
	if v := os.Getenv("XRPL_API_URL"); v != "" {
		XRPLAPIURL = v
	}
	if v := os.Getenv("ATTESTATION_TTL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			AttestationTTL = time.Duration(n) * time.Second
		}
	}
}
