package core

import (
	"testing"

	"github.com/evcc-io/evcc/core/loadpoint"
	"github.com/evcc-io/evcc/core/settings"
	"github.com/evcc-io/evcc/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func persistedHeatingPayload() map[string]any {
	return map[string]any{
		"title":          "Heater",
		"repeatingPlans": []any{map[string]any{"active": true, "energy": 3.0}},
		"heatingComfort": map[string]any{"minTemp": 21.0},
		"heatingBoosts":  []any{map[string]any{"startTemp": 18.0, "endTemp": 22.0}},
		"heatingPattern": map[string]any{"bands": []any{map[string]any{"peakW": 900.0}}},
	}
}

func TestNewLoadpointFromConfigRejectsPersistedHeatingInStaticConfig(t *testing.T) {
	_, err := NewLoadpointFromConfig(util.NewLogger("lp"), settings.NewDatabaseSettingsAdapter("t."), nil, persistedHeatingPayload())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid keys")
}

func TestNewLoadpointFromConfigAfterSplitAcceptsPersistedHeating(t *testing.T) {
	dynamic, static, err := loadpoint.SplitConfig(persistedHeatingPayload())
	require.NoError(t, err)
	assert.Equal(t, 21.0, dynamic.HeatingComfort.MinTemp)
	require.Len(t, dynamic.RepeatingPlans, 1)
	require.Len(t, dynamic.HeatingBoosts, 1)
	require.Len(t, dynamic.HeatingPattern.Bands, 1)
	assert.Empty(t, static)

	_, err = NewLoadpointFromConfig(util.NewLogger("lp"), settings.NewDatabaseSettingsAdapter("t."), nil, static)
	require.EqualError(t, err, "missing charger")
}

func TestNewLoadpointFromConfigAfterSplitAcceptsSmartCostLimitEnergy(t *testing.T) {
	_, static, err := loadpoint.SplitConfig(map[string]any{
		"smartCostLimitEnergy": true,
	})
	require.NoError(t, err)
	assert.NotContains(t, static, "smartCostLimitEnergy")

	_, err = NewLoadpointFromConfig(util.NewLogger("lp"), settings.NewDatabaseSettingsAdapter("t."), nil, static)
	require.EqualError(t, err, "missing charger")
}
