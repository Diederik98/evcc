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
