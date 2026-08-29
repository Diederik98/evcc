package tariff

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPriceBreakdownFixed(t *testing.T) {
	tarr, err := NewFromConfig(context.Background(), "fixed", map[string]any{
		"price":   0.20,
		"charges": 0.15,
		"tax":     0.06,
	})
	require.NoError(t, err)

	charges, tax, formula := PriceBreakdown(tarr)
	assert.InDelta(t, 0.15, charges, 1e-9)
	assert.InDelta(t, 0.06, tax, 1e-9)
	assert.False(t, formula)
}

func TestPriceBreakdownNil(t *testing.T) {
	charges, tax, formula := PriceBreakdown(nil)
	assert.Zero(t, charges)
	assert.Zero(t, tax)
	assert.False(t, formula)
}
