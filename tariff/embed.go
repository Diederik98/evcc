package tariff

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/plugin/golang/stdlib"
	"github.com/evcc-io/evcc/tariff/fixed"
	"github.com/traefik/yaegi/interp"
)

type embed struct {
	Features_ []api.Feature `mapstructure:"features"`

	Charges       float64             `mapstructure:"charges"`
	ChargesZones_ []chargesZoneConfig `mapstructure:"chargesZones"`
	Tax           float64             `mapstructure:"tax"`
	Formula       string              `mapstructure:"formula"`

	chargesZones fixed.Zones
	calc         func(price, charges float64, ts time.Time) (float64, error)
}

type chargesZoneConfig struct {
	Charges             float64
	Days, Hours, Months string
}

func (t *embed) init() (err error) {
	defer func() {
		if r := recover(); r != nil && err == nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()

	specs := make([]fixed.ZoneSpec, len(t.ChargesZones_))
	for i, c := range t.ChargesZones_ {
		specs[i] = fixed.ZoneSpec{Price: c.Charges, Days: c.Days, Hours: c.Hours, Months: c.Months}
	}
	zones, err := fixed.ParseZones(specs)
	if err != nil {
		return err
	}
	t.chargesZones = zones

	if t.Formula == "" {
		return nil
	}

	t.calc = func(price, charges float64, ts time.Time) (float64, error) {
		vm := interp.New(interp.Options{})
		if err := vm.Use(stdlib.Symbols); err != nil {
			return 0, err
		}
		vm.ImportUsed()

		if _, err := vm.Eval(fmt.Sprintf(`
		var (
			price float64 = %f
			charges float64 = %f
			tax float64 = %f
			ts = time.Unix(%d, 0).Local()
		)`, price, charges, t.Tax, ts.Unix())); err != nil {
			return 0, err
		}

		res, err := vm.Eval(t.Formula)
		if err != nil {
			return 0, err
		}

		if !res.CanFloat() {
			return 0, errors.New("formula did not return a float value")
		}

		return res.Float(), nil
	}

	// test the formula
	_, err = t.calc(0, t.Charges, time.Now())

	return err
}

// effectiveCharges resolves the charge for ts in local time; later zones win.
func (t *embed) effectiveCharges(ts time.Time) float64 {
	if len(t.chargesZones) == 0 {
		return t.Charges
	}

	ts = ts.Local()
	day := fixed.Day(int(ts.Weekday()))
	month := fixed.Month(ts.Month() - 1)
	hm := fixed.HourMin{Hour: ts.Hour(), Min: ts.Minute()}

	zones := t.chargesZones.ForDayAndMonth(day, month)
	for _, z := range slices.Backward(zones) {
		if z.Hours.Contains(hm) {
			return z.Price
		}
	}
	return t.Charges
}

func (t *embed) totalPrice(price float64, ts time.Time) float64 {
	charges := t.effectiveCharges(ts)
	if t.calc != nil {
		res, _ := t.calc(price, charges, ts)
		return res
	}
	return (price + charges) * (1 + t.Tax)
}

// rate builds a slot with source energy price and all-in Value (charges, tax, formula).
func (t *embed) rate(start, end time.Time, energy float64) api.Rate {
	value := energy
	if t != nil {
		value = t.totalPrice(energy, start)
	}
	return api.Rate{
		Start:  start,
		End:    end,
		Energy: api.NewEnergy(energy),
		Value:  value,
	}
}

// PriceCharges returns configured grid charges (before tax).
func (t *embed) PriceCharges() float64 {
	if t == nil {
		return 0
	}
	return t.Charges
}

// PriceTax returns configured VAT/tax as a fraction (0.06 = 6%).
func (t *embed) PriceTax() float64 {
	if t == nil {
		return 0
	}
	return t.Tax
}

// HasFormula reports whether a custom total-price formula is configured.
func (t *embed) HasFormula() bool {
	return t != nil && t.Formula != ""
}

var _ api.FeatureDescriber = (*embed)(nil)

func (t *embed) Features() []api.Feature {
	return t.Features_
}
