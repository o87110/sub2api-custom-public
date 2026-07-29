package groupaccess

import (
	"errors"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestEvaluateMinimumBalance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		current    float64
		minimum    float64
		eligible   bool
		usable     float64
		balanceGap float64
	}{
		{name: "disabled", current: 0, minimum: 0, eligible: true, usable: 0, balanceGap: 0},
		{name: "above", current: 120, minimum: 100, eligible: true, usable: 20, balanceGap: 0},
		{name: "equal", current: 100, minimum: 100, eligible: false, usable: 0, balanceGap: 0},
		{name: "below", current: 80, minimum: 100, eligible: false, usable: 0, balanceGap: 20},
		{name: "sub cent gap", current: 99.99999999, minimum: 100, eligible: false, usable: 0, balanceGap: 0.00000001},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EvaluateMinimumBalance(tt.current, tt.minimum)
			require.Equal(t, tt.eligible, got.Eligible)
			require.InDelta(t, tt.usable, got.UsableBalance, 0.000000001)
			require.InDelta(t, tt.balanceGap, got.BalanceGap, 0.000000001)
		})
	}
}

func TestCheckMinimumBalanceError(t *testing.T) {
	t.Parallel()

	err := CheckMinimumBalance(7, "Group A", 80, 100)
	require.Error(t, err)
	require.Equal(t, MinimumBalanceNotMetReason, infraerrors.Reason(err))
	require.Equal(
		t,
		"Group requires an available balance greater than $100.00; current balance is $80.00. Add more than $20.00 to continue.",
		infraerrors.Message(err),
	)

	var appErr *infraerrors.ApplicationError
	require.True(t, errors.As(err, &appErr))
	require.Equal(t, "7", appErr.Metadata["group_id"])
	require.Equal(t, "20.00", appErr.Metadata["balance_gap"])
	require.Equal(t, "0.00", appErr.Metadata["usable_balance"])
}

func TestCheckMinimumBalanceEqualMessage(t *testing.T) {
	t.Parallel()

	err := CheckMinimumBalance(7, "Group A", 100, 100)
	require.Equal(
		t,
		"Group requires an available balance greater than $100.00; current balance is $100.00. Increase the balance above $100.00 to continue.",
		infraerrors.Message(err),
	)
}

func TestFormatUSDPreservesSubCentValues(t *testing.T) {
	t.Parallel()

	require.Equal(t, "$100.00", FormatUSD(100))
	require.Equal(t, "$100.00", FormatUSD(100.001))
	require.Equal(t, "$20.12", FormatUSD(20.123))
	require.Equal(t, "$0.00000001", FormatUSD(0.00000001))
}
