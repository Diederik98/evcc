package tariff

import (
	"github.com/evcc-io/evcc/api"
)

type priceBreakdown interface {
	PriceCharges() float64
	PriceTax() float64
	HasFormula() bool
}

// PriceBreakdown returns grid charges, tax fraction, and whether a custom formula is set.
// Wrappers (slot, average, cache) are unwrapped until the underlying tariff embed is found.
func PriceBreakdown(t api.Tariff) (charges, tax float64, formula bool) {
	for range 8 {
		if t == nil {
			return 0, 0, false
		}
		if pb, ok := t.(priceBreakdown); ok {
			return pb.PriceCharges(), pb.PriceTax(), pb.HasFormula()
		}
		switch w := t.(type) {
		case *average:
			t = w.Tariff
		case *SlotWrapper:
			t = w.Tariff
		case *cachingProxy:
			if w.tariff != nil {
				t = w.tariff
				continue
			}
			return configPriceBreakdown(w.config)
		default:
			return 0, 0, false
		}
	}
	return 0, 0, false
}

func configPriceBreakdown(cfg map[string]any) (charges, tax float64, formula bool) {
	charges = configFloat(cfg, "charges")
	tax = configFloat(cfg, "tax")
	if s, ok := cfg["formula"].(string); ok && s != "" {
		formula = true
	}
	return
}

func configFloat(cfg map[string]any, key string) float64 {
	switch v := cfg[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}
