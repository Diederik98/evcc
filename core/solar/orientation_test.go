package solar

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClearSkyPOAEastPeaksBeforeWest(t *testing.T) {
	lat, lon := 51.0, 4.4
	day := time.Date(2026, 6, 21, 0, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	morning := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, day.Location())
	afternoon := time.Date(day.Year(), day.Month(), day.Day(), 17, 0, 0, 0, day.Location())

	eastMorning := clearSkyPOA(lat, lon, 35, -90, morning)
	westMorning := clearSkyPOA(lat, lon, 35, 90, morning)
	eastAfternoon := clearSkyPOA(lat, lon, 35, -90, afternoon)
	westAfternoon := clearSkyPOA(lat, lon, 35, 90, afternoon)

	assert.Greater(t, eastMorning, westMorning)
	assert.Greater(t, westAfternoon, eastAfternoon)
}

func TestIsClearSkyRejectsCloudyDay(t *testing.T) {
	var clear dayProfile
	clear.date = time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	for h := 6; h <= 18; h++ {
		x := float64(h-12) / 6
		clear.hours[h] = max(0, 1.2*(1-x*x))
	}
	assert.True(t, isClearSky(clear))

	cloudy := clear
	cloudy.hours[10] = 0.05
	cloudy.hours[11] = 0.9
	cloudy.hours[12] = 0.08
	cloudy.hours[13] = 0.85
	assert.False(t, isClearSky(cloudy))
}

func TestSuggestOrientationFromClearSky(t *testing.T) {
	lat, lon := 51.0, 4.4
	loc := time.FixedZone("CEST", 2*3600)
	trueAz, trueDec := -45.0, 35.0

	var slots []Slot
	for d := 0; d < 5; d++ {
		day := time.Date(2026, 6, 15+d, 0, 0, 0, 0, loc)
		for h := 5; h <= 21; h++ {
			t0 := time.Date(day.Year(), day.Month(), day.Day(), h, 0, 0, 0, loc)
			poa := clearSkyPOA(lat, lon, trueDec, trueAz, t0.Add(30*time.Minute))
			slots = append(slots, Slot{Start: t0, Energy: poa / 1000})
		}
	}

	sug, ok := SuggestOrientation(Geometry{Lat: lat, Lon: lon}, slots)
	require.True(t, ok)
	assert.InDelta(t, trueAz, sug.Azimuth, 20)
	assert.InDelta(t, trueDec, sug.Decline, 15)
	assert.GreaterOrEqual(t, sug.Days, 3)
}
