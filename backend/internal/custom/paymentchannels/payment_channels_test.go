package paymentchannels

import (
	"reflect"
	"testing"
)

func TestBuildMethodOptionsGroupsProvidersAndSortsEasyPayFirst(t *testing.T) {
	instances := []Instance{
		{
			ID:           1,
			ProviderKey:  ProviderAlipay,
			PaymentTypes: []string{MethodAlipay},
			Currency:     "CNY",
			Limits: map[string]Limits{
				MethodAlipay: {SingleMin: 20, SingleMax: 500},
			},
		},
		{
			ID:           2,
			ProviderKey:  ProviderEasyPay,
			PaymentTypes: []string{MethodAlipay, MethodWxpay},
			Currency:     "CNY",
			Limits: map[string]Limits{
				MethodAlipay: {SingleMin: 10, SingleMax: 1000},
				MethodWxpay:  {SingleMin: 5, SingleMax: 800},
			},
		},
		{
			ID:           3,
			ProviderKey:  ProviderWxpay,
			PaymentTypes: []string{MethodWxpay},
			Currency:     "CNY",
			Limits: map[string]Limits{
				MethodWxpay: {SingleMin: 30, SingleMax: 600},
			},
		},
	}

	options := BuildMethodOptions(instances, 0.03, true)
	gotIDs := make([]string, 0, len(options))
	for _, option := range options {
		gotIDs = append(gotIDs, option.ID)
	}
	wantIDs := []string{"easypay_alipay", "official_alipay", "easypay_wxpay", "official_wxpay"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("option ids = %v, want %v", gotIDs, wantIDs)
	}
	if got := options[1].Capabilities; !reflect.DeepEqual(got, []string{CapabilityAlipayMobilePrecreateDeepLink}) {
		t.Fatalf("official alipay capabilities = %v", got)
	}
	if len(options[0].Capabilities) != 0 {
		t.Fatalf("easypay alipay must not expose official capability: %v", options[0].Capabilities)
	}
}

func TestBuildMethodOptionsSortsStripeAirwallexAndCustomMethodsAfterBuiltIns(t *testing.T) {
	options := BuildMethodOptions([]Instance{
		{ID: 1, ProviderKey: "airwallex", PaymentTypes: []string{"airwallex"}, Currency: "USD"},
		{ID: 2, ProviderKey: ProviderEasyPay, PaymentTypes: []string{"ldc"}, Currency: "CNY"},
		{ID: 3, ProviderKey: "stripe", PaymentTypes: []string{"stripe"}, Currency: "USD"},
		{ID: 4, ProviderKey: ProviderEasyPay, PaymentTypes: []string{MethodAlipay}, Currency: "CNY"},
		{ID: 5, ProviderKey: ProviderAlipay, PaymentTypes: []string{MethodAlipay}, Currency: "CNY"},
	}, 0, false)

	gotIDs := make([]string, 0, len(options))
	for _, option := range options {
		gotIDs = append(gotIDs, option.ID)
	}
	wantIDs := []string{
		"easypay_alipay",
		"official_alipay",
		"stripe",
		"airwallex",
		"easypay_ldc",
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("option ids = %v, want %v", gotIDs, wantIDs)
	}
}

func TestBuildMethodOptionsAggregatesWithinProviderOnly(t *testing.T) {
	instances := []Instance{
		{
			ID:           1,
			ProviderKey:  ProviderEasyPay,
			PaymentTypes: []string{MethodAlipay},
			Currency:     "CNY",
			Limits:       map[string]Limits{MethodAlipay: {SingleMin: 20, SingleMax: 200, DailyLimit: 500}},
		},
		{
			ID:           2,
			ProviderKey:  ProviderEasyPay,
			PaymentTypes: []string{MethodAlipay},
			Currency:     "CNY",
			Limits:       map[string]Limits{MethodAlipay: {SingleMin: 10, SingleMax: 300, DailyLimit: 800}},
		},
		{
			ID:           3,
			ProviderKey:  ProviderAlipay,
			PaymentTypes: []string{MethodAlipay},
			Currency:     "USD",
			Limits:       map[string]Limits{MethodAlipay: {SingleMin: 50, SingleMax: 100}},
		},
	}

	options := BuildMethodOptions(instances, 0, false)
	if len(options) != 2 {
		t.Fatalf("options len = %d, want 2", len(options))
	}
	easyPay := options[0]
	if easyPay.SingleMin != 10 || easyPay.SingleMax != 300 || easyPay.DailyLimit != 800 {
		t.Fatalf("unexpected easypay limits: %+v", easyPay)
	}
	official := options[1]
	if official.Currency != "USD" || official.SingleMin != 50 || official.SingleMax != 100 {
		t.Fatalf("unexpected official limits: %+v", official)
	}
}

func TestBuildMethodOptionsOmitsOnlyMixedCurrencyChannel(t *testing.T) {
	instances := []Instance{
		{ID: 1, ProviderKey: ProviderEasyPay, PaymentTypes: []string{MethodAlipay}, Currency: "CNY"},
		{ID: 2, ProviderKey: ProviderEasyPay, PaymentTypes: []string{MethodAlipay}, Currency: "USD"},
		{ID: 3, ProviderKey: ProviderAlipay, PaymentTypes: []string{MethodAlipay}, Currency: "CNY"},
	}

	options := BuildMethodOptions(instances, 0, false)
	if len(options) != 1 || options[0].ID != "official_alipay" {
		t.Fatalf("options = %+v, want only official_alipay", options)
	}
}

func TestBuildMethodOptionsTreatsAnyUnlimitedInstanceAsUnlimited(t *testing.T) {
	instances := []Instance{
		{
			ID:           1,
			ProviderKey:  ProviderEasyPay,
			PaymentTypes: []string{MethodAlipay},
			Currency:     "CNY",
		},
		{
			ID:           2,
			ProviderKey:  ProviderEasyPay,
			PaymentTypes: []string{MethodAlipay},
			Currency:     "CNY",
			Limits:       map[string]Limits{MethodAlipay: {SingleMin: 10, SingleMax: 100, DailyLimit: 500}},
		},
	}

	options := BuildMethodOptions(instances, 0, false)
	if len(options) != 1 {
		t.Fatalf("options len = %d, want 1", len(options))
	}
	if options[0].SingleMin != 0 || options[0].SingleMax != 0 || options[0].DailyLimit != 0 {
		t.Fatalf("channel with an unlimited instance must be unlimited: %+v", options[0])
	}
}

func TestShouldFailClosedOnNoAvailableInstance(t *testing.T) {
	if !ShouldFailClosedOnNoAvailableInstance(" easypay ") {
		t.Fatal("explicit provider selection must fail closed")
	}
	if ShouldFailClosedOnNoAvailableInstance("") {
		t.Fatal("legacy provider-agnostic selection must preserve its existing fallback")
	}
}

func TestIsValidSelection(t *testing.T) {
	tests := []struct {
		paymentType string
		providerKey string
		want        bool
	}{
		{MethodAlipay, ProviderEasyPay, true},
		{MethodAlipay, ProviderAlipay, true},
		{MethodAlipay, ProviderWxpay, false},
		{MethodWxpay, ProviderEasyPay, true},
		{MethodWxpay, ProviderWxpay, true},
		{MethodWxpay, ProviderAlipay, false},
		{"stripe", "stripe", true},
		{"airwallex", "airwallex", true},
		{"bogus", "bogus", false},
		{"custom_method", ProviderEasyPay, false},
		{"stripe", ProviderEasyPay, false},
		{"stripe", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.paymentType+"_"+tt.providerKey, func(t *testing.T) {
			if got := IsValidSelection(tt.paymentType, tt.providerKey); got != tt.want {
				t.Fatalf("IsValidSelection(%q, %q) = %v, want %v", tt.paymentType, tt.providerKey, got, tt.want)
			}
		})
	}
}
