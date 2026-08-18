package planner

import (
	"testing"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/tariff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func flattenSlots(n int, homeW float64) []BatterySlot {
	start := time.Date(2026, 8, 18, 18, 0, 0, 0, time.UTC)
	hours := tariff.SlotDuration.Hours()
	slots := make([]BatterySlot, n)
	for i := range slots {
		s := start.Add(time.Duration(i) * tariff.SlotDuration)
		slots[i] = BatterySlot{
			Start:  s,
			End:    s.Add(tariff.SlotDuration),
			HomeWh: homeW * hours,
			Price:  0.20,
		}
	}
	return slots
}

func TestFlattenChargeDemandsSpreadsUnderGridLimit(t *testing.T) {
	slots := flattenSlots(16, 2000)
	start := slots[0].Start
	preferred := api.Rates{{Start: start.Add(8 * tariff.SlotDuration), End: start.Add(12 * tariff.SlotDuration)}}
	demand := ChargeDemand{
		RequiredWh: 11000,
		MaxW:       11000,
		Deadline:   start.Add(16 * tariff.SlotDuration),
		Preferred:  preferred,
	}

	added, currentW := FlattenChargeDemands(slots, []ChargeDemand{demand}, 10000)
	assert.InDelta(t, 11000, added, 1)
	assert.Len(t, currentW, 1)

	hours := tariff.SlotDuration.Hours()
	for _, s := range slots {
		residualW := max(0, s.HomeWh) / hours
		assert.LessOrEqual(t, residualW, 10000.0+1, "flattened load should stay at or under the grid limit")
	}

	cfg := BatteryConfig{
		Soc:            50,
		MinSoc:         20,
		MaxSoc:         100,
		ReserveSoc:     20,
		CapacityWh:     10000,
		ChargeW:        2500,
		DischargeW:     5000,
		EtaC:           0.9,
		EtaD:           0.9,
		CycleCost:      0.05,
		GridThresholdW: 10000,
		HeadroomW:      8000,
		LiveResidualW:  2000,
	}
	plan := PlanBattery(cfg, slots)
	assert.NotEqual(t, BatteryActionCharge, plan.Action)
	assert.Equal(t, 0.0, plan.PeakWh)
}

func TestFlattenChargeDemandsKeepsOvershootWhenDeadlineIsTight(t *testing.T) {
	slots := flattenSlots(8, 2000)
	start := slots[0].Start
	preferred := api.Rates{{Start: start.Add(2 * tariff.SlotDuration), End: start.Add(4 * tariff.SlotDuration)}}
	demand := ChargeDemand{
		RequiredWh: 11000,
		MaxW:       11000,
		Deadline:   start.Add(4 * tariff.SlotDuration),
		Preferred:  preferred,
	}

	added, _ := FlattenChargeDemands(slots, []ChargeDemand{demand}, 10000)
	assert.InDelta(t, 11000, added, 1)

	hours := tariff.SlotDuration.Hours()
	var peak bool
	for _, s := range slots {
		if s.HomeWh/hours > 10000+1 {
			peak = true
			break
		}
	}
	assert.True(t, peak, "tight deadline should still overshoot the grid limit")
}

func TestFlattenChargeDemandsOverlaysWithoutThreshold(t *testing.T) {
	start := time.Date(2026, 8, 18, 18, 0, 0, 0, time.UTC)
	slots := make([]BatterySlot, 8)
	for i := range slots {
		s := start.Add(time.Duration(i) * tariff.SlotDuration)
		slots[i] = BatterySlot{
			Start:  s,
			End:    s.Add(tariff.SlotDuration),
			HomeWh: 500,
			Price:  0.20,
		}
	}
	plan := api.Rates{{Start: start.Add(2 * tariff.SlotDuration), End: start.Add(6 * tariff.SlotDuration)}}
	demand := ChargeDemand{
		RequiredWh: 11000,
		MaxW:       11000,
		Preferred:  plan,
	}

	added, currentW := FlattenChargeDemands(slots, []ChargeDemand{demand}, 0)
	assert.InDelta(t, 11000, added, 0.1)
	assert.InDelta(t, 500, slots[0].HomeWh, 0.1)
	assert.InDelta(t, 500+11000*tariff.SlotDuration.Hours(), slots[2].HomeWh, 0.1)
	assert.Equal(t, 0.0, currentW[0])
}

func TestApplyChargePlanPartialSlot(t *testing.T) {
	start := time.Date(2026, 8, 18, 7, 0, 0, 0, time.UTC)
	slot := BatterySlot{Start: start, End: start.Add(tariff.SlotDuration), HomeWh: 200}
	plan := api.Rates{{Start: start, End: start.Add(5 * time.Minute)}}

	added := applyChargePlan([]BatterySlot{slot}, plan, 7200)
	assert.InDelta(t, 7200*5/60, added, 0.1)
}

func TestOverlapHours(t *testing.T) {
	a0 := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	a1 := a0.Add(15 * time.Minute)
	assert.Equal(t, 0.0, overlapHours(a0, a1, a1, a1.Add(time.Hour)))
	assert.InDelta(t, 0.25, overlapHours(a0, a1, a0.Add(-time.Hour), a1.Add(time.Hour)), 1e-9)
}

func TestFlattenChargeDemandsCurrentSlotWatts(t *testing.T) {
	slots := flattenSlots(8, 2000)
	start := slots[0].Start
	demand := ChargeDemand{
		RequiredWh: 4000,
		MaxW:       11000,
		Deadline:   start.Add(8 * tariff.SlotDuration),
		Preferred:  api.Rates{{Start: start, End: start.Add(tariff.SlotDuration)}},
	}

	_, currentW := FlattenChargeDemands(slots, []ChargeDemand{demand}, 10000)
	require.Len(t, currentW, 1)
	assert.InDelta(t, 8000, currentW[0], 1, "current slot should use remaining 8 kW headroom")
}
