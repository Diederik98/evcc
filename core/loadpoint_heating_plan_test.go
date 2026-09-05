package core

import (
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/core/loadpoint"
	"github.com/evcc-io/evcc/core/planner"
	"github.com/evcc-io/evcc/tariff"
	"github.com/evcc-io/evcc/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func brusselsMorning(t *testing.T, hour, min int) (*clock.Mock, *time.Location) {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Brussels")
	require.NoError(t, err)
	clk := clock.NewMock()
	clk.Set(time.Date(2026, 8, 22, hour, min, 0, 0, loc)) // Saturday
	return clk, loc
}

func heatingPlanLoadpoint(t *testing.T, clk *clock.Mock, comfort loadpoint.HeatingComfort) *Loadpoint {
	t.Helper()
	Voltage = 230
	ctrl := gomock.NewController(t)

	type featureDecorator struct {
		api.Charger
		api.FeatureDescriber
		api.PowerLimiter
	}

	power := comfort.AssumedPowerW
	if power <= 0 {
		power = 1150
	}

	fd := api.NewMockFeatureDescriber(ctrl)
	fd.EXPECT().Features().AnyTimes().Return([]api.Feature{api.Heating, api.IntegratedDevice})

	pl := api.NewMockPowerLimiter(ctrl)
	pl.EXPECT().GetMinMaxPower().AnyTimes().Return(0.0, power, nil)

	log := util.NewLogger("test")
	lp := NewLoadpoint(log, nil)
	lp.clock = clk
	lp.charger = &featureDecorator{
		Charger:          api.NewMockCharger(ctrl),
		FeatureDescriber: fd,
		PowerLimiter:     pl,
	}
	lp.status = api.StatusB // connected heat pump
	lp.heatingComfort = comfort
	lp.planner = planner.New(log, nil, planner.WithClock(clk))

	uiChan, pushChan, lpChan := createChannels(t)
	attachChannels(lp, uiChan, pushChan, lpChan)

	return lp
}

func defaultComfort(power float64) loadpoint.HeatingComfort {
	return loadpoint.HeatingComfort{
		MinTemp:       40,
		Hysteresis:    1.5,
		MinOnTime:     900, // 15 min
		StopTemp:      50,
		AssumedPowerW: power,
	}
}

func repeatingEveningPlan(energy float64) []api.RepeatingPlan {
	return []api.RepeatingPlan{{
		Weekdays: []int{0, 1, 2, 3, 4, 5, 6},
		Time:     "18:00",
		Tz:       "Europe/Brussels",
		Energy:   energy,
		Active:   true,
	}}
}

func heatingWouldStayOn(lp *Loadpoint) bool {
	return lp.heatingComfortHeating() || lp.plannerActive()
}

func TestHeatingComfortActivatesBelowMinTemp(t *testing.T) {
	clk, _ := brusselsMorning(t, 6, 30)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 38

	assert.True(t, lp.heatingComfortActive())
	status := lp.heatingStatusLocked()
	assert.Equal(t, "comfort", status.Reason)
}

func TestHeatingComfortInactiveAboveMinTemp(t *testing.T) {
	clk, _ := brusselsMorning(t, 10, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 42 // above min 40

	assert.False(t, lp.heatingComfortActive())
}

func TestHeatingComfortInactiveAtStopTemp(t *testing.T) {
	clk, _ := brusselsMorning(t, 10, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 52 // at/above stop 50

	assert.False(t, lp.heatingComfortActive())
}

func TestHeatingComfortHysteresisKeepsHeatingOn(t *testing.T) {
	clk, _ := brusselsMorning(t, 6, 0)
	comfort := defaultComfort(1150)
	lp := heatingPlanLoadpoint(t, clk, comfort)
	lp.vehicleSoc = 38
	require.True(t, lp.heatingComfortActive())

	// rose above min but below min+hysteresis: still on
	lp.vehicleSoc = 41
	assert.True(t, lp.heatingComfortActive())

	// after min on-time and above min+hysteresis: off
	clk.Add(16 * time.Minute)
	lp.vehicleSoc = 42
	assert.False(t, lp.heatingComfortActive())
}

func TestComfortAndRepeatingPlanMorningScenario(t *testing.T) {
	// User scenario: 09:20, tank warm-ish but kWh goal not met. Comfort may be
	// off while calendar plan must still schedule before 18:00.
	clk, loc := brusselsMorning(t, 9, 20)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 45 // above min 40, below stop 50
	lp.repeatingPlans = repeatingEveningPlan(2.3)

	assert.False(t, lp.heatingComfortActive(), "comfort should not run when above min temp")

	end, energy, id, _ := lp.nextEnergyPlan()
	require.Equal(t, 2, id)
	assert.InDelta(t, 2.3, energy, 0.01)
	assert.Equal(t, 18, end.In(loc).Hour())

	d := lp.getPlanRequiredDuration(energy, 1150)
	assert.Greater(t, d, time.Hour, "calendar must still need runtime, not goal reached")

	demand, ok := lp.chargeDemand(end, energy, false)
	require.True(t, ok)
	assert.InDelta(t, 2300, demand.RequiredWh, 1)
}

func TestComfortOnWhileRepeatingPlanStillPending(t *testing.T) {
	clk, _ := brusselsMorning(t, 7, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 38 // comfort floor active
	lp.repeatingPlans = repeatingEveningPlan(2.3)

	assert.True(t, lp.heatingComfortActive())
	assert.False(t, lp.plannerActive(), "calendar slot should not run at 07:00")

	d := lp.getPlanRequiredDuration(2.3, 1150)
	assert.Greater(t, d, time.Hour)
}

func TestRepeatingPlanExecutesInScheduledSlot(t *testing.T) {
	clk, loc := brusselsMorning(t, 16, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 45
	lp.repeatingPlans = repeatingEveningPlan(2.3)

	active := lp.plannerActive()
	assert.True(t, active, "planner should be active inside the pre-18:00 window")
	assert.True(t, lp.planActive)

	planTime := lp.EffectivePlanTime()
	assert.Equal(t, 18, planTime.In(loc).Hour())
}

func TestComfortAndRepeatingPlanBothAllowFastCharge(t *testing.T) {
	clk, _ := brusselsMorning(t, 7, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 38
	lp.repeatingPlans = repeatingEveningPlan(2.3)
	lp.enabled = true

	comfort := lp.heatingComfortHeating()
	plannerOn := lp.plannerActive()
	assert.True(t, comfort)
	assert.False(t, plannerOn)
	// Update() branch: minSocNotReached || plannerActive || comfortActive
	assert.True(t, comfort || plannerOn)
}

func TestUpdateHeatingBoostReasonComfort(t *testing.T) {
	clk, _ := brusselsMorning(t, 6, 30)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 38
	lp.enabled = true
	require.True(t, lp.heatingComfortActive())

	lp.UpdateHeatingBoost(500)
	status := lp.heatingStatusLocked()
	assert.True(t, status.Active)
	assert.Equal(t, "comfort", status.Reason)
}

func TestUpdateHeatingBoostReasonCalendar(t *testing.T) {
	clk, _ := brusselsMorning(t, 16, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 45
	lp.repeatingPlans = repeatingEveningPlan(2.3)
	lp.enabled = true
	require.True(t, lp.plannerActive())

	lp.UpdateHeatingBoost(800)
	status := lp.heatingStatusLocked()
	assert.True(t, status.Active)
	assert.Equal(t, "calendar", status.Reason)
}

func TestHorizonChargeDemandsIncludesRepeatingPlanWhileComfortCouldRun(t *testing.T) {
	clk, loc := brusselsMorning(t, 9, 20)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 38 // comfort would be allowed
	lp.repeatingPlans = repeatingEveningPlan(2.3)

	// Horizon ending before tomorrow's occurrence keeps a single demand.
	start := clk.Now().Truncate(tariff.SlotDuration)
	slots := []planner.BatterySlot{{
		Start: start,
		End:   time.Date(start.Year(), start.Month(), start.Day(), 23, 59, 0, 0, loc),
	}}

	demands := lp.HorizonChargeDemands(slots)
	require.Len(t, demands, 1)
	assert.InDelta(t, 2300, demands[0].RequiredWh, 1)
	assert.Equal(t, 18, demands[0].Deadline.In(loc).Hour())
}

func TestWarmTankDoesNotCancelRepeatingPlanDemand(t *testing.T) {
	// Battery overlay still sees the kWh goal when the tank is already warm.
	// Calendar slots keep heating to the scheduled energy, past comfort stop temp.
	clk, loc := brusselsMorning(t, 16, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 52 // above stop temp
	lp.repeatingPlans = repeatingEveningPlan(2.3)

	require.False(t, lp.heatingComfortActive())
	require.True(t, lp.LimitSocReached(), "stop temp is still the comfort ceiling")
	assert.Greater(t, lp.getPlanRequiredDuration(2.3, 1150), 30*time.Minute)
	assert.True(t, lp.plannerActive(), "calendar slot must keep heating to the scheduled kWh")
	assert.True(t, heatingWouldStayOn(lp))

	start := clk.Now().Truncate(tariff.SlotDuration)
	demands := lp.HorizonChargeDemands([]planner.BatterySlot{{
		Start: start,
		End:   time.Date(start.Year(), start.Month(), start.Day(), 23, 59, 0, 0, loc),
	}})
	require.Len(t, demands, 1)
	assert.InDelta(t, 2300, demands[0].RequiredWh, 1)
}

func TestComfortThenRepeatingPlanExecutesSameDay(t *testing.T) {
	clk, loc := brusselsMorning(t, 7, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.repeatingPlans = repeatingEveningPlan(2.3)

	// Early morning: comfort floor, calendar not active yet.
	lp.vehicleSoc = 38
	require.True(t, lp.heatingComfortActive())
	assert.False(t, lp.plannerActive())

	// Tank warmed by midday; calendar still pending.
	clk.Set(time.Date(2026, 8, 22, 12, 0, 0, 0, loc))
	lp.vehicleSoc = 45
	assert.False(t, lp.heatingComfortActive())
	assert.False(t, lp.plannerActive())
	assert.Greater(t, lp.getPlanRequiredDuration(2.3, 1150), time.Hour)

	// Plan slot: calendar takes over even though tank is above min temp.
	clk.Set(time.Date(2026, 8, 22, 17, 0, 0, 0, loc))
	require.True(t, lp.plannerActive())
	assert.True(t, lp.planActive)
	assert.Equal(t, 18, lp.EffectivePlanTime().In(loc).Hour())

	// After the deadline the heater must stop even if the kWh goal is unmet.
	clk.Set(time.Date(2026, 8, 22, 18, 5, 0, 0, loc))
	assert.False(t, lp.plannerActive())
	assert.False(t, lp.heatingComfortActive())
	assert.False(t, heatingWouldStayOn(lp))
	assert.True(t, lp.heatingPlanExclusive(), "tomorrow's occurrence still owns PV surplus")
}

func TestComfortDuringPlanSlotBothAllowFastCharge(t *testing.T) {
	clk, _ := brusselsMorning(t, 17, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.repeatingPlans = repeatingEveningPlan(2.3)
	lp.vehicleSoc = 38 // below min temp during calendar slot

	require.True(t, lp.heatingComfortActive())
	require.True(t, lp.plannerActive())
}

func TestHeatingPlanExclusiveBlocksOutsideCalendarSlot(t *testing.T) {
	clk, _ := brusselsMorning(t, 10, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.repeatingPlans = repeatingEveningPlan(2.3)
	lp.vehicleSoc = 45

	assert.True(t, lp.heatingPlanExclusive())
	assert.False(t, lp.plannerActive())
}

func TestGetNextOccurrenceFromUsesReferenceTime(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Brussels")
	require.NoError(t, err)
	now := time.Date(2026, 8, 22, 9, 20, 0, 0, loc)

	got, err := util.GetNextOccurrenceFrom(now, []int{0, 1, 2, 3, 4, 5, 6}, "18:00", "Europe/Brussels")
	require.NoError(t, err)
	assert.Equal(t, 18, got.Hour())
	assert.Equal(t, 22, got.Day())
}

func TestHeatingComfortStopTempWinsOverMinOn(t *testing.T) {
	clk, _ := brusselsMorning(t, 6, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 38
	require.True(t, lp.heatingComfortActive())

	clk.Add(time.Minute)
	lp.vehicleSoc = 51
	assert.False(t, lp.heatingComfortActive())
	assert.True(t, lp.LimitSocReached())
	assert.False(t, heatingWouldStayOn(lp))
}

func TestHeatingComfortHysteresisStopsAndRestartsBelowMin(t *testing.T) {
	clk, _ := brusselsMorning(t, 6, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.repeatingPlans = repeatingEveningPlan(2.3)
	lp.vehicleSoc = 38
	require.True(t, heatingWouldStayOn(lp))

	clk.Add(16 * time.Minute)
	lp.vehicleSoc = 42 // min 40 + hysteresis 1.5
	assert.False(t, lp.heatingComfortActive())
	assert.False(t, lp.plannerActive(), "calendar must not keep the heater on at 06:16")
	assert.False(t, heatingWouldStayOn(lp))

	lp.vehicleSoc = 39
	assert.True(t, lp.heatingComfortActive())
	assert.True(t, heatingWouldStayOn(lp))
}

func TestRepeatingHeatingPlanStopsWhenEnergyDelivered(t *testing.T) {
	clk, loc := brusselsMorning(t, 16, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 51
	lp.repeatingPlans = repeatingEveningPlan(2.3)

	require.True(t, lp.plannerActive(), "calendar slot starts with the full kWh goal")
	require.True(t, lp.repeatingPlanOffsetSet)

	lp.energyMetrics.Update(2.3)
	assert.Equal(t, time.Duration(0), lp.getPlanRequiredDuration(2.3, 1150))
	assert.False(t, lp.plannerActive(), "scheduled kWh must end the calendar slot")
	assert.False(t, lp.heatingComfortActive())
	assert.False(t, heatingWouldStayOn(lp))

	clk.Set(time.Date(2026, 8, 22, 17, 30, 0, 0, loc))
	assert.False(t, lp.plannerActive(), "must not restart before the deadline after the kWh goal")

	start := clk.Now().Truncate(tariff.SlotDuration)
	demands := lp.HorizonChargeDemands([]planner.BatterySlot{{
		Start: start,
		End:   time.Date(start.Year(), start.Month(), start.Day(), 23, 59, 0, 0, loc),
	}})
	assert.Empty(t, demands, "today's kWh goal is already delivered")
}

func TestRepeatingHeatingPlanResetsGoalNextDay(t *testing.T) {
	clk, loc := brusselsMorning(t, 16, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 45
	lp.repeatingPlans = repeatingEveningPlan(2.3)

	require.True(t, lp.plannerActive())
	lp.energyMetrics.Update(2.3)
	require.False(t, lp.plannerActive())

	clk.Set(time.Date(2026, 8, 23, 16, 0, 0, 0, loc))
	assert.Greater(t, lp.getPlanRequiredDuration(2.3, 1150), time.Hour)
	assert.True(t, lp.plannerActive(), "the next day's occurrence starts from a full kWh goal")
}

func TestHorizonChargeDemandsSkipsPastDeadlineOccurrence(t *testing.T) {
	clk, loc := brusselsMorning(t, 19, 32)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 51
	lp.repeatingPlans = repeatingEveningPlan(1.725)

	assert.False(t, lp.plannerActive(), "today's 18:00 deadline has passed")

	start := clk.Now().Truncate(tariff.SlotDuration)
	sameDay := lp.HorizonChargeDemands([]planner.BatterySlot{{
		Start: start,
		End:   time.Date(start.Year(), start.Month(), start.Day(), 23, 59, 0, 0, loc),
	}})
	assert.Empty(t, sameDay, "must not inject a finished occurrence starting now")

	demands := lp.HorizonChargeDemands([]planner.BatterySlot{{
		Start: start,
		End:   start.Add(24 * time.Hour),
	}})
	require.Len(t, demands, 1)
	assert.Equal(t, 23, demands[0].Deadline.In(loc).Day())
	assert.Equal(t, 18, demands[0].Deadline.In(loc).Hour())
}

func TestHorizonChargeDemandsKeepsActiveOverrun(t *testing.T) {
	clk, loc := brusselsMorning(t, 18, 5)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.repeatingPlans = repeatingEveningPlan(1.725)
	lp.planActive = true

	start := clk.Now().Truncate(tariff.SlotDuration)
	demands := lp.HorizonChargeDemands([]planner.BatterySlot{{
		Start: start,
		End:   time.Date(start.Year(), start.Month(), start.Day(), 23, 59, 0, 0, loc),
	}})
	require.Len(t, demands, 1)
	assert.Equal(t, 22, demands[0].Deadline.In(loc).Day())
}

func TestRepeatingHeatingPlanStopsAfterDeadline(t *testing.T) {
	clk, loc := brusselsMorning(t, 17, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 45
	lp.repeatingPlans = repeatingEveningPlan(2.3)

	require.True(t, lp.plannerActive())
	require.True(t, heatingWouldStayOn(lp))

	clk.Set(time.Date(2026, 8, 22, 18, 5, 0, 0, loc))
	assert.False(t, lp.plannerActive())
	assert.False(t, lp.heatingComfortActive())
	assert.False(t, lp.LimitSocReached())
	assert.False(t, heatingWouldStayOn(lp))
}

func TestRepeatingHeatingPlanContinuesPastStopTempDuringSlot(t *testing.T) {
	clk, _ := brusselsMorning(t, 17, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 45
	lp.repeatingPlans = repeatingEveningPlan(2.3)
	require.True(t, lp.plannerActive())

	lp.vehicleSoc = 50
	assert.True(t, lp.LimitSocReached(), "stop temp remains the comfort ceiling")
	assert.False(t, lp.heatingComfortActive())
	assert.True(t, lp.plannerActive(), "calendar must keep heating to the scheduled kWh")
	assert.True(t, heatingWouldStayOn(lp))
}

func TestRepeatingHeatingPlanIgnoresComfortStopTemp(t *testing.T) {
	clk, _ := brusselsMorning(t, 16, 0)
	comfort := defaultComfort(1150)
	comfort.StopTemp = 45
	lp := heatingPlanLoadpoint(t, clk, comfort)
	lp.vehicleSoc = 47
	lp.repeatingPlans = repeatingEveningPlan(2.3)

	require.False(t, lp.heatingComfortActive())
	require.True(t, lp.LimitSocReached())
	assert.True(t, lp.plannerActive(), "47°C must not abort a calendar slot at 45°C comfort stop")
	assert.Greater(t, lp.getPlanRequiredDuration(2.3, 1150), time.Hour)
}

func TestRepeatingHeatingPlanHonorsMinOnAfterSlot(t *testing.T) {
	clk, loc := brusselsMorning(t, 17, 50)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 45
	lp.repeatingPlans = repeatingEveningPlan(2.3)
	require.True(t, lp.plannerActive())
	lp.heatingBoostStart = clk.Now()

	clk.Set(time.Date(2026, 8, 22, 18, 2, 0, 0, loc))
	assert.True(t, lp.plannerActive(), "min on-time may briefly extend past the deadline")
	assert.True(t, heatingWouldStayOn(lp))

	clk.Set(time.Date(2026, 8, 22, 18, 10, 0, 0, loc))
	assert.False(t, lp.plannerActive())
	assert.False(t, heatingWouldStayOn(lp))
}

func TestRepeatingHeatingPlanDoesNotRestartNextCycleAfterDeadline(t *testing.T) {
	clk, loc := brusselsMorning(t, 17, 0)
	lp := heatingPlanLoadpoint(t, clk, defaultComfort(1150))
	lp.vehicleSoc = 45
	lp.repeatingPlans = repeatingEveningPlan(2.3)
	require.True(t, lp.plannerActive())

	clk.Set(time.Date(2026, 8, 22, 18, 5, 0, 0, loc))
	require.False(t, lp.plannerActive())
	assert.False(t, lp.planActive)
	assert.False(t, lp.plannerActive(), "a later cycle must not resume continuous heating")
	assert.False(t, heatingWouldStayOn(lp))
}
