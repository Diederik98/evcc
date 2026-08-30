package core

import (
	"testing"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/util"
	"github.com/stretchr/testify/assert"
)

func TestSmartLimitActiveEnergy(t *testing.T) {
	lp := NewLoadpoint(util.NewLogger("foo"), nil)
	limit := 0.05
	lp.smartCostLimit = &limit
	lp.smartCostLimitEnergy = true

	rates := api.Rates{{
		Start:  time.Now().Add(-time.Hour),
		End:    time.Now().Add(time.Hour),
		Value:  0.21,
		Energy: api.NewEnergy(0.04),
	}}

	assert.True(t, lp.smartLimitActive(lp.GetSmartCostLimit(), rates, true))

	lp.smartCostLimitEnergy = false
	assert.False(t, lp.smartLimitActive(lp.GetSmartCostLimit(), rates, true))
}
