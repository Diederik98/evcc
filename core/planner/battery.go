package planner

import (
	"math"
	"slices"
	"time"

	"github.com/evcc-io/evcc/tariff"
)

const (
	BatteryActionNormal    = "normal"
	BatteryActionCharge    = "charge"
	BatteryActionHold      = "hold"
	BatteryActionDischarge = "discharge"

	BatteryReasonIdle   = "idle"
	BatteryReasonPeak   = "peak"
	BatteryReasonCharge = "charge"
	BatteryReasonCheap  = "cheap"
	BatteryReasonHold   = "hold"
	BatteryReasonExport = "export"

	batteryEta            = 0.9
	defaultBatteryChargeW = 2500.0
	coverMargin           = 1.15 // charge a little more than predicted expensive need
	peakClusterMergeSlots = 2
)

// BatterySlot is one planning interval with tax-inclusive prices.
type BatterySlot struct {
	Start      time.Time
	End        time.Time
	HomeWh     float64 // house + all planned loads (peak residual)
	SolarWh    float64
	LoadWh     float64 // planned charger/heater energy included in HomeWh
	GridOnlyWh float64 // planned load the battery must not serve (subset of LoadWh)
	Price      float64 // grid price including charges and tax, per kWh
	FeedIn     float64
}

// BatteryConfig describes the battery and grid constraints for PlanBattery.
type BatteryConfig struct {
	Soc, MinSoc, MaxSoc, ReserveSoc float64
	CapacityWh                      float64
	ChargeW, DischargeW             float64
	EtaC, EtaD                      float64
	CycleCost                       float64 // €/kWh discharged (wear)
	Trade                           bool    // after cover, fill toward max and sell when feed-in beats later self-use
	GridThresholdW                  float64
	HeadroomW                       float64 // remaining import headroom for grid charging
	LiveResidualW                   float64 // grid import if the battery were idle
}

// BatteryPlan is the action for the current slot.
type BatteryPlan struct {
	Action         string
	Reason         string
	ChargeW        int
	DischargeW     int
	TargetSoc      float64
	PeakWh         float64
	CoverWh        float64
	SolarRoomWh    float64
	DischargeFloor float64
	ChargeCeiling  float64
}

// BatteryHorizonSlot is the planned action and forecasts for one interval.
type BatteryHorizonSlot struct {
	Start      time.Time
	End        time.Time
	Action     string
	Reason     string
	ChargeW    int
	DischargeW int
	HomeW      float64
	SolarW     float64
	LoadW      float64
	ResidualW  float64
	Price      float64
	FeedIn     float64
	Soc        float64
	CoverSoc   float64
	Peak       bool
}

type batteryNeed struct {
	coverWh     float64
	solarRoomWh float64
	solarAllWh  float64
	unmetACWh   float64
	unmetCost   float64
	firstNeed   int
}

// PlanBattery decides charge, hold, discharge or export for the current slot.
// Prices must already include taxes and levies. Peak shaving is the hard
// constraint: never grid-charge into a peak, and peak discharge stops at
// reserve. Cover is reserve plus later expensive battery-served import after
// solar, plus a small margin. Grid charging happens when the tax-inclusive
// spread covers round-trip losses and cycle cost. Hold only applies in cheap
// windows so stored energy is not dumped; expensive windows consume cover.
// With Trade, leftover above cover may be filled or sold when feed-in beats
// later self-use.
func PlanBattery(cfg BatteryConfig, slots []BatterySlot) BatteryPlan {
	if cfg.CapacityWh <= 0 || len(slots) == 0 {
		return BatteryPlan{Action: BatteryActionNormal, Reason: BatteryReasonIdle}
	}

	cfg = withBatteryDefaults(cfg)

	minWh := minEnergyWh(cfg)
	maxWh := cfg.CapacityWh * cfg.MaxSoc / 100
	floorWh := reserveEnergyWh(cfg)
	socWh := clamp(cfg.CapacityWh*cfg.Soc/100, minWh, maxWh)

	prices := slotPrices(slots)
	dischargeFloor, chargeCeiling := economicBands(prices, cfg.EtaC, cfg.EtaD, cfg.CycleCost)
	need := planNeed(cfg, slots, socWh, maxWh, floorWh, cheapestPrice(prices))

	targetWh := min(need.coverWh, maxWh-need.solarRoomWh)
	if targetWh < floorWh {
		targetWh = floorWh
	}

	plan := BatteryPlan{
		Action:         BatteryActionNormal,
		Reason:         BatteryReasonIdle,
		TargetSoc:      100 * targetWh / cfg.CapacityWh,
		PeakWh:         peakEnergyWh(cfg, slots),
		CoverWh:        targetWh,
		SolarRoomWh:    need.solarRoomWh,
		DischargeFloor: dischargeFloor,
		ChargeCeiling:  chargeCeiling,
	}

	cur := slots[0]
	hours := slotHours(cur)
	if hours <= 0 {
		return plan
	}

	profileResidualW := residualW(cur, hours)
	residualW := max(profileResidualW, cfg.LiveResidualW)
	overshootW := max(0, residualW-cfg.GridThresholdW)
	inPeak := cfg.GridThresholdW > 0 && overshootW > 0

	if inPeak && socWh > floorWh+1 {
		discharge := min(overshootW, cfg.DischargeW, (socWh-floorWh)/hours*cfg.EtaD)
		if discharge > 0 {
			plan.Action = BatteryActionDischarge
			plan.DischargeW = int(math.Round(discharge))
			plan.Reason = BatteryReasonPeak
			return plan
		}
	}

	if !inPeak {
		if p, ok := gridChargePlan(cfg, slots, socWh, targetWh, need, hours, maxWh); ok {
			p.TargetSoc = plan.TargetSoc
			p.PeakWh = plan.PeakWh
			p.CoverWh = plan.CoverWh
			p.SolarRoomWh = plan.SolarRoomWh
			p.DischargeFloor = plan.DischargeFloor
			p.ChargeCeiling = plan.ChargeCeiling
			return p
		}
	}

	if cfg.Trade {
		if w := exportExcessW(cfg, slots, socWh, targetWh); w > 0 {
			plan.Action = BatteryActionDischarge
			plan.DischargeW = int(math.Round(w))
			plan.Reason = BatteryReasonExport
			return plan
		}
	}

	// Hold only while this slot is cheap and a later hour is more expensive.
	// Expensive slots stay idle so the house can consume cover.
	if dischargeFloor > 0 && cur.Price > 0 && cur.Price < dischargeFloor && socWh > floorWh+1 && laterHigherPrice(slots, cur.Price) {
		plan.Action = BatteryActionHold
		plan.Reason = BatteryReasonHold
		return plan
	}

	return plan
}

func withBatteryDefaults(cfg BatteryConfig) BatteryConfig {
	if cfg.EtaC <= 0 {
		cfg.EtaC = batteryEta
	}
	if cfg.EtaD <= 0 {
		cfg.EtaD = batteryEta
	}
	if cfg.ChargeW <= 0 {
		cfg.ChargeW = defaultBatteryChargeW
	}
	if cfg.DischargeW <= 0 {
		cfg.DischargeW = defaultBatteryChargeW
	}
	if cfg.MaxSoc <= 0 {
		cfg.MaxSoc = 100
	}
	return cfg
}

func gridChargePlan(cfg BatteryConfig, slots []BatterySlot, socWh, coverWh float64, need batteryNeed, hours, maxWh float64) (BatteryPlan, bool) {
	cur := slots[0]

	tryCharge := func(targetWh float64, reason string) (BatteryPlan, bool) {
		deficit := targetWh - socWh
		if deficit <= 1 {
			return BatteryPlan{}, false
		}
		charge := min(cfg.ChargeW, cfg.HeadroomW, deficit/hours/cfg.EtaC)
		if charge <= 0 {
			return BatteryPlan{}, false
		}
		return BatteryPlan{Action: BatteryActionCharge, Reason: reason, ChargeW: int(math.Round(charge))}, true
	}

	coverCap := min(coverWh, maxWh-need.solarRoomWh)
	if socWh < coverCap-1 && need.unmetACWh > 1 {
		avoided := need.unmetCost / (need.unmetACWh / 1000)
		if gridChargePays(cur.Price, avoided, cfg.EtaC, cfg.EtaD, cfg.CycleCost) {
			must, cheap := chargeByDeadline(cfg, slots, socWh, coverCap, need.firstNeed, true)
			if must || cheap || !laterCheaperCharge(slots, need.firstNeed, cur.Price) {
				reason := BatteryReasonCheap
				if must {
					reason = BatteryReasonCharge
				}
				if p, ok := tryCharge(coverCap, reason); ok {
					return p, true
				}
			}
		}
	}

	if cfg.Trade {
		tradeCap := min(maxWh, maxWh-need.solarAllWh)
		if socWh >= coverWh-1 && socWh < tradeCap-1 {
			p75 := laterImportP75(slots)
			if gridChargePays(cur.Price, p75, cfg.EtaC, cfg.EtaD, cfg.CycleCost) {
				must, cheap := chargeByDeadline(cfg, slots, socWh, tradeCap, -1, true)
				if must || cheap || !laterCheaperCharge(slots, -1, cur.Price) {
					return tryCharge(tradeCap, BatteryReasonCheap)
				}
			}
		}
	}

	return BatteryPlan{}, false
}

func laterHigherPrice(slots []BatterySlot, priceNow float64) bool {
	if priceNow <= 0 {
		return false
	}
	for _, s := range slots[1:] {
		if s.Price > priceNow*1.05 {
			return true
		}
	}
	return false
}

func laterCheaperCharge(slots []BatterySlot, until int, priceNow float64) bool {
	if priceNow <= 0 {
		return false
	}
	end := len(slots)
	if until >= 0 {
		end = min(end, until)
	}
	for i := 1; i < end; i++ {
		if slots[i].Price > 0 && slots[i].Price < priceNow*0.95 {
			return true
		}
	}
	return false
}

func slotHours(s BatterySlot) float64 {
	d := s.End.Sub(s.Start)
	if d <= 0 {
		d = tariff.SlotDuration
	}
	return d.Hours()
}

func residualW(s BatterySlot, hours float64) float64 {
	if hours <= 0 {
		return 0
	}
	return max(0, (s.HomeWh-s.SolarWh)/hours)
}

// netPowerW is house minus solar in watts. Negative means PV surplus available
// to charge the battery.
func netPowerW(s BatterySlot, hours float64) float64 {
	if hours <= 0 {
		return 0
	}
	return (s.HomeWh - s.SolarWh) / hours
}

func thresholdWh(cfg BatteryConfig, hours float64) float64 {
	return max(0, cfg.GridThresholdW) * hours
}

func slotOvershootWh(cfg BatteryConfig, s BatterySlot) float64 {
	h := slotHours(s)
	return max(0, max(0, s.HomeWh-s.SolarWh)-thresholdWh(cfg, h))
}

func servedHomeWh(s BatterySlot) float64 {
	return max(0, s.HomeWh-s.GridOnlyWh)
}

func servedResidualAC(s BatterySlot) float64 {
	return max(0, servedHomeWh(s)-s.SolarWh)
}

func solarSurplusAC(s BatterySlot) float64 {
	return max(0, s.SolarWh-servedHomeWh(s))
}

func minEnergyWh(cfg BatteryConfig) float64 {
	return cfg.CapacityWh * cfg.MinSoc / 100
}

// reserveEnergyWh is the planner discharge floor. Peak shaving never plans
// below reserve; min SoC is only used by the live last-resort loop.
func reserveEnergyWh(cfg BatteryConfig) float64 {
	minWh := minEnergyWh(cfg)
	reserveWh := cfg.CapacityWh * cfg.ReserveSoc / 100
	if reserveWh > minWh {
		return reserveWh
	}
	return minWh
}

func peakEnergyWh(cfg BatteryConfig, slots []BatterySlot) float64 {
	var sum float64
	for _, s := range slots {
		sum += slotOvershootWh(cfg, s)
	}
	if cfg.EtaD > 0 {
		return sum / cfg.EtaD
	}
	return sum
}

func firstPeakIndex(cfg BatteryConfig, slots []BatterySlot) int {
	for i, s := range slots {
		if slotOvershootWh(cfg, s) > 0 {
			return i
		}
	}
	return -1
}

func chargeByDeadline(cfg BatteryConfig, slots []BatterySlot, socWh, targetWh float64, deadline int, peakLockout bool) (must, cheap bool) {
	deficit := targetWh - socWh
	if deficit <= 0 {
		return false, false
	}

	firstPeak := -1
	if cfg.GridThresholdW > 0 {
		firstPeak = firstPeakIndex(cfg, slots)
	}
	if peakLockout && firstPeak >= 0 && firstPeak < peakClusterMergeSlots {
		return false, false
	}

	until := len(slots)
	if deadline >= 0 {
		until = min(until, deadline)
	}
	if peakLockout && firstPeak >= 0 {
		until = min(until, firstPeak)
	}
	if until <= 0 {
		return true, true
	}

	var laterWh float64
	var prices []float64
	for i := 1; i < until; i++ {
		h := slotHours(slots[i])
		laterWh += cfg.ChargeW * h * cfg.EtaC
		if slots[i].Price > 0 {
			prices = append(prices, slots[i].Price)
		}
	}
	if socWh+laterWh < targetWh {
		must = true
	}

	if slots[0].Price <= 0 {
		return must, must
	}
	if len(prices) == 0 {
		return must, true
	}

	slices.Sort(prices)
	idx := max(0, len(prices)/2-1)
	cheap = slots[0].Price <= prices[idx]*1.05
	return must, cheap
}

// planNeed walks future slots. PV surplus fills first. Peak quarters are left
// to live shaving. Only later under-limit import that would pay after round-trip
// and cycle cost, relative to the cheapest horizon price, counts as cover.
func planNeed(cfg BatteryConfig, slots []BatterySlot, socWh, maxWh, floorWh, cheapRef float64) batteryNeed {
	out := batteryNeed{firstNeed: -1, coverWh: floorWh}
	if len(slots) < 2 || cfg.EtaD <= 0 {
		return out
	}

	desired := coverWalk(cfg, slots, floorWh, maxWh, floorWh, cheapRef, true)
	fromSoc := coverWalk(cfg, slots, socWh, maxWh, floorWh, cheapRef, true)
	out.solarRoomWh = fromSoc.solarRoomWh
	out.solarAllWh = max(desired.solarAllWh, fromSoc.solarAllWh)
	out.unmetACWh = fromSoc.unmetACWh
	out.unmetCost = fromSoc.unmetCost
	out.firstNeed = fromSoc.firstNeed
	if fromSoc.firstNeed < 0 {
		out.firstNeed = desired.firstNeed
	}
	out.coverWh = clamp(floorWh+desired.unmetDC*coverMargin, floorWh, maxWh)
	return out
}

type coverWalkResult struct {
	solarRoomWh, solarAllWh, unmetACWh, unmetCost, unmetDC float64
	firstNeed                                              int
}

func coverWalk(cfg BatteryConfig, slots []BatterySlot, startWh, maxWh, floorWh, cheapRef float64, trackSolarRoom bool) coverWalkResult {
	out := coverWalkResult{firstNeed: -1}
	soc := clamp(startWh, floorWh, maxWh)
	seenNeed := false
	etaC, etaD := cfg.EtaC, cfg.EtaD

	for i := 1; i < len(slots); i++ {
		s := slots[i]
		if surplusAC := solarSurplusAC(s); surplusAC > 0 && etaC > 0 {
			stored := min(maxWh-soc, surplusAC*etaC)
			if stored > 0 {
				soc += stored
				out.solarAllWh += stored
				if trackSolarRoom && !seenNeed {
					out.solarRoomWh += stored
				}
			}
		}

		if cfg.GridThresholdW > 0 && slotOvershootWh(cfg, s) > 0 {
			continue
		}

		servedAC := servedResidualAC(s)
		if cfg.GridThresholdW > 0 {
			servedAC = min(servedAC, cfg.GridThresholdW*slotHours(s))
		}
		if servedAC <= 1 {
			continue
		}
		if !slotIsExpensive(s.Price, cheapRef, cfg.EtaC, cfg.EtaD, cfg.CycleCost) {
			continue
		}

		needDC := servedAC / etaD
		take := min(max(0, soc-floorWh), needDC)
		soc -= take
		unmet := needDC - take
		if unmet <= 1 {
			continue
		}
		seenNeed = true
		out.unmetDC += unmet
		leftoverAC := unmet * etaD
		out.unmetACWh += leftoverAC
		if s.Price > 0 {
			out.unmetCost += leftoverAC / 1000 * s.Price
		}
		if out.firstNeed < 0 {
			out.firstNeed = i
		}
	}
	return out
}

func cheapestPrice(prices []float64) float64 {
	if len(prices) == 0 {
		return 0
	}
	minP := prices[0]
	for _, p := range prices[1:] {
		if p > 0 && (minP <= 0 || p < minP) {
			minP = p
		}
	}
	return minP
}

func slotIsExpensive(price, cheapRef, etaC, etaD, cycleCost float64) bool {
	if price <= 0 {
		return false
	}
	if cheapRef <= 0 {
		return gridChargePays(0, price, etaC, etaD, cycleCost)
	}
	if price <= cheapRef*1.02 {
		return false
	}
	return gridChargePays(cheapRef, price, etaC, etaD, cycleCost)
}

func gridChargePays(priceNow, avoided, etaC, etaD, cycleCost float64) bool {
	if avoided <= 0 {
		return false
	}
	rt := etaC * etaD
	if rt <= 0 {
		return false
	}
	if priceNow <= 0 {
		return true
	}
	return priceNow/rt+cycleCost < avoided
}

func laterImportP75(slots []BatterySlot) float64 {
	var prices []float64
	for _, s := range slots[1:] {
		if s.Price > 0 {
			prices = append(prices, s.Price)
		}
	}
	if len(prices) == 0 {
		return 0
	}
	slices.Sort(prices)
	return percentile(prices, 0.75)
}

// exportExcessW is AC discharge for selling leftover energy above cover.
// Energy reserved for later expensive self-use is never sold. If a later hour
// pays more, that hour is filled first and only overflow is sold now. Sell is
// an explicit trade: feed-in minus cycle cost must beat the € of keeping that
// slice for later self-consumption.
func exportExcessW(cfg BatteryConfig, slots []BatterySlot, socWh, targetWh float64) float64 {
	excessWh := socWh - targetWh
	if excessWh <= 1 {
		return 0
	}

	if cfg.GridThresholdW > 0 {
		firstPeak := firstPeakIndex(cfg, slots)
		if firstPeak >= 0 && firstPeak < peakClusterMergeSlots {
			return 0
		}
	}

	cur := slots[0]
	if cur.FeedIn <= 0 || cur.FeedIn-cfg.CycleCost <= 0 {
		return 0
	}

	hours := slotHours(cur)
	if hours <= 0 || cfg.EtaD <= 0 {
		return 0
	}

	var laterWh float64
	for _, s := range slots[1:] {
		if s.FeedIn > cur.FeedIn && s.FeedIn-cfg.CycleCost > 0 {
			laterWh += cfg.DischargeW * slotHours(s) / cfg.EtaD
		}
	}
	sellWh := excessWh - min(excessWh, laterWh)
	if sellWh <= 1 {
		return 0
	}

	if keep := selfConsumptionValueEur(cfg, slots, sellWh); keep > 0 {
		sell := sellWh / 1000 * cfg.EtaD * (cur.FeedIn - cfg.CycleCost)
		if sell <= keep {
			return 0
		}
	}

	return min(cfg.DischargeW, sellWh/hours*cfg.EtaD)
}

// selfConsumptionValueEur is the € value of keeping excessWh for later house
// load that would otherwise import at tax-inclusive Price.
func selfConsumptionValueEur(cfg BatteryConfig, slots []BatterySlot, excessWh float64) float64 {
	remaining := excessWh * cfg.EtaD
	var euros float64
	for _, s := range slots[1:] {
		if remaining <= 1 || s.Price <= 0 {
			continue
		}
		h := slotHours(s)
		residual := servedResidualAC(s)
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

func slotPrices(slots []BatterySlot) []float64 {
	var prices []float64
	for _, s := range slots {
		if s.Price > 0 {
			prices = append(prices, s.Price)
		}
	}
	return prices
}

func economicBands(prices []float64, etaC, etaD, cycleCost float64) (dischargeFloor, chargeCeiling float64) {
	if len(prices) < 2 {
		return 0, 0
	}
	sorted := slices.Clone(prices)
	slices.Sort(sorted)
	low := percentile(sorted, 0.25)
	high := percentile(sorted, 0.75)
	rt := etaC * etaD
	if rt <= 0 {
		return 0, 0
	}
	dischargeFloor = low/rt + cycleCost
	chargeCeiling = high*rt - cycleCost
	if chargeCeiling < 0 {
		chargeCeiling = 0
	}
	return dischargeFloor, chargeCeiling
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Round(p * float64(len(sorted)-1)))
	idx = max(0, min(len(sorted)-1, idx))
	return sorted[idx]
}

func clamp(v, lo, hi float64) float64 {
	return min(hi, max(lo, v))
}

// PlanBatteryHorizon returns the current-slot plan and a simulated schedule.
func PlanBatteryHorizon(cfg BatteryConfig, slots []BatterySlot) (BatteryPlan, []BatteryHorizonSlot) {
	if cfg.CapacityWh <= 0 || len(slots) == 0 {
		return PlanBattery(cfg, slots), nil
	}

	minWh := cfg.CapacityWh * cfg.MinSoc / 100
	maxWh := cfg.CapacityWh * cfg.MaxSoc / 100
	socWh := clamp(cfg.CapacityWh*cfg.Soc/100, minWh, maxWh)

	liveHeadroom := cfg.HeadroomW
	liveResidual := cfg.LiveResidualW
	out := make([]BatteryHorizonSlot, len(slots))
	var first BatteryPlan

	for i := range slots {
		step := cfg
		step.Soc = 100 * socWh / cfg.CapacityWh
		h := slotHours(slots[i])
		profile := residualW(slots[i], h)
		if i == 0 {
			step.HeadroomW = liveHeadroom
			step.LiveResidualW = liveResidual
		} else {
			step.LiveResidualW = profile
			step.HeadroomW = max(0, cfg.GridThresholdW-profile)
		}

		p := PlanBattery(step, slots[i:])
		if i == 0 {
			first = p
		}

		socWh = applyHorizonStep(step, slots[i], p, socWh)
		houseWh := max(0, slots[i].HomeWh-slots[i].LoadWh)
		out[i] = BatteryHorizonSlot{
			Start:      slots[i].Start,
			End:        slots[i].End,
			Action:     p.Action,
			Reason:     p.Reason,
			ChargeW:    p.ChargeW,
			DischargeW: p.DischargeW,
			HomeW:      watts(houseWh, h),
			SolarW:     watts(slots[i].SolarWh, h),
			LoadW:      watts(slots[i].LoadWh, h),
			ResidualW:  profile,
			Price:      slots[i].Price,
			FeedIn:     slots[i].FeedIn,
			Soc:        100 * socWh / cfg.CapacityWh,
			CoverSoc:   p.TargetSoc,
			Peak:       cfg.GridThresholdW > 0 && profile > cfg.GridThresholdW,
		}
	}

	return first, out
}

func watts(wh, hours float64) float64 {
	if hours <= 0 {
		return 0
	}
	return wh / hours
}

func applyHorizonStep(cfg BatteryConfig, s BatterySlot, plan BatteryPlan, socWh float64) float64 {
	hours := slotHours(s)
	if hours <= 0 || cfg.CapacityWh <= 0 {
		return socWh
	}

	minWh := minEnergyWh(cfg)
	maxWh := cfg.CapacityWh * cfg.MaxSoc / 100
	floorWh := reserveEnergyWh(cfg)
	net := netPowerW(s, hours)
	etaC, etaD := cfg.EtaC, cfg.EtaD
	if etaC <= 0 {
		etaC = batteryEta
	}
	if etaD <= 0 {
		etaD = batteryEta
	}

	switch plan.Action {
	case BatteryActionCharge:
		fromGrid := float64(plan.ChargeW)
		fromPv := 0.0
		if net < 0 {
			fromPv = min(-net, max(0, cfg.ChargeW-fromGrid))
		}
		socWh += (fromGrid + fromPv) * hours * etaC
	case BatteryActionDischarge:
		socWh -= float64(plan.DischargeW) * hours / etaD
	case BatteryActionHold:
		if net < 0 {
			socWh += min(-net, cfg.ChargeW) * hours * etaC
		}
	default:
		if net < 0 {
			socWh += min(-net, cfg.ChargeW) * hours * etaC
		} else if net > 0 && socWh > floorWh+1 {
			cover := min(net, cfg.DischargeW, (socWh-floorWh)/hours*etaD)
			socWh -= cover * hours / etaD
		}
	}

	return clamp(socWh, minWh, maxWh)
}
