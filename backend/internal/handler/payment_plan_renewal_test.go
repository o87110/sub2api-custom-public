package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckoutPlanExposesOnlyDerivedRenewalCapability(t *testing.T) {
	body, err := json.Marshal(checkoutPlan{ID: 1, RenewalAvailable: true})
	require.NoError(t, err)

	jsonText := string(body)
	require.Contains(t, jsonText, `"renewal_available":true`)
	require.False(t, strings.Contains(jsonText, "allow_existing_user_renewal"))
	require.False(t, strings.Contains(jsonText, "renewal_grace_days"))
}
