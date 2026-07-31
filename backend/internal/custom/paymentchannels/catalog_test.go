package paymentchannels

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderCatalogBuildsDynamicAndLegacyChannels(t *testing.T) {
	catalog := ProviderCatalog{}
	options := catalog.BuildOptions([]ProviderRecord{
		{ID: 1, ProviderKey: ProviderAlipay, Currency: "CNY"},
		{
			ID:             2,
			ProviderKey:    ProviderEasyPay,
			SupportedTypes: "alipay,ldc",
			Currency:       "CNY",
			LimitsJSON:     `{"ldc":{"SingleMin":3,"SingleMax":9}}`,
			Config:         map[string]string{"customMethods": `[{"type":"ldc","displayName":"LDC"}]`},
		},
	}, 2.5, nil, false)

	require.Len(t, options, 3)
	require.Equal(t, "official_alipay", options[1].ID)
	require.Equal(t, "easypay_ldc", options[2].ID)
	require.Equal(t, "LDC", options[2].DisplayName)
	require.Equal(t, float64(3), options[2].SingleMin)
}

func TestProviderCatalogValidatesCurrencyWithinProvider(t *testing.T) {
	catalog := ProviderCatalog{}
	records := []ProviderRecord{
		{ID: 1, ProviderKey: ProviderEasyPay, SupportedTypes: "alipay", Currency: "CNY"},
		{ID: 2, ProviderKey: ProviderAlipay, Currency: "USD"},
		{ID: 3, ProviderKey: ProviderStripe, SupportedTypes: "card", Currency: "USD"},
		{ID: 4, ProviderKey: ProviderStripe, SupportedTypes: "link", Currency: "EUR"},
	}

	currency, err := catalog.ValidateCurrency(records, MethodAlipay, ProviderEasyPay)
	require.NoError(t, err)
	require.Equal(t, "CNY", currency)

	_, err = catalog.ValidateCurrency(records, MethodStripe, ProviderStripe)
	var conflict *CurrencyConflictError
	require.True(t, errors.As(err, &conflict))
	require.Equal(t, ProviderStripe, conflict.ProviderKey)
}

func TestProviderCatalogRecognizesConfiguredDynamicMethod(t *testing.T) {
	catalog := ProviderCatalog{}
	records := []ProviderRecord{{
		ID:             1,
		ProviderKey:    ProviderEasyPay,
		SupportedTypes: "ldc",
	}}
	require.True(t, catalog.HasConfiguredSelection(records, "ldc", ProviderEasyPay))
	require.False(t, catalog.HasConfiguredSelection(records, MethodAlipay, ProviderStripe))
}
