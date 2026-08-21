package loadpoint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRebuildHeatingPattern(t *testing.T) {
	start := time.Date(2026, 8, 21, 4, 0, 0, 0, time.UTC)
	var boosts []HeatingBoost
	for i := range 4 {
		boosts = append(boosts, HeatingBoost{
			Start:     start,
			End:       start.Add(50 * time.Minute),
			StartTemp: 38,
			EndTemp:   48,
			EnergyWh:  2100,
			PeakW:     2800,
			ExtraW:    []float64{2800, 1200, 800},
			Quality:   HeatingBoostGood,
		})
		_ = i
	}

	p := RebuildHeatingPattern(boosts)
	require.Len(t, p.Bands, 1)
	band, ok := p.MatchBand(39)
	require.True(t, ok)
	assert.InDelta(t, 5, band.MinutesPerK, 0.1)
	assert.InDelta(t, 210, band.WhPerK, 0.1)
	assert.Equal(t, 2800.0, band.PeakW)

	d, energy, peak, learned := p.Estimate(39, 48, 2*time.Hour, 2000)
	assert.True(t, learned)
	assert.Equal(t, 45*time.Minute, d)
	assert.InDelta(t, 1890, energy, 1)
	assert.Equal(t, 2800.0, peak)
}

func TestClassifyBoost(t *testing.T) {
	start := time.Now()
	assert.Equal(t, HeatingBoostShort, ClassifyBoost(HeatingBoost{
		Start: start, End: start.Add(10 * time.Second),
	}, time.Minute))
	assert.Equal(t, HeatingBoostNoisy, ClassifyBoost(HeatingBoost{
		Start: start, End: start.Add(20 * time.Minute), StartTemp: 40, EndTemp: 40.5,
	}, time.Minute))
	assert.Equal(t, HeatingBoostGood, ClassifyBoost(HeatingBoost{
		Start: start, End: start.Add(20 * time.Minute), StartTemp: 38, EndTemp: 46,
	}, time.Minute))
}
