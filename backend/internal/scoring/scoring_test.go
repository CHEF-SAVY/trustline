package scoring

import (
	"math"
	"testing"
)

// almostEqual tolerates float64 rounding: the weighted sum of four clamped ratios lands on
// 99.99999999999999 rather than exactly 100.
func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestScore_EmptyAccountIsTierZero(t *testing.T) {
	got := Score(Features{})
	if got.RiskTier != 0 || got.MaxLTVBips != Tier0LTVBips {
		t.Fatalf("empty account: got tier %d ltv %d, want tier 0 ltv %d", got.RiskTier, got.MaxLTVBips, Tier0LTVBips)
	}
}

func TestScore_FullySaturatedIsTierThree(t *testing.T) {
	got := Score(Features{
		AccountAgeDays:         AgeSaturationDays,
		DistinctCounterparties: int(CounterpartySaturation),
		TransactionCount:       int(ActivitySaturationCount),
		PaymentVolumeXRP:       VolumeSaturationXRP,
	})
	if !almostEqual(got.Score, 100) {
		t.Fatalf("saturated score = %v, want 100", got.Score)
	}
	if got.RiskTier != 3 || got.MaxLTVBips != Tier3LTVBips {
		t.Fatalf("got tier %d ltv %d, want tier 3 ltv %d", got.RiskTier, got.MaxLTVBips, Tier3LTVBips)
	}
}

// Exceeding the saturation points must not push the score above 100 or change the tier. Guards the
// clamp that stops one huge feature from buying a top tier.
func TestScore_SaturationIsClamped(t *testing.T) {
	got := Score(Features{
		AccountAgeDays:         AgeSaturationDays * 100,
		DistinctCounterparties: int(CounterpartySaturation) * 100,
		TransactionCount:       int(ActivitySaturationCount) * 100,
		PaymentVolumeXRP:       VolumeSaturationXRP * 100,
	})
	if !almostEqual(got.Score, 100) {
		t.Fatalf("over-saturated score = %v, want clamped to 100", got.Score)
	}
}

// Volume alone carries only 15% weight, so a whale with no history, no age and one counterparty
// must not reach a lending tier. This is the core anti-gaming property.
func TestScore_VolumeAloneCannotReachTier1(t *testing.T) {
	got := Score(Features{PaymentVolumeXRP: VolumeSaturationXRP * 1000})
	if got.Score >= Tier1MinScore {
		t.Fatalf("volume-only score = %v, must stay below tier 1 threshold %v", got.Score, Tier1MinScore)
	}
	if got.RiskTier != 0 {
		t.Fatalf("volume-only tier = %d, want 0", got.RiskTier)
	}
}

// Every sub-score must be monotonic: more of a good thing never lowers the score.
func TestScore_IsMonotonic(t *testing.T) {
	base := Features{AccountAgeDays: 100, DistinctCounterparties: 5, TransactionCount: 40, PaymentVolumeXRP: 500}
	baseScore := Score(base).Score

	cases := []struct {
		name string
		mut  func(Features) Features
	}{
		{"age", func(f Features) Features { f.AccountAgeDays += 50; return f }},
		{"counterparties", func(f Features) Features { f.DistinctCounterparties += 5; return f }},
		{"transactions", func(f Features) Features { f.TransactionCount += 20; return f }},
		{"volume", func(f Features) Features { f.PaymentVolumeXRP += 250; return f }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Score(c.mut(base)).Score; got < baseScore {
				t.Fatalf("increasing %s lowered score: %v < %v", c.name, got, baseScore)
			}
		})
	}
}

// Negative or nonsensical inputs must not produce a credit line.
func TestScore_NegativeInputsAreSafe(t *testing.T) {
	got := Score(Features{AccountAgeDays: -1000, PaymentVolumeXRP: -5000, TransactionCount: -3, DistinctCounterparties: -7})
	if got.Score != 0 || got.RiskTier != 0 {
		t.Fatalf("negative inputs: got score %v tier %d, want 0/0", got.Score, got.RiskTier)
	}
}

// Weights must sum to 1, otherwise the score is not on a 0..100 scale and the tier thresholds
// silently mean something different.
func TestWeightsSumToOne(t *testing.T) {
	sum := WeightAge + WeightCounterparties + WeightActivity + WeightVolume
	if diff := sum - 1.0; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("weights sum to %v, want 1.0", sum)
	}
}

// Every attainable LTV must sit below the pool's 85% liquidation threshold, or a borrower drawing
// their full allowance would be instantly liquidatable. Cross-checks a Solidity constant.
func TestTierLTVsBelowLiquidationThreshold(t *testing.T) {
	const poolLiquidationThresholdBips = 8500
	for _, ltv := range []uint16{Tier0LTVBips, Tier1LTVBips, Tier2LTVBips, Tier3LTVBips} {
		if ltv >= poolLiquidationThresholdBips {
			t.Fatalf("tier LTV %d >= liquidation threshold %d", ltv, poolLiquidationThresholdBips)
		}
	}
}

func TestTierBoundaries(t *testing.T) {
	cases := []struct {
		score float64
		tier  uint8
	}{
		{0, 0}, {Tier1MinScore - 0.01, 0}, {Tier1MinScore, 1},
		{Tier2MinScore - 0.01, 1}, {Tier2MinScore, 2},
		{Tier3MinScore - 0.01, 2}, {Tier3MinScore, 3}, {100, 3},
	}
	for _, c := range cases {
		if got, _ := tierFor(c.score); got != c.tier {
			t.Errorf("tierFor(%v) = %d, want %d", c.score, got, c.tier)
		}
	}
}
