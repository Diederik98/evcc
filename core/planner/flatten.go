package planner

import (
	"cmp"
	"slices"
	"time"

	"github.com/evcc-io/evcc/api"
)

// ChargeDemand is planned charger or heater energy that can be flattened under the grid limit.
type ChargeDemand struct {
	RequiredWh float64
	MaxW       float64
	Deadline   time.Time
	Preferred  api.Rates
	Continuous bool // keep one contiguous window at max power
}

// FlattenChargeDemands spreads planned charging into household slots. When a
// grid threshold is set, each demand is filled under remaining import headroom
// first, preferring the original cheap slots, then other hours before the
// deadline. Energy that still does not fit is placed at max power so the
// battery planner can cover the residual peak.
func FlattenChargeDemands(slots []BatterySlot, demands []ChargeDemand, gridThresholdW float64) (addedWh float64, currentW []float64) {
	currentW = make([]float64, len(demands))
	if len(slots) == 0 {
		return 0, currentW
	}

	hours0 := slotHours(slots[0])
	for i, d := range demands {
		if d.RequiredWh <= 0 || d.MaxW <= 0 {
			continue
		}

		var alloc []float64
		if d.Continuous || gridThresholdW <= 0 || d.Deadline.IsZero() {
			before := slots[0].HomeWh
			addedWh += applyChargePlan(slots, d.Preferred, d.MaxW)
			if hours0 > 0 {
				currentW[i] = max(0, slots[0].HomeWh-before) / hours0
			}
			continue
		}

		alloc = flattenDemand(slots, d, gridThresholdW)
		for j, wh := range alloc {
			slots[j].HomeWh += wh
			slots[j].LoadWh += wh
			addedWh += wh
		}
		if hours0 > 0 {
			currentW[i] = alloc[0] / hours0
		}
	}

	return addedWh, currentW
}

func flattenDemand(slots []BatterySlot, d ChargeDemand, gridThresholdW float64) []float64 {
	alloc := make([]float64, len(slots))
	remaining := d.RequiredWh
	if remaining <= 0 {
		return alloc
	}

	preferred := preferredIndices(slots, d.Preferred)
	eligible := eligibleIndices(slots, d.Deadline)
	cheap := slices.Clone(eligible)
	slices.SortFunc(cheap, func(a, b int) int {
		return cmp.Compare(slots[a].Price, slots[b].Price)
	})

	fill := func(indices []int, respectHeadroom bool) {
		for _, i := range indices {
			if remaining <= 1 {
				return
			}
			h := overlapHours(slots[i].Start, slots[i].End, slots[i].Start, d.Deadline)
			if h <= 0 {
				continue
			}
			room := d.MaxW*h - alloc[i]
			if respectHeadroom {
				headroom := slotHeadroomWh(slots[i], gridThresholdW) - alloc[i]
				room = min(room, headroom)
			}
			if room <= 0 {
				continue
			}
			take := min(remaining, room)
			alloc[i] += take
			remaining -= take
		}
	}

	fill(preferred, true)
	fill(cheap, true)

	preferredLater := slices.Clone(preferred)
	slices.Reverse(preferredLater)
	later := slices.Clone(eligible)
	slices.Reverse(later)
	fill(preferredLater, false)
	fill(later, false)

	return alloc
}

func slotHeadroomWh(s BatterySlot, gridThresholdW float64) float64 {
	h := slotHours(s)
	if h <= 0 || gridThresholdW <= 0 {
		return 0
	}
	return max(0, gridThresholdW*h-max(0, s.HomeWh-s.SolarWh))
}

func preferredIndices(slots []BatterySlot, preferred api.Rates) []int {
	var idx []int
	for i, s := range slots {
		for _, r := range preferred {
			if overlapHours(s.Start, s.End, r.Start, r.End) > 0 {
				idx = append(idx, i)
				break
			}
		}
	}
	return idx
}

func eligibleIndices(slots []BatterySlot, deadline time.Time) []int {
	var idx []int
	for i, s := range slots {
		if overlapHours(s.Start, s.End, s.Start, deadline) > 0 {
			idx = append(idx, i)
		}
	}
	return idx
}

func applyChargePlan(slots []BatterySlot, plan api.Rates, powerW float64) float64 {
	if powerW <= 0 || len(plan) == 0 || len(slots) == 0 {
		return 0
	}

	var added float64
	for i := range slots {
		for _, r := range plan {
			h := overlapHours(slots[i].Start, slots[i].End, r.Start, r.End)
			if h <= 0 {
				continue
			}
			wh := powerW * h
			slots[i].HomeWh += wh
			slots[i].LoadWh += wh
			added += wh
		}
	}
	return added
}

func overlapHours(a0, a1, b0, b1 time.Time) float64 {
	start := a0
	if b0.After(start) {
		start = b0
	}
	end := a1
	if b1.Before(end) {
		end = b1
	}
	if !end.After(start) {
		return 0
	}
	return end.Sub(start).Hours()
}
