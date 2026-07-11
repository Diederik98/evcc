package core

import (
	"errors"
	"fmt"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/core/keys"
	"github.com/evcc-io/evcc/core/loadpoint"
	"github.com/evcc-io/evcc/util/config"
)

func batteryModeModified(mode api.BatteryMode) bool {
	return mode != api.BatteryUnknown && mode != api.BatteryNormal
}

func (site *Site) batteryConfigured() bool {
	return len(site.batteryMeters) > 0
}

func (site *Site) hasBatteryControl() bool {
	for _, dev := range site.batteryMeters {
		meter := dev.Instance()

		if api.HasCap[api.BatteryController](meter) {
			return true
		}
	}

	return false
}

// setBatteryMode sets the battery mode
func (site *Site) setBatteryMode(batMode api.BatteryMode) {
	site.batteryMode = batMode
	site.publish(keys.BatteryMode, batMode)
}

// SetBatteryMode sets the battery mode
func (site *Site) SetBatteryMode(batMode api.BatteryMode) {
	site.Lock()
	defer site.Unlock()

	site.log.DEBUG.Println("set battery mode:", batMode)

	if site.batteryMode != batMode {
		site.setBatteryMode(batMode)
	}

	if site.batteryModeExternal == api.BatteryUnknown {
		site.batteryModeExternalTimer = time.Time{}
	}
}

func (site *Site) updateBatteryMode(batteryGridChargeActive bool, rate api.Rate) {
	batteryMode := site.requiredBatteryMode(batteryGridChargeActive, rate)

	// put battery into hold mode when charging is active and HEMS dimmed
	fromToCharge := batteryMode == api.BatteryCharge || batteryMode == api.BatteryUnknown && site.batteryMode == api.BatteryCharge
	if dimmed := hemsDimmed(site.hems); fromToCharge && dimmed != nil && *dimmed {
		site.log.DEBUG.Println("battery mode: HEMS dimmed")
		batteryMode = api.BatteryHold
	}

	// NOTE: applyBatteryMode is always called when charge mode is active to validate max soc
	if modeChanged := batteryMode != api.BatteryUnknown; modeChanged || site.batteryMode == api.BatteryCharge {
		if err := site.applyBatteryMode(batteryMode); err == nil {
			if modeChanged {
				site.SetBatteryMode(batteryMode)
			}
		} else {
			site.log.ERROR.Println("battery mode:", err)
		}
	}
}

func (site *Site) peakShaveActive() bool {
	state := site.peakShaveState
	return state != "" && state != PeakShaveIdle
}

func (site *Site) peakShaveEnabled() bool {
	return site.GridThreshold > 0 && len(site.batteryMeters) > 0
}

const peakShaveUnlimitedPower = 100000

func (site *Site) peakShaveGridHeadroom() float64 {
	gridThresholdW := site.GridThreshold * 1000.0
	return max(0, gridThresholdW-site.gridPower)
}

func (site *Site) peakShaveRecoveryChargePower(maintainPower float64) int {
	gridCharge := min(maintainPower, site.peakShaveGridHeadroom())

	pvBonus := max(0, site.pvPower)
	total := gridCharge + pvBonus

	maxCharge := float64(peakShaveUnlimitedPower)
	for _, dev := range site.batteryMeters {
		limiter, ok := api.Cap[api.BatteryPowerLimiter](dev.Instance())
		if !ok {
			continue
		}
		if _, max := limiter.GetPowerLimits(); max > 0 {
			maxCharge = min(maxCharge, max)
		}
	}

	return int(min(total, maxCharge))
}

// peakShaveEffectiveMinSoc returns the minimum SoC used for peak shaving.
// Uses the configured battery min SoC when available, otherwise the site fallback.
func (site *Site) peakShaveEffectiveMinSoc() float64 {
	var maxMin float64
	hasLimiter := false

	for _, dev := range site.batteryMeters {
		meter := dev.Instance()
		limiter, ok := api.Cap[api.BatterySocLimiter](meter)
		if !ok {
			continue
		}
		min, _ := limiter.GetSocLimits()
		if min <= 0 {
			continue
		}
		hasLimiter = true
		maxMin = max(maxMin, min)
	}

	if hasLimiter {
		return maxMin
	}

	return site.PeakShaveMinSoc
}

// requiredBatteryMode determines required battery mode based on grid charge and rate
func (site *Site) requiredBatteryMode(batteryGridChargeActive bool, rate api.Rate) api.BatteryMode {
	var res api.BatteryMode
	batMode := site.GetBatteryMode()
	extMode := site.GetBatteryModeExternal()

	var extModeReset bool
	if extMode == api.BatteryUnknown {
		site.Lock()
		extModeReset = !site.batteryModeExternalTimer.IsZero()
		site.Unlock()
	}

	keepUnlessModified := func(s api.BatteryMode) api.BatteryMode {
		return map[bool]api.BatteryMode{false: s, true: api.BatteryUnknown}[batMode == s]
	}

	switch {
	case !site.batteryConfigured():
		res = api.BatteryUnknown
	case extModeReset:
		// require normal mode to leave external control
		res = api.BatteryNormal
	case extMode != api.BatteryUnknown:
		// require external mode only once
		if extMode != batMode {
			res = extMode
		}
	case batteryGridChargeActive:
		res = keepUnlessModified(api.BatteryCharge)
	case site.dischargeControlActive(rate):
		res = keepUnlessModified(api.BatteryHold)
	case batteryModeModified(batMode):
		res = api.BatteryNormal
	}

	return res
}

// batteryMaxSocReached checks is battery has exceed max soc limit
func (site *Site) batteryMaxSocReached(dev config.Device[api.Meter]) (bool, error) {
	meter := dev.Instance()

	batLimiter, ok := api.Cap[api.BatterySocLimiter](meter)
	if !ok {
		return false, nil
	}

	batSoc, ok := api.Cap[api.Battery](meter)
	if !ok {
		return false, errors.New("battery with soc limits must have soc")
	}

	soc, err := batSoc.Soc()
	if err != nil {
		return false, err
	}

	if _, max := batLimiter.GetSocLimits(); max > 0 && max < 100 && soc >= max {
		site.log.DEBUG.Printf("battery %s: limit soc reached (%.0f > %.0f)", deviceTitleOrName(dev), soc, max)
		return true, nil
	}

	return false, nil
}

func (site *Site) peakShaveOverridesBatteryMode() bool {
	site.RLock()
	defer site.RUnlock()

	switch site.peakShaveState {
	case PeakShaveActive, PeakShaveCritical, PeakShaveShedding, PeakShaveLockout, PeakShaveRecovery:
		return true
	case PeakShaveIdle:
		return site.peakShaveBatteryLimited
	default:
		return false
	}
}

// applyBatteryMode applies the mode to each battery
//
// api.BatteryCharge:
//
//	The current soc is validated against max soc.
//	In case max soc is reached, hold mode is applied.
func (site *Site) applyBatteryMode(mode api.BatteryMode) error {
	if site.peakShaveOverridesBatteryMode() {
		site.log.DEBUG.Println("battery mode: peak shaving controls mode, ignoring mode change")
		return nil
	}

	return site.applyBatteryModeDirect(mode)
}

func (site *Site) applyBatteryModeDirect(mode api.BatteryMode) error {
	fromToCharge := mode == api.BatteryCharge || mode == api.BatteryUnknown && site.batteryMode == api.BatteryCharge

	for _, dev := range site.batteryMeters {
		meter := dev.Instance()

		batCtrl, ok := api.Cap[api.BatteryController](meter)
		if !ok {
			continue
		}

		// validate max soc
		if fromToCharge && mode != api.BatteryHold {
			ok, err := site.batteryMaxSocReached(dev)
			if err != nil && !errors.Is(err, api.ErrNotAvailable) {
				return err
			}

			// put battery into hold mode when soc limit reached
			if ok {
				// TODO do this only once
				mode = api.BatteryHold
			}
		}

		if mode != api.BatteryUnknown {
			if err := batCtrl.SetBatteryMode(mode); err == nil {
				site.log.DEBUG.Printf("set battery %s mode: %s", deviceTitleOrName(dev), mode)
			} else if !errors.Is(err, api.ErrNotAvailable) {
				return err
			}
		}
	}

	return nil
}

func (site *Site) tariffRates(usage api.TariffUsage) (api.Rates, error) {
	tariff := site.GetTariff(usage)
	if tariff == nil || tariff.Type() == api.TariffTypePriceStatic {
		return nil, nil
	}

	return tariff.Rates()
}

func (site *Site) smartCostActive(lp loadpoint.API, rate api.Rate) bool {
	limit := lp.GetSmartCostLimit()
	return limit != nil && !rate.IsZero() && rate.Value <= *limit
}

func (site *Site) batteryGridChargeActive(rate api.Rate) bool {
	limit := site.GetBatteryGridChargeLimit()
	return limit != nil && !rate.IsZero() && rate.Value <= *limit
}

func (site *Site) dischargeControlActive(rate api.Rate) bool {
	if !site.GetBatteryDischargeControl() {
		return false
	}

	site.RLock()
	active := site.peakShaveActive()
	site.RUnlock()
	if active {
		return false
	}

	for _, lp := range site.Loadpoints() {
		smartCostActive := site.smartCostActive(lp, rate)
		if lp.GetStatus() == api.StatusC && (smartCostActive || lp.IsFastChargingActive()) {
			return true
		}
	}

	return false
}

const (
	PeakShaveIdle     = "idle"
	PeakShaveActive   = "shaving"
	PeakShaveCritical = "critical"
	PeakShaveShedding = "shedding"
	PeakShaveRecovery = "recovery"
	PeakShaveLockout  = "lockout"
)

func (site *Site) setBatteryLimitLimits(chargeLimit, dischargeLimit int) {
	for _, dev := range site.batteryMeters {
		meter := dev.Instance()
		limitCtrl, ok := api.Cap[api.BatteryLimitController](meter)
		if !ok {
			continue
		}
		if err := limitCtrl.SetChargeLimit(chargeLimit); err != nil {
			site.log.ERROR.Printf("set battery %s charge limit: %v", deviceTitleOrName(dev), err)
		}
		if dischargeLimit > 0 || chargeLimit == 0 {
			if err := limitCtrl.SetDischargeLimit(dischargeLimit); err != nil {
				site.log.ERROR.Printf("set battery %s discharge limit: %v", deviceTitleOrName(dev), err)
			}
		}
	}
}

func (site *Site) resetBatteryLimitLimits() {
	site.setBatteryLimitLimits(0, 0)
	site.applyBatteryModeDirect(api.BatteryNormal)
}

func (site *Site) clearPeakShaveLoadShedding() {
	for _, lp := range site.loadpoints {
		lp.SetPeakShaveMaxCurrent(nil)
	}
}

func (site *Site) applyPeakShaveLoadShedding() {
	for _, lp := range site.loadpoints {
		if lp.GetStatus() == api.StatusC {
			min := lp.GetMinCurrent()
			lp.SetPeakShaveMaxCurrent(&min)
			site.log.INFO.Printf("peak shaving: throttling loadpoint '%s' to minCurrent", lp.GetTitle())
		} else {
			lp.SetPeakShaveMaxCurrent(nil)
		}
	}
}

func (site *Site) peakShaveLoadShedDue(now time.Time) bool {
	delay := site.PeakShaveLoadShedDelay
	if delay <= 0 {
		return true
	}
	if site.peakShaveOverloadSince.IsZero() {
		return false
	}
	return now.Sub(site.peakShaveOverloadSince) >= time.Duration(delay)*time.Second
}

// ManageGridLimits executes the state machine for active peak shaving and recovery
func (site *Site) ManageGridLimits(batteryGridChargeActive bool) {
	if !site.peakShaveEnabled() {
		site.Lock()
		wasLimited := site.peakShaveBatteryLimited
		site.peakShaveState = PeakShaveIdle
		site.peakShaveOverloadSince = time.Time{}
		site.peakShaveBatteryLimited = false
		site.Unlock()
		site.clearPeakShaveLoadShedding()
		if wasLimited {
			site.resetBatteryLimitLimits()
		}
		return
	}

	if site.gridMeter != nil && !site.gridPowerValid {
		site.log.DEBUG.Println("peak shaving: grid meter unavailable, skipping")
		return
	}

	gridThresholdW := site.GridThreshold * 1000.0
	targetDischarge := site.gridPower - gridThresholdW

	soc := site.battery.Soc
	reserveSoc := site.PeakShaveReserveSoc
	minSoc := site.peakShaveEffectiveMinSoc()
	maintainPower := site.PeakShaveMaintainSocChargePower
	headroom := site.peakShaveGridHeadroom()
	now := time.Now()

	site.Lock()
	defer site.Unlock()

	oldState := site.peakShaveState
	if oldState == "" {
		oldState = PeakShaveIdle
	}

	var newState string

	if targetDischarge > 0 {
		if site.peakShaveOverloadSince.IsZero() {
			site.peakShaveOverloadSince = now
		}

		shedDue := site.peakShaveLoadShedDue(now)

		switch {
		case soc <= minSoc:
			newState = PeakShaveLockout
		case shedDue:
			newState = PeakShaveShedding
		case soc <= reserveSoc:
			newState = PeakShaveCritical
		default:
			newState = PeakShaveActive
		}
	} else {
		site.peakShaveOverloadSince = time.Time{}
		if soc < reserveSoc {
			newState = PeakShaveRecovery
		} else {
			newState = PeakShaveIdle
		}
	}

	if newState != oldState {
		site.log.DEBUG.Printf("peak shaving state transition: %s -> %s", oldState, newState)
		site.publish(keys.PeakShaveState, newState)
	}
	site.peakShaveState = newState

	switch newState {
	case PeakShaveActive:
		site.log.DEBUG.Printf("active peak shaving: gridPower (%.0fW) > threshold (%.0fW), targetDischarge: %.0fW, soc: %.0f%%", site.gridPower, gridThresholdW, targetDischarge, soc)
		site.clearPeakShaveLoadShedding()
		site.setBatteryLimitLimits(0, int(targetDischarge))
		site.peakShaveBatteryLimited = true

	case PeakShaveCritical:
		site.log.DEBUG.Printf("critical peak shaving: gridPower (%.0fW) > threshold (%.0fW), targetDischarge: %.0fW, soc: %.0f%%", site.gridPower, gridThresholdW, targetDischarge, soc)
		site.clearPeakShaveLoadShedding()
		site.setBatteryLimitLimits(0, int(targetDischarge))
		site.peakShaveBatteryLimited = true

	case PeakShaveShedding:
		site.log.DEBUG.Printf("peak shaving load shedding: gridPower (%.0fW) > threshold (%.0fW), targetDischarge: %.0fW, soc: %.0f%%", site.gridPower, gridThresholdW, targetDischarge, soc)
		site.setBatteryLimitLimits(0, int(targetDischarge))
		site.applyPeakShaveLoadShedding()
		site.peakShaveBatteryLimited = true

	case PeakShaveLockout:
		site.log.DEBUG.Printf("hard lockout: grid exceeds limit but battery is empty (soc: %.0f%% <= min: %.0f%%)", soc, minSoc)
		site.setBatteryLimitLimits(0, 0)
		site.applyPeakShaveLoadShedding()
		site.peakShaveBatteryLimited = true

	case PeakShaveRecovery:
		chargeLimit := site.peakShaveRecoveryChargePower(maintainPower)
		gridCharge := int(min(maintainPower, site.peakShaveGridHeadroom()))
		site.log.DEBUG.Printf("active recovery: soc (%.0f%%) < reserve (%.0f%%), charging up to %dW (%dW grid + %dW pv)", soc, reserveSoc, chargeLimit, gridCharge, max(0, chargeLimit-gridCharge))
		site.clearPeakShaveLoadShedding()
		site.setBatteryLimitLimits(chargeLimit, 0)
		site.peakShaveBatteryLimited = true

	case PeakShaveIdle:
		site.clearPeakShaveLoadShedding()
		if batteryGridChargeActive {
			chargeLimit := site.peakShaveRecoveryChargePower(headroom)
			site.log.DEBUG.Printf("grid charge under peak shave limit: charging up to %dW", chargeLimit)
			site.setBatteryLimitLimits(chargeLimit, 0)
			site.peakShaveBatteryLimited = true
		} else if site.peakShaveBatteryLimited {
			site.log.DEBUG.Println("peak shaving inactive, resetting battery limits to normal")
			site.resetBatteryLimitLimits()
			site.peakShaveBatteryLimited = false
		}
	}
}

func (site *Site) validatePeakShaveSoc(minSoc, reserveSoc float64) error {
	if minSoc > reserveSoc {
		return fmt.Errorf("min soc (%.0f) must not exceed reserve soc (%.0f)", minSoc, reserveSoc)
	}
	return nil
}
