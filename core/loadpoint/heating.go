package loadpoint

import (
	"math"
	"time"
)

const (
	HeatingBoostGood   = "good"
	HeatingBoostNoisy  = "noisy"
	HeatingBoostShort  = "short"
	maxHeatingBoosts   = 40
	heatingBandWidth   = 5.0
	minHeatingBoosts   = 3
	minHeatingDeltaK   = 2.0
	defaultHysteresisK = 1.5
)

// MaxHeatingBoostsStored is the number of boost episodes kept for learning.
func MaxHeatingBoostsStored() int {
	return maxHeatingBoosts
}

// HeatingComfort is the live comfort policy for a heating loadpoint.
type HeatingComfort struct {
	MinTemp          float64 `json:"minTemp"`          // start floor in °C, 0 disables
	Hysteresis       float64 `json:"hysteresis"`       // stop at min+hysteresis
	MinOnTime        int64   `json:"minOnTime"`        // seconds
	AssumedPowerW    float64 `json:"assumedPowerW"`    // extra watts while boosted, 0 = use max power
	MaxAssumedPowerW float64 `json:"maxAssumedPowerW"` // cap for residual inference
	StopTemp         float64 `json:"stopTemp"`         // comfort ceiling °C, 0 = charger limit. Calendar plans ignore this.
}

// HeatingBoost is one completed force-heat episode.
type HeatingBoost struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	StartTemp float64   `json:"startTemp"`
	EndTemp   float64   `json:"endTemp"`
	EnergyWh  float64   `json:"energyWh"`
	PeakW     float64   `json:"peakW"`
	ExtraW    []float64 `json:"extraW,omitempty"`
	Quality   string    `json:"quality"`
	Reason    string    `json:"reason"`
	Estimated bool      `json:"estimated"`
}

// HeatingBand is the learned extra-heat pattern for a start-temperature range.
type HeatingBand struct {
	MinStartTemp float64   `json:"minStartTemp"`
	MaxStartTemp float64   `json:"maxStartTemp"`
	MinutesPerK  float64   `json:"minutesPerK"`
	WhPerK       float64   `json:"whPerK"`
	PeakW        float64   `json:"peakW"`
	Shape        []float64 `json:"shape,omitempty"`
	Samples      int       `json:"samples"`
}

// HeatingPattern is the set of start-temperature bands.
type HeatingPattern struct {
	Bands []HeatingBand `json:"bands,omitempty"`
}

// HeatingStatus is published for the UI.
type HeatingStatus struct {
	Comfort   HeatingComfort `json:"comfort"`
	Boosts    []HeatingBoost `json:"boosts,omitempty"`
	Pattern   HeatingPattern `json:"pattern"`
	Active    bool           `json:"active"`
	Estimated bool           `json:"estimated"`
	Reason    string         `json:"reason,omitempty"`
	StartTemp float64        `json:"startTemp,omitempty"`
}

// EffectiveHysteresis returns the stop offset in K.
func (c HeatingComfort) EffectiveHysteresis() float64 {
	if c.Hysteresis > 0 {
		return c.Hysteresis
	}
	return defaultHysteresisK
}

// MinOnDuration returns the configured minimum on-time.
func (c HeatingComfort) MinOnDuration() time.Duration {
	if c.MinOnTime <= 0 {
		return 0
	}
	return time.Duration(c.MinOnTime) * time.Second
}

// MatchBand returns the learned band for a start temperature.
func (p HeatingPattern) MatchBand(startTemp float64) (HeatingBand, bool) {
	for _, b := range p.Bands {
		if startTemp >= b.MinStartTemp && startTemp < b.MaxStartTemp && b.Samples >= minHeatingBoosts {
			return b, true
		}
	}
	return HeatingBand{}, false
}

// Estimate returns duration and energy for heating from startTemp to stopTemp.
func (p HeatingPattern) Estimate(startTemp, stopTemp float64, fallbackDuration time.Duration, fallbackW float64) (time.Duration, float64, float64, bool) {
	delta := stopTemp - startTemp
	if delta < 0.5 {
		return 0, 0, fallbackW, true
	}
	band, ok := p.MatchBand(startTemp)
	if !ok || band.MinutesPerK <= 0 {
		energy := fallbackW * fallbackDuration.Hours()
		return fallbackDuration, energy, fallbackW, false
	}
	d := time.Duration(band.MinutesPerK*delta) * time.Minute
	if fallbackDuration > 0 && d > fallbackDuration {
		d = fallbackDuration
	}
	if d < time.Minute {
		d = time.Minute
	}
	energy := band.WhPerK * delta
	peak := band.PeakW
	if peak <= 0 {
		peak = fallbackW
	}
	return d, energy, peak, true
}

// RebuildHeatingPattern learns bands from good boost episodes.
func RebuildHeatingPattern(boosts []HeatingBoost) HeatingPattern {
	type acc struct {
		minT, maxT      float64
		minPerK, whPerK float64
		peak            float64
		shape           []float64
		n               int
	}
	byBand := map[int]*acc{}
	for _, b := range boosts {
		if b.Quality != HeatingBoostGood {
			continue
		}
		delta := b.EndTemp - b.StartTemp
		if delta < minHeatingDeltaK || b.End.Before(b.Start) {
			continue
		}
		idx := int(math.Floor(b.StartTemp / heatingBandWidth))
		a := byBand[idx]
		if a == nil {
			a = &acc{
				minT: float64(idx) * heatingBandWidth,
				maxT: float64(idx+1) * heatingBandWidth,
			}
			byBand[idx] = a
		}
		minutes := b.End.Sub(b.Start).Minutes()
		a.minPerK += minutes / delta
		a.whPerK += b.EnergyWh / delta
		if b.PeakW > a.peak {
			a.peak = b.PeakW
		}
		a.shape = mergeShape(a.shape, b.ExtraW)
		a.n++
	}

	var bands []HeatingBand
	for _, a := range byBand {
		if a.n <= 0 {
			continue
		}
		bands = append(bands, HeatingBand{
			MinStartTemp: a.minT,
			MaxStartTemp: a.maxT,
			MinutesPerK:  a.minPerK / float64(a.n),
			WhPerK:       a.whPerK / float64(a.n),
			PeakW:        a.peak,
			Shape:        a.shape,
			Samples:      a.n,
		})
	}
	return HeatingPattern{Bands: bands}
}

func mergeShape(avg, sample []float64) []float64 {
	if len(sample) == 0 {
		return avg
	}
	if len(avg) == 0 {
		return append([]float64(nil), sample...)
	}
	n := min(len(avg), len(sample))
	out := make([]float64, n)
	for i := range n {
		out[i] = (avg[i] + sample[i]) / 2
	}
	return out
}

// ClassifyBoost sets quality from duration, temperature rise, and noisy slots.
func ClassifyBoost(b HeatingBoost, minOn time.Duration) string {
	if b.End.Sub(b.Start) < min(time.Minute, minOn) {
		return HeatingBoostShort
	}
	if b.EndTemp-b.StartTemp < minHeatingDeltaK {
		return HeatingBoostNoisy
	}
	return HeatingBoostGood
}
