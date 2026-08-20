package solar

import (
	"math"
	"time"
)

// Slot is one production interval in kWh.
type Slot struct {
	Start  time.Time
	Energy float64
}

// Geometry is the configured solar forecast orientation.
type Geometry struct {
	Lat, Lon float64
	Az, Dec  float64
	HasAzDec bool
}

// Suggestion is a clear-sky fit of azimuth and tilt.
type Suggestion struct {
	Azimuth       float64  `json:"azimuth"`
	Decline       float64  `json:"decline"`
	Days          int      `json:"days"`
	ConfiguredAz  *float64 `json:"configuredAzimuth,omitempty"`
	ConfiguredDec *float64 `json:"configuredDecline,omitempty"`
}

const (
	minClearDays   = 3
	minCorrelation = 0.9
)

// SuggestOrientation fits forecast.solar azimuth/tilt using only clear-sky days.
func SuggestOrientation(geo Geometry, slots []Slot) (Suggestion, bool) {
	if geo.Lat == 0 && geo.Lon == 0 {
		return Suggestion{}, false
	}

	days := clearSkyDays(slots)
	if len(days) < minClearDays {
		return Suggestion{}, false
	}

	bestAz, bestDec, bestCorr := 0.0, 30.0, -1.0
	for az := -90.0; az <= 90; az += 15 {
		for dec := 10.0; dec <= 55; dec += 5 {
			corr := orientationCorr(geo.Lat, geo.Lon, az, dec, days)
			if corr > bestCorr {
				bestCorr, bestAz, bestDec = corr, az, dec
			}
		}
	}
	if bestCorr < minCorrelation {
		return Suggestion{}, false
	}

	for az := bestAz - 10; az <= bestAz+10; az += 5 {
		for dec := bestDec - 5; dec <= bestDec+5; dec += 5 {
			if dec < 0 || dec > 60 {
				continue
			}
			corr := orientationCorr(geo.Lat, geo.Lon, az, dec, days)
			if corr > bestCorr {
				bestCorr, bestAz, bestDec = corr, az, dec
			}
		}
	}

	sug := Suggestion{Azimuth: bestAz, Decline: bestDec, Days: len(days)}
	if geo.HasAzDec {
		az, dec := geo.Az, geo.Dec
		sug.ConfiguredAz = &az
		sug.ConfiguredDec = &dec
	}
	return sug, true
}

type dayProfile struct {
	date  time.Time
	hours [24]float64
}

func clearSkyDays(slots []Slot) []dayProfile {
	byDay := map[string]*dayProfile{}
	for _, s := range slots {
		if s.Energy <= 0 {
			continue
		}
		loc := s.Start.Location()
		day := time.Date(s.Start.Year(), s.Start.Month(), s.Start.Day(), 0, 0, 0, 0, loc)
		key := day.Format("2006-01-02")
		p, ok := byDay[key]
		if !ok {
			p = &dayProfile{date: day}
			byDay[key] = p
		}
		p.hours[s.Start.Hour()] += s.Energy
	}

	var days []dayProfile
	for _, p := range byDay {
		if isClearSky(*p) {
			days = append(days, *p)
		}
	}
	return days
}

func isClearSky(p dayProfile) bool {
	var energy float64
	var daylight []float64
	for _, e := range p.hours {
		energy += e
		if e > 0.02 {
			daylight = append(daylight, e)
		}
	}
	if energy < 0.8 || len(daylight) < 6 {
		return false
	}

	peak := 0
	for h, e := range p.hours {
		if e > p.hours[peak] {
			peak = h
		}
	}
	if peak < 8 || peak > 18 {
		return false
	}

	var jagged float64
	n := 0
	for h := 1; h < 23; h++ {
		if p.hours[h] <= 0.02 {
			continue
		}
		jagged += math.Abs(p.hours[h-1] - 2*p.hours[h] + p.hours[h+1])
		n++
	}
	if n < 4 || jagged/energy > 0.35 {
		return false
	}

	// rise then fall, allowing small noise
	for h := 1; h < peak; h++ {
		if p.hours[h] < p.hours[h-1]*0.65 && p.hours[h-1] > 0.05 {
			return false
		}
	}
	for h := peak + 1; h < 24; h++ {
		if p.hours[h] > p.hours[h-1]/0.65+0.05 && p.hours[h-1] > 0.05 && p.hours[h] > 0.05 {
			return false
		}
	}
	return true
}

func orientationCorr(lat, lon, az, dec float64, days []dayProfile) float64 {
	var xs, ys []float64
	for _, d := range days {
		for h := 6; h <= 20; h++ {
			t := time.Date(d.date.Year(), d.date.Month(), d.date.Day(), h, 30, 0, 0, d.date.Location())
			poa := clearSkyPOA(lat, lon, dec, az, t)
			if poa < 20 && d.hours[h] < 0.02 {
				continue
			}
			xs = append(xs, d.hours[h])
			ys = append(ys, poa)
		}
	}
	return correlation(xs, ys)
}

func correlation(x, y []float64) float64 {
	n := float64(len(x))
	if n < 8 || len(x) != len(y) {
		return -1
	}
	var sx, sy, sxx, syy, sxy float64
	for i := range x {
		sx += x[i]
		sy += y[i]
		sxx += x[i] * x[i]
		syy += y[i] * y[i]
		sxy += x[i] * y[i]
	}
	num := n*sxy - sx*sy
	den := math.Sqrt((n*sxx - sx*sx) * (n*syy - sy*sy))
	if den <= 0 {
		return -1
	}
	return num / den
}
