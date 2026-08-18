package core

import (
	"testing"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/core/planner"
	"github.com/evcc-io/evcc/tariff"
	"github.com/stretchr/testify/assert"
)

func TestApplyChargePlanAddsVehicleEnergyToOverlappingSlots(t *testing.T) {
	start := time.Date(2026, 8, 18, 18, 0, 0, 0, time.UTC)
	slots := make([]planner.BatterySlot, 8)
	for i := range slots {
		s := start.Add(time.Duration(i) * tariff.SlotDuration)
		slots[i] = planner.BatterySlot{
			Start:  s,
			End:    s.Add(tariff.SlotDuration),
			HomeWh: 500,
		}
	}

	plan := api.Rates{
		{Start: start.Add(2 * tariff.SlotDuration), End: start.Add(6 * tariff.SlotDuration)},
	}
	added := applyChargePlan(slots, plan, 11000)

	assert.InDelta(t, 11000, added, 0.1)
	assert.InDelta(t, 500, slots[0].HomeWh, 0.1)
	assert.InDelta(t, 500+11000*tariff.SlotDuration.Hours(), slots[2].HomeWh, 0.1)
	assert.InDelta(t, 500, slots[6].HomeWh, 0.1)
}

func TestApplyChargePlanPartialSlot(t *testing.T) {
	start := time.Date(2026, 8, 18, 7, 0, 0, 0, time.UTC)
	slot := planner.BatterySlot{Start: start, End: start.Add(tariff.SlotDuration), HomeWh: 200}
	plan := api.Rates{{Start: start, End: start.Add(5 * time.Minute)}}

	added := applyChargePlan([]planner.BatterySlot{slot}, plan, 7200)
	assert.InDelta(t, 7200*5/60, added, 0.1)
}

func TestOverlapHours(t *testing.T) {
	a0 := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	a1 := a0.Add(15 * time.Minute)
	assert.Equal(t, 0.0, overlapHours(a0, a1, a1, a1.Add(time.Hour)))
	assert.InDelta(t, 0.25, overlapHours(a0, a1, a0.Add(-time.Hour), a1.Add(time.Hour)), 1e-9)
}

func TestHouseholdPowerExcludesCharger(t *testing.T) {
	site := &Site{
		gridPower: 13000,
		pvPower:   0,
	}
	site.battery.Power = 0
	assert.Equal(t, 13000.0, site.householdPower())
}
