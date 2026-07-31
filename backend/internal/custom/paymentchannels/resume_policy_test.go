package paymentchannels

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPrepareWeChatOAuthClaimsPreservesProviderAndFee(t *testing.T) {
	now := time.Unix(100, 0)
	fee := 2.5
	claims, err := PrepareWeChatOAuthClaims(WeChatPaymentOAuthClaims{
		PaymentType: MethodWxpay,
		ProviderKey: ProviderWxpay,
		FeeRate:     &fee,
	}, now, time.Minute)
	require.NoError(t, err)
	require.Equal(t, WeChatPaymentOAuthTokenType, claims.TokenType)
	require.Equal(t, ProviderWxpay, claims.ProviderKey)
	require.Equal(t, int64(100), claims.IssuedAt)
	require.Equal(t, int64(160), claims.ExpiresAt)
}

func TestWeChatClaimPoliciesRejectInvalidContext(t *testing.T) {
	invalid := math.Inf(1)
	_, err := PrepareWeChatOAuthClaims(WeChatPaymentOAuthClaims{FeeRate: &invalid}, time.Now(), time.Minute)
	require.ErrorContains(t, err, "between 0 and 100")

	fee := 1.0
	_, err = PrepareWeChatOAuthClaims(WeChatPaymentOAuthClaims{
		ProviderKey: ProviderEasyPay,
		FeeRate:     &fee,
	}, time.Now(), time.Minute)
	require.ErrorContains(t, err, "provider must be wxpay")

	_, err = PrepareWeChatResumeClaims(WeChatPaymentResumeClaims{}, time.Now(), time.Minute)
	require.ErrorContains(t, err, "requires openid")
}
