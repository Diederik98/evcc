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
	CoverWh        float64                 `json:"coverWh,omitempty"`
	SolarRoomWh    float64                 `json:"solarRoomWh,omitempty"`
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
	CoverWh        float64                 `json:"coverWh,omitempty"`
	SolarRoomWh    float64                 `json:"solarRoomWh,omitempty"`
	CycleCost      float64                 `json:"cycleCost"`
	Trade          bool                    `json:"trade,omitempty"`
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

type batteryPlanSlotLoad struct {
	Title string  `json:"title"`
	LoadW float64 `json:"loadW"`
}

type batteryPlanSlotStatus struct {
	Start      time.Time             `json:"start"`
	End        time.Time             `json:"end"`
	Action     string                `json:"action"`
	Reason     string                `json:"reason"`
	ChargeW    int                   `json:"chargeW,omitempty"`
	DischargeW int                   `json:"dischargeW,omitempty"`
	HomeW      float64               `json:"homeW"`
	SolarW     float64               `json:"solarW"`
	LoadW      float64               `json:"loadW,omitempty"`
	Loads      []batteryPlanSlotLoad `json:"loads,omitempty"`
	ResidualW  float64               `json:"residualW"`
	Price      float64               `json:"price,omitempty"`
	Energy     *float64              `json:"energy,omitempty"`
	HasPrice   bool                  `json:"hasPrice,omitempty"`
	FeedIn     float64               `json:"feedIn,omitempty"`
	HasFeedIn  bool                  `json:"hasFeedIn,omitempty"`
	Soc        float64               `json:"soc"`
	CoverSoc   float64               `json:"coverSoc,omitempty"`
	Peak       bool                  `json:"peak,omitempty"`
	Measured   bool                  `json:"measured,omitempty"`
}

var _ api.BytesMarshaler = (*batteryPlanStatus)(nil)

func (s batteryPlanStatus) MarshalBytes() ([]byte, error) {
	return json.Marshal(s)
}

func (site *Site) evaluateBatteryPlan() (planner.BatteryPlan, bool) {
	site.batteryPlanLoadWh = 0
	site.batteryPlanLoadCaps = nil
	site.batteryPlanSlotLoads = nil
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
		Trade:          site.BatteryTrade,
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

const (
	batteryPlanHistorySlots = 96 // 24h measured history shown before forecast
	batteryPlanMinSlots     = 96 // 24h forecast horizon
	batteryPlanMaxSlots     = 96 * 7
)

func batteryPlanSlotCount(nGrid, nSolar, nFeedIn, nHome int) int {
	n := max(batteryPlanMinSlots, nHome, nGrid, nSolar, nFeedIn)
	return min(n, batteryPlanMaxSlots)
}

func extendProfile(home []float64, n int) []float64 {
	if n <= 0 {
		return nil
	}
	if len(home) >= n {
		return home[:n]
	}
	out := make([]float64, n)
	if len(home) == 0 {
		return out
	}
	for i := range out {
		out[i] = home[i%len(home)]
	}
	return out
}

func rateAt(rr api.Rates, ts time.Time) (api.Rate, bool) {
	if len(rr) == 0 {
		return api.Rate{}, false
	}
	r, err := rr.At(ts)
	if err != nil {
		return api.Rate{}, false
	}
	return r, true
}

func rateValueAt(rr api.Rates, ts time.Time) (float64, bool) {
	r, ok := rateAt(rr, ts)
	return r.Value, ok
}

func (site *Site) batteryPlanSlots() []planner.BatterySlot {
	var grid, solar, feedIn api.Rates
	if site.tariffs != nil {
		grid = currentRates(site.GetTariff(api.TariffUsageGrid))
		solar = currentRates(site.GetTariff(api.TariffUsageSolar))
		feedIn = currentRates(site.GetTariff(api.TariffUsageFeedIn))
	}
	grid.Sort()
	solar.Sort()
	feedIn.Sort()

	n := batteryPlanSlotCount(len(grid), len(solar), len(feedIn), batteryPlanMinSlots)

	var home []float64
	site.batteryPlanHomeSource = "fallback"
	if site.collectors != nil && site.collectors[metrics.Home] != nil {
		if p, err := site.homeProfile(n); err == nil {
			home = p
			site.batteryPlanHomeSource = "weekday"
		}
	}
	if len(home) == 0 {
		home = site.fallbackHomeProfile(n)
	}
	home = extendProfile(home, n)
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
		if price, ok := rateValueAt(grid, start); ok {
			s.Price = price
		}
		if feedIn, ok := rateValueAt(feedIn, start); ok {
			s.FeedIn = feedIn
		}
		if i == 0 && liveHomeWh > s.HomeWh {
			s.HomeWh = liveHomeWh
		}
		if i == 0 {
			s.SolarWh = liveSolarWh
		} else {
			s.SolarWh = max(0, solarEnergy(solar, s.Start, s.End))
		}
		slots[i] = s
	}

	site.batteryPlanLoadWh, site.batteryPlanLoadCaps, site.batteryPlanLoads, site.batteryPlanSlotLoads = applyLoadpointPlans(site.Loadpoints(), slots, site.GridThreshold*1000)
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
func applyLoadpointPlans(loadpoints []loadpoint.API, slots []planner.BatterySlot, gridThresholdW float64) (float64, []float64, []batteryPlanLoadStatus, [][]batteryPlanSlotLoad) {
	caps := make([]float64, len(loadpoints))
	slotLoads := make([][]batteryPlanSlotLoad, len(slots))
	var loads []batteryPlanLoadStatus
	var addedWh float64

	addSlotLoad := func(slotIdx int, title string, wh float64) {
		if slotIdx < 0 || slotIdx >= len(slots) || wh <= 0 {
			return
		}
		h := slots[slotIdx].End.Sub(slots[slotIdx].Start).Hours()
		if h <= 0 {
			return
		}
		w := wh / h
		for k := range slotLoads[slotIdx] {
			if slotLoads[slotIdx][k].Title == title {
				slotLoads[slotIdx][k].LoadW += w
				return
			}
		}
		slotLoads[slotIdx] = append(slotLoads[slotIdx], batteryPlanSlotLoad{Title: title, LoadW: w})
	}

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
			for slotIdx, wh := range planner.DemandSlotWh(slots, d, gridThresholdW) {
				addSlotLoad(slotIdx, title, wh)
			}
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

	return addedWh, caps, loads, slotLoads
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
		if site.batteryChargeOnlyLocked() {
			// Cheap charging owns hold/surplus. Do not drop RS485 control back to self-consumption.
			site.peakShaveBatteryLimited = true
			break
		}
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
	history := site.batteryPlanMeasuredSlots(batteryPlanHistorySlots)
	var grid api.Rates
	if site.tariffs != nil {
		grid = currentRates(site.GetTariff(api.TariffUsageGrid))
	}
	forecast := batteryPlanSlotStatuses(site.batteryPlanForecast, site.batteryPlanSlotLoads, false, grid)
	slots := append(history, forecast...)
	site.publish(keys.BatteryPlan, batteryPlanStatus{
		Action:         plan.Action,
		Reason:         plan.Reason,
		ChargeW:        plan.ChargeW,
		DischargeW:     plan.DischargeW,
		TargetSoc:      plan.TargetSoc,
		PeakWh:         plan.PeakWh,
		CoverWh:        plan.CoverWh,
		SolarRoomWh:    plan.SolarRoomWh,
		DischargeFloor: plan.DischargeFloor,
		LoadWh:         site.batteryPlanLoadWh,
		LoadW:          site.batteryPlanLoadW(),
		Slots:          slots,
		Explain:        site.batteryPlanExplanation(plan),
		Log:            append([]batteryPlanLogEntry(nil), site.batteryPlanLog...),
	})
}

func (site *Site) batteryPlanExplanation(plan planner.BatteryPlan) *batteryPlanExplain {
	chargeW, dischargeW := site.batteryPowerLimits()
	var hasPrices, hasSolar bool
	var grid api.Rates
	if site.tariffs != nil {
		grid = currentRates(site.GetTariff(api.TariffUsageGrid))
		grid.Sort()
	}
	for _, s := range site.batteryPlanForecast {
		if price, ok := rateValueAt(grid, s.Start); ok {
			hasPrices = true
			_ = price
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
	if plan.CoverWh > 0 {
		facts = append(facts, batteryPlanFact{
			Code:   "battery.cover",
			Params: map[string]any{"kwh": plan.CoverWh / 1000, "soc": plan.TargetSoc},
		})
	}
	if plan.SolarRoomWh > 50 {
		facts = append(facts, batteryPlanFact{
			Code:   "battery.solarRoom",
			Params: map[string]any{"kwh": plan.SolarRoomWh / 1000},
		})
	}
	if site.BatteryTrade {
		facts = append(facts, batteryPlanFact{Code: "battery.tradeOn"})
	} else {
		facts = append(facts, batteryPlanFact{Code: "battery.tradeOff"})
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
	if site.PeakShaveReserveSoc > 0 {
		facts = append(facts, batteryPlanFact{
			Code:   "battery.reserveFloor",
			Params: map[string]any{"soc": site.PeakShaveReserveSoc},
		})
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
		CoverWh:        plan.CoverWh,
		SolarRoomWh:    plan.SolarRoomWh,
		CycleCost:      site.BatteryCycleCost,
		Trade:          site.BatteryTrade,
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

func batteryPlanSlotStatuses(slots []planner.BatteryHorizonSlot, slotLoads [][]batteryPlanSlotLoad, measured bool, grid api.Rates) []batteryPlanSlotStatus {
	if len(slots) == 0 {
		return nil
	}
	out := make([]batteryPlanSlotStatus, len(slots))
	for i, s := range slots {
		st := batteryPlanSlotStatus{
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
			Soc:        s.Soc,
			CoverSoc:   s.CoverSoc,
			Peak:       s.Peak,
			Measured:   measured,
		}
		if r, ok := rateAt(grid, s.Start); ok {
			st.Price = r.Value
			st.Energy = api.CloneEnergy(r.Energy)
			st.HasPrice = true
		}
		if s.FeedIn > 0 {
			st.FeedIn = s.FeedIn
			st.HasFeedIn = true
		}
		if !measured && i < len(slotLoads) {
			st.Loads = slotLoads[i]
		}
		out[i] = st
	}
	return out
}

func (site *Site) batteryPlanMeasuredSlots(n int) []batteryPlanSlotStatus {
	if n <= 0 {
		return nil
	}
	now := time.Now().Truncate(tariff.SlotDuration)
	from := now.Add(-time.Duration(n) * tariff.SlotDuration)

	homeByStart := map[int64]float64{}
	pvByStart := map[int64]float64{}
	socByStart := map[int64]float64{}

	if series, err := metrics.QueryEnergy(from, now, "15m", false, metrics.EnergyFilter{Group: metrics.Home}); err == nil {
		for _, s := range series {
			for _, slot := range s.Data {
				homeByStart[slot.Start.Unix()] = slot.Energy * 1e3 / tariff.SlotDuration.Hours()
			}
		}
	}
	if series, err := metrics.QueryEnergy(from, now, "15m", false, metrics.EnergyFilter{Group: metrics.PV}); err == nil {
		for _, s := range series {
			for _, slot := range s.Data {
				pvByStart[slot.Start.Unix()] = slot.Energy * 1e3 / tariff.SlotDuration.Hours()
			}
		}
	}
	if series, err := metrics.QueryEnergy(from, now, "15m", false, metrics.EnergyFilter{Group: metrics.Battery}); err == nil {
		for _, s := range series {
			for _, slot := range s.Data {
				if slot.SocTemp != nil {
					socByStart[slot.Start.Unix()] = *slot.SocTemp
				}
			}
		}
	}

	var grid api.Rates
	if site.tariffs != nil {
		grid = currentRates(site.GetTariff(api.TariffUsageGrid))
		grid.Sort()
	}

	out := make([]batteryPlanSlotStatus, 0, n)
	for i := range n {
		start := from.Add(time.Duration(i) * tariff.SlotDuration)
		end := start.Add(tariff.SlotDuration)
		if !start.Before(now) {
			break
		}
		st := batteryPlanSlotStatus{
			Start:    start,
			End:      end,
			Action:   "normal",
			Reason:   "idle",
			HomeW:    homeByStart[start.Unix()],
			SolarW:   pvByStart[start.Unix()],
			Soc:      socByStart[start.Unix()],
			Measured: true,
		}
		if st.Soc == 0 && site.batteryConfigured() {
			st.Soc = site.battery.Soc
		}
		if r, ok := rateAt(grid, start); ok {
			st.Price = r.Value
			st.Energy = api.CloneEnergy(r.Energy)
			st.HasPrice = true
		}
		out = append(out, st)
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
