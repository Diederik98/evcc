package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetOccurrences(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Brussels")
	require.NoError(t, err)
	from := time.Date(2026, 8, 24, 12, 0, 0, 0, loc) // Monday
	to := from.Add(48 * time.Hour)

	got, err := GetOccurrences([]int{1, 2, 3, 4, 5}, "07:00", "Europe/Brussels", from, to)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, time.Tuesday, got[0].Weekday())
	assert.Equal(t, 7, got[0].Hour())
	assert.Equal(t, time.Wednesday, got[1].Weekday())
}
