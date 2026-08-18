package metrics

import (
	"errors"
	"time"

	"github.com/evcc-io/evcc/server/db"
	"github.com/evcc-io/evcc/tariff"
)

var ErrIncomplete = errors.New("meter profile incomplete")

// energyProfile returns a 15min average meter profile in Wh. The profile
// is sorted by timestamp starting at 00:00. It is guaranteed to contain 96 15min values.
func energyProfile(entity entity, from time.Time) (*[96]float64, error) {
	db, err := db.Instance.DB()
	if err != nil {
		return nil, err
	}

	// COALESCE guards against legacy rows with NULL energy
	rows, err := db.Query(`SELECT min(ts) AS ts, COALESCE(avg(energy), 0) AS energy
		FROM meters
		WHERE meter = ? AND ts >= ?
		GROUP BY strftime("%H:%M", ts, 'unixepoch', 'localtime')
		ORDER BY strftime("%H:%M", ts, 'unixepoch', 'localtime') ASC`,
		entity.Id, from.Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prev time.Time
	res := make([]float64, 0, 96)

	for rows.Next() {
		var ts SqlTime
		var val float64

		if err := rows.Scan(&ts, &val); err != nil {
			return nil, err
		}

		// interpolate single missing value, maybe due to regular restarts?
		if time.Time(ts).Sub(prev) == 2*tariff.SlotDuration {
			res = append(res, (val+res[len(res)-1])/2)
		}
		prev = time.Time(ts)

		res = append(res, val)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(res) != 96 {
		return nil, ErrIncomplete
	}

	return (*[96]float64)(res), nil
}

func weekend(d time.Weekday) bool {
	return d == time.Saturday || d == time.Sunday
}

func avg(sum float64, n int) float64 {
	if n <= 0 {
		return 0
	}
	return sum / float64(n)
}

// energyProfileWeekday returns a 15min average meter profile in kWh for the given
// weekday. Same-weekday samples are preferred, then weekday/weekend, then all days.
func energyProfileWeekday(entity entity, from time.Time, weekday time.Weekday) (*[96]float64, error) {
	db, err := db.Instance.DB()
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(`SELECT ts, COALESCE(energy, 0) FROM meters WHERE meter = ? AND ts >= ?`, entity.Id, from.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type acc struct {
		sum float64
		n   int
	}
	var same, class, all [96]acc

	for rows.Next() {
		var ts int64
		var energy float64
		if err := rows.Scan(&ts, &energy); err != nil {
			return nil, err
		}
		t := time.Unix(ts, 0).Local()
		slot := t.Hour()*4 + t.Minute()/15
		if slot < 0 || slot > 95 {
			continue
		}
		all[slot].sum += energy
		all[slot].n++
		if t.Weekday() == weekday {
			same[slot].sum += energy
			same[slot].n++
		}
		if weekend(t.Weekday()) == weekend(weekday) {
			class[slot].sum += energy
			class[slot].n++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var res [96]float64
	filled := 0
	for i := range res {
		switch {
		case same[i].n > 0:
			res[i] = avg(same[i].sum, same[i].n)
		case class[i].n > 0:
			res[i] = avg(class[i].sum, class[i].n)
		case all[i].n > 0:
			res[i] = avg(all[i].sum, all[i].n)
		default:
			continue
		}
		filled++
	}
	if filled != 96 {
		return nil, ErrIncomplete
	}

	return &res, nil
}
