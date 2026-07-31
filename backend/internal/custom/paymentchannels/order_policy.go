package paymentchannels

import (
	"fmt"
	"math"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type OrderPolicy struct{}

func (OrderPolicy) NormalizeProviderKey(providerKey string) string {
	return normalize(providerKey)
}

func (OrderPolicy) ResolveFeeRate(paymentType, providerKey string, globalFeeRate float64, settings ChannelSettings, resumed *float64) (float64, error) {
	feeRate := ResolveFeeRate(paymentType, providerKey, globalFeeRate, settings)
	if resumed == nil {
		return feeRate, nil
	}
	if err := ValidateFeeRate(*resumed); err != nil {
		return 0, infraerrors.BadRequest("INVALID_WECHAT_PAYMENT_RESUME_TOKEN", "wechat payment resume fee rate is invalid")
	}
	return *resumed, nil
}

func (OrderPolicy) ValidateSelection(paymentType, providerKey string, configured bool) error {
	if IsValidSelection(paymentType, providerKey) || configured {
		return nil
	}
	return infraerrors.BadRequest(
		"INVALID_PAYMENT_PROVIDER_SELECTION",
		"invalid payment provider selection",
	).WithMetadata(map[string]string{
		"payment_type": paymentType,
		"provider_key": providerKey,
	})
}

func (OrderPolicy) RevalidationError(paymentType, providerKey string, valid bool, cause error) error {
	if cause != nil {
		return infraerrors.ServiceUnavailable("PAYMENT_GATEWAY_ERROR", "payment_instance_revalidation_failed").
			WithMetadata(map[string]string{
				"payment_type": paymentType,
				"provider_key": providerKey,
			})
	}
	if !valid {
		return infraerrors.TooManyRequests("NO_AVAILABLE_INSTANCE", "no_available_instance")
	}
	return nil
}

func (OrderPolicy) UsesOfficialWeChat(requestProviderKey, selectedProviderKey string) bool {
	requestProviderKey = normalize(requestProviderKey)
	selectedProviderKey = normalize(selectedProviderKey)
	if requestProviderKey != "" && requestProviderKey != ProviderWxpay {
		return false
	}
	return selectedProviderKey == "" || selectedProviderKey == ProviderWxpay
}

func ValidateFeeRate(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
		return fmt.Errorf("wechat payment context fee rate must be between 0 and 100")
	}
	return nil
}

func NormalizeVisibleMethod(method string) string {
	return normalizeVisibleMethod(method)
}

func NormalizeOrderProviderKey(providerKey string) string {
	return strings.ToLower(strings.TrimSpace(providerKey))
}
