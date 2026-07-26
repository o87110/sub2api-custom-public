package paymentchannels

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseChannelSettingsNormalizesOverrides(t *testing.T) {
	settings, err := ParseChannelSettings(`{
		"easypay_alipay":{"display_name":" 支付宝优惠通道 ","fee_rate":1.5},
		"official_alipay":{"display_name":"支付宝备用通道","fee_rate":null},
		"easypay_wxpay":{"fee_rate":0},
		"stripe":{}
	}`)
	if err != nil {
		t.Fatalf("ParseChannelSettings returned error: %v", err)
	}
	if got := settings["easypay_alipay"].DisplayName; got != "支付宝优惠通道" {
		t.Fatalf("display name = %q", got)
	}
	if got := settings["easypay_alipay"].FeeRate; got == nil || *got != 1.5 {
		t.Fatalf("fee rate = %v", got)
	}
	if got := settings["official_alipay"].FeeRate; got != nil {
		t.Fatalf("null fee rate must inherit, got %v", *got)
	}
	if got := settings["easypay_wxpay"].FeeRate; got == nil || *got != 0 {
		t.Fatalf("explicit zero fee rate must be preserved, got %v", got)
	}
	if _, ok := settings["stripe"]; ok {
		t.Fatal("empty override must be removed")
	}
}

func TestParseChannelSettingsRejectsInvalidInput(t *testing.T) {
	longName := strings.Repeat("名", maxChannelDisplayNameRunes+1)
	tests := []struct {
		name string
		raw  string
	}{
		{name: "malformed json", raw: `{`},
		{name: "null object", raw: `null`},
		{name: "trailing value", raw: `{}` + `{}`},
		{name: "unknown field", raw: `{"stripe":{"unknown":true}}`},
		{name: "null channel setting", raw: `{"stripe":null}`},
		{name: "invalid id", raw: `{"Official Alipay":{"fee_rate":1}}`},
		{name: "negative fee", raw: `{"stripe":{"fee_rate":-0.01}}`},
		{name: "fee over one hundred", raw: `{"stripe":{"fee_rate":100.01}}`},
		{name: "fee precision", raw: `{"stripe":{"fee_rate":1.001}}`},
		{name: "control character", raw: `{"stripe":{"display_name":"bad\u000aname"}}`},
		{name: "trimmed control character", raw: `{"stripe":{"display_name":"\u000aname"}}`},
		{name: "long name", raw: `{"stripe":{"display_name":"` + longName + `"}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseChannelSettings(test.raw); err == nil {
				t.Fatalf("ParseChannelSettings(%s) unexpectedly succeeded", test.raw)
			}
		})
	}
}

func TestChannelSettingUnmarshalRejectsUnknownFields(t *testing.T) {
	var settings ChannelSettings
	if err := json.Unmarshal([]byte(`{"stripe":{"fee_rate":1,"unknown":true}}`), &settings); err == nil {
		t.Fatal("admin request decoding unexpectedly accepted an unknown channel-setting field")
	}
}

func TestApplyChannelSettingsAndResolveFeeRate(t *testing.T) {
	zero := 0.0
	custom := 1.25
	settings := ChannelSettings{
		"easypay_alipay": {
			DisplayName: "支付宝优惠通道",
			FeeRate:     &custom,
		},
		"official_alipay": {
			DisplayName: "支付宝备用通道",
		},
		"stripe": {
			FeeRate: &zero,
		},
	}
	input := []MethodOption{
		{ID: "easypay_alipay", PaymentType: MethodAlipay, ProviderKey: ProviderEasyPay, DisplayName: "old", FeeRate: 2.5},
		{ID: "official_alipay", PaymentType: MethodAlipay, ProviderKey: ProviderAlipay, FeeRate: 2.5},
		{ID: "stripe", PaymentType: MethodStripe, ProviderKey: ProviderStripe, FeeRate: 2.5},
	}

	got := ApplyChannelSettings(input, settings)
	if got[0].DisplayName != "支付宝优惠通道" || got[0].FeeRate != custom {
		t.Fatalf("EasyPay override = %+v", got[0])
	}
	if got[1].DisplayName != "支付宝备用通道" || got[1].FeeRate != 2.5 {
		t.Fatalf("official override = %+v", got[1])
	}
	if got[2].FeeRate != 0 {
		t.Fatalf("explicit zero fee rate = %v", got[2].FeeRate)
	}
	if input[0].DisplayName != "old" || input[0].FeeRate != 2.5 {
		t.Fatal("ApplyChannelSettings mutated its input")
	}

	if fee := ResolveFeeRate(MethodAlipay, ProviderEasyPay, 2.5, settings); fee != custom {
		t.Fatalf("explicit provider fee = %v", fee)
	}
	if fee := ResolveFeeRate(MethodAlipay, ProviderAlipay, 2.5, settings); fee != 2.5 {
		t.Fatalf("inherited provider fee = %v", fee)
	}
	if fee := ResolveFeeRate(MethodStripe, ProviderStripe, 2.5, settings); fee != 0 {
		t.Fatalf("zero provider fee = %v", fee)
	}
	if fee := ResolveFeeRate(MethodAlipay, "", 2.5, settings); fee != 2.5 {
		t.Fatalf("legacy provider-agnostic fee = %v", fee)
	}
}
