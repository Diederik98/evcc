package util

import (
	"fmt"
	"slices"
	"time"
)

// GetNextOccurrence returns the next occurrence of the given time on the specified weekdays.
func GetNextOccurrence(weekdays []int, timeStr string, tz string) (time.Time, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone: %w", err)
	}

	parsedTime, err := time.Parse("15:04", timeStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid time format, expected HH:MM: %w", err)
	}

	hour, minute := parsedTime.Hour(), parsedTime.Minute()

	now := time.Now().In(loc)

	target := time.Date(
		now.Year(), now.Month(), now.Day(),
		hour, minute, 0, 0, loc,
	)

	// If the target time has passed today, start from tomorrow
	if target.Before(now) {
		target = target.AddDate(0, 0, 1)
	}

	// Check the next 7 days for a valid match
	for range 7 {
		weekday := int(target.Weekday())
		if slices.Contains(weekdays, weekday) {
			return target, nil
		}
		target = target.AddDate(0, 0, 1)
	}

	return time.Time{}, fmt.Errorf("no valid weekday found")
}

// GetOccurrences returns matching weekday times in [from, to).
func GetOccurrences(weekdays []int, timeStr, tz string, from, to time.Time) ([]time.Time, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone: %w", err)
	}

	parsedTime, err := time.Parse("15:04", timeStr)
	if err != nil {
		return nil, fmt.Errorf("invalid time format, expected HH:MM: %w", err)
	}

	if to.Before(from) || len(weekdays) == 0 {
		return nil, nil
	}

	hour, minute := parsedTime.Hour(), parsedTime.Minute()
	start := from.In(loc)
	target := time.Date(start.Year(), start.Month(), start.Day(), hour, minute, 0, 0, loc)
	if !target.After(from) && !target.Equal(from) {
		target = target.AddDate(0, 0, 1)
	}

	var res []time.Time
	for !target.After(to) && !target.Equal(to) {
		if slices.Contains(weekdays, int(target.Weekday())) && !target.Before(from) {
			res = append(res, target)
		}
		target = target.AddDate(0, 0, 1)
		if len(res) > 14 {
			break
		}
	}
	return res, nil
}
