package handler

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/custom/paymentchannels"
)

func TestCheckoutInfoJSONIncludesDisabledPaymentNotice(t *testing.T) {
	payload, err := json.Marshal(checkoutInfoResponse{
		MethodOptions: []paymentchannels.MethodOption{{
			ID:                   "easypay_alipay",
			PaymentType:          paymentchannels.MethodAlipay,
			ProviderKey:          paymentchannels.ProviderEasyPay,
			PaymentNoticeEnabled: false,
		}},
	})
	if err != nil {
		t.Fatalf("marshal checkout-info response: %v", err)
	}

	var decoded struct {
		MethodOptions []map[string]any `json:"method_options"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode checkout-info response: %v", err)
	}
	if len(decoded.MethodOptions) != 1 {
		t.Fatalf("method_options length = %d, want 1", len(decoded.MethodOptions))
	}
	value, ok := decoded.MethodOptions[0]["payment_notice_enabled"]
	if !ok {
		t.Fatalf("checkout-info omitted payment_notice_enabled: %s", payload)
	}
	if enabled, ok := value.(bool); !ok || enabled {
		t.Fatalf("payment_notice_enabled = %#v, want false", value)
	}
}
