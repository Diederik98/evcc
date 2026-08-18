package metrics

import (
	"testing"
	"time"

	"github.com/evcc-io/evcc/server/db"
	"github.com/stretchr/testify/require"
)

func persistDay(t *testing.T, e entity, day time.Time, energy float64) {
	t.Helper()
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	for i := range 96 {
		require.NoError(t, persist(e, start.Add(time.Duration(i)*15*time.Minute), energy, 0, nil))
	}
}

func TestEnergyProfileWeekdayPrefersSameWeekday(t *testing.T) {
	require.NoError(t, db.NewInstance("sqlite", ":memory:"))
	require.NoError(t, SetupSchema())

	e := entity{Name: Home, Group: Home}
	require.NoError(t, db.Instance.FirstOrCreate(&e).Error)

	loc := time.Local
	tuesday := time.Date(2026, 8, 18, 0, 0, 0, 0, loc)  // Tuesday
	saturday := time.Date(2026, 8, 22, 0, 0, 0, 0, loc) // Saturday
	require.Equal(t, time.Tuesday, tuesday.Weekday())
	require.Equal(t, time.Saturday, saturday.Weekday())

	persistDay(t, e, tuesday, 1)
	persistDay(t, e, saturday, 5)

	from := tuesday.AddDate(0, 0, -1)
	tue, err := energyProfileWeekday(e, from, time.Tuesday)
	require.NoError(t, err)
	require.InDelta(t, 1, tue[0], 1e-9)
	require.InDelta(t, 1, tue[40], 1e-9)

	sat, err := energyProfileWeekday(e, from, time.Saturday)
	require.NoError(t, err)
	require.InDelta(t, 5, sat[0], 1e-9)

	all, err := energyProfile(e, from)
	require.NoError(t, err)
	require.InDelta(t, 3, all[0], 1e-9)
}

func TestEnergyProfileWeekdayFallsBackToWeekdayClass(t *testing.T) {
	require.NoError(t, db.NewInstance("sqlite", ":memory:"))
	require.NoError(t, SetupSchema())

	e := entity{Name: Home, Group: Home}
	require.NoError(t, db.Instance.FirstOrCreate(&e).Error)

	loc := time.Local
	monday := time.Date(2026, 8, 17, 0, 0, 0, 0, loc)
	require.Equal(t, time.Monday, monday.Weekday())
	persistDay(t, e, monday, 2)

	from := monday.AddDate(0, 0, -1)
	wed, err := energyProfileWeekday(e, from, time.Wednesday)
	require.NoError(t, err)
	require.InDelta(t, 2, wed[10], 1e-9)

	sat, err := energyProfileWeekday(e, from, time.Saturday)
	require.NoError(t, err)
	require.InDelta(t, 2, sat[10], 1e-9, "weekend falls back to all-days when no weekend samples exist")
}
