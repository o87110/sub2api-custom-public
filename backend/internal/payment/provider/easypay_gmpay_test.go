package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/custom/paymentchannels"
	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func gmpayTestConfig(apiBase string) map[string]string {
	return map[string]string{
		"apiBase": apiBase, "pid": "1000", "secretKey": "gmpay-secret",
		"notifyUrl": "https://sub.example/notify", "returnUrl": "https://sub.example/result",
		paymentchannels.EasyPayProtocolConfigKey: paymentchannels.EasyPayProtocolGmpay,
		paymentchannels.GmpayNetworksConfigKey:   "bep20,trc20,polygon",
		"paymentMode":                            "qrcode",
	}
}

func TestEasyPayGmpayNativeCreateQueryAndConfirm(t *testing.T) {
	const tradeID = "gmpay-trade-1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/payments/gmpay/v1/order/create-transaction":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode request: %v", err)
				return
			}
			if body["pid"] != "1000" {
				t.Errorf("pid = %v", body["pid"])
			}
			if body["network"] != "tron" {
				t.Errorf("network = %v, want tron", body["network"])
			}
			if body["token"] != "usdt" {
				t.Errorf("token = %v", body["token"])
			}
			if body["callback_version"] != float64(2) {
				t.Errorf("callback_version = %v, want 2", body["callback_version"])
			}
			if body["signature"] != paymentchannels.GmpaySign(body, "gmpay-secret") {
				t.Errorf("signature mismatch")
			}
			_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"` + tradeID + `","order_id":"sub2_order1","amount":100,"currency":"CNY","actual_amount":14.2857,"receive_address":"TTronAddress001","token":"USDT","status":1,"expiration_time":1767225600,"payment_url":"https://pay.example/cashier"}}`))
		case "/pay/checkout-counter-resp/" + tradeID:
			if r.Method != http.MethodGet {
				t.Errorf("checkout method = %s, want GET", r.Method)
			}
			_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"` + tradeID + `","amount":100,"actual_amount":14.2857,"token":"USDT","currency":"CNY","receive_address":"TTronAddress001","network":"tron","status":2,"expiration_time":1767225600,"payment_type":"gmpay","is_selected":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider, err := NewEasyPay("gmpay", gmpayTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if got := provider.SupportedTypes(); len(got) != 1 || got[0] != paymentchannels.BepusdtPaymentType {
		t.Fatalf("supported types = %v", got)
	}
	created, err := provider.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		OrderID: "sub2_order1", Amount: "100", PaymentType: "usdt", PaymentNetwork: "trc20", Subject: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.TradeNo != tradeID || created.PayURL != "https://pay.example/cashier" {
		t.Fatalf("created = %+v", created)
	}
	// QR mode renders the receiving address; CryptoAmount carries the USDT due.
	if created.QRCode != "TTronAddress001" || created.CryptoAmount != 14.2857 {
		t.Fatalf("created = %+v", created)
	}
	queried, err := provider.QueryOrder(context.Background(), tradeID)
	if err != nil {
		t.Fatal(err)
	}
	if queried.Status != payment.ProviderStatusPaid || queried.Amount != 100 {
		t.Fatalf("queried = %+v", queried)
	}
	// A paid projection must be flagged as query-only evidence: a
	// switched-network child payment settles the parent without touching its
	// network, amount or address, so reconciliation must not auto-fulfill on
	// it (the signed callback carries the settled child's network instead).
	if queried.Metadata["gmpay_query_paid"] != "true" {
		t.Fatalf("query metadata = %+v", queried.Metadata)
	}
	if err := provider.ConfirmNotification(context.Background(), &payment.PaymentNotification{
		TradeNo: tradeID, OrderID: "sub2_order1", Amount: 100, Status: payment.NotificationStatusSuccess,
	}); err != nil {
		t.Fatalf("confirm failed: %v", err)
	}
}

func TestEasyPayGmpayNativeQueryStatusMapping(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantStatus string
	}{
		{name: "wait pay", status: paymentchannels.GmpayStatusWaitPay, wantStatus: payment.ProviderStatusPending},
		{name: "paid", status: paymentchannels.GmpayStatusPaid, wantStatus: payment.ProviderStatusPaid},
		{name: "expired", status: paymentchannels.GmpayStatusExpired, wantStatus: payment.ProviderStatusFailed},
		{name: "confirming", status: paymentchannels.GmpayStatusConfirming, wantStatus: payment.ProviderStatusPending},
		// Manual review must stay pending so a possibly-paid order is never
		// auto-failed; it is resolved by human reconciliation.
		{name: "manual review", status: paymentchannels.GmpayStatusManualReview, wantStatus: payment.ProviderStatusPending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"t1","amount":100,"status":` + jsonInt(tt.status) + `}}`))
			}))
			defer server.Close()
			provider, err := NewEasyPay("gmpay", gmpayTestConfig(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			queried, err := provider.QueryOrder(context.Background(), "t1")
			if err != nil {
				t.Fatal(err)
			}
			if queried.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", queried.Status, tt.wantStatus)
			}
			// Status 6 must carry the manual-review flag so the local
			// lifecycle holds expiry/cancel instead of releasing inventory.
			if tt.status == paymentchannels.GmpayStatusManualReview && queried.Metadata["gmpay_manual_review"] != "true" {
				t.Fatalf("manual review metadata = %+v", queried.Metadata)
			}
		})
	}
}

func TestEasyPayGmpayNativeConfirmAmountMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Upstream reports 99 while the notification claimed 100: exact
		// decimal comparison must reject it.
		_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"t1","amount":99,"status":2}}`))
	}))
	defer server.Close()
	provider, err := NewEasyPay("gmpay", gmpayTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	err = provider.ConfirmNotification(context.Background(), &payment.PaymentNotification{
		TradeNo: "t1", OrderID: "o1", Amount: 100, Status: payment.NotificationStatusSuccess,
	})
	if err == nil {
		t.Fatal("amount mismatch should fail confirmation")
	}
}

func TestEasyPayGmpayNativeNotification(t *testing.T) {
	values := map[string]any{
		"pid": "1000", "trade_id": "t1", "order_id": "sub2_order1",
		"amount": 100, "actual_amount": 14.2857, "receive_address": "TTronAddress001",
		"token": "USDT", "network": "tron", "block_transaction_id": "0xabc", "status": 2,
	}
	values["signature"] = paymentchannels.GmpaySign(values, "gmpay-secret")
	raw, _ := json.Marshal(values)
	provider, err := NewEasyPay("gmpay", gmpayTestConfig("https://gmpay.example"))
	if err != nil {
		t.Fatal(err)
	}
	notification, err := provider.VerifyNotification(context.Background(), string(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	if notification.Status != payment.ProviderStatusSuccess || notification.OrderID != "sub2_order1" || notification.Amount != 100 {
		t.Fatalf("notification = %+v", notification)
	}
	if notification.Metadata["protocol"] != paymentchannels.EasyPayProtocolGmpay ||
		notification.Metadata["pid"] != "1000" || notification.Metadata["network"] != "tron" ||
		notification.Metadata["actual_amount"] != "14.2857" {
		t.Fatalf("metadata = %+v", notification.Metadata)
	}
	// Tampering must fail verification.
	tampered := string(raw[:len(raw)-2]) + `,"x":1}`
	if _, err := provider.VerifyNotification(context.Background(), tampered, nil); err == nil {
		t.Fatal("tampered notification should fail")
	}
}

func TestEasyPayGmpayNativeRejectsRefundAndUnsupportedNetwork(t *testing.T) {
	provider, err := NewEasyPay("gmpay", gmpayTestConfig("https://gmpay.example"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Refund(context.Background(), payment.RefundRequest{OrderID: "o1", Amount: "1"}); err == nil {
		t.Fatal("gmpay refund should be rejected")
	}
	// plasma is whitelisted for BEpusdt only.
	if _, err := provider.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		OrderID: "o1", Amount: "100", PaymentType: "usdt", PaymentNetwork: "plasma",
	}); err == nil {
		t.Fatal("plasma network should be rejected for GMPay")
	}
}

func jsonInt(value int) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
