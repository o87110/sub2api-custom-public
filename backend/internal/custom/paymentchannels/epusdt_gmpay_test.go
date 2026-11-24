package paymentchannels

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGmpaySignMatchesUpstreamVector pins our HMAC-SHA256 implementation to
// the fixed vector published by EPUSDT v2.1.4 (src/util/sign/sign_test.go):
// params {pid:"1000", name:"VIP", amount:100}, secret "test-secret".
func TestGmpaySignMatchesUpstreamVector(t *testing.T) {
	values := map[string]any{
		"signature": "ignored",
		"pid":       "1000",
		"empty":     "",
		"nil":       nil,
		"name":      "VIP",
		"amount":    100,
	}
	const want = "ced9141fab53a83d1178f903e7c22d8a2a7033520f31e319934e92455a008b6c"
	if got := GmpaySign(values, "test-secret"); got != want {
		t.Fatalf("GmpaySign() = %q, want %q", got, want)
	}
	// Numbers must be formatted with -1 precision: 100.0 and 100 sign alike.
	floats := map[string]any{"pid": "1000", "name": "VIP", "amount": 100.0}
	if got := GmpaySign(floats, "test-secret"); got != want {
		t.Fatalf("GmpaySign(float) = %q, want %q", got, want)
	}
	// A trailing decimal must change the signature, not collapse onto "100".
	decimal := map[string]any{"pid": "1000", "name": "VIP", "amount": 100.5}
	if got := GmpaySign(decimal, "test-secret"); got == want {
		t.Fatal("GmpaySign() should differ for amount=100.5")
	}
}

func TestParseGmpayNetworks(t *testing.T) {
	networks, err := ParseGmpayNetworks("polygon, bep20, trc20")
	if err != nil {
		t.Fatalf("ParseGmpayNetworks() error = %v", err)
	}
	if got := networks[0].Code; got != "bep20" {
		t.Fatalf("first network = %q, want bep20", got)
	}
	if got := networks[1].UpstreamNetwork; got != "tron" {
		t.Fatalf("second network upstream = %q, want tron", got)
	}
	if got := networks[2].UpstreamNetwork; got != "polygon" {
		t.Fatalf("third network upstream = %q, want polygon", got)
	}
	if _, err := ParseGmpayNetworks("bep20,bep20"); err == nil {
		t.Fatal("duplicate network should fail")
	}
	if _, err := ParseGmpayNetworks("plasma"); err == nil {
		t.Fatal("network outside the GMPay whitelist should fail")
	}
	if _, err := ParseGmpayNetworks(""); err == nil {
		t.Fatal("empty network list should fail")
	}
}

func TestValidateGmpayProviderMode(t *testing.T) {
	base := func() map[string]string {
		return map[string]string{
			EasyPayProtocolConfigKey: EasyPayProtocolGmpay,
			"apiBase":                "https://gw.example.com",
			GmpayNetworksConfigKey:   "bep20,trc20",
		}
	}
	if err := ValidateGmpayProviderMode(base(), BepusdtPaymentType, "popup", false); err != nil {
		t.Fatalf("popup mode should pass: %v", err)
	}
	if err := ValidateGmpayProviderMode(base(), BepusdtPaymentType, "qrcode", false); err != nil {
		t.Fatalf("qrcode mode should pass: %v", err)
	}
	if err := ValidateGmpayProviderMode(base(), BepusdtPaymentType, "redirect", false); err == nil {
		t.Fatal("redirect mode should fail")
	}
	if err := ValidateGmpayProviderMode(base(), BepusdtPaymentType, "popup", true); err == nil {
		t.Fatal("refunds should fail")
	}
	if err := ValidateGmpayProviderMode(base(), "alipay,usdt", "popup", false); err == nil {
		t.Fatal("non-usdt types should fail")
	}
	if err := ValidateGmpayProviderMode(base(), BepusdtPaymentType, "popup", false); err != nil {
		t.Fatal("sanity")
	}
	httpBase := base()
	httpBase["apiBase"] = "http://gw.example.com"
	if err := ValidateGmpayProviderMode(httpBase, BepusdtPaymentType, "popup", false); err == nil {
		t.Fatal("plain HTTP api base should fail")
	}
	noNetworks := base()
	noNetworks[GmpayNetworksConfigKey] = ""
	if err := ValidateGmpayProviderMode(noNetworks, BepusdtPaymentType, "popup", false); err == nil {
		t.Fatal("missing networks should fail")
	}
	// Non-GMPay configs pass through untouched (mirrors ValidateBepusdtProviderMode).
	if err := ValidateGmpayProviderMode(map[string]string{EasyPayProtocolConfigKey: EasyPayProtocolRainbow}, "alipay", "popup", true); err != nil {
		t.Fatalf("rainbow config should be ignored: %v", err)
	}
}

func TestProviderCatalogBuildsGmpayNetworkOptions(t *testing.T) {
	options := (ProviderCatalog{}).BuildOptions([]ProviderRecord{{
		ID:             1,
		ProviderKey:    ProviderEasyPay,
		SupportedTypes: BepusdtPaymentType,
		Config: map[string]string{
			EasyPayProtocolConfigKey: EasyPayProtocolGmpay,
			GmpayNetworksConfigKey:   "trc20,polygon,bep20",
		},
	}}, 0, nil, false)
	if len(options) != 1 || options[0].ID != "easypay_usdt" {
		t.Fatalf("options = %+v", options)
	}
	if got := options[0].NetworkOptions; len(got) != 3 || got[0].Code != "bep20" || got[1].Code != "trc20" || got[2].Code != "polygon" {
		t.Fatalf("network options = %+v", got)
	}
}

func TestInstanceCoordinatorRoutesNetworkToMatchingProtocol(t *testing.T) {
	loader := &stubInstanceLoader{records: []InstanceRecord{
		{ID: 1, ProviderKey: ProviderEasyPay, SupportedTypes: BepusdtPaymentType, Config: map[string]string{
			EasyPayProtocolConfigKey: EasyPayProtocolBepusdt, BepusdtNetworksConfigKey: "bep20,trc20",
		}, Enabled: true},
		{ID: 2, ProviderKey: ProviderEasyPay, SupportedTypes: BepusdtPaymentType, Config: map[string]string{
			EasyPayProtocolConfigKey: EasyPayProtocolGmpay, GmpayNetworksConfigKey: "polygon",
		}, Enabled: true},
		{ID: 3, ProviderKey: ProviderEasyPay, SupportedTypes: BepusdtPaymentType, Config: map[string]string{
			EasyPayProtocolConfigKey: EasyPayProtocolGmpay, GmpayNetworksConfigKey: "bep20",
		}, Enabled: true},
	}}
	result, err := NewInstanceCoordinator().Select(context.Background(), loader, InstanceSelectionRequest{
		PaymentType: BepusdtPaymentType, PaymentNetwork: "polygon", Strategy: StrategyRoundRobin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Selection == nil || result.Selection.InstanceID != "2" {
		t.Fatalf("polygon selection = %+v, want GMPay instance 2", result.Selection)
	}
	result, err = NewInstanceCoordinator().Select(context.Background(), loader, InstanceSelectionRequest{
		PaymentType: BepusdtPaymentType, PaymentNetwork: "bep20", Strategy: StrategyRoundRobin,
	})
	if err != nil {
		t.Fatal(err)
	}
	// bep20 is whitelisted on both the BEpusdt (1) and the GMPay (3)
	// instance; either may serve it, but never the polygon-only instance 2.
	if result.Selection == nil || (result.Selection.InstanceID != "1" && result.Selection.InstanceID != "3") {
		t.Fatalf("bep20 selection = %+v, want instance 1 or 3", result.Selection)
	}
	result, err = NewInstanceCoordinator().Select(context.Background(), loader, InstanceSelectionRequest{
		PaymentType: BepusdtPaymentType, PaymentNetwork: "trc20", Strategy: StrategyRoundRobin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Selection == nil || result.Selection.InstanceID != "1" {
		t.Fatalf("trc20 selection = %+v, want BEpusdt instance 1", result.Selection)
	}
}

func TestValidatePaymentNetworkSelectionAcceptsGmpay(t *testing.T) {
	selection := &OrderSelection{
		ProviderKey: ProviderEasyPay,
		Config: map[string]string{
			EasyPayProtocolConfigKey: EasyPayProtocolGmpay,
			GmpayNetworksConfigKey:   "bep20,trc20,polygon",
		},
	}
	if err := ValidatePaymentNetworkSelection(BepusdtPaymentType, "trc20", selection); err != nil {
		t.Fatalf("GMPay network selection should pass: %v", err)
	}
	if err := ValidatePaymentNetworkSelection(BepusdtPaymentType, "plasma", selection); err == nil {
		t.Fatal("network outside the GMPay whitelist should fail")
	}
	if err := ValidatePaymentNetworkSelection(BepusdtPaymentType, "", selection); err == nil {
		t.Fatal("missing network should fail for GMPay")
	}
}

func TestGmpayClientCreateAndQuery(t *testing.T) {
	const (
		pid        = "1000"
		secretKey  = "gmpay-secret"
		upstreamID = "3nQ9pL2xV7sK1mR8cT4y_aZ"
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/payments/gmpay/v1/order/create-transaction":
			var values map[string]any
			if err := json.NewDecoder(r.Body).Decode(&values); err != nil {
				t.Errorf("decode request: %v", err)
				return
			}
			if values["pid"] != pid {
				t.Errorf("pid = %v", values["pid"])
			}
			if values["network"] != "binance" {
				t.Errorf("network = %v, want binance", values["network"])
			}
			if values["token"] != "usdt" {
				t.Errorf("token = %v, want usdt", values["token"])
			}
			if values["currency"] != "cny" {
				t.Errorf("currency = %v, want cny", values["currency"])
			}
			if got, ok := values["callback_version"].(float64); !ok || got != 2 {
				t.Errorf("callback_version = %v, want 2", values["callback_version"])
			}
			if values["signature"] != GmpaySign(values, secretKey) {
				t.Errorf("signature mismatch")
			}
			_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"` + upstreamID + `","order_id":"sub2_20260101abcdefgh","amount":100.0000,"currency":"CNY","actual_amount":14.2857,"receive_address":"0xAbCdEf1234567890","token":"USDT","status":1,"expiration_time":1767225600,"payment_url":"https://pay.example/cashier/` + upstreamID + `"}}`))
		case "/pay/checkout-counter-resp/" + upstreamID:
			if r.Method != http.MethodGet {
				t.Errorf("checkout query method = %s, want GET", r.Method)
			}
			_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"` + upstreamID + `","amount":100.0000,"actual_amount":14.2857,"token":"USDT","currency":"CNY","receive_address":"0xAbCdEf1234567890","network":"binance","status":5,"expiration_time":1767225600,"payment_type":"gmpay","is_selected":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewGmpayClient(server.URL, pid, secretKey)
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.CreateTransaction(context.Background(), GmpayCreateRequest{
		OrderID: "sub2_20260101abcdefgh", NotifyURL: "https://sub.example/api/v1/payment/webhook/easypay",
		RedirectURL: "https://sub.example/payment/result", Amount: 100, Fiat: GmpayFiat,
		Token: GmpayToken, Network: "binance", Name: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.TradeID != upstreamID || created.OrderID != "sub2_20260101abcdefgh" {
		t.Fatalf("created = %+v", created)
	}
	if created.Amount != 100 || created.ActualAmount != 14.2857 {
		t.Fatalf("amounts = %+v", created)
	}
	if created.ReceiveAddress != "0xAbCdEf1234567890" || created.PaymentURL == "" || created.ExpirationTime != 1767225600 {
		t.Fatalf("created = %+v", created)
	}
	queried, err := client.QueryCheckout(context.Background(), created.TradeID)
	if err != nil {
		t.Fatal(err)
	}
	if queried.Status != GmpayStatusConfirming || queried.Network != "binance" || queried.Amount != 100 {
		t.Fatalf("queried = %+v", queried)
	}
	if !queried.IsSelected || queried.PaymentType != "gmpay" {
		t.Fatalf("queried = %+v", queried)
	}
}

func TestGmpayClientSurfacesOrderConflict(t *testing.T) {
	// The upstream carries the stable business errno inside the JSON body with
	// an HTTP 400 status; both transports must surface code=10002.
	newServer := func(status int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"status_code":10002,"message":"order ID conflict","data":null}`))
		}))
	}
	for _, tc := range []struct {
		name   string
		status int
	}{{"http-400", http.StatusBadRequest}, {"http-200", http.StatusOK}} {
		t.Run(tc.name, func(t *testing.T) {
			server := newServer(tc.status)
			defer server.Close()
			client, err := NewGmpayClient(server.URL, "1000", "secret")
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.CreateTransaction(context.Background(), GmpayCreateRequest{
				OrderID: "sub2_20260101abcdefgh", NotifyURL: "https://sub.example/notify",
				Amount: 100, Fiat: GmpayFiat, Token: GmpayToken, Network: "tron",
			})
			if err == nil || !strings.Contains(err.Error(), "code=10002") {
				t.Fatalf("conflict error = %v, want code=10002", err)
			}
		})
	}
}

func TestIsNativeUsdtProtocol(t *testing.T) {
	for _, protocol := range []string{EasyPayProtocolBepusdt, EasyPayProtocolGmpay} {
		if !IsNativeUsdtProtocol(protocol) {
			t.Fatalf("IsNativeUsdtProtocol(%q) = false, want true", protocol)
		}
	}
	for _, protocol := range []string{EasyPayProtocolRainbow, "", "easypay"} {
		if IsNativeUsdtProtocol(protocol) {
			t.Fatalf("IsNativeUsdtProtocol(%q) = true, want false", protocol)
		}
	}
}

func TestParseGmpayNotification(t *testing.T) {
	secretKey := "gmpay-secret"
	values := map[string]any{
		"pid": "1000", "trade_id": "3nQ9pL2xV7sK1mR8cT4y_aZ",
		"order_id": "sub2_20260101abcdefgh", "amount": 100.0000,
		"actual_amount": 14.2857, "receive_address": "0xAbCdEf1234567890",
		"token": "USDT", "network": "binance",
		"block_transaction_id": "0xabc123", "status": 2,
	}
	values["signature"] = GmpaySign(values, secretKey)
	raw, _ := json.Marshal(values)
	notification, err := ParseGmpayNotification(string(raw), secretKey)
	if err != nil {
		t.Fatal(err)
	}
	if notification.Status != GmpayStatusPaid || notification.OrderID != "sub2_20260101abcdefgh" ||
		notification.Network != "binance" || notification.Amount != 100 || notification.ActualAmount != 14.2857 {
		t.Fatalf("notification = %+v", notification)
	}
	// Tampered payload must fail verification.
	if _, err := ParseGmpayNotification(strings.Replace(string(raw), "sub2_20260101abcdefgh", "sub2_tampered_order99", 1), secretKey); err == nil {
		t.Fatal("tampered notification should fail")
	}
	// Wrong key must fail verification (secret rotation case).
	if _, err := ParseGmpayNotification(string(raw), "rotated-secret"); err == nil {
		t.Fatal("wrong secret should fail")
	}
	// A callback naming a token other than USDT does not belong to this
	// integration and must be rejected.
	wrongToken := map[string]any{
		"pid": "1000", "trade_id": "t", "order_id": "o", "amount": 100,
		"actual_amount": 1, "token": "TRX", "status": 2,
	}
	wrongToken["signature"] = GmpaySign(wrongToken, secretKey)
	rawWrongToken, _ := json.Marshal(wrongToken)
	if _, err := ParseGmpayNotification(string(rawWrongToken), secretKey); err == nil {
		t.Fatal("non-USDT token should fail")
	}
	missingToken := map[string]any{
		"pid": "1000", "trade_id": "t", "order_id": "o", "amount": 100,
		"actual_amount": 1, "status": 2,
	}
	missingToken["signature"] = GmpaySign(missingToken, secretKey)
	rawMissingToken, _ := json.Marshal(missingToken)
	if _, err := ParseGmpayNotification(string(rawMissingToken), secretKey); err == nil {
		t.Fatal("missing token should fail")
	}
	// Callbacks without the network field (v1 shape) still verify but carry an empty network.
	delete(values, "network")
	values["signature"] = GmpaySign(values, secretKey)
	raw, _ = json.Marshal(values)
	notification, err = ParseGmpayNotification(string(raw), secretKey)
	if err != nil {
		t.Fatal(err)
	}
	if notification.Network != "" {
		t.Fatalf("v1 notification network = %q, want empty", notification.Network)
	}
	// Incomplete payload must fail.
	broken := map[string]any{"pid": "1000", "trade_id": "t", "order_id": "o", "amount": 0, "status": 2}
	broken["signature"] = GmpaySign(broken, secretKey)
	rawBroken, _ := json.Marshal(broken)
	if _, err := ParseGmpayNotification(string(rawBroken), secretKey); err == nil {
		t.Fatal("incomplete notification should fail")
	}
}
