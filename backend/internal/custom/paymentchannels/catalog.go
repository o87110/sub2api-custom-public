package paymentchannels

import (
	"encoding/json"
	"strings"
)

const DefaultCurrency = "CNY"

type ProviderRecord struct {
	ID             int64
	ProviderKey    string
	SupportedTypes string
	LimitsJSON     string
	Currency       string
	Config         map[string]string
}

type CurrencyConflictError struct {
	PaymentType string
	ProviderKey string
}

func (e *CurrencyConflictError) Error() string {
	return "payment channel has enabled provider instances with mixed currencies"
}

// ProviderCatalog owns the custom provider/method projection and validation
// rules. Official services supply decrypted persistence records and delegate.
type ProviderCatalog struct{}

func (ProviderCatalog) BuildOptions(records []ProviderRecord, feeRate float64, settings ChannelSettings, alipayMobilePrecreateDeepLink bool) []MethodOption {
	options := BuildMethodOptions(projectProviderRecords(records), feeRate, alipayMobilePrecreateDeepLink)
	return ApplyChannelSettings(options, settings)
}

func (ProviderCatalog) ValidateCurrency(records []ProviderRecord, paymentType, providerKey string) (string, error) {
	paymentType = normalizeVisibleMethod(paymentType)
	providerKey = normalize(providerKey)
	if paymentType == "" || providerKey == "" {
		return DefaultCurrency, nil
	}

	matching := make([]Instance, 0, len(records))
	for _, instance := range projectProviderRecords(records) {
		if normalize(instance.ProviderKey) == providerKey && instanceSupportsPaymentType(instance, paymentType) {
			matching = append(matching, instance)
		}
	}
	if len(matching) == 0 {
		return DefaultCurrency, nil
	}
	currency, ok := aggregateCurrency(matching)
	if !ok {
		return "", &CurrencyConflictError{PaymentType: paymentType, ProviderKey: providerKey}
	}
	return currency, nil
}

func (ProviderCatalog) HasConfiguredSelection(records []ProviderRecord, paymentType, providerKey string) bool {
	paymentType = normalizeVisibleMethod(paymentType)
	providerKey = normalize(providerKey)
	if paymentType == "" || providerKey == "" {
		return false
	}
	for _, instance := range projectProviderRecords(records) {
		if normalize(instance.ProviderKey) != providerKey {
			continue
		}
		if providerKey == ProviderStripe {
			if paymentType == MethodStripe {
				return true
			}
			continue
		}
		if instanceSupportsPaymentType(instance, paymentType) {
			return true
		}
	}
	return false
}

func projectProviderRecords(records []ProviderRecord) []Instance {
	result := make([]Instance, 0, len(records))
	for _, record := range records {
		paymentTypes := visiblePaymentTypes(record.ProviderKey, record.SupportedTypes, record.Config)
		limits := make(map[string]Limits)
		displayNames := make(map[string]string)
		for _, paymentType := range paymentTypes {
			if channelLimits, ok := recordPaymentTypeLimits(record.LimitsJSON, paymentType); ok {
				limits[paymentType] = channelLimits
			}
			if displayName := customMethodDisplayName(record.ProviderKey, record.Config, paymentType); displayName != "" {
				displayNames[paymentType] = displayName
			}
		}
		networkOptions := []NetworkOption(nil)
		if normalize(record.ProviderKey) == ProviderEasyPay && IsBepusdtNativeConfig(record.Config) && containsString(paymentTypes, BepusdtPaymentType) {
			networkOptions = BepusdtNetworkOptions(record.Config)
		}
		currency := strings.ToUpper(strings.TrimSpace(record.Currency))
		if currency == "" {
			currency = DefaultCurrency
		}
		result = append(result, Instance{
			ID:             record.ID,
			ProviderKey:    record.ProviderKey,
			PaymentTypes:   paymentTypes,
			Currency:       currency,
			Limits:         limits,
			DisplayNames:   displayNames,
			NetworkOptions: networkOptions,
		})
	}
	return result
}

func visiblePaymentTypes(providerKey, supportedTypes string, config map[string]string) []string {
	providerKey = normalize(providerKey)
	if providerKey == ProviderStripe {
		return []string{MethodStripe}
	}
	if providerKey == ProviderAlipay || providerKey == ProviderWxpay || providerKey == ProviderEasyPay {
		return enabledVisibleMethodsForProvider(providerKey, supportedTypes)
	}

	result := make([]string, 0)
	seen := make(map[string]struct{})
	for _, supportedType := range splitSupportedTypes(supportedTypes) {
		method := normalizeVisibleMethod(supportedType)
		if method == "" {
			continue
		}
		if _, exists := seen[method]; exists {
			continue
		}
		seen[method] = struct{}{}
		result = append(result, method)
	}
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func enabledVisibleMethodsForProvider(providerKey, supportedTypes string) []string {
	methodSet := make(map[string]struct{}, 2)
	addMethod := func(method string) {
		if method = normalizeVisibleMethod(method); method != "" {
			methodSet[method] = struct{}{}
		}
	}

	switch normalize(providerKey) {
	case ProviderAlipay:
		if strings.TrimSpace(supportedTypes) == "" {
			addMethod(MethodAlipay)
		} else {
			for _, supportedType := range splitSupportedTypes(supportedTypes) {
				if normalizeVisibleMethod(supportedType) == MethodAlipay {
					addMethod(MethodAlipay)
					break
				}
			}
		}
	case ProviderWxpay:
		if strings.TrimSpace(supportedTypes) == "" {
			addMethod(MethodWxpay)
		} else {
			for _, supportedType := range splitSupportedTypes(supportedTypes) {
				if normalizeVisibleMethod(supportedType) == MethodWxpay {
					addMethod(MethodWxpay)
					break
				}
			}
		}
	case ProviderEasyPay:
		for _, supportedType := range splitSupportedTypes(supportedTypes) {
			addMethod(supportedType)
		}
	}

	methods := make([]string, 0, len(methodSet))
	for _, method := range []string{MethodAlipay, MethodWxpay} {
		if _, ok := methodSet[method]; ok {
			methods = append(methods, method)
			delete(methodSet, method)
		}
	}
	for _, supportedType := range splitSupportedTypes(supportedTypes) {
		method := normalizeVisibleMethod(supportedType)
		if _, ok := methodSet[method]; ok {
			methods = append(methods, method)
			delete(methodSet, method)
		}
	}
	return methods
}

func normalizeVisibleMethod(method string) string {
	method = normalize(method)
	switch {
	case method == ProviderEasyPay || method == ProviderAirwallex:
		return method
	case method == MethodStripe || method == "card" || method == "link":
		return MethodStripe
	case strings.HasPrefix(method, MethodAlipay):
		return MethodAlipay
	case strings.HasPrefix(method, MethodWxpay):
		return MethodWxpay
	default:
		return method
	}
}

func splitSupportedTypes(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func recordPaymentTypeLimits(raw, paymentType string) (Limits, bool) {
	if strings.TrimSpace(raw) == "" {
		return Limits{}, false
	}
	var limits map[string]Limits
	if err := json.Unmarshal([]byte(raw), &limits); err != nil {
		return Limits{}, false
	}
	if value, ok := limits[paymentType]; ok {
		return value, true
	}
	switch paymentType {
	case MethodAlipay:
		value, ok := limits["alipay_direct"]
		return value, ok
	case MethodWxpay:
		value, ok := limits["wxpay_direct"]
		return value, ok
	default:
		return Limits{}, false
	}
}

type customMethodDisplayConfig struct {
	Type        string `json:"type"`
	DisplayName string `json:"displayName"`
}

func customMethodDisplayName(providerKey string, config map[string]string, paymentType string) string {
	if normalize(providerKey) != ProviderEasyPay {
		return ""
	}
	raw := strings.TrimSpace(config["customMethods"])
	if raw == "" {
		return ""
	}
	var methods []customMethodDisplayConfig
	if err := json.Unmarshal([]byte(raw), &methods); err != nil {
		return ""
	}
	for _, method := range methods {
		if strings.TrimSpace(method.Type) == paymentType {
			return strings.TrimSpace(method.DisplayName)
		}
	}
	return ""
}

func instanceSupportsPaymentType(instance Instance, paymentType string) bool {
	for _, supportedType := range instance.PaymentTypes {
		if supportedType == paymentType {
			return true
		}
	}
	return false
}
