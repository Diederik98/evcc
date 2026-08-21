package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHouseholdPowerExcludesCharger(t *testing.T) {
	site := &Site{
		gridPower: 13000,
		pvPower:   0,
	}
	site.battery.Power = 0
	assert.Equal(t, 13000.0, site.householdPower())
}

func TestBatteryPlanSlotCountUsesAllPrices(t *testing.T) {
	assert.Equal(t, 96, batteryPlanSlotCount(0, 0, 0, 0))
	assert.Equal(t, 192, batteryPlanSlotCount(192, 10, 8, 96))
	assert.Equal(t, 96*7, batteryPlanSlotCount(2000, 0, 0, 96))
}

func TestExtendProfileRepeatsWeekday(t *testing.T) {
	home := []float64{1, 2, 3, 4}
	got := extendProfile(home, 10)
	assert.Equal(t, []float64{1, 2, 3, 4, 1, 2, 3, 4, 1, 2}, got)
	assert.Equal(t, home, extendProfile(home, 4))
}
