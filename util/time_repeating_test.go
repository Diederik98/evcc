package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetNextOccurrenceEmptyTimezone(t *testing.T) {
	weekdays := []int{0, 1, 2, 3, 4, 5, 6}
	got, err := GetNextOccurrence(weekdays, "18:00", "")
	require.NoError(t, err)
	assert.False(t, got.IsZero())
	assert.Contains(t, weekdays, int(got.Weekday()))
}

func TestGetNextOccurrenceAllWeekdaysBeforeDeadline(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Brussels")
	require.NoError(t, err)

	// Saturday 09:20 → next 18:00 is same day
	from := time.Date(2026, 8, 22, 9, 20, 0, 0, loc)
	to := from.Add(24 * time.Hour)
	got, err := GetOccurrences([]int{0, 1, 2, 3, 4, 5, 6}, "18:00", "Europe/Brussels", from, to)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 18, got[0].In(loc).Hour())
	assert.Equal(t, time.Saturday, got[0].In(loc).Weekday())
}
