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

// PlanBattery decides charge, hold or discharge for the current slot.
// Prices must already include taxes and levies. Peak energy is a hard constraint:
// the battery is charged ahead of predicted overshoots. Live charge/discharge
// watts are then tracked on a faster control loop. Economic cycling only happens
// when the tax-inclusive spread covers round-trip losses and cycle cost.
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

	minWh := cfg.CapacityWh * cfg.MinSoc / 100
	maxWh := cfg.CapacityWh * cfg.MaxSoc / 100
	reserveWh := cfg.CapacityWh * cfg.ReserveSoc / 100
	socWh := clamp(cfg.CapacityWh*cfg.Soc/100, minWh, maxWh)

	peakWh := peakEnergyWh(cfg, slots)
	targetWh := requiredEnergyWh(cfg, slots)
	targetWh = clamp(targetWh, minWh, maxWh)
	if reserveWh > minWh {
		targetWh = max(targetWh, minWh)
	}

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

	if cfg.GridThresholdW > 0 && overshootW > 0 && socWh > minWh+1 {
		discharge := min(overshootW, cfg.DischargeW, (socWh-minWh)/hours*cfg.EtaD)
		if discharge > 0 {
			plan.Action = BatteryActionDischarge
			plan.DischargeW = int(math.Round(discharge))
			plan.Reason = BatteryReasonPeak
			return plan
		}
	}

	deficitWh := targetWh - socWh
	if deficitWh > 1 && cfg.GridThresholdW > 0 {
		if mustCharge, cheap := chargeNow(cfg, slots, socWh, targetWh); mustCharge || cheap {
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

	if cfg.CycleCost >= 0 && chargeCeiling > 0 && cur.Price > 0 && cur.Price <= chargeCeiling && socWh < maxWh-1 && deficitWh <= 1 {
		charge := min(cfg.ChargeW, cfg.HeadroomW, (maxWh-socWh)/hours/cfg.EtaC)
		if charge > 0 {
			plan.Action = BatteryActionCharge
			plan.ChargeW = int(math.Round(charge))
			plan.Reason = BatteryReasonCheap
			return plan
		}
	}

	if dischargeFloor > 0 && cur.Price > 0 && cur.Price < dischargeFloor && socWh > minWh+1 && (deficitWh > -cfg.CapacityWh*0.05 || peakWh > 0) {
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

	if socWh < reserveWh-1 && cfg.GridThresholdW > 0 && cfg.HeadroomW > 0 {
		charge := min(cfg.ChargeW, cfg.HeadroomW, (reserveWh-socWh)/hours/cfg.EtaC)
		if charge > 0 {
			plan.Action = BatteryActionCharge
			plan.ChargeW = int(math.Round(charge))
			plan.Reason = BatteryReasonRecovery
			return plan
		}
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

func thresholdWh(cfg BatteryConfig, hours float64) float64 {
	return max(0, cfg.GridThresholdW) * hours
}

func peakEnergyWh(cfg BatteryConfig, slots []BatterySlot) float64 {
	var sum float64
	for _, s := range slots {
		h := slotHours(s)
		sum += max(0, (s.HomeWh-s.SolarWh)-thresholdWh(cfg, h))
	}
	if cfg.EtaD > 0 {
		return sum / cfg.EtaD
	}
	return sum
}

// requiredEnergyWh is how much energy must be in the battery now so later
// peaks never drive SoC below min, after using solar that arrives in between.
func requiredEnergyWh(cfg BatteryConfig, slots []BatterySlot) float64 {
	need := cfg.CapacityWh * cfg.MinSoc / 100
	maxWh := cfg.CapacityWh * cfg.MaxSoc / 100

	for i := len(slots) - 1; i >= 0; i-- {
		s := slots[i]
		h := slotHours(s)
		residual := max(0, s.HomeWh-s.SolarWh)
		surplus := max(0, s.SolarWh-s.HomeWh)
		overshoot := max(0, residual-thresholdWh(cfg, h))

		if overshoot > 0 && cfg.EtaD > 0 {
			need += overshoot / cfg.EtaD
		}
		if surplus > 0 && cfg.EtaC > 0 {
			need -= surplus * cfg.EtaC
		}
		need = clamp(need, cfg.CapacityWh*cfg.MinSoc/100, maxWh)
	}

	return need
}

func chargeNow(cfg BatteryConfig, slots []BatterySlot, socWh, targetWh float64) (must, cheap bool) {
	deficit := targetWh - socWh
	if deficit <= 0 {
		return false, false
	}

	firstPeak := firstPeakIndex(cfg, slots)
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
		h := slotHours(s)
		if max(0, s.HomeWh-s.SolarWh) > thresholdWh(cfg, h) {
			return i
		}
	}
	return -1
}

// exportExcessW is AC discharge for selling leftover energy on expensive
// feed-in hours. Energy reserved for later peaks is never sold. If a later
// hour pays more, that hour is filled first and only overflow is sold now.
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

	return min(cfg.DischargeW, sellWh/hours*cfg.EtaD)
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
