package core

import (
	"slices"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/core/keys"
	"github.com/evcc-io/evcc/core/planner"
	"github.com/evcc-io/evcc/tariff"
	"github.com/evcc-io/evcc/util"
)

type energyPlan struct {
	Id     int
	Energy float64
	End    time.Time
	Fixed  bool
}

func (lp *Loadpoint) GetRepeatingPlans() []api.RepeatingPlan {
	lp.RLock()
	defer lp.RUnlock()
	return lp.repeatingPlans
}

func (lp *Loadpoint) SetRepeatingPlans(plans []api.RepeatingPlan) error {
	lp.Lock()
	defer lp.Unlock()

	if plans == nil {
		plans = []api.RepeatingPlan{}
	}
	if err := lp.settings.SetJson(keys.RepeatingPlans, plans); err != nil {
		return err
	}
	lp.repeatingPlans = plans
	lp.publish(keys.RepeatingPlans, plans)
	lp.requestUpdate()
	return nil
}

func (lp *Loadpoint) nextEnergyPlan() (time.Time, float64, int, bool) {
	var plans []energyPlan
	if lp.planEnergy > 0 && !lp.planTime.IsZero() {
		plans = append(plans, energyPlan{Id: 1, Energy: lp.planEnergy, End: lp.planTime})
	}
	for i, rp := range lp.repeatingPlans {
		if !rp.Active || rp.Energy <= 0 || len(rp.Weekdays) == 0 {
			continue
		}
		t, err := util.GetNextOccurrenceFrom(lp.clock.Now(), rp.Weekdays, rp.Time, rp.Tz)
		if err != nil {
			lp.log.DEBUG.Printf("invalid repeating plan: weekdays=%v, time=%s, tz=%s, error=%v", rp.Weekdays, rp.Time, rp.Tz, err)
			continue
		}
		end := t
		if rp.Fixed {
			d := lp.energyDuration(rp.Energy, lp.heatingPlanPowerLocked())
			end = t.Add(d)
		}
		plans = append(plans, energyPlan{Id: i + 2, Energy: rp.Energy, End: end, Fixed: rp.Fixed})
	}
	if len(plans) == 0 {
		return time.Time{}, 0, 0, false
	}

	power := lp.heatingPlanPowerLocked()
	slices.SortStableFunc(plans, func(i, j energyPlan) int {
		is := i.End.Add(-lp.energyDuration(i.Energy, power))
		js := j.End.Add(-lp.energyDuration(j.Energy, power))
		return is.Compare(js)
	})

	p := plans[0]
	return p.End, p.Energy, p.Id, p.Fixed
}

func (lp *Loadpoint) energyDuration(energy, power float64) time.Duration {
	if power <= 0 {
		return 0
	}
	return time.Duration(energy * 1e3 / power * float64(time.Hour))
}

func (lp *Loadpoint) heatingPlanPowerLocked() float64 {
	if lp.heatingComfort.AssumedPowerW > 0 {
		return lp.heatingComfort.AssumedPowerW
	}
	if band, ok := lp.heatingPattern.MatchBand(lp.vehicleSoc); ok && band.PeakW > 0 {
		return band.PeakW
	}
	return lp.effectiveMaxPower()
}

func (lp *Loadpoint) heatingPlanPower() float64 {
	lp.RLock()
	defer lp.RUnlock()
	return lp.heatingPlanPowerLocked()
}

func (lp *Loadpoint) chargeDemand(deadline time.Time, energy float64, fixed bool) (planner.ChargeDemand, bool) {
	if energy <= 0 || deadline.IsZero() {
		return planner.ChargeDemand{}, false
	}
	power := lp.heatingPlanPowerLocked()
	if power <= 0 {
		return planner.ChargeDemand{}, false
	}

	required := lp.energyDuration(energy, power)
	if required <= 0 {
		return planner.ChargeDemand{}, false
	}

	var preferred api.Rates
	continuous := lp.getEffectivePlanStrategy().Continuous || lp.chargerHasFeature(api.Heating)
	if fixed {
		start := deadline.Add(-required)
		preferred = api.Rates{{Start: start, End: deadline}}
	} else {
		preferred = lp.GetPlan(deadline, required, lp.getEffectivePlanStrategy().Precondition, continuous)
		if len(preferred) == 0 {
			now := lp.clock.Now()
			start := deadline.Add(-required)
			if start.Before(now) {
				start = now
			}
			if deadline.After(start) {
				preferred = api.Rates{{Start: start, End: deadline}}
			}
		}
	}
	if len(preferred) == 0 {
		return planner.ChargeDemand{}, false
	}

	return planner.ChargeDemand{
		RequiredWh: energy * 1e3,
		MaxW:       power,
		Deadline:   deadline,
		Preferred:  preferred,
		Continuous: continuous || fixed,
	}, true
}

func (lp *Loadpoint) HorizonChargeDemands(slots []planner.BatterySlot) []planner.ChargeDemand {
	lp.RLock()
	defer lp.RUnlock()
	if len(slots) == 0 {
		return nil
	}

	from, to := slots[0].Start, slots[len(slots)-1].End
	var demands []planner.ChargeDemand

	if lp.planEnergy > 0 && !lp.planTime.IsZero() && lp.planTime.After(from.Add(-tariff.SlotDuration)) {
		if d, ok := lp.chargeDemand(lp.planTime, lp.planEnergy, false); ok {
			demands = append(demands, d)
		}
	}

	for _, rp := range lp.repeatingPlans {
		if !rp.Active || rp.Energy <= 0 || len(rp.Weekdays) == 0 {
			continue
		}
		times, err := util.GetOccurrences(rp.Weekdays, rp.Time, rp.Tz, from.Add(-12*time.Hour), to.Add(12*time.Hour))
		if err != nil {
			continue
		}
		for _, t := range times {
			deadline := t
			if rp.Fixed {
				deadline = t.Add(lp.energyDuration(rp.Energy, lp.heatingPlanPowerLocked()))
			}
			energy := rp.Energy
			if lp.repeatingPlanOffsetSet && deadline.Equal(lp.repeatingPlanEnd) {
				energy = lp.remainingPlanEnergy(rp.Energy)
			}
			if d, ok := lp.chargeDemand(deadline, energy, rp.Fixed); ok {
				demands = append(demands, d)
			}
		}
	}

	return demands
}
