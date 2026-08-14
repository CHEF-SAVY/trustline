// Package extension implements the TrustLine Flare Compute Extension.
//
// Flow, following the scaffold's POST /action pattern:
//  1. Receive an instruction relayed from TrustLineInstructionSender.
//  2. Decode the ScoreRequest (borrower + XRPL address).
//  3. Fetch the borrower's XRPL history and reduce it to features — inside the TEE.
//  4. Score it, build a CreditAttestation, and return it ABI-encoded in ActionResult.Data.
//
// tee-node signs the result on the way out; we never touch a signing key here.
package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"trustline-fce/internal/config"
	"trustline-fce/internal/scoring"
	"trustline-fce/internal/xrpl"
	"trustline-fce/pkg/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/flare-foundation/go-flare-common/pkg/tee/instruction"
	"github.com/flare-foundation/go-flare-common/pkg/tee/structs"
	teetypes "github.com/flare-foundation/tee-node/pkg/types"
	teeutils "github.com/flare-foundation/tee-node/pkg/utils"

	"github.com/flare-foundation/tee-node/pkg/processorutils"
)

// FeatureSource supplies scoring inputs for an XRPL address. Abstracted so the conformance harness
// can run the full handler deterministically with no network.
type FeatureSource interface {
	FetchFeatures(ctx context.Context, address string) (scoring.Features, error)
}

type Extension struct {
	mu     sync.RWMutex
	Server *http.Server

	features FeatureSource
	// now is injectable so tests get deterministic issuedAt/expiry values.
	now func() time.Time
	ttl time.Duration

	attestationsIssued int
	lastIssuedAt       uint64
	tierCounts         [4]int
}

// --- DO NOT MODIFY: New(), actionHandler() are boilerplate.
func New(extensionPort, signPort int) *Extension {
	return NewWithSource(
		extensionPort,
		signPort,
		xrpl.NewClient(config.XRPLAPIURL, config.XRPLRequestTimeout),
		time.Now,
		config.AttestationTTL,
	)
}

// NewWithSource builds an Extension with injectable dependencies, for tests and the local harness.
func NewWithSource(extensionPort, signPort int, src FeatureSource, now func() time.Time, ttl time.Duration) *Extension {
	e := &Extension{features: src, now: now, ttl: ttl}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /state", e.stateHandler)
	mux.HandleFunc("POST /action", e.actionHandler)

	e.Server = &http.Server{Addr: fmt.Sprintf(":%d", extensionPort), Handler: mux}
	return e
}

// stateHandler exposes aggregate counters only — never per-borrower data. /state is reachable
// through the public proxy.
func (e *Extension) stateHandler(w http.ResponseWriter, r *http.Request) {
	e.mu.RLock()
	stateResponse := types.StateResponse{
		StateVersion: teeutils.ToHash(config.Version),
		State: types.State{
			AttestationsIssued: e.attestationsIssued,
			LastIssuedAt:       e.lastIssuedAt,
			TierCounts:         e.tierCounts,
		},
	}
	e.mu.RUnlock()

	if err := json.NewEncoder(w).Encode(stateResponse); err != nil {
		http.Error(w, fmt.Sprintf("sending response: %v", err), http.StatusInternalServerError)
	}
}

func (e *Extension) processAction(action teetypes.Action) (int, []byte) {
	dataFixed, err := processorutils.Parse[instruction.DataFixed](action.Data.Message)
	if err != nil {
		return http.StatusBadRequest, []byte(fmt.Sprintf("decoding fixed data: %v", err))
	}

	switch {
	case dataFixed.OPType == teeutils.ToHash(config.OPTypeTrustLine):
		return e.processTrustLine(action, dataFixed)

	default:
		return http.StatusNotImplemented, []byte(fmt.Sprintf(
			"unsupported op type: received %s, expected %s (%s)",
			dataFixed.OPType.Hex(), teeutils.ToHash(config.OPTypeTrustLine).Hex(), config.OPTypeTrustLine,
		))
	}
}

// processTrustLine routes TRUSTLINE instructions by OPCommand.
func (e *Extension) processTrustLine(action teetypes.Action, df *instruction.DataFixed) (int, []byte) {
	switch {
	case df.OPCommand == teeutils.ToHash(config.OPCommandScoreBorrower):
		ar := e.processScoreBorrower(action, df)
		b, err := json.Marshal(ar)
		if err != nil {
			return http.StatusInternalServerError, []byte(fmt.Sprintf("encoding result: %v", err))
		}
		return http.StatusOK, b

	default:
		return http.StatusNotImplemented, []byte(fmt.Sprintf(
			"unsupported op command: received %s, expected %s (%s)",
			df.OPCommand.Hex(),
			teeutils.ToHash(config.OPCommandScoreBorrower).Hex(), config.OPCommandScoreBorrower,
		))
	}
}

// processScoreBorrower is the heart of TrustLine: private history in, signed verdict out.
//
// PRIVACY: `features` below holds the borrower's financial profile. It is used to compute a tier and
// then goes out of scope. It must never be logged, added to State, or written into the response.
func (e *Extension) processScoreBorrower(action teetypes.Action, df *instruction.DataFixed) teetypes.ActionResult {
	var req types.ScoreRequest
	if err := structs.DecodeTo(types.ScoreRequestArg, df.OriginalMessage, &req); err != nil {
		return buildResult(action, df, nil, 0, fmt.Errorf("decoding request: %w", err))
	}
	if req.Borrower == (common.Address{}) {
		return buildResult(action, df, nil, 0, fmt.Errorf("borrower must not be the zero address"))
	}
	if req.XrplAddress == "" {
		return buildResult(action, df, nil, 0, fmt.Errorf("xrplAddress must not be empty"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), config.XRPLRequestTimeout)
	defer cancel()

	features, err := e.features.FetchFeatures(ctx, req.XrplAddress)
	if err != nil {
		// The error is from our own code and carries no borrower data by construction, but keep the
		// message generic regardless.
		return buildResult(action, df, nil, 0, fmt.Errorf("fetching xrpl history: %w", err))
	}

	result := scoring.Score(features)

	issuedAt := uint64(e.now().Unix())
	attestation := types.CreditAttestation{
		Borrower:        req.Borrower,
		XrplAddressHash: crypto.Keccak256Hash([]byte(req.XrplAddress)),
		RiskTier:        result.RiskTier,
		MaxLTVBips:      result.MaxLTVBips,
		IssuedAt:        issuedAt,
		Expiry:          issuedAt + uint64(e.ttl.Seconds()),
	}

	encoded, err := attestation.Encode()
	if err != nil {
		return buildResult(action, df, nil, 0, fmt.Errorf("encoding attestation: %w", err))
	}

	e.mu.Lock()
	e.attestationsIssued++
	e.lastIssuedAt = issuedAt
	if int(result.RiskTier) < len(e.tierCounts) {
		e.tierCounts[result.RiskTier]++
	}
	e.mu.Unlock()

	return buildResult(action, df, encoded, 1, nil)
}
