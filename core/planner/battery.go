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

	BatteryReasonIdle     = "idle"
	BatteryReasonPeak     = "peak"
	BatteryReasonCharge   = "charge"
	BatteryReasonCheap    = "cheap"
	BatteryReasonHold     = "hold"
	BatteryReasonRecovery = "recovery"
	BatteryReasonExport   = "export"

	batteryEta            = 0.9
	defaultBatteryChargeW = 2500.0
)

// BatterySlot is one planning interval with tax-inclusive prices.
type BatterySlot struct {
	Start   time.Time
	End     time.Time
	HomeWh  float64
	SolarWh float64
	LoadWh  float64 // planned charger/heater energy included in HomeWh
	Price   float64 // grid price including charges and tax, per kWh
	FeedIn  float64
}

// BatteryConfig describes the battery and grid constraints for PlanBattery.
type BatteryConfig struct {
	Soc, MinSoc, MaxSoc, ReserveSoc float64
	CapacityWh                      float64
	ChargeW, DischargeW             float64
	EtaC, EtaD                      float64
	CycleCost                       float64 // €/kWh discharged (wear)
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
	Peak       bool
}

// PlanBattery decides charge, hold, discharge or export for the current slot.
// Prices must already include taxes and levies. Peak energy is a hard constraint:
// the battery is charged ahead of the next predicted overshoot cluster when
// later slots cannot cover the deficit. Below reserve, opportunistic cheap
// refill is skipped so a dip after a peak does not immediately grid-charge.
// for peaks stops at reserve SoC. Live charge/discharge watts are then tracked on
// a faster control loop. Economic cycling only happens when there is no grid
// limit and the tax-inclusive spread covers round-trip losses and cycle cost.
// Leftover energy above the peak reserve can be sold on expensive feed-in hours only.
func PlanBattery(cfg BatteryConfig, slots []BatterySlot) BatteryPlan {
	if cfg.CapacityWh <= 0 || len(slots) == 0 {
		return BatteryPlan{Action: BatteryActionNormal, Reason: BatteryReasonIdle}
	}

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

	minWh := minEnergyWh(cfg)
	maxWh := cfg.CapacityWh * cfg.MaxSoc / 100
	floorWh := reserveEnergyWh(cfg)
	socWh := clamp(cfg.CapacityWh*cfg.Soc/100, minWh, maxWh)

	peakWh := peakEnergyWh(cfg, slots)
	targetWh := requiredEnergyWh(cfg, slots)
	targetWh = clamp(targetWh, floorWh, maxWh)

	prices := slotPrices(slots)
	dischargeFloor, chargeCeiling := economicBands(prices, cfg.EtaC, cfg.EtaD, cfg.CycleCost)

	plan := BatteryPlan{
		Action:         BatteryActionNormal,
		Reason:         BatteryReasonIdle,
		TargetSoc:      100 * targetWh / cfg.CapacityWh,
		PeakWh:         peakWh,
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

	// Peak discharge stops at reserve. Min SoC is only the live last-resort floor.
	if cfg.GridThresholdW > 0 && overshootW > 0 && socWh > floorWh+1 {
		discharge := min(overshootW, cfg.DischargeW, (socWh-floorWh)/hours*cfg.EtaD)
		if discharge > 0 {
			plan.Action = BatteryActionDischarge
			plan.DischargeW = int(math.Round(discharge))
			plan.Reason = BatteryReasonPeak
			return plan
		}
	}

	deficitWh := targetWh - socWh
	if deficitWh > 1 && cfg.GridThresholdW > 0 {
		mustCharge, cheap := chargeNow(cfg, slots, socWh, targetWh)
		// Below reserve, only charge when later slots cannot refill before the
		// next peak. Cheap opportunistic refill of the reserve chatters with
		// discharge on the next overshoot.
		if mustCharge || (cheap && socWh >= floorWh) {
			charge := min(cfg.ChargeW, cfg.HeadroomW, deficitWh/hours/cfg.EtaC)
			if charge > 0 {
				plan.Action = BatteryActionCharge
				plan.ChargeW = int(math.Round(charge))
				if mustCharge {
					plan.Reason = BatteryReasonCharge
				} else {
					plan.Reason = BatteryReasonCheap
				}
				return plan
			}
		}
	}

	// Opportunistic cycling to max SoC is for economics-only sites. With a grid
	// limit it recharges between peaks and chatters charge/discharge.
	if cfg.GridThresholdW <= 0 && cfg.CycleCost >= 0 && chargeCeiling > 0 && cur.Price > 0 && cur.Price <= chargeCeiling && socWh < maxWh-1 && deficitWh <= 1 {
		charge := min(cfg.ChargeW, cfg.HeadroomW, (maxWh-socWh)/hours/cfg.EtaC)
		if charge > 0 {
			plan.Action = BatteryActionCharge
			plan.ChargeW = int(math.Round(charge))
			plan.Reason = BatteryReasonCheap
			return plan
		}
	}

	if dischargeFloor > 0 && cur.Price > 0 && cur.Price < dischargeFloor && socWh > floorWh+1 && (deficitWh > -cfg.CapacityWh*0.05 || peakWh > 0) {
		plan.Action = BatteryActionHold
		plan.Reason = BatteryReasonHold
		return plan
	}

	if w := exportExcessW(cfg, slots, socWh, targetWh); w > 0 {
		plan.Action = BatteryActionDischarge
		plan.DischargeW = int(math.Round(w))
		plan.Reason = BatteryReasonExport
		return plan
	}

	return plan
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

const peakClusterMergeSlots = 2

// requiredEnergyWh is reserve plus energy for the next peak cluster. Later
// clusters are not pre-charged: that would refill between peaks and chatter.
func requiredEnergyWh(cfg BatteryConfig, slots []BatterySlot) float64 {
	floorWh := reserveEnergyWh(cfg)
	maxWh := cfg.CapacityWh * cfg.MaxSoc / 100
	return clamp(floorWh+nextClusterEnergyWh(cfg, slots), floorWh, maxWh)
}

func nextClusterEnergyWh(cfg BatteryConfig, slots []BatterySlot) float64 {
	start := firstPeakIndex(cfg, slots)
	if start < 0 {
		return 0
	}
	// already in a peak, or the next one is the following quarter: use energy
	// already above reserve instead of grid-charging into a discharge slot
	if start < peakClusterMergeSlots {
		return 0
	}

	end := start
	quiet := 0
	for i := start; i < len(slots); i++ {
		if slotOvershootWh(cfg, slots[i]) > 0 {
			end = i
			quiet = 0
			continue
		}
		quiet++
		if quiet >= peakClusterMergeSlots {
			break
		}
	}

	var need float64
	for i := start; i <= end; i++ {
		overshoot := slotOvershootWh(cfg, slots[i])
		if overshoot > 0 && cfg.EtaD > 0 {
			need += overshoot / cfg.EtaD
		}
	}
	for i := 0; i < start; i++ {
		surplus := max(0, slots[i].SolarWh-slots[i].HomeWh)
		if surplus > 0 && cfg.EtaC > 0 {
			need -= surplus * cfg.EtaC
		}
	}
	return max(0, need)
}

func chargeNow(cfg BatteryConfig, slots []BatterySlot, socWh, targetWh float64) (must, cheap bool) {
	deficit := targetWh - socWh
	if deficit <= 0 {
		return false, false
	}

	firstPeak := firstPeakIndex(cfg, slots)
	if firstPeak >= 0 && firstPeak < peakClusterMergeSlots {
		return false, false
	}
	until := len(slots)
	if firstPeak >= 0 {
		until = firstPeak
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
	// charge in the cheaper half of remaining slots before the peak
	idx := max(0, len(prices)/2-1)
	cheap = slots[0].Price <= prices[idx]*1.05
	return must, cheap
}

func firstPeakIndex(cfg BatteryConfig, slots []BatterySlot) int {
	for i, s := range slots {
		if slotOvershootWh(cfg, s) > 0 {
			return i
		}
	}
	return -1
}

// exportExcessW is AC discharge for selling leftover energy on expensive
// feed-in hours. Energy reserved for later peaks is never sold. If a later
// hour pays more, that hour is filled first and only overflow is sold now.
//
// Energy that later house load would self-consume (avoiding tax-inclusive
// import) is never sold. Only true leftover above that need can be exported,
// and only when feed-in revenue beats the avoided-import value of that slice.
func exportExcessW(cfg BatteryConfig, slots []BatterySlot, socWh, targetWh float64) float64 {
	excessWh := socWh - targetWh
	if excessWh <= 1 {
		return 0
	}

	cur := slots[0]
	if cur.FeedIn <= 0 || cur.FeedIn < cfg.CycleCost {
		return 0
	}

	var feeds []float64
	for _, s := range slots {
		if s.FeedIn > 0 {
			feeds = append(feeds, s.FeedIn)
		}
	}
	if len(feeds) < 4 {
		return 0
	}

	sorted := slices.Clone(feeds)
	slices.Sort(sorted)
	if percentile(sorted, 0.75)-percentile(sorted, 0.25) < 0.01 {
		return 0
	}
	if cur.FeedIn < percentile(sorted, 0.75) {
		return 0
	}

	hours := slotHours(cur)
	if hours <= 0 || cfg.EtaD <= 0 {
		return 0
	}

	var laterWh float64
	for _, s := range slots[1:] {
		if s.FeedIn > cur.FeedIn && s.FeedIn >= cfg.CycleCost {
			laterWh += cfg.DischargeW * slotHours(s) / cfg.EtaD
		}
	}
	sellWh := excessWh - min(excessWh, laterWh)
	if sellWh <= 1 {
		return 0
	}

	// Keep enough for later self-consumption under the grid limit.
	scNeedDC := selfConsumptionNeedWh(cfg, slots) / cfg.EtaD
	sellWh -= min(sellWh, scNeedDC)
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

// selfConsumptionNeedWh is later house residual (under the grid threshold) that
// the battery could cover from AC discharge, in Wh.
func selfConsumptionNeedWh(cfg BatteryConfig, slots []BatterySlot) float64 {
	var need float64
	for _, s := range slots[1:] {
		h := slotHours(s)
		residual := max(0, s.HomeWh-s.SolarWh)
		if cfg.GridThresholdW > 0 {
			residual = min(residual, cfg.GridThresholdW*h)
		}
		need += residual
	}
	return need
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
		socWh += float64(plan.ChargeW) * hours * etaC
		if net < 0 {
			socWh += min(-net, cfg.ChargeW) * hours * etaC
		}
	case BatteryActionDischarge:
		socWh -= float64(plan.DischargeW) * hours / etaD
	case BatteryActionHold:
		// Hold blocks discharge to the house, but PV surplus may still charge.
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
