package ratedisplay

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidOverride(t *testing.T) {
	valid := 0.9
	require.True(t, ValidOverride(nil))
	require.True(t, ValidOverride(&valid))

	for _, value := range []float64{0, -0.1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		value := value
		require.False(t, ValidOverride(&value))
	}
}

func TestNormalizeTemplate(t *testing.T) {
	for input, expected := range map[string]string{
		"":            "",
		"  ":          "",
		"{rate}x":     "{rate}x",
		" {rate}优先用 ": "{rate}优先用",
		"约{rate}x":    "约{rate}x",
		"{rate}":      "{rate}",
	} {
		actual, ok := NormalizeTemplate(input)
		require.True(t, ok)
		require.Equal(t, expected, actual)
	}

	for _, value := range []string{
		"0.9x",
		"{rate}/{rate}",
		strings.Repeat("界", MaxTemplateRunes) + "{rate}",
	} {
		_, ok := NormalizeTemplate(value)
		require.False(t, ok)
	}
}
