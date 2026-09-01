package paymentchannels

import (
	"sort"
	"strings"
)

const (
	ProviderEasyPay   = "easypay"
	ProviderAlipay    = "alipay"
	ProviderWxpay     = "wxpay"
	ProviderStripe    = "stripe"
	ProviderAirwallex = "airwallex"

	MethodAlipay    = "alipay"
	MethodWxpay     = "wxpay"
	MethodStripe    = "stripe"
	MethodAirwallex = "airwallex"

	CapabilityAlipayMobilePrecreateDeepLink = "alipay_mobile_precreate_deep_link"
)

// Limits describes the amount boundaries shared by instances in one channel.
type Limits struct {
	DailyLimit float64
	SingleMin  float64
	SingleMax  float64
}

// Instance is the minimal provider-instance projection needed to build user
// facing payment-channel options.
type Instance struct {
	ID             int64
	ProviderKey    string
	PaymentTypes   []string
	Currency       string
	Limits         map[string]Limits
	DisplayNames   map[string]string
	NetworkOptions []NetworkOption
}

// MethodOption is the stable public representation of a selectable payment
// channel. Multiple instances with the same payment type and provider key are
// folded into one option.
// AmountRange is one contiguous gateway amount interval. Zero means unbounded.
type AmountRange struct {
	SingleMin float64 `json:"single_min"`
	SingleMax float64 `json:"single_max"`
}

type NetworkOption struct {
	Code        string `json:"code"`
	DisplayName string `json:"display_name"`
}

type MethodOption struct {
	ID             string          `json:"id"`
	PaymentType    string          `json:"payment_type"`
	ProviderKey    string          `json:"provider_key"`
	DisplayName    string          `json:"display_name,omitempty"`
	Currency       string          `json:"currency"`
	FeeRate        float64         `json:"fee_rate"`
	DailyLimit     float64         `json:"daily_limit"`
	SingleMin      float64         `json:"single_min"`
	SingleMax      float64         `json:"single_max"`
	AmountRanges   []AmountRange   `json:"amount_ranges,omitempty"`
	NetworkOptions []NetworkOption `json:"network_options,omitempty"`
	Available      bool            `json:"available"`
	Capabilities   []string        `json:"capabilities,omitempty"`
}

type channelKey struct {
	paymentType string
	providerKey string
}

// BuildMethodOptions groups enabled instances by payment type and provider.
// A group with mixed currencies is intentionally omitted so one broken channel
// does not hide another channel for the same payment method.
func BuildMethodOptions(instances []Instance, feeRate float64, alipayMobilePrecreateDeepLink bool) []MethodOption {
	groups := make(map[channelKey][]Instance)
	seen := make(map[channelKey]map[int64]struct{})

	for _, instance := range instances {
		providerKey := normalize(instance.ProviderKey)
		if providerKey == "" {
			continue
		}
		for _, rawPaymentType := range instance.PaymentTypes {
			paymentType := normalize(rawPaymentType)
			if paymentType == "" {
				continue
			}
			key := channelKey{paymentType: paymentType, providerKey: providerKey}
			if seen[key] == nil {
				seen[key] = make(map[int64]struct{})
			}
			if _, exists := seen[key][instance.ID]; exists {
				continue
			}
			seen[key][instance.ID] = struct{}{}
			groups[key] = append(groups[key], instance)
		}
	}

	options := make([]MethodOption, 0, len(groups))
	for key, groupedInstances := range groups {
		currency, consistent := aggregateCurrency(groupedInstances)
		if !consistent {
			continue
		}
		limits := aggregateLimits(key.paymentType, groupedInstances)
		amountRanges := aggregateAmountRanges(key.paymentType, groupedInstances)
		option := MethodOption{
			ID:           StableID(key.paymentType, key.providerKey),
			PaymentType:  key.paymentType,
			ProviderKey:  key.providerKey,
			DisplayName:  aggregateDisplayName(key.paymentType, groupedInstances),
			Currency:     currency,
			FeeRate:      feeRate,
			DailyLimit:   limits.DailyLimit,
			SingleMin:    limits.SingleMin,
			SingleMax:    limits.SingleMax,
			AmountRanges: amountRanges,
			Available:    len(amountRanges) > 0,
		}
		if key.paymentType == BepusdtPaymentType && key.providerKey == ProviderEasyPay {
			option.NetworkOptions = aggregateNetworkOptions(groupedInstances)
			if len(option.NetworkOptions) == 0 {
				option.Available = false
			}
		}
		if alipayMobilePrecreateDeepLink &&
			key.paymentType == MethodAlipay &&
			key.providerKey == ProviderAlipay {
			option.Capabilities = []string{CapabilityAlipayMobilePrecreateDeepLink}
		}
		options = append(options, option)
	}

	sort.Slice(options, func(i, j int) bool {
		left, right := optionSortKey(options[i]), optionSortKey(options[j])
		if left != right {
			return left < right
		}
		return options[i].ID < options[j].ID
	})
	return options
}

// StableID returns the API-stable identifier used by the frontend selection
// state. The four visible Alipay/WeChat combinations have explicit IDs.
func StableID(paymentType, providerKey string) string {
	paymentType = normalize(paymentType)
	providerKey = normalize(providerKey)
	switch {
	case paymentType == MethodAlipay && providerKey == ProviderEasyPay:
		return "easypay_alipay"
	case paymentType == MethodAlipay && providerKey == ProviderAlipay:
		return "official_alipay"
	case paymentType == MethodWxpay && providerKey == ProviderEasyPay:
		return "easypay_wxpay"
	case paymentType == MethodWxpay && providerKey == ProviderWxpay:
		return "official_wxpay"
	case providerKey == paymentType:
		return paymentType
	case providerKey == "":
		return paymentType
	default:
		return providerKey + "_" + paymentType
	}
}

// IsValidSelection validates the provider/payment-method combination without
// consulting runtime health. Valid but temporarily unavailable channels must
// continue to fail with the existing gateway-unavailable error.
func IsValidSelection(paymentType, providerKey string) bool {
	paymentType = normalize(paymentType)
	providerKey = normalize(providerKey)
	if paymentType == "" || providerKey == "" {
		return false
	}
	switch paymentType {
	case MethodAlipay:
		return providerKey == ProviderEasyPay || providerKey == ProviderAlipay
	case MethodWxpay:
		return providerKey == ProviderEasyPay || providerKey == ProviderWxpay
	case MethodStripe:
		return providerKey == ProviderStripe
	case MethodAirwallex:
		return providerKey == ProviderAirwallex
	default:
		return false
	}
}

// ShouldFailClosedOnNoAvailableInstance keeps explicit channel selections from
// bypassing per-instance limits while preserving the legacy provider-agnostic route.
func ShouldFailClosedOnNoAvailableInstance(providerKey string) bool {
	return normalize(providerKey) != ""
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func aggregateCurrency(instances []Instance) (string, bool) {
	currency := ""
	for _, instance := range instances {
		next := strings.ToUpper(strings.TrimSpace(instance.Currency))
		if next == "" {
			next = "CNY"
		}
		if currency == "" {
			currency = next
			continue
		}
		if currency != next {
			return "", false
		}
	}
	if currency == "" {
		currency = "CNY"
	}
	return currency, true
}

func aggregateDisplayName(paymentType string, instances []Instance) string {
	names := make([]string, 0, len(instances))
	for _, instance := range instances {
		names = append(names, instance.DisplayNames[paymentType])
	}
	return AggregateDisplayName(names)
}

func aggregateNetworkOptions(instances []Instance) []NetworkOption {
	if len(instances) == 0 {
		return nil
	}
	allowed := make(map[string]NetworkOption)
	for _, option := range instances[0].NetworkOptions {
		allowed[option.Code] = option
	}
	for _, instance := range instances[1:] {
		present := make(map[string]struct{}, len(instance.NetworkOptions))
		for _, option := range instance.NetworkOptions {
			present[option.Code] = struct{}{}
		}
		for code := range allowed {
			if _, ok := present[code]; !ok {
				delete(allowed, code)
			}
		}
	}
	result := make([]NetworkOption, 0, len(allowed))
	for _, code := range bepusdtNetworkOrder {
		if option, ok := allowed[code]; ok {
			result = append(result, option)
		}
	}
	return result
}

func aggregateAmountRanges(paymentType string, instances []Instance) []AmountRange {
	ranges := make([]AmountRange, 0, len(instances))
	for _, instance := range instances {
		channelLimits, ok := instance.Limits[paymentType]
		if !ok {
			channelLimits = Limits{}
		}
		if channelLimits.SingleMin > 0 && channelLimits.SingleMax > 0 &&
			channelLimits.SingleMin > channelLimits.SingleMax {
			continue
		}
		ranges = append(ranges, AmountRange{
			SingleMin: channelLimits.SingleMin,
			SingleMax: channelLimits.SingleMax,
		})
	}
	if len(ranges) < 2 {
		return ranges
	}

	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].SingleMin != ranges[j].SingleMin {
			return ranges[i].SingleMin < ranges[j].SingleMin
		}
		if ranges[i].SingleMax == 0 {
			return false
		}
		if ranges[j].SingleMax == 0 {
			return true
		}
		return ranges[i].SingleMax < ranges[j].SingleMax
	})

	merged := make([]AmountRange, 0, len(ranges))
	for _, next := range ranges {
		if len(merged) == 0 {
			merged = append(merged, next)
			continue
		}
		current := &merged[len(merged)-1]
		if current.SingleMax != 0 && next.SingleMin > current.SingleMax {
			merged = append(merged, next)
			continue
		}
		if current.SingleMax == 0 || next.SingleMax == 0 {
			current.SingleMax = 0
		} else if next.SingleMax > current.SingleMax {
			current.SingleMax = next.SingleMax
		}
	}
	return merged
}

func aggregateLimits(paymentType string, instances []Instance) Limits {
	limits := Limits{}
	dailyLimited := true
	for _, instance := range instances {
		channelLimits, ok := instance.Limits[paymentType]
		if !ok {
			channelLimits = Limits{}
		}
		limits.DailyLimit, dailyLimited = unionFloat(limits.DailyLimit, dailyLimited, channelLimits.DailyLimit, false)
	}
	if !dailyLimited {
		limits.DailyLimit = 0
	}

	ranges := aggregateAmountRanges(paymentType, instances)
	if len(ranges) > 0 {
		// Legacy clients can express only one interval. Expose the first safe
		// contiguous range instead of a hull that would admit gap amounts.
		limits.SingleMin = ranges[0].SingleMin
		limits.SingleMax = ranges[0].SingleMax
	}
	return limits
}

func unionFloat(current float64, currentLimited bool, next float64, preferMin bool) (float64, bool) {
	if next <= 0 {
		return current, false
	}
	if !currentLimited {
		return current, false
	}
	if current <= 0 {
		return next, true
	}
	if preferMin && next < current {
		return next, true
	}
	if !preferMin && next > current {
		return next, true
	}
	return current, true
}

func optionSortKey(option MethodOption) int {
	switch option.ID {
	case "easypay_alipay":
		return 10
	case "official_alipay":
		return 20
	case "easypay_wxpay":
		return 30
	case "official_wxpay":
		return 40
	case "stripe":
		return 50
	case "airwallex":
		return 60
	case "easypay_usdt":
		return 45
	default:
		return 100
	}
}
