package core

import (
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/core/loadpoint"
	"github.com/evcc-io/evcc/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func heatingLoadpoint(t *testing.T, maxPowerW float64) *Loadpoint {
	t.Helper()
	ctrl := gomock.NewController(t)

	type featureDecorator struct {
		api.Charger
		api.FeatureDescriber
		api.PowerLimiter
	}

	fd := api.NewMockFeatureDescriber(ctrl)
	fd.EXPECT().Features().AnyTimes().Return([]api.Feature{api.Heating})

	pl := api.NewMockPowerLimiter(ctrl)
	pl.EXPECT().GetMinMaxPower().AnyTimes().Return(0.0, maxPowerW, nil)

	lp := NewLoadpoint(util.NewLogger("test"), nil)
	lp.charger = &featureDecorator{
		Charger:          api.NewMockCharger(ctrl),
		FeatureDescriber: fd,
		PowerLimiter:     pl,
	}
	lp.heatingComfort = loadpoint.HeatingComfort{
		StopTemp:      50,
		AssumedPowerW: maxPowerW,
	}
	return lp
}

func TestGetPlanRequiredDurationRepeatingHeatingDespiteStopTemp(t *testing.T) {
	// Calendar plan: 2.3 kWh before 18:00. Tank already at stop temp must not
	// show "goal already reached" while the kWh goal is still outstanding.
	lp := heatingLoadpoint(t, 1150)
	lp.vehicleSoc = 52 // above default stop temp 50

	lp.repeatingPlans = []api.RepeatingPlan{{
		Weekdays: []int{0, 1, 2, 3, 4, 5, 6},
		Time:     "18:00",
		Tz:       "Europe/Brussels",
		Energy:   2.3,
		Active:   true,
	}}

	d := lp.getPlanRequiredDuration(2.3, 1150)
	assert.Greater(t, d, time.Hour, "must still plan heating time for the energy goal")
	assert.InDelta(t, 2*time.Hour, d, float64(15*time.Minute))
}

func TestGetPlanRequiredDurationStaticHeatingDespiteStopTemp(t *testing.T) {
	lp := heatingLoadpoint(t, 1150)
	lp.vehicleSoc = 55
	lp.planEnergy = 2.3
	lp.planTime = time.Now().Add(8 * time.Hour)

	d := lp.getPlanRequiredDuration(2.3, 1150)
	assert.Greater(t, d, 30*time.Minute)
}

func TestGetPlanRequiredDurationStaticHeatingSubtractsDeliveredEnergy(t *testing.T) {
	lp := heatingLoadpoint(t, 1000)
	lp.planEnergy = 2.0
	lp.planTime = time.Now().Add(6 * time.Hour)
	lp.planEnergyOffset = 0
	lp.energyMetrics.Update(1.5) // 1.5 kWh already delivered this session

	d := lp.getPlanRequiredDuration(2.0, 1000)
	assert.InDelta(t, 30*time.Minute, d, float64(2*time.Minute))
}

func TestGetPlanRequiredDurationRepeatingUsesFullGoal(t *testing.T) {
	lp := heatingLoadpoint(t, 1000)
	lp.vehicleSoc = 40
	lp.repeatingPlans = []api.RepeatingPlan{{
		Weekdays: []int{1, 2, 3, 4, 5, 6, 0},
		Time:     "18:00",
		Tz:       "Europe/Brussels",
		Energy:   2.3,
		Active:   true,
	}}

	d := lp.getPlanRequiredDuration(2.3, 1000)
	assert.InDelta(t, 2*time.Hour+18*time.Minute, d, float64(5*time.Minute))
}

func TestNextEnergyPlanRepeatingAllWeekdays(t *testing.T) {
	clk := clock.NewMock()
	loc, err := time.LoadLocation("Europe/Brussels")
	require.NoError(t, err)
	// Saturday 09:20
	clk.Set(time.Date(2026, 8, 22, 9, 20, 0, 0, loc))

	lp := heatingLoadpoint(t, 1150)
	lp.clock = clk
	lp.repeatingPlans = []api.RepeatingPlan{{
		Weekdays: []int{0, 1, 2, 3, 4, 5, 6},
		Time:     "18:00",
		Tz:       "Europe/Brussels",
		Energy:   2.3,
		Active:   true,
	}}

	end, energy, id, fixed := lp.nextEnergyPlan()
	require.Equal(t, 2, id)
	assert.InDelta(t, 2.3, energy, 0.01)
	assert.False(t, fixed)
	assert.Equal(t, 18, end.In(loc).Hour())
	assert.Equal(t, 22, end.In(loc).Day())
}

func TestNextEnergyPlanEmptyTimezoneUsesLocal(t *testing.T) {
	lp := heatingLoadpoint(t, 1150)
	lp.repeatingPlans = []api.RepeatingPlan{{
		Weekdays: []int{0, 1, 2, 3, 4, 5, 6},
		Time:     "18:00",
		Tz:       "",
		Energy:   2.3,
		Active:   true,
	}}

	end, energy, id, _ := lp.nextEnergyPlan()
	assert.Equal(t, 2, id)
	assert.InDelta(t, 2.3, energy, 0.01)
	assert.False(t, end.IsZero())
}

func TestChargeDemandRepeatingHeatingNearStopTemp(t *testing.T) {
	lp := heatingLoadpoint(t, 1150)
	lp.vehicleSoc = 49.8 // within 0.5 K of stop: pattern estimate returns 0

	demand, ok := lp.chargeDemand(time.Now().Add(8*time.Hour), 2.3, false)
	require.True(t, ok, "must still schedule kWh goal when tank is nearly at stop temp")
	assert.InDelta(t, 2300, demand.RequiredWh, 1)
	assert.Greater(t, demand.MaxW, 0.0)
}

func TestGetNextOccurrenceSameDayBeforeDeadline(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Brussels")
	require.NoError(t, err)

	// Pin "now" by calling at a known weekday: use Saturday Aug 22 2026 09:20
	// GetNextOccurrence uses time.Now(), so we test the logic via nextEnergyPlan
	// with mock clock above. Here verify util directly with a weekday that matches.
	got, err := util.GetOccurrences(
		[]int{0, 1, 2, 3, 4, 5, 6},
		"18:00",
		"Europe/Brussels",
		time.Date(2026, 8, 22, 9, 20, 0, 0, loc),
		time.Date(2026, 8, 23, 0, 0, 0, 0, loc),
	)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 18, got[0].Hour())
	assert.Equal(t, time.Saturday, got[0].Weekday())
}
