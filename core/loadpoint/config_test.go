package loadpoint

import (
	"testing"

	"github.com/evcc-io/evcc/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestSplitConfigUI(t *testing.T) {
	payload := map[string]any{
		"title": "Water Heater",
		"ui": map[string]any{
			"minTemp": 20.0,
			"maxTemp": 45.0,
		},
	}

	dynamic, other, err := SplitConfig(payload)
	require.NoError(t, err)

	assert.Equal(t, 20.0, dynamic.UI.MinTemp)
	assert.Equal(t, 45.0, dynamic.UI.MaxTemp)
	assert.NotContains(t, other, "ui")
}

func TestSplitConfigStoresHeatingAndRepeatingPlans(t *testing.T) {
	payload := map[string]any{
		"charger": "heatpump",
		"repeatingPlans": []any{
			map[string]any{"active": true, "energy": 4.5},
		},
		"heatingComfort": map[string]any{"minTemp": 21.0, "assumedPowerW": 800.0},
		"heatingBoosts": []any{
			map[string]any{"startTemp": 18.0, "endTemp": 22.0, "energyWh": 1200.0},
		},
		"heatingPattern": map[string]any{
			"bands": []any{map[string]any{"peakW": 900.0}},
		},
	}

	dynamic, other, err := SplitConfig(payload)
	require.NoError(t, err)

	assert.Equal(t, "heatpump", other["charger"])
	assert.NotContains(t, other, "repeatingPlans")
	assert.NotContains(t, other, "heatingComfort")
	assert.NotContains(t, other, "heatingBoosts")
	assert.NotContains(t, other, "heatingPattern")
	require.Len(t, dynamic.RepeatingPlans, 1)
	assert.Equal(t, 21.0, dynamic.HeatingComfort.MinTemp)
	assert.Equal(t, 800.0, dynamic.HeatingComfort.AssumedPowerW)
	require.Len(t, dynamic.HeatingBoosts, 1)
	assert.Equal(t, 18.0, dynamic.HeatingBoosts[0].StartTemp)
	require.Len(t, dynamic.HeatingPattern.Bands, 1)
	assert.Equal(t, 900.0, dynamic.HeatingPattern.Bands[0].PeakW)
}

func TestSplitConfigStripsSmartCostLimitEnergy(t *testing.T) {
	payload := map[string]any{
		"charger":              "wallbox",
		"smartCostLimitEnergy": true,
	}

	dynamic, other, err := SplitConfig(payload)
	require.NoError(t, err)

	assert.Equal(t, "wallbox", other["charger"])
	assert.NotContains(t, other, "smartCostLimitEnergy")
	require.NotNil(t, dynamic.SmartCostLimitEnergy)
	assert.True(t, *dynamic.SmartCostLimitEnergy)
}

func expectApplyBase(lp *MockAPI) {
	lp.EXPECT().SetTitle("")
	lp.EXPECT().SetPriority(0)
	lp.EXPECT().SetSmartCostLimit(nil)
	lp.EXPECT().SetSmartFeedInPriorityLimit(nil)
	lp.EXPECT().SetThresholds(ThresholdsConfig{})
	lp.EXPECT().SetPlanEnergy(gomock.Any(), 0.0)
	lp.EXPECT().SetPlanStrategy(api.PlanStrategy{})
	lp.EXPECT().SetBatteryBoostLimit(0)
	lp.EXPECT().SetBatteryDischargeExclude(false)
	lp.EXPECT().SetLimitEnergy(0.0)
	lp.EXPECT().SetLimitSoc(0)
	lp.EXPECT().SetSocConfig(SocConfig{})
	lp.EXPECT().SetUI(UIConfig{})
	lp.EXPECT().SetDefaultMode(api.ModeEmpty)
	lp.EXPECT().SetPhasesConfigured(0).Return(nil)
}

func TestApplyStoresHeatingAndRepeatingPlans(t *testing.T) {
	ctrl := gomock.NewController(t)
	lp := NewMockAPI(ctrl)

	payload := DynamicConfig{
		RepeatingPlans: []api.RepeatingPlan{{Active: true, Energy: 3}},
		HeatingComfort: HeatingComfort{MinTemp: 21},
		HeatingBoosts:  []HeatingBoost{{StartTemp: 18, EndTemp: 22}},
		HeatingPattern: HeatingPattern{Bands: []HeatingBand{{PeakW: 900}}},
	}

	expectApplyBase(lp)
	lp.EXPECT().SetRepeatingPlans(payload.RepeatingPlans).Return(nil)
	lp.EXPECT().SetHeatingComfort(payload.HeatingComfort).Return(nil)
	lp.EXPECT().SetHeatingHistory(payload.HeatingBoosts, payload.HeatingPattern).Return(nil)

	require.NoError(t, payload.Apply(lp))
}

func TestApplyKeepsHeatingHistoryWhenOmitted(t *testing.T) {
	ctrl := gomock.NewController(t)
	lp := NewMockAPI(ctrl)

	expectApplyBase(lp)
	lp.EXPECT().SetRepeatingPlans(nil).Return(nil)
	lp.EXPECT().SetHeatingComfort(HeatingComfort{}).Return(nil)

	require.NoError(t, DynamicConfig{}.Apply(lp))
}
