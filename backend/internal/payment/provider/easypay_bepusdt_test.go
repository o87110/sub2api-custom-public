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

func TestEasyPayBepusdtNativeCreateQueryAndCancel(t *testing.T) {
	token := "native-token"
	paths := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		switch r.URL.Path {
		case "/api/v1/order/create-transaction":
			if body["trade_type"] != "usdt.polygon" {
				t.Errorf("trade_type = %v", body["trade_type"])
			}
			if body["notify_url"] != "https://sub.example/notify" {
				t.Errorf("notify_url = %v", body["notify_url"])
			}
			if body["redirect_url"] != "https://sub.example/result" {
				t.Errorf("redirect_url = %v", body["redirect_url"])
			}
			_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"native-trade","amount":"12.34","payment_url":"https://pay.example/native"}}`))
		case "/api/v1/pay/info":
			_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"native-trade","money":"12.34","status":2}}`))
		case "/api/v1/order/cancel-transaction":
			_, _ = w.Write([]byte(`{"status_code":200,"message":"success"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider, err := NewEasyPay("native", map[string]string{
		"apiBase": server.URL, "apiToken": token,
		"notifyUrl": "https://sub.example/notify", "returnUrl": "https://sub.example/result",
		paymentchannels.EasyPayProtocolConfigKey: paymentchannels.EasyPayProtocolBepusdt,
		paymentchannels.BepusdtNetworksConfigKey: "polygon",
		"paymentMode":                            "popup",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := provider.SupportedTypes(); len(got) != 1 || got[0] != paymentchannels.BepusdtPaymentType {
		t.Fatalf("supported types = %v", got)
	}
	created, err := provider.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		OrderID: "order-1", Amount: "12.34", PaymentType: "usdt", PaymentNetwork: "polygon", Subject: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.TradeNo != "native-trade" || created.PayURL == "" {
		t.Fatalf("created = %+v", created)
	}
	queried, err := provider.QueryOrder(context.Background(), created.TradeNo)
	if err != nil {
		t.Fatal(err)
	}
	if queried.Status != payment.ProviderStatusPaid || queried.Amount != 12.34 {
		t.Fatalf("queried = %+v", queried)
	}
	if err := provider.CancelPayment(context.Background(), created.TradeNo); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 3 || paths[0] != "/api/v1/order/create-transaction" || paths[1] != "/api/v1/pay/info" || paths[2] != "/api/v1/order/cancel-transaction" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestEasyPayBepusdtNativeNotification(t *testing.T) {
	token := "native-token"
	values := map[string]any{"trade_id": "trade-1", "order_id": "order-1", "amount": 12.34, "status": 2}
	values["signature"] = paymentchannels.BepusdtSign(values, token)
	raw, _ := json.Marshal(values)
	provider, err := NewEasyPay("native", map[string]string{
		"apiBase": "https://bepusdt.example", "apiToken": token,
		"notifyUrl": "https://sub.example/notify", "returnUrl": "https://sub.example/result",
		paymentchannels.EasyPayProtocolConfigKey: paymentchannels.EasyPayProtocolBepusdt,
		paymentchannels.BepusdtNetworksConfigKey: "bep20",
	})
	if err != nil {
		t.Fatal(err)
	}
	notification, err := provider.VerifyNotification(context.Background(), string(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	if notification.Status != payment.ProviderStatusSuccess || notification.OrderID != "order-1" {
		t.Fatalf("notification = %+v", notification)
	}
}

func TestEasyPayBepusdtNativeConfirmNotification(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr bool
	}{
		{name: "confirmed", payload: `{"status_code":200,"data":{"trade_id":"trade-1","order_id":"order-1","money":"12.34","status":2}}`},
		{name: "wrong order", payload: `{"status_code":200,"data":{"trade_id":"trade-1","order_id":"order-2","money":"12.34","status":2}}`, wantErr: true},
		{name: "wrong trade", payload: `{"status_code":200,"data":{"trade_id":"trade-2","order_id":"order-1","money":"12.34","status":2}}`, wantErr: true},
		{name: "wrong amount", payload: `{"status_code":200,"data":{"trade_id":"trade-1","order_id":"order-1","money":"12.35","status":2}}`, wantErr: true},
		{name: "unpaid", payload: `{"status_code":200,"data":{"trade_id":"trade-1","order_id":"order-1","money":"12.34","status":1}}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/pay/info" {
					t.Errorf("query path = %s", r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode query: %v", err)
				} else if body["trade_id"] != "trade-1" {
					t.Errorf("query trade_id = %v", body["trade_id"])
				}
				_, _ = w.Write([]byte(tt.payload))
			}))
			defer server.Close()
			provider, err := NewEasyPay("native", map[string]string{
				"apiBase": server.URL, "apiToken": "native-token",
				"notifyUrl": "https://sub.example/notify", "returnUrl": "https://sub.example/result",
				paymentchannels.EasyPayProtocolConfigKey: paymentchannels.EasyPayProtocolBepusdt,
				paymentchannels.BepusdtNetworksConfigKey: "bep20",
			})
			if err != nil {
				t.Fatal(err)
			}
			err = provider.ConfirmNotification(context.Background(), &payment.PaymentNotification{
				TradeNo: "trade-1", OrderID: "order-1", Amount: 12.34, Status: payment.NotificationStatusSuccess,
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("ConfirmNotification error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
