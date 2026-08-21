package settings

import (
	"testing"

	"github.com/evcc-io/evcc/core/loadpoint"
	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/util/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigSettingsJsonRoundTripMap(t *testing.T) {
	conf := &config.Config{Data: map[string]any{
		"heatingComfort": map[string]any{
			"minTemp":       21.0,
			"assumedPowerW": 800.0,
		},
	}}
	s := NewConfigSettingsAdapter(util.NewLogger("cfg"), conf)

	var c loadpoint.HeatingComfort
	require.NoError(t, s.Json("heatingComfort", &c))
	assert.Equal(t, 21.0, c.MinTemp)
	assert.Equal(t, 800.0, c.AssumedPowerW)
}

func TestConfigSettingsJsonRoundTripSlice(t *testing.T) {
	conf := &config.Config{Data: map[string]any{
		"heatingBoosts": []any{
			map[string]any{"startTemp": 18.0, "endTemp": 22.0, "energyWh": 1200.0},
		},
	}}
	s := NewConfigSettingsAdapter(util.NewLogger("cfg"), conf)

	var boosts []loadpoint.HeatingBoost
	require.NoError(t, s.Json("heatingBoosts", &boosts))
	require.Len(t, boosts, 1)
	assert.Equal(t, 18.0, boosts[0].StartTemp)
	assert.Equal(t, 1200.0, boosts[0].EnergyWh)
}

func TestConfigSettingsJsonRoundTripString(t *testing.T) {
	conf := &config.Config{Data: map[string]any{
		"heatingComfort": `{"minTemp":19,"hysteresis":1.5}`,
	}}
	s := NewConfigSettingsAdapter(util.NewLogger("cfg"), conf)

	var c loadpoint.HeatingComfort
	require.NoError(t, s.Json("heatingComfort", &c))
	assert.Equal(t, 19.0, c.MinTemp)
	assert.Equal(t, 1.5, c.Hysteresis)
}
