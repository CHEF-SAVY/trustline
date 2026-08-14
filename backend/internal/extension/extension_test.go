package extension

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"trustline-fce/internal/config"
	"trustline-fce/internal/scoring"
	"trustline-fce/pkg/types"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	teetypes "github.com/flare-foundation/tee-node/pkg/types"
	teeutils "github.com/flare-foundation/tee-node/pkg/utils"
)

// fakeSource returns fixed features with no network access, so the whole handler runs deterministically.
type fakeSource struct {
	features scoring.Features
	err      error
	gotAddr  string
}

func (f *fakeSource) FetchFeatures(_ context.Context, address string) (scoring.Features, error) {
	f.gotAddr = address
	return f.features, f.err
}

const testXRPLAddress = "rPEPPER7kfTD9w2To4CQk6UCfuHM9c6GDY"

var testBorrower = common.HexToAddress("0x1111111111111111111111111111111111111111")

func fixedNow() time.Time { return time.Unix(1_760_000_000, 0) }

func newTestExtension(src FeatureSource) *Extension {
	return NewWithSource(0, 0, src, fixedNow, 24*time.Hour)
}

// buildAction assembles the instruction envelope the TEE node delivers to POST /action.
// Data.Message is the JSON-encoded DataFixed payload, which is what processorutils.Parse expects.
func buildAction(t *testing.T, opType, opCommand string, message []byte) teetypes.Action {
	t.Helper()
	type dataFixed struct {
		InstructionID      common.Hash    `json:"instructionId"`
		TeeID              common.Address `json:"teeId"`
		Timestamp          uint64         `json:"timestamp"`
		RewardEpochID      uint32         `json:"rewardEpochId"`
		OPType             common.Hash    `json:"opType"`
		OPCommand          common.Hash    `json:"opCommand"`
		Cosigners          []string       `json:"cosigners"`
		CosignersThreshold uint64         `json:"cosignersThreshold"`
		OriginalMessage    hexutil.Bytes  `json:"originalMessage"`
	}
	msg, err := json.Marshal(dataFixed{
		OPType:          teeutils.ToHash(opType),
		OPCommand:       teeutils.ToHash(opCommand),
		OriginalMessage: message,
	})
	if err != nil {
		t.Fatalf("marshalling DataFixed: %v", err)
	}
	return teetypes.Action{
		Data: teetypes.ActionData{
			ID:            common.HexToHash("0xabc1"),
			SubmissionTag: teetypes.Threshold,
			Message:       msg,
		},
	}
}

func encodeScoreRequest(t *testing.T, borrower common.Address, xrplAddr string) []byte {
	t.Helper()
	packed, err := abi.Arguments{types.ScoreRequestArg}.Pack(struct {
		Borrower    common.Address
		XrplAddress string
	}{borrower, xrplAddr})
	if err != nil {
		t.Fatalf("packing ScoreRequest: %v", err)
	}
	return packed
}

func TestProcessScoreBorrower_ProducesDecodableAttestation(t *testing.T) {
	src := &fakeSource{features: scoring.Features{
		AccountAgeDays:         scoring.AgeSaturationDays,
		DistinctCounterparties: int(scoring.CounterpartySaturation),
		TransactionCount:       int(scoring.ActivitySaturationCount),
		PaymentVolumeXRP:       scoring.VolumeSaturationXRP,
	}}
	e := newTestExtension(src)

	action := buildAction(t, config.OPTypeTrustLine, config.OPCommandScoreBorrower,
		encodeScoreRequest(t, testBorrower, testXRPLAddress))

	status, body := e.processAction(action)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}

	var ar teetypes.ActionResult
	if err := json.Unmarshal(body, &ar); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if ar.Status != 1 {
		t.Fatalf("ActionResult.Status = %d (log %q), want 1", ar.Status, ar.Log)
	}

	att, err := types.DecodeCreditAttestation(ar.Data)
	if err != nil {
		t.Fatalf("decoding attestation: %v", err)
	}
	if att.Borrower != testBorrower {
		t.Errorf("borrower = %s, want %s", att.Borrower, testBorrower)
	}
	if att.RiskTier != 3 || att.MaxLTVBips != scoring.Tier3LTVBips {
		t.Errorf("tier/ltv = %d/%d, want 3/%d", att.RiskTier, att.MaxLTVBips, scoring.Tier3LTVBips)
	}
	if want := crypto.Keccak256Hash([]byte(testXRPLAddress)); att.XrplAddressHash != want {
		t.Errorf("xrplAddressHash = %s, want %s", att.XrplAddressHash, want)
	}
	if att.IssuedAt != uint64(fixedNow().Unix()) {
		t.Errorf("issuedAt = %d, want %d", att.IssuedAt, fixedNow().Unix())
	}
	if att.Expiry != att.IssuedAt+86400 {
		t.Errorf("expiry = %d, want issuedAt+86400", att.Expiry)
	}
	if src.gotAddr != testXRPLAddress {
		t.Errorf("fetched %q, want %q", src.gotAddr, testXRPLAddress)
	}
}

// THE privacy test. The attestation is exactly six ABI words and nothing else; if someone adds a
// feature field to the payload this fails immediately.
func TestAttestationPayloadLeaksNoFeatures(t *testing.T) {
	src := &fakeSource{features: scoring.Features{
		AccountAgeDays:         1234.5,
		DistinctCounterparties: 42,
		TransactionCount:       999,
		PaymentVolumeXRP:       777_777,
	}}
	e := newTestExtension(src)

	action := buildAction(t, config.OPTypeTrustLine, config.OPCommandScoreBorrower,
		encodeScoreRequest(t, testBorrower, testXRPLAddress))
	_, body := e.processAction(action)

	var ar teetypes.ActionResult
	if err := json.Unmarshal(body, &ar); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(ar.Data) != 6*32 {
		t.Fatalf("attestation is %d bytes, want exactly 192 (6 words) — a field may have been added", len(ar.Data))
	}

	// No feature value may appear anywhere in the signed payload.
	for _, forbidden := range []uint64{1234, 42, 999, 777_777} {
		if containsWordValue(ar.Data, forbidden) {
			t.Errorf("feature value %d leaked into the attestation payload", forbidden)
		}
	}

	// The raw XRPL address must never appear — only its hash.
	if strings.Contains(string(ar.Data), testXRPLAddress) {
		t.Error("raw XRPL address leaked into the attestation payload")
	}
}

// containsWordValue reports whether any 32-byte word of data equals v.
func containsWordValue(data []byte, v uint64) bool {
	for i := 0; i+32 <= len(data); i += 32 {
		word := data[i : i+32]
		var got uint64
		for _, b := range word[24:] {
			got = got<<8 | uint64(b)
		}
		allZeroPrefix := true
		for _, b := range word[:24] {
			if b != 0 {
				allZeroPrefix = false
				break
			}
		}
		if allZeroPrefix && got == v {
			return true
		}
	}
	return false
}

// /state is served through the public proxy, so it must expose aggregates only.
func TestStateExposesOnlyAggregates(t *testing.T) {
	e := newTestExtension(&fakeSource{features: scoring.Features{AccountAgeDays: 400, DistinctCounterparties: 30, TransactionCount: 250, PaymentVolumeXRP: 20000}})
	action := buildAction(t, config.OPTypeTrustLine, config.OPCommandScoreBorrower,
		encodeScoreRequest(t, testBorrower, testXRPLAddress))
	e.processAction(action)

	rec := httptest.NewRecorder()
	e.stateHandler(rec, httptest.NewRequest(http.MethodGet, "/state", nil))

	raw := rec.Body.String()
	if strings.Contains(raw, testXRPLAddress) {
		t.Error("/state leaked the XRPL address")
	}
	if strings.Contains(strings.ToLower(raw), strings.ToLower(testBorrower.Hex())) {
		t.Error("/state leaked the borrower address")
	}

	var resp types.StateResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if resp.State.AttestationsIssued != 1 {
		t.Errorf("attestationsIssued = %d, want 1", resp.State.AttestationsIssued)
	}
	if resp.State.TierCounts[3] != 1 {
		t.Errorf("tierCounts[3] = %d, want 1", resp.State.TierCounts[3])
	}
}

func TestUnsupportedOpTypeAndCommand(t *testing.T) {
	e := newTestExtension(&fakeSource{})
	msg := encodeScoreRequest(t, testBorrower, testXRPLAddress)

	if status, _ := e.processAction(buildAction(t, "WRONG_TYPE", config.OPCommandScoreBorrower, msg)); status != http.StatusNotImplemented {
		t.Errorf("wrong op type: status = %d, want 501", status)
	}
	if status, _ := e.processAction(buildAction(t, config.OPTypeTrustLine, "WRONG_COMMAND", msg)); status != http.StatusNotImplemented {
		t.Errorf("wrong op command: status = %d, want 501", status)
	}
}

func TestRejectsEmptyInputs(t *testing.T) {
	e := newTestExtension(&fakeSource{})

	cases := []struct {
		name     string
		borrower common.Address
		xrplAddr string
	}{
		{"zero borrower", common.Address{}, testXRPLAddress},
		{"empty xrpl address", testBorrower, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			action := buildAction(t, config.OPTypeTrustLine, config.OPCommandScoreBorrower,
				encodeScoreRequest(t, c.borrower, c.xrplAddr))
			_, body := e.processAction(action)

			var ar teetypes.ActionResult
			if err := json.Unmarshal(body, &ar); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if ar.Status != 0 {
				t.Errorf("status = %d, want 0 (error)", ar.Status)
			}
		})
	}
}

// An account with no history is a legitimate tier-0 result, not a failure.
func TestEmptyAccountYieldsTierZeroSuccess(t *testing.T) {
	e := newTestExtension(&fakeSource{features: scoring.Features{}})
	action := buildAction(t, config.OPTypeTrustLine, config.OPCommandScoreBorrower,
		encodeScoreRequest(t, testBorrower, testXRPLAddress))

	_, body := e.processAction(action)
	var ar teetypes.ActionResult
	if err := json.Unmarshal(body, &ar); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ar.Status != 1 {
		t.Fatalf("status = %d, want 1 — an empty account is a valid outcome", ar.Status)
	}
	att, err := types.DecodeCreditAttestation(ar.Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if att.RiskTier != 0 || att.MaxLTVBips != 0 {
		t.Errorf("tier/ltv = %d/%d, want 0/0", att.RiskTier, att.MaxLTVBips)
	}
}

// The OPType/OPCommand strings are a three-way contract with Solidity. Pin them so a rename here
// cannot silently diverge from TrustLineInstructionSender.sol.
func TestOpConstantsMatchSolidity(t *testing.T) {
	if config.OPTypeTrustLine != "TRUSTLINE" {
		t.Errorf("OPType = %q, must match bytes32(\"TRUSTLINE\") in Solidity", config.OPTypeTrustLine)
	}
	if config.OPCommandScoreBorrower != "SCORE_BORROWER" {
		t.Errorf("OPCommand = %q, must match bytes32(\"SCORE_BORROWER\") in Solidity", config.OPCommandScoreBorrower)
	}
	if strings.HasPrefix(config.OPTypeTrustLine, "F_") {
		t.Error("OPType must not use the reserved F_ prefix")
	}
}
