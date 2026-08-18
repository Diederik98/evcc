package planner

import (
	"testing"
	"time"

	"github.com/evcc-io/evcc/tariff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSlots(prices []float64, homeW, solarW float64) []BatterySlot {
	start := time.Date(2026, 8, 17, 18, 0, 0, 0, time.UTC)
	slots := make([]BatterySlot, len(prices))
	for i, p := range prices {
		s := start.Add(time.Duration(i) * tariff.SlotDuration)
		hours := tariff.SlotDuration.Hours()
		slots[i] = BatterySlot{
			Start:   s,
			End:     s.Add(tariff.SlotDuration),
			HomeWh:  homeW * hours,
			SolarWh: solarW * hours,
			Price:   p,
		}
	}
	return slots
}

func TestPlanBatteryChargeForUpcomingPeak(t *testing.T) {
	// quiet now, 8 kW evening peak above a 5 kW grid limit. Battery is nearly
	// empty: grid-charge during the quiet slots so the peak can be shaved.
	cfg := BatteryConfig{
		Soc:            25,
		MinSoc:         20,
		MaxSoc:         100,
		ReserveSoc:     40,
		CapacityWh:     10000,
		ChargeW:        2500,
		DischargeW:     5000,
		EtaC:           0.9,
		EtaD:           0.9,
		GridThresholdW: 5000,
		HeadroomW:      4000,
		LiveResidualW:  2000,
	}
	prices := make([]float64, 16)
	for i := range prices {
		prices[i] = 0.20
	}
	slots := testSlots(prices, 2000, 0)
	for i := 8; i < len(slots); i++ {
		slots[i].HomeWh = 8000 * tariff.SlotDuration.Hours()
	}

	plan := PlanBattery(cfg, slots)
	require.Equal(t, BatteryActionCharge, plan.Action, "should pre-charge for the evening peak")
	assert.Greater(t, plan.ChargeW, 0)
	assert.Greater(t, plan.TargetSoc, cfg.Soc)
	assert.Greater(t, plan.PeakWh, 0.0)
}

func TestPlanBatteryDischargeDuringPeak(t *testing.T) {
	cfg := BatteryConfig{
		Soc:            80,
		MinSoc:         20,
		MaxSoc:         100,
		ReserveSoc:     40,
		CapacityWh:     10000,
		ChargeW:        2500,
		DischargeW:     5000,
		EtaC:           0.9,
		EtaD:           0.9,
		GridThresholdW: 5000,
		HeadroomW:      4000,
		LiveResidualW:  9000, // already over the 5 kW limit
	}
	slots := testSlots([]float64{0.40, 0.40, 0.30}, 8000, 0)

	plan := PlanBattery(cfg, slots)
	require.Equal(t, BatteryActionDischarge, plan.Action)
	assert.Equal(t, BatteryReasonPeak, plan.Reason)
	assert.Greater(t, plan.DischargeW, 0)
}

func TestPlanBatteryHoldCheapNightForMorningPeak(t *testing.T) {
	// cheap night (0.12 including tax), expensive morning (0.40). Battery has
	// enough energy: hold overnight instead of emptying into cheap hours.
	cfg := BatteryConfig{
		Soc:            70,
		MinSoc:         20,
		MaxSoc:         100,
		ReserveSoc:     40,
		CapacityWh:     10000,
		ChargeW:        2500,
		DischargeW:     5000,
		EtaC:           0.9,
		EtaD:           0.9,
		CycleCost:      0.05,
		GridThresholdW: 5000,
		HeadroomW:      4000,
		LiveResidualW:  1500,
	}
	cfg.Soc = 90
	prices := make([]float64, 24)
	for i := range prices {
		if i < 16 {
			prices[i] = 0.25
		} else {
			prices[i] = 0.35
		}
	}
	// night: 2 kW house, morning slots: 8 kW peak
	slots := testSlots(prices, 2000, 0)
	for i := 16; i < len(slots); i++ {
		slots[i].HomeWh = 8000 * tariff.SlotDuration.Hours()
	}

	plan := PlanBattery(cfg, slots)
	require.Equal(t, BatteryActionHold, plan.Action, "should hold cheap energy for the morning peak")
	assert.Equal(t, BatteryReasonHold, plan.Reason)
	assert.Greater(t, plan.DischargeFloor, 0.12)
}

func TestPlanBatteryTaxesWidenSpreadNeededToCycle(t *testing.T) {
	cfg := BatteryConfig{
		Soc:            50,
		MinSoc:         10,
		MaxSoc:         90,
		ReserveSoc:     20,
		CapacityWh:     10000,
		ChargeW:        2500,
		DischargeW:     5000,
		EtaC:           0.9,
		EtaD:           0.9,
		CycleCost:      0.05,
		GridThresholdW: 0, // no physical peak: economics only
		HeadroomW:      4000,
		LiveResidualW:  500,
	}

	// wholesale 0.10 vs 0.20 is worth a cycle. The same spot prices plus 0.15
	// levies and 21% VAT make lost energy too expensive to cycle.
	wholesaleCheap := testSlots([]float64{0.10, 0.10, 0.20, 0.20}, 500, 0)
	taxed := make([]BatterySlot, len(wholesaleCheap))
	copy(taxed, wholesaleCheap)
	for i := range taxed {
		taxed[i].Price = (wholesaleCheap[i].Price + 0.15) * 1.21
	}

	wholesalePlan := PlanBattery(cfg, wholesaleCheap)
	taxedPlan := PlanBattery(cfg, taxed)

	assert.Equal(t, BatteryActionCharge, wholesalePlan.Action, "untaxed spread should still look cheap")
	assert.NotEqual(t, BatteryActionCharge, taxedPlan.Action, "tax-inclusive spread should not grid-charge")
}

func TestPlanBatteryDoesNotDischargeBelowThreshold(t *testing.T) {
	cfg := BatteryConfig{
		Soc:            60,
		MinSoc:         20,
		MaxSoc:         100,
		ReserveSoc:     40,
		CapacityWh:     10000,
		ChargeW:        2500,
		DischargeW:     8000,
		EtaC:           0.9,
		EtaD:           0.9,
		GridThresholdW: 10000,
		HeadroomW:      5000,
		LiveResidualW:  9000,
	}
	slots := testSlots([]float64{0.30, 0.30}, 9000, 0)

	plan := PlanBattery(cfg, slots)
	assert.NotEqual(t, BatteryActionDischarge, plan.Action)
}

func TestPlanBatteryEmptyConfig(t *testing.T) {
	plan := PlanBattery(BatteryConfig{}, nil)
	assert.Equal(t, BatteryActionNormal, plan.Action)
}

func exportCfg() BatteryConfig {
	return BatteryConfig{
		Soc:            90,
		MinSoc:         20,
		MaxSoc:         100,
		ReserveSoc:     20,
		CapacityWh:     10000,
		ChargeW:        2500,
		DischargeW:     2500,
		EtaC:           0.9,
		EtaD:           0.9,
		CycleCost:      0.05,
		GridThresholdW: 10000,
		HeadroomW:      8000,
		LiveResidualW:  1000,
	}
}

func TestPlanBatteryExportsExcessOnExpensiveFeedIn(t *testing.T) {
	cfg := exportCfg()
	prices := []float64{0.35, 0.20, 0.18, 0.18, 0.19, 0.20, 0.18, 0.22}
	slots := testSlots(prices, 1000, 0)
	feedIn := []float64{0.16, 0.04, 0.04, 0.04, 0.04, 0.05, 0.04, 0.12}
	for i := range slots {
		slots[i].FeedIn = feedIn[i]
	}

	plan := PlanBattery(cfg, slots)
	require.Equal(t, BatteryActionDischarge, plan.Action)
	assert.Equal(t, BatteryReasonExport, plan.Reason)
	assert.Greater(t, plan.DischargeW, 0)
}

func TestPlanBatteryExportsOnlyOverflowWhenLaterHourPaysMore(t *testing.T) {
	cfg := exportCfg()
	cfg.Soc = 30
	cfg.CapacityWh = 2000
	prices := []float64{0.30, 0.18, 0.18, 0.18, 0.40, 0.18, 0.18, 0.18}
	slots := testSlots(prices, 1000, 0)
	feedIn := []float64{0.12, 0.04, 0.04, 0.04, 0.18, 0.04, 0.04, 0.04}
	for i := range slots {
		slots[i].FeedIn = feedIn[i]
	}

	plan := PlanBattery(cfg, slots)
	assert.NotEqual(t, BatteryReasonExport, plan.Reason, "small leftover should wait for the higher feed-in hour")
}

func TestPlanBatteryDoesNotExportEnergyNeededForPeak(t *testing.T) {
	cfg := exportCfg()
	cfg.Soc = 25
	cfg.GridThresholdW = 5000
	cfg.LiveResidualW = 2000
	prices := make([]float64, 16)
	for i := range prices {
		prices[i] = 0.20
	}
	slots := testSlots(prices, 2000, 0)
	for i := 8; i < len(slots); i++ {
		slots[i].HomeWh = 8000 * tariff.SlotDuration.Hours()
	}
	for i := range slots {
		slots[i].FeedIn = 0.04
	}
	slots[0].FeedIn = 0.16

	plan := PlanBattery(cfg, slots)
	assert.NotEqual(t, BatteryReasonExport, plan.Reason)
	assert.NotEqual(t, BatteryActionDischarge, plan.Action)
}

func TestPlanBatteryDoesNotExportStaticFeedIn(t *testing.T) {
	cfg := exportCfg()
	prices := []float64{0.30, 0.18, 0.18, 0.18, 0.20, 0.18, 0.18, 0.22}
	slots := testSlots(prices, 1000, 0)
	for i := range slots {
		slots[i].FeedIn = 0.08
	}

	plan := PlanBattery(cfg, slots)
	assert.NotEqual(t, BatteryReasonExport, plan.Reason)
}
