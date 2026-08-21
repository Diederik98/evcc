package core

import (
	"encoding/json"
	"fmt"
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
	Action         string                  `json:"action"`
	Reason         string                  `json:"reason"`
	ChargeW        int                     `json:"chargeW"`
	DischargeW     int                     `json:"dischargeW"`
	TargetSoc      float64                 `json:"targetSoc"`
	PeakWh         float64                 `json:"peakWh"`
	DischargeFloor float64                 `json:"dischargeFloor"`
	LoadWh         float64                 `json:"loadWh,omitempty"`
	LoadW          float64                 `json:"loadW,omitempty"`
	Slots          []batteryPlanSlotStatus `json:"slots,omitempty"`
	Explain        *batteryPlanExplain     `json:"explain,omitempty"`
	Log            []batteryPlanLogEntry   `json:"log,omitempty"`
}

type batteryPlanExplain struct {
	Generated      time.Time               `json:"generated"`
	HomeSource     string                  `json:"homeSource"`
	HasPrices      bool                    `json:"hasPrices"`
	HasSolar       bool                    `json:"hasSolar"`
	GridThresholdW float64                 `json:"gridThresholdW"`
	LiveResidualW  float64                 `json:"liveResidualW"`
	CapacityWh     float64                 `json:"capacityWh"`
	Soc            float64                 `json:"soc"`
	MinSoc         float64                 `json:"minSoc"`
	ReserveSoc     float64                 `json:"reserveSoc"`
	MaxSoc         float64                 `json:"maxSoc"`
	TargetSoc      float64                 `json:"targetSoc"`
	PeakWh         float64                 `json:"peakWh"`
	CycleCost      float64                 `json:"cycleCost"`
	ChargeW        float64                 `json:"chargeW"`
	DischargeW     float64                 `json:"dischargeW"`
	Facts          []batteryPlanFact       `json:"facts,omitempty"`
	Loads          []batteryPlanLoadStatus `json:"loads,omitempty"`
}

type batteryPlanFact struct {
	Code   string         `json:"code"`
	Params map[string]any `json:"params,omitempty"`
}

type batteryPlanLoadStatus struct {
	Title     string    `json:"title"`
	Heating   bool      `json:"heating,omitempty"`
	Estimated bool      `json:"estimated,omitempty"`
	EnergyWh  float64   `json:"energyWh"`
	PowerW    float64   `json:"powerW"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Deadline  time.Time `json:"deadline"`
	Mode      string    `json:"mode,omitempty"`
	Pattern   string    `json:"pattern,omitempty"`
}

type batteryPlanLogEntry struct {
	Time   time.Time `json:"time"`
	Code   string    `json:"code"`
	Detail string    `json:"detail,omitempty"`
}

type batteryPlanSlotStatus struct {
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Action     string    `json:"action"`
	Reason     string    `json:"reason"`
	ChargeW    int       `json:"chargeW,omitempty"`
	DischargeW int       `json:"dischargeW,omitempty"`
	HomeW      float64   `json:"homeW"`
	SolarW     float64   `json:"solarW"`
	LoadW      float64   `json:"loadW,omitempty"`
	ResidualW  float64   `json:"residualW"`
	Price      float64   `json:"price,omitempty"`
	FeedIn     float64   `json:"feedIn,omitempty"`
	Soc        float64   `json:"soc"`
	Peak       bool      `json:"peak,omitempty"`
}

var _ api.BytesMarshaler = (*batteryPlanStatus)(nil)

func (s batteryPlanStatus) MarshalBytes() ([]byte, error) {
	return json.Marshal(s)
}

func (site *Site) evaluateBatteryPlan() (planner.BatteryPlan, bool) {
	site.batteryPlanLoadWh = 0
	site.batteryPlanLoadCaps = nil
	site.batteryPlanForecast = nil
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

	plan, slotsOut := planner.PlanBatteryHorizon(cfg, slots)
	site.batteryPlanForecast = slotsOut
	return plan, true
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
	site.batteryPlanHomeSource = "fallback"
	if site.collectors != nil && site.collectors[metrics.Home] != nil {
		if p, err := site.homeProfile(96); err == nil {
			home = p
			site.batteryPlanHomeSource = "weekday"
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

	site.batteryPlanLoadWh, site.batteryPlanLoadCaps, site.batteryPlanLoads = applyLoadpointPlans(site.Loadpoints(), slots, site.GridThreshold*1000)
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
func applyLoadpointPlans(loadpoints []loadpoint.API, slots []planner.BatterySlot, gridThresholdW float64) (float64, []float64, []batteryPlanLoadStatus) {
	caps := make([]float64, len(loadpoints))
	var loads []batteryPlanLoadStatus
	var addedWh float64

	for i, lp := range loadpoints {
		var demands []planner.ChargeDemand
		if hd, ok := lp.(interface {
			HorizonChargeDemands([]planner.BatterySlot) []planner.ChargeDemand
		}); ok {
			demands = hd.HorizonChargeDemands(slots)
		} else {
			plan, powerW := loadpointChargePlan(lp)
			if powerW > 0 && len(plan) > 0 {
				var required float64
				for _, r := range plan {
					required += powerW * r.End.Sub(r.Start).Hours()
				}
				demands = append(demands, planner.ChargeDemand{
					RequiredWh: required,
					MaxW:       powerW,
					Deadline:   lp.EffectivePlanTime(),
					Preferred:  plan,
					Continuous: lp.EffectivePlanStrategy().Continuous,
				})
			}
		}
		if len(demands) == 0 {
			continue
		}

		wh, cur := planner.FlattenChargeDemands(slots, demands, gridThresholdW)
		addedWh += wh
		for _, w := range cur {
			caps[i] += w
		}

		heating := false
		estimated := !lp.HasChargeMeter()
		pattern := "assumed"
		title := lp.GetTitle()
		if concrete, ok := lp.(*Loadpoint); ok {
			heating = concrete.chargerHasFeature(api.Heating)
			if heating && !concrete.HasChargeMeter() {
				estimated = true
				if _, learned := concrete.heatingPattern.MatchBand(concrete.vehicleSoc); learned {
					pattern = "learned"
				} else {
					pattern = "estimated"
				}
			}
		}
		for _, d := range demands {
			start, end := planner.Start(d.Preferred), planner.End(d.Preferred)
			mode := "flex"
			if d.Continuous {
				mode = "continuous"
			}
			loads = append(loads, batteryPlanLoadStatus{
				Title:     title,
				Heating:   heating,
				Estimated: estimated,
				EnergyWh:  d.RequiredWh,
				PowerW:    d.MaxW,
				Start:     start,
				End:       end,
				Deadline:  d.Deadline,
				Mode:      mode,
				Pattern:   pattern,
			})
		}
	}

	return addedWh, caps, loads
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
		site.batteryPlanHold = true
		if site.hasBatteryLimitController() {
			// live loop absorbs surplus; do not return to self-consumption
			site.peakShaveBatteryLimited = true
			break
		}
		if site.peakShaveBatteryLimited {
			site.resetBatteryLimitLimits()
			site.peakShaveBatteryLimited = false
		}

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

func (site *Site) publishBatteryPlan(plan planner.BatteryPlan) {
	site.recordBatteryPlanLog(plan)
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
		Slots:          batteryPlanSlotStatuses(site.batteryPlanForecast),
		Explain:        site.batteryPlanExplanation(plan),
		Log:            append([]batteryPlanLogEntry(nil), site.batteryPlanLog...),
	})
}

func (site *Site) batteryPlanExplanation(plan planner.BatteryPlan) *batteryPlanExplain {
	chargeW, dischargeW := site.batteryPowerLimits()
	var hasPrices, hasSolar bool
	for _, s := range site.batteryPlanForecast {
		if s.Price > 0 {
			hasPrices = true
		}
		if s.SolarW > 0 {
			hasSolar = true
		}
	}

	facts := []batteryPlanFact{
		{Code: "home." + site.batteryPlanHomeSource},
	}
	if hasPrices {
		facts = append(facts, batteryPlanFact{Code: "prices.yes"})
	} else {
		facts = append(facts, batteryPlanFact{Code: "prices.no"})
	}
	if hasSolar {
		facts = append(facts, batteryPlanFact{Code: "solar.yes"})
	} else {
		facts = append(facts, batteryPlanFact{Code: "solar.no"})
	}
	if site.GridThreshold > 0 {
		facts = append(facts, batteryPlanFact{
			Code:   "limit",
			Params: map[string]any{"kw": site.GridThreshold},
		})
	}
	if plan.PeakWh > 0 {
		facts = append(facts, batteryPlanFact{
			Code:   "peak",
			Params: map[string]any{"kwh": plan.PeakWh / 1000, "soc": plan.TargetSoc},
		})
	}
	facts = append(facts, batteryPlanFact{
		Code:   "action." + plan.Action,
		Params: map[string]any{"reason": plan.Reason, "chargeW": plan.ChargeW, "dischargeW": plan.DischargeW},
	})
	if plan.Reason == planner.BatteryReasonPeak {
		facts = append(facts, batteryPlanFact{Code: "battery.coverPeak"})
	}
	if plan.Reason == planner.BatteryReasonHold {
		facts = append(facts, batteryPlanFact{Code: "battery.holdForLater"})
	}
	if plan.Reason == planner.BatteryReasonCheap {
		facts = append(facts, batteryPlanFact{Code: "battery.cheapCycle"})
	}
	for _, load := range site.batteryPlanLoads {
		code := "load.charger"
		if load.Heating {
			code = "load.heat"
		}
		facts = append(facts, batteryPlanFact{
			Code: code,
			Params: map[string]any{
				"title":     load.Title,
				"kwh":       load.EnergyWh / 1000,
				"w":         load.PowerW,
				"estimated": load.Estimated,
				"pattern":   load.Pattern,
			},
		})
	}

	return &batteryPlanExplain{
		Generated:      time.Now(),
		HomeSource:     site.batteryPlanHomeSource,
		HasPrices:      hasPrices,
		HasSolar:       hasSolar,
		GridThresholdW: site.GridThreshold * 1000,
		LiveResidualW:  max(0, site.gridPower-site.battery.Power),
		CapacityWh:     site.battery.Capacity * 1e3,
		Soc:            site.battery.Soc,
		MinSoc:         site.peakShaveEffectiveMinSoc(),
		ReserveSoc:     site.PeakShaveReserveSoc,
		MaxSoc:         site.batteryPlanMaxSoc(),
		TargetSoc:      plan.TargetSoc,
		PeakWh:         plan.PeakWh,
		CycleCost:      site.BatteryCycleCost,
		ChargeW:        chargeW,
		DischargeW:     dischargeW,
		Facts:          facts,
		Loads:          site.batteryPlanLoads,
	}
}

func (site *Site) recordBatteryPlanLog(plan planner.BatteryPlan) {
	fp := fmt.Sprintf("%s:%s:%d:%d:%.0f", plan.Action, plan.Reason, plan.ChargeW, plan.DischargeW, site.batteryPlanLoadWh)
	for _, s := range site.batteryPlanForecast {
		if len(fp) > 180 {
			break
		}
		if s.Action != "" {
			fp += s.Action[:1]
		}
		if s.Peak {
			fp += "P"
		}
	}
	for _, l := range site.batteryPlanLoads {
		fp += fmt.Sprintf("/%s-%s", l.Start.Format("1504"), l.End.Format("1504"))
	}
	if fp == site.batteryPlanFingerprint {
		return
	}

	code := "replan"
	switch {
	case site.batteryPlanFingerprint == "":
		code = "created"
	case plan.Reason == planner.BatteryReasonPeak:
		code = "peak"
	case len(site.batteryPlanLoads) > 0:
		code = "load"
	}
	site.batteryPlanFingerprint = fp
	site.batteryPlanLog = append(site.batteryPlanLog, batteryPlanLogEntry{
		Time:   time.Now(),
		Code:   code,
		Detail: fmt.Sprintf("%s/%s", plan.Action, plan.Reason),
	})
	if len(site.batteryPlanLog) > 30 {
		site.batteryPlanLog = site.batteryPlanLog[len(site.batteryPlanLog)-30:]
	}
}

func batteryPlanSlotStatuses(slots []planner.BatteryHorizonSlot) []batteryPlanSlotStatus {
	if len(slots) == 0 {
		return nil
	}
	out := make([]batteryPlanSlotStatus, len(slots))
	for i, s := range slots {
		out[i] = batteryPlanSlotStatus{
			Start:      s.Start,
			End:        s.End,
			Action:     s.Action,
			Reason:     s.Reason,
			ChargeW:    s.ChargeW,
			DischargeW: s.DischargeW,
			HomeW:      s.HomeW,
			SolarW:     s.SolarW,
			LoadW:      s.LoadW,
			ResidualW:  s.ResidualW,
			Price:      s.Price,
			FeedIn:     s.FeedIn,
			Soc:        s.Soc,
			Peak:       s.Peak,
		}
	}
	return out
}

func (site *Site) publishIdleBatteryPlan() {
	site.batteryPlanForecast = nil
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
