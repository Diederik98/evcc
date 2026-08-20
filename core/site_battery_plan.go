package core

import (
	"encoding/json"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/core/keys"
	"github.com/evcc-io/evcc/core/loadpoint"
	"github.com/evcc-io/evcc/core/metrics"
	"github.com/evcc-io/evcc/core/planner"
	"github.com/evcc-io/evcc/tariff"
)

const defaultBatteryCycleCost = 0.05 // €/kWh throughput

type batteryPlanStatus struct {
	Action         string  `json:"action"`
	Reason         string  `json:"reason"`
	ChargeW        int     `json:"chargeW"`
	DischargeW     int     `json:"dischargeW"`
	TargetSoc      float64 `json:"targetSoc"`
	PeakWh         float64 `json:"peakWh"`
	DischargeFloor float64 `json:"dischargeFloor"`
	LoadWh         float64 `json:"loadWh,omitempty"`
	LoadW          float64 `json:"loadW,omitempty"`
}

var _ api.BytesMarshaler = (*batteryPlanStatus)(nil)

func (s batteryPlanStatus) MarshalBytes() ([]byte, error) {
	return json.Marshal(s)
}

func (site *Site) evaluateBatteryPlan() (planner.BatteryPlan, bool) {
	site.batteryPlanLoadWh = 0
	site.batteryPlanLoadCaps = nil
	if !site.batteryConfigured() || site.battery.Capacity <= 0 {
		return planner.BatteryPlan{}, false
	}

	slots := site.batteryPlanSlots()
	if len(slots) == 0 {
		return planner.BatteryPlan{}, false
	}

	chargeW, dischargeW := site.batteryPowerLimits()
	cfg := planner.BatteryConfig{
		Soc:            site.battery.Soc,
		MinSoc:         site.peakShaveEffectiveMinSoc(),
		MaxSoc:         site.batteryPlanMaxSoc(),
		ReserveSoc:     site.PeakShaveReserveSoc,
		CapacityWh:     site.battery.Capacity * 1e3,
		ChargeW:        chargeW,
		DischargeW:     dischargeW,
		EtaC:           0.9,
		EtaD:           0.9,
		CycleCost:      site.BatteryCycleCost,
		GridThresholdW: site.GridThreshold * 1000,
		HeadroomW:      site.peakShaveGridHeadroom(),
		LiveResidualW:  max(0, site.gridPower-site.battery.Power),
	}

	return planner.PlanBattery(cfg, slots), true
}

func (site *Site) batteryPlanMaxSoc() float64 {
	maxSoc := 100.0
	for _, dev := range site.batteryMeters {
		limiter, ok := api.Cap[api.BatterySocLimiter](dev.Instance())
		if !ok {
			continue
		}
		_, maxLimit := limiter.GetSocLimits()
		if maxLimit > 0 {
			maxSoc = min(maxSoc, maxLimit)
		}
	}
	return maxSoc
}

func (site *Site) batteryPowerLimits() (chargeW, dischargeW float64) {
	chargeW, dischargeW = 2500, 2500
	for _, dev := range site.batteryMeters {
		limiter, ok := api.Cap[api.BatteryPowerLimiter](dev.Instance())
		if !ok {
			continue
		}
		c, d := limiter.GetPowerLimits()
		if c > 0 {
			chargeW = c
		}
		if d > 0 {
			dischargeW = d
		}
	}
	return chargeW, dischargeW
}

func (site *Site) batteryPlanSlots() []planner.BatterySlot {
	var grid, solar, feedIn api.Rates
	if site.tariffs != nil {
		grid = currentRates(site.GetTariff(api.TariffUsageGrid))
		solar = currentRates(site.GetTariff(api.TariffUsageSolar))
		feedIn = currentRates(site.GetTariff(api.TariffUsageFeedIn))
	}

	var home []float64
	if site.collectors != nil && site.collectors[metrics.Home] != nil {
		if p, err := site.homeProfile(96); err == nil {
			home = p
		}
	}
	if len(home) == 0 {
		home = site.fallbackHomeProfile(96)
	}

	n := len(home)
	if len(grid) > 0 {
		n = min(n, len(grid))
	}
	n = min(n, 96)
	if n == 0 {
		return nil
	}

	now := time.Now()
	hours := tariff.SlotDuration.Hours()
	liveHomeWh := site.householdPower() * hours
	liveSolarWh := max(0, site.pvPower) * hours

	slots := make([]planner.BatterySlot, n)
	for i := range n {
		start := now.Truncate(tariff.SlotDuration).Add(time.Duration(i) * tariff.SlotDuration)
		s := planner.BatterySlot{
			Start:  start,
			End:    start.Add(tariff.SlotDuration),
			HomeWh: home[i],
		}
		if i == 0 && liveHomeWh > s.HomeWh {
			s.HomeWh = liveHomeWh
		}
		if i == 0 {
			s.SolarWh = liveSolarWh
		} else if len(solar) > 0 {
			s.SolarWh = max(0, solarEnergy(solar, s.Start, s.End))
		}
		if i < len(grid) {
			s.Price = grid[i].Value
		}
		if i < len(feedIn) {
			s.FeedIn = feedIn[i].Value
		}
		slots[i] = s
	}

	site.batteryPlanLoadWh, site.batteryPlanLoadCaps = applyLoadpointPlans(site.Loadpoints(), slots, site.GridThreshold*1000)
	return slots
}

func (site *Site) householdPower() float64 {
	var charge float64
	for _, lp := range site.Loadpoints() {
		charge += lp.GetChargePower()
	}
	return max(0, site.gridPower+max(0, site.pvPower)+site.battery.Power-charge)
}

func (site *Site) fallbackHomeProfile(minLen int) []float64 {
	hours := tariff.SlotDuration.Hours()
	wh := site.householdPower() * hours
	if wh <= 0 {
		wh = 400 * hours
	}
	res := make([]float64, minLen)
	for i := range res {
		res[i] = wh
	}
	return res
}

// applyLoadpointPlans adds planned charger/heater energy onto household slots,
// flattening under the grid limit when there is enough time before the deadline.
func applyLoadpointPlans(loadpoints []loadpoint.API, slots []planner.BatterySlot, gridThresholdW float64) (float64, []float64) {
	demands := make([]planner.ChargeDemand, len(loadpoints))
	for i, lp := range loadpoints {
		plan, powerW := loadpointChargePlan(lp)
		if powerW <= 0 || len(plan) == 0 {
			continue
		}
		var required float64
		for _, r := range plan {
			required += powerW * r.End.Sub(r.Start).Hours()
		}
		demands[i] = planner.ChargeDemand{
			RequiredWh: required,
			MaxW:       powerW,
			Deadline:   lp.EffectivePlanTime(),
			Preferred:  plan,
			Continuous: lp.EffectivePlanStrategy().Continuous,
		}
	}
	return planner.FlattenChargeDemands(slots, demands, gridThresholdW)
}

func loadpointChargePlan(lp loadpoint.API) (api.Rates, float64) {
	planTime := lp.EffectivePlanTime()
	if planTime.IsZero() {
		return nil, 0
	}

	goal, _ := lp.GetPlanGoal()
	if goal <= 0 {
		return nil, 0
	}

	maxPower := lp.EffectiveMaxPower()
	if maxPower <= 0 {
		return nil, 0
	}

	required := lp.GetPlanRequiredDuration(goal, maxPower)
	if required <= 0 {
		return nil, 0
	}

	strategy := lp.EffectivePlanStrategy()
	plan := lp.GetPlan(planTime, required, strategy.Precondition, strategy.Continuous)
	if len(plan) == 0 {
		now := time.Now()
		start := planTime.Add(-required)
		if start.Before(now) {
			start = now
		}
		if !planTime.After(start) {
			return nil, 0
		}
		plan = api.Rates{{Start: start, End: planTime}}
	}

	return plan, maxPower
}

func (site *Site) applyBatteryPlan(plan planner.BatteryPlan) {
	site.batteryPlanHold = false

	site.publish(keys.BatteryPlan, batteryPlanStatus{
		Action:         plan.Action,
		Reason:         plan.Reason,
		ChargeW:        plan.ChargeW,
		DischargeW:     plan.DischargeW,
		TargetSoc:      plan.TargetSoc,
		PeakWh:         plan.PeakWh,
		DischargeFloor: plan.DischargeFloor,
		LoadWh:         site.batteryPlanLoadWh,
		LoadW:          site.batteryPlanLoadW(),
	})

	switch plan.Action {
	case planner.BatteryActionCharge:
		site.log.DEBUG.Printf("battery plan: charge %dW (reason %s, target soc %.0f%%, peak %.0fWh)", plan.ChargeW, plan.Reason, plan.TargetSoc, plan.PeakWh)
		site.batteryPlanChargeW = plan.ChargeW
		site.batteryPlanDischargeW = 0
		site.setBatteryLimitLimits(plan.ChargeW, 0)
		site.peakShaveBatteryLimited = true

	case planner.BatteryActionDischarge:
		site.log.DEBUG.Printf("battery plan: discharge %dW (reason %s)", plan.DischargeW, plan.Reason)
		site.batteryPlanChargeW = 0
		if plan.Reason == planner.BatteryReasonExport {
			site.batteryPlanDischargeW = plan.DischargeW
		} else {
			site.batteryPlanDischargeW = 0
		}
		site.setBatteryLimitLimits(0, plan.DischargeW)
		site.peakShaveBatteryLimited = true

	case planner.BatteryActionHold:
		site.log.DEBUG.Printf("battery plan: hold (reason %s, floor %.3f)", plan.Reason, plan.DischargeFloor)
		site.batteryPlanChargeW = 0
		site.batteryPlanDischargeW = 0
		if site.peakShaveBatteryLimited {
			site.resetBatteryLimitLimits()
			site.peakShaveBatteryLimited = false
		}
		site.batteryPlanHold = true

	default:
		site.batteryPlanChargeW = 0
		site.batteryPlanDischargeW = 0
		if site.peakShaveBatteryLimited {
			site.log.DEBUG.Println("battery plan: idle, resetting battery limits")
			site.resetBatteryLimitLimits()
			site.peakShaveBatteryLimited = false
		}
	}
}

func (site *Site) batteryPlanLoadW() float64 {
	var sum float64
	for _, w := range site.batteryPlanLoadCaps {
		sum += w
	}
	return sum
}

func (site *Site) publishIdleBatteryPlan() {
	site.publish(keys.BatteryPlan, batteryPlanStatus{Action: planner.BatteryActionNormal, Reason: planner.BatteryReasonIdle})
}

func (site *Site) applyPlannedLoadpointCaps() {
	if site.peakShaveState == PeakShaveShedding || site.peakShaveState == PeakShaveLockout {
		return
	}

	v := Voltage
	if v <= 0 {
		v = site.Voltage
	}
	if v <= 0 {
		v = 230
	}

	for i, lp := range site.loadpoints {
		if lp.GetMode() == api.ModeNow {
			lp.SetPeakShaveMaxCurrent(nil)
			continue
		}
		if i >= len(site.batteryPlanLoadCaps) || site.batteryPlanLoadCaps[i] <= 0 {
			lp.SetPeakShaveMaxCurrent(nil)
			continue
		}

		phases := lp.ActivePhases()
		if phases <= 0 {
			phases = 1
		}
		cur := site.batteryPlanLoadCaps[i] / (float64(phases) * v)
		minC := lp.GetMinCurrent()
		if cur < minC {
			cur = minC
		}
		maxC := lp.GetMaxCurrent()
		if maxC > 0 && cur > maxC {
			cur = maxC
		}
		lp.SetPeakShaveMaxCurrent(&cur)
	}
}
