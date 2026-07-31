package paymentchannels

import (
	"errors"
	"math"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestOrderPolicyPreservesSelectionAndFeeErrors(t *testing.T) {
	policy := OrderPolicy{}
	require.NoError(t, policy.ValidateSelection(MethodAlipay, ProviderEasyPay, false))
	require.NoError(t, policy.ValidateSelection("ldc", ProviderEasyPay, true))
	err := policy.ValidateSelection(MethodAlipay, ProviderStripe, false)
	require.Equal(t, "INVALID_PAYMENT_PROVIDER_SELECTION", infraerrors.Reason(err))

	_, err = policy.ResolveFeeRate(MethodWxpay, ProviderWxpay, 1, nil, func() *float64 {
		value := math.Inf(1)
		return &value
	}())
	require.Equal(t, "INVALID_WECHAT_PAYMENT_RESUME_TOKEN", infraerrors.Reason(err))
}

func TestOrderPolicyMapsRevalidationAndWechatProvider(t *testing.T) {
	policy := OrderPolicy{}
	require.Equal(t, "PAYMENT_GATEWAY_ERROR", infraerrors.Reason(policy.RevalidationError(MethodAlipay, ProviderEasyPay, false, errors.New("db"))))
	require.Equal(t, "NO_AVAILABLE_INSTANCE", infraerrors.Reason(policy.RevalidationError(MethodAlipay, ProviderEasyPay, false, nil)))
	require.NoError(t, policy.RevalidationError(MethodAlipay, ProviderEasyPay, true, nil))
	require.True(t, policy.UsesOfficialWeChat(ProviderWxpay, ""))
	require.False(t, policy.UsesOfficialWeChat(ProviderEasyPay, ProviderWxpay))
}
