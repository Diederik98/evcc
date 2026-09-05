package api

import (
	"encoding/json"
	"slices"
	"time"
)

// Rate is a grid tariff rate
type Rate struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Value float64   `json:"value"`
	// Energy is the source energy price before charges, tax, or formula.
	// Nil when the tariff has no separate source price (CO2, solar, all-in feeds).
	Energy *float64 `json:"energy,omitempty"`
}

// NewEnergy returns a distinct energy-price pointer.
func NewEnergy(v float64) *float64 {
	e := v
	return &e
}

// CloneEnergy copies an optional energy-price pointer.
func CloneEnergy(p *float64) *float64 {
	if p == nil {
		return nil
	}
	e := *p
	return &e
}

// Cost returns the source energy price when energy is true and Energy is set, otherwise Value.
func (r Rate) Cost(energy bool) float64 {
	if energy && r.Energy != nil {
		return *r.Energy
	}
	return r.Value
}

// IsZero returns is the rate is the zero value
func (r Rate) IsZero() bool {
	return r.Start.IsZero() && r.End.IsZero() && r.Value == 0
}

// Rates is a slice of (future) tariff rates
type Rates []Rate

// Sort rates by start time
func (rr Rates) Sort() {
	slices.SortStableFunc(rr, func(i, j Rate) int {
		return i.Start.Compare(j.Start)
	})
}

// At returns the rate for given timestamp or error.
// Rates MUST be sorted by start time.
func (rr Rates) At(ts time.Time) (Rate, error) {
	if i, ok := slices.BinarySearchFunc(rr, ts, func(r Rate, ts time.Time) int {
		switch {
		case ts.Before(r.Start):
			return +1
		case !ts.Before(r.End):
			return -1
		default:
			return 0
		}
	}); ok {
		return rr[i], nil
	}

	return Rate{}, ErrNotAvailable
}

var _ BytesMarshaler = (*Rates)(nil)

// MarshalBytes implements server.BytesMarshaler
func (r Rates) MarshalBytes() ([]byte, error) {
	return json.Marshal(r)
}
