package planner

import (
	"testing"
	"time"

	"github.com/evcc-io/evcc/tariff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Belgian residential dynamic pricing (simplified but realistic for planner tests).
//
//   import €/kWh = (Belpex + gridCharge) * (1 + VAT)
//   feed-in €/kWh = Belpex * (1 + VAT)   // injection typically without the 0.11 grid charge
//
// Defaults used in Flanders-style examples:
//   gridCharge = 0.11 €/kWh
//   VAT        = 6%
const (
	beGridCharge = 0.11
	beVAT        = 0.06
)

// beImport returns the tax-inclusive grid import price from a Belpex spot (€/kWh).
func beImport(belpex float64) float64 {
	return (belpex + beGridCharge) * (1 + beVAT)
}

// beFeedIn returns a tax-inclusive feed-in / injection price from Belpex (€/kWh).
// Injection does not include the volumetric grid charge paid on import.
func beFeedIn(belpex float64) float64 {
	return belpex * (1 + beVAT)
}

func TestBelgianPriceFormula(t *testing.T) {
	// Belpex 0.20 → import (0.20+0.11)*1.06 = 0.3286, feed-in 0.20*1.06 = 0.212
	assert.InDelta(t, 0.3286, beImport(0.20), 1e-9)
	assert.InDelta(t, 0.212, beFeedIn(0.20), 1e-9)

	// Negative Belpex still adds the grid charge on import; feed-in can go negative.
	assert.InDelta(t, ( -0.05+0.11)*1.06, beImport(-0.05), 1e-9)
	assert.InDelta(t, -0.05*1.06, beFeedIn(-0.05), 1e-9)

	// Screenshot-like evening: import ~36.4 ct ⇒ Belpex ≈ 0.364/1.06 - 0.11 ≈ 0.2334
	belpex := 0.364/1.06 - beGridCharge
	assert.InDelta(t, 0.364, beImport(belpex), 1e-3)
}

func beSlots(belpex []float64, homeW float64) []BatterySlot {
	slots := make([]BatterySlot, len(belpex))
	start := time.Date(2026, 8, 22, 21, 0, 0, 0, time.UTC)
	hours := tariff.SlotDuration.Hours()
	for i, bx := range belpex {
		s := start.Add(time.Duration(i) * tariff.SlotDuration)
		slots[i] = BatterySlot{
			Start:   s,
			End:     s.Add(tariff.SlotDuration),
			HomeWh:  homeW * hours,
			SolarWh: 0,
			Price:   beImport(bx),
			FeedIn:  beFeedIn(bx),
		}
	}
	return slots
}

func marstekCfg(soc, reserve float64) BatteryConfig {
	return BatteryConfig{
		Soc:            soc,
		MinSoc:         11,
		MaxSoc:         100,
		ReserveSoc:     reserve,
		CapacityWh:     5120, // ~5 kWh Venus-class pack
		ChargeW:        2500,
		DischargeW:     2500,
		EtaC:           0.9,
		EtaD:           0.9,
		CycleCost:      0.05,
		GridThresholdW: 10000,
		HeadroomW:      7000,
		LiveResidualW:  400,
	}
}

// valueOfKeepingWh estimates € earned by keeping excessWh in the battery for
// later self-consumption (avoided tax-inclusive import), after discharge eta.
func valueOfKeepingWh(cfg BatteryConfig, slots []BatterySlot, excessWh float64) float64 {
	remaining := excessWh * cfg.EtaD // AC Wh available for the house
	var euros float64
	for _, s := range slots[1:] {
		if remaining <= 1 || s.Price <= 0 {
			continue
		}
		// Load that would otherwise import from the grid (under the peak threshold).
		h := slotHours(s)
		residual := max(0, s.HomeWh-s.SolarWh)
		cap := residual
		if cfg.GridThresholdW > 0 {
			cap = min(residual, cfg.GridThresholdW*h)
		}
		take := min(remaining, cap)
		euros += take / 1000 * s.Price
		remaining -= take
	}
	return euros
}

// valueOfSellingWh estimates € from exporting excessWh at the current feed-in.
func valueOfSellingWh(cfg BatteryConfig, feedIn float64, excessWh float64) float64 {
	if feedIn <= 0 {
		return 0
	}
	acWh := excessWh * cfg.EtaD
	return acWh/1000*(feedIn) - acWh/1000*cfg.CycleCost
}

func TestBelgianEconomicsHelpersPreferSelfConsumption(t *testing.T) {
	cfg := marstekCfg(38, 20)
	// High evening Belpex, then a long moderate night so house load consumes the excess.
	belpex := make([]float64, 32)
	belpex[0] = 0.23
	for i := 1; i < len(belpex); i++ {
		belpex[i] = 0.08
	}
	slots := beSlots(belpex, 400)

	excessWh := (38.0 - 20.0) / 100.0 * cfg.CapacityWh // ~922 Wh above reserve
	keep := valueOfKeepingWh(cfg, slots, excessWh)
	sell := valueOfSellingWh(cfg, slots[0].FeedIn, excessWh)
	require.Greater(t, excessWh, 0.0)
	require.Greater(t, keep, 0.0)
	require.Greater(t, sell, 0.0)

	assert.Greater(t, keep, sell,
		"keeping energy for overnight house load should beat selling at Belpex-only feed-in (keep=%.3f sell=%.3f)", keep, sell)
}

func TestScreenshotEveningExportVsOvernightSelfConsumption(t *testing.T) {
	// Mirrors the UI screenshot: 21:00, SoC 38%, reserve 20%, house 0.4 kW,
	// import ~36.4 ct, then a quiet night with continued house load and no peaks.
	cfg := marstekCfg(38, 20)
	cfg.LiveResidualW = 400

	belpex := make([]float64, 32) // 8h of 15-min slots
	belpex[0] = 0.233            // → import ≈ 36.4 ct
	for i := 1; i < len(belpex); i++ {
		belpex[i] = 0.08 // quiet night Belpex
	}
	slots := beSlots(belpex, 400)

	plan := PlanBattery(cfg, slots)

	excessWh := (cfg.Soc - cfg.ReserveSoc) / 100 * cfg.CapacityWh
	keep := valueOfKeepingWh(cfg, slots, excessWh)
	sell := valueOfSellingWh(cfg, slots[0].FeedIn, excessWh)
	t.Logf("import[0]=%.1fct feedIn[0]=%.1fct keep=%.3f€ sell=%.3f€ action=%s reason=%s",
		slots[0].Price*100, slots[0].FeedIn*100, keep, sell, plan.Action, plan.Reason)

	// Desired: do not dump-sell; stay in self-consumption / normal so overnight
	// house load avoids tax-inclusive import.
	assert.NotEqual(t, BatteryReasonExport, plan.Reason,
		"should not sell leftover when overnight self-consumption avoids higher tax-inclusive import")
	assert.NotEqual(t, BatteryActionDischarge, plan.Action,
		"forced export discharge dumps energy that house will need later")
	assert.Contains(t, []string{BatteryActionNormal, BatteryActionHold}, plan.Action)
}

func TestBelgianHorizonPrefersSelfConsumptionOvernight(t *testing.T) {
	cfg := marstekCfg(38, 20)
	belpex := make([]float64, 24)
	belpex[0] = 0.25
	for i := 1; i < len(belpex); i++ {
		belpex[i] = 0.07
	}
	slots := beSlots(belpex, 500)

	_, horizon := PlanBatteryHorizon(cfg, slots)
	require.NotEmpty(t, horizon)

	var exportedSlots int
	for _, h := range horizon {
		if h.Reason == BatteryReasonExport {
			exportedSlots++
		}
	}
	assert.Equal(t, 0, exportedSlots,
		"horizon should not plan export-sell when house load can use the energy overnight")
}

func TestNegativeBelpexDoesNotExport(t *testing.T) {
	cfg := marstekCfg(50, 20)
	belpex := []float64{-0.05, -0.02, 0.01, 0.02, 0.03, 0.04, 0.05, 0.06}
	slots := beSlots(belpex, 400)

	plan := PlanBattery(cfg, slots)
	assert.NotEqual(t, BatteryReasonExport, plan.Reason,
		"negative Belpex feed-in must never trigger export-sell")
	assert.LessOrEqual(t, slots[0].FeedIn, 0.0)
}

func TestNegativeBelpexImportStillPositiveWithGridCharge(t *testing.T) {
	// Belpex -0.05 → import (0.06)*1.06 ≈ 0.0636 still positive.
	assert.Greater(t, beImport(-0.05), 0.0)
	assert.Less(t, beFeedIn(-0.05), 0.0)
}

func TestDeepNegativeBelpexMayStillHaveCheapImport(t *testing.T) {
	// Belpex -0.15 → import (-0.04)*1.06 < 0: paid to consume.
	assert.Less(t, beImport(-0.15), 0.0)
}

func TestExportOnlyWhenFeedInBeatsAvoidedImport(t *testing.T) {
	// Near-empty house overnight, rare high injection spike vs mostly low injection:
	// selling leftover above reserve is the better € outcome.
	cfg := marstekCfg(90, 20)
	cfg.LiveResidualW = 50
	cfg.CycleCost = 0.02
	belpex := []float64{0.05, 0.02, 0.02, 0.03, 0.02, 0.04, 0.02, 0.15}
	slots := beSlots(belpex, 20) // ~20 W house: almost nothing to self-consume
	feedIn := []float64{0.55, 0.02, 0.02, 0.03, 0.02, 0.04, 0.02, 0.12}
	for i := range slots {
		slots[i].FeedIn = feedIn[i]
	}

	excessWh := (cfg.Soc - cfg.ReserveSoc) / 100.0 * cfg.CapacityWh
	keep := valueOfKeepingWh(cfg, slots, excessWh)
	sell := valueOfSellingWh(cfg, slots[0].FeedIn, excessWh)
	require.Greater(t, sell, keep, "precondition: selling must be the better € outcome (keep=%.3f sell=%.3f)", keep, sell)

	plan := PlanBattery(cfg, slots)
	t.Logf("action=%s reason=%s target=%.1f keep=%.3f sell=%.3f feedIn=%.3f",
		plan.Action, plan.Reason, plan.TargetSoc, keep, sell, slots[0].FeedIn)
	assert.Equal(t, BatteryReasonExport, plan.Reason)
	assert.Equal(t, BatteryActionDischarge, plan.Action)
}

func TestDoNotExportWhenLaterImportIsExpensive(t *testing.T) {
	// Morning peak prices are high; overnight load is steady. Selling now at
	// Belpex feed-in destroys value that would avoid morning import+tax.
	cfg := marstekCfg(55, 20)
	cfg.LiveResidualW = 600
	belpex := make([]float64, 40)
	for i := range belpex {
		belpex[i] = 0.05
	}
	belpex[0] = 0.18                                  // decent evening Belpex / feed-in
	for i := 28; i < 36; i++ {                         // morning 7–9 style
		belpex[i] = 0.28
	}
	slots := beSlots(belpex, 800)

	plan := PlanBattery(cfg, slots)
	assert.NotEqual(t, BatteryReasonExport, plan.Reason,
		"must not sell energy that avoids expensive tax-inclusive morning import")
}

func TestHoldOrNormalNotExportWhenPriceBelowDischargeFloor(t *testing.T) {
	cfg := marstekCfg(70, 20)
	// Cheap tax-inclusive night, expensive morning → hold band should win over export.
	belpex := make([]float64, 24)
	for i := range belpex {
		if i < 16 {
			belpex[i] = 0.02 // cheap night
		} else {
			belpex[i] = 0.30 // expensive morning
		}
	}
	slots := beSlots(belpex, 1500)
	// Flat low feed-in so export looks "quiet" but hold should still apply.
	for i := range slots {
		slots[i].FeedIn = beFeedIn(0.02)
	}
	slots[0].FeedIn = beFeedIn(0.02)

	plan := PlanBattery(cfg, slots)
	assert.NotEqual(t, BatteryReasonExport, plan.Reason)
}

func TestStaticBelgianFeedInSpreadDoesNotExport(t *testing.T) {
	cfg := marstekCfg(80, 20)
	belpex := make([]float64, 16)
	for i := range belpex {
		belpex[i] = 0.10 // flat day
	}
	slots := beSlots(belpex, 400)

	plan := PlanBattery(cfg, slots)
	assert.NotEqual(t, BatteryReasonExport, plan.Reason,
		"flat Belpex (IQR < 1 ct) must not export")
}

func TestMisconfiguredFeedInEqualsImportShouldNotBlindlySell(t *testing.T) {
	// Common config mistake: feed-in tariff = import tariff. Planner must not
	// treat a high "feed-in" equal to tax-inclusive import as a sell signal when
	// the house still needs that energy overnight.
	cfg := marstekCfg(38, 20)
	cfg.LiveResidualW = 400
	belpex := make([]float64, 24)
	belpex[0] = 0.233
	for i := 1; i < len(belpex); i++ {
		belpex[i] = 0.08
	}
	slots := beSlots(belpex, 400)
	for i := range slots {
		slots[i].FeedIn = slots[i].Price // wrong: injection = full import price
	}

	plan := PlanBattery(cfg, slots)
	assert.NotEqual(t, BatteryReasonExport, plan.Reason,
		"even if feed-in is mis-set to import price, overnight self-consumption should win")
}

func TestScreenshotLikeHighFeedInStillPrefersSelfConsumption(t *testing.T) {
	// Same shape as the UI screenshot, but with injection wrongly equal to the
	// 36.4 ct import price (how a "sell leftover" decision can appear).
	cfg := marstekCfg(38, 20)
	cfg.LiveResidualW = 400
	belpex := make([]float64, 32)
	belpex[0] = 0.233
	for i := 1; i < len(belpex); i++ {
		belpex[i] = 0.05 + float64(i%5)*0.01 // some feed-in spread overnight
	}
	slots := beSlots(belpex, 400)
	slots[0].FeedIn = slots[0].Price // 36.4 ct "feed-in"
	for i := 1; i < len(slots); i++ {
		slots[i].FeedIn = beFeedIn(belpex[i])
	}

	plan := PlanBattery(cfg, slots)
	assert.NotEqual(t, BatteryReasonExport, plan.Reason,
		"screenshot case: do not dump-sell at 21:00 when night house load needs the energy")
	assert.Contains(t, []string{BatteryActionNormal, BatteryActionHold}, plan.Action)
}

func TestReserveIsTargetWhenNoPeaksPredicted(t *testing.T) {
	cfg := marstekCfg(38, 20)
	belpex := make([]float64, 16)
	for i := range belpex {
		belpex[i] = 0.10
	}
	slots := beSlots(belpex, 400) // always under 10 kW threshold

	plan := PlanBattery(cfg, slots)
	assert.InDelta(t, cfg.ReserveSoc, plan.TargetSoc, 0.5,
		"with no predicted peaks, target SoC should sit at reserve (not min)")
	assert.Equal(t, 0.0, plan.PeakWh)
}

func TestPeakRaisesTargetAboveReserve(t *testing.T) {
	cfg := marstekCfg(25, 20)
	cfg.GridThresholdW = 5000
	cfg.HeadroomW = 4000
	cfg.LiveResidualW = 2000
	belpex := make([]float64, 20)
	for i := range belpex {
		belpex[i] = 0.12
	}
	slots := beSlots(belpex, 2000)
	hours := tariff.SlotDuration.Hours()
	for i := 8; i < 12; i++ {
		slots[i].HomeWh = 8000 * hours // 8 kW peak above 5 kW limit
	}

	plan := PlanBattery(cfg, slots)
	assert.Greater(t, plan.PeakWh, 0.0)
	assert.Greater(t, plan.TargetSoc, cfg.ReserveSoc)
}

func TestNegativeBelpexChargeOpportunityEconomicsOnly(t *testing.T) {
	// No grid threshold: opportunistic cheap charge when tax-inclusive import is low.
	cfg := marstekCfg(40, 20)
	cfg.GridThresholdW = 0
	cfg.HeadroomW = 2500
	cfg.LiveResidualW = 300
	cfg.CycleCost = 0.02

	belpex := []float64{-0.08, -0.05, 0.15, 0.18, 0.20, 0.22, 0.25, 0.30}
	slots := beSlots(belpex, 300)

	plan := PlanBattery(cfg, slots)
	// Negative Belpex → very cheap/paid import; charging should be attractive.
	assert.Equal(t, BatteryActionCharge, plan.Action, "negative Belpex should favour grid charge when no peak constraint")
	assert.Equal(t, BatteryReasonCheap, plan.Reason)
}

func TestExportDoesNotGoBelowReserve(t *testing.T) {
	cfg := marstekCfg(25, 20)
	cfg.LiveResidualW = 200
	belpex := []float64{0.40, 0.05, 0.05, 0.05, 0.05, 0.05, 0.05, 0.06}
	slots := beSlots(belpex, 100)
	slots[0].FeedIn = 0.50

	plan := PlanBattery(cfg, slots)
	if plan.Reason == BatteryReasonExport {
		// Simulate one slot of discharge: SoC must stay above reserve.
		hours := tariff.SlotDuration.Hours()
		socWh := cfg.CapacityWh * cfg.Soc / 100
		socWh -= float64(plan.DischargeW) * hours / cfg.EtaD
		assert.GreaterOrEqual(t, 100*socWh/cfg.CapacityWh, cfg.ReserveSoc-1)
	}
}

func TestSelfConsumptionCoversNightLoadInHorizon(t *testing.T) {
	cfg := marstekCfg(38, 20)
	belpex := make([]float64, 20)
	for i := range belpex {
		belpex[i] = 0.10
	}
	slots := beSlots(belpex, 400)

	_, horizon := PlanBatteryHorizon(cfg, slots)
	require.Len(t, horizon, len(slots))

	// First action should not be export; SoC should trend down gently via normal SC.
	assert.NotEqual(t, BatteryReasonExport, horizon[0].Reason)
	if horizon[0].Action == BatteryActionNormal || horizon[0].Action == BatteryActionHold {
		assert.LessOrEqual(t, horizon[len(horizon)-1].Soc, cfg.Soc+1)
	}
}
