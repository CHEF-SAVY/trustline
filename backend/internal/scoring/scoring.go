// Package scoring turns XRPL account features into a risk tier and a max LTV.
//
// Design constraints, in priority order:
//
//  1. **Transparent.** Every weight and threshold is a named constant in this file. No ML, no
//     opaque model. A judge, a borrower, or a lender can read this and predict the outcome.
//  2. **Bounded.** The score is a weighted sum of four sub-scores, each clamped to [0,1], so no
//     single feature can dominate and the total is always in [0,100].
//  3. **Monotonic.** More history, more volume and more counterparties never lower a score. This
//     matters because a non-monotonic rule would let a borrower game the model by doing less.
//  4. **Privacy-preserving.** Inputs are consumed here and discarded. Only the tier and LTV leave
//     the TEE. Nothing in this package may log a feature value — see the note in Score.
//
// KNOWN LIMITATION (stated openly rather than buried): this is Sybil-resistant only in a weak
// sense. An attacker willing to age accounts and cycle funds between them can manufacture a decent
// score. Counterparty diversity raises that cost but does not eliminate it. A production system
// would need stake, social graph, or identity signals layered on top. For an MVP the honest claim
// is "better than nothing, and fully auditable", not "unforgeable".
package scoring

import "math"

// Features are the inputs derived from a borrower's XRPL account. All are computed inside the TEE.
type Features struct {
	// AccountAgeDays is days since the account's first observed ledger.
	AccountAgeDays float64
	// TransactionCount is the number of transactions observed.
	TransactionCount int
	// PaymentVolumeXRP is total XRP moved (sent + received), in whole XRP.
	PaymentVolumeXRP float64
	// DistinctCounterparties is the number of unique addresses transacted with.
	DistinctCounterparties int
}

// Weights of each sub-score in the final 0..100 score. They sum to 1.
//
// Rationale: age and counterparty diversity are the hardest to fake cheaply, so they carry the most
// weight. Raw volume is the easiest to inflate by moving funds in a loop, so it carries the least.
const (
	WeightAge            = 0.35
	WeightCounterparties = 0.30
	WeightActivity       = 0.20
	WeightVolume         = 0.15
)

// Saturation points — the value at which a sub-score reaches 1.0. Beyond these, more does not help,
// which keeps a single whale-sized feature from buying a top tier on its own.
const (
	AgeSaturationDays       = 365.0 // one year of history is treated as fully established
	CounterpartySaturation  = 25.0  // 25 distinct counterparties reads as genuine economic activity
	ActivitySaturationCount = 200.0 // 200 transactions
	VolumeSaturationXRP     = 10_000.0
)

// Tier thresholds on the 0..100 score.
const (
	Tier1MinScore = 25.0
	Tier2MinScore = 50.0
	Tier3MinScore = 75.0
)

// Max LTV in basis points per tier.
//
// Tier 0 is 0, meaning "no underwriting benefit" — the pool falls back to its standard
// overcollateralized LTV. Tiers rise to 75%, deliberately kept below the pool's 85% liquidation
// threshold so a borrower drawing their full allowance is not instantly liquidatable.
const (
	Tier0LTVBips = 0
	Tier1LTVBips = 6000
	Tier2LTVBips = 7000
	Tier3LTVBips = 7500
)

// Result is the scoring outcome. Deliberately minimal: the feature values are NOT carried out.
type Result struct {
	RiskTier   uint8
	MaxLTVBips uint16
	// Score is retained for local tests and reasoning only. It must never be written into the
	// attestation payload or any log that leaves the TEE.
	Score float64
}

// ratio clamps value/saturation to [0,1].
func ratio(value, saturation float64) float64 {
	if saturation <= 0 || value <= 0 {
		return 0
	}
	return math.Min(value/saturation, 1.0)
}

// Score maps features to a tier and LTV.
//
// PRIVACY: this function must not log. Its inputs are the borrower's private financial history, and
// anything written to stdout inside the TEE is visible to the operator running the container.
func Score(f Features) Result {
	ageScore := ratio(f.AccountAgeDays, AgeSaturationDays)
	counterpartyScore := ratio(float64(f.DistinctCounterparties), CounterpartySaturation)
	activityScore := ratio(float64(f.TransactionCount), ActivitySaturationCount)
	volumeScore := ratio(f.PaymentVolumeXRP, VolumeSaturationXRP)

	score := 100.0 * (WeightAge*ageScore +
		WeightCounterparties*counterpartyScore +
		WeightActivity*activityScore +
		WeightVolume*volumeScore)

	tier, ltv := tierFor(score)
	return Result{RiskTier: tier, MaxLTVBips: ltv, Score: score}
}

func tierFor(score float64) (uint8, uint16) {
	switch {
	case score >= Tier3MinScore:
		return 3, Tier3LTVBips
	case score >= Tier2MinScore:
		return 2, Tier2LTVBips
	case score >= Tier1MinScore:
		return 1, Tier1LTVBips
	default:
		return 0, Tier0LTVBips
	}
}
