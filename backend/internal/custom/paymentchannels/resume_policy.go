package paymentchannels

import (
	"fmt"
	"strings"
	"time"
)

const (
	WeChatPaymentOAuthTokenType  = "wechat_payment_oauth"
	WeChatPaymentResumeTokenType = "wechat_payment_resume"
	OrderTypeBalance             = "balance"
)

type WeChatPaymentOAuthClaims struct {
	TokenType   string   `json:"tk,omitempty"`
	PaymentType string   `json:"pt,omitempty"`
	ProviderKey string   `json:"pk,omitempty"`
	Amount      string   `json:"amt,omitempty"`
	OrderType   string   `json:"ot,omitempty"`
	PlanID      int64    `json:"pid,omitempty"`
	FeeRate     *float64 `json:"fr,omitempty"`
	IssuedAt    int64    `json:"iat"`
	ExpiresAt   int64    `json:"exp,omitempty"`
}

type WeChatPaymentResumeClaims struct {
	TokenType   string   `json:"tk,omitempty"`
	OpenID      string   `json:"openid"`
	PaymentType string   `json:"pt,omitempty"`
	ProviderKey string   `json:"pk,omitempty"`
	Amount      string   `json:"amt,omitempty"`
	OrderType   string   `json:"ot,omitempty"`
	PlanID      int64    `json:"pid,omitempty"`
	FeeRate     *float64 `json:"fr,omitempty"`
	RedirectTo  string   `json:"rd,omitempty"`
	Scope       string   `json:"scp,omitempty"`
	IssuedAt    int64    `json:"iat"`
	ExpiresAt   int64    `json:"exp,omitempty"`
}

func PrepareWeChatOAuthClaims(claims WeChatPaymentOAuthClaims, now time.Time, ttl time.Duration) (WeChatPaymentOAuthClaims, error) {
	if claims.FeeRate == nil {
		return claims, fmt.Errorf("wechat payment context requires fee rate")
	}
	if err := ValidateFeeRate(*claims.FeeRate); err != nil {
		return claims, err
	}
	setPaymentContextDefaults(&claims.PaymentType, &claims.ProviderKey, &claims.OrderType)
	if claims.ProviderKey != "" && claims.ProviderKey != ProviderWxpay {
		return claims, fmt.Errorf("wechat payment oauth token provider must be wxpay")
	}
	claims.IssuedAt, claims.ExpiresAt = tokenTimes(claims.IssuedAt, claims.ExpiresAt, now, ttl)
	claims.TokenType = WeChatPaymentOAuthTokenType
	return claims, nil
}

func ValidateWeChatOAuthClaims(claims WeChatPaymentOAuthClaims) (WeChatPaymentOAuthClaims, error) {
	if claims.TokenType != WeChatPaymentOAuthTokenType {
		return claims, fmt.Errorf("wechat payment oauth token type mismatch")
	}
	if claims.FeeRate == nil {
		return claims, fmt.Errorf("wechat payment context requires fee rate")
	}
	if err := ValidateFeeRate(*claims.FeeRate); err != nil {
		return claims, err
	}
	setPaymentContextDefaults(&claims.PaymentType, &claims.ProviderKey, &claims.OrderType)
	if claims.ProviderKey != "" && claims.ProviderKey != ProviderWxpay {
		return claims, fmt.Errorf("wechat payment oauth token provider is invalid")
	}
	return claims, nil
}

func PrepareWeChatResumeClaims(claims WeChatPaymentResumeClaims, now time.Time, ttl time.Duration) (WeChatPaymentResumeClaims, error) {
	claims.OpenID = strings.TrimSpace(claims.OpenID)
	if claims.OpenID == "" {
		return claims, fmt.Errorf("wechat payment resume token requires openid")
	}
	if claims.FeeRate != nil {
		if err := ValidateFeeRate(*claims.FeeRate); err != nil {
			return claims, err
		}
	}
	setPaymentContextDefaults(&claims.PaymentType, &claims.ProviderKey, &claims.OrderType)
	claims.IssuedAt, claims.ExpiresAt = tokenTimes(claims.IssuedAt, claims.ExpiresAt, now, ttl)
	claims.TokenType = WeChatPaymentResumeTokenType
	return claims, nil
}

func ValidateWeChatResumeClaims(claims WeChatPaymentResumeClaims) (WeChatPaymentResumeClaims, error) {
	if claims.TokenType != WeChatPaymentResumeTokenType {
		return claims, fmt.Errorf("wechat payment resume token type mismatch")
	}
	claims.OpenID = strings.TrimSpace(claims.OpenID)
	if claims.OpenID == "" {
		return claims, fmt.Errorf("wechat payment resume token missing openid")
	}
	if claims.FeeRate != nil {
		if err := ValidateFeeRate(*claims.FeeRate); err != nil {
			return claims, err
		}
	}
	setPaymentContextDefaults(&claims.PaymentType, &claims.ProviderKey, &claims.OrderType)
	return claims, nil
}

func setPaymentContextDefaults(paymentType, providerKey, orderType *string) {
	if normalized := normalizeVisibleMethod(*paymentType); normalized != "" {
		*paymentType = normalized
	}
	if *paymentType == "" {
		*paymentType = MethodWxpay
	}
	*providerKey = normalize(*providerKey)
	if *orderType == "" {
		*orderType = OrderTypeBalance
	}
}

func tokenTimes(issuedAt, expiresAt int64, now time.Time, ttl time.Duration) (int64, int64) {
	if issuedAt == 0 {
		issuedAt = now.Unix()
	}
	if expiresAt == 0 {
		expiresAt = now.Add(ttl).Unix()
	}
	return issuedAt, expiresAt
}
