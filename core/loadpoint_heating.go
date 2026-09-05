package core

import (
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/core/keys"
	"github.com/evcc-io/evcc/core/loadpoint"
	"github.com/evcc-io/evcc/tariff"
)

func (lp *Loadpoint) GetHeatingComfort() loadpoint.HeatingComfort {
	lp.RLock()
	defer lp.RUnlock()
	return lp.heatingComfort
}

func (lp *Loadpoint) SetHeatingComfort(c loadpoint.HeatingComfort) error {
	lp.Lock()
	defer lp.Unlock()

	if c.Hysteresis < 0 {
		c.Hysteresis = 0
	}
	if c.MinOnTime < 0 {
		c.MinOnTime = 0
	}
	if err := lp.settings.SetJson(keys.HeatingComfort, c); err != nil {
		return err
	}
	lp.heatingComfort = c
	lp.publishHeatingStatus()
	lp.requestUpdate()
	return nil
}

func (lp *Loadpoint) SetHeatingHistory(boosts []loadpoint.HeatingBoost, pattern loadpoint.HeatingPattern) error {
	lp.Lock()
	defer lp.Unlock()

	if boosts == nil {
		boosts = []loadpoint.HeatingBoost{}
	}
	if err := lp.settings.SetJson(keys.HeatingBoosts, boosts); err != nil {
		return err
	}
	if pattern.Bands == nil && len(boosts) > 0 {
		pattern = loadpoint.RebuildHeatingPattern(boosts)
	}
	if err := lp.settings.SetJson(keys.HeatingPattern, pattern); err != nil {
		return err
	}
	lp.heatingBoosts = boosts
	lp.heatingPattern = pattern
	lp.publishHeatingStatus()
	return nil
}

func (lp *Loadpoint) GetHeatingStatus() loadpoint.HeatingStatus {
	lp.RLock()
	defer lp.RUnlock()
	return lp.heatingStatusLocked()
}

func (lp *Loadpoint) heatingStatusLocked() loadpoint.HeatingStatus {
	reason := lp.heatingBoostReason
	if reason == "" && !lp.heatingComfortSince.IsZero() {
		reason = "comfort"
	} else if reason == "" && lp.planActive && lp.chargerHasFeature(api.Heating) {
		reason = "calendar"
	}
	return loadpoint.HeatingStatus{
		Comfort:   lp.heatingComfort,
		Boosts:    lp.heatingBoosts,
		Pattern:   lp.heatingPattern,
		Active:    lp.heatingBoostActive,
		Estimated: !lp.HasChargeMeter() && lp.chargerHasFeature(api.Heating),
		Reason:    reason,
		StartTemp: lp.heatingBoostStartTemp,
	}
}

func (lp *Loadpoint) publishHeatingStatus() {
	lp.publish(keys.HeatingComfort, lp.heatingComfort)
	lp.publish(keys.HeatingStatus, lp.heatingStatusLocked())
}

func (lp *Loadpoint) heatingStopTempLocked() float64 {
	if lp.heatingComfort.StopTemp > 0 {
		return lp.heatingComfort.StopTemp
	}
	if lp.limitSoc > 0 {
		return float64(lp.limitSoc)
	}
	return 50
}

func (lp *Loadpoint) heatingComfortHeating() bool {
	lp.Lock()
	defer lp.Unlock()
	return lp.heatingComfortActive()
}

func (lp *Loadpoint) heatingComfortActive() bool {
	if !lp.chargerHasFeature(api.Heating) {
		return false
	}
	minTemp := lp.heatingComfort.MinTemp
	if minTemp <= 0 || lp.vehicleSoc <= 0 {
		return false
	}

	if stop := lp.heatingStopTempLocked(); stop > 0 && lp.vehicleSoc >= stop {
		lp.heatingComfortSince = time.Time{}
		return false
	}

	stop := minTemp + lp.heatingComfort.EffectiveHysteresis()
	if !lp.heatingComfortSince.IsZero() {
		if lp.vehicleSoc >= stop && lp.heatingMinOnElapsed() {
			lp.heatingComfortSince = time.Time{}
			lp.publishHeatingStatus()
			return false
		}
		return true
	}

	if lp.vehicleSoc < minTemp {
		lp.heatingComfortSince = lp.clock.Now()
		lp.heatingBoostReason = "comfort"
		lp.log.DEBUG.Printf("heating comfort floor: %.1f°C < %.1f°C", lp.vehicleSoc, minTemp)
		lp.publishHeatingStatus()
		return true
	}
	return false
}

func (lp *Loadpoint) heatingMinOnElapsed() bool {
	d := lp.heatingComfort.MinOnDuration()
	if d <= 0 {
		return true
	}
	start := lp.heatingComfortSince
	if start.IsZero() {
		start = lp.heatingBoostStart
	}
	if start.IsZero() {
		return true
	}
	return lp.clock.Since(start) >= d
}

func (lp *Loadpoint) heatingMinOnRemaining() time.Duration {
	d := lp.heatingComfort.MinOnDuration()
	if d <= 0 || !lp.chargerHasFeature(api.Heating) {
		return 0
	}
	start := lp.heatingBoostStart
	if start.IsZero() {
		return 0
	}
	left := d - lp.clock.Since(start)
	if left < 0 {
		return 0
	}
	return left
}

// UpdateHeatingBoost samples inferred extra power while a heating boost is on.
func (lp *Loadpoint) UpdateHeatingBoost(householdW float64) {
	if !lp.chargerHasFeature(api.Heating) {
		lp.heatingLastHouseholdW = householdW
		return
	}

	lp.Lock()
	defer lp.Unlock()

	enabled := lp.enabled || lp.status == api.StatusC
	now := lp.clock.Now()

	if enabled && !lp.heatingBoostActive {
		lp.heatingBoostActive = true
		lp.heatingBoostStart = now
		lp.heatingBoostStartTemp = lp.vehicleSoc
		lp.heatingBoostBaselineW = lp.heatingLastHouseholdW
		if lp.heatingBoostBaselineW <= 0 {
			lp.heatingBoostBaselineW = householdW
		}
		lp.heatingBoostEnergyWh = 0
		lp.heatingBoostExtra = nil
		lp.heatingBoostSlotStart = now.Truncate(tariff.SlotDuration)
		lp.heatingBoostSlotWh = 0
		if lp.heatingBoostReason == "" {
			if !lp.heatingComfortSince.IsZero() {
				lp.heatingBoostReason = "comfort"
			} else if lp.planActive {
				lp.heatingBoostReason = "calendar"
			} else {
				lp.heatingBoostReason = "now"
			}
		}
		lp.log.DEBUG.Printf("heating boost start: baseline %.0fW temp %.1f°C reason %s", lp.heatingBoostBaselineW, lp.heatingBoostStartTemp, lp.heatingBoostReason)
		lp.publishHeatingStatus()
	}

	if lp.heatingBoostActive {
		extra := householdW - lp.heatingBoostBaselineW
		if extra < 0 {
			extra = 0
		}
		maxW := lp.heatingComfort.MaxAssumedPowerW
		if maxW <= 0 {
			maxW = 6000
		}
		if extra > maxW {
			extra = maxW
		}
		if lp.HasChargeMeter() && lp.chargePower > extra {
			extra = lp.chargePower
		}

		rising := lp.vehicleSoc <= 0 || lp.vehicleSoc+0.2 >= lp.heatingBoostStartTemp
		if !rising {
			extra = 0
		}

		slot := now.Truncate(tariff.SlotDuration)
		if !slot.Equal(lp.heatingBoostSlotStart) && !lp.heatingBoostSlotStart.IsZero() {
			h := tariff.SlotDuration.Hours()
			if h > 0 && lp.heatingBoostSlotWh > 0 {
				lp.heatingBoostExtra = append(lp.heatingBoostExtra, lp.heatingBoostSlotWh/h)
			}
			lp.heatingBoostSlotStart = slot
			lp.heatingBoostSlotWh = 0
		}
		if !lp.heatingBoostSample.IsZero() {
			dt := lp.clock.Since(lp.heatingBoostSample).Hours()
			if dt > 0 {
				lp.heatingBoostEnergyWh += extra * dt
				lp.heatingBoostSlotWh += extra * dt
			}
		}
		lp.heatingBoostSample = now
	}

	if !enabled && lp.heatingBoostActive {
		lp.finishHeatingBoostLocked("stop")
	}

	lp.heatingLastHouseholdW = householdW
}

func (lp *Loadpoint) finishHeatingBoostLocked(reason string) {
	now := lp.clock.Now()
	if lp.heatingBoostSlotWh > 0 {
		h := tariff.SlotDuration.Hours()
		if h > 0 {
			lp.heatingBoostExtra = append(lp.heatingBoostExtra, lp.heatingBoostSlotWh/h)
		}
	}

	peak := 0.0
	for _, w := range lp.heatingBoostExtra {
		peak = max(peak, w)
	}

	ep := loadpoint.HeatingBoost{
		Start:     lp.heatingBoostStart,
		End:       now,
		StartTemp: lp.heatingBoostStartTemp,
		EndTemp:   lp.vehicleSoc,
		EnergyWh:  lp.heatingBoostEnergyWh,
		PeakW:     peak,
		ExtraW:    lp.heatingBoostExtra,
		Reason:    reason,
		Estimated: !lp.HasChargeMeter(),
	}
	if ep.Reason == "" {
		ep.Reason = lp.heatingBoostReason
	}
	ep.Quality = loadpoint.ClassifyBoost(ep, lp.heatingComfort.MinOnDuration())

	lp.heatingBoosts = append(lp.heatingBoosts, ep)
	if len(lp.heatingBoosts) > loadpoint.MaxHeatingBoostsStored() {
		lp.heatingBoosts = lp.heatingBoosts[len(lp.heatingBoosts)-loadpoint.MaxHeatingBoostsStored():]
	}
	lp.heatingPattern = loadpoint.RebuildHeatingPattern(lp.heatingBoosts)
	_ = lp.settings.SetJson(keys.HeatingBoosts, lp.heatingBoosts)
	_ = lp.settings.SetJson(keys.HeatingPattern, lp.heatingPattern)

	lp.log.INFO.Printf("heating boost: %.1f→%.1f°C in %v, %.2fkWh extra (quality %s, %s)",
		ep.StartTemp, ep.EndTemp, ep.End.Sub(ep.Start).Round(time.Second), ep.EnergyWh/1e3, ep.Quality, ep.Reason)

	lp.heatingBoostActive = false
	lp.heatingBoostReason = ""
	lp.heatingBoostStart = time.Time{}
	lp.heatingBoostSample = time.Time{}
	lp.heatingBoostSlotWh = 0
	lp.heatingBoostExtra = nil
	lp.publishHeatingStatus()
}

func (lp *Loadpoint) restoreHeating() {
	var c loadpoint.HeatingComfort
	if err := lp.settings.Json(keys.HeatingComfort, &c); err == nil {
		lp.heatingComfort = c
	}
	var boosts []loadpoint.HeatingBoost
	if err := lp.settings.Json(keys.HeatingBoosts, &boosts); err == nil {
		lp.heatingBoosts = boosts
	}
	var pattern loadpoint.HeatingPattern
	if err := lp.settings.Json(keys.HeatingPattern, &pattern); err == nil {
		lp.heatingPattern = pattern
	} else {
		lp.heatingPattern = loadpoint.RebuildHeatingPattern(lp.heatingBoosts)
	}

	var plans []api.RepeatingPlan
	if err := lp.settings.Json(keys.RepeatingPlans, &plans); err == nil {
		lp.repeatingPlans = plans
	}
}
